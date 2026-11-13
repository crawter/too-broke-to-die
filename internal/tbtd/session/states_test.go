// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/settings"
	"too-broke-to-die/internal/tbtd/simulation"
)

func change(t *testing.T, policy *FramePolicy, event StateEvent, destination AppState) {
	t.Helper()
	if err := policy.Transition(context.Background(), event, destination); err != nil {
		t.Fatalf("%s: %v", event, err)
	}
}

func TestRestorationSaveAcknowledgmentPausesUntilResume(t *testing.T) {
	ctx := context.Background()
	world := frameWorld(t)
	policy := NewFramePolicy()
	if err := policy.BindWorld(ctx, world); err != nil {
		t.Fatal(err)
	}
	change(t, policy, ContinueGame, "")
	change(t, policy, BeginSave, "")
	if policy.State() != StateSaving || !world.PlayerStatus().Suspended {
		t.Fatal("restoration save did not suspend gameplay")
	}
	begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond})
	if drain(t, policy, world) != 0 {
		t.Fatal("gameplay advanced before acknowledgment")
	}
	change(t, policy, SaveSuccess, StatePause)
	if policy.State() != StatePause || !world.PlayerStatus().Suspended {
		t.Fatal("acknowledged restoration did not remain paused")
	}
	begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond})
	if drain(t, policy, world) != 0 {
		t.Fatal("gameplay advanced without explicit Resume")
	}
	change(t, policy, ResumeGame, "")
	if policy.State() != StateActive || world.PlayerStatus().Suspended {
		t.Fatal("Resume did not return to active gameplay")
	}
	begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond})
	if drain(t, policy, world) != 3 {
		t.Fatal("gameplay did not advance after Resume")
	}
	_, state := world.Snapshot(ctx).Data(ctx)
	if state.Tick != 3 {
		t.Fatalf("world advanced %d ticks, want 3", state.Tick)
	}
}

func TestStateTransitionsAndAcknowledgedSaving(t *testing.T) {
	ctx := context.Background()
	policy := NewFramePolicy()
	if policy.State() != StateTitle {
		t.Fatal("initial state")
	}
	if err := policy.Transition(ctx, ResumeGame, ""); !errors.Is(err, InvalidAction) {
		t.Fatal("invalid transition accepted")
	}
	change(t, policy, NewGame, "")
	if policy.State() != StateSaving {
		t.Fatal("new game must save")
	}
	if err := policy.Transition(ctx, SaveSuccess, StateActive); !errors.Is(err, InvalidAction) {
		t.Fatal("new game bypassed restoration")
	}
	change(t, policy, SaveFailure, "")
	if policy.State() != StateError {
		t.Fatal("failed save")
	}
	change(t, policy, RetryOperation, "")
	change(t, policy, SaveSuccess, StateRestoring)
	change(t, policy, RestoreActive, "")
	if policy.State() != StatePause {
		t.Fatal("restored active play was not paused")
	}
	change(t, policy, ResumeGame, "")
	change(t, policy, OpenLedger, "")
	change(t, policy, CloseLedger, "")
	if policy.State() != StateActive {
		t.Fatal("ledger lost active source")
	}
	change(t, policy, OpenPause, "")
	change(t, policy, OpenLedger, "")
	change(t, policy, CloseLedger, "")
	if policy.State() != StatePause {
		t.Fatal("ledger lost pause source")
	}
	change(t, policy, ResumeGame, "")
	change(t, policy, StartScene, "")
	change(t, policy, OpenPause, "")
	change(t, policy, ResumeGame, "")
	if policy.State() != StateScene {
		t.Fatal("pause lost scene source")
	}
	change(t, policy, BeginSave, "")
	change(t, policy, SaveSuccess, StateScene)
	change(t, policy, FinishScene, "")
	change(t, policy, BeginSave, "")
	change(t, policy, SaveSuccess, StateDead)
	change(t, policy, RetryDeath, "")
	change(t, policy, RestoreScene, "")
	change(t, policy, BeginSave, "")
	change(t, policy, SaveSuccess, StateEnd)
	change(t, policy, ReplayEnding, "")
	if policy.State() != StateScene {
		t.Fatal("ending replay")
	}
}

func TestFocusAndDisconnectPrecedeActionsAndTicks(t *testing.T) {
	ctx := context.Background()
	for _, loss := range []FrameInput{
		{Focus: FocusLost},
		{Devices: []DeviceChange{{Device: settings.Controller, Active: true, Connected: false}}},
	} {
		world := frameWorld(t)
		policy := activePolicy(t, world)
		begin(t, policy, FrameInput{Elapsed: time.Millisecond, Edges: []ActionEdge{{Action: settings.Throw, Kind: Pressed}}})
		loss.Elapsed = 50 * time.Millisecond
		loss.Edges = []ActionEdge{{Action: settings.Melee, Kind: Pressed}}
		loss.Selections = []MenuSelection{{Action: "resume"}}
		decision := begin(t, policy, loss)
		if !decision.Interrupted || decision.State != StatePause || len(decision.Selections) != 0 || drain(t, policy, world) != 0 || !world.PlayerStatus().Suspended {
			t.Fatalf("loss: %+v", decision)
		}
		_, state := world.Snapshot(ctx).Data(ctx)
		if state.Tick != 0 || state.Health != 100 {
			t.Fatal("advanced before interruption")
		}
		begin(t, policy, FrameInput{Focus: FocusGained})
		if policy.State() != StatePause {
			t.Fatal("focus resumed play")
		}
		// Zero samples permit keyboard/mouse recovery after disconnection.
		begin(t, policy, FrameInput{Values: []ActionValue{{Action: settings.Throw}, {Action: settings.Melee}, {Action: settings.MenuConfirm}, {Action: settings.RemoveLimb}}})
		change(t, policy, ResumeGame, "")
		begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond})
		for i := 0; i < 3; i++ {
			_, command, ok := policy.NextTick(ctx)
			if !ok || len(command.Edges) != 0 {
				t.Fatal("interrupted attack leaked")
			}
			if err := world.StepCombat(ctx, simulation.CombatCommand{Player: command, Visible: simulation.Rect{X: -500, Y: -100, Width: 2000, Height: 1000}}); err != nil {
				t.Fatal(err)
			}
		}
		if world.CombatView(ctx).Attack.ID != 0 {
			t.Fatal("attack started after focus loss")
		}
	}
}

func TestResumeRequiresReleasedControlsAndNewPress(t *testing.T) {
	ctx := context.Background()
	for _, action := range []settings.Action{settings.Melee, settings.Throw, settings.RemoveLimb, settings.MenuConfirm} {
		policy := activePolicy(t, nil)
		begin(t, policy, FrameInput{Edges: []ActionEdge{{Action: action, Kind: Pressed}, {Action: settings.Pause, Kind: Pressed}}})
		if policy.State() != StatePause {
			t.Fatal("pause")
		}
		if err := policy.Transition(ctx, ResumeGame, ""); !errors.Is(err, InvalidAction) {
			t.Fatalf("resumed while %s held", action)
		}
		begin(t, policy, FrameInput{Edges: []ActionEdge{{Action: action, Kind: Released}, {Action: settings.Pause, Kind: Released}}})
		change(t, policy, ResumeGame, "")
		begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond})
		_, tick, ok := policy.NextTick(ctx)
		if !ok || len(tick.Edges) != 0 {
			t.Fatal("menu leaked held action")
		}
		drain(t, policy, nil)
		begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: action, Kind: Pressed}}})
		_, tick, ok = policy.NextTick(ctx)
		if !ok || (action != settings.MenuConfirm && (len(tick.Edges) != 1 || !tick.Edges[0].Pressed)) {
			t.Fatalf("fresh %s press lost", action)
		}
		drain(t, policy, nil)
	}
}

func TestLedgerFocusLossAndStalls(t *testing.T) {
	world := frameWorld(t)
	policy := activePolicy(t, world)
	begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Ledger, Kind: Pressed}, {Action: settings.Throw, Kind: Pressed}}})
	if policy.State() != StateLedger || drain(t, policy, world) != 0 {
		t.Fatal("ledger did not stop gameplay")
	}
	begin(t, policy, FrameInput{Focus: FocusLost})
	begin(t, policy, FrameInput{Focus: FocusGained, Edges: []ActionEdge{{Action: settings.Ledger, Kind: Released}, {Action: settings.Throw, Kind: Released}}})
	change(t, policy, CloseLedger, "")
	if policy.State() != StatePause {
		t.Fatal("focus loss under ledger resumed gameplay")
	}
	change(t, policy, ResumeGame, "")
	decision := begin(t, policy, FrameInput{Elapsed: 251 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Melee, Kind: Pressed}}})
	if !decision.Stalled || decision.State != StatePause || drain(t, policy, world) != 0 || !world.PlayerStatus().Suspended {
		t.Fatalf("stall: %+v", decision)
	}
}

func TestSceneClockFreezesOnPauseFocusAndSave(t *testing.T) {
	policy := activePolicy(t, nil)
	change(t, policy, StartScene, "")
	begin(t, policy, FrameInput{Elapsed: 4 * time.Millisecond})
	if drain(t, policy, nil) != 0 {
		t.Fatal("early scene tick")
	}
	change(t, policy, OpenPause, "")
	begin(t, policy, FrameInput{Elapsed: time.Second})
	if drain(t, policy, nil) != 0 {
		t.Fatal("paused scene advanced")
	}
	change(t, policy, ResumeGame, "")
	begin(t, policy, FrameInput{Focus: FocusLost, Elapsed: time.Second})
	begin(t, policy, FrameInput{Focus: FocusGained})
	change(t, policy, ResumeGame, "")
	change(t, policy, BeginSave, "")
	begin(t, policy, FrameInput{Elapsed: time.Second})
	if drain(t, policy, nil) != 0 {
		t.Fatal("saving scene advanced")
	}
	change(t, policy, SaveSuccess, StateScene)
	begin(t, policy, FrameInput{Elapsed: 5 * time.Millisecond})
	if drain(t, policy, nil) != 1 {
		t.Fatal("scene remainder was not frozen")
	}
}

func TestTickBudgetCanStopForDurableCandidate(t *testing.T) {
	ctx := context.Background()
	world := frameWorld(t)
	policy := activePolicy(t, world)
	begin(t, policy, FrameInput{Elapsed: 50 * time.Millisecond})
	_, command, ok := policy.NextTick(ctx)
	if !ok {
		t.Fatal("missing first tick")
	}
	if err := world.StepPlayer(ctx, command); err != nil {
		t.Fatal(err)
	}
	change(t, policy, BeginSave, "")
	if drain(t, policy, world) != 0 {
		t.Fatal("ticks after durable candidate")
	}
	_, state := world.Snapshot(ctx).Data(ctx)
	if state.Tick != 1 || !world.PlayerStatus().Suspended {
		t.Fatal("world advanced during save")
	}
	change(t, policy, SaveSuccess, StateActive)
	begin(t, policy, FrameInput{})
	if drain(t, policy, world) != 0 {
		t.Fatal("old frame budget leaked after save")
	}
}

func TestSimulationCombatBufferUsesTicksAndMenuClearsIt(t *testing.T) {
	ctx := context.Background()
	world := frameWorld(t)
	policy := activePolicy(t, world)
	if err := world.StartPlayerAction(ctx, simulation.HurtAction, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Melee, Kind: Pressed}}})
	for i := 0; i < 3; i++ {
		_, command, ok := policy.NextTick(ctx)
		if !ok {
			t.Fatal("tick")
		}
		if err := world.StepCombat(ctx, simulation.CombatCommand{Player: command, Visible: simulation.Rect{X: -500, Y: -100, Width: 2000, Height: 1000}}); err != nil {
			t.Fatal(err)
		}
	}
	if world.PlayerStatus().Buffered != settings.Melee {
		t.Fatal("real simulation did not retain combat buffer")
	}
	change(t, policy, OpenLedger, "")
	if world.PlayerStatus().Buffered != "" {
		t.Fatal("menu did not clear simulation buffer")
	}
	change(t, policy, CloseLedger, "")
	for i := 0; i < 6; i++ {
		begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond})
		for {
			_, command, ok := policy.NextTick(ctx)
			if !ok {
				break
			}
			if err := world.StepCombat(ctx, simulation.CombatCommand{Player: command, Visible: simulation.Rect{X: -500, Y: -100, Width: 2000, Height: 1000}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if world.CombatView(ctx).Attack.ID != 0 {
		t.Fatal("buffered attack survived ledger")
	}
}

// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
	"too-broke-to-die/internal/tbtd/simulation"
)

func frameWorld(t *testing.T) *simulation.World {
	t.Helper()
	ctx := context.Background()
	fsys := os.DirFS("../../..")
	manifest, err := content.LoadCampaign(ctx, fsys, "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := campaign.New(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	bays, err := content.LoadServiceBays(ctx, fsys, "content/data/encounters/templates.json", "content/data/encounters/variants.json", manifest)
	if err != nil {
		t.Fatal(err)
	}
	world, err := simulation.NewWorld(ctx, owner, manifest, campaign.Snapshot{}, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	return world
}

func activePolicy(t *testing.T, world *simulation.World) *FramePolicy {
	t.Helper()
	ctx := context.Background()
	policy := NewFramePolicy()
	if err := policy.BindWorld(ctx, world); err != nil {
		t.Fatal(err)
	}
	for _, event := range []StateEvent{ContinueGame, RestoreActive, ResumeGame} {
		if err := policy.Transition(ctx, event, ""); err != nil {
			t.Fatal(err)
		}
	}
	return policy
}

func begin(t *testing.T, policy *FramePolicy, input FrameInput) FrameDecision {
	t.Helper()
	ctx := context.Background()
	command, err := NewFrameCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := policy.BeginFrame(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func drain(t *testing.T, policy *FramePolicy, world *simulation.World) int {
	t.Helper()
	ctx := context.Background()
	count := 0
	for {
		kind, input, ok := policy.NextTick(ctx)
		if !ok {
			break
		}
		count++
		if kind == GameplayTick && world != nil {
			if err := world.StepPlayer(ctx, input); err != nil {
				t.Fatal(err)
			}
		}
	}
	return count
}

func TestFrameSchedulesUseExactClockAndRealSimulation(t *testing.T) {
	ctx := context.Background()
	var reference simulation.State
	for _, hz := range []int{30, 60, 144} {
		world := frameWorld(t)
		policy := activePolicy(t, world)
		ticks := 0
		var previous time.Duration
		for frame := 1; frame <= hz*2; frame++ {
			now := time.Duration(int64(frame) * int64(time.Second) / int64(hz))
			input := FrameInput{Elapsed: now - previous}
			if frame == 1 {
				input.Values = []ActionValue{{Action: settings.MoveRight, X: 1}}
			}
			begin(t, policy, input)
			ticks += drain(t, policy, world)
			previous = now
		}
		_, state := world.Snapshot(ctx).Data(ctx)
		if ticks != 240 || state.Tick != 240 {
			t.Fatalf("%d Hz: ticks %d, world %d", hz, ticks, state.Tick)
		}
		if hz == 30 {
			reference = state
		} else if !reflect.DeepEqual(reference, state) {
			t.Fatalf("%d Hz changed world: %+v versus %+v", hz, state.Player, reference.Player)
		}
	}
}

func TestDurationQuantization(t *testing.T) {
	for _, tc := range []struct {
		duration time.Duration
		ticks    int64
	}{
		{0, 0}, {time.Nanosecond, 1}, {25 * time.Millisecond, 3}, {25*time.Millisecond + 1, 4},
		{80 * time.Millisecond, 10}, {120 * time.Millisecond, 15}, {350 * time.Millisecond, 42}, {time.Second, 120},
	} {
		got, err := DurationTicks(tc.duration)
		if err != nil || got != tc.ticks {
			t.Fatalf("%v: %d, %v", tc.duration, got, err)
		}
	}
	if _, err := DurationTicks(-1); err == nil {
		t.Fatal("negative duration accepted")
	}
}

func TestClockBoundariesOverloadAndInterrupt(t *testing.T) {
	ctx := context.Background()
	var clock FixedClock
	stalled, err := clock.Begin(ctx, 250*time.Millisecond, StateActive)
	if err != nil || stalled {
		t.Fatalf("250 ms: %v %v", stalled, err)
	}
	for i := 0; i < 8; i++ {
		if !clock.Next(StateActive) {
			t.Fatal("short budget")
		}
	}
	if clock.Next(StateActive) {
		t.Fatal("more than eight ticks")
	}
	diagnostic := clock.Diagnostic()
	if diagnostic.Name != BacklogDropped || diagnostic.State != StateActive || diagnostic.ScaledNanoseconds != 22*int64(time.Second) || diagnostic.Dropped != time.Duration(22*int64(time.Second)/120) {
		t.Fatalf("overload diagnostic: %+v", diagnostic)
	}
	stalled, err = clock.Begin(ctx, 251*time.Millisecond, StateActive)
	if err != nil || !stalled || clock.Next(StateActive) || clock.Fraction() != 0 {
		t.Fatal("stall did not clear and pause")
	}
	if _, err = clock.Begin(ctx, -1, StateActive); !errors.Is(err, InvalidAction) {
		t.Fatalf("negative: %v", err)
	}
	if _, err = clock.Begin(ctx, 50*time.Millisecond, StateActive); err != nil {
		t.Fatal(err)
	}
	if !clock.Next(StateActive) {
		t.Fatal("missing first tick")
	}
	if _, err = clock.Begin(ctx, 0, StateActive); !errors.Is(err, InvalidAction) {
		t.Fatal("unconsumed budget accepted")
	}
	clock.Interrupt()
	if clock.Next(StateActive) || clock.Fraction() != 0 {
		t.Fatal("interrupt did not stop budget")
	}
	if _, err = clock.Begin(ctx, 0, StateActive); err != nil || clock.Next(StateActive) {
		t.Fatal("interrupted ticks leaked")
	}
}

func TestEdgesRetainedUntilFirstTickAndHeldValues(t *testing.T) {
	ctx := context.Background()
	policy := activePolicy(t, nil)
	begin(t, policy, FrameInput{Elapsed: time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight, X: .75}, {Action: settings.Aim, X: -1, Y: .5}}, Edges: []ActionEdge{{Action: settings.Jump, Kind: Pressed}, {Action: settings.Melee, Kind: Pressed}}})
	if drain(t, policy, nil) != 0 {
		t.Fatal("unexpected tick")
	}
	begin(t, policy, FrameInput{Elapsed: time.Millisecond, Edges: []ActionEdge{{Action: settings.Jump, Kind: Released}, {Action: settings.Melee, Kind: Pressed}}})
	if drain(t, policy, nil) != 0 {
		t.Fatal("unexpected tick")
	}
	begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond})
	kind, first, ok := policy.NextTick(ctx)
	if !ok || kind != GameplayTick || first.Move != .75 || first.Aim != (simulation.Vector{X: -1, Y: .5}) || len(first.Edges) != 3 || first.Edges[2].Pressed {
		t.Fatalf("first tick %+v", first)
	}
	_, second, ok := policy.NextTick(ctx)
	if !ok || second.Move != first.Move || second.Aim != first.Aim || len(second.Edges) != 0 {
		t.Fatalf("second tick %+v", second)
	}
	first.Edges[0].Action = settings.Throw
	if drain(t, policy, nil) != 1 {
		t.Fatal("remaining tick")
	}
}

func TestAimAppliedBeforeThrowAndHeldAttackDoesNotRepeat(t *testing.T) {
	ctx := context.Background()
	world := frameWorld(t)
	policy := activePolicy(t, world)
	begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond, Values: []ActionValue{{Action: settings.Aim, X: -1}}, Edges: []ActionEdge{{Action: settings.Throw, Kind: Pressed}}})
	for n := 0; n < 3; n++ {
		_, command, ok := policy.NextTick(ctx)
		if !ok {
			t.Fatal("missing tick")
		}
		if err := world.StepCombat(ctx, simulation.CombatCommand{Player: command, Visible: simulation.Rect{X: -500, Y: -100, Width: 2000, Height: 1000}}); err != nil {
			t.Fatal(err)
		}
	}
	attack := world.CombatView(ctx).Attack
	if !attack.Throw || attack.Facing != -1 || attack.ID != 1 {
		t.Fatalf("aim or throw lost: %+v", attack)
	}
	for frame := 0; frame < 20; frame++ {
		begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Throw, Kind: Pressed}}})
		for {
			_, command, ok := policy.NextTick(ctx)
			if !ok {
				break
			}
			if len(command.Edges) != 0 {
				t.Fatal("held throw repeated")
			}
			if err := world.StepCombat(ctx, simulation.CombatCommand{Player: command, Visible: simulation.Rect{X: -500, Y: -100, Width: 2000, Height: 1000}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if world.CombatView(ctx).Attack.ID != 0 {
		t.Fatal("held throw started another attack")
	}
}

func TestInterruptionDuringDurableWorkRequiresResume(t *testing.T) {
	ctx := context.Background()
	for _, loss := range []struct {
		name  string
		input FrameInput
	}{
		{name: "focus", input: FrameInput{Focus: FocusLost}},
		{name: "controller", input: FrameInput{Devices: []DeviceChange{{Device: settings.Controller, Active: true}}}},
	} {
		for _, destination := range []AppState{StateActive, StateScene} {
			for _, failFirst := range []bool{false, true} {
				t.Run(loss.name+"/"+string(destination)+"/"+map[bool]string{false: "save", true: "retry"}[failFirst], func(t *testing.T) {
					world := frameWorld(t)
					policy := activePolicy(t, world)
					if destination == StateScene {
						change(t, policy, StartScene, "")
						begin(t, policy, FrameInput{Elapsed: 4 * time.Millisecond})
						if drain(t, policy, world) != 0 {
							t.Fatal("scene advanced before its first tick")
						}
					}
					change(t, policy, BeginSave, "")
					if failFirst {
						change(t, policy, SaveFailure, "")
					}
					input := loss.input
					input.Elapsed = 50 * time.Millisecond
					input.Values = []ActionValue{{Action: settings.Throw, X: 1}}
					input.Edges = []ActionEdge{{Action: settings.Throw, Kind: Pressed}}
					decision := begin(t, policy, input)
					want := StateSaving
					if failFirst {
						want = StateError
					}
					if !decision.Interrupted || policy.State() != want || drain(t, policy, world) != 0 {
						t.Fatalf("interruption abandoned durable work: %+v", decision)
					}
					// Focus can return, but the interruption remains latched.
					begin(t, policy, FrameInput{Focus: FocusGained})
					if failFirst {
						change(t, policy, RetryOperation, "")
					}
					change(t, policy, SaveSuccess, destination)
					if policy.State() != StatePause || !world.PlayerStatus().Suspended {
						t.Fatal("acknowledgment resumed an interrupted destination")
					}
					if err := policy.Transition(ctx, ResumeGame, ""); !errors.Is(err, InvalidAction) {
						t.Fatal("held throw did not prevent Resume")
					}
					begin(t, policy, FrameInput{Values: []ActionValue{{Action: settings.Throw}}})
					change(t, policy, ResumeGame, "")
					if policy.State() != destination {
						t.Fatalf("Resume returned to %s, want %s", policy.State(), destination)
					}
					begin(t, policy, FrameInput{Elapsed: 5 * time.Millisecond})
					if destination == StateScene && drain(t, policy, world) != 1 {
						t.Fatal("scene clock did not preserve its frozen fractional tick")
					}
				})
			}
		}
	}
}

func TestInterruptedSaveSurvivesRestorationAndRecovery(t *testing.T) {
	ctx := context.Background()
	world := frameWorld(t)
	policy := NewFramePolicy()
	if err := policy.BindWorld(ctx, world); err != nil {
		t.Fatal(err)
	}
	change(t, policy, NewGame, "")
	begin(t, policy, FrameInput{Focus: FocusLost})
	change(t, policy, SaveSuccess, StateRestoring)
	if policy.State() != StateRestoring {
		t.Fatal("interruption bypassed restoration")
	}
	change(t, policy, SaveFailure, "")
	begin(t, policy, FrameInput{Focus: FocusGained})
	change(t, policy, RetryOperation, "")
	change(t, policy, RestoreScene, "")
	if policy.State() != StatePause {
		t.Fatal("interruption was lost during restoration retry")
	}
	change(t, policy, ResumeGame, "")
	if policy.State() != StateScene {
		t.Fatal("Resume did not return to the restored scene")
	}
}

func TestSaveWithoutInterruptionReturnsToSource(t *testing.T) {
	for _, destination := range []AppState{StateActive, StateScene} {
		policy := activePolicy(t, nil)
		if destination == StateScene {
			change(t, policy, StartScene, "")
		}
		change(t, policy, BeginSave, "")
		change(t, policy, SaveSuccess, destination)
		if policy.State() != destination {
			t.Fatalf("uninterrupted save returned to %s", policy.State())
		}
	}
}

func TestRestorationFailureRetriesItsOwnOperation(t *testing.T) {
	ctx := context.Background()
	for _, previousSave := range []bool{false, true} {
		policy := NewFramePolicy()
		if previousSave {
			change(t, policy, NewGame, "")
			change(t, policy, SaveFailure, "")
			change(t, policy, RetryOperation, "")
			if policy.State() != StateSaving {
				t.Fatal("save retry did not return to saving")
			}
			change(t, policy, SaveSuccess, StateRestoring)
		} else {
			change(t, policy, ContinueGame, "")
		}
		for i := 0; i < 2; i++ {
			change(t, policy, SaveFailure, "")
			change(t, policy, RetryOperation, "")
			if policy.State() != StateRestoring {
				t.Fatalf("restoration retry went to %s", policy.State())
			}
		}
		change(t, policy, RestoreActive, "")
		if policy.State() != StatePause {
			t.Fatal("restoration did not pause")
		}
		if err := policy.Transition(ctx, RetryOperation, ""); !errors.Is(err, InvalidAction) {
			t.Fatal("retry accepted outside error state")
		}
	}
}

func TestFrameJumpBufferTickBoundary(t *testing.T) {
	ctx := context.Background()
	for _, blockedTicks := range []int{14, 15} {
		world := frameWorld(t)
		policy := activePolicy(t, world)
		duration := time.Duration(blockedTicks) * time.Second / 120
		if err := world.StartPlayerAction(ctx, simulation.HurtAction, duration); err != nil {
			t.Fatal(err)
		}
		begin(t, policy, FrameInput{Elapsed: time.Millisecond, Edges: []ActionEdge{{Action: settings.Jump, Kind: Pressed}}})
		if drain(t, policy, world) != 0 {
			t.Fatal("jump edge consumed in a zero-tick frame")
		}
		begin(t, policy, FrameInput{Elapsed: 7 * time.Millisecond})
		if drain(t, policy, world) != 0 || world.PlayerStatus().Buffered != "" {
			t.Fatal("jump reached simulation before the first tick")
		}
		launchTick := -1
		for frame := 0; frame < 6; frame++ {
			begin(t, policy, FrameInput{Elapsed: 25 * time.Millisecond})
			for {
				_, command, ok := policy.NextTick(ctx)
				if !ok {
					break
				}
				_, before := world.Snapshot(ctx).Data(ctx)
				if before.Tick == 0 && (len(command.Edges) != 1 || command.Edges[0].Action != settings.Jump || !command.Edges[0].Pressed) {
					t.Fatalf("retained jump missing: %+v", command.Edges)
				}
				if before.Tick > 0 && len(command.Edges) != 0 {
					t.Fatal("held jump repeated on a later tick")
				}
				if err := world.StepPlayer(ctx, command); err != nil {
					t.Fatal(err)
				}
				_, after := world.Snapshot(ctx).Data(ctx)
				if before.Tick == 0 && world.PlayerStatus().Buffered != settings.Jump {
					t.Fatal("jump was not buffered while hurt")
				}
				if before.Player.Velocity.Y <= 0 && after.Player.Velocity.Y > 700 {
					launchTick = int(before.Tick)
				}
			}
		}
		if blockedTicks == 14 && launchTick != 14 {
			t.Fatalf("valid buffered jump launched at tick %d", launchTick)
		}
		if blockedTicks == 15 && (launchTick != -1 || world.PlayerStatus().Buffered != "") {
			t.Fatalf("expired jump launched at tick %d", launchTick)
		}
	}
}

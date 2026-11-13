// Copyright (C) 2026 crawter
// All rights reserved.

package input

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
	"too-broke-to-die/internal/tbtd/simulation"
)

func newSampler(t *testing.T) *Sampler {
	t.Helper()
	sampler, err := New(context.Background(), settings.Defaults(context.Background()), func() uint64 { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	return sampler
}

func frame() Frame {
	return Frame{Facing: 1, View: Viewport{X: 100, Y: 50, Width: 1280, Height: 720, LogicalWidth: 1280, LogicalHeight: 720, Offset: Point{X: 640, Y: 360}, Zoom: 1}}
}

func sample(t *testing.T, sampler *Sampler, frame Frame, snapshot Snapshot) session.FrameCommand {
	t.Helper()
	command, err := sampler.SampleSnapshot(context.Background(), frame, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func value(input session.FrameInput, action settings.Action) session.ActionValue {
	for _, item := range input.Values {
		if item.Action == action {
			return item
		}
	}
	return session.ActionValue{}
}

func edge(input session.FrameInput, action settings.Action, kind session.EdgeKind) bool {
	return slices.Contains(input.Edges, session.ActionEdge{Action: action, Kind: kind})
}

func neutral() Snapshot {
	return Snapshot{Focused: true, Connected: true, Buttons: make(map[Control]bool), Pointer: Point{X: 740, Y: 410}}
}

func activate(snapshot *Snapshot, control Control) {
	switch control.Name {
	case "LeftStickUp":
		snapshot.LeftStick.Y = -1
	case "LeftStickDown":
		snapshot.LeftStick.Y = 1
	case "LeftStickLeft":
		snapshot.LeftStick.X = -1
	case "LeftStickRight":
		snapshot.LeftStick.X = 1
	case "RightStickUp":
		snapshot.RightStick.Y = -1
	case "RightStickDown":
		snapshot.RightStick.Y = 1
	case "RightStickLeft":
		snapshot.RightStick.X = -1
	case "RightStickRight":
		snapshot.RightStick.X = 1
	case "Wheel":
		snapshot.Wheel = 1
	default:
		snapshot.Buttons[control] = true
	}
}

func TestDefaultBindingsAndEdges(t *testing.T) {
	ctx := context.Background()
	defaults := settings.Defaults(ctx)
	// Independent inventory: every PDD action and its keyboard/mouse and Xbox
	// controls, including menu alternatives and ledger tabs.
	want := []settings.Binding{
		{Context: settings.Gameplay, Action: settings.MoveLeft, Device: settings.Keyboard, Control: "A"},
		{Context: settings.Gameplay, Action: settings.MoveLeft, Device: settings.Keyboard, Control: "Left"},
		{Context: settings.Gameplay, Action: settings.MoveLeft, Device: settings.Controller, Control: "LeftStickLeft"},
		{Context: settings.Gameplay, Action: settings.MoveRight, Device: settings.Keyboard, Control: "D"},
		{Context: settings.Gameplay, Action: settings.MoveRight, Device: settings.Keyboard, Control: "Right"},
		{Context: settings.Gameplay, Action: settings.MoveRight, Device: settings.Controller, Control: "LeftStickRight"},
		{Context: settings.Gameplay, Action: settings.Down, Device: settings.Keyboard, Control: "S"},
		{Context: settings.Gameplay, Action: settings.Down, Device: settings.Keyboard, Control: "Down"},
		{Context: settings.Gameplay, Action: settings.Down, Device: settings.Controller, Control: "LeftStickDown"},
		{Context: settings.Gameplay, Action: settings.Jump, Device: settings.Keyboard, Control: "Space"},
		{Context: settings.Gameplay, Action: settings.Jump, Device: settings.Controller, Control: "A"},
		{Context: settings.Gameplay, Action: settings.Aim, Device: settings.Mouse, Control: "Motion"},
		{Context: settings.Gameplay, Action: settings.Aim, Device: settings.Controller, Control: "RightStick"},
		{Context: settings.Gameplay, Action: settings.Melee, Device: settings.Mouse, Control: "Left"},
		{Context: settings.Gameplay, Action: settings.Melee, Device: settings.Controller, Control: "RB"},
		{Context: settings.Gameplay, Action: settings.Throw, Device: settings.Mouse, Control: "Right"},
		{Context: settings.Gameplay, Action: settings.Throw, Device: settings.Controller, Control: "RT"},
		{Context: settings.Gameplay, Action: settings.Evade, Device: settings.Keyboard, Control: "LeftShift"},
		{Context: settings.Gameplay, Action: settings.Evade, Device: settings.Controller, Control: "B"},
		{Context: settings.Gameplay, Action: settings.Interact, Device: settings.Keyboard, Control: "E"},
		{Context: settings.Gameplay, Action: settings.Interact, Device: settings.Controller, Control: "X"},
		{Context: settings.Gameplay, Action: settings.SelectPayload, Device: settings.Keyboard, Control: "Q"},
		{Context: settings.Gameplay, Action: settings.SelectPayload, Device: settings.Mouse, Control: "Wheel"},
		{Context: settings.Gameplay, Action: settings.SelectPayload, Device: settings.Controller, Control: "LB"},
		{Context: settings.Gameplay, Action: settings.SelectLeftArm, Device: settings.Keyboard, Control: "1"},
		{Context: settings.Gameplay, Action: settings.SelectLeftArm, Device: settings.Controller, Control: "DPadLeft"},
		{Context: settings.Gameplay, Action: settings.SelectRightArm, Device: settings.Keyboard, Control: "2"},
		{Context: settings.Gameplay, Action: settings.SelectRightArm, Device: settings.Controller, Control: "DPadRight"},
		{Context: settings.Gameplay, Action: settings.SelectLeftLeg, Device: settings.Keyboard, Control: "3"},
		{Context: settings.Gameplay, Action: settings.SelectLeftLeg, Device: settings.Controller, Control: "DPadDown"},
		{Context: settings.Gameplay, Action: settings.SelectRightLeg, Device: settings.Keyboard, Control: "4"},
		{Context: settings.Gameplay, Action: settings.SelectRightLeg, Device: settings.Controller, Control: "DPadUp"},
		{Context: settings.Gameplay, Action: settings.RemoveLimb, Device: settings.Keyboard, Control: "R"},
		{Context: settings.Gameplay, Action: settings.RemoveLimb, Device: settings.Controller, Control: "Y"},
		{Context: settings.Gameplay, Action: settings.Regenerate, Device: settings.Keyboard, Control: "F"},
		{Context: settings.Gameplay, Action: settings.Regenerate, Device: settings.Controller, Control: "LT"},
		{Context: settings.Gameplay, Action: settings.Ledger, Device: settings.Keyboard, Control: "Tab"},
		{Context: settings.Gameplay, Action: settings.Ledger, Device: settings.Controller, Control: "View"},
		{Context: settings.Gameplay, Action: settings.Pause, Device: settings.Keyboard, Control: "Escape"},
		{Context: settings.Gameplay, Action: settings.Pause, Device: settings.Controller, Control: "Menu"},
		{Context: settings.Menu, Action: settings.MenuUp, Device: settings.Keyboard, Control: "Up"},
		{Context: settings.Menu, Action: settings.MenuUp, Device: settings.Keyboard, Control: "W"},
		{Context: settings.Menu, Action: settings.MenuUp, Device: settings.Controller, Control: "DPadUp"},
		{Context: settings.Menu, Action: settings.MenuUp, Device: settings.Controller, Control: "LeftStickUp"},
		{Context: settings.Menu, Action: settings.MenuDown, Device: settings.Keyboard, Control: "Down"},
		{Context: settings.Menu, Action: settings.MenuDown, Device: settings.Keyboard, Control: "S"},
		{Context: settings.Menu, Action: settings.MenuDown, Device: settings.Controller, Control: "DPadDown"},
		{Context: settings.Menu, Action: settings.MenuDown, Device: settings.Controller, Control: "LeftStickDown"},
		{Context: settings.Menu, Action: settings.MenuLeft, Device: settings.Keyboard, Control: "Left"},
		{Context: settings.Menu, Action: settings.MenuLeft, Device: settings.Controller, Control: "DPadLeft"},
		{Context: settings.Menu, Action: settings.MenuLeft, Device: settings.Controller, Control: "LeftStickLeft"},
		{Context: settings.Menu, Action: settings.MenuRight, Device: settings.Keyboard, Control: "Right"},
		{Context: settings.Menu, Action: settings.MenuRight, Device: settings.Controller, Control: "DPadRight"},
		{Context: settings.Menu, Action: settings.MenuRight, Device: settings.Controller, Control: "LeftStickRight"},
		{Context: settings.Menu, Action: settings.MenuConfirm, Device: settings.Keyboard, Control: "Enter"},
		{Context: settings.Menu, Action: settings.MenuConfirm, Device: settings.Keyboard, Control: "Space"},
		{Context: settings.Menu, Action: settings.MenuConfirm, Device: settings.Controller, Control: "A"},
		{Context: settings.Menu, Action: settings.MenuBack, Device: settings.Keyboard, Control: "Escape"},
		{Context: settings.Menu, Action: settings.MenuBack, Device: settings.Controller, Control: "B"},
		{Context: settings.Menu, Action: settings.MenuSelect, Device: settings.Mouse, Control: "Left"},
		{Context: settings.LedgerMenu, Action: settings.LedgerPrevious, Device: settings.Controller, Control: "LB"},
		{Context: settings.LedgerMenu, Action: settings.LedgerNext, Device: settings.Controller, Control: "RB"},
	}
	if !slices.Equal(want, defaults.Bindings) {
		t.Fatal("default inventory does not match SYS-INP")
	}
	for _, binding := range want {
		if binding.Action == settings.Aim {
			continue
		}
		t.Run(string(binding.Context)+"/"+string(binding.Action)+"/"+string(binding.Device)+"/"+binding.Control, func(t *testing.T) {
			sampler := newSampler(t)
			snapshot := neutral()
			sample(t, sampler, frame(), snapshot)
			activate(&snapshot, Control{Device: binding.Device, Name: binding.Control})
			got := sample(t, sampler, frame(), snapshot).Data(ctx)
			if !edge(got, binding.Action, session.Pressed) {
				t.Fatalf("missing press: %+v", got.Edges)
			}
			if binding.Control == "Wheel" {
				if !edge(got, binding.Action, session.Released) {
					t.Fatal("wheel must release in the same frame")
				}
				return
			}
			got = sample(t, sampler, frame(), snapshot).Data(ctx)
			if edge(got, binding.Action, session.Pressed) {
				t.Fatal("held control repeated")
			}
			got = sample(t, sampler, frame(), neutral()).Data(ctx)
			if !edge(got, binding.Action, session.Released) {
				t.Fatal("missing release")
			}
		})
	}
}

func TestBindingsRemapValidationAndTokens(t *testing.T) {
	ctx := context.Background()
	sampler := newSampler(t)
	values := settings.Defaults(ctx)
	for i := range values.Bindings {
		binding := &values.Bindings[i]
		if binding.Action == settings.Interact && binding.Device == settings.Keyboard {
			binding.Control = "Z"
		}
	}
	if err := sampler.SetBindings(ctx, values); err != nil {
		t.Fatal(err)
	}
	values.Bindings[0].Control = "Z" // caller mutation cannot alter the sampler
	tokens := sampler.Tokens(ctx, settings.Interact, settings.Keyboard)
	if len(tokens) != 1 || tokens[0].Control != "Z" {
		t.Fatalf("remapped prompt: %+v", tokens)
	}
	snapshot := neutral()
	sample(t, sampler, frame(), snapshot)
	snapshot.Buttons[Control{settings.Keyboard, "Z"}] = true
	if !edge(sample(t, sampler, frame(), snapshot).Data(ctx), settings.Interact, session.Pressed) {
		t.Fatal("remapped input did not fire")
	}
	bad := settings.Defaults(ctx)
	bad.Bindings = append(bad.Bindings, settings.Binding{Context: settings.Gameplay, Action: settings.Interact, Device: settings.Keyboard, Control: "R"})
	if err := sampler.SetBindings(ctx, bad); !errors.Is(err, settings.ErrInvalid) {
		t.Fatalf("duplicate accepted: %v", err)
	}
	bad = settings.Defaults(ctx)
	bad.Bindings = slices.DeleteFunc(bad.Bindings, func(binding settings.Binding) bool {
		return binding.Context == settings.Menu && binding.Action == settings.MenuBack && binding.Control == "Escape"
	})
	if err := sampler.SetBindings(ctx, bad); !errors.Is(err, settings.ErrInvalid) {
		t.Fatal("recovery binding removed")
	}
	if sampler.Tokens(ctx, settings.Interact, settings.Keyboard)[0].Control != "Z" {
		t.Fatal("failed update changed bindings")
	}
	if err := sampler.RestoreDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if sampler.Tokens(ctx, settings.Interact, settings.Keyboard)[0].Control != "E" {
		t.Fatal("defaults not restored")
	}
}

func TestDeadZonesAimAndPrompts(t *testing.T) {
	ctx := context.Background()
	sampler := newSampler(t)
	snapshot := neutral()
	initial := frame()
	initial.Facing = -1
	if got := value(sample(t, sampler, initial, snapshot).Data(ctx), settings.Aim); got.X != -1 || got.Y != 0 {
		t.Fatalf("initial aim: %+v", got)
	}
	snapshot.LeftStick.X = .20
	snapshot.RightStick.X = .25
	got := sample(t, sampler, frame(), snapshot).Data(ctx)
	if value(got, settings.MoveRight).X != 0 || sampler.ActiveDevice() != settings.Keyboard {
		t.Fatal("dead-zone boundary changed movement or prompts")
	}
	snapshot.LeftStick.X = .60
	snapshot.RightStick = Point{X: .6, Y: -.8}
	got = sample(t, sampler, frame(), snapshot).Data(ctx)
	if math.Abs(value(got, settings.MoveRight).X-.5) > 1e-9 || sampler.ActiveDevice() != settings.Controller {
		t.Fatalf("stick scaling or prompt: %+v", got.Values)
	}
	aim := value(got, settings.Aim)
	if math.Abs(aim.X-.6) > 1e-9 || math.Abs(aim.Y-.8) > 1e-9 {
		t.Fatalf("360-degree aim: %+v", aim)
	}
	snapshot.RightStick = Point{}
	got = sample(t, sampler, frame(), snapshot).Data(ctx)
	if value(got, settings.Aim) != aim {
		t.Fatal("neutral stick lost aim")
	}
	snapshot.Buttons[Control{settings.Keyboard, "E"}] = true
	sample(t, sampler, frame(), snapshot)
	if sampler.ActiveDevice() != settings.Keyboard {
		t.Fatal("keyboard did not take prompts")
	}
	sample(t, sampler, frame(), snapshot)
	if sampler.ActiveDevice() != settings.Keyboard {
		t.Fatal("idle held stick stole prompts")
	}
	snapshot.Pointer.X += 10
	sample(t, sampler, frame(), snapshot)
	if sampler.ActiveDevice() != settings.Mouse {
		t.Fatal("pointer motion did not take prompts")
	}
}

func TestPointerTransformAndRetention(t *testing.T) {
	view := frame().View
	view.Target = Point{X: 100, Y: 200}
	view.Zoom = 2
	world, ok := view.WorldPoint(context.Background(), Point{X: 940, Y: 310})
	if !ok || world != (Point{X: 200, Y: 250}) {
		t.Fatalf("camera conversion: %+v %v", world, ok)
	}
	for _, point := range []Point{{X: 99, Y: 100}, {X: 1380, Y: 100}, {X: 100, Y: 770}} {
		if _, ok := view.WorldPoint(context.Background(), point); ok {
			t.Fatalf("bar accepted: %+v", point)
		}
	}
	ctx := context.Background()
	sampler := newSampler(t)
	snapshot := neutral()
	f := frame()
	f.View = view
	f.ThrowOrigin = Point{X: 100, Y: 200}
	sample(t, sampler, f, snapshot)
	snapshot.Pointer = Point{X: 940, Y: 310}
	aim := value(sample(t, sampler, f, snapshot).Data(ctx), settings.Aim)
	if math.Abs(aim.X-2/math.Sqrt(5)) > 1e-9 || math.Abs(aim.Y-1/math.Sqrt(5)) > 1e-9 {
		t.Fatalf("pointer aim: %+v", aim)
	}
	snapshot.Pointer = Point{X: 50, Y: 50}
	if got := value(sample(t, sampler, f, snapshot).Data(ctx), settings.Aim); got != aim {
		t.Fatal("bar changed aim")
	}
	snapshot.Pointer = Point{X: 740, Y: 410}
	if got := value(sample(t, sampler, f, snapshot).Data(ctx), settings.Aim); got != aim {
		t.Fatal("centered pointer changed aim")
	}
}

func activePolicy(t *testing.T) *session.FramePolicy {
	t.Helper()
	policy := session.NewFramePolicy()
	for _, event := range []session.StateEvent{session.ContinueGame, session.RestoreActive, session.ResumeGame} {
		if err := policy.Transition(context.Background(), event, ""); err != nil {
			t.Fatal(err)
		}
	}
	return policy
}

func begin(t *testing.T, policy *session.FramePolicy, command session.FrameCommand) session.FrameDecision {
	t.Helper()
	decision, err := policy.BeginFrame(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func TestEdgesAcrossTickSchedules(t *testing.T) {
	ctx := context.Background()
	sampler, policy := newSampler(t), activePolicy(t)
	snapshot := neutral()
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	snapshot.Buttons[Control{settings.Mouse, "Left"}] = true
	snapshot.Buttons[Control{settings.Keyboard, "D"}] = true
	f := frame()
	f.Elapsed = time.Millisecond
	begin(t, policy, sample(t, sampler, f, snapshot))
	if _, _, ok := policy.NextTick(ctx); ok {
		t.Fatal("unexpected tick")
	}
	f.Elapsed = 25 * time.Millisecond
	begin(t, policy, sample(t, sampler, f, snapshot))
	for i := 0; i < 3; i++ {
		_, tick, ok := policy.NextTick(ctx)
		if !ok || tick.Move != 1 {
			t.Fatalf("tick %d: %+v", i, tick)
		}
		found := false
		for _, item := range tick.Edges {
			found = found || (item.Action == settings.Melee && item.Pressed)
		}
		if found != (i == 0) {
			t.Fatalf("edge repeated or lost on tick %d: %+v", i, tick.Edges)
		}
	}
	if _, _, ok := policy.NextTick(ctx); ok {
		t.Fatal("extra tick")
	}
}

func TestPauseFocusAndControllerLoss(t *testing.T) {
	ctx := context.Background()
	for _, reason := range []string{"pause", "focus", "disconnect"} {
		t.Run(reason, func(t *testing.T) {
			sampler, policy := newSampler(t), activePolicy(t)
			snapshot := neutral()
			begin(t, policy, sample(t, sampler, frame(), snapshot))
			if reason == "disconnect" {
				snapshot.Buttons[Control{settings.Controller, "RB"}] = true
			} else {
				snapshot.Buttons[Control{settings.Mouse, "Left"}] = true
			}
			f := frame()
			f.Elapsed = time.Millisecond
			begin(t, policy, sample(t, sampler, f, snapshot))
			if _, _, ok := policy.NextTick(ctx); ok {
				t.Fatal("unexpected tick")
			}
			switch reason {
			case "pause":
				snapshot.Buttons[Control{settings.Keyboard, "Escape"}] = true
			case "focus":
				snapshot.Focused = false
			case "disconnect":
				snapshot.Connected = false
			}
			f.Elapsed = 25 * time.Millisecond
			decision := begin(t, policy, sample(t, sampler, f, snapshot))
			if decision.State != session.StatePause || (reason != "pause" && !decision.Interrupted) {
				t.Fatalf("not paused before tick: %+v", decision)
			}
			if _, _, ok := policy.NextTick(ctx); ok {
				t.Fatal("interrupted tick advanced")
			}
			if reason != "disconnect" {
				if err := policy.Transition(ctx, session.ResumeGame, ""); !errors.Is(err, session.InvalidAction) {
					t.Fatal("resumed with held attack or lost focus")
				}
			}
			snapshot = neutral()
			if reason == "disconnect" {
				snapshot.Connected = false
			}
			begin(t, policy, sample(t, sampler, frame(), snapshot))
			if policy.State() != session.StatePause {
				t.Fatal("focus or controller recovery resumed automatically")
			}
			if err := policy.Transition(ctx, session.ResumeGame, ""); err != nil {
				t.Fatal(err)
			}
			begin(t, policy, sample(t, sampler, f, snapshot))
			_, tick, ok := policy.NextTick(ctx)
			if !ok || len(tick.Edges) != 0 {
				t.Fatalf("buffered attack leaked: %+v", tick.Edges)
			}
		})
	}
}

func TestControllerReconnectHeldControlsRequireRelease(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		control string
		action  settings.Action
		remap   bool
	}{
		{control: "RB", action: settings.Melee},
		{control: "RT", action: settings.Throw},
		{control: "Y", action: settings.RemoveLimb},
		{control: "A", action: settings.MenuConfirm},
		{control: "RightStickClick", action: settings.Melee, remap: true},
	}
	for _, tc := range cases {
		for _, heldBeforeLoss := range []bool{false, true} {
			name := tc.control + "/pressed-while-disconnected"
			if heldBeforeLoss {
				name = tc.control + "/held-before-disconnect"
			}
			t.Run(name, func(t *testing.T) {
				sampler, policy := newSampler(t), activePolicy(t)
				if tc.remap {
					values := settings.Defaults(ctx)
					for i := range values.Bindings {
						binding := &values.Bindings[i]
						if binding.Action == tc.action && binding.Device == settings.Controller {
							binding.Control = tc.control
						}
					}
					if err := sampler.SetBindings(ctx, values); err != nil {
						t.Fatal(err)
					}
				}
				snapshot := neutral()
				begin(t, policy, sample(t, sampler, frame(), snapshot))
				// Activate the controller without advancing a tick. A pending
				// gameplay edge must be cleared by the disconnection.
				snapshot.Buttons[Control{settings.Controller, "DPadUp"}] = true
				if heldBeforeLoss {
					snapshot.Buttons[Control{settings.Controller, tc.control}] = true
				}
				begin(t, policy, sample(t, sampler, frame(), snapshot))
				if sampler.ActiveDevice() != settings.Controller {
					t.Fatal("controller was not active")
				}
				snapshot.Connected = false
				decision := begin(t, policy, sample(t, sampler, frame(), snapshot))
				if !decision.Interrupted || decision.State != session.StatePause {
					t.Fatalf("disconnect did not pause: %+v", decision)
				}
				// Keyboard navigation and explicit Resume remain available.
				keyboard := neutral()
				keyboard.Connected = false
				keyboard.Buttons[Control{settings.Keyboard, "W"}] = true
				command := sample(t, sampler, frame(), keyboard)
				if !edge(command.Data(ctx), settings.MenuUp, session.Pressed) {
					t.Fatal("keyboard menu recovery unavailable")
				}
				begin(t, policy, command)
				delete(keyboard.Buttons, Control{settings.Keyboard, "W"})
				begin(t, policy, sample(t, sampler, frame(), keyboard))
				if policy.State() != session.StatePause {
					t.Fatal("controller loss resumed automatically")
				}
				if err := policy.Transition(ctx, session.ResumeGame, ""); err != nil {
					t.Fatal(err)
				}

				reconnected := neutral()
				reconnected.Buttons[Control{settings.Controller, tc.control}] = true
				f := frame()
				f.Elapsed = 25 * time.Millisecond
				for i := 0; i < 2; i++ {
					command = sample(t, sampler, f, reconnected)
					data := command.Data(ctx)
					if edge(data, tc.action, session.Pressed) || (tc.control == "A" && edge(data, settings.Jump, session.Pressed)) {
						t.Fatalf("held reconnect generated a press: %+v", data.Edges)
					}
					if sampler.ActiveDevice() != settings.Keyboard {
						t.Fatal("quarantined button stole device prompts")
					}
					begin(t, policy, command)
					count := 0
					for {
						_, tick, ok := policy.NextTick(ctx)
						if !ok {
							break
						}
						count++
						for _, item := range tick.Edges {
							if item.Pressed {
								t.Fatalf("held input reached tick: %+v", tick.Edges)
							}
						}
					}
					if count == 0 {
						t.Fatal("gameplay did not advance")
					}
				}
				// Only a focused, connected release re-arms this physical control.
				delete(reconnected.Buttons, Control{settings.Controller, tc.control})
				begin(t, policy, sample(t, sampler, frame(), reconnected))
				reconnected.Buttons[Control{settings.Controller, tc.control}] = true
				command = sample(t, sampler, f, reconnected)
				if !edge(command.Data(ctx), tc.action, session.Pressed) {
					t.Fatal("fresh controller press was not re-armed")
				}
				begin(t, policy, command)
				tickAction := tc.action
				if tc.action == settings.MenuConfirm {
					tickAction = settings.Jump
				}
				for i := 0; ; i++ {
					_, tick, ok := policy.NextTick(ctx)
					if !ok {
						if i == 0 {
							t.Fatal("fresh press had no tick")
						}
						break
					}
					pressed := false
					for _, item := range tick.Edges {
						pressed = pressed || (item.Action == tickAction && item.Pressed)
					}
					if pressed != (i == 0) {
						t.Fatalf("fresh press on tick %d: %+v", i, tick.Edges)
					}
				}
			})
		}
	}
}

func TestQuarantinedControllerDoesNotSwallowIndependentInput(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name        string
		controller  string
		independent Control
		action      settings.Action
		wheel       bool
	}{
		{name: "melee", controller: "RB", independent: Control{settings.Mouse, "Left"}, action: settings.Melee},
		{name: "throw", controller: "RT", independent: Control{settings.Mouse, "Right"}, action: settings.Throw},
		{name: "removal", controller: "Y", independent: Control{settings.Keyboard, "R"}, action: settings.RemoveLimb},
		{name: "confirm", controller: "A", independent: Control{settings.Keyboard, "Enter"}, action: settings.MenuConfirm},
		{name: "wheel-remap", controller: "RB", independent: Control{settings.Mouse, "Wheel"}, action: settings.Melee, wheel: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sampler, policy := newSampler(t), activePolicy(t)
			if tc.wheel {
				values := settings.Defaults(ctx)
				values.Bindings = slices.DeleteFunc(values.Bindings, func(binding settings.Binding) bool {
					return binding.Action == settings.SelectPayload && binding.Device == settings.Mouse && binding.Control == "Wheel"
				})
				for i := range values.Bindings {
					binding := &values.Bindings[i]
					if binding.Action == settings.Melee && binding.Device == settings.Mouse {
						binding.Control = "Wheel"
					}
				}
				if err := sampler.SetBindings(ctx, values); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := neutral()
			begin(t, policy, sample(t, sampler, frame(), snapshot))
			snapshot.Buttons[Control{settings.Controller, "DPadUp"}] = true
			begin(t, policy, sample(t, sampler, frame(), snapshot))
			snapshot.Connected = false
			if decision := begin(t, policy, sample(t, sampler, frame(), snapshot)); !decision.Interrupted || decision.State != session.StatePause {
				t.Fatalf("disconnect did not pause: %+v", decision)
			}
			if err := policy.Transition(ctx, session.ResumeGame, ""); err != nil {
				t.Fatal(err)
			}
			snapshot = neutral()
			snapshot.Buttons[Control{settings.Controller, tc.controller}] = true
			f := frame()
			f.Elapsed = 25 * time.Millisecond
			// Let FramePolicy observe the quarantined held control on two
			// earlier frames, without disconnecting it again.
			for i := 0; i < 2; i++ {
				command := sample(t, sampler, f, snapshot)
				data := command.Data(ctx)
				if !data.ResumeBlocked || edge(data, tc.action, session.Pressed) || value(data, tc.action).X != 0 {
					t.Fatalf("unsafe held controller input: %+v", data)
				}
				data.ResumeBlocked = false
				if !command.Data(ctx).ResumeBlocked {
					t.Fatal("frame data mutation changed the command")
				}
				begin(t, policy, command)
				for {
					if _, _, ok := policy.NextTick(ctx); !ok {
						break
					}
				}
			}
			for cycle := 0; cycle < 2; cycle++ {
				if tc.wheel {
					snapshot.Wheel = 1
				} else {
					snapshot.Buttons[tc.independent] = true
				}
				short := frame()
				short.Elapsed = time.Millisecond
				command := sample(t, sampler, short, snapshot)
				data := command.Data(ctx)
				if !data.ResumeBlocked || !edge(data, tc.action, session.Pressed) {
					t.Fatalf("independent press missing: %+v", data.Edges)
				}
				begin(t, policy, command)
				if _, _, ok := policy.NextTick(ctx); ok {
					t.Fatal("unexpected tick before independent edge")
				}
				if tc.wheel {
					snapshot.Wheel = 0
				}
				begin(t, policy, sample(t, sampler, f, snapshot))
				for i := 0; ; i++ {
					_, tick, ok := policy.NextTick(ctx)
					if !ok {
						if i == 0 {
							t.Fatal("no eligible tick")
						}
						break
					}
					if tc.action == settings.MenuConfirm {
						continue // menu actions do not enter gameplay ticks
					}
					pressed := slices.ContainsFunc(tick.Edges, func(item simulation.PlayerEdge) bool {
						return item.Action == tc.action && item.Pressed
					})
					if pressed != (i == 0) {
						t.Fatalf("independent press on tick %d: %+v", i, tick.Edges)
					}
				}
				if !tc.wheel {
					delete(snapshot.Buttons, tc.independent)
					begin(t, policy, sample(t, sampler, frame(), snapshot))
				}
			}
			// Even after an independent press and release, the physically held
			// quarantined controller still prevents Resume from a new pause.
			snapshot.Buttons[Control{settings.Keyboard, "Escape"}] = true
			if decision := begin(t, policy, sample(t, sampler, frame(), snapshot)); decision.State != session.StatePause {
				t.Fatalf("pause: %+v", decision)
			}
			delete(snapshot.Buttons, Control{settings.Keyboard, "Escape"})
			begin(t, policy, sample(t, sampler, frame(), snapshot))
			if err := policy.Transition(ctx, session.ResumeGame, ""); !errors.Is(err, session.InvalidAction) {
				t.Fatal("held quarantined control allowed Resume")
			}
			delete(snapshot.Buttons, Control{settings.Controller, tc.controller})
			command := sample(t, sampler, frame(), snapshot)
			if command.Data(ctx).ResumeBlocked {
				t.Fatal("observed release did not clear Resume blocker")
			}
			begin(t, policy, command)
			if err := policy.Transition(ctx, session.ResumeGame, ""); err != nil {
				t.Fatal(err)
			}
			snapshot.Buttons[Control{settings.Controller, tc.controller}] = true
			command = sample(t, sampler, f, snapshot)
			if !edge(command.Data(ctx), tc.action, session.Pressed) {
				t.Fatal("controller did not re-arm on a fresh press")
			}
			begin(t, policy, command)
			_, tick, ok := policy.NextTick(ctx)
			if !ok {
				t.Fatal("fresh controller press had no tick")
			}
			tickAction := tc.action
			if tc.action == settings.MenuConfirm {
				tickAction = settings.Jump
			}
			if !slices.ContainsFunc(tick.Edges, func(item simulation.PlayerEdge) bool {
				return item.Action == tickAction && item.Pressed
			}) {
				t.Fatalf("fresh controller edge lost: %+v", tick.Edges)
			}
		})
	}
}

func TestControllerReconnectBeforeResumeAndFocusRecovery(t *testing.T) {
	ctx := context.Background()
	sampler, policy := newSampler(t), activePolicy(t)
	snapshot := neutral()
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	snapshot.Buttons[Control{settings.Controller, "DPadUp"}] = true
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	snapshot.Connected = false
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	if policy.State() != session.StatePause {
		t.Fatal("controller loss did not pause")
	}
	snapshot.Focused = false
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	snapshot = neutral()
	snapshot.Focused = false
	snapshot.Buttons[Control{settings.Controller, "RB"}] = true
	command := sample(t, sampler, frame(), snapshot)
	if edge(command.Data(ctx), settings.Melee, session.Pressed) {
		t.Fatal("unfocused reconnect generated a press")
	}
	begin(t, policy, command)
	snapshot.Focused = true
	command = sample(t, sampler, frame(), snapshot)
	if edge(command.Data(ctx), settings.Melee, session.Pressed) {
		t.Fatal("focus restoration generated a press")
	}
	begin(t, policy, command)
	if err := policy.Transition(ctx, session.ResumeGame, ""); !errors.Is(err, session.InvalidAction) {
		t.Fatal("reconnected held attack did not prevent Resume")
	}
	delete(snapshot.Buttons, Control{settings.Controller, "RB"})
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	if err := policy.Transition(ctx, session.ResumeGame, ""); err != nil {
		t.Fatal(err)
	}
	snapshot.Buttons[Control{settings.Controller, "RB"}] = true
	f := frame()
	f.Elapsed = 25 * time.Millisecond
	command = sample(t, sampler, f, snapshot)
	if !edge(command.Data(ctx), settings.Melee, session.Pressed) {
		t.Fatal("press after focused release not restored")
	}
	begin(t, policy, command)
	_, tick, ok := policy.NextTick(ctx)
	if !ok || !slices.ContainsFunc(tick.Edges, func(item simulation.PlayerEdge) bool {
		return item.Action == settings.Melee && item.Pressed
	}) {
		t.Fatalf("fresh melee not delivered: %+v", tick.Edges)
	}
}

func TestControllerReconnectReleaseAndIndependentInput(t *testing.T) {
	ctx := context.Background()
	sampler, policy := newSampler(t), activePolicy(t)
	snapshot := neutral()
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	snapshot.Buttons[Control{settings.Controller, "DPadUp"}] = true
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	snapshot.Connected = false
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	if err := policy.Transition(ctx, session.ResumeGame, ""); err != nil {
		t.Fatal(err)
	}
	// Reconnecting with RB held suppresses that control, but other devices
	// remain available. A second disconnect must not count as its release.
	snapshot = neutral()
	snapshot.Buttons[Control{settings.Controller, "RB"}] = true
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	snapshot.Connected = false
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	if policy.State() != session.StateActive {
		t.Fatal("inactive controller loss unexpectedly paused keyboard play")
	}
	snapshot = neutral()
	snapshot.Buttons[Control{settings.Controller, "RB"}] = true
	snapshot.Buttons[Control{settings.Controller, "RT"}] = true
	snapshot.Buttons[Control{settings.Mouse, "Left"}] = true
	f := frame()
	f.Elapsed = 25 * time.Millisecond
	command := sample(t, sampler, f, snapshot)
	data := command.Data(ctx)
	if edge(data, settings.Throw, session.Pressed) || !edge(data, settings.Melee, session.Pressed) {
		t.Fatalf("unsafe controller or independent mouse input: %+v", data.Edges)
	}
	begin(t, policy, command)
	_, tick, ok := policy.NextTick(ctx)
	if !ok || !slices.ContainsFunc(tick.Edges, func(item simulation.PlayerEdge) bool {
		return item.Action == settings.Melee && item.Pressed
	}) {
		t.Fatalf("independent mouse press lost: %+v", tick.Edges)
	}
	for {
		if _, _, ok := policy.NextTick(ctx); !ok {
			break
		}
	}
	// An observed neutral reconnect needs no additional release on a later
	// fresh press, including after a second interruption.
	snapshot.Connected = false
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	snapshot = neutral()
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	snapshot.Buttons[Control{settings.Controller, "RT"}] = true
	command = sample(t, sampler, frame(), snapshot)
	if !edge(command.Data(ctx), settings.Throw, session.Pressed) {
		t.Fatal("neutral reconnect did not permit a subsequent new press")
	}
}

func TestEveryGameplayActionCanBeRemapped(t *testing.T) {
	ctx := context.Background()
	actions := []settings.Action{settings.MoveLeft, settings.MoveRight, settings.Down, settings.Jump, settings.Melee, settings.Throw, settings.Evade, settings.Interact, settings.SelectPayload, settings.SelectLeftArm, settings.SelectRightArm, settings.SelectLeftLeg, settings.SelectRightLeg, settings.RemoveLimb, settings.Regenerate, settings.Ledger, settings.Pause}
	for _, action := range actions {
		for _, family := range []settings.Device{settings.Keyboard, settings.Controller} {
			t.Run(string(action)+"/"+string(family), func(t *testing.T) {
				values := settings.Defaults(ctx)
				name := "Z"
				if family == settings.Controller {
					name = "RightStickClick"
				}
				found := false
				for i := range values.Bindings {
					binding := &values.Bindings[i]
					if binding.Context == settings.Gameplay && binding.Action == action && !found && ((family == settings.Controller) == (binding.Device == settings.Controller)) {
						binding.Device, binding.Control, found = family, name, true
					}
				}
				if !found {
					t.Fatal("default action not found")
				}
				sampler := newSampler(t)
				if err := sampler.SetBindings(ctx, values); err != nil {
					t.Fatal(err)
				}
				snapshot := neutral()
				sample(t, sampler, frame(), snapshot)
				snapshot.Buttons[Control{family, name}] = true
				if !edge(sample(t, sampler, frame(), snapshot).Data(ctx), action, session.Pressed) {
					t.Fatal("remapped action did not fire")
				}
				if !slices.ContainsFunc(sampler.Tokens(ctx, action, family), func(token Token) bool { return token.Control == name }) {
					t.Fatal("prompt did not change")
				}
			})
		}
	}
	// Moving aim to the other stick requires moving the three overlapping
	// directional bindings as a single validated settings draft.
	values := settings.Defaults(ctx)
	for i := range values.Bindings {
		binding := &values.Bindings[i]
		if binding.Context != settings.Gameplay || binding.Device != settings.Controller {
			continue
		}
		switch binding.Action {
		case settings.Aim:
			binding.Control = "LeftStick"
		case settings.MoveLeft:
			binding.Control = "LeftStickClick"
		case settings.MoveRight:
			binding.Control = "RightStickClick"
		case settings.Down:
			binding.Control = "RightStickDown"
		}
	}
	sampler := newSampler(t)
	if err := sampler.SetBindings(ctx, values); err != nil {
		t.Fatal(err)
	}
	snapshot := neutral()
	sample(t, sampler, frame(), snapshot)
	snapshot.LeftStick = Point{X: -1}
	aim := value(sample(t, sampler, frame(), snapshot).Data(ctx), settings.Aim)
	if aim.X != -1 || aim.Y != 0 {
		t.Fatalf("remapped aim: %+v", aim)
	}
}

func TestReleaseBeforeResumeForEveryDangerousControl(t *testing.T) {
	ctx := context.Background()
	for _, control := range []Control{{settings.Mouse, "Left"}, {settings.Mouse, "Right"}, {settings.Keyboard, "R"}, {settings.Keyboard, "Enter"}} {
		t.Run(string(control.Device)+"/"+control.Name, func(t *testing.T) {
			sampler, policy := newSampler(t), activePolicy(t)
			snapshot := neutral()
			begin(t, policy, sample(t, sampler, frame(), snapshot))
			snapshot.Buttons[control] = true
			snapshot.Buttons[Control{settings.Keyboard, "Escape"}] = true
			begin(t, policy, sample(t, sampler, frame(), snapshot))
			if err := policy.Transition(ctx, session.ResumeGame, ""); !errors.Is(err, session.InvalidAction) {
				t.Fatal("held input allowed resume")
			}
			begin(t, policy, sample(t, sampler, frame(), neutral()))
			if err := policy.Transition(ctx, session.ResumeGame, ""); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPointerConfirmMustReleaseAfterMeleeRemap(t *testing.T) {
	ctx := context.Background()
	sampler, policy := newSampler(t), activePolicy(t)
	values := settings.Defaults(ctx)
	for i := range values.Bindings {
		binding := &values.Bindings[i]
		if binding.Action == settings.Melee && binding.Device == settings.Mouse {
			binding.Device, binding.Control = settings.Keyboard, "Z"
		}
	}
	if err := sampler.SetBindings(ctx, values); err != nil {
		t.Fatal(err)
	}
	snapshot := neutral()
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	snapshot.Buttons[Control{settings.Keyboard, "Escape"}] = true
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	snapshot.Buttons[Control{settings.Mouse, "Left"}] = true
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	if err := policy.Transition(ctx, session.ResumeGame, ""); !errors.Is(err, session.InvalidAction) {
		t.Fatal("held pointer confirmation allowed Resume")
	}
	begin(t, policy, sample(t, sampler, frame(), neutral()))
	if err := policy.Transition(ctx, session.ResumeGame, ""); err != nil {
		t.Fatal(err)
	}
}

func TestNoTickPressAndReleaseAreOrdered(t *testing.T) {
	ctx := context.Background()
	sampler, policy := newSampler(t), activePolicy(t)
	snapshot := neutral()
	begin(t, policy, sample(t, sampler, frame(), snapshot))
	snapshot.Buttons[Control{settings.Keyboard, "Space"}] = true
	f := frame()
	f.Elapsed = time.Millisecond
	begin(t, policy, sample(t, sampler, f, snapshot))
	if _, _, ok := policy.NextTick(ctx); ok {
		t.Fatal("unexpected tick")
	}
	begin(t, policy, sample(t, sampler, f, neutral()))
	if _, _, ok := policy.NextTick(ctx); ok {
		t.Fatal("unexpected tick")
	}
	f.Elapsed = 10 * time.Millisecond
	begin(t, policy, sample(t, sampler, f, neutral()))
	_, tick, ok := policy.NextTick(ctx)
	if !ok || len(tick.Edges) != 2 || tick.Edges[0].Action != settings.Jump || !tick.Edges[0].Pressed || tick.Edges[1].Pressed {
		t.Fatalf("press and release order: %+v", tick.Edges)
	}
}

func TestInactiveControllerDisconnectAndPointerSelection(t *testing.T) {
	ctx := context.Background()
	sampler := newSampler(t)
	snapshot := neutral()
	sample(t, sampler, frame(), snapshot)
	snapshot.Connected = false
	snapshot.Buttons[Control{settings.Keyboard, "E"}] = true
	command := sample(t, sampler, frame(), snapshot).Data(ctx)
	if !edge(command, settings.Interact, session.Pressed) {
		t.Fatal("inactive controller disconnect swallowed keyboard input")
	}
	if pointer, ok := sampler.Pointer(); !ok || pointer != (Point{X: 640, Y: 360}) {
		t.Fatalf("logical pointer: %+v %v", pointer, ok)
	}
	snapshot.Pointer.X = 0
	sample(t, sampler, frame(), snapshot)
	if _, ok := sampler.Pointer(); ok {
		t.Fatal("pointer in a bar available for menu hit testing")
	}
}

func TestInvalidSamplesAndThreadGuard(t *testing.T) {
	ctx := context.Background()
	if _, err := New(ctx, settings.Defaults(ctx), nil); !errors.Is(err, ErrThread) {
		t.Fatal("missing thread accepted")
	}
	thread := uint64(1)
	sampler, err := New(ctx, settings.Defaults(ctx), func() uint64 { return thread })
	if err != nil {
		t.Fatal(err)
	}
	thread = 2
	if _, err = sampler.Sample(ctx, frame()); !errors.Is(err, ErrThread) {
		t.Fatal("wrong thread entered native sampling")
	}
	snapshot := neutral()
	snapshot.LeftStick.X = math.NaN()
	if _, err = sampler.SampleSnapshot(ctx, frame(), snapshot); !errors.Is(err, session.InvalidAction) {
		t.Fatal("non-finite sample accepted")
	}
	f := frame()
	f.Elapsed = -time.Second
	if _, err = sampler.SampleSnapshot(ctx, f, neutral()); !errors.Is(err, session.InvalidAction) {
		t.Fatal("negative elapsed accepted")
	}
}

func TestNativeControlCodes(t *testing.T) {
	want := map[string]int32{
		"A": rl.GamepadButtonRightFaceDown, "B": rl.GamepadButtonRightFaceRight,
		"X": rl.GamepadButtonRightFaceLeft, "Y": rl.GamepadButtonRightFaceUp,
		"LB": rl.GamepadButtonLeftTrigger1, "RB": rl.GamepadButtonRightTrigger1,
		"View": rl.GamepadButtonMiddleLeft, "Menu": rl.GamepadButtonMiddleRight,
		"DPadLeft": rl.GamepadButtonLeftFaceLeft, "DPadRight": rl.GamepadButtonLeftFaceRight,
		"DPadDown": rl.GamepadButtonLeftFaceDown, "DPadUp": rl.GamepadButtonLeftFaceUp,
		"LeftStickClick": rl.GamepadButtonLeftThumb, "RightStickClick": rl.GamepadButtonRightThumb,
	}
	if len(want) != len(padCodes) {
		t.Fatal("native gamepad inventory differs")
	}
	for name, code := range want {
		if got, ok := padCodes[name]; !ok || got != code {
			t.Fatalf("%s maps to %d, want %d", name, got, code)
		}
	}
	if keyCodes["Escape"] != rl.KeyEscape || keyCodes["LeftShift"] != rl.KeyLeftShift || keyCodes["Space"] != rl.KeySpace || keyCodes["Enter"] != rl.KeyEnter || keyCodes["Tab"] != rl.KeyTab {
		t.Fatal("native keyboard special-key mapping")
	}
}

func TestSharedControlsAndPromptPriority(t *testing.T) {
	ctx := context.Background()
	sampler := newSampler(t)
	snapshot := neutral()
	sample(t, sampler, frame(), snapshot)
	snapshot.Buttons[Control{settings.Keyboard, "Space"}] = true
	snapshot.Buttons[Control{settings.Controller, "A"}] = true
	snapshot.Buttons[Control{settings.Mouse, "Right"}] = true
	got := sample(t, sampler, frame(), snapshot).Data(ctx)
	if sampler.ActiveDevice() != settings.Controller || !edge(got, settings.Jump, session.Pressed) {
		t.Fatal("simultaneous device choice was not deterministic")
	}
	delete(snapshot.Buttons, Control{settings.Keyboard, "Space"})
	got = sample(t, sampler, frame(), snapshot).Data(ctx)
	if edge(got, settings.Jump, session.Released) || value(got, settings.Jump).X != 1 {
		t.Fatal("one of two bindings released the shared action")
	}
	delete(snapshot.Buttons, Control{settings.Controller, "A"})
	got = sample(t, sampler, frame(), snapshot).Data(ctx)
	if !edge(got, settings.Jump, session.Released) {
		t.Fatal("last binding did not release the action")
	}
}

func TestStickThresholdsAndWheelPulses(t *testing.T) {
	ctx := context.Background()
	sampler := newSampler(t)
	snapshot := neutral()
	sample(t, sampler, frame(), snapshot)
	snapshot.RightStick = Point{X: 0, Y: -.251}
	got := sample(t, sampler, frame(), snapshot).Data(ctx)
	if aim := value(got, settings.Aim); math.Abs(aim.X) > 1e-9 || math.Abs(aim.Y-1) > 1e-9 {
		t.Fatalf("right-stick just outside dead zone: %+v", aim)
	}
	if sampler.ActiveDevice() != settings.Keyboard {
		t.Fatal("minor stick noise switched prompt device")
	}
	snapshot.RightStick = Point{}
	snapshot.Wheel = 1
	for i := 0; i < 2; i++ {
		got = sample(t, sampler, frame(), snapshot).Data(ctx)
		if !edge(got, settings.SelectPayload, session.Pressed) || !edge(got, settings.SelectPayload, session.Released) {
			t.Fatal("consecutive wheel pulses were lost")
		}
	}
}

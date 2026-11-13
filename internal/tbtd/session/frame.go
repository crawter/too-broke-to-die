// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"too-broke-to-die/internal/tbtd/settings"
	"too-broke-to-die/internal/tbtd/simulation"
)

// ActionValue is a normalized scalar or world-space aim vector. Components
// are finite and in [-1, 1]; zero aim retains the previous direction. The
// sampler combines device contributions into at most one value per action.
type ActionValue struct {
	Action settings.Action

	X float64

	Y float64
}

// EdgeKind identifies an ordered transition, independent of held values.
type EdgeKind string

const (
	// Pressed records a transition from released to pressed.
	Pressed EdgeKind = "pressed"

	// Released records a transition from pressed to released.
	Released EdgeKind = "released"
)

// ActionEdge is retained until an eligible simulation tick consumes it.
type ActionEdge struct {
	Action settings.Action

	Kind EdgeKind
}

// FocusChange describes a change rather than implying an auto-resume.
type FocusChange string

const (
	// FocusUnchanged reports no change in window focus this frame.
	FocusUnchanged FocusChange = ""

	// FocusGained reports focus returning without implicitly resuming play.
	FocusGained FocusChange = "gained"

	// FocusLost reports focus leaving the window and takes priority over play.
	FocusLost FocusChange = "lost"
)

// DeviceChange records attachment and the latest prompt device separately.
type DeviceChange struct {
	Device settings.Device

	Connected bool

	Active bool
}

// MenuAction identifies a semantic menu selection without a native control.
type MenuAction string

// MenuSelection names an adapter-independent menu action and optional target.
type MenuSelection struct {
	Action MenuAction

	Target string
}

// ReticleInput carries the mouse position on the unscaled logical canvas.
// A controller supplies aim direction instead; no pointer is invented for it.
type ReticleInput struct {
	Pointer bool
	Visible bool
	X, Y    float64
}

// FrameInput is the construction data for one frame. Elapsed is an interval
// from a monotonic clock, not an absolute timestamp. Edges remain ordered.
// Values update the named controls; omitted values retain their last sample.
// The sampler must report zero for released controls, including after focus
// returns, so the session can safely enforce release-before-Resume. ResumeBlocked
// is sampled every frame and reports quarantined physical controls separately
// from actionable values and edges.
type FrameInput struct {
	Elapsed time.Duration

	Values []ActionValue

	Edges []ActionEdge

	// ResumeBlocked reports a held quarantined control that cannot trigger
	// until release. It must not participate in action-edge deduplication.
	ResumeBlocked bool

	Reticle ReticleInput

	Focus FocusChange

	Devices []DeviceChange

	CloseRequested bool

	Selections []MenuSelection
}

// FrameCommand owns its inputs. Data returns independent copies, so sampling
// and frame handling can safely have different owners.
type FrameCommand struct{ input FrameInput }

// NewFrameCommand validates the normalized shape and copies all collections.
func NewFrameCommand(ctx context.Context, input FrameInput) (_ FrameCommand, err error) {
	defer mon.Task()(&ctx)(&err)
	if input.Elapsed < 0 || (input.Focus != FocusUnchanged && input.Focus != FocusGained && input.Focus != FocusLost) {
		return FrameCommand{}, fmt.Errorf("%w: frame timing or focus", InvalidAction)
	}
	if input.Reticle.Visible && (!input.Reticle.Pointer || math.IsNaN(input.Reticle.X) || math.IsNaN(input.Reticle.Y) || math.IsInf(input.Reticle.X, 0) || math.IsInf(input.Reticle.Y, 0) || input.Reticle.X < 0 || input.Reticle.X >= 1280 || input.Reticle.Y < 0 || input.Reticle.Y >= 720) {
		return FrameCommand{}, fmt.Errorf("%w: reticle position", InvalidAction)
	}
	seen := make(map[settings.Action]bool, len(input.Values))
	for _, value := range input.Values {
		if !value.Action.Valid() || seen[value.Action] || math.IsNaN(value.X) || math.IsNaN(value.Y) || math.IsInf(value.X, 0) || math.IsInf(value.Y, 0) || math.Abs(value.X) > 1 || math.Abs(value.Y) > 1 {
			return FrameCommand{}, fmt.Errorf("%w: action value", InvalidAction)
		}
		seen[value.Action] = true
	}
	for _, edge := range input.Edges {
		if !edge.Action.Valid() || (edge.Kind != Pressed && edge.Kind != Released) {
			return FrameCommand{}, fmt.Errorf("%w: action edge", InvalidAction)
		}
	}
	for _, device := range input.Devices {
		if device.Device != settings.Keyboard && device.Device != settings.Mouse && device.Device != settings.Controller {
			return FrameCommand{}, fmt.Errorf("%w: input device", InvalidAction)
		}
	}
	for _, selection := range input.Selections {
		if selection.Action == "" {
			return FrameCommand{}, fmt.Errorf("%w: menu selection", InvalidAction)
		}
	}
	return FrameCommand{input: cloneFrameInput(input)}, nil
}

// Data returns an independent copy of the frame input.
func (command FrameCommand) Data(ctx context.Context) FrameInput {
	defer mon.Task()(&ctx)(nil)
	return cloneFrameInput(command.input)
}

func cloneFrameInput(input FrameInput) FrameInput {
	input.Values = slices.Clone(input.Values)
	input.Edges = slices.Clone(input.Edges)
	input.Devices = slices.Clone(input.Devices)
	input.Selections = slices.Clone(input.Selections)
	return input
}

// FramePolicy owns the display-free frame, input and state policies. It is
// single-owner, like World. BindWorld connects a concrete world so entering a
// menu or storage state clears the world's own tick-based action buffers too.
// The subsequent coordinator remains responsible for campaign and storage.
type FramePolicy struct {
	states StatePolicy

	game FixedClock

	scene FixedClock

	input inputQueue

	world *simulation.World

	focused bool

	// A storage or restoration interruption must survive acknowledgment and
	// recovery until the next active or scene destination is safely paused.
	pausePending bool
}

// NewFramePolicy starts at the focused title screen, before a world exists.
func NewFramePolicy() *FramePolicy { return &FramePolicy{focused: true} }

// State returns the current application state for immutable view construction.
func (policy *FramePolicy) State() AppState { return policy.states.State() }

// BindWorld replaces the transient world after restoration. A world installed
// outside active play is suspended until the policy permits active play.
func (policy *FramePolicy) BindWorld(ctx context.Context, world *simulation.World) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world != nil {
		if policy.State() == StateActive {
			if err = world.ResumePlayer(ctx, nil); err != nil {
				return err
			}
		} else {
			world.SuspendPlayer(ctx)
		}
	}
	policy.world = world
	return nil
}

// Transition changes state and applies clock and input safety atomically with
// respect to the session owner. A Resume requires focus and released controls.
// SaveSuccess is called only after the coordinator receives acknowledgment.
func (policy *FramePolicy) Transition(ctx context.Context, event StateEvent, destination AppState) (err error) {
	defer mon.Task()(&ctx)(&err)
	if event == ResumeGame && (!policy.focused || !policy.input.canResume()) {
		return fmt.Errorf("%w: resume requires focus and released controls", InvalidAction)
	}
	before := policy.State()
	next := policy.states
	if err = next.Transition(ctx, event, destination); err != nil {
		return err
	}
	// A focus/device interruption in saving, error recovery or restoration
	// remains latched until a playable destination can be safely paused.
	pending := policy.pausePending
	if (next.State() == StateActive || next.State() == StateScene) && (!policy.focused || pending) {
		if err = next.focusPause(ctx); err != nil {
			return err
		}
	}
	after := next.State()
	switch after {
	case StatePause, StateTitle, StateDead, StateEnd:
		pending = false
	}
	if before != StateActive && after == StateActive && policy.world != nil {
		if err = policy.world.ResumePlayer(ctx, nil); err != nil {
			return err
		}
	}
	if before == StateActive && after != StateActive && policy.world != nil {
		policy.world.SuspendPlayer(ctx)
	}
	policy.states = next
	policy.pausePending = pending
	policy.onTransition(ctx, before, after)
	return nil
}

func (policy *FramePolicy) onTransition(ctx context.Context, before, after AppState) {
	defer mon.Task()(&ctx)(nil)
	if before == after {
		return
	}
	policy.game.Clear()
	policy.input.clearGameplay()
	if before == StateScene {
		policy.scene.Freeze()
	}
	// A new scene or a completed scene has no old fractional progress. Pausing
	// or saving a scene retains its fractional tick until that scene returns.
	if after == StateScene && before != StatePause && before != StateSaving && before != StateError {
		policy.scene.Clear()
	}
	if before == StateScene && after == StateActive {
		policy.scene.Clear()
	}
}

// FrameDecision reports interruptions before any gameplay or menu selection is
// acted upon. Selections are owned copies and are empty after focus/device loss.
type FrameDecision struct {
	State AppState

	Interrupted bool

	Stalled bool

	CloseRequested bool

	Selections []MenuSelection
}

// BeginFrame samples once, handles interruptions and overlays first, and opens
// a fixed-tick budget. Call NextTick until it returns false, or Transition to
// saving/error after any tick. A state change consumes no elapsed gameplay time.
func (policy *FramePolicy) BeginFrame(ctx context.Context, command FrameCommand) (_ FrameDecision, err error) {
	defer mon.Task()(&ctx)(&err)
	frame := command.Data(ctx)
	if policy.game.open || policy.scene.open {
		return FrameDecision{}, fmt.Errorf("%w: previous frame has unconsumed ticks", InvalidAction)
	}
	disconnected := false
	for _, device := range frame.Devices {
		if device.Device == settings.Controller && device.Active && !device.Connected {
			disconnected = true
		}
	}
	if frame.Focus == FocusGained {
		policy.focused = true
	} else if frame.Focus == FocusLost {
		policy.focused = false
	}
	edges := policy.input.sample(ctx, frame, disconnected)
	decision := FrameDecision{CloseRequested: frame.CloseRequested}
	if frame.Focus == FocusLost || disconnected {
		before := policy.State()
		if before == StateSaving || before == StateError || before == StateRestoring {
			policy.pausePending = true
		}
		if err = policy.states.focusPause(ctx); err != nil {
			return FrameDecision{}, err
		}
		if policy.world != nil {
			policy.world.SuspendPlayer(ctx)
		}
		policy.game.Clear()
		policy.scene.Freeze()
		policy.input.clearGameplay()
		policy.onTransition(ctx, before, policy.State())
		decision.State, decision.Interrupted = policy.State(), true
		return decision, nil
	}
	decision.Selections = slices.Clone(frame.Selections)
	before := policy.State()
	// Pause and ledger edges are processed before gameplay edges regardless of
	// their order in the sample. A pause button never implicitly resumes.
	for _, edge := range edges {
		if edge.Kind != Pressed {
			continue
		}
		if edge.Action == settings.Pause && (policy.State() == StateActive || policy.State() == StateScene) {
			if err = policy.Transition(ctx, OpenPause, ""); err != nil {
				return FrameDecision{}, err
			}
			break
		}
		if edge.Action == settings.Ledger {
			event := OpenLedger
			if policy.State() == StateLedger {
				event = CloseLedger
			}
			if policy.State() == StateActive || policy.State() == StatePause || policy.State() == StateLedger {
				if err = policy.Transition(ctx, event, ""); err != nil {
					return FrameDecision{}, err
				}
				break
			}
		}
	}
	if before == policy.State() && policy.State() == StatePause {
		for _, selection := range frame.Selections {
			if selection.Action == "resume" {
				if err = policy.Transition(ctx, ResumeGame, ""); err != nil {
					return FrameDecision{}, err
				}
				break
			}
		}
	}
	decision.State = policy.State()
	if before != policy.State() || !policy.focused {
		return decision, nil
	}
	if policy.State() == StateActive {
		policy.input.enqueue(ctx, frame, edges)
		decision.Stalled, err = policy.game.Begin(ctx, frame.Elapsed, policy.State())
	} else if policy.State() == StateScene {
		decision.Stalled, err = policy.scene.Begin(ctx, frame.Elapsed, policy.State())
	}
	if err != nil {
		return FrameDecision{}, err
	}
	if decision.Stalled {
		if err = policy.Transition(ctx, OpenPause, ""); err != nil {
			return FrameDecision{}, err
		}
		decision.State, decision.Interrupted = policy.State(), true
	}
	return decision, nil
}

// TickKind identifies the clock advanced by NextTick.
type TickKind string

const (
	// GameplayTick advances the authoritative world by one fixed tick.
	GameplayTick TickKind = "gameplay"

	// SceneTick advances an authored scene without advancing gameplay.
	SceneTick TickKind = "scene"
)

// NextTick consumes one eligible tick. Gameplay input includes aim and ordered
// edges on the first tick only. A scene tick never carries gameplay input.
func (policy *FramePolicy) NextTick(ctx context.Context) (_ TickKind, _ simulation.PlayerCommand, ok bool) {
	defer mon.Task()(&ctx)(nil)
	switch policy.State() {
	case StateActive:
		if policy.game.Next(StateActive) {
			return GameplayTick, policy.input.playerTick(ctx), true
		}
	case StateScene:
		if policy.scene.Next(StateScene) {
			return SceneTick, simulation.PlayerCommand{}, true
		}
	}
	return "", simulation.PlayerCommand{}, false
}

// Diagnostic returns a bounded-update diagnostic for the current clock.
func (policy *FramePolicy) Diagnostic() ClockDiagnostic {
	if policy.State() == StateScene {
		return policy.scene.Diagnostic()
	}
	return policy.game.Diagnostic()
}

// Interpolation is the current clock's fractional tick, never authoritative.
func (policy *FramePolicy) Interpolation() float64 {
	if policy.State() == StateScene {
		return policy.scene.Fraction()
	}
	if policy.State() == StateActive {
		return policy.game.Fraction()
	}
	return 0
}

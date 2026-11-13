// Copyright (C) 2026 crawter
// All rights reserved.

// Package input translates one desktop device sample into session commands.
package input

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	monkit "github.com/spacemonkeygo/monkit/v3"

	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

var mon = monkit.Package()

// ErrThread indicates sampling on a thread other than the raylib owner.
var ErrThread = errors.New("input sampled on a different thread")

// ThreadID returns an OS thread identity. The caller locks the initial thread
// for the window lifetime and supplies the same identity function as assets.
type ThreadID func() uint64

// Frame describes the presentation geometry and timing for a single sample.
// Facing is used only until the first valid aim. The UI resolves navigation
// edges and pointer hit tests into selections, then adds at most one selection
// to the command before calling the session.
type Frame struct {
	Elapsed        time.Duration
	View           Viewport
	ThrowOrigin    Point
	Facing         int
	CloseRequested bool
}

// Sampler is owned by the main thread. Session FramePolicy owns the action
// queue across frames and drains edges once on the first eligible tick.
type Sampler struct {
	threadID      ThreadID
	owner         uint64
	bindings      []settings.Binding
	active        settings.Device
	focused       bool
	connected     bool
	initialized   bool
	previous      Snapshot
	held          map[settings.Action]bool
	aim           Point
	aimed         bool
	pointerUsed   bool
	pointer       Point
	pointerInView bool

	// A disconnected controller cannot prove that its controls were released.
	// Quarantine dangerous physical controls observed held on reconnection.
	controllerRecovery bool
	controllerBlocked  map[Control]bool
}

// New validates settings without opening any native device.
func New(ctx context.Context, values settings.Values, threadID ThreadID) (_ *Sampler, err error) {
	defer mon.Task()(&ctx)(&err)
	if threadID == nil {
		return nil, ErrThread
	}
	owner := threadID()
	if owner == 0 {
		return nil, ErrThread
	}
	sampler := &Sampler{threadID: threadID, owner: owner, active: settings.Keyboard, focused: true, held: make(map[settings.Action]bool)}
	if err = sampler.SetBindings(ctx, values); err != nil {
		return nil, err
	}
	return sampler, nil
}

// Sample polls raylib once on the owning thread. The caller disables raylib's
// Escape exit key and supplies the window close request separately.
func (sampler *Sampler) Sample(ctx context.Context, frame Frame) (_ session.FrameCommand, err error) {
	defer mon.Task()(&ctx)(&err)
	if sampler == nil || sampler.threadID == nil || sampler.threadID() != sampler.owner {
		return session.FrameCommand{}, ErrThread
	}
	return sampler.SampleSnapshot(ctx, frame, pollNative(ctx))
}

// SampleSnapshot translates a single already-polled snapshot. This seam is
// useful to exercise the actual adapter without opening a window or device.
func (sampler *Sampler) SampleSnapshot(ctx context.Context, frame Frame, snapshot Snapshot) (_ session.FrameCommand, err error) {
	defer mon.Task()(&ctx)(&err)
	if sampler == nil || frame.Elapsed < 0 {
		return session.FrameCommand{}, fmt.Errorf("%w: sampler or elapsed time", session.InvalidAction)
	}
	for _, value := range []float64{snapshot.Pointer.X, snapshot.Pointer.Y, snapshot.Wheel, snapshot.LeftStick.X, snapshot.LeftStick.Y, snapshot.RightStick.X, snapshot.RightStick.Y, frame.ThrowOrigin.X, frame.ThrowOrigin.Y} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return session.FrameCommand{}, fmt.Errorf("%w: non-finite device input", session.InvalidAction)
		}
	}
	if !snapshot.Connected {
		snapshot.LeftStick, snapshot.RightStick = Point{}, Point{}
	}
	input := session.FrameInput{Elapsed: frame.Elapsed, CloseRequested: frame.CloseRequested}
	if snapshot.Focused != sampler.focused {
		if snapshot.Focused {
			input.Focus = session.FocusGained
		} else {
			input.Focus = session.FocusLost
		}
	}
	disconnected := sampler.connected && !snapshot.Connected
	interrupted := disconnected && sampler.active == settings.Controller
	blocked := sampler.controllerQuarantine(snapshot, disconnected, interrupted)
	if snapshot.Connected != sampler.connected {
		input.Devices = append(input.Devices, session.DeviceChange{Device: settings.Controller, Connected: snapshot.Connected, Active: sampler.active == settings.Controller})
	}
	oldActive := sampler.active
	if snapshot.Focused && !interrupted {
		sampler.chooseDevice(snapshot, blocked)
	}
	if disconnected && sampler.active == settings.Controller {
		sampler.active = settings.Keyboard
	}
	if sampler.active != oldActive {
		input.Devices = append(input.Devices, session.DeviceChange{Device: sampler.active, Connected: true, Active: true})
	}

	// Aggregate before calculating edges: releasing one of two bindings for the
	// same action must not release that action while the other is still held.
	values := make(map[settings.Action]float64)
	pulses := make(map[settings.Action]bool)
	order := make([]settings.Action, 0)
	seen := make(map[settings.Action]bool)
	for _, binding := range sampler.bindings {
		if binding.Action == settings.Aim {
			continue
		}
		if !seen[binding.Action] {
			order = append(order, binding.Action)
			seen[binding.Action] = true
		}
		control := Control{Device: binding.Device, Name: binding.Control}
		value := controlValue(snapshot, control)
		if blocked[control] {
			// A quarantined control gates Resume without making an independent
			// device's matching action appear held to the session.
			if dangerousAction(binding.Action) && value != 0 {
				input.ResumeBlocked = true
			}
			continue
		}
		if binding.Device == settings.Mouse && binding.Control == "Wheel" {
			pulses[binding.Action] = value != 0
		} else {
			values[binding.Action] = math.Max(values[binding.Action], value)
		}
	}
	for _, action := range order {
		value := values[action]
		// Focus loss may make the native library synthesize releases. Require a
		// real, focused sample before considering a previously held control up.
		if !snapshot.Focused && sampler.held[action] {
			value = math.Max(value, 1)
		}
		down := value != 0
		wasHeld := sampler.held[action]
		if snapshot.Focused && !interrupted && down != wasHeld {
			kind := session.Released
			if down {
				kind = session.Pressed
			}
			input.Edges = append(input.Edges, session.ActionEdge{Action: action, Kind: kind})
		}
		if snapshot.Focused && !interrupted && pulses[action] && !down && !wasHeld {
			input.Edges = append(input.Edges, session.ActionEdge{Action: action, Kind: session.Pressed}, session.ActionEdge{Action: action, Kind: session.Released})
		}
		sampler.held[action] = down
		// A pointer click is a menu confirmation too, even when melee has
		// been remapped. Report its held state to the session's Resume gate
		// without inventing a second navigation edge for the UI.
		if action == settings.MenuConfirm {
			value = math.Max(value, values[settings.MenuSelect])
			if !snapshot.Focused && sampler.held[settings.MenuSelect] {
				value = 1
			}
		}
		input.Values = append(input.Values, session.ActionValue{Action: action, X: value})
	}
	if !sampler.aimed {
		sampler.aim = Point{X: 1}
		if frame.Facing < 0 {
			sampler.aim.X = -1
		}
	}
	if snapshot.Focused && !interrupted {
		sampler.sampleAim(ctx, snapshot, frame)
	}
	aim := sampler.aim
	// Until the player aims explicitly, allow movement to determine facing.
	// A zero aim delegates the same facing fallback to simulation on that tick.
	if !sampler.aimed && (values[settings.MoveLeft] != 0 || values[settings.MoveRight] != 0) {
		aim = Point{}
	}
	input.Values = append(input.Values, session.ActionValue{Action: settings.Aim, X: aim.X, Y: aim.Y})
	command, err := session.NewFrameCommand(ctx, input)
	if err != nil {
		return session.FrameCommand{}, err
	}
	sampler.focused, sampler.connected, sampler.initialized = snapshot.Focused, snapshot.Connected, true
	sampler.pointer, sampler.pointerInView = frame.View.LogicalPoint(ctx, snapshot.Pointer)
	if !snapshot.Focused {
		sampler.pointerInView = false
	}
	// Snapshot maps belong to the caller and may be reused immediately.
	sampler.previous = snapshot
	sampler.previous.Buttons = make(map[Control]bool, len(snapshot.Buttons))
	for control, down := range snapshot.Buttons {
		sampler.previous.Buttons[control] = down
	}
	return command, nil
}

// dangerousAction identifies actions whose physical inputs must be released
// after an interruption, including pointer-style menu confirmation.
func dangerousAction(action settings.Action) bool {
	switch action {
	case settings.Melee, settings.Throw, settings.RemoveLimb, settings.MenuConfirm, settings.MenuSelect:
		return true
	default:
		return false
	}
}

// controllerQuarantine is measured by SampleSnapshot. It works with physical
// controls rather than actions because one controller button may be bound in
// more than one context (for example, A is both jump and confirm).
func (sampler *Sampler) controllerQuarantine(snapshot Snapshot, disconnected, interrupted bool) map[Control]bool {
	if interrupted || (disconnected && len(sampler.controllerBlocked) != 0) {
		sampler.controllerRecovery = true
	}
	if !snapshot.Connected {
		return nil
	}
	if sampler.controllerRecovery {
		dangerous := make(map[Control]bool)
		for _, binding := range sampler.bindings {
			if binding.Device == settings.Controller && dangerousAction(binding.Action) {
				dangerous[Control{Device: binding.Device, Name: binding.Control}] = true
			}
		}
		if !snapshot.Focused {
			// Until focus returns, even an apparent release is unreliable.
			for control := range sampler.controllerBlocked {
				dangerous[control] = true
			}
			return dangerous
		}
		if sampler.controllerBlocked == nil {
			sampler.controllerBlocked = make(map[Control]bool)
		}
		for control := range dangerous {
			if controlValue(snapshot, control) != 0 {
				sampler.controllerBlocked[control] = true
			}
		}
		sampler.controllerRecovery = false
	}
	if snapshot.Focused {
		for control := range sampler.controllerBlocked {
			if controlValue(snapshot, control) == 0 {
				delete(sampler.controllerBlocked, control)
			}
		}
	}
	return sampler.controllerBlocked
}

// Pointer returns the latest logical-canvas cursor and whether it is inside
// the focused content area. UI hit testing must ignore it when false.
func (sampler *Sampler) Pointer() (Point, bool) {
	return sampler.pointer, sampler.pointerInView
}

// chooseDevice gives simultaneous new controller input priority, then mouse,
// then keyboard. Continuing held controls and sub-dead-zone noise never steal
// prompts. Meaningful stick changes must exceed a small jitter threshold.
func (sampler *Sampler) chooseDevice(snapshot Snapshot, blocked map[Control]bool) {
	keyboard, mouse, controller := false, false, false
	for _, binding := range sampler.bindings {
		control := Control{Device: binding.Device, Name: binding.Control}
		if blocked[control] {
			continue
		}
		if control.Name == "Motion" || control.Name == "Wheel" || (control.Device == settings.Controller && (strings.HasPrefix(control.Name, "LeftStick") || strings.HasPrefix(control.Name, "RightStick")) && !strings.HasSuffix(control.Name, "Click")) {
			continue
		}
		current, previous := controlValue(snapshot, control), controlValue(sampler.previous, control)
		if current > 0 && previous == 0 {
			switch control.Device {
			case settings.Keyboard:
				keyboard = true
			case settings.Mouse:
				mouse = true
			case settings.Controller:
				controller = true
			}
		}
	}
	if sampler.initialized && math.Hypot(snapshot.Pointer.X-sampler.previous.Pointer.X, snapshot.Pointer.Y-sampler.previous.Pointer.Y) >= 2 {
		mouse = true
	}
	if snapshot.Wheel != 0 {
		mouse = true
	}
	if snapshot.Connected {
		for _, pair := range [][2]Point{{scaledStick(snapshot.LeftStick, .20), scaledStick(sampler.previous.LeftStick, .20)}, {scaledStick(snapshot.RightStick, .25), scaledStick(sampler.previous.RightStick, .25)}} {
			if math.Hypot(pair[0].X, pair[0].Y) > 0 && math.Hypot(pair[0].X-pair[1].X, pair[0].Y-pair[1].Y) >= .05 {
				controller = true
			}
		}
	}
	switch {
	case controller:
		sampler.active = settings.Controller
	case mouse:
		sampler.active = settings.Mouse
		sampler.pointerUsed = true
	case keyboard:
		sampler.active = settings.Keyboard
	}
}

func (sampler *Sampler) sampleAim(ctx context.Context, snapshot Snapshot, frame Frame) {
	defer mon.Task()(&ctx)(nil)
	for _, binding := range sampler.bindings {
		if binding.Action != settings.Aim {
			continue
		}
		if binding.Device == settings.Mouse && sampler.active != settings.Controller && sampler.pointerUsed {
			world, ok := frame.View.WorldPoint(ctx, snapshot.Pointer)
			if ok {
				if aim, valid := direction(frame.ThrowOrigin, world); valid {
					sampler.aim, sampler.aimed = aim, true
				}
			}
		}
		if binding.Device == settings.Controller && snapshot.Connected {
			stick, previous, dead := snapshot.RightStick, sampler.previous.RightStick, .25
			if binding.Control == "LeftStick" {
				stick, previous, dead = snapshot.LeftStick, sampler.previous.LeftStick, .20
			}
			stick, previous = scaledStick(stick, dead), scaledStick(previous, dead)
			// A stick just outside its dead zone still aims even before its
			// movement is large enough to replace the current prompt device.
			if sampler.active == settings.Controller || stick != previous {
				if aim, valid := direction(Point{}, Point{X: stick.X, Y: -stick.Y}); valid {
					sampler.aim, sampler.aimed = aim, true
				}
			}
		}
	}
}

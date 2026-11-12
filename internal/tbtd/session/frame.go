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

// FrameInput is the construction data for one frame. Elapsed is an interval
// from a monotonic clock, not an absolute timestamp. Edges remain ordered.
type FrameInput struct {
	Elapsed time.Duration

	Values []ActionValue

	Edges []ActionEdge

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

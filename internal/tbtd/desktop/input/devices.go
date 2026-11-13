// Copyright (C) 2026 crawter
// All rights reserved.

package input

import (
	"context"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"too-broke-to-die/internal/tbtd/settings"
)

// Control identifies a portable physical control, independent of its action.
type Control struct {
	Device settings.Device
	Name   string
}

// Point uses screen coordinates for the pointer and stick axes, and world
// coordinates (positive Y upward) for camera targets and throw origins.
type Point struct{ X, Y float64 }

// Snapshot is a single device poll. Buttons contains digital controls only;
// sticks use raylib's screen-space axes, in [-1,1]. Wheel is a per-frame pulse.
// A disconnected controller contributes no controls.
type Snapshot struct {
	Focused    bool
	Connected  bool
	Buttons    map[Control]bool
	Pointer    Point
	Wheel      float64
	LeftStick  Point
	RightStick Point
}

var keyCodes = map[string]int32{
	"Left": rl.KeyLeft, "Right": rl.KeyRight, "Up": rl.KeyUp, "Down": rl.KeyDown,
	"Space": rl.KeySpace, "Enter": rl.KeyEnter, "Escape": rl.KeyEscape, "Tab": rl.KeyTab,
	"LeftShift": rl.KeyLeftShift, "RightShift": rl.KeyRightShift,
	"LeftControl": rl.KeyLeftControl, "RightControl": rl.KeyRightControl,
	"LeftAlt": rl.KeyLeftAlt, "RightAlt": rl.KeyRightAlt,
	"Backspace": rl.KeyBackspace, "Delete": rl.KeyDelete, "Home": rl.KeyHome,
	"End": rl.KeyEnd, "PageUp": rl.KeyPageUp, "PageDown": rl.KeyPageDown,
}

var padCodes = map[string]int32{
	"A": rl.GamepadButtonRightFaceDown, "B": rl.GamepadButtonRightFaceRight,
	"X": rl.GamepadButtonRightFaceLeft, "Y": rl.GamepadButtonRightFaceUp,
	"LB": rl.GamepadButtonLeftTrigger1, "RB": rl.GamepadButtonRightTrigger1,
	"View": rl.GamepadButtonMiddleLeft, "Menu": rl.GamepadButtonMiddleRight,
	"DPadUp": rl.GamepadButtonLeftFaceUp, "DPadDown": rl.GamepadButtonLeftFaceDown,
	"DPadLeft": rl.GamepadButtonLeftFaceLeft, "DPadRight": rl.GamepadButtonLeftFaceRight,
	"LeftStickClick": rl.GamepadButtonLeftThumb, "RightStickClick": rl.GamepadButtonRightThumb,
}

// pollNative is called exactly once per displayed frame on the raylib thread.
// Raylib presents trigger axes from -1 (released) to +1 (pressed).
func pollNative(ctx context.Context) Snapshot {
	defer mon.Task()(&ctx)(nil)
	snapshot := Snapshot{Focused: rl.IsWindowFocused() && !rl.IsWindowMinimized(), Connected: rl.IsGamepadAvailable(0), Buttons: make(map[Control]bool)}
	for name, code := range keyCodes {
		snapshot.Buttons[Control{settings.Keyboard, name}] = rl.IsKeyDown(code)
	}
	for code := int32('0'); code <= '9'; code++ {
		snapshot.Buttons[Control{settings.Keyboard, string(rune(code))}] = rl.IsKeyDown(code)
	}
	for code := int32('A'); code <= 'Z'; code++ {
		snapshot.Buttons[Control{settings.Keyboard, string(rune(code))}] = rl.IsKeyDown(code)
	}
	for name, code := range map[string]rl.MouseButton{"Left": rl.MouseButtonLeft, "Right": rl.MouseButtonRight, "Middle": rl.MouseButtonMiddle} {
		snapshot.Buttons[Control{settings.Mouse, name}] = rl.IsMouseButtonDown(code)
	}
	pointer := rl.GetMousePosition()
	snapshot.Pointer = Point{X: float64(pointer.X), Y: float64(pointer.Y)}
	snapshot.Wheel = float64(rl.GetMouseWheelMove())
	if snapshot.Connected {
		for name, code := range padCodes {
			snapshot.Buttons[Control{settings.Controller, name}] = rl.IsGamepadButtonDown(0, code)
		}
		snapshot.Buttons[Control{settings.Controller, "LT"}] = rl.GetGamepadAxisMovement(0, rl.GamepadAxisLeftTrigger) > 0
		snapshot.Buttons[Control{settings.Controller, "RT"}] = rl.GetGamepadAxisMovement(0, rl.GamepadAxisRightTrigger) > 0
		snapshot.LeftStick = Point{X: float64(rl.GetGamepadAxisMovement(0, rl.GamepadAxisLeftX)), Y: float64(rl.GetGamepadAxisMovement(0, rl.GamepadAxisLeftY))}
		snapshot.RightStick = Point{X: float64(rl.GetGamepadAxisMovement(0, rl.GamepadAxisRightX)), Y: float64(rl.GetGamepadAxisMovement(0, rl.GamepadAxisRightY))}
	}
	return snapshot
}

// scaledStick applies a radial dead zone and linear scaling, clamping the
// diagonal to unit magnitude. The enclosing sample measures this hot helper.
func scaledStick(point Point, dead float64) Point {
	length := math.Hypot(point.X, point.Y)
	if length <= dead || math.IsNaN(length) || math.IsInf(length, 0) {
		return Point{}
	}
	scale := (math.Min(length, 1) - dead) / (1 - dead) / length
	return Point{X: point.X * scale, Y: point.Y * scale}
}

func controlValue(snapshot Snapshot, control Control) float64 {
	if control.Device == settings.Controller && !snapshot.Connected {
		return 0
	}
	if control.Device == settings.Mouse && control.Name == "Wheel" {
		if snapshot.Wheel != 0 {
			return 1
		}
		return 0
	}
	if control.Device == settings.Controller {
		left, right := scaledStick(snapshot.LeftStick, .20), scaledStick(snapshot.RightStick, .25)
		switch control.Name {
		case "LeftStickUp":
			return math.Max(0, -left.Y)
		case "LeftStickDown":
			return math.Max(0, left.Y)
		case "LeftStickLeft":
			return math.Max(0, -left.X)
		case "LeftStickRight":
			return math.Max(0, left.X)
		case "RightStickUp":
			return math.Max(0, -right.Y)
		case "RightStickDown":
			return math.Max(0, right.Y)
		case "RightStickLeft":
			return math.Max(0, -right.X)
		case "RightStickRight":
			return math.Max(0, right.X)
		}
	}
	if snapshot.Buttons[control] {
		return 1
	}
	return 0
}

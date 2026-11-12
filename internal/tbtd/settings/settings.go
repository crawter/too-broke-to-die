// Copyright (C) 2026 crawter
// All rights reserved.

// Package settings defines player preferences independently of campaign progress.
package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	monkit "github.com/spacemonkeygo/monkit/v3"
)

var mon = monkit.Package()

// ErrInvalid identifies malformed or conflicting preferences.
var ErrInvalid = errors.New("invalid settings")

// DisplayMode is a requested presentation mode, not a native window handle.
type DisplayMode string

const (
	// Windowed requests a window with desktop decorations.
	Windowed DisplayMode = "windowed"

	// Borderless requests a borderless desktop presentation.
	Borderless DisplayMode = "borderless"

	// Fullscreen requests fullscreen presentation.
	Fullscreen DisplayMode = "fullscreen"
)

// Display is a requested mode. Device support and timed confirmation belong to
// the desktop, which may fall back to a supported window without changing play.
type Display struct {
	Mode DisplayMode

	Width int

	Height int

	VSync bool
}

// Audio levels are integer percentages, inclusive from 0 through 100.
type Audio struct {
	Master int

	Music int

	Effects int

	Ambience int

	Vocal int

	Mono bool

	ReducedDynamicRange bool
}

// Shake controls decorative camera shake.
type Shake string

const (
	// ShakeOff disables decorative screen shake.
	ShakeOff Shake = "off"

	// ShakeLow uses the reduced default screen shake.
	ShakeLow Shake = "low"

	// ShakeFull uses the full specified screen shake within its safety limits.
	ShakeFull Shake = "full"
)

// Particles controls blood particle density, not essential wounds or harm.
type Particles string

const (
	// ParticlesFull shows the standard blood particle density.
	ParticlesFull Particles = "full"

	// ParticlesReduced limits sprays and stains while retaining essential harm.
	ParticlesReduced Particles = "reduced"
)

// Interface contains the independent caption and text options.
type Interface struct {
	TextSize int

	DialogueCaptions bool

	SoundCaptions bool

	CaptionOpacity int
}

// Accessibility changes presentation and input assistance, never game rules.
type Accessibility struct {
	Shake Shake

	ReducedMotion bool

	HighContrast bool

	Blood Particles

	HoldAssist bool
}

// Action is a semantic action, independent of a native device key code.
type Action string

const (
	// MoveLeft runs the player to the left.
	MoveLeft Action = "move_left"

	// MoveRight runs the player to the right.
	MoveRight Action = "move_right"

	// Down combines with jump to drop through a one-way platform.
	Down Action = "down"

	// Jump starts a jump or, with Down, a platform drop.
	Jump Action = "jump"

	// Aim supplies the throw direction from the pointer or stick.
	Aim Action = "aim"

	// Melee starts a horizontal melee attack.
	Melee Action = "melee"

	// Throw launches the selected payload.
	Throw Action = "throw"

	// Evade starts an evasive move in the facing direction.
	Evade Action = "evade"

	// Interact operates a nearby target or retrieves or replaces a limb.
	Interact Action = "interact"

	// SelectPayload cycles the available limb and feces payloads.
	SelectPayload Action = "select_payload"

	// SelectLeftArm selects the left arm socket.
	SelectLeftArm Action = "select_left_arm"

	// SelectRightArm selects the right arm socket.
	SelectRightArm Action = "select_right_arm"

	// SelectLeftLeg selects the left leg socket.
	SelectLeftLeg Action = "select_left_leg"

	// SelectRightLeg selects the right leg socket.
	SelectRightLeg Action = "select_right_leg"

	// RemoveLimb starts removal of the selected attached limb.
	RemoveLimb Action = "remove_limb"

	// Regenerate starts the channel to restore missing limbs.
	Regenerate Action = "regenerate"

	// Ledger opens or closes the ledger.
	Ledger Action = "ledger"

	// Pause opens or closes the pause menu.
	Pause Action = "pause"

	// MenuUp moves menu focus upward.
	MenuUp Action = "menu_up"

	// MenuDown moves menu focus downward.
	MenuDown Action = "menu_down"

	// MenuLeft moves menu focus or selection left.
	MenuLeft Action = "menu_left"

	// MenuRight moves menu focus or selection right.
	MenuRight Action = "menu_right"

	// MenuConfirm confirms the focused menu choice.
	MenuConfirm Action = "menu_confirm"

	// MenuBack returns from or cancels the current menu.
	MenuBack Action = "menu_back"

	// MenuSelect selects a menu item with the pointer.
	MenuSelect Action = "menu_select"

	// LedgerPrevious switches to the previous ledger tab.
	LedgerPrevious Action = "ledger_previous"

	// LedgerNext switches to the next ledger tab.
	LedgerNext Action = "ledger_next"
)

// Valid reports whether an action is part of the normalized input contract.
func (action Action) Valid() bool {
	return validAction(Gameplay, action) || validAction(Menu, action) || validAction(LedgerMenu, action)
}

// Context separates actions which may use the same physical input.
type Context string

const (
	// Gameplay identifies bindings active during gameplay.
	Gameplay Context = "gameplay"

	// Menu identifies bindings for menu navigation and recovery.
	Menu Context = "menu"

	// LedgerMenu identifies bindings for changing ledger tabs.
	LedgerMenu Context = "ledger"
)

// Device identifies an input family rather than a particular attached device.
type Device string

const (
	// Keyboard identifies keyboard controls.
	Keyboard Device = "keyboard"

	// Mouse identifies mouse buttons, motion, and wheel input.
	Mouse Device = "mouse"

	// Controller identifies controls on the supported gamepad.
	Controller Device = "controller"
)

// Binding names one physical control in one action context. Stick directions
// and mouse motion/wheel are separate named controls, not native codes.
type Binding struct {
	Context Context

	Action Action

	Device Device

	Control string
}

// Values is a portable settings value. Clone at ownership boundaries because
// Bindings is mutable; no settings value contains campaign progress.
type Values struct {
	Display Display

	Audio Audio

	Interface Interface

	Accessibility Accessibility

	Bindings []Binding
}

// Clone returns an independent copy suitable for a worker or UI draft.
func (values Values) Clone(ctx context.Context) Values {
	defer mon.Task()(&ctx)(nil)
	values.Bindings = slices.Clone(values.Bindings)
	return values
}

// Defaults returns a fresh copy of the PDD defaults. Restore Defaults consists
// of replacing the settings draft with this value; it does not touch a campaign.
func Defaults(ctx context.Context) Values {
	defer mon.Task()(&ctx)(nil)
	return Values{
		Display: Display{Mode: Borderless, Width: 1920, Height: 1080, VSync: true},

		Audio: Audio{Master: 80, Music: 65, Effects: 90, Ambience: 55, Vocal: 80},

		Interface: Interface{TextSize: 100, DialogueCaptions: true, SoundCaptions: true, CaptionOpacity: 90},

		Accessibility: Accessibility{Shake: ShakeLow, Blood: ParticlesFull},

		Bindings: defaultBindings(),
	}
}

// defaultBindings is measured by Defaults.
func defaultBindings() []Binding {
	return []Binding{
		{Gameplay, MoveLeft, Keyboard, "A"}, {Gameplay, MoveLeft, Keyboard, "Left"}, {Gameplay, MoveLeft, Controller, "LeftStickLeft"},
		{Gameplay, MoveRight, Keyboard, "D"}, {Gameplay, MoveRight, Keyboard, "Right"}, {Gameplay, MoveRight, Controller, "LeftStickRight"},
		{Gameplay, Down, Keyboard, "S"}, {Gameplay, Down, Keyboard, "Down"}, {Gameplay, Down, Controller, "LeftStickDown"},
		{Gameplay, Jump, Keyboard, "Space"}, {Gameplay, Jump, Controller, "A"},
		{Gameplay, Aim, Mouse, "Motion"}, {Gameplay, Aim, Controller, "RightStick"},
		{Gameplay, Melee, Mouse, "Left"}, {Gameplay, Melee, Controller, "RB"},
		{Gameplay, Throw, Mouse, "Right"}, {Gameplay, Throw, Controller, "RT"},
		{Gameplay, Evade, Keyboard, "LeftShift"}, {Gameplay, Evade, Controller, "B"},
		{Gameplay, Interact, Keyboard, "E"}, {Gameplay, Interact, Controller, "X"},
		{Gameplay, SelectPayload, Keyboard, "Q"}, {Gameplay, SelectPayload, Mouse, "Wheel"}, {Gameplay, SelectPayload, Controller, "LB"},
		{Gameplay, SelectLeftArm, Keyboard, "1"}, {Gameplay, SelectLeftArm, Controller, "DPadLeft"},
		{Gameplay, SelectRightArm, Keyboard, "2"}, {Gameplay, SelectRightArm, Controller, "DPadRight"},
		{Gameplay, SelectLeftLeg, Keyboard, "3"}, {Gameplay, SelectLeftLeg, Controller, "DPadDown"},
		{Gameplay, SelectRightLeg, Keyboard, "4"}, {Gameplay, SelectRightLeg, Controller, "DPadUp"},
		{Gameplay, RemoveLimb, Keyboard, "R"}, {Gameplay, RemoveLimb, Controller, "Y"},
		{Gameplay, Regenerate, Keyboard, "F"}, {Gameplay, Regenerate, Controller, "LT"},
		{Gameplay, Ledger, Keyboard, "Tab"}, {Gameplay, Ledger, Controller, "View"},
		{Gameplay, Pause, Keyboard, "Escape"}, {Gameplay, Pause, Controller, "Menu"},
		{Menu, MenuUp, Keyboard, "Up"}, {Menu, MenuUp, Keyboard, "W"}, {Menu, MenuUp, Controller, "DPadUp"}, {Menu, MenuUp, Controller, "LeftStickUp"},
		{Menu, MenuDown, Keyboard, "Down"}, {Menu, MenuDown, Keyboard, "S"}, {Menu, MenuDown, Controller, "DPadDown"}, {Menu, MenuDown, Controller, "LeftStickDown"},
		{Menu, MenuLeft, Keyboard, "Left"}, {Menu, MenuLeft, Controller, "DPadLeft"}, {Menu, MenuLeft, Controller, "LeftStickLeft"},
		{Menu, MenuRight, Keyboard, "Right"}, {Menu, MenuRight, Controller, "DPadRight"}, {Menu, MenuRight, Controller, "LeftStickRight"},
		{Menu, MenuConfirm, Keyboard, "Enter"}, {Menu, MenuConfirm, Keyboard, "Space"}, {Menu, MenuConfirm, Controller, "A"},
		{Menu, MenuBack, Keyboard, "Escape"}, {Menu, MenuBack, Controller, "B"}, {Menu, MenuSelect, Mouse, "Left"},
		{LedgerMenu, LedgerPrevious, Controller, "LB"}, {LedgerMenu, LedgerNext, Controller, "RB"},
	}
}

// Validate checks settings independently of any particular monitor or device.
// Duplicate physical controls are rejected within an action context; Escape
// and controller B must remain assigned to menu recovery.
func (values Values) Validate(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if values.Display.Mode != Windowed && values.Display.Mode != Borderless && values.Display.Mode != Fullscreen {
		return fmt.Errorf("%w: display mode", ErrInvalid)
	}
	if values.Display.Width < 1280 || values.Display.Height < 720 || values.Display.Width > 16384 || values.Display.Height > 16384 {
		return fmt.Errorf("%w: display dimensions", ErrInvalid)
	}
	for _, level := range []int{values.Audio.Master, values.Audio.Music, values.Audio.Effects, values.Audio.Ambience, values.Audio.Vocal} {
		if level < 0 || level > 100 {
			return fmt.Errorf("%w: volume", ErrInvalid)
		}
	}
	if (values.Interface.TextSize != 100 && values.Interface.TextSize != 125 && values.Interface.TextSize != 150) || values.Interface.CaptionOpacity < 90 || values.Interface.CaptionOpacity > 100 {
		return fmt.Errorf("%w: interface", ErrInvalid)
	}
	if (values.Accessibility.Shake != ShakeOff && values.Accessibility.Shake != ShakeLow && values.Accessibility.Shake != ShakeFull) || (values.Accessibility.Blood != ParticlesFull && values.Accessibility.Blood != ParticlesReduced) {
		return fmt.Errorf("%w: accessibility", ErrInvalid)
	}
	seen := make(map[Binding]bool)
	type controlKey struct {
		Context Context

		Device Device

		Control string
	}
	controls := make(map[controlKey]bool)
	keyboardMouse := make(map[Action]bool)
	mouse := make(map[Action]bool)
	controller := make(map[Action]bool)
	for _, binding := range values.Bindings {
		if !validAction(binding.Context, binding.Action) || !validControl(binding.Device, binding.Control) {
			return fmt.Errorf("%w: binding %s", ErrInvalid, binding.Action)
		}
		vector := (binding.Device == Mouse && binding.Control == "Motion") || (binding.Device == Controller && (binding.Control == "LeftStick" || binding.Control == "RightStick"))
		if (binding.Action == Aim) != vector {
			return fmt.Errorf("%w: incompatible aim binding", ErrInvalid)
		}
		key := controlKey{Context: binding.Context, Device: binding.Device, Control: binding.Control}
		if controls[key] {
			return fmt.Errorf("%w: duplicate control %s", ErrInvalid, binding.Control)
		}
		controls[key] = true
		seen[binding] = true
		if binding.Device == Controller {
			controller[binding.Action] = true
		} else {
			keyboardMouse[binding.Action] = true
			if binding.Device == Mouse {
				mouse[binding.Action] = true
			}
		}
	}
	// A whole stick and any of its directions overlap physically even though
	// their portable tokens differ. Check this within each action context.
	for _, context := range []Context{Gameplay, Menu, LedgerMenu} {
		for _, stick := range []string{"LeftStick", "RightStick"} {
			if !controls[controlKey{Context: context, Device: Controller, Control: stick}] {
				continue
			}
			for _, direction := range []string{"Up", "Down", "Left", "Right"} {
				if controls[controlKey{Context: context, Device: Controller, Control: stick + direction}] {
					return fmt.Errorf("%w: overlapping stick binding", ErrInvalid)
				}
			}
		}
	}
	for _, action := range []Action{MoveLeft, MoveRight, Down, Jump, Aim, Melee, Throw, Evade, Interact, SelectPayload, SelectLeftArm, SelectRightArm, SelectLeftLeg, SelectRightLeg, RemoveLimb, Regenerate, Ledger, Pause} {
		if !keyboardMouse[action] || !controller[action] {
			return fmt.Errorf("%w: action %s must be available on both input families", ErrInvalid, action)
		}
	}
	for _, action := range []Action{MenuUp, MenuDown, MenuLeft, MenuRight, MenuConfirm, MenuBack} {
		if !keyboardMouse[action] || !controller[action] {
			return fmt.Errorf("%w: menu action %s must be available on both input families", ErrInvalid, action)
		}
	}
	if !mouse[MenuSelect] || !controller[LedgerPrevious] || !controller[LedgerNext] {
		return fmt.Errorf("%w: mouse selection or controller ledger tabs unbound", ErrInvalid)
	}
	if !seen[Binding{Menu, MenuBack, Keyboard, "Escape"}] || !seen[Binding{Menu, MenuBack, Controller, "B"}] {
		return fmt.Errorf("%w: menu recovery bindings", ErrInvalid)
	}
	return nil
}

// The following per-binding helpers are measured by Validate.
func validAction(context Context, action Action) bool {
	switch context {
	case Gameplay:
		switch action {
		case MoveLeft, MoveRight, Down, Jump, Aim, Melee, Throw, Evade, Interact, SelectPayload, SelectLeftArm, SelectRightArm, SelectLeftLeg, SelectRightLeg, RemoveLimb, Regenerate, Ledger, Pause:
			return true
		}
	case Menu:
		return action == MenuUp || action == MenuDown || action == MenuLeft || action == MenuRight || action == MenuConfirm || action == MenuBack || action == MenuSelect
	case LedgerMenu:
		return action == LedgerPrevious || action == LedgerNext
	}
	return false
}

func validControl(device Device, control string) bool {
	switch device {
	case Keyboard:
		if len(control) == 1 && strings.Contains("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", control) {
			return true
		}
		switch control {
		case "Left", "Right", "Up", "Down", "Space", "Enter", "Escape", "Tab", "LeftShift", "RightShift", "LeftControl", "RightControl", "LeftAlt", "RightAlt", "Backspace", "Delete", "Home", "End", "PageUp", "PageDown":
			return true
		}
	case Mouse:
		return control == "Left" || control == "Right" || control == "Middle" || control == "Wheel" || control == "Motion"
	case Controller:
		switch control {
		case "A", "B", "X", "Y", "LB", "RB", "LT", "RT", "View", "Menu", "LeftStick", "LeftStickUp", "LeftStickDown", "LeftStickLeft", "LeftStickRight", "RightStick", "RightStickUp", "RightStickDown", "RightStickLeft", "RightStickRight", "DPadUp", "DPadDown", "DPadLeft", "DPadRight", "LeftStickClick", "RightStickClick":
			return true
		}
	}
	return false
}

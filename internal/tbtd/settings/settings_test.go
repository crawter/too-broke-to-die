// Copyright (C) 2026 crawter
// All rights reserved.

package settings

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestDefaultsAndIsolation(t *testing.T) {
	ctx := context.Background()
	values := Defaults(ctx)
	if err := values.Validate(ctx); err != nil {
		t.Fatal(err)
	}
	if values.Display != (Display{Mode: Borderless, Width: 1920, Height: 1080, VSync: true}) || values.Audio != (Audio{Master: 80, Music: 65, Effects: 90, Ambience: 55, Vocal: 80}) {
		t.Fatalf("default display or audio: %+v %+v", values.Display, values.Audio)
	}
	if values.Interface != (Interface{TextSize: 100, DialogueCaptions: true, SoundCaptions: true, CaptionOpacity: 90}) || values.Accessibility != (Accessibility{Shake: ShakeLow, Blood: ParticlesFull}) {
		t.Fatalf("default accessibility: %+v %+v", values.Interface, values.Accessibility)
	}
	for _, binding := range []Binding{
		{Context: Gameplay, Action: Aim, Device: Mouse, Control: "Motion"},
		{Context: Gameplay, Action: Melee, Device: Controller, Control: "RB"},
		{Context: Gameplay, Action: Regenerate, Device: Controller, Control: "LT"},
		{Context: Menu, Action: MenuBack, Device: Keyboard, Control: "Escape"},
		{Context: Menu, Action: MenuBack, Device: Controller, Control: "B"},
	} {
		if !slices.Contains(values.Bindings, binding) {
			t.Fatalf("missing default binding %+v", binding)
		}
	}
	copy := values.Clone(ctx)
	copy.Bindings[0].Control = "Z"
	if values.Bindings[0].Control != "A" || Defaults(ctx).Bindings[0].Control != "A" {
		t.Fatal("clone or restore defaults shares bindings")
	}
}

func TestSettingsValidationBoundaries(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string

		change func(*Values)

		valid bool
	}{
		{"lower boundaries", func(v *Values) {
			v.Audio = Audio{}
			v.Display.Width = 1280
			v.Display.Height = 720
			v.Interface.CaptionOpacity = 90
		}, true},
		{"upper boundaries", func(v *Values) {
			v.Audio = Audio{Master: 100, Music: 100, Effects: 100, Ambience: 100, Vocal: 100, Mono: true, ReducedDynamicRange: true}
			v.Interface.TextSize = 150
			v.Interface.CaptionOpacity = 100
			v.Accessibility = Accessibility{Shake: ShakeOff, Blood: ParticlesReduced, ReducedMotion: true, HighContrast: true, HoldAssist: true}
			v.Display.Mode = Fullscreen
		}, true},
		{"windowed and medium text", func(v *Values) {
			v.Display.Mode = Windowed
			v.Interface.TextSize = 125
			v.Accessibility.Shake = ShakeFull
		}, true},
		{"negative master", func(v *Values) { v.Audio.Master = -1 }, false},
		{"music overflow", func(v *Values) { v.Audio.Music = 101 }, false},
		{"effects overflow", func(v *Values) { v.Audio.Effects = 101 }, false},
		{"ambience overflow", func(v *Values) { v.Audio.Ambience = 101 }, false},
		{"vocal overflow", func(v *Values) { v.Audio.Vocal = 101 }, false},
		{"bad mode", func(v *Values) { v.Display.Mode = "exclusive?" }, false},
		{"too small", func(v *Values) { v.Display.Width = 1279 }, false},
		{"too tall", func(v *Values) { v.Display.Height = 16385 }, false},
		{"bad text", func(v *Values) { v.Interface.TextSize = 110 }, false},
		{"opacity low", func(v *Values) { v.Interface.CaptionOpacity = 89 }, false},
		{"opacity high", func(v *Values) { v.Interface.CaptionOpacity = 101 }, false},
		{"bad shake", func(v *Values) { v.Accessibility.Shake = "huge" }, false},
		{"bad particles", func(v *Values) { v.Accessibility.Blood = "none" }, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			values := Defaults(ctx)
			test.change(&values)
			err := values.Validate(ctx)
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected invalid settings, got %v", err)
			}
		})
	}
}

func TestBindingsRemappingAndRecovery(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string

		change func(*Values)

		valid bool
	}{
		{"remap gameplay", func(v *Values) { v.Bindings[0].Control = "Z" }, true},
		{"reuse in another context", func(v *Values) {
			v.Bindings = append(v.Bindings, Binding{Context: Menu, Action: MenuSelect, Device: Keyboard, Control: "A"})
		}, true},
		{"reuse on another device", func(v *Values) {
			v.Bindings = append(v.Bindings, Binding{Context: Gameplay, Action: Interact, Device: Controller, Control: "LeftStickClick"})
		}, true},
		{"swap sticks", func(v *Values) {
			for i := range v.Bindings {
				if v.Bindings[i].Context != Gameplay || v.Bindings[i].Device != Controller {
					continue
				}
				switch v.Bindings[i].Control {
				case "RightStick":
					v.Bindings[i].Control = "LeftStick"
				case "LeftStickLeft":
					v.Bindings[i].Control = "RightStickLeft"
				case "LeftStickRight":
					v.Bindings[i].Control = "RightStickRight"
				case "LeftStickDown":
					v.Bindings[i].Control = "RightStickDown"
				}
			}
		}, true},
		{"overlapping stick", func(v *Values) {
			for i := range v.Bindings {
				if v.Bindings[i].Action == Aim && v.Bindings[i].Device == Controller {
					v.Bindings[i].Control = "LeftStick"
				}
			}
		}, false},
		{"non-vector aim", func(v *Values) {
			for i := range v.Bindings {
				if v.Bindings[i].Action == Aim && v.Bindings[i].Device == Controller {
					v.Bindings[i].Control = "LeftStickClick"
				}
			}
		}, false},
		{"missing controller action", func(v *Values) {
			v.Bindings = slices.DeleteFunc(v.Bindings, func(b Binding) bool { return b.Action == Throw && b.Device == Controller })
		}, false},
		{"missing keyboard action", func(v *Values) {
			v.Bindings = slices.DeleteFunc(v.Bindings, func(b Binding) bool { return b.Action == Melee && b.Device == Mouse })
		}, false},
		{"missing mouse selection", func(v *Values) {
			v.Bindings = slices.DeleteFunc(v.Bindings, func(b Binding) bool { return b.Action == MenuSelect && b.Device == Mouse })
			v.Bindings = append(v.Bindings, Binding{Context: Menu, Action: MenuSelect, Device: Keyboard, Control: "Z"})
		}, false},
		{"missing controller ledger tab", func(v *Values) {
			v.Bindings = slices.DeleteFunc(v.Bindings, func(b Binding) bool { return b.Action == LedgerNext && b.Device == Controller })
			v.Bindings = append(v.Bindings, Binding{Context: LedgerMenu, Action: LedgerNext, Device: Keyboard, Control: "Z"})
		}, false},
		{"duplicate different action", func(v *Values) {
			v.Bindings = append(v.Bindings, Binding{Context: Gameplay, Action: Jump, Device: Keyboard, Control: "A"})
		}, false},
		{"duplicate same action", func(v *Values) { v.Bindings = append(v.Bindings, v.Bindings[0]) }, false},
		{"missing escape", func(v *Values) {
			v.Bindings = slices.DeleteFunc(v.Bindings, func(b Binding) bool { return b.Context == Menu && b.Device == Keyboard && b.Control == "Escape" })
		}, false},
		{"missing controller back", func(v *Values) {
			v.Bindings = slices.DeleteFunc(v.Bindings, func(b Binding) bool { return b.Context == Menu && b.Device == Controller && b.Control == "B" })
		}, false},
		{"steal escape", func(v *Values) {
			for i := range v.Bindings {
				if v.Bindings[i].Context == Menu && v.Bindings[i].Control == "Escape" {
					v.Bindings[i].Action = MenuConfirm
				}
			}
		}, false},
		{"unknown key", func(v *Values) { v.Bindings[0].Control = "not-a-key" }, false},
		{"unknown device", func(v *Values) { v.Bindings[0].Device = "touch" }, false},
		{"wrong context", func(v *Values) { v.Bindings[0].Context = Menu }, false},
		{"missing action", func(v *Values) {
			v.Bindings = slices.DeleteFunc(v.Bindings, func(b Binding) bool { return b.Action == Regenerate })
		}, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			values := Defaults(ctx)
			test.change(&values)
			err := values.Validate(ctx)
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected invalid bindings, got %v", err)
			}
		})
	}
}

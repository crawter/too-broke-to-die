// Copyright (C) 2026 crawter
// All rights reserved.

package input

import (
	"context"

	"too-broke-to-die/internal/tbtd/settings"
)

// Token is a presentation-independent key label or controller glyph identity.
// Control is the exact portable binding token, so presentation can resolve a
// glyph consistently in menus, dialogue, tutorials and settings.
type Token struct {
	Device  settings.Device
	Control string
	Label   string
}

// Tokens returns all current bindings for the requested device family in
// settings order. Keyboard and mouse are one family for gameplay prompts.
func (sampler *Sampler) Tokens(ctx context.Context, action settings.Action, device settings.Device) []Token {
	defer mon.Task()(&ctx)(nil)
	var tokens []Token
	if device != settings.Keyboard && device != settings.Mouse && device != settings.Controller {
		return nil
	}
	for _, binding := range sampler.bindings {
		if binding.Action != action || (device == settings.Controller) != (binding.Device == settings.Controller) {
			continue
		}
		label := binding.Control
		if binding.Device == settings.Keyboard {
			switch binding.Control {
			case "Left":
				label = "←"
			case "Right":
				label = "→"
			case "Up":
				label = "↑"
			case "Down":
				label = "↓"
			case "Space":
				label = "Пробел"
			case "Escape":
				label = "Esc"
			case "LeftShift":
				label = "Левый Shift"
			case "RightShift":
				label = "Правый Shift"
			case "LeftControl":
				label = "Левый Ctrl"
			case "RightControl":
				label = "Правый Ctrl"
			case "LeftAlt":
				label = "Левый Alt"
			case "RightAlt":
				label = "Правый Alt"
			}
		}
		if binding.Device == settings.Mouse {
			switch binding.Control {
			case "Left":
				label = "ЛКМ"
			case "Right":
				label = "ПКМ"
			case "Middle":
				label = "СКМ"
			case "Wheel":
				label = "Колесо мыши"
			case "Motion":
				label = "Мышь"
			}
		}
		tokens = append(tokens, Token{Device: binding.Device, Control: binding.Control, Label: label})
	}
	return tokens
}

// ActiveDevice identifies the most recent meaningful input, not idle noise.
func (sampler *Sampler) ActiveDevice() settings.Device { return sampler.active }

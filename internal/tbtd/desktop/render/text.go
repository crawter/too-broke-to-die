// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

// wrapText uses the final font's measured width. Long words are split at rune
// boundaries rather than shrunk or clipped. Newlines remain explicit breaks.
func wrapText(text string, width float64, measure func(string) float64) ([]string, error) {
	if width <= 0 || measure == nil {
		return nil, content.ErrInvalid.New("invalid text layout")
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		var line string
		for _, word := range strings.FieldsFunc(paragraph, unicode.IsSpace) {
			candidate := word
			if line != "" {
				candidate = line + " " + word
			}
			if measure(candidate) <= width {
				line = candidate
				continue
			}
			if line != "" {
				lines = append(lines, line)
				line = ""
			}
			for _, r := range word {
				candidate = line + string(r)
				if measure(candidate) > width {
					if line == "" {
						return nil, content.ErrInvalid.New("text glyph exceeds panel width")
					}
					lines = append(lines, line)
					line = string(r)
					if measure(line) > width {
						return nil, content.ErrInvalid.New("text glyph exceeds panel width")
					}
				} else {
					line = candidate
				}
			}
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func (ui *Interface) text(ctx context.Context, id string, values map[string]string) (string, error) {
	return ui.texts.Render(ctx, id, values)
}

// Prompt resolves an authored template using the sampler's current bindings
// and active device. The map binds grammatical field names to semantic actions.
func (ui *Interface) Prompt(ctx context.Context, id string, fields map[string]settings.Action) (_ string, err error) {
	defer mon.Task()(&ctx)(&err)
	if ui == nil || ui.sampler == nil {
		return "", content.ErrInvalid.New("missing prompt bindings")
	}
	values := make(map[string]string, len(fields))
	for name, action := range fields {
		tokens := ui.sampler.Tokens(ctx, action, ui.sampler.ActiveDevice())
		if len(tokens) == 0 {
			return "", content.ErrInvalid.Wrap(fmt.Errorf("%s: missing binding for %s", id, action))
		}
		labels := make([]string, len(tokens))
		for i, token := range tokens {
			labels[i] = token.Label
		}
		values[name] = strings.Join(labels, " / ")
	}
	return ui.text(ctx, id, values)
}

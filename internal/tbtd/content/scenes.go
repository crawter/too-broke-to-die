// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Scene is an authored sequence associated with exactly one narrative or ending
// entry. Its units are ordered; neither the scene nor its presentation grants
// campaign rewards or changes gates.
type Scene struct {
	ID string `json:"id"`

	Narrative NarrativeID `json:"narrative,omitempty"`

	Ending EndingID `json:"ending,omitempty"`

	Units []SceneUnit `json:"units"`
}

// SceneUnit is a player-advanced text and its presentation references. A unit
// identity is stable across revisions; changing its text does not rename it.
type SceneUnit struct {
	ID string `json:"id"`

	Text string `json:"text"`

	Speaker string `json:"speaker,omitempty"`

	TextID PresentationID `json:"text_id"`

	Background PresentationID `json:"background,omitempty"`

	Direction string `json:"direction,omitempty"`
}

func validateScenes(ctx context.Context, scenes []Scene, narratives map[NarrativeID]Narrative, endings map[EndingID]Ending, presentations map[PresentationID]Presentation) (err error) {
	defer mon.Task()(&ctx)(&err)
	seenScenes := make(map[string]bool)
	seenUnits := make(map[string]bool)
	seenNarratives := make(map[NarrativeID]bool)
	seenEndings := make(map[EndingID]bool)
	for _, scene := range scenes {
		if !validID(scene.ID, "SCN") || seenScenes[scene.ID] || len(scene.Units) == 0 || (scene.Narrative == "") == (scene.Ending == "") {
			return fmt.Errorf("invalid or duplicate scene %q", scene.ID)
		}
		seenScenes[scene.ID] = true
		if scene.Narrative != "" {
			if seenNarratives[scene.Narrative] {
				return fmt.Errorf("duplicate scene for %s", scene.Narrative)
			}
			seenNarratives[scene.Narrative] = true
			if err = require(scene.ID, "narrative", scene.Narrative, narratives); err != nil {
				return err
			}
		} else {
			if seenEndings[scene.Ending] {
				return fmt.Errorf("duplicate scene for %s", scene.Ending)
			}
			seenEndings[scene.Ending] = true
			if err = require(scene.ID, "ending", scene.Ending, endings); err != nil {
				return err
			}
		}
		for _, unit := range scene.Units {
			if !validID(unit.ID, "UNIT") || seenUnits[unit.ID] || strings.TrimSpace(unit.Text) == "" || !utf8.ValidString(unit.Text) || !utf8.ValidString(unit.Speaker) || !utf8.ValidString(unit.Direction) {
				return fmt.Errorf("%s: invalid or duplicate unit %q", scene.ID, unit.ID)
			}
			seenUnits[unit.ID] = true
			if err = require(unit.ID, "text", unit.TextID, presentations); err != nil {
				return err
			}
			if err = optional(unit.ID, "background", unit.Background, presentations); err != nil {
				return err
			}
		}
	}
	return nil
}

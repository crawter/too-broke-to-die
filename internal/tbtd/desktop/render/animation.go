// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
)

// playerClip selects from the current state, never from interpolated state.
// Interruptions therefore replace the old clip on the very next view.
func playerClip(player session.PlayerView) content.PresentationID {
	switch player.Action {
	case "dead":
		return "AN-GRI-DEAD"
	case "hurt":
		return "AN-GRI-HURT"
	case "evade":
		return "AN-GRI-EVADE"
	case "remove":
		if (player.SelectedSocket == "LIM-LA" && !player.Limbs[1]) || (player.SelectedSocket == "LIM-RA" && !player.Limbs[0]) {
			return "AN-GRI-TEETH"
		}
		return "AN-GRI-REMOVE"
	case "interact":
		switch player.Channel {
		case "regenerate":
			return "AN-GRI-REGEN"
		case "replace":
			return "AN-GRI-REPLACE"
		default:
			return "AN-GRI-INTERACT"
		}
	case "throw":
		return "AN-GRI-THROW"
	case "melee":
		if !player.Limbs[0] && !player.Limbs[1] {
			return "AN-GRI-MELEE-HEADBUTT"
		}
		return "AN-GRI-MELEE"
	}
	if !player.Grounded {
		return "AN-GRI-JUMP"
	}
	if player.Velocity.X == 0 {
		return "AN-GRI-IDLE"
	}
	legs := 0
	if player.Limbs[2] {
		legs++
	}
	if player.Limbs[3] {
		legs++
	}
	switch legs {
	case 0:
		return "AN-GRI-CRAWL"
	case 1:
		return "AN-GRI-HOP"
	default:
		return "AN-GRI-RUN"
	}
}

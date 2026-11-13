// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
)

// hospitalConveyors points inward, away from the protected alcoves and the
// arena edges. Their geometry matches the authored encounter features.
func hospitalConveyors(phase int) []Rect {
	var strips []Rect
	if phase >= 1 {
		strips = append(strips, Rect{X: 240, Y: 0, Width: 240, Height: 8})
	}
	if phase >= 2 {
		strips = append(strips, Rect{X: 800, Y: 0, Width: 240, Height: 8})
	}
	return strips
}

func (arena *BossArena) carryConveyor(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	world := arena.world
	if world.boss.definition.Encounter != "ENC-HOS-05" || world.boss.stage == BossPending || world.boss.stage == BossDefeated || world.state.Player.Support != "boundary-floor" || world.state.Player.Velocity.Y > 0 {
		return nil
	}
	body := world.state.Player.Bounds
	for index, strip := range hospitalConveyors(world.boss.phase) {
		if centerX(body) < strip.X || centerX(body) >= strip.X+strip.Width {
			continue
		}
		direction := 1.0
		if index == 1 {
			direction = -1
		}
		result, moveErr := world.grid.Move(ctx, body, Vector{X: direction * 60 * TickSeconds}, platformColliders(world.state.Platforms), "")
		if moveErr != nil {
			return moveErr
		}
		world.state.Player.Bounds = result.Bounds
		break
	}
	return nil
}

// moveHospitalRig keeps the extraction rig at a floor-reachable height while
// carrying it between attack-specific positions. The visible target and its
// authoritative hitbox move together, including during exposed recovery.
func (arena *BossArena) moveHospitalRig() {
	boss := &arena.world.boss
	if boss.definition.Encounter != "ENC-HOS-05" || (boss.stage != BossActive && boss.stage != BossRecovery) {
		return
	}
	target := 600.0
	switch boss.attack().ID {
	case "HOS-A":
		target = 576
	case "HOS-B":
		target = 624
	}
	boss.bounds.X = approach(boss.bounds.X, target, 80*TickSeconds)
}

// hospitalTrolley gives the trolley's position at an active-tick boundary.
// Contact is swept relative to player movement by the arena executor.
func hospitalTrolley(marker Vector, elapsed int) Rect {
	x := min(1125, 112+float64(elapsed)*3)
	if marker.X >= 640 {
		x = max(112, 1125-float64(elapsed)*3)
	}
	return Rect{X: x, Y: 0, Width: 43, Height: 48}
}

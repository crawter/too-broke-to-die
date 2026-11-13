// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"

	"too-broke-to-die/internal/tbtd/content"
)

// The gantry and its stairs are authored one-way platforms. The waste volume
// is independent of the player's feces projectiles and never rises with time.
func (arena *BossArena) sumpHit(from, to Rect) (DamageHit, bool) {
	world := arena.world
	height := world.boss.definition.Phases[world.boss.phase].WasteHeight
	waste := Rect{X: 0, Y: 0, Width: 1280, Height: float64(height)}
	if world.protected(from) || world.protected(to) || !sweptOverlap(from, to, waste) {
		return DamageHit{}, false
	}
	return DamageHit{Attacker: "ministry-sump", Attack: fmt.Sprintf("waste-%d", world.state.Tick), Kind: OrdinaryDamage, Amount: 15, Direction: 1}, true
}

func ministryMarkers(attack content.BossAttack, player Rect) []Vector {
	x := centerX(player)
	switch attack.ID {
	case "MIN-A":
		return []Vector{{X: x, Y: centerY(player)}}
	case "MIN-B":
		return []Vector{{X: math.Max(192, math.Min(1088, x))}}
	case "MIN-C":
		if x < 640 {
			return []Vector{{X: 0}}
		}
		return []Vector{{X: 1}}
	}
	return nil
}

func ministryWarnings(attack content.BossAttack, markers []Vector) []Rect {
	var areas []Rect
	for _, marker := range markers {
		switch attack.ID {
		case "MIN-A":
			areas = append(areas, Rect{X: marker.X - 20, Y: marker.Y - 20, Width: 40, Height: 40})
		case "MIN-B":
			areas = append(areas, Rect{X: marker.X - 80, Y: 160, Width: 160, Height: 400})
		case "MIN-C":
			if marker.X == 0 {
				areas = append(areas, Rect{X: 112, Y: 160, Width: 528, Height: 80})
			} else {
				areas = append(areas, Rect{X: 640, Y: 160, Width: 528, Height: 80})
			}
		}
	}
	return areas
}

// trajectoryHits advances actual swept projectiles, not player-following hit
// scans. Each shot is bounded by its first terrain, protected-area or player
// contact. At phase thresholds the arena removes the entire sequence.
func (arena *BossArena) trajectoryHits(ctx context.Context, from, to Rect) (_ []DamageHit, err error) {
	defer mon.Task()(&ctx)(&err)
	world, boss := arena.world, &arena.world.boss
	attack := boss.attack()
	// Give the player time to move even when standing against the lift at
	// lock; the three releases still remain exactly 250 ms apart.
	if attack.ID == "MIN-A" && boss.elapsed >= 30 && (boss.elapsed-30)%msTicks(attack.ReleaseIntervalMS) == 0 {
		index := (boss.elapsed - 30) / msTicks(attack.ReleaseIntervalMS)
		if index < attack.ReleaseCount {
			origin := Vector{X: centerX(boss.bounds), Y: boss.bounds.Y + 55}
			dx, dy := boss.locked[0].X-origin.X, boss.locked[0].Y-origin.Y
			length := math.Hypot(dx, dy)
			if length < 1 {
				dx, dy, length = -1, 0, 1
			}
			arena.projectiles = append(arena.projectiles, ArenaProjectile{ID: fmt.Sprintf("MIN-A-%d-%d", boss.recovery, index), Position: origin, Velocity: Vector{X: 720 * dx / length, Y: 720 * dy / length}, Remaining: 240})
		}
	}
	if attack.ID == "INS-C" && boss.elapsed == 0 {
		for index, marker := range boss.locked {
			arena.projectiles = append(arena.projectiles, ArenaProjectile{ID: fmt.Sprintf("INS-C-%d-%d", boss.recovery, index), Position: Vector{X: marker.X, Y: 320}, Velocity: Vector{Y: -360}, Remaining: 120})
		}
	}
	var hits []DamageHit
	remaining := make([]ArenaProjectile, 0, len(arena.projectiles))
	for _, shot := range arena.projectiles {
		delta := Vector{X: shot.Velocity.X * TickSeconds, Y: shot.Velocity.Y * TickSeconds}
		dynamic := platformColliders(world.state.Platforms)
		dynamic = append(dynamic,
			Collider{ID: "arena-left-projectile-boundary", Kind: ProjectileTarget, Bounds: Rect{X: 111, Y: 0, Width: 1, Height: 560}},
			Collider{ID: "arena-right-projectile-boundary", Kind: ProjectileTarget, Bounds: Rect{X: 1168, Y: 0, Width: 1, Height: 560}},
		)
		for index, area := range world.combat.protection {
			dynamic = append(dynamic, Collider{ID: fmt.Sprintf("arena-protection-%d", index), Kind: ProjectileTarget, Bounds: area})
		}
		terrain, blocked, sweepErr := world.grid.Sweep(ctx, Rect{X: shot.Position.X, Y: shot.Position.Y}, delta, dynamic, SweepOptions{Projectile: true})
		if sweepErr != nil {
			return nil, sweepErr
		}
		relative := Vector{X: delta.X - (to.X - from.X), Y: delta.Y - (to.Y - from.Y)}
		contact, hit := impactTime(Rect{X: shot.Position.X, Y: shot.Position.Y}, relative, Collider{ID: "player", Kind: ProjectileTarget, Bounds: from})
		if hit && !world.protected(from) && !world.protected(to) && (!blocked || contact.Time < terrain.Time) {
			direction := 1
			if shot.Velocity.X < 0 || (shot.Velocity.X == 0 && centerX(to) < centerX(boss.bounds)) {
				direction = -1
			}
			hits = append(hits, DamageHit{Attacker: string(boss.definition.Encounter), Attack: shot.ID, Kind: OrdinaryDamage, Amount: attack.Damage, Direction: direction})
			continue
		}
		shot.Remaining--
		if !blocked && shot.Remaining > 0 {
			shot.Position.X += delta.X
			shot.Position.Y += delta.Y
			remaining = append(remaining, shot)
		}
	}
	arena.projectiles = remaining
	return hits, nil
}

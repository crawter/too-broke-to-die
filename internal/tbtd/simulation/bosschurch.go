// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
)

// confessionHits advances one ordered horizontal release sequence against the
// actual terrain and protected areas. All three travel in the same direction,
// leaving the opposite floor escape open.
func (arena *BossArena) confessionHits(ctx context.Context, from, to Rect) (_ []DamageHit, err error) {
	defer mon.Task()(&ctx)(&err)
	world, boss := arena.world, &arena.world.boss
	attack := boss.attack()
	if boss.elapsed%msTicks(attack.ReleaseIntervalMS) == 0 {
		index := boss.elapsed / msTicks(attack.ReleaseIntervalMS)
		if index < len(attack.ProjectileHeights) {
			x, speed := 112.0+Tolerance, 360.0
			if boss.locked[0].X >= 640 {
				x, speed = 1168-Tolerance, -360
			}
			arena.projectiles = append(arena.projectiles, ArenaProjectile{
				ID:       fmt.Sprintf("CHU-C-%d-%d", boss.recovery, index),
				Position: Vector{X: x, Y: float64(attack.ProjectileHeights[index])},
				Velocity: Vector{X: speed},
			})
		}
	}
	var hits []DamageHit
	remaining := make([]ArenaProjectile, 0, len(arena.projectiles))
	for _, shot := range arena.projectiles {
		delta := Vector{X: shot.Velocity.X * TickSeconds}
		dynamic := platformColliders(world.state.Platforms)
		// The loft's protected alcove boundaries stop projectiles at every
		// height, including the 180-pixel shot above the refill volumes.
		dynamic = append(dynamic,
			Collider{ID: "confession-left-boundary", Kind: ProjectileTarget, Bounds: Rect{X: 111, Y: 0, Width: 1, Height: 560}},
			Collider{ID: "confession-right-boundary", Kind: ProjectileTarget, Bounds: Rect{X: 1168, Y: 0, Width: 1, Height: 560}},
		)
		for index, area := range world.combat.protection {
			dynamic = append(dynamic, Collider{ID: fmt.Sprintf("arena-protection-%d", index), Kind: ProjectileTarget, Bounds: area})
		}
		terrain, blocked, sweepErr := world.grid.Sweep(ctx, Rect{X: shot.Position.X, Y: shot.Position.Y}, delta, dynamic, SweepOptions{Projectile: true})
		if sweepErr != nil {
			return nil, sweepErr
		}
		relative := Vector{X: delta.X - (to.X - from.X), Y: -(to.Y - from.Y)}
		contact, hit := impactTime(Rect{X: shot.Position.X, Y: shot.Position.Y}, relative, Collider{ID: "player", Kind: ProjectileTarget, Bounds: from})
		if hit && !world.protected(from) && !world.protected(to) && (!blocked || contact.Time < terrain.Time) {
			direction := 1
			if shot.Velocity.X < 0 {
				direction = -1
			}
			hits = append(hits, DamageHit{Attacker: string(boss.definition.Encounter), Attack: shot.ID, Kind: OrdinaryDamage, Amount: attack.Damage, Direction: direction})
			continue
		}
		if !blocked {
			shot.Position.X += delta.X
			remaining = append(remaining, shot)
		}
	}
	arena.projectiles = remaining
	return hits, nil
}

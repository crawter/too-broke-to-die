// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
)

// Projectile is a live swept projectile. An impact consumes it exactly once.
type Projectile struct {
	ID          string
	Attacker    string
	Attack      string
	Position    Vector
	Velocity    Vector
	PlayerOwned bool
}

func (world *World) launchPlayerProjectile(ctx context.Context, attack PlayerAttack) {
	defer mon.Task()(&ctx)(nil)
	if world.releaseLimbProjectile(ctx, attack) {
		return
	}
	direction := world.combat.aim
	if direction == (Vector{}) {
		direction = Vector{X: float64(attack.Facing)}
	}
	body := world.state.Player.Bounds
	id := fmt.Sprintf("player-%d", attack.ID)
	world.combat.projectiles = append(world.combat.projectiles, Projectile{ID: id, Attacker: "player", Attack: fmt.Sprint(attack.ID), Position: Vector{X: centerX(body), Y: centerY(body)}, Velocity: Vector{X: direction.X * 600, Y: direction.Y * 600}, PlayerOwned: true})
}

func (world *World) launchEnemyProjectile(enemy Enemy) {
	player, source := world.state.Player.Bounds, enemy.Body.Bounds
	dx, dy := centerX(player)-centerX(source), centerY(player)-centerY(source)
	length := math.Hypot(dx, dy)
	if length == 0 {
		dx, dy, length = float64(enemy.Facing), 0, 1
	}
	attack := fmt.Sprint(enemy.AttackID)
	world.combat.projectiles = append(world.combat.projectiles, Projectile{ID: enemy.ID + "-" + attack, Attacker: enemy.ID, Attack: attack, Position: Vector{X: centerX(source), Y: centerY(source)}, Velocity: Vector{X: dx * 360 / length, Y: dy * 360 / length}})
}

func (world *World) advanceProjectiles(ctx context.Context, playerOwned bool) (_ []DamageHit, err error) {
	defer mon.Task()(&ctx)(&err)
	var hits []DamageHit
	if playerOwned && world.limbs.generation != 0 {
		if !world.limbs.inTick {
			return nil, fmt.Errorf("limb-enabled worlds require StepLimbs")
		}
		if err = world.advanceLimbActions(ctx); err != nil {
			return nil, err
		}
		if err = world.advanceLimbProjectiles(ctx); err != nil {
			return nil, err
		}
	}
	remaining := make([]Projectile, 0, len(world.combat.projectiles))
	for _, projectile := range world.combat.projectiles {
		if projectile.PlayerOwned != playerOwned {
			remaining = append(remaining, projectile)
			continue
		}
		if projectile.PlayerOwned {
			projectile.Velocity.Y -= 1400 * TickSeconds
		}
		delta := Vector{X: projectile.Velocity.X * TickSeconds, Y: projectile.Velocity.Y * TickSeconds}
		var targets []Collider
		for index, area := range world.combat.protection {
			if validRect(area, false) {
				targets = append(targets, Collider{ID: fmt.Sprintf("combat-protection:%d", index), Kind: ProjectileTarget, Bounds: area})
			}
		}
		if projectile.PlayerOwned {
			for _, enemy := range world.combat.enemies {
				if enemy.Health > 0 {
					targets = append(targets, Collider{ID: "combat-enemy:" + enemy.ID, Kind: ProjectileTarget, Bounds: enemy.Body.Bounds})
				}
			}
			for _, civilian := range world.combat.civilians {
				if !civilian.Incapacitated {
					targets = append(targets, Collider{ID: "combat-civilian:" + civilian.ID, Kind: ProjectileTarget, Bounds: civilian.Bounds})
				}
			}
		} else if !world.motion.dead && !world.protected(world.state.Player.Bounds) {
			targets = append(targets, Collider{ID: "combat-player", Kind: ProjectileTarget, Bounds: world.state.Player.Bounds})
		}
		impact, found, sweepErr := world.SweepTargets(ctx, Rect{X: projectile.Position.X, Y: projectile.Position.Y}, delta, targets, SweepOptions{Projectile: true})
		if sweepErr != nil {
			return nil, sweepErr
		}
		if !found {
			projectile.Position.X += delta.X
			projectile.Position.Y += delta.Y
			remaining = append(remaining, projectile)
			continue
		}
		if impact.Collider.ID == "combat-player" {
			direction := 1
			if projectile.Velocity.X < 0 {
				direction = -1
			}
			hits = append(hits, DamageHit{Attacker: projectile.Attacker, Attack: projectile.Attack, Kind: OrdinaryDamage, Amount: 18, Direction: direction})
		}
		for i := range world.combat.enemies {
			enemy := &world.combat.enemies[i]
			if impact.Collider.ID == "combat-enemy:"+enemy.ID {
				direction := 1
				if projectile.Velocity.X < 0 {
					direction = -1
				}
				world.applyEnemyHit(enemy, DamageHit{Attacker: projectile.Attacker, Attack: projectile.Attack, Kind: OrdinaryDamage, Amount: 8, Direction: direction})
			}
		}
		for i := range world.combat.civilians {
			civilian := &world.combat.civilians[i]
			if impact.Collider.ID == "combat-civilian:"+civilian.ID {
				civilian.Incapacitated = true
			}
		}
	}
	world.combat.projectiles = remaining
	return hits, nil
}

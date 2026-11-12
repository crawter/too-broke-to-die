// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"slices"
)

// EnemyKind identifies the ordinary attack contract.
type EnemyKind string

const (
	// MeleeEnemy approaches and strikes the player at close range.
	MeleeEnemy EnemyKind = "MELEE"
	// RangedEnemy maintains distance and launches projectiles.
	RangedEnemy EnemyKind = "RANGED"
	// FollowerEnemy uses the institute follower's ordinary melee behavior.
	FollowerEnemy EnemyKind = "FOLLOWER"
)

// EnemyPhase describes the telegraph and recovery visible to the player.
type EnemyPhase string

const (
	// EnemyApproach seeks the enemy's preferred distance from the player.
	EnemyApproach EnemyPhase = "approach"
	// EnemyTelegraph warns of the pending attack before it releases.
	EnemyTelegraph EnemyPhase = "telegraph"
	// EnemyRecovery follows an attack and permits retaliation.
	EnemyRecovery EnemyPhase = "recovery"
	// EnemyHurt prevents attack initiation while hurt stun lasts.
	EnemyHurt EnemyPhase = "hurt"
	// EnemyDefeated is harmless and has no blocking body.
	EnemyDefeated EnemyPhase = "defeated"
)

// Enemy is a detached ordinary actor state. Corpses are never colliders.
type Enemy struct {
	ID          string
	Kind        EnemyKind
	Body        Body
	Health      int
	Phase       EnemyPhase
	Until       uint64
	HurtUntil   uint64
	ImmuneUntil uint64
	Facing      int
	AttackID    uint64
	StrikeTick  uint64
}

// AddEnemy installs an authored enemy with its default full health.
func (world *World) AddEnemy(ctx context.Context, id string, kind EnemyKind, bounds Rect) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil || id == "" || (kind != MeleeEnemy && kind != RangedEnemy && kind != FollowerEnemy) || !validRect(bounds, false) {
		return fmt.Errorf("invalid ordinary enemy")
	}
	for _, enemy := range world.combat.enemies {
		if enemy.ID == id {
			return fmt.Errorf("duplicate enemy %s", id)
		}
	}
	for _, civilian := range world.combat.civilians {
		if civilian.ID == id {
			return fmt.Errorf("duplicate combat actor %s", id)
		}
	}
	for _, collider := range world.grid.colliders {
		if collider.ID == id || ((collider.Kind == Solid || collider.Kind == PlayerOnly) && overlaps(bounds, collider.Bounds)) {
			return fmt.Errorf("enemy overlaps or duplicates %s", collider.ID)
		}
	}
	world.combat.enemies = append(world.combat.enemies, Enemy{ID: id, Kind: kind, Body: Body{ID: id, Bounds: bounds}, Health: 60, Phase: EnemyApproach, Facing: -1})
	slices.SortFunc(world.combat.enemies, func(a, b Enemy) int { return compareID(a.ID, b.ID) })
	return nil
}

func centerX(box Rect) float64 { return box.X + box.Width/2 }
func centerY(box Rect) float64 { return box.Y + box.Height/2 }

func (world *World) protected(box Rect) bool {
	for _, area := range world.combat.protection {
		if overlaps(box, area) {
			return true
		}
	}
	return false
}

func (world *World) advanceEnemies(ctx context.Context, visible Rect) (err error) {
	defer mon.Task()(&ctx)(&err)
	for i := range world.combat.enemies {
		enemy := &world.combat.enemies[i]
		if enemy.Health <= 0 {
			continue
		}
		tick := world.state.Tick
		if enemy.Phase == EnemyHurt && tick >= enemy.Until {
			enemy.Phase = EnemyApproach
		}
		if enemy.Phase == EnemyRecovery && tick >= enemy.Until {
			enemy.Phase = EnemyApproach
		}
		if enemy.Phase == EnemyTelegraph && tick >= enemy.Until {
			enemy.StrikeTick = tick
			enemy.Phase = EnemyRecovery
			if enemy.Kind == RangedEnemy {
				enemy.Until = tick + 108
			} else {
				enemy.Until = tick + 78
			}
		}
		player := world.state.Player.Bounds
		dx := centerX(player) - centerX(enemy.Body.Bounds)
		if enemy.Phase == EnemyApproach {
			if dx < 0 {
				enemy.Facing = -1
			} else {
				enemy.Facing = 1
			}
			distance := math.Abs(dx)
			separation := math.Max(0, distance-(player.Width+enemy.Body.Bounds.Width)/2)
			canSee := overlaps(visible, enemy.Body.Bounds) && overlaps(visible, player) && !world.protected(enemy.Body.Bounds) && !world.protected(player)
			inRange := separation <= 44 && math.Abs(centerY(player)-centerY(enemy.Body.Bounds)) < 48
			if enemy.Kind == RangedEnemy {
				inRange = distance >= 240 && distance <= 360
			}
			if canSee && inRange && !world.motion.dead {
				enemy.AttackID++
				enemy.Phase = EnemyTelegraph
				if enemy.Kind == RangedEnemy {
					enemy.Until = tick + 78
				} else {
					enemy.Until = tick + 60
				}
			}
		}
		velocity := enemy.Body.Velocity
		if enemy.Phase != EnemyHurt {
			velocity.X = 0
			if enemy.Phase == EnemyApproach && !world.protected(enemy.Body.Bounds) {
				distance := math.Abs(dx)
				if enemy.Kind == RangedEnemy {
					if distance > 360 {
						velocity.X = float64(enemy.Facing) * 110
					} else if distance < 240 {
						velocity.X = -float64(enemy.Facing) * 110
					}
				} else if distance-(player.Width+enemy.Body.Bounds.Width)/2 > 44 {
					velocity.X = float64(enemy.Facing) * 150
				}
			}
		}
		velocity.Y = math.Max(-1000, velocity.Y-2200*TickSeconds)
		// Resolve the two axes separately with the same swept solver. This also
		// avoids a rounding-sized floor overlap during a diagonal landing.
		for _, delta := range []Vector{{X: velocity.X * TickSeconds}, {Y: velocity.Y * TickSeconds}} {
			result, moveErr := world.grid.Move(ctx, enemy.Body.Bounds, delta, platformColliders(world.state.Platforms), "")
			if moveErr != nil {
				return fmt.Errorf("enemy %s at tick %d: %w", enemy.ID, tick, moveErr)
			}
			if !world.protected(result.Bounds) {
				enemy.Body.Bounds = result.Bounds
			}
			for _, contact := range result.Contacts {
				if contact.Normal.X != 0 {
					velocity.X = 0
				}
				if contact.Normal.Y != 0 {
					velocity.Y = 0
				}
			}
		}
		enemy.Body.Velocity = velocity
	}
	return nil
}

func (world *World) enemyHits(ctx context.Context) (_ []DamageHit, err error) {
	defer mon.Task()(&ctx)(&err)
	var hits []DamageHit
	player := world.state.Player.Bounds
	if world.protected(player) {
		return hits, nil
	}
	for _, enemy := range world.combat.enemies {
		if enemy.Health <= 0 || enemy.Kind == RangedEnemy || enemy.StrikeTick != world.state.Tick || enemy.Phase != EnemyRecovery {
			continue
		}
		body := enemy.Body.Bounds
		area := Rect{X: body.X + body.Width, Y: body.Y + (body.Height-36)/2, Width: 48, Height: 36}
		if enemy.Facing < 0 {
			area.X = body.X - 48
		}
		if world.protected(area) || !overlaps(area, player) {
			continue
		}
		clear, err := world.strikeClear(ctx, body, area, player, enemy.Facing)
		if err != nil {
			return nil, err
		}
		if clear {
			hits = append(hits, DamageHit{Attacker: enemy.ID, Attack: fmt.Sprint(enemy.AttackID), Kind: OrdinaryDamage, Amount: 18, Direction: enemy.Facing})
		}
	}
	return hits, nil
}

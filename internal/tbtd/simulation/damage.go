// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"slices"
)

// DamageKind distinguishes ordinary impacts from immunity-bypassing death.
type DamageKind string

const (
	// OrdinaryDamage is blocked by ordinary post-hit or evade immunity.
	OrdinaryDamage DamageKind = "ordinary"
	// LethalDamage bypasses ordinary immunity and ends the player's life.
	LethalDamage DamageKind = "lethal"
)

// DamageHit carries stable attacker and attack identities. Direction is the
// horizontal knockback direction, either -1 or +1. An event's direct and
// splash contacts must share its attack identity. Each ordinary attack identity
// may contact a target only once, even if immunity rejected it. Direct contact
// wins over splash when both describe the same event in one tick.
type DamageHit struct {
	Attacker  string
	Attack    string
	Kind      DamageKind
	Amount    int
	Direction int
	Splash    bool
}

// HitOutcome distinguishes accepted impacts, immunity and duplicate contacts.
type HitOutcome struct {
	Hit    DamageHit
	Target string
	Result string
}

const (
	// HitAccepted means the impact applied its damage and reaction.
	HitAccepted = "accepted"
	// HitImmune means ordinary immunity rejected the impact.
	HitImmune = "immune"
	// HitDuplicate means the same attack event already contacted this target.
	HitDuplicate = "duplicate"
	// HitAfterDeath means the target had already died before resolution.
	HitAfterDeath = "after_death"
)

func validHits(hits []DamageHit) bool {
	for _, hit := range hits {
		if hit.Attacker == "" || hit.Attack == "" || (hit.Kind != OrdinaryDamage && hit.Kind != LethalDamage) || hit.Amount <= 0 || (hit.Direction != -1 && hit.Direction != 1) {
			return false
		}
	}
	return true
}

func (world *World) resolvePlayerHits(ctx context.Context, hits []DamageHit) {
	defer mon.Task()(&ctx)(nil)
	ordered := slices.Clone(hits)
	slices.SortStableFunc(ordered, func(a, b DamageHit) int {
		// Lethal events resolve after ordinary damage and are never suppressed.
		if a.Kind != b.Kind {
			if a.Kind == OrdinaryDamage {
				return -1
			}
			return 1
		}
		if order := compareID(a.Attacker, b.Attacker); order != 0 {
			return order
		}
		if order := compareID(a.Attack, b.Attack); order != 0 {
			return order
		}
		if a.Splash != b.Splash {
			if a.Splash {
				return 1
			}
			return -1
		}
		if a.Amount != b.Amount {
			return b.Amount - a.Amount
		}
		return a.Direction - b.Direction
	})
	seen := make(map[[3]string]bool)
	for _, hit := range ordered {
		key := [3]string{hit.Attacker, hit.Attack, string(hit.Kind)}
		targetKey := [3]string{hit.Attacker, hit.Attack, "player"}
		result := HitAccepted
		switch {
		case seen[key] || (hit.Kind == OrdinaryDamage && world.combat.receivedHits[targetKey]):
			result = HitDuplicate
		case world.motion.dead:
			result = HitAfterDeath
		case hit.Kind == OrdinaryDamage && (world.state.Tick < world.combat.immuneUntil || world.motion.immune):
			result = HitImmune
		case hit.Kind == LethalDamage:
			world.state.Health = 0
			world.motion.dead = true
			world.motion.action = DeathAction
			world.motion.buffer = ""
		case hit.Kind == OrdinaryDamage:
			world.state.Health = max(0, world.state.Health-hit.Amount)
			world.state.Player.Velocity = Vector{X: float64(hit.Direction) * 260, Y: 220}
			world.motion.action = HurtAction
			world.motion.actionUntil = world.state.Tick + 22
			world.combat.immuneUntil = world.state.Tick + 96
			if world.state.Health == 0 {
				world.motion.dead = true
				world.motion.action = DeathAction
				world.motion.buffer = ""
			}
		}
		seen[key] = true
		if hit.Kind == OrdinaryDamage {
			if world.combat.receivedHits == nil {
				world.combat.receivedHits = make(map[[3]string]bool)
			}
			world.combat.receivedHits[targetKey] = true
		}
		world.combat.outcomes = append(world.combat.outcomes, HitOutcome{Hit: hit, Target: "player", Result: result})
	}
}

type enemyHit struct {
	Target string
	Hit    DamageHit
}

func (world *World) applyEnemyHit(enemy *Enemy, hit DamageHit) {
	world.combat.pendingEnemyHits = append(world.combat.pendingEnemyHits, enemyHit{Target: enemy.ID, Hit: hit})
}

// resolveEnemyHits makes simultaneous hits independent of projectile insertion
// order. A direct contact wins over splash from the same event and target.
func (world *World) resolveEnemyHits(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	hits := world.combat.pendingEnemyHits
	slices.SortFunc(hits, func(a, b enemyHit) int {
		if order := compareID(a.Hit.Attacker, b.Hit.Attacker); order != 0 {
			return order
		}
		if order := compareID(a.Hit.Attack, b.Hit.Attack); order != 0 {
			return order
		}
		if order := compareID(a.Target, b.Target); order != 0 {
			return order
		}
		if a.Hit.Splash != b.Hit.Splash {
			if a.Hit.Splash {
				return 1
			}
			return -1
		}
		if a.Hit.Amount != b.Hit.Amount {
			return b.Hit.Amount - a.Hit.Amount
		}
		return a.Hit.Direction - b.Hit.Direction
	})
	seen := make(map[[3]string]bool)
	for _, candidate := range hits {
		for i := range world.combat.enemies {
			enemy := &world.combat.enemies[i]
			if enemy.ID != candidate.Target {
				continue
			}
			key := [3]string{candidate.Hit.Attacker, candidate.Hit.Attack, candidate.Target}
			result := HitImmune
			switch {
			case seen[key]:
				result = HitDuplicate
			case enemy.Health <= 0:
				result = HitAfterDeath
			case world.hurtEnemy(enemy, candidate.Hit.Amount, candidate.Hit.Direction):
				result = HitAccepted
			}
			seen[key] = true
			world.combat.outcomes = append(world.combat.outcomes, HitOutcome{Hit: candidate.Hit, Target: candidate.Target, Result: result})
			break
		}
	}
	world.combat.pendingEnemyHits = nil
}

func (world *World) hurtEnemy(enemy *Enemy, damage, direction int) bool {
	if enemy.Health <= 0 || world.state.Tick < enemy.ImmuneUntil {
		return false
	}
	enemy.Health = max(0, enemy.Health-damage)
	enemy.Body.Velocity = Vector{X: float64(direction) * 240, Y: 180}
	enemy.HurtUntil = world.state.Tick + 30
	enemy.ImmuneUntil = world.state.Tick + 96
	enemy.Phase = EnemyHurt
	enemy.Until = enemy.HurtUntil
	if enemy.Health == 0 {
		enemy.Phase = EnemyDefeated
		enemy.Body.Velocity = Vector{}
	}
	return true
}

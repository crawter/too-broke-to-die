// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"slices"
)

// clearBossProjectiles removes only this encounter's attack projectiles;
// player throws and other entities' projectiles keep their identities.
func (world *World) clearBossProjectiles() {
	id := string(world.boss.definition.Encounter)
	world.combat.projectiles = slices.DeleteFunc(world.combat.projectiles, func(projectile Projectile) bool { return !projectile.PlayerOwned && projectile.Attacker == id })
}

func (world *World) beginBossTransition() {
	world.clearBossProjectiles()
	world.boss.phase++
	world.boss.index = 0
	world.boss.stage = BossTransition
	world.boss.elapsed = 0
	world.boss.activeMin = 0
	world.boss.locked = nil
	world.boss.markers = nil
	world.boss.staggerUntil = 0
	world.boss.staggered = false
	world.boss.recoveryMin = 0
	world.resetBossRelays()
}

// advanceBoss is measured by StepBoss. If a single impact crosses both
// thresholds, each transition receives its full protection interval in order.
func (world *World) advanceBoss() {
	boss := &world.boss
	boss.elapsed++
	if boss.elapsed < boss.duration() {
		return
	}
	boss.elapsed = 0
	switch boss.stage {
	case BossTelegraph:
		if len(boss.markers) > 0 {
			boss.locked = slices.Clone(boss.markers)
		} else {
			body := world.state.Player.Bounds
			boss.locked = []Vector{{X: centerX(body), Y: centerY(body)}}
		}
		boss.markers = nil
		boss.activeMin = 0
		boss.stage = BossActive
	case BossActive:
		boss.activeMin = 0
		boss.stage = BossRecovery
		boss.recovery++
		boss.staggered = false
		boss.staggerUntil = 0
		boss.recoveryMin = 0
	case BossRecovery:
		boss.index = (boss.index + 1) % len(boss.definition.Phases[boss.phase].Attacks)
		boss.stage = BossTelegraph
		boss.locked = nil
		boss.staggerUntil = 0
	case BossTransition:
		if boss.phase < 2 && boss.health <= boss.definition.Thresholds[boss.phase] {
			world.beginBossTransition()
			return
		}
		boss.stage = BossTelegraph
	}
}

func (world *World) bossImpact(source string, damage int, stagger bool) {
	boss := &world.boss
	if boss.health <= 0 {
		return
	}
	if !boss.exposed() {
		boss.hits = append(boss.hits, BossHit{Source: source, Result: "protected"})
		return
	}
	boss.health = max(0, boss.health-damage)
	boss.hits = append(boss.hits, BossHit{Source: source, Result: "exposed", Damage: damage})
	if stagger && !boss.staggered {
		boss.staggered = true
		// One stagger can extend the recovery only enough to show its full
		// 250 milliseconds; subsequent impacts cannot extend it again.
		boss.recoveryMin = max(boss.recoveryMin, boss.elapsed+30)
		boss.staggerUntil = world.state.Tick + 30
	}
}

// resolveBossImpacts uses the inherited swept throws and actual melee shape.
// The previous attack preserves an already-active strike if player damage in
// this same tick cleared the inherited combat state.
func (world *World) resolveBossImpacts(ctx context.Context, previous PlayerAttack, previousAction ActionKind) (err error) {
	defer mon.Task()(&ctx)(&err)
	boss := &world.boss
	attack := world.combat.attack
	if attack.ID == 0 && previousAction == AttackAction && previous.ID != 0 && !previous.Throw && world.state.Tick >= previous.Start+15 && world.state.Tick < previous.Start+25 {
		attack = previous
		attack.Phase = PhaseActive
	}
	if attack.ID != 0 && !attack.Throw && attack.Phase == PhaseActive && boss.meleeHit != attack.ID {
		body := world.state.Player.Bounds
		area := Rect{X: body.X + body.Width, Y: body.Y + (body.Height-36)/2, Width: attack.Reach, Height: 36}
		if attack.Facing < 0 {
			area.X = body.X - attack.Reach
		}
		if !world.protected(body) && !world.protected(area) && overlaps(area, boss.bounds) {
			clear, clearErr := world.strikeClear(ctx, body, area, boss.bounds, attack.Facing)
			if clearErr != nil {
				return clearErr
			}
			if clear {
				boss.meleeHit = attack.ID
				world.bossImpact("melee", attack.Damage, false)
			}
		}
	}
	id := string(boss.definition.Encounter)
	for _, event := range world.limbs.events {
		if event.Kind != "impact" || event.Target != id {
			continue
		}
		damage := 35
		if socketMask(event.Socket)&(LeftArm|RightArm) != 0 {
			damage = 25
		}
		world.bossImpact("limb", damage, false)
	}
	for _, impact := range world.feces.impacts {
		if impact.Target != id || (impact.Kind != "boss_exposed" && impact.Kind != "boss_protected") {
			continue
		}
		world.bossImpact("feces", 8, true)
	}
	return nil
}

// prepareBossMarkers is the in-package encounter hook for marked circles,
// lanes, levels or trajectories. The last telegraph sample is copied at the
// boundary and cannot be changed during the committed active attack.
func (world *World) prepareBossMarkers(ctx context.Context, markers []Vector) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.boss.stage != BossTelegraph || len(markers) != world.boss.attack().TargetCount {
		return fmt.Errorf("invalid boss target markers")
	}
	for _, marker := range markers {
		if !validVector(marker) {
			return fmt.Errorf("invalid boss target position")
		}
	}
	world.boss.markers = slices.Clone(markers)
	return nil
}

// extendBossActive lets an encounter executor retain protection until its
// committed physical sweep or projectile release sequence has completed.
// The authored minimum remains in force and every interval is bounded.
func (world *World) extendBossActive(ctx context.Context, ticks int) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.boss.stage != BossActive || ticks < world.boss.duration() || ticks > msTicks(3600000) {
		return fmt.Errorf("invalid boss active duration")
	}
	world.boss.activeMin = ticks
	return nil
}

// exposed includes institute relay protection, not merely the timed recovery.
func (boss *bossState) exposed() bool {
	if boss.stage != BossRecovery {
		return false
	}
	for _, relay := range boss.relays {
		if relay.Required && relay.Health > 0 {
			return false
		}
	}
	return true
}

func (world *World) resetBossRelays() {
	boss := &world.boss
	for i := range boss.relays {
		relay := &boss.relays[i]
		relay.Required = slices.Contains(boss.definition.Phases[boss.phase].Relays, relay.ID)
		if relay.Required {
			relay.Health = boss.definition.RelayHealth
			relay.StoppedUntil, relay.CooldownUntil, relay.meleeHit = 0, 0, 0
		}
	}
}

func (world *World) resolveBossRelays(ctx context.Context, previous PlayerAttack, previousAction ActionKind) (err error) {
	defer mon.Task()(&ctx)(&err)
	attack := world.combat.attack
	if attack.ID == 0 && previousAction == AttackAction && previous.ID != 0 && !previous.Throw && world.state.Tick >= previous.Start+15 && world.state.Tick < previous.Start+25 {
		attack = previous
		attack.Phase = PhaseActive
	}
	for i := range world.boss.relays {
		relay := &world.boss.relays[i]
		if !relay.Required || relay.Health <= 0 {
			continue
		}
		id := "boss-relay-" + relay.ID
		if attack.ID != 0 && !attack.Throw && attack.Phase == PhaseActive && relay.meleeHit != attack.ID {
			body := world.state.Player.Bounds
			area := Rect{X: body.X + body.Width, Y: body.Y + (body.Height-36)/2, Width: attack.Reach, Height: 36}
			if attack.Facing < 0 {
				area.X = body.X - attack.Reach
			}
			if !world.protected(body) && !world.protected(area) && overlaps(area, relay.Bounds) {
				clear, clearErr := world.strikeClear(ctx, body, area, relay.Bounds, attack.Facing)
				if clearErr != nil {
					return clearErr
				}
				if clear {
					relay.Health = max(0, relay.Health-attack.Damage)
					relay.meleeHit = attack.ID
				}
			}
		}
		for _, event := range world.limbs.events {
			if event.Kind == "impact" && event.Target == id {
				damage := 35
				if socketMask(event.Socket)&(LeftArm|RightArm) != 0 {
					damage = 25
				}
				relay.Health = max(0, relay.Health-damage)
			}
		}
		for _, impact := range world.feces.impacts {
			if impact.Kind == "source" && impact.Target == id && world.state.Tick >= relay.CooldownUntil && relay.Health > 0 {
				relay.StoppedUntil = world.state.Tick + 240
				relay.CooldownUntil = world.state.Tick + 480
			}
		}
	}
	return nil
}

// BossRecoveryID identifies an exposed interval for attack-specific hooks.
func (world *World) BossRecoveryID(ctx context.Context) string {
	defer mon.Task()(&ctx)(nil)
	if world == nil || world.boss.stage != BossRecovery {
		return ""
	}
	return fmt.Sprintf("%s-%d", world.boss.definition.Encounter, world.boss.recovery)
}

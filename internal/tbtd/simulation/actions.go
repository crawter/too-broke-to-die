// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"time"

	"too-broke-to-die/internal/tbtd/settings"
)

// ActionKind is ordered by the PDD's cancellation priority, not animation.
type ActionKind uint8

const (
	// FreeAction has no committed action and permits movement and new actions.
	FreeAction ActionKind = iota
	// AttackAction commits a melee attack or throw while allowing movement and jumps.
	AttackAction
	// RemovalAction commits a limb-removal channel until interrupted or complete.
	RemovalAction
	// InteractionAction commits an interaction or regeneration channel.
	InteractionAction
	// EvadeAction commits the facing-direction evade; it cannot start over another action.
	EvadeAction
	// HurtAction commits hurt stun and cancels lower-priority actions.
	HurtAction
	// DeathAction ends gameplay and cancels every lower-priority action.
	DeathAction
)

// StartPlayerAction lets later combat and interaction systems install a timed
// commitment. Higher-priority events interrupt lower-priority ones. Evade is
// started only by an input edge and cannot interrupt any commitment.
func (world *World) StartPlayerAction(ctx context.Context, action ActionKind, duration time.Duration) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || !world.state.HasPlayer || world.motion.dead || world.motion.suspended || action < AttackAction || action > DeathAction || action == EvadeAction || duration <= 0 {
		return fmt.Errorf("invalid player action")
	}
	ticks, err := DurationTicks(duration)
	if err != nil {
		return err
	}
	if action <= world.motion.action && world.motion.actionUntil > world.state.Tick {
		return fmt.Errorf("player action is committed")
	}
	world.motion.action = action
	world.motion.actionUntil = world.state.Tick + uint64(ticks)
	if action > AttackAction {
		world.combat.attack = PlayerAttack{}
		world.combat.hitTargets = nil
	}
	if action == HurtAction {
		world.motion.immune = world.state.Tick < world.combat.immuneUntil
	}
	if action == DeathAction {
		world.state.Health = 0
		world.motion.dead = true
		world.motion.buffer = ""
		world.motion.immune = false
	}
	return nil
}

// ConsumeCombatPress returns a buffered melee or throw edge once when combat
// can start it. The caller then installs AttackAction and owns release phases.
func (world *World) ConsumeCombatPress(ctx context.Context) settings.Action {
	defer mon.Task()(&ctx)(nil)
	if world == nil || world.motion.dead || world.motion.suspended || world.motion.action != FreeAction {
		return ""
	}
	// Consumers run after movement, while damage and action starts for the
	// just-completed tick still use that tick's inclusive buffer boundary.
	tick := world.state.Tick
	if tick > 0 {
		tick--
	}
	if tick >= world.motion.bufferUntil {
		return ""
	}
	action := world.motion.buffer
	if action != settings.Melee && action != settings.Throw {
		return ""
	}
	world.motion.buffer = ""
	return action
}

func edgeBit(action settings.Action) uint8 {
	switch action {
	case settings.Jump:
		return 1
	case settings.Melee:
		return 2
	case settings.Throw:
		return 4
	case settings.Evade:
		return 8
	}
	return 0
}

// collectEdges retains one action, never converting a held button into a
// second press. The session preserves the ordered transitions across frames.
func (motion *playerMotion) collectEdges(edges []PlayerEdge, tick uint64) {
	if motion.buffer != "" && tick >= motion.bufferUntil {
		motion.buffer = ""
	}
	for _, edge := range edges {
		bit := edgeBit(edge.Action)
		if bit == 0 {
			continue
		}
		if !edge.Pressed {
			motion.held &^= bit
			if edge.Action == settings.Jump {
				motion.jumpHeld = false
			}
			continue
		}
		if motion.held&bit != 0 {
			continue
		}
		motion.held |= bit
		if edge.Action == settings.Jump {
			motion.jumpHeld = true
		}
		// Evade has priority over jump and attacks when edges arrive together.
		if motion.buffer == "" || (edge.Action == settings.Evade && motion.buffer != settings.Evade) {
			motion.buffer = edge.Action
			motion.bufferUntil = tick + bufferTicks
		}
	}
}

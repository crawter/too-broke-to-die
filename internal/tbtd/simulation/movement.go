// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"

	"too-broke-to-die/internal/tbtd/settings"
)

func legSpeeds(limbs LimbMask) (run, jump, evade float64) {
	legs := limbs & (LeftLeg | RightLeg)
	switch legs {
	case LeftLeg | RightLeg:
		return 320, 760, 650
	case LeftLeg, RightLeg:
		return 230, 620, 440
	default:
		return 120, 0, 0
	}
}

func approach(value, target, amount float64) float64 {
	if value < target {
		return math.Min(value+amount, target)
	}
	return math.Max(value-amount, target)
}

func (world *World) startJump(tick uint64, jumpSpeed float64) bool {
	motion := &world.motion
	if motion.buffer != settings.Jump || tick >= motion.bufferUntil || jumpSpeed == 0 || motion.jumpUsed || (motion.action != FreeAction && motion.action != AttackAction) {
		return false
	}
	if !motion.hasGround || tick-motion.lastGround > forgivenessTicks {
		return false
	}
	motion.buffer = ""
	motion.jumpUsed = true
	motion.jumpActive = true
	world.state.Player.Velocity.Y = jumpSpeed
	return true
}

// StepPlayer advances one authoritative tick using normalized input, the
// world's existing platform/collision solver and fixed 120 Hz integration.
// Errors do not consume input or mutate the world. The raw Step remains the
// collision primitive for callers constructing other kinematic bodies.
func (world *World) StepPlayer(ctx context.Context, command PlayerCommand) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil || !world.state.HasPlayer || world.motion.dead || world.motion.suspended || world.state.Player.Crushed || !finite(command.Move) || math.Abs(command.Move) > 1 || !validVector(command.Aim) || math.Abs(command.Aim.X) > 1 || math.Abs(command.Aim.Y) > 1 {
		return fmt.Errorf("invalid player tick")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	for _, edge := range command.Edges {
		if !edge.Action.Valid() {
			return fmt.Errorf("invalid input edge")
		}
	}
	before := cloneState(world.state)
	previous := cloneState(world.previous)
	motionBefore := world.motion
	defer func() {
		if err != nil {
			world.state = before
			world.previous = previous
			world.motion = motionBefore
		}
	}()

	motion := &world.motion
	tick := world.state.Tick
	if motion.action != FreeAction && tick >= motion.actionUntil {
		if motion.action == EvadeAction {
			run, _, _ := legSpeeds(motion.limbs)
			world.state.Player.Velocity.X = math.Max(-run, math.Min(run, world.state.Player.Velocity.X))
		}
		motion.action = FreeAction
	}
	if motion.drop != "" && tick >= motion.dropUntil {
		motion.drop = ""
	}
	motion.collectEdges(command.Edges, tick)
	body := &world.state.Player
	grounded := body.Support != "" && body.Support != motion.drop && body.Velocity.Y <= 0
	if grounded {
		motion.hasGround = true
		motion.lastGround = tick
		motion.jumpUsed = false
	}
	if motion.action == FreeAction {
		if command.Move != 0 {
			if command.Move < 0 {
				motion.facing = -1
			} else {
				motion.facing = 1
			}
		}
		if command.Aim.X != 0 {
			if command.Aim.X < 0 {
				motion.facing = -1
			} else {
				motion.facing = 1
			}
		}
	}
	run, jump, evade := legSpeeds(motion.limbs)
	if motion.buffer == settings.Jump && command.Down && grounded && (motion.action == FreeAction || motion.action == AttackAction) && world.validDrop(body.Support) && body.Support != "" {
		motion.drop = body.Support
		motion.dropUntil = tick + dropTicks
		motion.buffer = ""
		motion.jumpUsed = true
		motion.hasGround = false
		grounded = false
	} else {
		world.startJump(tick, jump)
	}
	if motion.buffer == settings.Evade && motion.action == FreeAction && evade > 0 && tick >= motion.cooldownUntil {
		motion.buffer = ""
		motion.action = EvadeAction
		motion.actionUntil = tick + evadeTicks
		motion.immuneUntil = tick + evadeImmunityTicks
		motion.cooldownUntil = tick + evadeCooldownTicks
	}
	motion.immune = motion.action == EvadeAction && tick < motion.immuneUntil

	velocity := body.Velocity
	if motion.action == EvadeAction {
		velocity.X = float64(motion.facing) * evade
	} else {
		if motion.action == AttackAction {
			run *= 0.8
		}
		if motion.action == FreeAction || motion.action == AttackAction {
			velocity.X = math.Max(-run, math.Min(run, velocity.X))
		}
		target := command.Move * run
		acceleration := 1600.0
		if grounded {
			acceleration = 2400
			if command.Move == 0 {
				acceleration = 3000
			}
		}
		if motion.action == InteractionAction || motion.action == RemovalAction || motion.action == HurtAction || motion.action == DeathAction {
			target = 0
		}
		velocity.X = approach(velocity.X, target, acceleration*TickSeconds)
	}
	if motion.jumpActive && !motion.jumpHeld && velocity.Y > 280 {
		velocity.Y = 280
	}
	velocity.Y = math.Max(-1000, velocity.Y-2200*TickSeconds)
	trace, err := world.stepWithTrace(ctx, TickInput{Velocity: velocity, DropThrough: motion.drop})
	if err != nil {
		return err
	}
	// The collision primitive sees the launch velocity already applied; the
	// previous snapshot must instead represent the state before input.
	world.previous = before
	body = &world.state.Player
	if body.Velocity.Y <= 0 {
		motion.jumpActive = false
	}
	if body.Support != "" && body.Support != motion.drop && body.Velocity.Y <= 0 {
		motion.hasGround = true
		motion.lastGround = tick
		motion.jumpUsed = false
		// A press still valid on the landing tick must not expire before the
		// next tick can consume it. Launch without inheriting platform velocity.
		world.startJump(tick, jump)
	}
	if body.Crushed || world.crossedLethal(trace) {
		motion.dead = true
		motion.action = DeathAction
		motion.buffer = ""
		motion.immune = false
	}
	return nil
}

func (world *World) crossedLethal(trace []movementSegment) bool {
	for _, segment := range trace {
		delta := Vector{X: segment.to.X - segment.from.X, Y: segment.to.Y - segment.from.Y}
		for _, volume := range world.lethal {
			if overlaps(segment.from, volume) || overlaps(segment.to, volume) {
				return true
			}
			if _, hit := impactTime(segment.from, delta, Collider{Kind: Solid, Bounds: volume}); hit {
				return true
			}
		}
	}
	return false
}

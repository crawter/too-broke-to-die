// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"math"
	"slices"
)

// Reaction describes a bounded, non-damaging forced movement.
type Reaction struct {
	Actor       string
	Source      string
	Kind        string
	Direction   int
	Until       uint64
	ImmuneUntil uint64
}

type reactionState struct {
	active   []Reaction
	cooldown map[string]uint64
	immune   map[string]uint64
}

func cloneReactions(state reactionState) reactionState {
	state.active = slices.Clone(state.active)
	if state.cooldown != nil {
		copied := make(map[string]uint64, len(state.cooldown))
		for key, value := range state.cooldown {
			copied[key] = value
		}
		state.cooldown = copied
	}
	if state.immune != nil {
		copied := make(map[string]uint64, len(state.immune))
		for key, value := range state.immune {
			copied[key] = value
		}
		state.immune = copied
	}
	return state
}

// Reactions returns an isolated description of currently forced reactions.
func (world *World) Reactions(ctx context.Context) []Reaction {
	defer mon.Task()(&ctx)(nil)
	if world == nil {
		return nil
	}
	return slices.Clone(world.objects.reactions.active)
}

func (world *World) beginReaction(actor, source, kind string, direction int, ticks uint64) {
	state := &world.objects.reactions
	if state.immune == nil {
		state.immune = make(map[string]uint64)
	}
	if state.cooldown == nil {
		state.cooldown = make(map[string]uint64)
	}
	key := actor + "\x00" + source
	if world.state.Tick < state.immune[actor] || world.state.Tick < state.cooldown[key] {
		return
	}
	state.immune[actor] = world.state.Tick + 120
	if kind == "slip" {
		state.cooldown[key] = world.state.Tick + 240
	}
	state.active = append(state.active, Reaction{Actor: actor, Source: source, Kind: kind, Direction: direction, Until: world.state.Tick + ticks, ImmuneUntil: state.immune[actor]})
	world.objects.events = append(world.objects.events, InteractionEvent{Kind: kind, Target: source})
}

// safeReactionSpeed prevents forced horizontal travel from entering the 32px
// exclusion around lethal volumes. It never makes the lethal volume harmless.
func (world *World) safeReactionSpeed(body Rect, direction int, speed float64) float64 {
	distance := speed * TickSeconds
	for _, volume := range world.lethal {
		// A pit commonly ends at floor height: the player is above its
		// volume before falling in, but the lip is already dangerous.
		if body.Y > volume.Y+volume.Height+PlayerHeight || body.Y+body.Height <= volume.Y {
			continue
		}
		gap := volume.X - 32 - (body.X + body.Width)
		if direction < 0 {
			gap = body.X - (volume.X + volume.Width + 32)
		}
		if gap >= 0 {
			distance = math.Min(distance, gap)
		} else if (direction > 0 && body.X < volume.X+volume.Width) || (direction < 0 && body.X+body.Width > volume.X) {
			distance = 0
		}
	}
	return math.Max(0, distance/TickSeconds)
}

func (world *World) prepareReaction(ctx context.Context, player *PlayerCommand) {
	defer mon.Task()(&ctx)(nil)
	state := &world.objects.reactions
	for _, reaction := range state.active {
		if reaction.Actor == "player" && world.state.Tick < reaction.Until && world.motion.action != HurtAction && !world.motion.dead && !world.motion.suspended {
			speed := world.safeReactionSpeed(world.state.Player.Bounds, reaction.Direction, 180)
			// Seed velocity to account for the inherited integrator's braking,
			// including an airborne tick after leaving a ledge.
			braking := 1600 * TickSeconds
			body := world.state.Player
			if body.Support != "" && body.Support != world.motion.drop && body.Velocity.Y <= 0 {
				braking = 3000 * TickSeconds
			}
			world.motion.action = InteractionAction
			world.motion.actionUntil = reaction.Until
			world.state.Player.Velocity.X = float64(reaction.Direction) * (speed + braking)
			player.Move = 0
			var releases []PlayerEdge
			for _, edge := range player.Edges {
				if !edge.Pressed {
					releases = append(releases, edge)
				}
			}
			player.Edges = releases
		}
		if reaction.Actor == "player" {
			continue
		}
		for i := range world.combat.enemies {
			enemy := &world.combat.enemies[i]
			if enemy.ID == reaction.Actor && enemy.Health > 0 && enemy.Phase == EnemyHurt && enemy.HurtUntil == reaction.Until && world.state.Tick < reaction.Until {
				enemy.Body.Velocity.X = float64(reaction.Direction) * 180
			}
		}
	}
}

func smearContact(body Rect, smear Smear) bool {
	return body.X <= smear.Position.X+24 && body.X+body.Width >= smear.Position.X-24 && math.Abs(body.Y-smear.Position.Y) <= 2
}

func (world *World) finishReactions(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	state := &world.objects.reactions
	for key, until := range state.cooldown {
		if world.state.Tick >= until {
			delete(state.cooldown, key)
		}
	}
	for actor, until := range state.immune {
		if world.state.Tick >= until {
			delete(state.immune, actor)
		}
	}
	state.active = slices.DeleteFunc(state.active, func(reaction Reaction) bool {
		if reaction.Actor == "player" {
			return world.motion.dead || world.motion.suspended || world.motion.action == HurtAction || world.state.Tick >= reaction.Until
		}
		for _, enemy := range world.combat.enemies {
			if enemy.ID == reaction.Actor {
				return enemy.Health <= 0 || enemy.HurtUntil != reaction.Until || world.state.Tick >= reaction.Until
			}
		}
		return true
	})
	player := world.state.Player
	if !world.motion.dead && !world.motion.suspended && world.motion.action == FreeAction && player.Support != "" && math.Abs(player.Velocity.X) > 0 {
		for _, smear := range world.feces.smears {
			if smearContact(player.Bounds, smear) {
				direction := int(math.Copysign(1, player.Velocity.X))
				world.beginReaction("player", smear.ID, "slip", direction, 42)
				break
			}
		}
		for _, object := range world.objects.items {
			if object.Kind == ObjectProp && overlaps(player.Bounds, object.Bounds) {
				direction := int(math.Copysign(1, player.Velocity.X))
				world.beginReaction("player", object.ID, "prop_launch", direction, 84)
				break
			}
		}
	}
	for i := range world.combat.enemies {
		enemy := &world.combat.enemies[i]
		if enemy.Health <= 0 || enemy.Phase != EnemyApproach || enemy.Body.Velocity.X == 0 {
			continue
		}
		for _, smear := range world.feces.smears {
			if smearContact(enemy.Body.Bounds, smear) {
				before := len(state.active)
				direction := int(math.Copysign(1, enemy.Body.Velocity.X))
				world.beginReaction(enemy.ID, smear.ID, "slip", direction, 42)
				if len(state.active) != before {
					enemy.Phase, enemy.Until, enemy.HurtUntil = EnemyHurt, world.state.Tick+42, world.state.Tick+42
				}
				break
			}
		}
		if enemy.Phase != EnemyApproach {
			continue
		}
		for _, object := range world.objects.items {
			if object.Kind == ObjectProp && overlaps(enemy.Body.Bounds, object.Bounds) {
				before := len(state.active)
				direction := int(math.Copysign(1, enemy.Body.Velocity.X))
				world.beginReaction(enemy.ID, object.ID, "prop_launch", direction, 84)
				if len(state.active) != before {
					enemy.Phase, enemy.Until, enemy.HurtUntil = EnemyHurt, world.state.Tick+84, world.state.Tick+84
				}
				break
			}
		}
	}
}

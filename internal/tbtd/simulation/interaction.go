// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

// InteractionKind determines the explicit selection priority.
type InteractionKind string

const (
	// InteractionSocket is a carried limb's socket and has first priority.
	InteractionSocket InteractionKind = "socket"
	// InteractionNPC is a conversation target, ahead of ordinary objects.
	InteractionNPC InteractionKind = "npc"
	// InteractionService is a marked service target selected by distance.
	InteractionService InteractionKind = "service"
	// InteractionLoose is a loose limb or movable object selected by distance.
	InteractionLoose InteractionKind = "loose"
	// InteractionRefill is a marked refill station selected by distance.
	InteractionRefill InteractionKind = "refill"
)

// InteractionTarget is an authored prompt, not a certification or resource.
// A nonempty Reason makes the target visibly unavailable without consumption.
type InteractionTarget struct {
	ID           string
	Kind         InteractionKind
	Bounds       Rect
	Input        settings.Action
	Prerequisite string
	Reason       string
	Moving       bool
	HoldTicks    int
}

// InteractionEvent is a transient completed or cancelled interaction.
type InteractionEvent struct {
	Kind   string
	Target string
	// Position is the impact point for a paddle latch, before the limb settles.
	Position Vector
}

// InteractionView exposes a detached prompt and progress for presentation.
type InteractionView struct {
	Target   InteractionTarget
	Found    bool
	Progress int
	Duration int
	Invalid  string
	Events   []InteractionEvent
}

type interactionChannel struct {
	target InteractionTarget
	start  uint64
	until  uint64
}

// AddInteractionTarget registers an explicitly marked service or conversation.
func (world *World) AddInteractionTarget(ctx context.Context, target InteractionTarget) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || target.ID == "" || !validRect(target.Bounds, false) || (target.Kind != InteractionNPC && target.Kind != InteractionService && target.Kind != InteractionLoose) || target.HoldTicks < 0 || target.HoldTicks > 72000 {
		return fmt.Errorf("invalid interaction target")
	}
	if target.Input == "" {
		target.Input = settings.Interact
	}
	if target.Input != settings.Interact {
		return fmt.Errorf("unsupported interaction input")
	}
	if target.HoldTicks == 0 {
		target.HoldTicks = 36
	}
	for _, existing := range world.objects.interactions {
		if existing.ID == target.ID {
			return fmt.Errorf("duplicate interaction target %s", target.ID)
		}
	}
	world.objects.interactions = append(world.objects.interactions, target)
	slices.SortFunc(world.objects.interactions, func(a, b InteractionTarget) int { return compareID(a.ID, b.ID) })
	return nil
}

func targetDistance(body, target Rect) float64 {
	dx := math.Max(0, math.Max(body.X-(target.X+target.Width), target.X-(body.X+body.Width)))
	dy := math.Max(0, math.Max(body.Y-(target.Y+target.Height), target.Y-(body.Y+body.Height)))
	return math.Hypot(dx, dy)
}

func targetPriority(kind InteractionKind) int {
	switch kind {
	case InteractionSocket:
		return 0
	case InteractionNPC:
		return 1
	default:
		return 2
	}
}

func (world *World) selectedInteraction(facing int) (InteractionTarget, bool) {
	body := world.state.Player.Bounds
	var candidates []InteractionTarget
	if world.limbs.carried != "" {
		candidates = append(candidates, InteractionTarget{ID: world.limbs.carried, Kind: InteractionSocket, Bounds: body, Input: settings.Interact, Prerequisite: "matching missing socket", HoldTicks: 48})
	}
	candidates = append(candidates, world.objects.interactions...)
	for _, civilian := range world.combat.civilians {
		if !civilian.Incapacitated {
			candidates = append(candidates, InteractionTarget{ID: civilian.ID, Kind: InteractionNPC, Bounds: civilian.Bounds, Input: settings.Interact, HoldTicks: 36})
		}
	}
	for _, station := range world.feces.stations {
		candidates = append(candidates, InteractionTarget{ID: station.ID, Kind: InteractionRefill, Bounds: Rect{X: station.Position.X, Y: station.Position.Y, Width: Tolerance, Height: Tolerance}, Input: settings.Interact, HoldTicks: 180})
	}
	for _, object := range world.objects.items {
		if object.Kind != ObjectCrate && object.Kind != ObjectTrolley {
			continue
		}
		input := settings.MoveRight
		if centerX(body) > centerX(object.Bounds) {
			input = settings.MoveLeft
		}
		target := InteractionTarget{ID: object.ID, Kind: InteractionLoose, Bounds: object.Bounds, Input: input, Prerequisite: "grounded pushing"}
		if world.state.Player.Support == "" {
			target.Reason = "requires_grounded"
		}
		candidates = append(candidates, target)
	}
	for _, limb := range world.limbs.objects {
		if !limb.Carried && !limb.Airborne && limb.Generation == world.limbs.generation && world.motion.limbs&socketMask(limb.Socket) == 0 {
			candidates = append(candidates, InteractionTarget{ID: limb.ID, Kind: InteractionLoose, Bounds: Rect{X: limb.Position.X, Y: limb.Position.Y, Width: Tolerance, Height: Tolerance}, Input: settings.Interact, Prerequisite: "matching missing socket", HoldTicks: 48})
		}
	}
	var best InteractionTarget
	bestDistance := math.Inf(1)
	bestFacing, found := false, false
	for _, target := range candidates {
		distance := targetDistance(body, target.Bounds)
		if distance > 48 {
			continue
		}
		front := (centerX(target.Bounds)-centerX(body))*float64(facing) >= 0
		if !found || targetPriority(target.Kind) < targetPriority(best.Kind) || (targetPriority(target.Kind) == targetPriority(best.Kind) && (distance < bestDistance || (distance == bestDistance && ((front && !bestFacing) || (front == bestFacing && target.ID < best.ID))))) {
			best, bestDistance, bestFacing, found = target, distance, front, true
		}
	}
	if found {
		best = world.annotateInteraction(best)
	}
	return best, found
}

func (world *World) latchTarget(id string) bool {
	for _, entity := range world.state.Entities {
		if string(entity.ID) == id && entity.Role == content.RoleLatch {
			return true
		}
	}
	return false
}

func (world *World) annotateInteraction(target InteractionTarget) InteractionTarget {
	if !world.latchTarget(target.ID) {
		return target
	}
	target.Prerequisite = "weighted pressure plate"
	for _, object := range world.objects.items {
		if object.Kind == ObjectPlate && object.Active {
			return target
		}
	}
	target.Reason = "requires_weighted_plate"
	return target
}

// InteractionView returns the selected prompt, current hold and last tick's events.
func (world *World) InteractionView(ctx context.Context) InteractionView {
	defer mon.Task()(&ctx)(nil)
	if world == nil || !world.state.HasPlayer {
		return InteractionView{}
	}
	channel := world.objects.channel
	target, found := world.selectedInteraction(world.motion.facing)
	progress, duration := 0, 0
	if channel.target.ID != "" {
		target, found = channel.target, true
		duration = int(channel.until - channel.start)
		progress = int(min(world.state.Tick-channel.start, uint64(duration)))
	}
	return InteractionView{Target: target, Found: found, Progress: progress, Duration: duration, Invalid: world.objects.invalid, Events: slices.Clone(world.objects.events)}
}

func (world *World) interactionThreatened() bool {
	area := world.state.Player.Bounds
	area.X -= 48
	area.Width += 96
	for _, enemy := range world.combat.enemies {
		if enemy.Health > 0 && enemy.Phase == EnemyTelegraph && !world.protected(enemy.Body.Bounds) && !world.protected(world.state.Player.Bounds) && (enemy.Kind == RangedEnemy || overlaps(area, enemy.Body.Bounds)) {
			return true
		}
	}
	for _, projectile := range world.combat.projectiles {
		if projectile.PlayerOwned {
			continue
		}
		origin := Rect{X: projectile.Position.X, Y: projectile.Position.Y}
		delta := Vector{X: projectile.Velocity.X * TickSeconds * 36, Y: projectile.Velocity.Y * TickSeconds * 36}
		if _, hit := impactTime(origin, delta, Collider{Kind: Solid, Bounds: area}); hit {
			return true
		}
	}
	return false
}

func (world *World) installAuthoredInteractions(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	for _, entity := range world.state.Entities {
		switch entity.Role {
		case content.RoleConsole, content.RoleTerminal, content.RoleSwitch, content.RoleLatch, content.RoleRecorder, content.RoleRefuge, content.RoleCheckpoint:
		default:
			continue
		}
		target := InteractionTarget{ID: string(entity.ID), Kind: InteractionService, Bounds: Rect{X: entity.Position.X - 8, Y: entity.Position.Y, Width: 16, Height: 32}, Input: settings.Interact, HoldTicks: 36}
		if err = world.AddInteractionTarget(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

// ObjectCommand advances the complete interaction, feces, limb and combat tick.
type ObjectCommand struct {
	Feces FecesCommand
}

// StepObjects is the interaction-enabled fixed-tick entry point. Errors leave
// all world state unchanged. Completed holds are transient candidates only.
func (world *World) StepObjects(ctx context.Context, command ObjectCommand) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil {
		return fmt.Errorf("invalid object tick")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	before, previous, motion := cloneState(world.state), cloneState(world.previous), world.motion
	combat, limbs, feces, objects := cloneCombat(world.combat), cloneLimbs(world.limbs), cloneFeces(world.feces), cloneObjects(world.objects)
	defer func() {
		if err != nil {
			world.state, world.previous, world.motion = before, previous, motion
			world.combat, world.limbs, world.feces, world.objects = combat, limbs, feces, objects
		}
	}()
	world.objects.events = nil
	world.objects.invalid = ""
	player := &command.Feces.Limb.Combat.Player
	world.prepareReaction(ctx, player)
	world.prepareInteraction(ctx, &command.Feces)
	moving := world.objects.channel.target.ID != "" && world.objects.channel.target.Moving && world.motion.action == InteractionAction
	if moving {
		// The inherited movement integrator permits movement during an attack,
		// so use that movement mode without installing an attack. Keep the
		// channel's priority by suppressing new action edges until it ends.
		world.motion.action = AttackAction
		var releases []PlayerEdge
		for _, edge := range player.Edges {
			if !edge.Pressed {
				releases = append(releases, edge)
			}
		}
		player.Edges = releases
	}
	platforms, err := world.objectPlatforms(ctx, player.Move)
	if err != nil {
		return err
	}
	world.state.Platforms = platforms
	if err = world.StepFeces(ctx, command.Feces); err != nil {
		return err
	}
	if moving && world.motion.action == AttackAction {
		world.motion.action = InteractionAction
	}
	world.absorbObjectPlatforms()
	// Temporary collider platforms are not part of the preceding snapshot.
	world.previous.Platforms = slices.Clone(before.Platforms)
	if err = world.resolveObjectImpacts(ctx, limbs); err != nil {
		return err
	}
	if err = world.updateObjects(ctx); err != nil {
		return err
	}
	world.finishInteraction(ctx, *player)
	world.finishReactions(ctx)
	return nil
}

func (world *World) prepareInteraction(ctx context.Context, command *FecesCommand) {
	defer mon.Task()(&ctx)(nil)
	player := &command.Limb.Combat.Player
	state := &world.objects
	if state.channel.target.ID != "" && (world.motion.action != InteractionAction || world.motion.suspended || world.motion.dead || !world.limbs.interactHeld || (!state.channel.target.Moving && !world.stationary(*player)) || targetDistance(world.state.Player.Bounds, state.channel.target.Bounds) > 48) {
		state.events = append(state.events, InteractionEvent{Kind: "cancelled", Target: state.channel.target.ID})
		state.channel = interactionChannel{}
		world.limbs.interactHeld = false
		state.held = false
		if world.motion.action == InteractionAction {
			world.motion.action = FreeAction
		}
	}
	facing := world.motion.facing
	if world.motion.action == FreeAction || world.state.Tick >= world.motion.actionUntil {
		if player.Move != 0 {
			facing = int(math.Copysign(1, player.Move))
		}
		if player.Aim.X != 0 {
			facing = int(math.Copysign(1, player.Aim.X))
		}
	}
	target, found := world.selectedInteraction(facing)
	command.Station = ""
	var evade, regenerate bool
	for _, edge := range player.Edges {
		if edge.Action == settings.Evade && edge.Pressed && world.motion.held&edgeBit(settings.Evade) == 0 {
			_, _, speed := legSpeeds(world.motion.limbs)
			evade = speed > 0 && world.state.Tick >= world.motion.cooldownUntil && (world.motion.action == FreeAction || world.state.Tick >= world.motion.actionUntil)
		}
		if edge.Action == settings.Regenerate && edge.Pressed {
			regenerate = true
		}
	}
	var edges []PlayerEdge
	for _, edge := range player.Edges {
		if edge.Action != settings.Interact {
			edges = append(edges, edge)
			continue
		}
		fresh := edge.Pressed && !state.held
		state.held = edge.Pressed
		// Once a lower-level refill or replacement has started, its owner
		// must receive release even if a closer prompt appears meanwhile.
		if world.feces.channel != "" || world.limbs.channel.kind == "replace" {
			edges = append(edges, edge)
			continue
		}
		if state.channel.target.ID != "" {
			if !state.held {
				state.events = append(state.events, InteractionEvent{Kind: "cancelled", Target: state.channel.target.ID})
				state.channel = interactionChannel{}
				world.limbs.interactHeld = false
				if world.motion.action == InteractionAction {
					world.motion.action = FreeAction
				}
			}
			continue
		}
		if !found || target.Kind == InteractionSocket || (target.Kind == InteractionLoose && world.limbIndex(target.ID) >= 0) {
			edges = append(edges, edge)
			continue
		}
		if target.Kind == InteractionRefill {
			command.Station = target.ID
			edges = append(edges, edge)
			continue
		}
		if target.Input != settings.Interact {
			if fresh {
				state.invalid = "push_with_movement"
			}
			continue
		}
		if !fresh || evade || regenerate {
			continue
		}
		switch {
		case target.Reason != "":
			state.invalid = target.Reason
		case target.Kind == InteractionNPC && world.interactionThreatened():
			state.invalid = "area_threatened"
		case !target.Moving && !world.stationary(*player):
			state.invalid = "requires_stationary_ground"
		default:
			duration := time.Duration(target.HoldTicks) * time.Second / 120
			// Convert exact ticks to a duration that rounds up to that tick count.
			if startErr := world.StartPlayerAction(ctx, InteractionAction, duration); startErr != nil {
				state.invalid = "action_committed"
			} else {
				world.motion.buffer = ""
				world.limbs.interactHeld = true
				state.channel = interactionChannel{target: target, start: world.state.Tick, until: world.state.Tick + uint64(target.HoldTicks)}
			}
		}
	}
	player.Edges = edges
}

func (world *World) finishInteraction(ctx context.Context, player PlayerCommand) {
	defer mon.Task()(&ctx)(nil)
	state := &world.objects
	channel := state.channel
	if channel.target.ID == "" {
		return
	}
	complete := world.state.Tick >= channel.until && state.held && targetDistance(world.state.Player.Bounds, channel.target.Bounds) <= 48
	if complete {
		world.limbs.interactHeld = false
		if world.latchTarget(channel.target.ID) {
			for _, object := range world.objects.items {
				if object.Kind == ObjectPlate && object.Active {
					if err := world.latchGate(ctx, object.ID); err != nil {
						state.invalid = "requires_weighted_plate"
						state.channel = interactionChannel{}
						return
					}
					break
				}
			}
			if world.annotateInteraction(channel.target).Reason != "" {
				state.invalid = "requires_weighted_plate"
				state.channel = interactionChannel{}
				return
			}
		}
		state.events = append(state.events, InteractionEvent{Kind: "completed", Target: channel.target.ID})
		state.channel = interactionChannel{}
		return
	}
	if !state.held || world.motion.action != InteractionAction || world.motion.dead || world.motion.suspended || (!channel.target.Moving && !world.stationary(player)) || targetDistance(world.state.Player.Bounds, channel.target.Bounds) > 48 {
		state.events = append(state.events, InteractionEvent{Kind: "cancelled", Target: channel.target.ID})
		state.channel = interactionChannel{}
		world.limbs.interactHeld = false
		if world.motion.action == InteractionAction {
			world.motion.action = FreeAction
		}
	}
}

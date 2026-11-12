// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"slices"

	"too-broke-to-die/internal/tbtd/content"
)

// ObjectKind distinguishes authored puzzle objects from decoration.
type ObjectKind string

const (
	// ObjectCrate is a grounded, pushable pressure-plate weight.
	ObjectCrate ObjectKind = "crate"
	// ObjectTrolley is a grounded, pushable service load.
	ObjectTrolley ObjectKind = "trolley"
	// ObjectPlate detects the player, a crate or a settled limb.
	ObjectPlate ObjectKind = "plate"
	// ObjectGate blocks passage unless its plate or latch opens it.
	ObjectGate ObjectKind = "gate"
	// ObjectPaddle is a marked, latchable limb-projectile target.
	ObjectPaddle ObjectKind = "paddle"
	// ObjectBreakable blocks passage until its staged damage breaks it.
	ObjectBreakable ObjectKind = "breakable"
	// ObjectDecoration is visual only and never blocks movement.
	ObjectDecoration ObjectKind = "decoration"
	// ObjectProp triggers a bounded physical reaction on contact.
	ObjectProp ObjectKind = "prop"
)

// PuzzleObject is transient, except CertifiedOpen, which must come from an
// acknowledged campaign transition. Bounds and Origin use world coordinates.
type PuzzleObject struct {
	ID            string
	Kind          ObjectKind
	Bounds        Rect
	Origin        Rect
	Reachable     Rect
	Required      bool
	CertifiedOpen bool
	PermanentOpen bool
	Plate         string
	Damage        int
	Broken        bool
	Active        bool
	Latched       bool
	// Restoring means the object is at its origin but temporarily nonblocking
	// while its collision volume is occupied.
	Restoring bool
	LostTicks int
	Ticket    content.DeedID
}

type objectState struct {
	items        []PuzzleObject
	interactions []InteractionTarget
	channel      interactionChannel
	held         bool
	events       []InteractionEvent
	invalid      string
	reactions    reactionState
}

func cloneObjects(state objectState) objectState {
	state.items = slices.Clone(state.items)
	state.interactions = slices.Clone(state.interactions)
	state.events = slices.Clone(state.events)
	state.reactions = cloneReactions(state.reactions)
	return state
}

// AddPuzzleObject installs an explicitly marked object. Decoration never
// participates in collision or evidence. Origins are fixed for attempt reset.
func (world *World) AddPuzzleObject(ctx context.Context, object PuzzleObject) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil || object.ID == "" || !validRect(object.Bounds, false) || !validRect(object.Origin, false) || !validRect(object.Reachable, false) || object.Damage != 0 || (object.Broken && !object.CertifiedOpen) || object.LostTicks != 0 || object.Active || object.Latched || object.Restoring {
		return fmt.Errorf("invalid puzzle object")
	}
	if object.Required && !inside(object.Reachable, object.Origin) {
		return fmt.Errorf("required origin is outside reachable bounds")
	}
	switch object.Kind {
	case ObjectCrate, ObjectTrolley, ObjectPlate, ObjectGate, ObjectPaddle, ObjectBreakable, ObjectDecoration, ObjectProp:
	default:
		return fmt.Errorf("invalid object kind %q", object.Kind)
	}
	if object.CertifiedOpen {
		if object.Kind != ObjectBreakable || object.Ticket == "" || !slices.Contains(world.state.Receipts, object.Ticket) {
			return fmt.Errorf("certified opening requires its committed receipt")
		}
		object.Broken = true
	}
	if object.PermanentOpen && (object.Kind != ObjectGate || !slices.Contains(world.state.ItemRoutes, content.ObjectID(object.ID))) {
		return fmt.Errorf("permanent gate opening requires a committed route")
	}
	if object.Kind == ObjectGate {
		found := false
		for _, existing := range world.objects.items {
			found = found || (existing.ID == object.Plate && existing.Kind == ObjectPlate)
		}
		if !found && !object.PermanentOpen {
			return fmt.Errorf("gate requires a marked plate")
		}
		object.Active = object.PermanentOpen
	}
	for _, existing := range world.objects.items {
		if existing.ID == object.ID {
			return fmt.Errorf("duplicate object %s", object.ID)
		}
	}
	for _, collider := range world.grid.colliders {
		if collider.ID == objectColliderID(object.ID) {
			return fmt.Errorf("duplicate object collider %s", object.ID)
		}
	}
	for _, platform := range world.state.Platforms {
		if platform.Collider.ID == objectColliderID(object.ID) {
			return fmt.Errorf("duplicate object collider %s", object.ID)
		}
	}
	world.objects.items = append(world.objects.items, object)
	slices.SortFunc(world.objects.items, func(a, b PuzzleObject) int { return compareID(a.ID, b.ID) })
	world.syncObjectPlatforms()
	return nil
}

func objectColliderID(id string) string { return "object:" + id }

func objectBlocks(object PuzzleObject) bool {
	return !object.Restoring && (object.Kind == ObjectCrate || object.Kind == ObjectTrolley || (object.Kind == ObjectBreakable && !object.Broken) || (object.Kind == ObjectGate && !object.Active))
}

// objectCollider is shared by live flight and previews through the world's
// existing dynamic-collider collection. Paddles are targets, not solid bodies.
func objectCollider(object PuzzleObject) (Collider, bool) {
	kind := Solid
	if object.Kind == ObjectPaddle && !object.Latched {
		kind = ProjectileTarget
	} else if !objectBlocks(object) {
		return Collider{}, false
	}
	return Collider{ID: objectColliderID(object.ID), Bounds: object.Bounds, Kind: kind}, true
}

// Objects returns an isolated, identity-ordered copy of transient object state.
func (world *World) Objects(ctx context.Context) []PuzzleObject {
	defer mon.Task()(&ctx)(nil)
	if world == nil {
		return nil
	}
	return slices.Clone(world.objects.items)
}

// LatchPlate keeps an already weighted plate active until attempt reset.
func (world *World) LatchPlate(ctx context.Context, id string) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil {
		return fmt.Errorf("missing world")
	}
	for i := range world.objects.items {
		object := &world.objects.items[i]
		if object.ID == id && object.Kind == ObjectPlate && object.Active {
			object.Latched = true
			for j := range world.objects.items {
				gate := &world.objects.items[j]
				if gate.Kind == ObjectGate && gate.Plate == id {
					gate.Latched, gate.Active = true, true
				}
			}
			world.syncObjectPlatforms()
			return nil
		}
	}
	return fmt.Errorf("plate is not active: %s", id)
}

func (world *World) latchGate(ctx context.Context, plateID string) (err error) {
	defer mon.Task()(&ctx)(&err)
	weighted := false
	for _, plate := range world.objects.items {
		if plate.ID == plateID && plate.Kind == ObjectPlate && plate.Active {
			weighted = true
		}
	}
	if !weighted {
		return fmt.Errorf("plate is not active: %s", plateID)
	}
	latched := false
	for i := range world.objects.items {
		gate := &world.objects.items[i]
		if gate.Kind == ObjectGate && gate.Plate == plateID {
			gate.Latched, gate.Active = true, true
			latched = true
		}
	}
	if !latched {
		return fmt.Errorf("no gate for plate: %s", plateID)
	}
	world.syncObjectPlatforms()
	return nil
}

// ResetObjects restores temporary puzzle state for an explicit attempt reset.
// Durable certified openings remain open and campaign records are untouched.
func (world *World) ResetObjects(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	if world == nil {
		return
	}
	for i := range world.objects.items {
		object := &world.objects.items[i]
		object.Bounds = object.Origin
		object.Active, object.Latched = object.PermanentOpen, false
		object.LostTicks, object.Damage = 0, 0
		object.Broken = object.CertifiedOpen
		object.Restoring = false
		object.Restoring = object.Kind != ObjectGate && objectBlocks(*object)
	}
	world.resolveRestorations(ctx)
	world.deferOccupiedGateClosures()
	if world.objects.channel.target.ID != "" && world.motion.action == InteractionAction {
		world.motion.action = FreeAction
	}
	world.objects.channel = interactionChannel{}
	world.objects.held = false
	world.limbs.interactHeld = false
	world.syncObjectEntities()
	world.syncObjectPlatforms()
}

// DamageBreakable applies an authoritative attack to a marked obstacle. A
// visible damage stage precedes the second hit; debris is never a collider.
func (world *World) DamageBreakable(ctx context.Context, id string) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil {
		return fmt.Errorf("missing world")
	}
	for i := range world.objects.items {
		object := &world.objects.items[i]
		if object.ID == id && object.Kind == ObjectBreakable && !object.Broken {
			object.Damage++
			if object.Damage >= 2 {
				object.Broken = true
			}
			world.syncObjectPlatforms()
			return nil
		}
	}
	return fmt.Errorf("breakable not available: %s", id)
}

func inside(outer, inner Rect) bool {
	return inner.X >= outer.X && inner.Y >= outer.Y && inner.X+inner.Width <= outer.X+outer.Width && inner.Y+inner.Height <= outer.Y+outer.Height
}

// occupiedVolume checks bodies and existing blocking geometry before making a
// restored object or closing gate solid. Restoring objects can be included for
// gates so a gate cannot close over an object awaiting its own solid volume.
func (world *World) occupiedVolume(box Rect, excluded string, includeRestoring bool) bool {
	if world.state.HasPlayer && !world.motion.dead && overlaps(box, world.state.Player.Bounds) {
		return true
	}
	for _, enemy := range world.combat.enemies {
		if enemy.Health > 0 && overlaps(box, enemy.Body.Bounds) {
			return true
		}
	}
	for _, civilian := range world.combat.civilians {
		if !civilian.Incapacitated && overlaps(box, civilian.Bounds) {
			return true
		}
	}
	for _, collider := range world.grid.colliders {
		if collider.Kind != ProjectileTarget && overlaps(box, collider.Bounds) {
			return true
		}
	}
	for _, platform := range world.state.Platforms {
		if !world.isObjectPlatform(platform.Collider.ID) && platform.Collider.Kind != ProjectileTarget && overlaps(box, platform.Collider.Bounds) {
			return true
		}
	}
	for _, other := range world.objects.items {
		if other.ID != excluded && (objectBlocks(other) || (includeRestoring && other.Restoring)) && overlaps(box, other.Bounds) {
			return true
		}
	}
	return false
}

// recoveryMoveClear checks the full displacement before moving a body out of a
// restored object's origin. It never crosses terrain or a lethal volume.
func (world *World) recoveryMoveClear(body Rect, delta Vector, objectID, actorID string) bool {
	destination := moved(body, delta)
	if !validRect(destination, false) || world.crossedLethal([]movementSegment{{from: body, to: destination}}) {
		return false
	}
	blocked := func(collider Collider) bool {
		if collider.Kind == ProjectileTarget {
			return false
		}
		if overlaps(destination, collider.Bounds) {
			return true
		}
		hit, found := impactTime(body, delta, collider)
		return found && hit.Time < 1
	}
	for _, collider := range world.grid.colliders {
		if blocked(collider) {
			return false
		}
	}
	for _, platform := range world.state.Platforms {
		if !world.isObjectPlatform(platform.Collider.ID) && blocked(platform.Collider) {
			return false
		}
	}
	for _, object := range world.objects.items {
		if object.ID != objectID && objectBlocks(object) && blocked(Collider{ID: objectColliderID(object.ID), Kind: Solid, Bounds: object.Bounds}) {
			return false
		}
	}
	if actorID != "player" && world.state.HasPlayer && !world.motion.dead && overlaps(destination, world.state.Player.Bounds) {
		return false
	}
	for _, enemy := range world.combat.enemies {
		if actorID != "enemy:"+enemy.ID && enemy.Health > 0 && overlaps(destination, enemy.Body.Bounds) {
			return false
		}
	}
	for _, civilian := range world.combat.civilians {
		if actorID != "civilian:"+civilian.ID && !civilian.Incapacitated && overlaps(destination, civilian.Bounds) {
			return false
		}
	}
	return true
}

func (world *World) recoveryPosition(body Rect, object PuzzleObject, actorID string) (Rect, bool) {
	box := object.Bounds
	// Equal distances prefer left, right, up, then down, independent of maps.
	moves := []Vector{
		{X: box.X - (body.X + body.Width)},
		{X: box.X + box.Width - body.X},
		{Y: box.Y + box.Height - body.Y},
		{Y: box.Y - (body.Y + body.Height)},
	}
	slices.SortStableFunc(moves, func(a, b Vector) int {
		da, db := math.Abs(a.X)+math.Abs(a.Y), math.Abs(b.X)+math.Abs(b.Y)
		if da < db {
			return -1
		}
		if da > db {
			return 1
		}
		return 0
	})
	for _, delta := range moves {
		destination := moved(body, delta)
		if !overlaps(destination, box) && world.recoveryMoveClear(body, delta, object.ID, actorID) {
			return destination, true
		}
	}
	return body, false
}

func (world *World) resolveRestorations(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	for i := range world.objects.items {
		object := &world.objects.items[i]
		if !object.Restoring {
			continue
		}
		if world.state.HasPlayer && !world.motion.dead && overlaps(world.state.Player.Bounds, object.Bounds) {
			if position, ok := world.recoveryPosition(world.state.Player.Bounds, *object, "player"); ok {
				world.state.Player.Bounds = position
				world.state.Player.Velocity = Vector{}
				world.state.Player.Support = ""
			}
		}
		for j := range world.combat.enemies {
			enemy := &world.combat.enemies[j]
			if enemy.Health <= 0 || !overlaps(enemy.Body.Bounds, object.Bounds) {
				continue
			}
			if position, ok := world.recoveryPosition(enemy.Body.Bounds, *object, "enemy:"+enemy.ID); ok && (!world.protected(position) || world.protected(enemy.Body.Bounds)) {
				enemy.Body.Bounds = position
				enemy.Body.Velocity = Vector{}
				enemy.Body.Support = ""
				world.syncRecoveryActor(enemy.ID, position)
			}
		}
		for j := range world.combat.civilians {
			civilian := &world.combat.civilians[j]
			if civilian.Incapacitated || !overlaps(civilian.Bounds, object.Bounds) {
				continue
			}
			if position, ok := world.recoveryPosition(civilian.Bounds, *object, "civilian:"+civilian.ID); ok {
				civilian.Bounds = position
				world.syncRecoveryActor(civilian.ID, position)
			}
		}
		if !world.occupiedVolume(object.Bounds, object.ID, false) {
			object.Restoring = false
		}
	}
}

// syncRecoveryActor keeps the presentation snapshot aligned with a body moved
// out of an occupied origin. Actor definitions use their bottom-center point.
func (world *World) syncRecoveryActor(id string, bounds Rect) {
	for i := range world.state.Entities {
		if string(world.state.Entities[i].ID) == id {
			world.state.Entities[i].Position = Vector{X: centerX(bounds), Y: bounds.Y}
			return
		}
	}
}

// An unlatched gate waits open until its volume is empty. Unlike a crusher it
// cannot materialize over a player, civilian, enemy, or movable puzzle object.
func (world *World) deferOccupiedGateClosures() {
	for i := range world.objects.items {
		gate := &world.objects.items[i]
		if gate.Kind == ObjectGate && !gate.Active && world.occupiedVolume(gate.Bounds, gate.ID, true) {
			gate.Active = true
		}
	}
}

func (world *World) updateObjects(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	for i := range world.objects.items {
		object := &world.objects.items[i]
		if object.Required && !inside(object.Reachable, object.Bounds) {
			object.LostTicks++
			if object.LostTicks >= 240 {
				object.Bounds = object.Origin
				object.LostTicks = 0
				if object.Kind != ObjectGate && objectBlocks(*object) {
					object.Restoring = true
				}
			}
		} else {
			object.LostTicks = 0
		}
		if object.Kind != ObjectPlate {
			continue
		}
		active := world.state.Player.Support != "" && overlaps(object.Bounds, world.state.Player.Bounds)
		for _, other := range world.objects.items {
			if other.Kind == ObjectCrate && overlaps(object.Bounds, other.Bounds) {
				active = true
			}
		}
		weights, weightErr := world.LimbWeights(ctx, object.Bounds)
		if weightErr != nil {
			return weightErr
		}
		object.Active = active || len(weights) != 0 || object.Latched
	}
	for i := range world.objects.items {
		gate := &world.objects.items[i]
		if gate.Kind != ObjectGate {
			continue
		}
		gate.Active = gate.Latched || gate.PermanentOpen
		for _, plate := range world.objects.items {
			if plate.Kind == ObjectPlate && plate.ID == gate.Plate && plate.Active {
				gate.Active = true
			}
		}
	}
	world.resolveRestorations(ctx)
	world.deferOccupiedGateClosures()
	world.syncObjectEntities()
	world.syncObjectPlatforms()
	return nil
}

func (world *World) isObjectPlatform(id string) bool {
	for _, object := range world.objects.items {
		if objectColliderID(object.ID) == id {
			return true
		}
	}
	return false
}

// syncObjectPlatforms exposes the same live collision shapes to ordinary
// world sweeps, throw previews and the next authoritative tick.
func (world *World) syncObjectPlatforms() {
	platforms := make([]Platform, 0, len(world.state.Platforms)+len(world.objects.items))
	for _, platform := range world.state.Platforms {
		if !world.isObjectPlatform(platform.Collider.ID) {
			platforms = append(platforms, platform)
		}
	}
	for _, object := range world.objects.items {
		if collider, ok := objectCollider(object); ok {
			platforms = append(platforms, Platform{Collider: collider})
		}
	}
	slices.SortFunc(platforms, func(a, b Platform) int { return compareID(a.Collider.ID, b.Collider.ID) })
	world.state.Platforms = platforms
}

// syncObjectEntities keeps the established interpolation snapshots consistent
// with the separately exposed object state.
func (world *World) syncObjectEntities() {
	for i := range world.state.Entities {
		entity := &world.state.Entities[i]
		for _, object := range world.objects.items {
			if string(entity.ID) == object.ID {
				entity.Position.X = centerX(object.Bounds)
				if object.Kind == ObjectGate {
					entity.Position.X = object.Bounds.X
				}
				entity.Position.Y = object.Bounds.Y
				if object.Kind == ObjectPaddle {
					entity.Position.Y = centerY(object.Bounds)
				}
				break
			}
		}
	}
}

// objectPlatforms gives the existing swept solver the live blocking objects.
// Only grounded contact from behind applies a bounded pushing velocity.
func (world *World) objectPlatforms(ctx context.Context, move float64) (_ []Platform, err error) {
	defer mon.Task()(&ctx)(&err)
	platforms := make([]Platform, 0, len(world.state.Platforms))
	for _, platform := range world.state.Platforms {
		if !world.isObjectPlatform(platform.Collider.ID) {
			platforms = append(platforms, platform)
		}
	}
	body := world.state.Player
	for _, object := range world.objects.items {
		if collider, ok := objectCollider(object); ok {
			platforms = append(platforms, Platform{Collider: collider})
		}
	}
	for i := range platforms {
		platform := &platforms[i]
		var object *PuzzleObject
		for j := range world.objects.items {
			if objectColliderID(world.objects.items[j].ID) == platform.Collider.ID {
				object = &world.objects.items[j]
				break
			}
		}
		if object == nil || (object.Kind != ObjectCrate && object.Kind != ObjectTrolley) || body.Support == "" || body.Velocity.Y > 0 || move == 0 || world.motion.action != FreeAction {
			continue
		}
		box := object.Bounds
		vertical := body.Bounds.Y < box.Y+box.Height && body.Bounds.Y+body.Bounds.Height > box.Y
		gap := box.X - (body.Bounds.X + body.Bounds.Width)
		if move < 0 {
			gap = body.Bounds.X - (box.X + box.Width)
		}
		if !vertical || gap < -Tolerance || gap > 1 {
			continue
		}
		delta := Vector{X: math.Max(-1, math.Min(1, move)) * 100 * TickSeconds}
		others := otherPlatforms(platforms, platform.Collider.ID, "")
		support, supportErr := world.grid.Support(ctx, box, others, "")
		if supportErr != nil {
			return nil, supportErr
		}
		if support == "" {
			continue
		}
		hit, found, sweepErr := world.grid.Sweep(ctx, box, delta, others, SweepOptions{})
		if sweepErr != nil {
			return nil, sweepErr
		}
		if found {
			delta.X *= hit.Time
		}
		platform.Velocity.X = delta.X / TickSeconds
	}
	return platforms, nil
}

func (world *World) absorbObjectPlatforms() {
	platforms := world.state.Platforms[:0]
	for _, platform := range world.state.Platforms {
		matched := false
		for i := range world.objects.items {
			object := &world.objects.items[i]
			if objectColliderID(object.ID) == platform.Collider.ID {
				object.Bounds = platform.Collider.Bounds
				matched = true
				break
			}
		}
		if !matched {
			platforms = append(platforms, platform)
		}
	}
	world.state.Platforms = platforms
}

func (world *World) resolveObjectImpacts(ctx context.Context, before limbState) (err error) {
	defer mon.Task()(&ctx)(&err)
	attack := world.combat.attack
	if attack.ID != 0 && !attack.Throw && attack.Phase == PhaseActive {
		body := world.state.Player.Bounds
		area := Rect{X: body.X + body.Width, Y: body.Y + (body.Height-36)/2, Width: attack.Reach, Height: 36}
		if attack.Facing < 0 {
			area.X = body.X - attack.Reach
		}
		for i := range world.objects.items {
			object := &world.objects.items[i]
			key := objectColliderID(object.ID)
			if object.Kind != ObjectBreakable || object.Broken || world.combat.hitTargets[key] || !overlaps(area, object.Bounds) {
				continue
			}
			clear, clearErr := world.strikeClear(ctx, body, area, object.Bounds, attack.Facing)
			if clearErr != nil {
				return clearErr
			}
			if clear {
				world.combat.hitTargets[key] = true
				if err = world.DamageBreakable(ctx, object.ID); err != nil {
					return err
				}
			}
		}
	}
	// A limb can hit a solid once while damaging; later bounces are only
	// decorative. A newly released limb can also impact on its release tick.
	damaging := make(map[string]bool, len(before.objects))
	for _, limb := range before.objects {
		if limb.Generation == before.generation && limb.Airborne && limb.Damaging {
			damaging[limb.ID] = true
		}
	}
	for _, event := range world.limbs.events {
		if event.Kind == "released" {
			damaging[event.Object] = true
			continue
		}
		if event.Kind != "impact" {
			continue
		}
		validImpact := damaging[event.Object]
		delete(damaging, event.Object)
		if !validImpact {
			continue
		}
		for i := range world.objects.items {
			object := &world.objects.items[i]
			if event.Target != objectColliderID(object.ID) {
				continue
			}
			switch object.Kind {
			case ObjectBreakable:
				if !object.Broken {
					if err = world.DamageBreakable(ctx, object.ID); err != nil {
						return err
					}
				}
			case ObjectPaddle:
				index := world.limbIndex(event.Object)
				if index < 0 || object.Latched {
					continue
				}
				limb := &world.limbs.objects[index]
				object.Active, object.Latched = true, true
				world.objects.events = append(world.objects.events, InteractionEvent{Kind: "paddle_latched", Target: object.ID, Position: limb.Position})
				// The paddle is a projectile target, not a support surface.
				// Sweep a tiny body so only real supporting terrain catches it.
				impact, found, sweepErr := world.Sweep(ctx, Rect{X: limb.Position.X, Y: limb.Position.Y, Width: Tolerance, Height: Tolerance}, Vector{Y: -10000}, SweepOptions{})
				if sweepErr != nil {
					return sweepErr
				}
				if found {
					limb.Position.Y -= 10000 * impact.Time
					limb.Position.Y += Tolerance
					limb.Airborne, limb.Damaging = false, false
					limb.Velocity = Vector{}
				}
			}
		}
	}
	return nil
}

func (world *World) installAuthoredObjects(ctx context.Context, bounds Rect) (err error) {
	defer mon.Task()(&ctx)(&err)
	for _, entity := range world.state.Entities {
		var kind ObjectKind
		width, height := 48.0, 32.0
		switch entity.Role {
		case content.RoleCrate:
			kind = ObjectCrate
		case content.RoleTrolley:
			kind, width, height = ObjectTrolley, 64, 40
		case content.RolePlate:
			kind, width, height = ObjectPlate, 64, 4
		case content.RolePaddle:
			kind, width, height = ObjectPaddle, 64, 64
		default:
			continue
		}
		box := Rect{X: entity.Position.X - width/2, Y: entity.Position.Y, Width: width, Height: height}
		if kind == ObjectPaddle {
			box.Y -= height / 2
		}
		object := PuzzleObject{ID: string(entity.ID), Kind: kind, Bounds: box, Origin: box, Reachable: bounds, Required: true, Ticket: entity.Ticket}
		if err = world.AddPuzzleObject(ctx, object); err != nil {
			return err
		}
	}
	var plateID string
	for _, object := range world.objects.items {
		if object.Kind == ObjectPlate {
			plateID = object.ID
		}
	}
	for _, entity := range world.state.Entities {
		if entity.Role != content.RoleGate {
			continue
		}
		permanent := slices.Contains(world.state.ItemRoutes, entity.ID)
		if plateID == "" && !permanent {
			continue
		}
		box := Rect{X: entity.Position.X, Y: entity.Position.Y, Width: 16, Height: 128}
		object := PuzzleObject{ID: string(entity.ID), Kind: ObjectGate, Bounds: box, Origin: box, Reachable: bounds, Plate: plateID, PermanentOpen: permanent, Ticket: entity.Ticket}
		if err = world.AddPuzzleObject(ctx, object); err != nil {
			return err
		}
	}
	return nil
}

// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"too-broke-to-die/internal/tbtd/content"
)

func (world *World) rescueWeighted() bool {
	var plate Rect
	found := false
	for _, object := range world.objects.items {
		if object.ID == string(world.service.roles[content.RolePlate]) && object.Kind == ObjectPlate {
			plate, found = object.Bounds, true
			break
		}
	}
	if !found {
		return false
	}
	for _, object := range world.objects.items {
		if object.ID == string(world.service.roles[content.RoleCrate]) && object.Kind == ObjectCrate && overlaps(plate, object.Bounds) {
			return true
		}
	}
	for _, limb := range world.limbs.objects {
		if limb.Generation == world.limbs.generation && !limb.Carried && !limb.Airborne && limb.Position.X >= plate.X && limb.Position.X <= plate.X+plate.Width && limb.Position.Y >= plate.Y && limb.Position.Y <= plate.Y+plate.Height {
			return true
		}
	}
	return false
}

func (world *World) rescueGateLatched() bool {
	for _, object := range world.objects.items {
		if object.ID == string(world.service.roles[content.RoleGate]) && object.Kind == ObjectGate {
			return object.Latched
		}
	}
	return false
}

func (world *World) rescueInteraction(target string, hazards []bool) {
	service := &world.service
	if target != string(service.roles[content.RoleActor]) || service.refuge {
		return
	}
	if !world.rescueGateLatched() {
		service.invalid = "latch_gate_before_following"
		return
	}
	if service.waiting {
		if len(hazards) != 0 && service.waitIndex < len(hazards) && hazards[service.waitIndex] {
			service.invalid = "wait_for_hazard"
			return
		}
		service.waiting = false
		service.waitIndex++
	}
	service.following = true
}

// rescueStep sweeps the adult against solid route geometry. Variant cover is
// authored to block players and projectiles, not the rescue adult or trolley.
func (world *World) rescueStep(box Rect, distance float64) float64 {
	delta := Vector{X: distance}
	limit := 1.0
	for _, collider := range world.grid.colliders {
		if collider.Kind != Solid || collider.ID == "variant-cover" {
			continue
		}
		if hit, found := impactTime(box, delta, collider); found && hit.Time < limit {
			limit = hit.Time
		}
	}
	for _, object := range world.objects.items {
		if !objectBlocks(object) {
			continue
		}
		collider := Collider{ID: objectColliderID(object.ID), Kind: Solid, Bounds: object.Bounds}
		if hit, found := impactTime(box, delta, collider); found && hit.Time < limit {
			limit = hit.Time
		}
	}
	for _, platform := range world.state.Platforms {
		collider := platform.Collider
		if world.isObjectPlatform(collider.ID) || collider.Kind != Solid {
			continue
		}
		if hit, found := impactTime(box, delta, collider); found && hit.Time < limit {
			limit = hit.Time
		}
	}
	return distance * limit
}

func (world *World) advanceRescue(hazards []bool) {
	service := &world.service
	for i := range world.combat.civilians {
		adult := &world.combat.civilians[i]
		if adult.ID != string(service.roles[content.RoleActor]) {
			continue
		}
		if adult.Incapacitated && !service.refuge {
			world.failService()
			return
		}
		if service.refuge {
			return
		}
		for index, hazard := range service.hazards {
			if len(hazards) > index && hazards[index] && overlaps(adult.Bounds, rectFromContent(hazard.Bounds)) {
				adult.Incapacitated = true
				world.failService()
				return
			}
		}
		if !service.following || service.waiting || !world.rescueGateLatched() {
			return
		}
		for service.waitIndex < len(service.hazards) && adult.Bounds.X >= service.hazards[service.waitIndex].Bounds.X {
			service.waitIndex++
		}
		step := 100 * TickSeconds
		if service.waitIndex < len(service.hazards) {
			boundary := service.hazards[service.waitIndex].Waiting.X + service.hazards[service.waitIndex].Waiting.Width
			if adult.Bounds.X+adult.Bounds.Width+step >= boundary {
				step = max(0, boundary-adult.Bounds.X-adult.Bounds.Width)
				service.waiting = true
			}
		}
		adult.Bounds.X += world.rescueStep(adult.Bounds, step)
		world.syncRecoveryActor(adult.ID, adult.Bounds)
		for _, entity := range world.state.Entities {
			if entity.ID == service.roles[content.RoleRefuge] && adult.Bounds.X >= entity.Position.X {
				service.refuge, service.following, service.waiting = true, false, false
				// The protected adult remains in the entity view, but no longer
				// competes with the refuge terminal for interaction priority.
				world.combat.civilians = append(world.combat.civilians[:i], world.combat.civilians[i+1:]...)
				return
			}
		}
		return
	}
}

// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"

	"too-broke-to-die/internal/tbtd/content"
)

// serviceTrolleyCollider identifies the active request's moving platform.
func (world *World) serviceTrolleyCollider(id string) bool {
	return world.service.deed.Family == "SUP" && world.service.status == ServiceActive && id == objectColliderID(string(world.service.roles[content.RoleTrolley]))
}

// trolleyCatwalkPush reports an adjacent grounded push from the SUP catwalk.
func (world *World) trolleyCatwalkPush(move float64) bool {
	body := world.state.Player
	if move == 0 || body.Support == "" || body.Velocity.Y > 0 || world.motion.action != FreeAction {
		return false
	}
	for _, object := range world.objects.items {
		if object.Kind != ObjectTrolley || !world.serviceTrolleyCollider(objectColliderID(object.ID)) {
			continue
		}
		box := object.Bounds
		if body.Bounds.Y < box.Y+box.Height || body.Bounds.Y-(box.Y+box.Height) > 40+Tolerance {
			return false
		}
		gap := box.X - (body.Bounds.X + body.Bounds.Width)
		if move < 0 {
			gap = body.Bounds.X - (box.X + box.Width)
		}
		return gap >= -Tolerance && gap <= 1
	}
	return false
}

// moveCatwalkTrolley uses the ordinary swept solver with only the authored
// player/projectile cover excluded. Player and projectile sweeps keep the
// original grid throughout the tick.
func (world *World) moveCatwalkTrolley(ctx context.Context, displacement float64) (err error) {
	defer mon.Task()(&ctx)(&err)
	if displacement == 0 {
		return nil
	}
	displacement = math.Max(-100*TickSeconds, math.Min(100*TickSeconds, displacement))
	for i := range world.objects.items {
		object := &world.objects.items[i]
		if object.Kind != ObjectTrolley || !world.serviceTrolleyCollider(objectColliderID(object.ID)) {
			continue
		}
		delta := Vector{X: displacement}
		colliders := otherPlatforms(world.state.Platforms, objectColliderID(object.ID), "")
		hit, found, sweepErr := world.grid.Sweep(ctx, object.Bounds, delta, colliders, SweepOptions{Ignore: "variant-cover"})
		if sweepErr != nil {
			return fmt.Errorf("trolley sweep: %w", sweepErr)
		}
		if found {
			delta.X *= hit.Time
		}
		object.Bounds = moved(object.Bounds, delta)
		world.syncObjectEntities()
		world.syncObjectPlatforms()
		return nil
	}
	return fmt.Errorf("active trolley missing")
}

func (world *World) supplyReady() bool {
	service := &world.service
	var pad Vector
	found := false
	for _, entity := range world.state.Entities {
		if entity.ID == service.roles[content.RolePad] {
			pad, found = entity.Position, true
			break
		}
	}
	if !found {
		return false
	}
	for _, object := range world.objects.items {
		if object.ID == string(service.roles[content.RoleTrolley]) && object.Kind == ObjectTrolley && object.Ticket == service.attempt.Deed && !object.Restoring {
			return centerX(object.Bounds) >= pad.X && object.Bounds.Y <= pad.Y+Tolerance && object.Bounds.Y >= pad.Y-Tolerance
		}
	}
	return false
}

func (world *World) supplyCrusher() {
	service := &world.service
	if len(service.hazards) == 0 || !service.hazards[0].Lethal {
		return
	}
	crusher := rectFromContent(service.hazards[0].Bounds)
	for _, trolley := range world.objects.items {
		if trolley.ID == string(service.roles[content.RoleTrolley]) && trolley.Ticket == service.attempt.Deed && trolley.Kind == ObjectTrolley && overlaps(trolley.Bounds, crusher) {
			service.sealBroken = true
			world.failService()
			return
		}
	}
}

// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
)

// fecesStep is the actual fixed-tick integrator used for both preview and flight.
func fecesStep(velocity *Vector) Vector {
	velocity.Y -= fecesGravity * TickSeconds
	return Vector{X: velocity.X * TickSeconds, Y: velocity.Y * TickSeconds}
}

// ThrowPreview contains independent points and the first blocking impact.
type ThrowPreview struct {
	Points []Vector
	Impact *Impact
	Kind   string
}

// PreviewThrow traces the feces trajectory, without tracking or moving targets.
// Neutral aim retains the most recently sampled direction, then facing.
func (world *World) PreviewThrow(ctx context.Context, aim Vector, targets []FecesTarget) (_ ThrowPreview, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil || !world.state.HasPlayer || !validVector(aim) || math.Abs(aim.X) > 1 || math.Abs(aim.Y) > 1 {
		return ThrowPreview{}, fmt.Errorf("invalid throw preview")
	}
	if aim == (Vector{}) {
		aim = world.combat.aim
	}
	if aim == (Vector{}) {
		aim = Vector{X: float64(world.motion.facing)}
	}
	length := math.Hypot(aim.X, aim.Y)
	if length == 0 {
		return ThrowPreview{}, fmt.Errorf("invalid throw direction")
	}
	velocity := Vector{X: aim.X * fecesSpeed / length, Y: aim.Y * fecesSpeed / length}
	body := world.state.Player.Bounds
	position := Vector{X: centerX(body), Y: centerY(body)}
	preview := ThrowPreview{Points: []Vector{position}}
	colliders := world.limbTargets(nil)
	allTargets := append(append([]FecesTarget(nil), world.feces.targets...), targets...)
	for _, target := range allTargets {
		if target.Collider.ID == "" || target.Collider.Kind != ProjectileTarget || !validRect(target.Collider.Bounds, false) {
			return ThrowPreview{}, fmt.Errorf("invalid feces target")
		}
		colliders = append(colliders, target.Collider)
	}
	distance := 0.0
	for i := 0; i < 240 && distance < 600; i++ {
		delta := fecesStep(&velocity)
		step := math.Hypot(delta.X, delta.Y)
		if step > 600-distance {
			ratio := (600 - distance) / step
			delta.X *= ratio
			delta.Y *= ratio
			step = 600 - distance
		}
		impact, found, sweepErr := world.SweepTargets(ctx, Rect{X: position.X, Y: position.Y}, delta, colliders, SweepOptions{Projectile: true})
		if sweepErr != nil {
			return ThrowPreview{}, sweepErr
		}
		if found {
			position.X += delta.X * impact.Time
			position.Y += delta.Y * impact.Time
			preview.Points = append(preview.Points, position)
			preview.Impact = &impact
			preview.Kind = "blocked"
			if impact.Collider.Kind != ProjectileTarget && impact.Normal.Y > 0 {
				preview.Kind = "floor"
			}
			for _, target := range allTargets {
				if target.Collider.ID == impact.Collider.ID {
					preview.Kind = string(target.Kind)
				}
			}
			for _, enemy := range world.combat.enemies {
				if impact.Collider.ID == "combat-enemy:"+enemy.ID {
					preview.Kind = "enemy"
				}
			}
			return preview, nil
		}
		position.X += delta.X
		position.Y += delta.Y
		preview.Points = append(preview.Points, position)
		distance += step
	}
	return preview, nil
}

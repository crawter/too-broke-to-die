// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
)

func platformColliders(platforms []Platform) []Collider {
	result := make([]Collider, 0, len(platforms))
	for _, platform := range platforms {
		result = append(result, platform.Collider)
	}
	return result
}

func otherPlatforms(platforms []Platform, excluded, drop string) []Collider {
	result := make([]Collider, 0, len(platforms))
	for _, platform := range platforms {
		if platform.Collider.ID != excluded && platform.Collider.ID != drop {
			result = append(result, platform.Collider)
		}
	}
	return result
}

func sameDisplacement(before, after Rect, delta Vector) bool {
	return math.Abs(after.X-before.X-delta.X) <= Tolerance && math.Abs(after.Y-before.Y-delta.Y) <= Tolerance
}

// contactBudget is shared by every actor collision resolved during one tick.
type contactBudget struct {
	remaining int
	limited   bool
}

func (budget *contactBudget) contact() bool {
	if budget.remaining == 0 {
		budget.limited = true
		return false
	}
	budget.remaining--
	return true
}

func (budget *contactBudget) move(ctx context.Context, grid *Grid, box Rect, delta Vector, dynamic []Collider, ignore string) (_ MoveResult, err error) {
	defer mon.Task()(&ctx)(&err)
	result, err := grid.moveWithLimit(ctx, box, delta, dynamic, ignore, budget.remaining)
	if err != nil {
		return MoveResult{}, err
	}
	budget.remaining -= len(result.Contacts)
	if result.Diagnostic != "" {
		budget.limited = true
	}
	return result, nil
}

// reversePlatform tries the opposite movement from the original position. A
// standing rider is carried only if the reverse path is also clear.
func (world *World) reversePlatform(ctx context.Context, index int, delta Vector, carry bool, budget *contactBudget) (err error) {
	defer mon.Task()(&ctx)(&err)
	platform := &world.state.Platforms[index]
	platform.Velocity.X = -platform.Velocity.X
	platform.Velocity.Y = -platform.Velocity.Y
	delta.X, delta.Y = -delta.X, -delta.Y
	others := otherPlatforms(world.state.Platforms, platform.Collider.ID, "")
	_, blocked, err := world.grid.Sweep(ctx, platform.Collider.Bounds, delta, others, SweepOptions{Ignore: platform.Collider.ID})
	if err != nil || blocked {
		return err
	}
	body := &world.state.Player
	if carry {
		others = otherPlatforms(world.state.Platforms, platform.Collider.ID, body.DropThrough)
		result, moveErr := budget.move(ctx, world.grid, body.Bounds, delta, others, body.DropThrough)
		if moveErr != nil {
			return moveErr
		}
		if budget.limited || !sameDisplacement(body.Bounds, result.Bounds, delta) {
			return nil
		}
		body.Bounds = result.Bounds
	} else if _, touched := impactTime(platform.Collider.Bounds, delta, Collider{ID: body.ID, Bounds: body.Bounds, Kind: Solid}); touched {
		return nil
	}
	platform.Collider.Bounds = moved(platform.Collider.Bounds, delta)
	return nil
}

// advancePlatforms runs before the actor's own movement. It reverses a
// non-crusher when carrying or pushing would trap the player. Platform motion
// is never added to the rider's own velocity.
func (world *World) advancePlatforms(ctx context.Context, budget *contactBudget) (err error) {
	defer mon.Task()(&ctx)(&err)
	body := &world.state.Player
	platforms := world.state.Platforms
	for i := range platforms {
		platform := &platforms[i]
		delta := Vector{X: platform.Velocity.X * TickSeconds, Y: platform.Velocity.Y * TickSeconds}
		if delta == (Vector{}) {
			continue
		}
		others := otherPlatforms(platforms, platform.Collider.ID, "")
		hit, blocked, sweepErr := world.grid.Sweep(ctx, platform.Collider.Bounds, delta, others, SweepOptions{Ignore: platform.Collider.ID})
		if sweepErr != nil {
			return sweepErr
		}
		if blocked && hit.Time < 1 {
			if platform.Crusher {
				delta.X *= hit.Time
				delta.Y *= hit.Time
			} else {
				carry := body.Support == platform.Collider.ID && body.DropThrough != platform.Collider.ID
				if err = world.reversePlatform(ctx, i, delta, carry, budget); err != nil {
					return err
				}
				if budget.limited {
					break
				}
				continue
			}
		}
		carry := body.Support == platform.Collider.ID && body.DropThrough != platform.Collider.ID
		push := Vector{}
		if carry {
			push = delta
		} else {
			var contact Impact
			var touched bool
			if platform.Collider.Kind == Solid {
				contact, touched = impactTime(platform.Collider.Bounds, delta, Collider{ID: body.ID, Bounds: body.Bounds, Kind: Solid})
			} else if body.DropThrough != platform.Collider.ID {
				// Sweep the stationary body relative to the platform's motion.
				// A one-way top can catch from below only if the body started above it.
				contact, touched = impactTime(body.Bounds, Vector{X: -delta.X, Y: -delta.Y}, platform.Collider)
			}
			if touched {
				if !budget.contact() {
					break
				}
				push = Vector{X: delta.X * (1 - contact.Time), Y: delta.Y * (1 - contact.Time)}
			}
		}
		if push != (Vector{}) {
			others = otherPlatforms(platforms, platform.Collider.ID, body.DropThrough)
			result, moveErr := budget.move(ctx, world.grid, body.Bounds, push, others, body.DropThrough)
			if moveErr != nil {
				return fmt.Errorf("platform %s: %w", platform.Collider.ID, moveErr)
			}
			if budget.limited {
				break
			}
			if !sameDisplacement(body.Bounds, result.Bounds, push) {
				if platform.Crusher {
					body.Crushed = true
					body.Bounds = result.Bounds
					platform.Collider.Bounds = moved(platform.Collider.Bounds, delta)
				} else if err = world.reversePlatform(ctx, i, delta, carry, budget); err != nil {
					return err
				}
				if budget.limited {
					break
				}
				continue
			}
			body.Bounds = result.Bounds
		}
		platform.Collider.Bounds = moved(platform.Collider.Bounds, delta)
	}
	return nil
}

func (world *World) validDrop(id string) bool {
	if id == "" {
		return true
	}
	if id != world.state.Player.Support && id != world.state.Player.DropThrough {
		return false
	}
	for _, collider := range world.grid.colliders {
		if collider.ID == id {
			return collider.Kind == OneWay
		}
	}
	for _, platform := range world.state.Platforms {
		if platform.Collider.ID == id {
			return platform.Collider.Kind == OneWay
		}
	}
	return false
}

// Step advances exactly one 1/120-second tick; errors leave the world unchanged.
func (world *World) Step(ctx context.Context, input TickInput) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil || !world.state.HasPlayer || world.state.Player.Crushed || !validVector(input.Velocity) || !world.validDrop(input.DropThrough) {
		return fmt.Errorf("invalid gameplay tick")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	before := cloneState(world.state)
	defer func() {
		if err != nil {
			world.state = before
		}
	}()
	world.state.Diagnostics = nil
	world.state.Player.Velocity = input.Velocity
	world.state.Player.DropThrough = input.DropThrough
	budget := contactBudget{remaining: maxBlockingContacts}
	if err = world.advancePlatforms(ctx, &budget); err != nil {
		return err
	}
	body := &world.state.Player
	if budget.limited {
		world.state.Diagnostics = append(world.state.Diagnostics, Diagnostic{Name: CollisionIterationLimit, Encounter: world.state.Encounter, Entity: body.ID})
		world.finishTick(before)
		return nil
	}
	if body.Crushed {
		body.Velocity = Vector{}
		body.Support = ""
		world.finishTick(before)
		return nil
	}
	delta := Vector{X: input.Velocity.X * TickSeconds, Y: input.Velocity.Y * TickSeconds}
	result, err := budget.move(ctx, world.grid, body.Bounds, delta, otherPlatforms(world.state.Platforms, "", input.DropThrough), input.DropThrough)
	if err != nil {
		return err
	}
	body.Bounds = result.Bounds
	for _, contact := range result.Contacts {
		if contact.Normal.X != 0 {
			body.Velocity.X = 0
		}
		if contact.Normal.Y != 0 {
			body.Velocity.Y = 0
		}
	}
	if result.Diagnostic != "" {
		world.state.Diagnostics = append(world.state.Diagnostics, Diagnostic{Name: result.Diagnostic, Encounter: world.state.Encounter, Entity: body.ID})
	}
	body.Support = ""
	if body.Velocity.Y <= 0 {
		body.Support, err = world.grid.Support(ctx, body.Bounds, otherPlatforms(world.state.Platforms, "", input.DropThrough), input.DropThrough)
		if err != nil {
			return err
		}
	}
	world.finishTick(before)
	return nil
}

// finishTick is measured by Step.
func (world *World) finishTick(before State) {
	for i := range world.state.Emergencies {
		emergency := &world.state.Emergencies[i]
		if emergency.Encounter == world.state.Encounter && emergency.WarningTicks > 0 {
			emergency.WarningTicks--
		}
	}
	world.state.Tick++
	world.previous = before
}

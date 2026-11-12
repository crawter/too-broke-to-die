// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
)

// SweepOptions selects projectile or body collision and one excluded collider.
type SweepOptions struct {
	Projectile bool
	Ignore     string
}

// Impact is the earliest hit along a complete displacement, with time in [0,1].
type Impact struct {
	Collider Collider
	Time     float64
	Normal   Vector
}

func eligible(c Collider, options SweepOptions) bool {
	if c.ID == options.Ignore {
		return false
	}
	if options.Projectile {
		return c.Kind != PlayerOnly
	}
	return c.Kind != ProjectileTarget
}

func interval(a0, a1, b0, b1, delta float64, point bool) (float64, float64, bool) {
	if delta == 0 {
		if point {
			return math.Inf(-1), math.Inf(1), a0 >= b0 && a0 <= b1
		}
		return math.Inf(-1), math.Inf(1), a1 > b0 && a0 < b1
	}
	if delta > 0 {
		return (b0 - a1) / delta, (b1 - a0) / delta, true
	}
	return (b1 - a0) / delta, (b0 - a1) / delta, true
}

// impactTime is a hot-loop helper; the enclosing sweep is instrumented.
func impactTime(box Rect, delta Vector, collider Collider) (Impact, bool) {
	target := collider.Bounds
	if collider.Kind == OneWay {
		top := target.Y + target.Height
		if delta.Y >= 0 || box.Y < top-Tolerance {
			return Impact{}, false
		}
		t := (top - box.Y) / delta.Y
		if t < -Tolerance || t > 1 {
			return Impact{}, false
		}
		t = math.Max(0, t)
		left, right := box.X+delta.X*t, box.X+box.Width+delta.X*t
		if right <= target.X || left >= target.X+target.Width {
			return Impact{}, false
		}
		return Impact{Collider: collider, Time: t, Normal: Vector{Y: 1}}, true
	}
	pointInside := box.Width == 0 && box.Height == 0 && box.X > target.X && box.X < target.X+target.Width && box.Y > target.Y && box.Y < target.Y+target.Height
	if overlaps(box, target) || pointInside {
		return Impact{Collider: collider, Time: 0}, true
	}
	x0, x1, xok := interval(box.X, box.X+box.Width, target.X, target.X+target.Width, delta.X, box.Width == 0)
	y0, y1, yok := interval(box.Y, box.Y+box.Height, target.Y, target.Y+target.Height, delta.Y, box.Height == 0)
	if !xok || !yok {
		return Impact{}, false
	}
	entry, exit := math.Max(x0, y0), math.Min(x1, y1)
	if entry < 0 || entry > 1 || entry > exit || exit < 0 {
		return Impact{}, false
	}
	var normal Vector
	if x0 >= y0 {
		if delta.X > 0 {
			normal.X = -1
		} else {
			normal.X = 1
		}
	} else if delta.Y > 0 {
		normal.Y = -1
	} else {
		normal.Y = 1
	}
	return Impact{Collider: collider, Time: entry, Normal: normal}, true
}

func sweptBounds(box Rect, delta Vector) Rect {
	return Rect{X: math.Min(box.X, box.X+delta.X), Y: math.Min(box.Y, box.Y+delta.Y), Width: box.Width + math.Abs(delta.X), Height: box.Height + math.Abs(delta.Y)}
}

// Sweep checks the complete displacement against terrain and additional dynamic
// colliders. Equal impact times are ordered by stable collider identity.
func (grid *Grid) Sweep(ctx context.Context, box Rect, delta Vector, dynamic []Collider, options SweepOptions) (_ Impact, _ bool, err error) {
	defer mon.Task()(&ctx)(&err)
	if !validRect(box, options.Projectile) || !validVector(delta) {
		return Impact{}, false, fmt.Errorf("invalid sweep")
	}
	candidates, err := grid.Query(ctx, sweptBounds(box, delta))
	if err != nil {
		return Impact{}, false, err
	}
	seen := make(map[string]bool, len(grid.colliders)+len(dynamic))
	for _, collider := range grid.colliders {
		seen[collider.ID] = true
	}
	for _, collider := range dynamic {
		if collider.ID == "" || seen[collider.ID] || !validKind(collider.Kind) || !validRect(collider.Bounds, false) {
			return Impact{}, false, fmt.Errorf("invalid or duplicate dynamic collider %q", collider.ID)
		}
		seen[collider.ID] = true
	}
	candidates = append(candidates, dynamic...)
	var best Impact
	found := false
	for _, collider := range candidates {
		if collider.ID == "" || !validKind(collider.Kind) || !validRect(collider.Bounds, false) {
			return Impact{}, false, fmt.Errorf("invalid dynamic collider %q", collider.ID)
		}
		if !eligible(collider, options) {
			continue
		}
		hit, ok := impactTime(box, delta, collider)
		if ok && (!found || hit.Time < best.Time || (hit.Time == best.Time && hit.Collider.ID < best.Collider.ID)) {
			best, found = hit, true
		}
	}
	return best, found, nil
}

// MoveResult records actual travel, blocking contacts, and any solver limit.
type MoveResult struct {
	Bounds     Rect
	Contacts   []Impact
	Diagnostic string
}

// separateTolerance corrects only rounding-sized overlaps; real penetration
// remains an error. It is measured by the enclosing movement operation.
func (grid *Grid) separateTolerance(ctx context.Context, box Rect, dynamic []Collider, ignore string) (Rect, error) {
	for i := 0; i < 4; i++ {
		hit, found, err := grid.Sweep(ctx, box, Vector{}, dynamic, SweepOptions{Ignore: ignore})
		if err != nil || !found {
			return box, err
		}
		target := hit.Collider.Bounds
		depths := []float64{box.X + box.Width - target.X, target.X + target.Width - box.X, box.Y + box.Height - target.Y, target.Y + target.Height - box.Y}
		index := 0
		for j := 1; j < len(depths); j++ {
			if depths[j] < depths[index] {
				index = j
			}
		}
		if depths[index] > Tolerance {
			return Rect{}, fmt.Errorf("movement starts overlapping %s", hit.Collider.ID)
		}
		switch index {
		case 0:
			box.X -= depths[index]
		case 1:
			box.X += depths[index]
		case 2:
			box.Y -= depths[index]
		case 3:
			box.Y += depths[index]
		}
	}
	_, found, err := grid.Sweep(ctx, box, Vector{}, dynamic, SweepOptions{Ignore: ignore})
	if err != nil {
		return Rect{}, err
	}
	if found {
		return Rect{}, fmt.Errorf("too many initial collision overlaps")
	}
	return box, nil
}

const maxBlockingContacts = 4

// Move sweeps a rectangular actor, sliding along at most four contacts.
func (grid *Grid) Move(ctx context.Context, box Rect, delta Vector, dynamic []Collider, ignore string) (_ MoveResult, err error) {
	defer mon.Task()(&ctx)(&err)
	return grid.moveWithLimit(ctx, box, delta, dynamic, ignore, maxBlockingContacts)
}

// moveWithLimit shares an enclosing tick's remaining blocking-contact budget.
func (grid *Grid) moveWithLimit(ctx context.Context, box Rect, delta Vector, dynamic []Collider, ignore string, limit int) (_ MoveResult, err error) {
	defer mon.Task()(&ctx)(&err)
	if !validRect(box, false) || !validVector(delta) || limit < 0 || limit > maxBlockingContacts {
		return MoveResult{}, fmt.Errorf("invalid movement")
	}
	box, err = grid.separateTolerance(ctx, box, dynamic, ignore)
	if err != nil {
		return MoveResult{}, err
	}
	result := MoveResult{Bounds: box}
	remaining := delta
	for i := 0; i < limit && (remaining.X != 0 || remaining.Y != 0); i++ {
		hit, found, sweepErr := grid.Sweep(ctx, result.Bounds, remaining, dynamic, SweepOptions{Ignore: ignore})
		if sweepErr != nil {
			return MoveResult{}, sweepErr
		}
		if !found {
			result.Bounds = moved(result.Bounds, remaining)
			remaining = Vector{}
			break
		}
		if hit.Normal == (Vector{}) {
			return MoveResult{}, fmt.Errorf("movement starts overlapping %s", hit.Collider.ID)
		}
		result.Bounds = moved(result.Bounds, Vector{X: remaining.X * hit.Time, Y: remaining.Y * hit.Time})
		result.Contacts = append(result.Contacts, hit)
		remaining.X *= 1 - hit.Time
		remaining.Y *= 1 - hit.Time
		if hit.Normal.X != 0 {
			remaining.X = 0
		}
		if hit.Normal.Y != 0 {
			remaining.Y = 0
		}
	}
	if remaining != (Vector{}) {
		result.Diagnostic = CollisionIterationLimit
	}
	return result, nil
}

// Sweep queries the world's terrain and moving platforms through the same
// solver used by actor movement. Projectile previews can use this directly.
func (world *World) Sweep(ctx context.Context, box Rect, delta Vector, options SweepOptions) (_ Impact, _ bool, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil {
		return Impact{}, false, fmt.Errorf("world has no collision space")
	}
	return world.grid.Sweep(ctx, box, delta, platformColliders(world.state.Platforms), options)
}

// SweepTargets adds explicit eligible actor or machinery targets to the same
// world solver, so projectile execution and previews need no second solver.
func (world *World) SweepTargets(ctx context.Context, box Rect, delta Vector, targets []Collider, options SweepOptions) (_ Impact, _ bool, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil {
		return Impact{}, false, fmt.Errorf("world has no collision space")
	}
	dynamic := append(platformColliders(world.state.Platforms), targets...)
	return world.grid.Sweep(ctx, box, delta, dynamic, options)
}

// Support probes one world pixel below the body using the same collision rules.
func (grid *Grid) Support(ctx context.Context, box Rect, dynamic []Collider, ignore string) (_ string, err error) {
	defer mon.Task()(&ctx)(&err)
	hit, found, err := grid.Sweep(ctx, box, Vector{Y: -1}, dynamic, SweepOptions{Ignore: ignore})
	if err != nil {
		return "", err
	}
	if found && hit.Normal.Y == 1 {
		return hit.Collider.ID, nil
	}
	return "", nil
}

// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"slices"
)

const gridSize = 128.0
const maxGridCells = 100000

type cell struct{ X, Y int }

// Grid is an immutable 128-pixel broad phase of static colliders.
type Grid struct {
	colliders []Collider
	cells     map[cell][]int
}

func cellRange(r Rect) (int, int, int, int, bool) {
	if !validRect(r, true) {
		return 0, 0, 0, 0, false
	}
	x0, x1 := int(math.Floor(r.X/gridSize)), int(math.Floor((r.X+r.Width)/gridSize))
	y0, y1 := int(math.Floor(r.Y/gridSize)), int(math.Floor((r.Y+r.Height)/gridSize))
	if (x1-x0+1)*(y1-y0+1) > maxGridCells {
		return 0, 0, 0, 0, false
	}
	return x0, x1, y0, y1, true
}

// NewGrid validates and copies the terrain; identities must be unique.
func NewGrid(ctx context.Context, colliders []Collider) (_ *Grid, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	grid := &Grid{colliders: slices.Clone(colliders), cells: make(map[cell][]int)}
	slices.SortFunc(grid.colliders, func(a, b Collider) int { return compareID(a.ID, b.ID) })
	for i, collider := range grid.colliders {
		if collider.ID == "" || !validKind(collider.Kind) || !validRect(collider.Bounds, false) || (i > 0 && grid.colliders[i-1].ID == collider.ID) {
			return nil, fmt.Errorf("invalid collider %q", collider.ID)
		}
		x0, x1, y0, y1, ok := cellRange(collider.Bounds)
		if !ok {
			return nil, fmt.Errorf("collider %q exceeds grid bounds", collider.ID)
		}
		for x := x0; x <= x1; x++ {
			for y := y0; y <= y1; y++ {
				key := cell{X: x, Y: y}
				grid.cells[key] = append(grid.cells[key], i)
			}
		}
	}
	return grid, nil
}

func compareID(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// Query returns possible colliders in identity order without exposing grid storage.
func (grid *Grid) Query(ctx context.Context, bounds Rect) (_ []Collider, err error) {
	defer mon.Task()(&ctx)(&err)
	if grid == nil {
		return nil, fmt.Errorf("nil grid")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	x0, x1, y0, y1, ok := cellRange(bounds)
	if !ok {
		return nil, fmt.Errorf("invalid query bounds")
	}
	seen := make([]bool, len(grid.colliders))
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for _, index := range grid.cells[cell{X: x, Y: y}] {
				seen[index] = true
			}
		}
	}
	var result []Collider
	for i, collider := range grid.colliders {
		if seen[i] {
			result = append(result, collider)
		}
	}
	return result, nil
}

// Query returns static broad-phase candidates and moving platforms in stable
// identity order. The result is detached from the world.
func (world *World) Query(ctx context.Context, bounds Rect) (_ []Collider, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil {
		return nil, fmt.Errorf("world has no collision space")
	}
	result, err := world.grid.Query(ctx, bounds)
	if err != nil {
		return nil, err
	}
	result = append(result, platformColliders(world.state.Platforms)...)
	slices.SortFunc(result, func(a, b Collider) int { return compareID(a.ID, b.ID) })
	return result, nil
}

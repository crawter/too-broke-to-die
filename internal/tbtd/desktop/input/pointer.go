// Copyright (C) 2026 crawter
// All rights reserved.

package input

import (
	"context"
	"math"
)

// Viewport maps a letterboxed screen rectangle to a logical canvas and a
// camera. Offset is the logical point at which Target appears. Camera rotation
// is clockwise in screen coordinates; world Y increases upward.
type Viewport struct {
	X, Y, Width, Height         float64
	LogicalWidth, LogicalHeight float64
	Target, Offset              Point
	Zoom, Rotation              float64
}

// LogicalPoint maps the pointer to the unrotated interface canvas. Points in
// bars are invalid, so menu selection cannot accidentally activate an item.
func (view Viewport) LogicalPoint(ctx context.Context, pointer Point) (Point, bool) {
	defer mon.Task()(&ctx)(nil)
	for _, value := range []float64{view.X, view.Y, view.Width, view.Height, view.LogicalWidth, view.LogicalHeight, pointer.X, pointer.Y} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return Point{}, false
		}
	}
	if view.Width <= 0 || view.Height <= 0 || view.LogicalWidth <= 0 || view.LogicalHeight <= 0 || pointer.X < view.X || pointer.Y < view.Y || pointer.X >= view.X+view.Width || pointer.Y >= view.Y+view.Height {
		return Point{}, false
	}
	return Point{X: (pointer.X - view.X) * view.LogicalWidth / view.Width, Y: (pointer.Y - view.Y) * view.LogicalHeight / view.Height}, true
}

// WorldPoint converts a screen position to world coordinates. Bars and invalid
// transforms are rejected rather than being clamped to the edge of the world.
func (view Viewport) WorldPoint(ctx context.Context, pointer Point) (Point, bool) {
	defer mon.Task()(&ctx)(nil)
	for _, value := range []float64{view.X, view.Y, view.Width, view.Height, view.LogicalWidth, view.LogicalHeight, view.Target.X, view.Target.Y, view.Offset.X, view.Offset.Y, view.Zoom, view.Rotation, pointer.X, pointer.Y} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return Point{}, false
		}
	}
	if view.Width <= 0 || view.Height <= 0 || view.LogicalWidth <= 0 || view.LogicalHeight <= 0 || view.Zoom <= 0 || pointer.X < view.X || pointer.Y < view.Y || pointer.X >= view.X+view.Width || pointer.Y >= view.Y+view.Height {
		return Point{}, false
	}
	x := ((pointer.X-view.X)*view.LogicalWidth/view.Width - view.Offset.X) / view.Zoom
	y := ((pointer.Y-view.Y)*view.LogicalHeight/view.Height - view.Offset.Y) / view.Zoom
	angle := view.Rotation * math.Pi / 180
	cosine, sine := math.Cos(angle), math.Sin(angle)
	return Point{X: view.Target.X + cosine*x + sine*y, Y: view.Target.Y + sine*x - cosine*y}, true
}

func direction(from, to Point) (Point, bool) {
	dx, dy := to.X-from.X, to.Y-from.Y
	length := math.Hypot(dx, dy)
	if length == 0 || math.IsNaN(length) || math.IsInf(length, 0) {
		return Point{}, false
	}
	return Point{X: dx / length, Y: dy / length}, true
}

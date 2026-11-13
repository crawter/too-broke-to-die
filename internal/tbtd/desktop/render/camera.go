// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"math"
	"time"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/desktop/input"
	"too-broke-to-die/internal/tbtd/session"
)

const (
	// LogicalWidth is the fixed presentation width in logical pixels.
	LogicalWidth = 1280.0
	// LogicalHeight is the fixed presentation height in logical pixels.
	LogicalHeight = 720.0
)

// Camera is presentation-only. X and Y are world coordinates at screen centre;
// Y increases upward. The caller resets it when the encounter changes.
type Camera struct {
	X, Y        float64
	baseX       float64
	look        float64
	initialized bool
}

// Follow applies the camera policy for point markers. Use FollowBounds when
// authored landing areas are available, so their full extents are visible.
func (camera *Camera) Follow(ctx context.Context, player session.Point, velocityX float64, elapsed time.Duration, locked bool, arenaLeft float64, landings []session.Point) {
	defer mon.Task()(&ctx)(nil)
	bounds := make([]session.WorldRect, 0, len(landings))
	for _, point := range landings {
		bounds = append(bounds, session.WorldRect{X: point.X, Y: point.Y})
	}
	if err := camera.FollowBounds(ctx, player, velocityX, elapsed, locked, arenaLeft, bounds); err != nil {
		return // This compatibility method retains the previous camera on invalid input.
	}
}

// FollowBounds simultaneously frames every supplied landing. An impossible or
// invalid constraint returns a content error without changing the camera.
func (camera *Camera) FollowBounds(ctx context.Context, player session.Point, velocityX float64, elapsed time.Duration, locked bool, arenaLeft float64, landings []session.WorldRect) (err error) {
	defer mon.Task()(&ctx)(&err)
	if camera == nil || !validPoint(player) || !validPoint(session.Point{X: velocityX, Y: arenaLeft}) || elapsed < 0 {
		return content.ErrInvalid.New("invalid camera input")
	}
	next := *camera
	if !next.initialized {
		next.baseX, next.X, next.Y = player.X, player.X, 160
		next.initialized = true
	}
	if locked {
		next.baseX, next.X, next.Y, next.look = arenaLeft+640, arenaLeft+640, 160, 0
	} else {
		next.baseX = math.Max(player.X-120, math.Min(player.X+120, next.baseX))
		target := 0.0
		if velocityX > 0 {
			target = 120
		} else if velocityX < 0 {
			target = -120
		}
		step := elapsed.Seconds() * 600
		next.look += math.Max(-step, math.Min(step, target-next.look))
		next.X = next.baseX + next.look
		next.Y = math.Max(player.Y-40, math.Min(player.Y+160, next.Y))
	}
	for _, landing := range landings {
		if !validPoint(session.Point{X: landing.X, Y: landing.Y}) || !validPoint(session.Point{X: landing.Width, Y: landing.Height}) || landing.Width < 0 || landing.Height < 0 {
			return content.ErrInvalid.New("invalid landing bounds")
		}
	}
	var minX, maxX, minY, maxY float64
	for _, margin := range []float64{20, 0} {
		if locked && margin != 0 {
			continue
		}
		minX, maxX = player.X-640, player.X+640
		minY, maxY = player.Y-360, player.Y+360
		for _, landing := range landings {
			minX = math.Max(minX, landing.X+landing.Width-(640-margin))
			maxX = math.Min(maxX, landing.X+640-margin)
			minY = math.Max(minY, landing.Y+landing.Height-(360-margin))
			maxY = math.Min(maxY, landing.Y+360-margin)
		}
		if minX <= maxX && minY <= maxY {
			break
		}
	}
	if minX > maxX || minY > maxY {
		return content.ErrInvalid.New("mandatory landing bounds cannot fit in camera")
	}
	if locked && (next.X < minX || next.X > maxX || next.Y < minY || next.Y > maxY) {
		return content.ErrInvalid.New("mandatory landing bounds outside locked arena")
	}
	if !locked {
		next.X = math.Max(minX, math.Min(maxX, next.X))
		next.Y = math.Max(minY, math.Min(maxY, next.Y))
	}
	*camera = next
	return nil
}

// Viewport returns the same unshaken transform used for pointer input.
func (camera Camera) Viewport(width, height int) input.Viewport {
	if width <= 0 || height <= 0 {
		return input.Viewport{}
	}
	scale := math.Min(float64(width)/LogicalWidth, float64(height)/LogicalHeight)
	w, h := LogicalWidth*scale, LogicalHeight*scale
	return input.Viewport{X: (float64(width) - w) / 2, Y: (float64(height) - h) / 2, Width: w, Height: h, LogicalWidth: LogicalWidth, LogicalHeight: LogicalHeight, Target: input.Point{X: camera.X, Y: camera.Y}, Offset: input.Point{X: 640, Y: 360}, Zoom: 1}
}

func interpolate(entity session.EntityView, fraction float64) session.Point {
	fraction = math.Max(0, math.Min(1, fraction))
	return session.Point{X: entity.PreviousX + (entity.X-entity.PreviousX)*fraction, Y: entity.PreviousY + (entity.Y-entity.PreviousY)*fraction}
}

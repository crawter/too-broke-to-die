// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"too-broke-to-die/internal/tbtd/desktop/input"
	"too-broke-to-die/internal/tbtd/session"
)

// markerShape keeps essential distinctions independent of color and audio.
type markerShape string

const (
	shapeSerrated markerShape = "serrated"
	shapeActive   markerShape = "active"
	shapeDormant  markerShape = "dormant"
	shapeSafe     markerShape = "safe"
	shapeDoorway  markerShape = "doorway"
	shapeCorners  markerShape = "corners"
	shapeRing     markerShape = "ring"
	shapeCross    markerShape = "cross"
	shapeDiamond  markerShape = "diamond"
	shapeHatch    markerShape = "hatch"
	shapeBroken   markerShape = "broken"
	shapeArrow    markerShape = "arrow"
	shapeWave     markerShape = "wave"
)

func markerFor(marker session.WorldMarker) markerShape {
	switch marker.Kind {
	case "danger":
		switch marker.State {
		case "active":
			return shapeActive
		case "dormant":
			return shapeDormant
		case "inactive":
			return shapeSafe
		default:
			return shapeSerrated
		}
	case "immune":
		return shapeHatch
	case "refuge":
		return shapeDoorway
	case "interaction", "limb":
		return shapeCorners
	case "receptor":
		return shapeRing
	case "projectile":
		return shapeArrow
	case "smear":
		return shapeWave
	case "boss":
		if marker.Label == "ЗАЩИТА" {
			return shapeHatch
		}
		return shapeBroken
	case "relay":
		return shapeDiamond
	}
	return ""
}

func impactShape(kind string) markerShape {
	switch kind {
	case "terrain":
		return shapeCross
	case "receptor":
		return shapeRing
	case "invalid":
		return shapeDiamond
	}
	return ""
}

// reticlePoint uses the unshaken viewport; mouse coordinates stay fixed in
// logical screen space, while controller aim is projected from the player.
func reticlePoint(view input.Viewport, aim session.AimView, player session.Point) (rl.Vector2, bool) {
	if !aim.Visible || view.Width <= 0 || view.Height <= 0 {
		return rl.Vector2{}, false
	}
	scale := view.Width / LogicalWidth
	if aim.Reticle.Pointer {
		if !aim.Reticle.Visible {
			return rl.Vector2{}, false
		}
		x, y := aim.Reticle.X, aim.Reticle.Y
		if !validPoint(session.Point{X: x, Y: y}) || x < 0 || x >= LogicalWidth || y < 0 || y >= LogicalHeight {
			return rl.Vector2{}, false
		}
		return rl.Vector2{X: float32(view.X + x*scale), Y: float32(view.Y + y*scale)}, true
	}
	if !validPoint(player) || !validPoint(aim.Direction) {
		return rl.Vector2{}, false
	}
	length := math.Hypot(aim.Direction.X, aim.Direction.Y)
	if length == 0 {
		return rl.Vector2{}, false
	}
	target := session.Point{X: player.X + 96*aim.Direction.X/length, Y: player.Y + 29 + 96*aim.Direction.Y/length}
	point := screenPoint(view, target)
	if float64(point.X) < view.X || float64(point.X) >= view.X+view.Width || float64(point.Y) < view.Y || float64(point.Y) >= view.Y+view.Height {
		return rl.Vector2{}, false
	}
	return point, true
}

func drawReticle(point rl.Vector2, scale float32) {
	rl.DrawCircleLinesV(point, 9*scale, rl.Black)
	rl.DrawCircleLinesV(point, 7*scale, rl.White)
	rl.DrawLineEx(rl.Vector2{X: point.X - 12*scale, Y: point.Y}, rl.Vector2{X: point.X + 12*scale, Y: point.Y}, 2*scale, rl.White)
	rl.DrawLineEx(rl.Vector2{X: point.X, Y: point.Y - 12*scale}, rl.Vector2{X: point.X, Y: point.Y + 12*scale}, 2*scale, rl.White)
}

// drawMarker is called only after every asset and coordinate has been checked.
// Its shapes and labels sit above actors and decorations, with no world shake.
func drawMarker(view input.Viewport, font rl.Font, item drawItem, highContrast bool) {
	scale := float32(view.Width / LogicalWidth)
	color := rl.White
	thickness := 2 * scale
	if highContrast {
		thickness = 4 * scale
	}
	if len(item.path) > 0 {
		for i, point := range item.path {
			if i%4 == 0 || i == len(item.path)-1 {
				rl.DrawCircleV(screenPoint(view, point), 2.5*scale, color)
			}
		}
		return
	}
	bounds := item.bounds
	if bounds.Width == 0 && bounds.Height == 0 {
		bounds.X, bounds.Y, bounds.Width, bounds.Height = item.point.X-8, item.point.Y-8, 16, 16
	}
	if bounds.Width == 0 {
		bounds.Width = 16
	}
	if bounds.Height == 0 {
		bounds.Height = 16
	}
	top := screenPoint(view, session.Point{X: bounds.X, Y: bounds.Y + bounds.Height})
	w, h := float32(bounds.Width)*scale, float32(bounds.Height)*scale
	left, right, upper, lower := top.X, top.X+w, top.Y, top.Y+h
	line := func(x1, y1, x2, y2 float32) {
		rl.DrawLineEx(rl.Vector2{X: x1, Y: y1}, rl.Vector2{X: x2, Y: y2}, thickness, color)
	}
	switch item.shape {
	case shapeSerrated, shapeActive:
		rl.DrawRectangleLinesEx(rl.Rectangle{X: left, Y: upper, Width: w, Height: h}, thickness, color)
		count := min(128, max(1, int(math.Ceil(float64(w/(24*scale))))))
		for i := 0; i < count; i++ {
			x := left + float32(i)*w/float32(count)
			next := left + float32(i+1)*w/float32(count)
			line(x, upper, (x+next)/2, upper+8*scale)
			line((x+next)/2, upper+8*scale, next, upper)
		}
		if item.shape == shapeActive {
			line(left, lower, right, upper)
			line(left, upper, right, lower)
		}
	case shapeDormant:
		xCount := min(128, max(1, int(math.Ceil(float64(w/(16*scale))))))
		yCount := min(128, max(1, int(math.Ceil(float64(h/(16*scale))))))
		for i := 0; i < xCount; i += 2 {
			x1, x2 := left+float32(i)*w/float32(xCount), left+float32(i+1)*w/float32(xCount)
			line(x1, upper, x2, upper)
			line(x1, lower, x2, lower)
		}
		for i := 0; i < yCount; i += 2 {
			y1, y2 := upper+float32(i)*h/float32(yCount), upper+float32(i+1)*h/float32(yCount)
			line(left, y1, left, y2)
			line(right, y1, right, y2)
		}
	case shapeSafe:
		rl.DrawRectangleLinesEx(rl.Rectangle{X: left, Y: upper, Width: w, Height: h}, thickness, color)
		line(left, upper+h/2, right, upper+h/2)
	case shapeDoorway:
		rl.DrawRectangleLinesEx(rl.Rectangle{X: left, Y: upper, Width: w, Height: h}, thickness, color)
		rl.DrawRectangleLinesEx(rl.Rectangle{X: left + 4*scale, Y: upper + 4*scale, Width: max(1, w-8*scale), Height: max(1, h-8*scale)}, thickness, color)
	case shapeCorners, shapeBroken:
		corner := 8 * scale
		line(left, upper, left+corner, upper)
		line(left, upper, left, upper+corner)
		line(right, upper, right-corner, upper)
		line(right, upper, right, upper+corner)
		line(left, lower, left+corner, lower)
		line(left, lower, left, lower-corner)
		line(right, lower, right-corner, lower)
		line(right, lower, right, lower-corner)
	case shapeRing:
		rl.DrawCircleLinesV(rl.Vector2{X: left + w/2, Y: upper + h/2}, max(w, h)/2, color)
	case shapeDiamond:
		line(left+w/2, upper, right, upper+h/2)
		line(right, upper+h/2, left+w/2, lower)
		line(left+w/2, lower, left, upper+h/2)
		line(left, upper+h/2, left+w/2, upper)
	case shapeCross:
		line(left, upper, right, lower)
		line(left, lower, right, upper)
	case shapeHatch:
		rl.DrawRectangleLinesEx(rl.Rectangle{X: left, Y: upper, Width: w, Height: h}, thickness, color)
		line(left, lower, right, upper)
		line(left, upper, right, lower)
	case shapeArrow:
		line(left, upper+h/2, right, upper+h/2)
		line(right-6*scale, upper, right, upper+h/2)
		line(right-6*scale, lower, right, upper+h/2)
	case shapeWave:
		line(left, upper+h/2, left+w/3, upper)
		line(left+w/3, upper, left+2*w/3, lower)
		line(left+2*w/3, lower, right, upper+h/2)
	}
	if item.label != "" {
		position := rl.Vector2{X: left, Y: upper - 22*scale}
		size := rl.MeasureTextEx(font, item.label, 20*scale, scale)
		rl.DrawRectangleRec(rl.Rectangle{X: position.X - 2*scale, Y: position.Y, Width: size.X + 4*scale, Height: size.Y}, rl.Black)
		rl.DrawTextEx(font, item.label, position, 20*scale, scale, color)
	}
}

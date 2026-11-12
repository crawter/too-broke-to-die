// Copyright (C) 2026 crawter
// All rights reserved.

// Package simulation owns device-independent authoritative world state.
package simulation

import (
	"math"

	"too-broke-to-die/internal/tbtd/content"
)

const (
	// TickSeconds is the duration of one authoritative simulation step.
	TickSeconds = 1.0 / 120
	// Tolerance is the collision tolerance in world pixels.
	Tolerance = 1.0 / 1024
	// PlayerWidth and PlayerHeight never depend on animation or limbs.
	PlayerWidth  = 28.0
	PlayerHeight = 58.0
	// CollisionIterationLimit identifies an exhausted movement solver.
	CollisionIterationLimit = "collision_iteration_limit"
)

// Vector uses world pixels and positive upward Y.
type Vector struct{ X, Y float64 }

// Rect uses a bottom-left origin, width and height in world pixels.
type Rect struct{ X, Y, Width, Height float64 }

// ColliderKind distinguishes movement and projectile blocking rules.
type ColliderKind string

const (
	// Solid blocks actors and projectiles on every side.
	Solid ColliderKind = "solid"
	// OneWay blocks actors and projectiles only when crossing its top downward
	// from above; an actor may exclude its current platform while dropping.
	OneWay ColliderKind = "one_way"
	// PlayerOnly blocks actor movement on every side but not projectiles.
	PlayerOnly ColliderKind = "player_only"
	// ProjectileTarget receives projectile impacts but does not block actors.
	ProjectileTarget ColliderKind = "projectile_target"
)

// Collider has a stable identity unique within a world.
type Collider struct {
	ID     string
	Bounds Rect
	Kind   ColliderKind
}

// Body is an authoritative actor. Velocity is never augmented by platform carry.
type Body struct {
	ID          string
	Bounds      Rect
	Velocity    Vector
	DropThrough string
	Support     string
	Crushed     bool
}

// Platform moves by Velocity each fixed tick; Crusher opts out of reversal.
type Platform struct {
	Collider Collider
	Velocity Vector
	Crusher  bool
}

// Entity describes a reconstructed actor or interactive object.
type Entity struct {
	ID        content.ObjectID
	Role      content.ObjectRole
	Kind      string
	Position  Vector
	Ticket    content.DeedID
	Protected bool
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func validVector(v Vector) bool {
	return finite(v.X) && finite(v.Y) && math.Abs(v.X) <= 1e7 && math.Abs(v.Y) <= 1e7
}
func validRect(r Rect, allowPoint bool) bool {
	return validVector(Vector{X: r.X, Y: r.Y}) && finite(r.Width) && finite(r.Height) && ((r.Width > 0 && r.Height > 0) || (allowPoint && r.Width >= 0 && r.Height >= 0)) && r.Width <= 1e6 && r.Height <= 1e6 && math.Abs(r.X+r.Width) <= 1e7 && math.Abs(r.Y+r.Height) <= 1e7
}
func overlaps(a, b Rect) bool {
	return a.X < b.X+b.Width && a.X+a.Width > b.X && a.Y < b.Y+b.Height && a.Y+a.Height > b.Y
}
func moved(r Rect, v Vector) Rect { r.X += v.X; r.Y += v.Y; return r }
func rectFromContent(r content.Rect) Rect {
	return Rect{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height}
}
func validKind(kind ColliderKind) bool {
	return kind == Solid || kind == OneWay || kind == PlayerOnly || kind == ProjectileTarget
}

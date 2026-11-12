// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"

	"too-broke-to-die/internal/tbtd/settings"
)

// LimbMask identifies the independently attached sockets.
type LimbMask uint8

const (
	// LeftArm marks the attached LIM-LA socket.
	LeftArm LimbMask = 1 << iota
	// RightArm marks the attached LIM-RA socket.
	RightArm
	// LeftLeg marks the attached LIM-LL socket.
	LeftLeg
	// RightLeg marks the attached LIM-RL socket.
	RightLeg
	// AllLimbs marks all four sockets as attached.
	AllLimbs = LeftArm | RightArm | LeftLeg | RightLeg
)

// PlayerEdge is a single ordered normalized action transition. Its action
// identity is the same one used by session.FrameCommand and settings.
type PlayerEdge struct {
	Action  settings.Action
	Pressed bool
}

// PlayerCommand contains the held movement and aim values and edges for one
// simulation tick. The session retains edges across frames without a tick.
type PlayerCommand struct {
	Move  float64
	Aim   Vector
	Down  bool
	Edges []PlayerEdge
}

// PlayerStatus is a value copy for combat and presentation. Immune describes
// the tick just completed, not protection against lethal terrain or crushing.
type PlayerStatus struct {
	Limbs          LimbMask
	Facing         int
	Action         ActionKind
	OrdinaryImmune bool
	Dead           bool
	Suspended      bool
	Buffered       settings.Action
}

type playerMotion struct {
	limbs         LimbMask
	facing        int
	action        ActionKind
	actionUntil   uint64
	immuneUntil   uint64
	cooldownUntil uint64
	dropUntil     uint64
	drop          string
	lastGround    uint64
	hasGround     bool
	jumpUsed      bool
	jumpHeld      bool
	jumpActive    bool
	held          uint8
	buffer        settings.Action
	bufferUntil   uint64
	immune        bool
	dead          bool
	suspended     bool
}

func newPlayerMotion() playerMotion {
	return playerMotion{limbs: AllLimbs, facing: 1}
}

// PlayerStatus returns an independent value. It is valid after StepPlayer and
// remains available even when a lethal boundary has ended gameplay.
func (world *World) PlayerStatus() PlayerStatus {
	if world == nil {
		return PlayerStatus{}
	}
	motion := world.motion
	return PlayerStatus{Limbs: motion.limbs, Facing: motion.facing, Action: motion.action, OrdinaryImmune: motion.immune, Dead: motion.dead, Suspended: motion.suspended, Buffered: motion.buffer}
}

// SetPlayerLimbs updates capabilities without changing the collision box.
// The limb system owns socket transitions and calls this after its channels.
func (world *World) SetPlayerLimbs(ctx context.Context, limbs LimbMask) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || !world.state.HasPlayer || limbs&^AllLimbs != 0 {
		return fmt.Errorf("invalid player limbs")
	}
	world.motion.limbs = limbs
	count := 0
	for _, limb := range []LimbMask{LeftArm, RightArm, LeftLeg, RightLeg} {
		if limbs&limb != 0 {
			count++
		}
	}
	world.state.Limbs = count
	return nil
}

// SuspendPlayer clears buffered and held gameplay actions. The session calls
// this on menus, focus loss or disconnection before any more ticks.
func (world *World) SuspendPlayer(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	if world == nil {
		return
	}
	world.motion.suspended = true
	world.motion.buffer = ""
	world.motion.held = 0
	world.motion.jumpHeld = false
}

// ResumePlayer requires the session's current held controls, so an input held
// through a menu cannot become a new attack on resume.
func (world *World) ResumePlayer(ctx context.Context, held []settings.Action) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || !world.state.HasPlayer || world.motion.dead {
		return fmt.Errorf("cannot resume player")
	}
	for _, action := range held {
		switch action {
		case settings.Melee, settings.Throw, settings.RemoveLimb, settings.MenuConfirm:
			return fmt.Errorf("release action before resume: %s", action)
		}
	}
	world.motion.suspended = false
	return nil
}

// PlayerState captures the collision and capability state together for a
// caller without sharing mutable world data.
func (world *World) PlayerState(ctx context.Context) (Body, PlayerStatus) {
	defer mon.Task()(&ctx)(nil)
	if world == nil {
		return Body{}, PlayerStatus{}
	}
	return world.state.Player, world.PlayerStatus()
}

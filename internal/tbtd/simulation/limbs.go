// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"too-broke-to-die/internal/tbtd/settings"
)

// SocketID is the stable identity of a body socket.
type SocketID string

const (
	// SocketLeftArm identifies LIM-LA.
	SocketLeftArm SocketID = "LIM-LA"
	// SocketRightArm identifies LIM-RA.
	SocketRightArm SocketID = "LIM-RA"
	// SocketLeftLeg identifies LIM-LL.
	SocketLeftLeg SocketID = "LIM-LL"
	// SocketRightLeg identifies LIM-RL.
	SocketRightLeg SocketID = "LIM-RL"
)

// LimbObject is an independent transient object, never a campaign resource.
// Only settled objects can be used as pressure-plate weights.
type LimbObject struct {
	ID         string
	Socket     SocketID
	Generation uint64
	Attack     uint64
	Position   Vector
	Velocity   Vector
	Carried    bool
	Airborne   bool
	Damaging   bool
	Bounced    bool
	FlightTick uint64
	ignored    string
}

// LimbEvent records a completed transition or a swept projectile impact.
// An interaction_requested event delegates a non-limb interaction to the
// service executor; Target names a nearby civilian when one has priority.
type LimbEvent struct {
	Kind   string
	Object string
	Socket SocketID
	Target string
}

// LimbView is an isolated snapshot of the player's sockets and loose objects.
type LimbView struct {
	Attached    LimbMask
	Selected    SocketID
	Generation  uint64
	Carried     string
	LimbPayload bool
	Objects     []LimbObject
	Events      []LimbEvent
	Channel     string
	Remaining   uint64
	ThrowPhase  CombatPhase
	Invalid     string
}

type limbChannel struct {
	kind   string
	socket SocketID
	object string
	until  uint64
}

type limbThrow struct {
	object string
	attack uint64
}

type limbState struct {
	selected     SocketID
	generation   uint64
	nextObject   uint64
	carried      string
	payload      bool
	objects      []LimbObject
	events       []LimbEvent
	channel      limbChannel
	throw        limbThrow
	regenHeld    bool
	interactHeld bool
	throwHeld    bool
	removeHeld   bool
	payloadHeld  bool
	throwPending string
	inTick       bool
	invalid      string
	targets      []Collider
}

func cloneLimbs(state limbState) limbState {
	state.objects = slices.Clone(state.objects)
	state.events = slices.Clone(state.events)
	state.targets = slices.Clone(state.targets)
	return state
}

func socketMask(socket SocketID) LimbMask {
	switch socket {
	case SocketLeftArm:
		return LeftArm
	case SocketRightArm:
		return RightArm
	case SocketLeftLeg:
		return LeftLeg
	case SocketRightLeg:
		return RightLeg
	default:
		return 0
	}
}

func (world *World) initLimbs() {
	if world.limbs.generation == 0 {
		world.limbs.generation = 1
	}
	if world.limbs.selected == "" {
		world.limbs.selected = SocketLeftArm
	}
}

// LimbView returns independent objects and events; mutating it cannot affect play.
func (world *World) LimbView(ctx context.Context) LimbView {
	defer mon.Task()(&ctx)(nil)
	if world == nil {
		return LimbView{}
	}
	state := cloneLimbs(world.limbs)
	selected, generation := state.selected, state.generation
	if selected == "" {
		selected = SocketLeftArm
	}
	if generation == 0 {
		generation = 1
	}
	var remaining uint64
	if state.channel.until > world.state.Tick {
		remaining = state.channel.until - world.state.Tick
	}
	var phase CombatPhase
	if state.throw.object != "" && state.throw.attack == world.combat.attack.ID {
		phase = world.combat.attack.Phase
	}
	return LimbView{Attached: world.motion.limbs, Selected: selected, Generation: generation, Carried: state.carried, LimbPayload: state.payload, Objects: state.objects, Events: state.events, Channel: state.channel.kind, Remaining: remaining, ThrowPhase: phase, Invalid: state.invalid}
}

// LimbCommand adds service projectile targets to the normal combat tick. Their
// identities must be unique among the terrain, platforms and combat targets.
// A service executor may latch an impact event independently of the object.
type LimbCommand struct {
	Combat  CombatCommand
	Targets []Collider
}

// StepLimbs advances a combat tick including limb controls. Callers must use
// this entry point consistently while playing with limb objects. Targets are
// transient for this tick and are never stored in campaign progress.
func (world *World) StepLimbs(ctx context.Context, command LimbCommand) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil || !validRect(command.Combat.Visible, false) || !validHits(command.Combat.Hits) || !finite(command.Combat.Player.Move) || math.Abs(command.Combat.Player.Move) > 1 || !validVector(command.Combat.Player.Aim) || math.Abs(command.Combat.Player.Aim.X) > 1 || math.Abs(command.Combat.Player.Aim.Y) > 1 {
		return fmt.Errorf("invalid limb tick")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	for _, edge := range command.Combat.Player.Edges {
		if !edge.Action.Valid() {
			return fmt.Errorf("invalid input edge")
		}
	}
	ids := make(map[string]bool, len(command.Targets))
	for _, target := range command.Targets {
		if target.ID == "" || ids[target.ID] || target.Kind != ProjectileTarget || !validRect(target.Bounds, false) {
			return fmt.Errorf("invalid limb target %q", target.ID)
		}
		ids[target.ID] = true
	}
	before, previous, motion, combat, limbs := cloneState(world.state), cloneState(world.previous), world.motion, cloneCombat(world.combat), cloneLimbs(world.limbs)
	defer func() {
		if err != nil {
			world.state, world.previous, world.motion, world.combat, world.limbs = before, previous, motion, combat, limbs
		}
	}()
	world.initLimbs()
	world.limbs.events = nil
	world.limbs.invalid = ""
	world.limbs.targets = slices.Clone(command.Targets)
	if world.motion.action != FreeAction && world.state.Tick >= world.motion.actionUntil {
		world.motion.action = FreeAction
	}
	world.cancelInterruptedLimbAction(ctx, command.Combat.Player)
	command.Combat.Player.Edges = world.limbInputs(ctx, command.Combat.Player)
	world.limbs.inTick = true
	if err = world.StepCombat(ctx, command.Combat); err != nil {
		return err
	}
	world.cancelInterruptedLimbAction(ctx, PlayerCommand{})
	world.limbs.targets = nil
	world.limbs.inTick = false
	return nil
}

func (world *World) stationary(command PlayerCommand) bool {
	body := world.state.Player
	return body.Support != "" && body.Velocity.Y <= 0 && body.Velocity.X == 0 && command.Move == 0
}

func (world *World) cancelInterruptedLimbAction(ctx context.Context, command PlayerCommand) {
	defer mon.Task()(&ctx)(nil)
	state := &world.limbs
	channel := state.channel
	if channel.kind != "" {
		expected := RemovalAction
		if channel.kind != "remove" {
			expected = InteractionAction
		}
		if world.motion.action != expected || world.motion.dead || world.motion.suspended || !world.stationary(command) || (channel.kind == "regenerate" && !state.regenHeld) || (channel.kind == "replace" && !state.interactHeld) {
			state.events = append(state.events, LimbEvent{Kind: "cancelled", Socket: channel.socket, Object: channel.object})
			state.channel = limbChannel{}
			if world.motion.action == expected {
				world.motion.action = FreeAction
			}
		}
	}
	// A suspended attack resumes at the same tick; its payload binding must
	// survive the pause until the attack is released or actually interrupted.
	if state.throw.object != "" && (world.motion.action != AttackAction || world.motion.dead) {
		state.throw = limbThrow{}
	}
}

func (world *World) limbInputs(ctx context.Context, command PlayerCommand) []PlayerEdge {
	defer mon.Task()(&ctx)(nil)
	state := &world.limbs
	var remaining []PlayerEdge
	var remove, regenerate, interact, throw bool
	var removeSocket SocketID
	var evade bool
	evadeHeld := world.motion.held&edgeBit(settings.Evade) != 0
	for _, edge := range command.Edges {
		switch edge.Action {
		case settings.SelectLeftArm, settings.SelectRightArm, settings.SelectLeftLeg, settings.SelectRightLeg:
			if edge.Pressed {
				switch edge.Action {
				case settings.SelectLeftArm:
					state.selected = SocketLeftArm
				case settings.SelectRightArm:
					state.selected = SocketRightArm
				case settings.SelectLeftLeg:
					state.selected = SocketLeftLeg
				case settings.SelectRightLeg:
					state.selected = SocketRightLeg
				}
			}
		case settings.SelectPayload:
			if edge.Pressed && !state.payloadHeld && state.carried != "" {
				state.payload = !state.payload
			}
			state.payloadHeld = edge.Pressed
		case settings.RemoveLimb:
			if edge.Pressed && !state.removeHeld && !remove {
				remove, removeSocket = true, state.selected
			}
			state.removeHeld = edge.Pressed
		case settings.Regenerate:
			regenerate = regenerate || (edge.Pressed && !state.regenHeld)
			state.regenHeld = edge.Pressed
		case settings.Interact:
			interact = interact || (edge.Pressed && !state.interactHeld)
			state.interactHeld = edge.Pressed
		case settings.Throw:
			if edge.Pressed && !state.throwHeld && state.payload {
				throw = true
			}
			state.throwHeld = edge.Pressed
			if !state.payload || !edge.Pressed {
				remaining = append(remaining, edge)
			}
		case settings.Evade:
			evade = evade || (edge.Pressed && !evadeHeld)
			evadeHeld = edge.Pressed
			remaining = append(remaining, edge)
		default:
			remaining = append(remaining, edge)
		}
	}
	world.cancelInterruptedLimbAction(ctx, command)
	if state.throwPending != "" && (world.motion.buffer != settings.Throw || world.state.Tick >= world.motion.bufferUntil || state.throwPending != state.carried || world.limbIndex(state.throwPending) < 0 || world.motion.limbs&(LeftArm|RightArm) == 0) {
		if world.motion.buffer == settings.Throw {
			world.motion.buffer = ""
		}
		state.throwPending = ""
	}
	if throw {
		if state.carried == "" || world.motion.limbs&(LeftArm|RightArm) == 0 {
			state.invalid = "cannot_throw_limb"
		} else if state.throwPending == "" && (world.motion.buffer == "" || world.state.Tick >= world.motion.bufferUntil) {
			state.throwPending = state.carried
			world.motion.buffer = settings.Throw
			world.motion.bufferUntil = world.state.Tick + bufferTicks
		}
	}
	// A legal evade outranks new channels and attacks, but cannot interrupt a commitment.
	_, _, evadeSpeed := legSpeeds(world.motion.limbs)
	if evade && world.motion.action == FreeAction && evadeSpeed > 0 && world.state.Tick >= world.motion.cooldownUntil {
		return remaining
	}
	if regenerate && state.regenHeld {
		world.startLimbChannel(ctx, command, "regenerate", "", "")
	} else if interact {
		world.interactLimb(ctx, command)
		if len(state.events) > 0 && state.events[len(state.events)-1].Kind == "interaction_requested" && state.throwPending != "" {
			world.motion.buffer = ""
			state.throwPending = ""
		}
	} else if remove {
		world.startLimbChannel(ctx, command, "remove", "", removeSocket)
	}
	if state.throwPending != "" && world.motion.action == FreeAction {
		world.motion.buffer = ""
		world.startLimbThrow(ctx, command)
		state.throwPending = ""
	}
	return remaining
}

func (world *World) startLimbChannel(ctx context.Context, command PlayerCommand, kind, object string, socket SocketID) {
	defer mon.Task()(&ctx)(nil)
	state := &world.limbs
	if !world.stationary(command) || world.motion.dead || world.motion.suspended {
		state.invalid = "requires_stationary_ground"
		return
	}
	action, duration := InteractionAction, 1500*time.Millisecond
	switch kind {
	case "remove":
		action, duration = RemovalAction, 350*time.Millisecond
		if socketMask(socket)&world.motion.limbs == 0 || world.state.Health <= 8 {
			state.invalid = "cannot_remove"
			return
		}
	case "replace":
		duration = 400 * time.Millisecond
		index := world.limbIndex(object)
		if index < 0 || world.motion.limbs&socketMask(state.objects[index].Socket) != 0 {
			state.invalid = "cannot_replace"
			return
		}
		socket = state.objects[index].Socket
	case "regenerate":
		if world.motion.limbs == AllLimbs {
			state.invalid = "no_missing_limbs"
			return
		}
	}
	if err := world.StartPlayerAction(ctx, action, duration); err != nil {
		state.invalid = "action_committed"
		return
	}
	state.channel = limbChannel{kind: kind, socket: socket, object: object, until: world.motion.actionUntil}
}

func (world *World) limbIndex(id string) int {
	for i := range world.limbs.objects {
		if world.limbs.objects[i].ID == id && world.limbs.objects[i].Generation == world.limbs.generation {
			return i
		}
	}
	return -1
}

func (world *World) limbDistance(object LimbObject) float64 {
	body := world.state.Player.Bounds
	dx := math.Max(body.X-object.Position.X, math.Max(0, object.Position.X-(body.X+body.Width)))
	dy := math.Max(body.Y-object.Position.Y, math.Max(0, object.Position.Y-(body.Y+body.Height)))
	return math.Hypot(dx, dy)
}

func (world *World) limbReachable(object LimbObject) bool {
	return object.Carried || (!object.Airborne && world.limbDistance(object) <= 48)
}

// nearestCivilian implements the existing NPC-over-loose-object priority.
func (world *World) nearestCivilian(ctx context.Context) string {
	defer mon.Task()(&ctx)(nil)
	body := world.state.Player.Bounds
	best, distance, bestFacing := "", math.Inf(1), false
	for _, civilian := range world.combat.civilians {
		if civilian.Incapacitated {
			continue
		}
		box := civilian.Bounds
		dx := math.Max(0, math.Max(body.X-(box.X+box.Width), box.X-(body.X+body.Width)))
		dy := math.Max(0, math.Max(body.Y-(box.Y+box.Height), box.Y-(body.Y+body.Height)))
		d := math.Hypot(dx, dy)
		facing := (centerX(box)-centerX(body))*float64(world.motion.facing) >= 0
		if d <= 48 && (d < distance || (d == distance && (best == "" || (facing && !bestFacing) || (facing == bestFacing && civilian.ID < best)))) {
			best, distance, bestFacing = civilian.ID, d, facing
		}
	}
	return best
}

func (world *World) interactLimb(ctx context.Context, command PlayerCommand) {
	defer mon.Task()(&ctx)(nil)
	state := &world.limbs
	if world.motion.dead || world.motion.suspended || (world.motion.action >= InteractionAction && world.state.Tick < world.motion.actionUntil) {
		state.invalid = "cannot_interact"
		return
	}
	if state.carried != "" {
		world.startLimbChannel(ctx, command, "replace", state.carried, "")
		return
	}
	if world.stationary(command) {
		if civilian := world.nearestCivilian(ctx); civilian != "" {
			state.events = append(state.events, LimbEvent{Kind: "interaction_requested", Target: civilian})
			return
		}
	}
	best, distance := -1, math.Inf(1)
	body := world.state.Player.Bounds
	for i, object := range state.objects {
		if !world.limbReachable(object) || object.Generation != state.generation || world.motion.limbs&socketMask(object.Socket) != 0 {
			continue
		}
		d := world.limbDistance(object)
		facing := (object.Position.X-centerX(body))*float64(world.motion.facing) >= 0
		bestFacing := best >= 0 && (state.objects[best].Position.X-centerX(body))*float64(world.motion.facing) >= 0
		if d < distance || (d == distance && (best < 0 || (facing && !bestFacing) || (facing == bestFacing && object.ID < state.objects[best].ID))) {
			best, distance = i, d
		}
	}
	if best < 0 {
		if !world.stationary(command) {
			state.invalid = "cannot_interact"
			return
		}
		state.events = append(state.events, LimbEvent{Kind: "interaction_requested"})
		return
	}
	object := &state.objects[best]
	if world.motion.limbs&(LeftArm|RightArm) != 0 {
		object.Carried = true
		state.carried, state.payload = object.ID, true
		state.events = append(state.events, LimbEvent{Kind: "retrieved", Object: object.ID, Socket: object.Socket})
		if !world.stationary(command) {
			return
		}
	}
	world.startLimbChannel(ctx, command, "replace", object.ID, "")
}

func (world *World) startLimbThrow(ctx context.Context, command PlayerCommand) {
	defer mon.Task()(&ctx)(nil)
	state := &world.limbs
	index := world.limbIndex(state.carried)
	if world.motion.limbs&(LeftArm|RightArm) == 0 || index < 0 || !state.objects[index].Carried || world.motion.action != FreeAction {
		state.invalid = "cannot_throw_limb"
		return
	}
	if command.Aim != (Vector{}) {
		length := math.Hypot(command.Aim.X, command.Aim.Y)
		world.combat.aim = Vector{X: command.Aim.X / length, Y: command.Aim.Y / length}
		if command.Aim.X != 0 {
			world.motion.facing = int(math.Copysign(1, command.Aim.X))
		}
	}
	world.combat.nextAttack++
	state.throw = limbThrow{object: state.carried, attack: world.combat.nextAttack}
	world.combat.attack = PlayerAttack{ID: state.throw.attack, Throw: true, Start: world.state.Tick, Facing: world.motion.facing, Phase: PhaseWindup}
	world.combat.hitTargets = make(map[string]bool)
	world.motion.action = AttackAction
	world.motion.actionUntil = world.state.Tick + 48
}

func (world *World) dropCarried(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	state := &world.limbs
	if index := world.limbIndex(state.carried); index >= 0 {
		body := world.state.Player.Bounds
		object := &state.objects[index]
		object.Position = Vector{X: centerX(body), Y: body.Y + Tolerance}
		object.Carried, object.Airborne, object.Damaging = false, false, false
		object.Velocity = Vector{}
	}
	state.carried, state.payload = "", false
}

// advanceLimbActions runs after movement and before damage in the combat tick.
func (world *World) advanceLimbActions(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	state := &world.limbs
	// A moving support can leave the player airborne during the tick. A
	// lethal contact on the completion boundary still resolves afterwards.
	if !world.motion.dead && state.channel.kind != "" && !world.stationary(PlayerCommand{}) {
		world.cancelInterruptedLimbAction(ctx, PlayerCommand{})
	}
	channel := state.channel
	if channel.kind != "" && world.state.Tick >= channel.until {
		switch channel.kind {
		case "remove":
			if world.state.Health > 8 && world.motion.limbs&socketMask(channel.socket) != 0 {
				teeth := socketMask(channel.socket)&(LeftArm|RightArm) != 0 && world.motion.limbs&(LeftArm|RightArm) == socketMask(channel.socket)
				world.dropCarried(ctx)
				if err = world.SetPlayerLimbs(ctx, world.motion.limbs&^socketMask(channel.socket)); err != nil {
					return err
				}
				world.state.Health -= 8
				state.nextObject++
				body := world.state.Player.Bounds
				object := LimbObject{ID: fmt.Sprintf("limb-%d", state.nextObject), Socket: channel.socket, Generation: state.generation, Position: Vector{X: centerX(body), Y: body.Y + Tolerance}}
				if world.motion.limbs&(LeftArm|RightArm) != 0 {
					object.Carried = true
					state.carried, state.payload = object.ID, true
				}
				state.objects = append(state.objects, object)
				if teeth {
					state.events = append(state.events, LimbEvent{Kind: "teeth_assisted", Socket: object.Socket, Object: object.ID})
				}
				state.events = append(state.events, LimbEvent{Kind: "pain", Socket: object.Socket, Object: object.ID}, LimbEvent{Kind: "blood", Socket: object.Socket, Object: object.ID}, LimbEvent{Kind: "capability", Socket: object.Socket, Object: object.ID})
			}
		case "replace":
			index := world.limbIndex(channel.object)
			if index >= 0 && world.limbReachable(state.objects[index]) && world.motion.limbs&socketMask(channel.socket) == 0 {
				state.objects = slices.Delete(state.objects, index, index+1)
				if state.carried == channel.object {
					state.carried, state.payload = "", false
				}
				if err = world.SetPlayerLimbs(ctx, world.motion.limbs|socketMask(channel.socket)); err != nil {
					return err
				}
				state.events = append(state.events, LimbEvent{Kind: "replaced", Socket: channel.socket, Object: channel.object}, LimbEvent{Kind: "capability", Socket: channel.socket})
			}
		case "regenerate":
			state.objects = nil
			state.carried, state.payload = "", false
			state.generation++
			if err = world.SetPlayerLimbs(ctx, AllLimbs); err != nil {
				return err
			}
			state.events = append(state.events, LimbEvent{Kind: "regenerated"}, LimbEvent{Kind: "capability"})
		}
		state.channel = limbChannel{}
	}
	if index := world.limbIndex(state.carried); index >= 0 {
		body := world.state.Player.Bounds
		state.objects[index].Position = Vector{X: centerX(body), Y: centerY(body)}
	}
	return nil
}

// releaseLimbProjectile adapts the existing combat release boundary. Combat's
// original release assumes feces, so restore that tentative decrement and
// replace its release record before the completed tick is observable.
func (world *World) releaseLimbProjectile(ctx context.Context, attack PlayerAttack) bool {
	defer mon.Task()(&ctx)(nil)
	state := &world.limbs
	throw := &state.throw
	if !state.inTick || throw.object == "" || throw.attack != attack.ID {
		return false
	}
	world.state.Feces++
	if len(world.combat.releases) == 0 {
		state.invalid = "limb_release_missing_record"
		return true
	}
	index := world.limbIndex(throw.object)
	if index < 0 || state.carried != throw.object || !state.objects[index].Carried {
		world.combat.releases = world.combat.releases[:len(world.combat.releases)-1]
		state.invalid = "cannot_throw_limb"
		return true
	}
	object := &state.objects[index]
	direction := world.combat.aim
	if direction == (Vector{}) {
		direction = Vector{X: float64(attack.Facing)}
	}
	speed := limbSpeed(object.Socket)
	body := world.state.Player.Bounds
	object.Position = Vector{X: centerX(body), Y: centerY(body)}
	object.Velocity = Vector{X: direction.X * speed, Y: direction.Y * speed}
	object.Carried, object.Airborne, object.Damaging = false, true, true
	object.Bounced = false
	object.ignored = ""
	object.Attack = throw.attack
	object.FlightTick = world.state.Tick
	state.carried, state.payload = "", false
	world.combat.releases[len(world.combat.releases)-1] = AttackRelease{AttackID: throw.attack, ProjectileID: object.ID, Payload: string(object.Socket)}
	state.events = append(state.events, LimbEvent{Kind: "released", Object: object.ID, Socket: object.Socket})
	return true
}

func limbSpeed(socket SocketID) float64 {
	if socketMask(socket)&(LeftArm|RightArm) != 0 {
		return 650
	}
	return 600
}

// limbTargets is shared by the projectile tick and its trajectory preview.
func (world *World) limbTargets(extra []Collider) []Collider {
	targets := slices.Clone(extra)
	for index, area := range world.combat.protection {
		if validRect(area, false) {
			targets = append(targets, Collider{ID: fmt.Sprintf("combat-protection:%d", index), Kind: ProjectileTarget, Bounds: area})
		}
	}
	for _, enemy := range world.combat.enemies {
		if enemy.Health > 0 {
			targets = append(targets, Collider{ID: "combat-enemy:" + enemy.ID, Kind: ProjectileTarget, Bounds: enemy.Body.Bounds})
		}
	}
	for _, civilian := range world.combat.civilians {
		if !civilian.Incapacitated {
			targets = append(targets, Collider{ID: "combat-civilian:" + civilian.ID, Kind: ProjectileTarget, Bounds: civilian.Bounds})
		}
	}
	return targets
}

// LimbPreview contains the fixed-tick trajectory up to 600 pixels or the first
// impact. The points and impact are independent of the authoritative world.
type LimbPreview struct {
	Points []Vector
	Impact *Impact
}

// PreviewLimbThrow uses the same launch speed, gravity and swept collision as
// a real limb throw. A zero aim uses the last direction, then facing.
func (world *World) PreviewLimbThrow(ctx context.Context, aim Vector, targets []Collider) (_ LimbPreview, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil || !world.state.HasPlayer || !validVector(aim) || math.Abs(aim.X) > 1 || math.Abs(aim.Y) > 1 || world.motion.limbs&(LeftArm|RightArm) == 0 || !world.limbs.payload {
		return LimbPreview{}, fmt.Errorf("cannot preview limb throw")
	}
	index := world.limbIndex(world.limbs.carried)
	if index < 0 || !world.limbs.objects[index].Carried {
		return LimbPreview{}, fmt.Errorf("no carried limb")
	}
	for _, target := range targets {
		if target.ID == "" || target.Kind != ProjectileTarget || !validRect(target.Bounds, false) {
			return LimbPreview{}, fmt.Errorf("invalid limb target")
		}
	}
	if aim == (Vector{}) {
		aim = world.combat.aim
	}
	if aim == (Vector{}) {
		aim = Vector{X: float64(world.motion.facing)}
	}
	length := math.Hypot(aim.X, aim.Y)
	if length == 0 {
		return LimbPreview{}, fmt.Errorf("invalid throw direction")
	}
	speed := limbSpeed(world.limbs.objects[index].Socket)
	velocity := Vector{X: aim.X * speed / length, Y: aim.Y * speed / length}
	body := world.state.Player.Bounds
	position := Vector{X: centerX(body), Y: centerY(body)}
	preview := LimbPreview{Points: []Vector{position}}
	colliders := world.limbTargets(targets)
	distance := 0.0
	for i := 0; i < 240 && distance < 600; i++ {
		velocity.Y -= 1600 * TickSeconds
		delta := Vector{X: velocity.X * TickSeconds, Y: velocity.Y * TickSeconds}
		step := math.Hypot(delta.X, delta.Y)
		if step > 600-distance {
			ratio := (600 - distance) / step
			delta.X *= ratio
			delta.Y *= ratio
			step = 600 - distance
		}
		impact, found, sweepErr := world.SweepTargets(ctx, Rect{X: position.X, Y: position.Y}, delta, colliders, SweepOptions{Projectile: true})
		if sweepErr != nil {
			return LimbPreview{}, sweepErr
		}
		if found {
			position.X += delta.X * impact.Time
			position.Y += delta.Y * impact.Time
			preview.Points = append(preview.Points, position)
			preview.Impact = &impact
			return preview, nil
		}
		position.X += delta.X
		position.Y += delta.Y
		preview.Points = append(preview.Points, position)
		distance += step
	}
	return preview, nil
}

func (world *World) advanceLimbProjectiles(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	for i := range world.limbs.objects {
		object := &world.limbs.objects[i]
		if !object.Airborne || object.Generation != world.limbs.generation {
			continue
		}
		if world.state.Tick-object.FlightTick >= 240 {
			object.Airborne, object.Damaging = false, false
			object.Velocity = Vector{}
			continue
		}
		object.Velocity.Y -= 1600 * TickSeconds
		delta := Vector{X: object.Velocity.X * TickSeconds, Y: object.Velocity.Y * TickSeconds}
		var targets []Collider
		if object.Damaging {
			targets = world.limbTargets(world.limbs.targets)
		}
		impact, found, sweepErr := world.SweepTargets(ctx, Rect{X: object.Position.X, Y: object.Position.Y}, delta, targets, SweepOptions{Projectile: true, Ignore: object.ignored})
		if sweepErr != nil {
			return sweepErr
		}
		if !found {
			object.Position.X += delta.X
			object.Position.Y += delta.Y
			continue
		}
		object.Position.X += delta.X * impact.Time
		object.Position.Y += delta.Y * impact.Time
		if impact.Collider.Kind == ProjectileTarget && !object.Damaging {
			object.ignored = impact.Collider.ID
			continue
		}
		world.limbs.events = append(world.limbs.events, LimbEvent{Kind: "impact", Object: object.ID, Socket: object.Socket, Target: impact.Collider.ID})
		if impact.Collider.Kind == ProjectileTarget {
			object.Damaging = false
			object.ignored = impact.Collider.ID
			direction := 1
			if object.Velocity.X < 0 {
				direction = -1
			}
			damage := 35
			if socketMask(object.Socket)&(LeftArm|RightArm) != 0 {
				damage = 25
			}
			for j := range world.combat.enemies {
				enemy := &world.combat.enemies[j]
				if impact.Collider.ID == "combat-enemy:"+enemy.ID {
					world.applyEnemyHit(enemy, DamageHit{Attacker: "player", Attack: fmt.Sprint(object.Attack), Kind: OrdinaryDamage, Amount: damage, Direction: direction})
				}
			}
			for j := range world.combat.civilians {
				if impact.Collider.ID == "combat-civilian:"+world.combat.civilians[j].ID {
					world.combat.civilians[j].Incapacitated = true
				}
			}
			continue
		}
		object.Damaging = false
		if object.Bounced || impact.Normal == (Vector{}) {
			object.Airborne = false
			object.Velocity = Vector{}
			continue
		}
		object.Bounced = true
		object.Velocity.X *= 0.35
		object.Velocity.Y *= 0.35
		if impact.Normal.X != 0 {
			object.Velocity.X = -object.Velocity.X
		}
		if impact.Normal.Y != 0 {
			object.Velocity.Y = -object.Velocity.Y
		}
		object.Position.X += impact.Normal.X * Tolerance
		object.Position.Y += impact.Normal.Y * Tolerance
	}
	return nil
}

// LimbWeights returns settled object identities whose positions lie in a plate.
// Regeneration and retrieval remove those weights on the same tick.
func (world *World) LimbWeights(ctx context.Context, plate Rect) (_ []string, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || !validRect(plate, false) {
		return nil, fmt.Errorf("invalid plate")
	}
	var weights []string
	for _, object := range world.limbs.objects {
		if object.Generation == world.limbs.generation && !object.Carried && !object.Airborne && object.Position.X >= plate.X && object.Position.X <= plate.X+plate.Width && object.Position.Y >= plate.Y && object.Position.Y <= plate.Y+plate.Height {
			weights = append(weights, object.ID)
		}
	}
	slices.Sort(weights)
	return weights, nil
}

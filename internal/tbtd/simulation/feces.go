// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

const (
	fecesCapacity = 5
	fecesSpeed    = 600.0
	fecesGravity  = 1400.0
)

// FecesTargetKind identifies an explicitly authored projectile receptor.
type FecesTargetKind string

const (
	// FecesReceptor is marked machinery that accepts a ticket-bound impact.
	FecesReceptor FecesTargetKind = "receptor"
	// FecesMachinery blocks projectiles but has no unmarked feces reaction.
	FecesMachinery FecesTargetKind = "machinery"
	// FecesBoss accepts damage only during an identified exposed recovery.
	FecesBoss FecesTargetKind = "boss"
	// FecesSource exposes an impact and timing for a recruitment source.
	FecesSource FecesTargetKind = "source"
)

// FecesTarget is an explicit collision volume. Boss recovery IDs belong to the
// encounter executor and must be unique per recovery within an attempt; an
// empty recovery or protected boss rejects the hit.
type FecesTarget struct {
	Collider Collider
	Kind     FecesTargetKind
	Ticket   content.DeedID
	Exposed  bool
	Recovery string
}

// RefillStation is a marked, unlimited source; reporting never blocks its use.
type RefillStation struct {
	ID        string
	Position  Vector
	Reporting bool
}

// WastePickup is sealed ammunition, not an enemy hazard.
type WastePickup struct {
	ID        string
	Bounds    Rect
	Collected bool
}

// FecesImpact describes one consumed projectile and its first collision.
// Boss damage and stagger are candidates for the boss executor, not a victory.
type FecesImpact struct {
	Projectile    string
	Target        string
	Ticket        content.DeedID
	Kind          string
	Position      Vector
	Damage        int
	StaggerTicks  int
	Recovery      string
	Tick          uint64
	StopTicks     int
	CooldownTicks int
}

// Smear is temporary non-damaging terrain for the physical-reaction system.
type Smear struct {
	ID       string
	Surface  string
	Position Vector
	Expires  uint64
}

// WasteCandidate identifies the attempt and the campaign-owned contact to prepare.
type WasteCandidate struct {
	Attempt campaign.Attempt
	Contact campaign.Contact
}

// FecesView is an isolated transient view. Impacts are for the last tick;
// pending waste remains available until the session commits or restores.
type FecesView struct {
	Portions     int
	Impacts      []FecesImpact
	Smears       []Smear
	Pickups      []WastePickup
	PendingWaste *WasteCandidate
	Channel      string
	Remaining    uint64
	Invalid      string
}

type distraction struct {
	Until       uint64
	ImmuneUntil uint64
}

type fecesState struct {
	stations     []RefillStation
	pickups      []WastePickup
	targets      []FecesTarget
	impacts      []FecesImpact
	smears       []Smear
	distracted   map[string]distraction
	bossRecovery map[string]string
	attempt      campaign.Attempt
	reported     bool
	pending      *WasteCandidate
	channel      string
	until        uint64
	held         bool
	inTick       bool
	invalid      string
}

func cloneFeces(state fecesState) fecesState {
	state.stations = slices.Clone(state.stations)
	state.pickups = slices.Clone(state.pickups)
	state.targets = slices.Clone(state.targets)
	state.impacts = slices.Clone(state.impacts)
	state.smears = slices.Clone(state.smears)
	if state.pending != nil {
		pending := *state.pending
		state.pending = &pending
	}
	if state.distracted != nil {
		copyMap := make(map[string]distraction, len(state.distracted))
		for id, timer := range state.distracted {
			copyMap[id] = timer
		}
		state.distracted = copyMap
	}
	if state.bossRecovery != nil {
		copyMap := make(map[string]string, len(state.bossRecovery))
		for id, recovery := range state.bossRecovery {
			copyMap[id] = recovery
		}
		state.bossRecovery = copyMap
	}
	return state
}

// FecesView returns detached data for presentation and later reaction work.
func (world *World) FecesView(ctx context.Context) FecesView {
	defer mon.Task()(&ctx)(nil)
	if world == nil {
		return FecesView{}
	}
	state := cloneFeces(world.feces)
	var remaining uint64
	if state.until > world.state.Tick {
		remaining = state.until - world.state.Tick
	}
	return FecesView{Portions: world.state.Feces, Impacts: state.impacts, Smears: state.smears, Pickups: state.pickups, PendingWaste: state.pending, Channel: state.channel, Remaining: remaining, Invalid: state.invalid}
}

// AddRefillStation registers a marked station. Existing authored stations are
// installed during world construction, including ministry reporting stations.
func (world *World) AddRefillStation(ctx context.Context, station RefillStation) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || station.ID == "" || !validVector(station.Position) {
		return fmt.Errorf("invalid refill station")
	}
	for _, existing := range world.feces.stations {
		if existing.ID == station.ID {
			return fmt.Errorf("duplicate refill station %s", station.ID)
		}
	}
	world.feces.stations = append(world.feces.stations, station)
	slices.SortFunc(world.feces.stations, func(a, b RefillStation) int { return compareID(a.ID, b.ID) })
	return nil
}

// AddWastePickup registers a sealed pickup. It remains when capacity is full.
func (world *World) AddWastePickup(ctx context.Context, pickup WastePickup) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || pickup.ID == "" || pickup.Collected || !validRect(pickup.Bounds, false) {
		return fmt.Errorf("invalid waste pickup")
	}
	for _, existing := range world.feces.pickups {
		if existing.ID == pickup.ID {
			return fmt.Errorf("duplicate waste pickup %s", pickup.ID)
		}
	}
	world.feces.pickups = append(world.feces.pickups, pickup)
	slices.SortFunc(world.feces.pickups, func(a, b WastePickup) int { return compareID(a.ID, b.ID) })
	return nil
}

// FecesCommand advances the inherited limb/combat tick. Station is the selected
// marked interaction target, not an instruction to refill without a hold.
// Targets supplies current boss/source volumes in addition to authored cameras.
type FecesCommand struct {
	Limb    LimbCommand
	Station string
	Targets []FecesTarget
}

// StepFeces advances a single authoritative tick. Use this entry point while
// feces interactions are active; it delegates movement and combat to StepLimbs.
func (world *World) StepFeces(ctx context.Context, command FecesCommand) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil {
		return fmt.Errorf("invalid feces tick")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	base := cloneFeces(world.feces)
	before, previous, motion, combat, limbs := cloneState(world.state), cloneState(world.previous), world.motion, cloneCombat(world.combat), cloneLimbs(world.limbs)
	defer func() {
		if err != nil {
			world.state, world.previous, world.motion, world.combat, world.limbs, world.feces = before, previous, motion, combat, limbs, base
		}
	}()
	ids := make(map[string]bool)
	for _, target := range world.feces.targets {
		ids[target.Collider.ID] = true
	}
	for _, target := range command.Targets {
		if target.Collider.ID == "" || ids[target.Collider.ID] || target.Collider.Kind != ProjectileTarget || !validRect(target.Collider.Bounds, false) || (target.Kind != FecesReceptor && target.Kind != FecesMachinery && target.Kind != FecesBoss && target.Kind != FecesSource) {
			return fmt.Errorf("invalid feces target %q", target.Collider.ID)
		}
		ids[target.Collider.ID] = true
	}
	world.feces.targets = append(world.feces.targets, command.Targets...)
	world.feces.inTick = true
	world.feces.impacts = nil
	world.feces.invalid = ""
	world.prepareDistraction()
	world.refillInput(ctx, &command)
	if err = world.StepLimbs(ctx, command.Limb); err != nil {
		return err
	}
	world.cancelRefill(command.Limb.Combat.Player)
	if world.feces.invalid == "" {
		switch world.combat.invalid {
		case "throw_empty":
			world.feces.invalid = "refill_at_marked_station"
		case "throw_requires_arm":
			world.feces.invalid = "reattach_or_regenerate_arm"
		}
	}
	world.feces.targets = world.feces.targets[:len(base.targets)]
	world.feces.inTick = false
	return nil
}

func (world *World) station(id string) (RefillStation, bool) {
	for _, station := range world.feces.stations {
		if station.ID == id {
			return station, true
		}
	}
	return RefillStation{}, false
}

func (world *World) stationReachable(station RefillStation) bool {
	body := world.state.Player.Bounds
	dx := math.Max(0, math.Max(body.X-station.Position.X, station.Position.X-(body.X+body.Width)))
	dy := math.Max(0, math.Max(body.Y-station.Position.Y, station.Position.Y-(body.Y+body.Height)))
	return math.Hypot(dx, dy) <= 48
}

func (world *World) cancelRefill(command PlayerCommand) {
	state := &world.feces
	if state.channel == "" {
		return
	}
	station, ok := world.station(state.channel)
	if !world.limbs.interactHeld {
		state.held = false
	}
	if !ok || !state.held || world.motion.action != InteractionAction || world.motion.dead || world.motion.suspended || !world.stationary(command) || !world.stationReachable(station) {
		state.channel, state.until = "", 0
		world.limbs.interactHeld = false
		if world.motion.action == InteractionAction {
			world.motion.action = FreeAction
		}
	}
}

func (world *World) refillInput(ctx context.Context, command *FecesCommand) {
	defer mon.Task()(&ctx)(nil)
	state := &world.feces
	player := &command.Limb.Combat.Player
	var edges []PlayerEdge
	var evade bool
	_, _, speed := legSpeeds(world.motion.limbs)
	free := world.motion.action == FreeAction || world.state.Tick >= world.motion.actionUntil
	if free && speed > 0 && world.state.Tick >= world.motion.cooldownUntil {
		held := world.motion.held&edgeBit(settings.Evade) != 0
		for _, edge := range player.Edges {
			if edge.Action == settings.Evade {
				evade = evade || (edge.Pressed && !held)
				held = edge.Pressed
			}
		}
	}
	for _, edge := range player.Edges {
		if edge.Action != settings.Interact {
			edges = append(edges, edge)
			continue
		}
		fresh := edge.Pressed && !state.held
		state.held = edge.Pressed
		// The inherited carried-socket interaction takes precedence over a
		// station; let StepLimbs process both press and release edges.
		if state.channel == "" && world.limbs.carried != "" {
			edges = append(edges, edge)
			continue
		}
		if state.channel != "" || command.Station != "" {
			if fresh && state.channel == "" && !evade {
				station, ok := world.station(command.Station)
				if !ok || !world.stationReachable(station) || !world.stationary(*player) || world.nearestCivilian(ctx) != "" {
					state.invalid = "cannot_refill"
				} else if startErr := world.StartPlayerAction(ctx, InteractionAction, 1500*time.Millisecond); startErr != nil {
					state.invalid = "action_committed"
				} else {
					state.channel, state.until = station.ID, world.state.Tick+180
					// SuspendPlayer clears this inherited hold marker before Resume.
					world.limbs.interactHeld = true
				}
			}
			continue
		}
		edges = append(edges, edge)
	}
	player.Edges = edges
	world.cancelRefill(*player)
}

// advanceFecesChannel runs at the completion boundary, before damage.
func (world *World) advanceFecesChannel(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	state := &world.feces
	// Lethal movement on the completion boundary is resolved after the
	// completed channel, just like the inherited limb channels.
	if !world.motion.dead || state.channel == "" || world.state.Tick < state.until {
		world.cancelRefill(PlayerCommand{})
	}
	if state.channel != "" && world.state.Tick >= state.until {
		station, _ := world.station(state.channel)
		world.state.Feces = fecesCapacity
		if station.Reporting && !state.reported && state.attempt.Deed != "" && state.attempt.Number != 0 {
			state.pending = &WasteCandidate{Attempt: state.attempt, Contact: campaign.Contact{Kind: campaign.IncidentWaste, Detail: station.ID}}
			state.reported = true
		}
		state.channel, state.until = "", 0
		world.limbs.interactHeld = false
	}
	for i := range state.pickups {
		pickup := &state.pickups[i]
		if !pickup.Collected && !world.motion.dead && world.state.Feces < fecesCapacity && overlaps(pickup.Bounds, world.state.Player.Bounds) {
			world.state.Feces++
			pickup.Collected = true
		}
	}
	state.smears = slices.DeleteFunc(state.smears, func(smear Smear) bool { return world.state.Tick >= smear.Expires })
}

// PrepareWaste delegates incident identity, deduplication and debt to campaign.
// The returned snapshot is only a proposal and must not be published before save.
func (world *World) PrepareWaste(ctx context.Context, owner *campaign.Campaign, committed campaign.Snapshot, transition campaign.Transition) (_ campaign.Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || owner == nil || world.feces.pending == nil {
		return campaign.Snapshot{}, fmt.Errorf("no pending waste contact")
	}
	transition.Contacts = append(slices.Clone(transition.Contacts), world.feces.pending.Contact)
	data := committed.Data(ctx)
	// A committed certification can clear the active attempt. Only campaign
	// may decide whether a retry has the original identity and exact payload.
	if data.LatestOperation != nil && transition.Sequence == data.OperationSequence {
		return owner.Prepare(ctx, committed, transition)
	}
	attempt, ok := committed.ActiveAttempt()
	if !ok || attempt.Deed != world.feces.pending.Attempt.Deed || attempt.Number != world.feces.pending.Attempt.Number {
		return campaign.Snapshot{}, fmt.Errorf("waste attempt changed")
	}
	return owner.Prepare(ctx, committed, transition)
}

func (world *World) prepareDistraction() {
	for i := range world.combat.enemies {
		enemy := &world.combat.enemies[i]
		timer := world.feces.distracted[enemy.ID]
		if enemy.Health > 0 && world.state.Tick < timer.Until {
			enemy.Phase, enemy.Until = EnemyHurt, timer.Until
			enemy.StrikeTick = 0
			if world.state.Tick >= enemy.HurtUntil {
				enemy.Body.Velocity.X = 0
			}
		}
	}
}

func (world *World) fecesImpact(projectile Projectile, impact Impact, position Vector) {
	state := &world.feces
	event := FecesImpact{Projectile: projectile.ID, Target: impact.Collider.ID, Position: position, Tick: world.state.Tick, Kind: "blocked"}
	for _, target := range state.targets {
		if target.Collider.ID != impact.Collider.ID {
			continue
		}
		event.Ticket, event.Recovery = target.Ticket, target.Recovery
		switch target.Kind {
		case FecesReceptor:
			event.Kind = "receptor"
		case FecesSource:
			event.Kind, event.StopTicks, event.CooldownTicks = "source", 240, 480
		case FecesBoss:
			if target.Exposed && target.Recovery != "" {
				event.Kind, event.Damage = "boss_exposed", 8
				if state.bossRecovery == nil {
					state.bossRecovery = make(map[string]string)
				}
				if state.bossRecovery[target.Collider.ID] != target.Recovery {
					event.StaggerTicks = 30
					state.bossRecovery[target.Collider.ID] = target.Recovery
				}
			} else {
				event.Kind = "boss_protected"
			}
		}
		break
	}
	for i := range world.combat.enemies {
		enemy := &world.combat.enemies[i]
		if impact.Collider.ID != "combat-enemy:"+enemy.ID {
			continue
		}
		event.Kind, event.Damage = "enemy", 8
		if state.distracted == nil {
			state.distracted = make(map[string]distraction)
		}
		timer := state.distracted[enemy.ID]
		if world.state.Tick >= timer.ImmuneUntil {
			timer = distraction{Until: world.state.Tick + 144, ImmuneUntil: world.state.Tick + 144 + 240}
			state.distracted[enemy.ID] = timer
			enemy.Phase, enemy.Until, enemy.StrikeTick = EnemyHurt, timer.Until, 0
		}
	}
	if impact.Collider.Kind != ProjectileTarget && impact.Normal.Y > 0 {
		event.Kind = "floor"
		state.smears = slices.DeleteFunc(state.smears, func(smear Smear) bool {
			return smear.Surface == impact.Collider.ID && math.Abs(smear.Position.X-position.X) <= 24
		})
		state.smears = append(state.smears, Smear{ID: projectile.ID, Surface: impact.Collider.ID, Position: position, Expires: world.state.Tick + 480})
	}
	state.impacts = append(state.impacts, event)
}

// Distraction reports the actual active and subsequent immunity intervals.
func (world *World) Distraction(id string) (bool, bool) {
	if world == nil {
		return false, false
	}
	timer := world.feces.distracted[id]
	return world.state.Tick < timer.Until, world.state.Tick >= timer.Until && world.state.Tick < timer.ImmuneUntil
}

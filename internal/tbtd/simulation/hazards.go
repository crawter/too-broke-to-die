// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"slices"

	"too-broke-to-die/internal/tbtd/content"
)

// HazardPhase describes the authoritative station interval.
type HazardPhase string

const (
	// HazardDormant is visible before an accepted attempt leaves the vestibule.
	HazardDormant HazardPhase = "dormant"
	// HazardWarning marks a station that is about to activate.
	HazardWarning HazardPhase = "warning"
	// HazardActive marks the interval in which the station can cause damage.
	HazardActive HazardPhase = "active"
	// HazardSafe marks an inactive or held-open station between activations.
	HazardSafe HazardPhase = "inactive"
)

// HazardView contains both the marked danger volume and its waiting floor.
type HazardView struct {
	ID       string
	Kind     string
	Bounds   Rect
	Waiting  Rect
	Phase    HazardPhase
	Lethal   bool
	HeldOpen bool
}

type encounterState struct {
	hazards          []content.BayHazard
	features         []content.BayFeature
	variant          int
	ticket           content.DeedID
	tickets          []content.DeedID
	room             content.InstitutionBay
	warningText      string
	aftermathText    string
	started          bool
	elapsed          int
	crusherShift     int
	crusherHolding   bool
	feed             recruitmentState
	emergency        emergencyRuntime
	barrierPending   bool
	aftermathPending bool
	aftermathActors  bool
}

func cloneEncounter(state encounterState) encounterState {
	state.hazards = slices.Clone(state.hazards)
	state.features = slices.Clone(state.features)
	state.tickets = slices.Clone(state.tickets)
	state.feed = cloneRecruitment(state.feed)
	return state
}

func (world *World) installEncounter() {
	world.installRecruitment()
}

func msTicks(ms int) int { return (ms*120 + 999) / 1000 }

func (world *World) crusherHeld() bool {
	for _, feature := range world.encounter.features {
		if feature.ID != "CRUSHER-CONTROL-L" && feature.ID != "CRUSHER-CONTROL-R" {
			continue
		}
		plate := Rect{X: feature.X, Y: feature.Y, Width: feature.Width, Height: feature.Height}
		if overlaps(plate, world.state.Player.Bounds) {
			return true
		}
		for _, object := range world.objects.items {
			if object.Kind == ObjectCrate && !object.Restoring && overlaps(plate, object.Bounds) {
				return true
			}
		}
		for _, limb := range world.limbs.objects {
			if limb.Generation == world.limbs.generation && !limb.Carried && !limb.Airborne && limb.Position.X >= plate.X && limb.Position.X <= plate.X+plate.Width && limb.Position.Y >= plate.Y && limb.Position.Y <= plate.Y+plate.Height {
				return true
			}
		}
	}
	return false
}

func (world *World) stationViews() []HazardView {
	views := make([]HazardView, 0, len(world.encounter.hazards))
	for index, hazard := range world.encounter.hazards {
		kind := map[content.LocationID]string{"LOC-HOS": "steam", "LOC-CHU": "censer", "LOC-MIN": "waste", "LOC-INS": "cable"}[world.state.Location]
		if world.encounter.room.Family == "UTL" {
			kind = "electrical_arc"
		}
		if hazard.Lethal {
			kind = "crusher"
		}
		view := HazardView{ID: fmt.Sprintf("station-%s-%02d", world.service.attempt.Deed, index+1), Kind: kind, Bounds: rectFromContent(hazard.Bounds), Waiting: rectFromContent(hazard.Waiting), Phase: HazardDormant, Lethal: hazard.Lethal}
		if hazard.Lethal {
			// The authored player-only catwalk passes over the crushing zone.
			for _, feature := range world.encounter.features {
				if feature.ID == "CATWALK" && feature.Y > view.Bounds.Y && feature.Y < view.Bounds.Y+view.Bounds.Height {
					view.Bounds.Height = feature.Y - view.Bounds.Y
				}
			}
		}
		if world.encounter.started && world.service.status == ServiceActive {
			warning, active, inactive := msTicks(hazard.WarningMS), msTicks(hazard.ActiveMS), msTicks(hazard.InactiveMS)
			elapsed := world.encounter.elapsed - msTicks(hazard.OffsetMS)
			if hazard.Lethal {
				elapsed -= world.encounter.crusherShift
				if world.encounter.crusherHolding && !world.crusherHeld() {
					elapsed = 0
				}
			}
			if elapsed < 0 {
				view.Phase = HazardSafe
			} else {
				phase := elapsed % (warning + active + inactive)
				switch {
				case phase < warning:
					view.Phase = HazardWarning
				case phase < warning+active:
					view.Phase = HazardActive
				default:
					view.Phase = HazardSafe
				}
			}
			if hazard.Lethal && world.crusherHeld() {
				view.HeldOpen, view.Phase = true, HazardSafe
			}
		}
		views = append(views, view)
	}
	return views
}

// Hazards returns a detached, stable-order view of the current station phases.
func (world *World) Hazards(ctx context.Context) []HazardView {
	defer mon.Task()(&ctx)(nil)
	if world == nil {
		return nil
	}
	return world.stationViews()
}

// StepEncounter advances an authored bay including service, hazards, emergency
// and recruitment. It is the full-bay entry point; lower-level StepServices is
// retained for callers executing an isolated service machine. The caller must
// stop on any pending durable candidate, and may not run while paused.
func (world *World) StepEncounter(ctx context.Context, command ServiceCommand) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil || world.encounter.room.Encounter != world.state.Encounter {
		return fmt.Errorf("not a service bay")
	}
	if world.encounter.emergency.pending != "" {
		return fmt.Errorf("emergency transition awaiting commit")
	}
	if len(command.Hazards) != 0 || command.CrusherActive || !validHits(command.Objects.Feces.Limb.Combat.Hits) {
		return fmt.Errorf("invalid encounter hazard inputs")
	}
	before, previous, motion, grid := cloneState(world.state), cloneState(world.previous), world.motion, world.grid
	combat, limbs, feces, objects := cloneCombat(world.combat), cloneLimbs(world.limbs), cloneFeces(world.feces), cloneObjects(world.objects)
	service, encounter := copyService(world.service), cloneEncounter(world.encounter)
	defer func() {
		if err != nil {
			world.state, world.previous, world.motion, world.grid = before, previous, motion, grid
			world.combat, world.limbs, world.feces, world.objects = combat, limbs, feces, objects
			world.service, world.encounter = service, encounter
		}
	}()
	if err = world.syncEmergency(ctx); err != nil {
		return err
	}
	world.prepareRescueActor()
	world.syncRecruitment()
	queuedAtStart := len(world.encounter.feed.queue)
	world.recruitmentTargets(&command)
	held := world.crusherHeld()
	if world.encounter.crusherHolding && !held {
		// Releasing a held-open crusher must give a fresh visible warning.
		world.encounter.crusherShift = world.encounter.elapsed
	}
	world.encounter.crusherHolding = held
	stations := world.stationViews()
	if world.service.attempt.Deed != "" {
		command.Hazards = make([]bool, len(stations))
	}
	for i, station := range stations {
		active := station.Phase == HazardActive
		if command.Hazards != nil {
			command.Hazards[i] = active
		}
		if station.Lethal && active {
			command.CrusherActive = true
		}
	}
	// Predict environmental contacts on an isolated copy, then hand them to
	// the inherited combat tick so it orders them with enemy and input hits.
	hits, err := world.predictHazardHits(ctx, command, stations)
	if err != nil {
		return err
	}
	command.Objects.Feces.Limb.Combat.Hits = append(slices.Clone(command.Objects.Feces.Limb.Combat.Hits), hits...)
	if err = world.StepServices(ctx, command); err != nil {
		return err
	}
	if err = world.recruitmentImpacts(ctx, combat.attack, motion.action); err != nil {
		return err
	}
	if err = world.advanceRecruitment(ctx, queuedAtStart, before.Tick); err != nil {
		return err
	}
	if !world.encounter.started && world.service.status == ServiceActive && world.state.Player.Bounds.X >= 240 {
		world.encounter.started = true
	} else if world.encounter.started && world.service.status == ServiceActive {
		world.encounter.elapsed++
	}
	if command.CrusherActive && world.service.status == ServiceActive {
		world.supplyCrusher()
	}
	// The prediction supplied damage to combat; this advances the committed
	// transient clock and records actual completed cleanup or escape candidates.
	world.advanceEmergency(before.Player.Bounds)
	if world.motion.dead {
		world.state.Health = 0
	}
	if world.motion.action != AttackAction {
		world.combat.attack = PlayerAttack{}
		world.combat.hitTargets = nil
	}
	world.cancelInterruptedLimbAction(ctx, PlayerCommand{})
	world.cancelRefill(PlayerCommand{})
	if world.objects.channel.target.ID != "" && world.motion.action != InteractionAction {
		world.objects.events = append(world.objects.events, InteractionEvent{Kind: "cancelled", Target: world.objects.channel.target.ID})
		world.objects.channel = interactionChannel{}
		world.objects.held, world.limbs.interactHeld = false, false
	}
	world.motion.immune = !world.motion.dead && (world.state.Tick < world.combat.immuneUntil || (world.motion.action == EvadeAction && before.Tick < world.motion.immuneUntil))
	world.aftermathCollision(before.Player.Bounds, command.Objects.Feces.Limb.Combat.Player.Move)
	if err = world.installEmergencyBarrier(ctx); err != nil {
		return err
	}
	if err = world.installAftermath(ctx); err != nil {
		return err
	}
	return nil
}

func (world *World) copyEncounterWorld(ctx context.Context) *World {
	defer mon.Task()(&ctx)(nil)
	result := *world
	result.state, result.previous = cloneState(world.state), cloneState(world.previous)
	result.combat, result.limbs, result.feces = cloneCombat(world.combat), cloneLimbs(world.limbs), cloneFeces(world.feces)
	result.objects, result.service = cloneObjects(world.objects), copyService(world.service)
	result.encounter = cloneEncounter(world.encounter)
	result.lethal = slices.Clone(world.lethal)
	return &result
}

// predictHazardHits uses the same movement and collision code as the real tick.
// The copy owns every mutable collection, so prediction publishes no evidence,
// resource consumption, enemy damage or campaign transition.
func (world *World) predictHazardHits(ctx context.Context, command ServiceCommand, stations []HazardView) (_ []DamageHit, err error) {
	defer mon.Task()(&ctx)(&err)
	needed := false
	for _, station := range stations {
		needed = needed || station.Phase == HazardActive
	}
	emergency := world.encounter.emergency
	needed = needed || (emergency.id != "" && emergency.warning == 0 && validRect(world.emergencyDanger(emergency.active), false))
	if !needed {
		return nil, nil
	}
	trial := world.copyEncounterWorld(ctx)
	command.Objects.Feces.Limb.Combat.Hits = nil
	if err = trial.StepServices(ctx, command); err != nil {
		return nil, err
	}
	var hits []DamageHit
	for i, station := range stations {
		if station.Phase != HazardActive || trial.protected(trial.state.Player.Bounds) || !sweptOverlap(world.state.Player.Bounds, trial.state.Player.Bounds, station.Bounds) {
			continue
		}
		kind, amount := OrdinaryDamage, 15
		if station.Lethal {
			kind, amount = LethalDamage, 100
		}
		hazard := world.encounter.hazards[i]
		cycleTicks := msTicks(hazard.WarningMS) + msTicks(hazard.ActiveMS) + msTicks(hazard.InactiveMS)
		cycle := (world.encounter.elapsed - msTicks(hazard.OffsetMS)) / cycleTicks
		hits = append(hits, DamageHit{Attacker: station.ID, Attack: fmt.Sprint(cycle), Kind: kind, Amount: amount, Direction: 1})
	}
	hits = append(hits, trial.advanceEmergency(world.state.Player.Bounds)...)
	return hits, nil
}

// The adult's authored centre is just beyond the first 760 station. Its own
// 16-pixel collision width leaves that holding point clear; the independently
// specified 28-pixel player box is never changed.
func (world *World) prepareRescueActor() {
	if world.service.deed.Family != "RES" {
		return
	}
	for i := range world.combat.civilians {
		adult := &world.combat.civilians[i]
		if adult.ID == string(world.service.roles[content.RoleActor]) && adult.Bounds.Width != 16 {
			adult.Bounds.X = centerX(adult.Bounds) - 8
			adult.Bounds.Width = 16
		}
	}
}

func sweptOverlap(from, to, area Rect) bool {
	if overlaps(from, area) || overlaps(to, area) {
		return true
	}
	_, hit := impactTime(from, Vector{X: to.X - from.X, Y: to.Y - from.Y}, Collider{Kind: Solid, Bounds: area})
	return hit
}

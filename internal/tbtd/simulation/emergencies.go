// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
)

// AftermathEffect identifies the permanent local result without granting credit.
// Bounds locate the authored remnants; Decorative effects never block movement.
type AftermathEffect struct {
	ID         string
	Kind       string
	Bounds     Rect
	Decorative bool
}

// EmergencyExecution is transient presentation data; Stage remains the last
// acknowledged campaign stage even while an aftermath candidate is pending.
type EmergencyExecution struct {
	ID           content.EmergencyID
	Stage        string
	Kind         string
	Hazard       string
	Warning      string
	Aftermath    string
	RefugeName   string
	Refuge       Rect
	Envelope     Rect
	Barrier      Rect
	Danger       Rect
	WarningTicks int
	ActiveTicks  int
	BypassUsable bool
	Candidate    content.EmergencyID
	EventID      string
	Effects      []AftermathEffect
}

type emergencyRuntime struct {
	id       content.EmergencyID
	warning  int
	active   int
	pending  content.EmergencyID
	prepared *campaign.Snapshot
}

func (world *World) syncEmergency(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	for _, stage := range world.state.Emergencies {
		if stage.ID != world.encounter.room.Emergency || stage.Encounter != world.state.Encounter {
			continue
		}
		if stage.Stage == campaign.EmergencyAftermath {
			world.encounter.barrierPending = true
			if err = world.installEmergencyBarrier(ctx); err != nil {
				return err
			}
			if !world.encounter.aftermathActors {
				world.encounter.aftermathPending = world.encounter.room.AftermathBlocking
				if err = world.installAftermath(ctx); err != nil {
					return err
				}
				if err = world.installAftermathActors(ctx); err != nil {
					return err
				}
				world.objects.interactions = slices.DeleteFunc(world.objects.interactions, func(target InteractionTarget) bool { return target.ID == string(stage.ID)+"-cleanup" })
				world.encounter.emergency = emergencyRuntime{}
			}
			return nil
		}
		if stage.Stage != campaign.EmergencyActive {
			continue
		}
		if world.encounter.emergency.id == stage.ID {
			return world.installEmergencyBarrier(ctx)
		}
		world.encounter.barrierPending = true
		if err = world.installEmergencyBarrier(ctx); err != nil {
			return err
		}
		world.encounter.emergency = emergencyRuntime{id: stage.ID, warning: 180}
		refuge := rectFromContent(world.encounter.room.Space.Refuge)
		target := InteractionTarget{ID: string(stage.ID) + "-cleanup", Kind: InteractionService, Bounds: refuge, Prerequisite: "optional emergency cleanup", HoldTicks: 36}
		if err = world.AddInteractionTarget(ctx, target); err != nil {
			return err
		}
		return nil
	}
	return nil
}

// EmergencyView exposes the authored warning, marked area and bypass, without
// turning a candidate into a committed campaign stage.
func (world *World) EmergencyView(ctx context.Context) EmergencyExecution {
	defer mon.Task()(&ctx)(nil)
	if world == nil || world.encounter.room.Encounter != world.state.Encounter {
		return EmergencyExecution{}
	}
	room := world.encounter.room
	var stage string
	for _, item := range world.state.Emergencies {
		if item.ID == room.Emergency {
			stage = item.Stage
			break
		}
	}
	if stage == "" {
		return EmergencyExecution{}
	}
	runtime := world.encounter.emergency
	warning := runtime.warning
	if runtime.id == "" && stage == campaign.EmergencyActive {
		warning = 180
	}
	view := EmergencyExecution{ID: room.Emergency, Stage: stage, Kind: room.Space.Kind, Hazard: room.Hazard, Warning: world.encounter.warningText, Aftermath: world.encounter.aftermathText, RefugeName: room.Space.RefugeName, Refuge: rectFromContent(room.Space.Refuge), Envelope: rectFromContent(room.Space.Envelope), Barrier: rectFromContent(room.Space.Barrier), WarningTicks: warning, ActiveTicks: runtime.active, BypassUsable: true, Candidate: runtime.pending}
	if runtime.pending != "" {
		view.EventID = campaign.EmergencyTransitionID(runtime.pending)
	}
	if stage == campaign.EmergencyAftermath {
		view.Effects = []AftermathEffect{{ID: string(room.Emergency) + "-remains", Kind: aftermathKind(room.Emergency), Bounds: rectFromContent(room.Aftermath), Decorative: !room.AftermathBlocking}}
	}
	if stage == campaign.EmergencyActive && warning == 0 && runtime.active < 960 {
		view.Danger = world.emergencyDanger(runtime.active)
	}
	return view
}

func aftermathKind(id content.EmergencyID) string {
	switch id {
	case "EM-HOS-SUP":
		return "steaming_pipe"
	case "EM-HOS-RES":
		return "wrecked_beds"
	case "EM-HOS-UTL":
		return "damaged_manifold"
	case "EM-HOS-PRV":
		return "burnt_screens"
	case "EM-CHU-SUP":
		return "wax_and_tins"
	case "EM-CHU-RES":
		return "broken_turnstile"
	case "EM-CHU-UTL":
		return "bell_exhibit"
	case "EM-CHU-PRV":
		return "paid_scandal_advertisement"
	case "EM-MIN-SUP":
		return "processing_report"
	case "EM-MIN-RES":
		return "desks"
	case "EM-MIN-UTL":
		return "contaminated_floor"
	case "EM-MIN-PRV":
		return "audit_success_file"
	case "EM-INS-SUP":
		return "sales_graphic"
	case "EM-INS-RES":
		return "fallen_arch"
	case "EM-INS-UTL":
		return "replacement_advertisements"
	case "EM-INS-PRV":
		return "attention_report"
	}
	return ""
}

func (world *World) emergencyDanger(active int) Rect {
	space := world.encounter.room.Space
	switch space.Kind {
	case "area":
		return rectFromContent(space.Envelope)
	case "fall":
		if active == 0 {
			return rectFromContent(space.Envelope)
		}
	case "wave":
		start, end := rectFromContent(space.Start), rectFromContent(space.End)
		distance := space.Speed * float64(active) * TickSeconds
		if end.X < start.X {
			distance = -distance
			if start.X+distance < end.X {
				return Rect{}
			}
		} else if start.X+distance > end.X {
			return Rect{}
		}
		start.X += distance
		return start
	}
	return Rect{}
}

// advanceEmergency counts only resumed gameplay ticks. The one-shot wave or
// fall is never replayed as damaging rubble; area hazards use normal immunity.
func (world *World) advanceEmergency(previous Rect) []DamageHit {
	runtime := &world.encounter.emergency
	if runtime.id == "" || runtime.pending != "" {
		return nil
	}
	for _, event := range world.objects.events {
		if event.Kind == "completed" && event.Target == string(runtime.id)+"-cleanup" {
			runtime.pending = runtime.id
		}
	}
	body := world.state.Player.Bounds
	// Reaching either end of the high bypass is an actual crossing, not the
	// safe ground-level spawn used when an active emergency is restored.
	if body.X >= 1450 || (body.X+body.Width <= 240 && body.Y >= 150) {
		// Movement and the escape candidate precede same-tick lethal damage.
		runtime.pending = runtime.id
	}
	if runtime.warning > 0 {
		runtime.warning--
		for i := range world.state.Emergencies {
			if world.state.Emergencies[i].ID == runtime.id {
				world.state.Emergencies[i].WarningTicks = runtime.warning
			}
		}
		return nil
	}
	area := world.emergencyDanger(runtime.active)
	contact := validRect(area, false) && sweptOverlap(previous, body, area)
	if world.encounter.room.Space.Kind == "wave" && validRect(area, false) {
		next := world.emergencyDanger(runtime.active + 1)
		if !validRect(next, false) {
			next = rectFromContent(world.encounter.room.Space.End)
		}
		// Sweep relative motion so neither the wave nor a moving player can
		// tunnel through the other's marked collision box.
		relative := moved(body, Vector{X: area.X - next.X, Y: area.Y - next.Y})
		contact = contact || sweptOverlap(previous, relative, area)
	}
	var hits []DamageHit
	if contact && !world.protected(body) {
		amount := 15
		attack := fmt.Sprint(world.state.Tick)
		if world.encounter.room.Space.Kind == "fall" {
			amount, attack = 35, "fall"
		} else if world.encounter.room.Space.Kind == "wave" {
			attack = "wave"
		}
		hits = append(hits, DamageHit{Attacker: string(runtime.id), Attack: attack, Kind: OrdinaryDamage, Amount: amount, Direction: 1})
	}
	runtime.active++
	if runtime.active >= 960 {
		runtime.pending = runtime.id
	}
	return hits
}

func emergencyBarrierID(id content.EmergencyID) string {
	return "emergency-barrier-" + string(id)
}

// installEmergencyBarrier never materializes a solid around an occupied body.
// If the player or another actor is in the way, subsequent ticks retry it.
func (world *World) installEmergencyBarrier(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if !world.encounter.barrierPending {
		return nil
	}
	id := emergencyBarrierID(world.encounter.room.Emergency)
	for _, collider := range world.grid.colliders {
		if collider.ID == id {
			world.encounter.barrierPending = false
			return nil
		}
	}
	area := rectFromContent(world.encounter.room.Space.Barrier)
	if world.occupiedVolume(area, "", true) {
		return nil
	}
	colliders := append(slices.Clone(world.grid.colliders), Collider{ID: id, Kind: Solid, Bounds: area})
	grid, err := NewGrid(ctx, colliders)
	if err != nil {
		return err
	}
	world.grid = grid
	world.encounter.barrierPending = false
	return nil
}

// installAftermath defers a permanent solid while any live body or movable
// object occupies it; a committed aftermath must never materialize inside one.
func (world *World) installAftermath(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if !world.encounter.aftermathPending {
		return nil
	}
	area := rectFromContent(world.encounter.room.Aftermath)
	if overlaps(area, world.state.Player.Bounds) {
		return nil
	}
	for _, enemy := range world.combat.enemies {
		if enemy.Health > 0 && overlaps(area, enemy.Body.Bounds) {
			return nil
		}
	}
	for _, civilian := range world.combat.civilians {
		if overlaps(area, civilian.Bounds) {
			return nil
		}
	}
	for _, object := range world.objects.items {
		if objectBlocks(object) && overlaps(area, object.Bounds) {
			return nil
		}
	}
	colliders := slices.Clone(world.grid.colliders)
	colliders = append(colliders, Collider{ID: "aftermath-" + string(world.encounter.room.Emergency), Kind: Solid, Bounds: area})
	grid, err := NewGrid(ctx, colliders)
	if err != nil {
		return err
	}
	world.grid = grid
	world.encounter.aftermathPending = false
	return nil
}

// installAftermathActors reconstructs the people affected by the committed
// outcome. The actor positions are transient representations, not new deeds.
func (world *World) installAftermathActors(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world.encounter.aftermathActors {
		return nil
	}
	room := world.encounter.room
	committed := false
	for _, stage := range world.state.Emergencies {
		if stage.ID == room.Emergency && stage.Encounter == world.state.Encounter && stage.Stage == campaign.EmergencyAftermath {
			committed = true
			break
		}
	}
	if !committed {
		return nil
	}
	switch room.Emergency {
	case "EM-HOS-UTL":
		// CHR-NIN is the PDD's established character identity. The manual
		// bypass side of the barrier is outside the damaged manifold.
		x := room.Space.Barrier.X + room.Space.Barrier.Width + 48
		box := Rect{X: x - 8, Y: 0, Width: 16, Height: PlayerHeight}
		if err = world.AddProtectedCivilian(ctx, Civilian{ID: "CHR-NIN", Bounds: box}); err != nil {
			return err
		}
		world.state.Entities = append(world.state.Entities, Entity{ID: "CHR-NIN", Role: content.RoleActor, Kind: "nina_manifold", Position: Vector{X: x}, Protected: true})
		world.combat.protection = append(world.combat.protection, Rect{X: x - 24, Y: 0, Width: 48, Height: 100})
	case "EM-CHU-RES", "EM-MIN-RES":
		index := 0
		for i := range world.state.Entities {
			entity := &world.state.Entities[i]
			if entity.Role != content.RoleActor || entity.Ticket == "" {
				continue
			}
			x := room.Space.Refuge.X + 5 + float64(index)*6
			kind := "adult_refuge"
			if room.Emergency == "EM-MIN-RES" {
				x = room.Aftermath.X - 190 + float64(index)*28
				kind = "worker_desk"
			}
			entity.Position = Vector{X: x}
			entity.Kind, entity.Protected = kind, true
			box := Rect{X: x - 8, Y: 0, Width: 16, Height: PlayerHeight}
			found := false
			for j := range world.combat.civilians {
				civilian := &world.combat.civilians[j]
				if civilian.ID == string(entity.ID) {
					civilian.Bounds, civilian.Incapacitated = box, false
					found = true
					break
				}
			}
			if !found {
				if err = world.AddProtectedCivilian(ctx, Civilian{ID: string(entity.ID), Bounds: box}); err != nil {
					return err
				}
			}
			if room.Emergency == "EM-MIN-RES" {
				world.combat.protection = append(world.combat.protection, Rect{X: x - 12, Y: 0, Width: 24, Height: 80})
			}
			index++
		}
		if room.Emergency == "EM-CHU-RES" {
			world.combat.protection = append(world.combat.protection, Rect{X: room.Space.Refuge.X - 16, Y: 0, Width: room.Space.Refuge.Width + 32, Height: room.Space.Refuge.Height})
		}
	}
	slices.SortFunc(world.state.Entities, func(a, b Entity) int { return compareID(string(a.ID), string(b.ID)) })
	world.encounter.aftermathActors = true
	return nil
}

// aftermathCollision produces a bounded, harmless reaction when the player
// walks into the broken sponsored turnstile, not when standing still beside it.
func (world *World) aftermathCollision(before Rect, move float64) {
	if world.encounter.room.Emergency != "EM-CHU-RES" || !world.encounter.aftermathActors || world.motion.dead || world.motion.action != FreeAction || move == 0 {
		return
	}
	area := rectFromContent(world.encounter.room.Aftermath)
	body := world.state.Player.Bounds
	if !sweptOverlap(before, body, area) || (move > 0 && centerX(before) >= centerX(area)) || (move < 0 && centerX(before) <= centerX(area)) {
		return
	}
	direction := -1
	if move < 0 {
		direction = 1
	}
	world.beginReaction("player", "EM-CHU-RES-turnstile", "turnstile_collision", direction, 84)
}

// AcknowledgeEncounterService applies a saved service result and its local
// emergency geometry together. Callers playing a full bay use this instead of
// the isolated service machine's AcknowledgeService method.
func (world *World) AcknowledgeEncounterService(ctx context.Context, owner *campaign.Campaign, manifest *content.Campaign, bays []content.ServiceBay, result ServiceAcknowledgment) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil {
		return fmt.Errorf("invalid encounter world")
	}
	trial := world.copyEncounterWorld(ctx)
	if err = trial.AcknowledgeService(ctx, owner, manifest, bays, result); err != nil {
		return err
	}
	if err = trial.syncEmergency(ctx); err != nil {
		return err
	}
	*world = *trial
	return nil
}

// PrepareEmergency combines an aftermath candidate with any same-tick death
// in one campaign transition. A proposal is not a committed stage.
func (world *World) PrepareEmergency(ctx context.Context, owner *campaign.Campaign, committed campaign.Snapshot, transition campaign.Transition) (_ campaign.Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || owner == nil || world.encounter.emergency.pending == "" || transition.Aftermath != "" {
		return campaign.Snapshot{}, fmt.Errorf("no emergency candidate")
	}
	if !reflect.DeepEqual(committed.Data(ctx), world.service.committed.Data(ctx)) && (world.encounter.emergency.prepared == nil || !reflect.DeepEqual(committed.Data(ctx), world.encounter.emergency.prepared.Data(ctx))) {
		return campaign.Snapshot{}, fmt.Errorf("emergency uses a different committed snapshot")
	}
	transition.Aftermath = world.encounter.emergency.pending
	proposal, err := owner.Prepare(ctx, committed, transition)
	if err != nil {
		return campaign.Snapshot{}, err
	}
	if world.encounter.emergency.prepared != nil && !reflect.DeepEqual(world.encounter.emergency.prepared.Data(ctx), proposal.Data(ctx)) {
		return campaign.Snapshot{}, fmt.Errorf("emergency proposal changed")
	}
	world.encounter.emergency.prepared = &proposal
	return proposal, nil
}

// AcknowledgeEmergency publishes only the exact successful storage result.
func (world *World) AcknowledgeEmergency(ctx context.Context, owner *campaign.Campaign, result ServiceAcknowledgment) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || owner == nil || world.encounter.emergency.prepared == nil || !result.Succeeded {
		return fmt.Errorf("emergency save has not succeeded")
	}
	data := result.Snapshot.Data(ctx)
	if result.CampaignID == "" || result.CampaignID != data.CampaignID || result.Sequence == 0 || result.Sequence != data.OperationSequence || !reflect.DeepEqual(world.encounter.emergency.prepared.Data(ctx), data) {
		return fmt.Errorf("emergency acknowledgment mismatch")
	}
	if err = owner.Validate(ctx, result.Snapshot); err != nil {
		return err
	}
	state, combat, objects := cloneState(world.state), cloneCombat(world.combat), cloneObjects(world.objects)
	encounter, grid := cloneEncounter(world.encounter), world.grid
	defer func() {
		if err != nil {
			world.state, world.combat, world.objects = state, combat, objects
			world.encounter, world.grid = encounter, grid
		}
	}()
	id := world.encounter.emergency.id
	world.encounter.barrierPending = true
	if err = world.installEmergencyBarrier(ctx); err != nil {
		return err
	}
	world.encounter.aftermathPending = world.encounter.room.AftermathBlocking
	if err = world.installAftermath(ctx); err != nil {
		world.encounter.aftermathPending = false
		return err
	}
	for i := range world.state.Emergencies {
		if world.state.Emergencies[i].ID == id {
			world.state.Emergencies[i].Stage = campaign.EmergencyAftermath
			world.state.Emergencies[i].WarningTicks = 0
		}
	}
	if err = world.installAftermathActors(ctx); err != nil {
		return err
	}
	world.objects.interactions = slices.DeleteFunc(world.objects.interactions, func(target InteractionTarget) bool { return target.ID == string(id)+"-cleanup" })
	world.encounter.emergency = emergencyRuntime{}
	world.service.committed = result.Snapshot
	return nil
}

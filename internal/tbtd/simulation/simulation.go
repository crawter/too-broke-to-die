// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"slices"

	monkit "github.com/spacemonkeygo/monkit/v3"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
)

var mon = monkit.Package()

// World is the sole owner of its transient state. It is not concurrent-safe.
type World struct {
	grid     *Grid
	state    State
	previous State
	motion   playerMotion
	limbs    limbState
	lethal   []Rect
	combat   combatState
}

// TickInput supplies one actor's velocity and the one-way platform currently
// excluded by a drop. Session controls whether a tick is allowed to run.
type TickInput struct {
	Velocity    Vector
	DropThrough string
}

// NewWorld derives its restoration plan from the validated committed campaign.
// bays must be the concrete output of content.LoadServiceBays. The world takes
// copies and never modifies the campaign, content, or persisted progress.
func NewWorld(ctx context.Context, owner *campaign.Campaign, manifest *content.Campaign, committed campaign.Snapshot, bays []content.ServiceBay, platforms []Platform) (_ *World, err error) {
	defer mon.Task()(&ctx)(&err)
	if owner == nil || manifest == nil {
		return nil, fmt.Errorf("missing campaign or content manifest")
	}
	if err = validateBays(ctx, manifest, bays); err != nil {
		return nil, err
	}
	plan, err := owner.RestorePlan(ctx, committed)
	if err != nil {
		return nil, fmt.Errorf("restoration plan: %w", err)
	}
	state := State{
		Location: plan.Location, Anchor: plan.Anchor, Mode: plan.State,
		SceneEntry: plan.SceneEntry, Ending: plan.Ending,
		ActiveTicket: plan.ActiveTicket, Objective: plan.InitialObjective,
		Health: plan.Health, Limbs: plan.Limbs, Feces: plan.Feces,
		Routes: slices.Clone(plan.Routes), ItemRoutes: slices.Clone(plan.ItemRoutes),
		OwnedItems: slices.Clone(plan.OwnedItems), Installed: slices.Clone(plan.Installed),
		Defeated: slices.Clone(plan.Defeated),
	}
	for _, receipt := range plan.Receipts {
		state.Receipts = append(state.Receipts, receipt.Certification.Deed)
	}
	slices.Sort(state.Receipts)
	slices.Sort(state.Routes)
	slices.Sort(state.ItemRoutes)
	slices.Sort(state.OwnedItems)
	slices.Sort(state.Installed)
	slices.Sort(state.Defeated)
	for _, emergency := range plan.Emergencies {
		if emergency.WarningMilliseconds <= 0 || !emergency.BehindSafeBoundary || !emergency.BypassUsable {
			return nil, fmt.Errorf("unsafe emergency %s", emergency.Emergency.ID)
		}
		warning := 0
		if emergency.Emergency.Stage == campaign.EmergencyActive {
			warning = int(math.Ceil(float64(emergency.WarningMilliseconds) * 120 / 1000))
		} else if emergency.Emergency.Stage != campaign.EmergencyAftermath {
			return nil, fmt.Errorf("unknown emergency stage %s", emergency.Emergency.Stage)
		}
		state.Emergencies = append(state.Emergencies, EmergencyState{ID: emergency.Emergency.ID, Encounter: emergency.Encounter, Stage: emergency.Emergency.Stage, WarningTicks: warning, SafeBoundary: true, BypassUsable: true})
	}
	slices.SortFunc(state.Emergencies, func(a, b EmergencyState) int { return compareID(string(a.ID), string(b.ID)) })
	world := &World{state: state}
	if plan.Ending != "" && (plan.State == "ST-SCN" || plan.State == "ST-END") {
		if len(platforms) != 0 {
			return nil, fmt.Errorf("ending cannot contain combat platforms")
		}
		world.previous = cloneState(world.state)
		return world, nil
	}
	if plan.Health != 100 || plan.Limbs != 4 || plan.Feces != 5 || !plan.ClearTransient || plan.Location == "" || plan.Anchor == "" {
		return nil, fmt.Errorf("invalid gameplay recovery plan")
	}
	var location *content.InstitutionGeometry
	seen := make(map[content.DeedID]bool, len(bays))
	for i := range bays {
		bay := &bays[i]
		if bay.Deed == "" || seen[bay.Deed] || bay.Encounter == "" || bay.Institution.Location == "" {
			return nil, fmt.Errorf("invalid encounter binding %s", bay.Deed)
		}
		seen[bay.Deed] = true
		if bay.Institution.Location == plan.Location && location == nil {
			location = &bay.Institution
		}
	}
	if location == nil {
		return nil, fmt.Errorf("missing location %s", plan.Location)
	}
	var colliders []Collider
	var bounds, floor content.Rect
	var solids, lethal []content.Rect
	var spawn content.GeometryPoint
	found := false
	isBay := false
	for _, prologue := range location.Prologue {
		if prologue.Anchor == plan.Anchor {
			found = true
			world.state.Encounter, bounds, floor, solids, lethal, spawn = prologue.Encounter, prologue.Bounds, prologue.Floor, prologue.Solids, prologue.Lethal, prologue.Spawn
			for i, platform := range prologue.Platforms {
				colliders = append(colliders, Collider{ID: fmt.Sprintf("prologue-platform-%03d", i), Kind: OneWay, Bounds: rectFromContent(platform)})
			}
		}
	}
	boss := location.Boss
	if plan.Anchor == boss.Anchor || plan.Anchor == boss.OutAnchor {
		found = true
		world.state.Encounter, bounds, floor, solids, lethal, spawn = boss.Encounter, boss.Bounds, boss.Floor, boss.Solids, boss.Lethal, boss.Checkpoint
		if plan.Anchor == boss.OutAnchor {
			spawn = boss.Exit
		}
		for i, platform := range boss.Platforms {
			colliders = append(colliders, Collider{ID: fmt.Sprintf("boss-platform-%03d", i), Kind: OneWay, Bounds: rectFromContent(platform)})
		}
		world.state.BossActive = !slices.Contains(plan.Defeated, boss.Encounter)
		world.state.Entities = append(world.state.Entities,
			Entity{ID: boss.Terminal, Role: content.RoleTerminal, Position: Vector{X: boss.TerminalPosition.X, Y: boss.TerminalPosition.Y}, Protected: true},
			Entity{ID: content.ObjectID("station-" + string(boss.Encounter) + "-CHECKPOINT"), Role: content.RoleCheckpoint, Position: Vector{X: boss.Checkpoint.X, Y: boss.Checkpoint.Y}, Protected: true},
		)
		for i, refill := range boss.Refills {
			world.state.Entities = append(world.state.Entities, Entity{ID: content.ObjectID(fmt.Sprintf("station-%s-REFILL-%03d", boss.Encounter, i)), Role: content.RoleRefill, Position: Vector{X: refill.X, Y: refill.Y}, Protected: true})
		}
		slices.SortFunc(world.state.Entities, func(a, b Entity) int { return compareID(string(a.ID), string(b.ID)) })
	}
	for _, lobby := range location.ReturnLobbies {
		if lobby.Anchor == plan.Anchor {
			found = true
			bounds, floor, solids, lethal, spawn = lobby.Bounds, lobby.Floor, lobby.Solids, lobby.Lethal, lobby.Spawn
			world.state.Encounter = "ENC-MIN-05"
		}
	}
	for _, room := range location.Bays {
		if room.Anchor != plan.Anchor {
			continue
		}
		found = true
		isBay = true
		world.state.Encounter = room.Encounter
		var selected *content.ServiceBay
		for i := range bays {
			bay := &bays[i]
			if bay.Encounter != room.Encounter {
				continue
			}
			if selected == nil || bay.Deed == plan.ActiveTicket || (selected.Deed != plan.ActiveTicket && bay.Deed < selected.Deed) {
				selected = bay
			}
		}
		if selected == nil {
			return nil, fmt.Errorf("missing bay %s", room.Encounter)
		}
		world.combat.protection = append(world.combat.protection, rectFromContent(selected.Safety.VestibuleProtect), rectFromContent(selected.Safety.RefugeProtect))
		bounds = content.Rect{X: 0, Y: -8, Width: selected.Template.Width, Height: selected.Template.Height + 8}
		floor = selected.Template.Floor
		spawn = content.GeometryPoint{X: 96, Y: 0}
		gateOpen := false
		for _, placed := range selected.Objects {
			if placed.Object.Role == content.RoleGate && slices.Contains(plan.ItemRoutes, placed.Object.ID) {
				gateOpen = true
			}
		}
		for _, terrain := range selected.Template.Terrain {
			if terrain.Kind == "gate" && gateOpen {
				continue
			}
			kind := ColliderKind(terrain.Kind)
			switch terrain.Kind {
			case "player_platform":
				kind = OneWay
			case "gate":
				kind = Solid
			case "crusher":
				continue // timed crusher belongs to hazard execution
			}
			colliders = append(colliders, Collider{ID: "terrain-" + terrain.ID, Kind: kind, Bounds: rectFromContent(terrain.Bounds)})
		}
		if selected.Variant.CoverKind == "player_projectile" {
			colliders = append(colliders, Collider{ID: "variant-cover", Kind: Solid, Bounds: rectFromContent(selected.Variant.Cover)})
		}
		for _, hazard := range selected.Hazards {
			lethal = append(lethal, hazard.Bounds)
		}
		for i, platform := range selected.Safety.Platforms {
			colliders = append(colliders, Collider{ID: fmt.Sprintf("bypass-%03d", i), Kind: OneWay, Bounds: rectFromContent(platform)})
		}
		for _, emergency := range world.state.Emergencies {
			if emergency.ID != room.Emergency {
				continue
			}
			colliders = append(colliders, Collider{ID: "emergency-barrier-" + string(emergency.ID), Kind: Solid, Bounds: rectFromContent(room.Space.Barrier)})
			if emergency.Stage == campaign.EmergencyActive {
				lethal = append(lethal, room.Space.Envelope)
			}
			if emergency.Stage == campaign.EmergencyAftermath && room.AftermathBlocking {
				colliders = append(colliders, Collider{ID: "aftermath-" + string(emergency.ID), Kind: Solid, Bounds: rectFromContent(room.Aftermath)})
			}
		}
		world.state.Entities = restoreEntities(ctx, bays, room.Encounter, plan)
		for _, station := range selected.Vestibule {
			world.state.Entities = append(world.state.Entities, Entity{ID: content.ObjectID("station-" + string(room.Encounter) + "-" + string(station.Role)), Role: station.Role, Position: Vector{X: station.X, Y: station.Y}, Protected: true})
		}
		if selected.Template.ID == "BAY-PRV" {
			for _, feature := range selected.Template.Features {
				if feature.ID == "REFILL" {
					world.state.Entities = append(world.state.Entities, Entity{ID: content.ObjectID("station-" + string(room.Encounter) + "-REFILL-BAY"), Role: content.RoleRefill, Position: Vector{X: feature.X, Y: feature.Y}, Protected: true})
				}
			}
		}
		if selected.Deed == plan.ActiveTicket {
			for i, enemy := range selected.Variant.Enemies {
				world.state.Entities = append(world.state.Entities, Entity{ID: content.ObjectID(fmt.Sprintf("enemy-%s-%03d", selected.Deed, i)), Kind: enemy.Kind, Position: Vector{X: enemy.X}, Ticket: selected.Deed})
			}
		}
		slices.SortFunc(world.state.Entities, func(a, b Entity) int { return compareID(string(a.ID), string(b.ID)) })
	}
	if !found || !validRect(rectFromContent(bounds), false) || !validRect(rectFromContent(floor), false) || !validVector(Vector{X: spawn.X, Y: spawn.Y}) {
		return nil, fmt.Errorf("unknown or invalid anchor %s", plan.Anchor)
	}
	colliders = append(colliders, Collider{ID: "boundary-floor", Kind: Solid, Bounds: rectFromContent(floor)})
	colliders = append(colliders,
		Collider{ID: "boundary-left", Kind: Solid, Bounds: Rect{X: bounds.X - 8, Y: bounds.Y, Width: 8, Height: bounds.Height}},
		Collider{ID: "boundary-right", Kind: Solid, Bounds: Rect{X: bounds.X + bounds.Width, Y: bounds.Y, Width: 8, Height: bounds.Height}},
		Collider{ID: "boundary-ceiling", Kind: Solid, Bounds: Rect{X: bounds.X, Y: bounds.Y + bounds.Height, Width: bounds.Width, Height: 8}},
	)
	for i, solid := range solids {
		colliders = append(colliders, Collider{ID: fmt.Sprintf("solid-%03d", i), Kind: Solid, Bounds: rectFromContent(solid)})
	}
	world.grid, err = NewGrid(ctx, colliders)
	if err != nil {
		return nil, err
	}
	world.state.Player = Body{ID: "player", Bounds: Rect{X: spawn.X - PlayerWidth/2, Y: spawn.Y, Width: PlayerWidth, Height: PlayerHeight}}
	world.state.HasPlayer = true
	world.motion = newPlayerMotion()
	// Authored static lethal volumes are distinct from timed hazard stations.
	// Those stations are activated by the later hazard execution system.
	if !isBay {
		for _, volume := range lethal {
			world.lethal = append(world.lethal, rectFromContent(volume))
		}
	}
	body := world.state.Player.Bounds
	if body.X < bounds.X || body.Y < bounds.Y || body.X+body.Width > bounds.X+bounds.Width || body.Y+body.Height > bounds.Y+bounds.Height {
		return nil, fmt.Errorf("anchor %s is outside the encounter", plan.Anchor)
	}
	if err = world.validateSpawn(ctx, lethal, platforms); err != nil {
		return nil, err
	}
	world.state.Platforms = slices.Clone(platforms)
	slices.SortFunc(world.state.Platforms, func(a, b Platform) int { return compareID(a.Collider.ID, b.Collider.ID) })
	world.state.Player.Support, err = world.grid.Support(ctx, world.state.Player.Bounds, platformColliders(world.state.Platforms), "")
	if err != nil {
		return nil, err
	}
	if world.state.Player.Support == "" {
		return nil, fmt.Errorf("anchor %s has no support", plan.Anchor)
	}
	for _, entity := range world.state.Entities {
		if entity.Kind == "MELEE" || entity.Kind == "RANGED" {
			kind := EnemyKind(entity.Kind)
			if plan.Location == "LOC-INS" && kind == MeleeEnemy {
				kind = FollowerEnemy
			}
			box := Rect{X: entity.Position.X - PlayerWidth/2, Y: entity.Position.Y, Width: PlayerWidth, Height: PlayerHeight}
			if err = world.AddEnemy(ctx, string(entity.ID), kind, box); err != nil {
				return nil, fmt.Errorf("enemy placement: %w", err)
			}
		}
		if entity.Role == content.RoleActor {
			box := Rect{X: entity.Position.X - PlayerWidth/2, Y: entity.Position.Y, Width: PlayerWidth, Height: PlayerHeight}
			if err = world.AddProtectedCivilian(ctx, Civilian{ID: string(entity.ID), Bounds: box}); err != nil {
				return nil, fmt.Errorf("civilian placement: %w", err)
			}
		}
	}
	world.previous = cloneState(world.state)
	return world, nil
}

// validateBays checks that mutable expanded records still match the validated
// manifest and that repeated authored layouts have not diverged.
func validateBays(ctx context.Context, manifest *content.Campaign, bays []content.ServiceBay) (err error) {
	defer mon.Task()(&ctx)(&err)
	if len(bays) != 96 {
		return fmt.Errorf("expected 96 authored service bays")
	}
	expected := make(map[content.DeedID]content.CampaignDeed)
	for _, deed := range manifest.Deeds(ctx) {
		if deed.Family != "END" {
			expected[deed.Deed.ID] = deed
		}
	}
	objects := make(map[content.ObjectID]content.Object)
	for _, object := range manifest.Objects(ctx) {
		objects[object.ID] = object
	}
	layouts := make(map[content.LocationID]content.InstitutionGeometry)
	templates := make(map[string]content.BayTemplate)
	variants := make(map[int]content.BayVariant)
	safety := make(map[content.EncounterID]content.BaySafety)
	seen := make(map[content.DeedID]bool)
	for _, bay := range bays {
		deed, ok := expected[bay.Deed]
		if !ok || seen[bay.Deed] || bay.Encounter != deed.Deed.Encounter || bay.Anchor != deed.Deed.Anchor || bay.Emergency != deed.Emergency || bay.Institution.Location != deed.Location || bay.Template.ID != "BAY-"+deed.Family || bay.Variant.Number != deed.Variant || len(bay.Objects) != len(deed.Deed.Objects) {
			return fmt.Errorf("invalid bay binding %s", bay.Deed)
		}
		seen[bay.Deed] = true
		if bay.Template.Width != 1536 || bay.Template.Height != 560 || !validRect(rectFromContent(bay.Template.Floor), false) || bay.Template.Floor.Y+bay.Template.Floor.Height != 0 {
			return fmt.Errorf("invalid bay floor %s", bay.Deed)
		}
		checkObjects := make(map[content.ObjectID]bool)
		for _, placed := range bay.Objects {
			original, exists := objects[placed.Object.ID]
			if !exists || original != placed.Object || checkObjects[placed.Object.ID] || !slices.Contains(deed.Deed.Objects, placed.Object.ID) || !validVector(Vector{X: placed.X, Y: placed.Y}) {
				return fmt.Errorf("invalid object in %s", bay.Deed)
			}
			checkObjects[placed.Object.ID] = true
			matched := false
			for _, point := range bay.Template.Objects {
				if point.Role == placed.Object.Role && point.X == placed.X && point.Y == placed.Y {
					matched = true
				}
			}
			if !matched {
				return fmt.Errorf("invalid object origin %s", placed.Object.ID)
			}
		}
		if previous, exists := layouts[deed.Location]; exists && !reflect.DeepEqual(previous, bay.Institution) {
			return fmt.Errorf("inconsistent layout %s", deed.Location)
		}
		layouts[deed.Location] = bay.Institution
		if previous, exists := templates[deed.Family]; exists && !reflect.DeepEqual(previous, bay.Template) {
			return fmt.Errorf("inconsistent template %s", deed.Family)
		}
		templates[deed.Family] = bay.Template
		if previous, exists := variants[deed.Variant]; exists && !reflect.DeepEqual(previous, bay.Variant) {
			return fmt.Errorf("inconsistent variant %d", deed.Variant)
		}
		variants[deed.Variant] = bay.Variant
		if previous, exists := safety[bay.Encounter]; exists && !reflect.DeepEqual(previous, bay.Safety) {
			return fmt.Errorf("inconsistent safety %s", bay.Encounter)
		}
		safety[bay.Encounter] = bay.Safety
	}
	return nil
}

func restoreEntities(ctx context.Context, bays []content.ServiceBay, encounter content.EncounterID, plan campaign.RestorationPlan) []Entity {
	defer mon.Task()(&ctx)(nil)
	entities := make(map[content.ObjectID]Entity)
	for _, bay := range bays {
		if bay.Encounter != encounter {
			continue
		}
		for _, placed := range bay.Objects {
			object := placed.Object
			mandatory := slices.Contains(plan.MandatoryActors, object.ID) || slices.Contains(plan.ProtectedTerminals, object.ID)
			initial := plan.ActiveTicket == bay.Deed && slices.Contains(plan.InitialObjects, object.ID)
			if mandatory || initial {
				entities[object.ID] = Entity{ID: object.ID, Role: object.Role, Position: Vector{X: placed.X, Y: placed.Y}, Ticket: object.Deed, Protected: mandatory}
			}
		}
	}
	result := make([]Entity, 0, len(entities))
	for _, entity := range entities {
		result = append(result, entity)
	}
	slices.SortFunc(result, func(a, b Entity) int { return compareID(string(a.ID), string(b.ID)) })
	return result
}

func (world *World) validateSpawn(ctx context.Context, lethal []content.Rect, platforms []Platform) (err error) {
	defer mon.Task()(&ctx)(&err)
	body := world.state.Player.Bounds
	candidates, err := world.grid.Query(ctx, body)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, collider := range world.grid.colliders {
		seen[collider.ID] = true
	}
	for _, collider := range candidates {
		if (collider.Kind == Solid || collider.Kind == PlayerOnly) && overlaps(body, collider.Bounds) {
			return fmt.Errorf("spawn overlaps %s", collider.ID)
		}
	}
	for _, volume := range lethal {
		if !validRect(rectFromContent(volume), false) || overlaps(body, rectFromContent(volume)) {
			return fmt.Errorf("invalid or lethal spawn")
		}
	}
	for _, platform := range platforms {
		collider := platform.Collider
		if collider.ID == "" || seen[collider.ID] || !validRect(collider.Bounds, false) || !validVector(platform.Velocity) || (collider.Kind != Solid && collider.Kind != OneWay) || overlaps(body, collider.Bounds) {
			return fmt.Errorf("invalid platform or overlapping spawn %s", collider.ID)
		}
		seen[collider.ID] = true
		candidates, err = world.grid.Query(ctx, collider.Bounds)
		if err != nil {
			return err
		}
		for _, other := range candidates {
			if (other.Kind == Solid || other.Kind == PlayerOnly) && overlaps(collider.Bounds, other.Bounds) {
				return fmt.Errorf("platform %s overlaps %s", collider.ID, other.ID)
			}
		}
	}
	for i := range platforms {
		for j := 0; j < i; j++ {
			if overlaps(platforms[i].Collider.Bounds, platforms[j].Collider.Bounds) {
				return fmt.Errorf("overlapping platforms")
			}
		}
	}
	return nil
}

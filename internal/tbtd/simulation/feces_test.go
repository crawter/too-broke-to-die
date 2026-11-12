// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

func fecesTick(t *testing.T, world *World, command FecesCommand) {
	t.Helper()
	if command.Limb.Combat.Visible == (Rect{}) {
		command.Limb.Combat.Visible = Rect{X: -500, Y: -20, Width: 100, Height: 100}
	}
	if err := world.StepFeces(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func fecesTicks(t *testing.T, world *World, count int, targets ...FecesTarget) {
	t.Helper()
	for i := 0; i < count; i++ {
		fecesTick(t, world, FecesCommand{Targets: targets})
	}
}

func fecesShot(t *testing.T, world *World, target FecesTarget) FecesImpact {
	t.Helper()
	for world.motion.action != FreeAction && world.state.Tick < world.motion.actionUntil {
		fecesTick(t, world, FecesCommand{Targets: []FecesTarget{target}})
	}
	command := FecesCommand{Targets: []FecesTarget{target}}
	command.Limb.Combat.Player = PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{release(settings.Throw), press(settings.Throw)}}
	fecesTick(t, world, command)
	for i := 0; i < 100; i++ {
		fecesTick(t, world, FecesCommand{Targets: []FecesTarget{target}})
		if len(world.feces.impacts) > 0 {
			return world.feces.impacts[0]
		}
	}
	t.Fatal("projectile never impacted")
	return FecesImpact{}
}

func targetFixture(kind FecesTargetKind) FecesTarget {
	return FecesTarget{Collider: Collider{ID: "target", Kind: ProjectileTarget, Bounds: Rect{X: 50, Width: 2, Height: 80}}, Kind: kind, Ticket: content.DeedID("D-HOS-PRV-01")}
}

func TestFecesCapacityAndRelease(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	pickup := WastePickup{ID: "sealed", Bounds: world.state.Player.Bounds}
	if err := world.AddWastePickup(ctx, pickup); err != nil {
		t.Fatal(err)
	}
	if err := world.AddWastePickup(ctx, pickup); err == nil {
		t.Fatal("duplicate pickup accepted")
	}
	fecesTick(t, world, FecesCommand{})
	if world.state.Feces != 5 || world.feces.pickups[0].Collected {
		t.Fatal("pickup consumed at capacity")
	}
	world.state.Feces = 4
	fecesTick(t, world, FecesCommand{})
	if world.state.Feces != 5 || !world.feces.pickups[0].Collected {
		t.Fatal("sealed pickup failed")
	}
	world.state.Feces = 0
	fecesTick(t, world, FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Throw)}}}}})
	if world.motion.action != FreeAction || world.CombatView(ctx).InvalidAction != "throw_empty" || world.FecesView(ctx).Invalid != "refill_at_marked_station" {
		t.Fatal("empty throw did not give feedback")
	}
	if err := world.SetPlayerLimbs(ctx, LeftLeg|RightLeg); err != nil {
		t.Fatal(err)
	}
	world.state.Feces = 5
	fecesTick(t, world, FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{release(settings.Throw), press(settings.Throw)}}}}})
	if world.motion.action != FreeAction || world.CombatView(ctx).InvalidAction != "throw_requires_arm" {
		t.Fatal("armless throw accepted")
	}
	for _, damageAt := range []int{11, 12, 13} {
		world := combatWorld(t)
		for tick := 1; tick <= damageAt; tick++ {
			command := FecesCommand{}
			if tick == 1 {
				command.Limb.Combat.Player.Edges = []PlayerEdge{press(settings.Throw)}
			}
			if tick == damageAt {
				command.Limb.Combat.Hits = []DamageHit{{Attacker: "hazard", Attack: "one", Kind: OrdinaryDamage, Amount: 15, Direction: 1}}
			}
			fecesTick(t, world, command)
		}
		want := 5
		if damageAt >= 12 {
			want = 4
		}
		if world.state.Feces != want {
			t.Fatalf("damage at %d consumed %d portions", damageAt, 5-world.state.Feces)
		}
	}
}

func TestFecesExhaustionRequiresFreshPress(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	target := targetFixture(FecesMachinery)
	for i := 0; i < 5; i++ {
		impact := fecesShot(t, world, target)
		if impact.Kind != "blocked" || world.state.Feces != 4-i {
			t.Fatalf("shot %d: portions %d, impact %+v", i, world.state.Feces, impact)
		}
	}
	for world.motion.action != FreeAction && world.state.Tick < world.motion.actionUntil {
		fecesTick(t, world, FecesCommand{})
	}
	fecesTick(t, world, FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{release(settings.Throw), press(settings.Throw)}}}}})
	if world.state.Feces != 0 || world.motion.action != FreeAction || world.FecesView(ctx).Invalid != "refill_at_marked_station" {
		t.Fatal("exhausted input started an attack")
	}
}

func TestFecesRefillAndInterruption(t *testing.T) {
	ctx := context.Background()
	for _, interruptedAt := range []int{0, 179, 180} {
		world := combatWorld(t)
		world.state.Feces = 0
		if err := world.AddRefillStation(ctx, RefillStation{ID: "toilet", Position: Vector{X: 28}}); err != nil {
			t.Fatal(err)
		}
		for tick := 1; tick <= 180; tick++ {
			command := FecesCommand{}
			if tick == 1 {
				command.Station = "toilet"
				command.Limb.Combat.Player.Edges = []PlayerEdge{press(settings.Interact)}
			}
			if tick == interruptedAt {
				command.Limb.Combat.Hits = []DamageHit{{Attacker: "hazard", Attack: "one", Kind: OrdinaryDamage, Amount: 15, Direction: 1}}
			}
			fecesTick(t, world, command)
			if tick == 179 && world.state.Feces != 0 {
				t.Fatal("refill completed early")
			}
		}
		want := 5
		if interruptedAt == 179 {
			want = 0
		}
		if world.state.Feces != want {
			t.Fatalf("damage at %d: got %d want %d", interruptedAt, world.state.Feces, want)
		}
	}
	world := combatWorld(t)
	world.state.Feces = 1
	if err := world.AddRefillStation(ctx, RefillStation{ID: "toilet", Position: Vector{X: 28}}); err != nil {
		t.Fatal(err)
	}
	fecesTick(t, world, FecesCommand{Station: "toilet", Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}}}})
	fecesTicks(t, world, 50)
	fecesTick(t, world, FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{release(settings.Interact)}}}}})
	fecesTicks(t, world, 150)
	if world.state.Feces != 1 || world.feces.channel != "" {
		t.Fatal("released interaction completed")
	}
	fecesTick(t, world, FecesCommand{Station: "toilet", Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}}}})
	fecesTicks(t, world, 50)
	world.SuspendPlayer(ctx)
	if err := world.ResumePlayer(ctx, nil); err != nil {
		t.Fatal(err)
	}
	fecesTicks(t, world, 180)
	if world.state.Feces != 1 || world.feces.channel != "" {
		t.Fatal("refill resumed after suspension")
	}
}

func TestFecesRefillDefersToCarriedSocket(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftArm)
	carried := world.LimbView(ctx).Carried
	if carried == "" {
		t.Fatal("fixture did not create a carried limb")
	}
	if err := world.AddRefillStation(ctx, RefillStation{ID: "toilet", Position: Vector{X: 28}}); err != nil {
		t.Fatal(err)
	}
	world.state.Feces = 0
	interact := FecesCommand{Station: "toilet", Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}}}}
	fecesTick(t, world, interact)
	if view := world.LimbView(ctx); view.Channel != "replace" || view.Carried != carried || world.feces.channel != "" {
		t.Fatalf("station overrode replacement: %+v, refill %q", view, world.feces.channel)
	}
	fecesTicks(t, world, 10)
	fecesTick(t, world, FecesCommand{Station: "toilet", Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{release(settings.Interact)}}}}})
	if world.LimbView(ctx).Channel != "" || world.LimbView(ctx).Carried != carried || world.state.Feces != 0 {
		t.Fatal("release did not cancel the replacement")
	}
	fecesTick(t, world, interact)
	fecesTicks(t, world, 47)
	if world.motion.limbs != AllLimbs || world.LimbView(ctx).Carried != "" || world.state.Feces != 0 {
		t.Fatal("carried limb was not replaced without refilling")
	}
	fecesTick(t, world, FecesCommand{Station: "toilet", Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{release(settings.Interact), press(settings.Interact)}}}}})
	fecesTicks(t, world, 179)
	if world.state.Feces != 5 {
		t.Fatal("station unavailable after replacing the limb")
	}
}

func TestFecesTargetsAndBossRecovery(t *testing.T) {
	for _, kind := range []FecesTargetKind{FecesReceptor, FecesMachinery, FecesSource} {
		world := combatWorld(t)
		impact := fecesShot(t, world, targetFixture(kind))
		want := string(kind)
		if kind == FecesMachinery {
			want = "blocked"
		}
		if impact.Kind != want || impact.Ticket != "D-HOS-PRV-01" || len(world.combat.projectiles) != 0 {
			t.Fatalf("target %s: %+v", kind, impact)
		}
		if kind == FecesSource && (impact.StopTicks != 240 || impact.CooldownTicks != 480 || impact.Tick != world.state.Tick) {
			t.Fatalf("source timing: %+v", impact)
		}
	}
	world := combatWorld(t)
	boss := targetFixture(FecesBoss)
	impact := fecesShot(t, world, boss)
	if impact.Kind != "boss_protected" || impact.Damage != 0 || impact.StaggerTicks != 0 {
		t.Fatalf("protected impact: %+v", impact)
	}
	boss.Exposed, boss.Recovery = true, "recovery-1"
	impact = fecesShot(t, world, boss)
	if impact.Kind != "boss_exposed" || impact.Damage != 8 || impact.StaggerTicks != 30 {
		t.Fatalf("exposed impact: %+v", impact)
	}
	impact = fecesShot(t, world, boss)
	if impact.Damage != 8 || impact.StaggerTicks != 0 {
		t.Fatalf("repeated stagger: %+v", impact)
	}
	boss.Recovery = "recovery-2"
	impact = fecesShot(t, world, boss)
	if impact.StaggerTicks != 30 {
		t.Fatalf("new recovery: %+v", impact)
	}
}

func TestFecesEnemyDistractionAndImmunity(t *testing.T) {
	world := combatWorld(t)
	addCombatEnemy(t, world, "enemy", MeleeEnemy, 50)
	world.combat.projectiles = append(world.combat.projectiles, Projectile{ID: "airborne", Attacker: "enemy", Attack: "old", Position: Vector{X: 400, Y: 100}, Velocity: Vector{X: 10}})
	command := FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Throw)}}}}}
	fecesTick(t, world, command)
	var hit FecesImpact
	for i := 0; i < 40; i++ {
		fecesTick(t, world, FecesCommand{})
		if len(world.feces.impacts) != 0 {
			hit = world.feces.impacts[0]
			break
		}
	}
	if hit.Kind != "enemy" || world.combat.enemies[0].Health != 52 {
		t.Fatalf("enemy impact: %+v %+v", hit, world.combat.enemies[0])
	}
	active, immune := world.Distraction("enemy")
	if !active || immune {
		t.Fatal("missing distraction")
	}
	until := world.feces.distracted["enemy"].Until
	if len(world.combat.projectiles) != 1 || world.combat.projectiles[0].ID != "airborne" {
		t.Fatal("distraction canceled a projectile already airborne")
	}
	for world.motion.action != FreeAction && world.state.Tick < world.motion.actionUntil {
		fecesTick(t, world, FecesCommand{})
	}
	world.combat.enemies[0].Body.Bounds = Rect{X: 50, Width: 28, Height: 58}
	world.combat.enemies[0].Body.Velocity = Vector{}
	fecesTick(t, world, FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{release(settings.Throw), press(settings.Throw)}}}}})
	var secondHit bool
	for i := 0; i < 40; i++ {
		fecesTick(t, world, FecesCommand{})
		for _, event := range world.feces.impacts {
			secondHit = secondHit || event.Kind == "enemy"
		}
		if secondHit {
			break
		}
	}
	if !secondHit || world.feces.distracted["enemy"].Until != until {
		t.Fatal("a repeated hit extended distraction")
	}
	for world.state.Tick < until-1 {
		fecesTick(t, world, FecesCommand{})
	}
	if active, _ = world.Distraction("enemy"); !active {
		t.Fatal("distraction ended early")
	}
	fecesTick(t, world, FecesCommand{})
	active, immune = world.Distraction("enemy")
	if active || !immune {
		t.Fatal("immunity did not follow distraction")
	}
	world.combat.enemies[0].Body.Bounds = Rect{X: 50, Width: 28, Height: 58}
	world.combat.enemies[0].Body.Velocity = Vector{}
	fecesTick(t, world, FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{release(settings.Throw), press(settings.Throw)}}}}})
	for i := 0; i < 40; i++ {
		fecesTick(t, world, FecesCommand{})
		if len(world.feces.impacts) != 0 {
			break
		}
	}
	active, immune = world.Distraction("enemy")
	if active || !immune || world.feces.distracted["enemy"].Until != until {
		t.Fatal("hit restarted distraction during immunity")
	}
	for world.state.Tick < until+240 {
		fecesTick(t, world, FecesCommand{})
	}
	_, immune = world.Distraction("enemy")
	if immune {
		t.Fatal("distraction immunity did not expire")
	}
	world.combat.enemies[0].Body.Bounds = Rect{X: 50, Width: 28, Height: 58}
	world.combat.enemies[0].Body.Velocity = Vector{}
	fecesTick(t, world, FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{release(settings.Throw), press(settings.Throw)}}}}})
	for i := 0; i < 40; i++ {
		fecesTick(t, world, FecesCommand{})
		if len(world.feces.impacts) != 0 {
			break
		}
	}
	active, _ = world.Distraction("enemy")
	if !active {
		t.Fatal("new hit after immunity did not distract")
	}
}

func TestFecesPreviewAndSmear(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	preview, err := world.PreviewThrow(ctx, Vector{X: 1}, nil)
	if err != nil || preview.Impact == nil || preview.Kind != "floor" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	fecesTick(t, world, FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Throw)}}}}})
	var impact FecesImpact
	for i := 0; i < 100; i++ {
		fecesTick(t, world, FecesCommand{})
		if len(world.feces.impacts) > 0 {
			impact = world.feces.impacts[0]
			break
		}
	}
	predicted := preview.Points[len(preview.Points)-1]
	if impact.Kind != "floor" || math.Hypot(impact.Position.X-predicted.X, impact.Position.Y-predicted.Y) > 1 || len(world.feces.smears) != 1 {
		t.Fatalf("preview %v actual %+v smears %+v", predicted, impact, world.feces.smears)
	}
	expires := world.feces.smears[0].Expires
	fecesTicks(t, world, int(expires-world.state.Tick)-1)
	if len(world.feces.smears) != 1 {
		t.Fatal("smear expired early")
	}
	fecesTick(t, world, FecesCommand{})
	if len(world.feces.smears) != 0 {
		t.Fatal("smear persisted")
	}
}

func TestFecesPreviewTargetsAndRange(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	target := targetFixture(FecesReceptor)
	preview, err := world.PreviewThrow(ctx, Vector{X: 1}, []FecesTarget{target})
	if err != nil || preview.Impact == nil || preview.Impact.Collider.ID != "target" || preview.Kind != "receptor" {
		t.Fatalf("target preview: %+v %v", preview, err)
	}
	actual := fecesShot(t, world, target)
	point := preview.Points[len(preview.Points)-1]
	if actual.Target != "target" || math.Hypot(actual.Position.X-point.X, actual.Position.Y-point.Y) > 1 {
		t.Fatalf("preview %v actual %+v", point, actual)
	}
	world = combatWorld(t)
	world.state.Player.Bounds.Y = 2000
	world.combat.aim = Vector{X: -1}
	preview, err = world.PreviewThrow(ctx, Vector{}, nil)
	if err != nil || preview.Impact != nil || preview.Points[1].X >= preview.Points[0].X {
		t.Fatalf("neutral direction: %+v %v", preview, err)
	}
	distance := 0.0
	for i := 1; i < len(preview.Points); i++ {
		distance += math.Hypot(preview.Points[i].X-preview.Points[i-1].X, preview.Points[i].Y-preview.Points[i-1].Y)
	}
	if math.Abs(distance-600) > 1e-8 {
		t.Fatalf("preview distance %v", distance)
	}
	if _, err := world.PreviewThrow(ctx, Vector{X: math.NaN()}, nil); err == nil {
		t.Fatal("invalid aim accepted")
	}
}

func TestFecesReportingCampaignIdentity(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	snapshot, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = owner.Accept(ctx, snapshot, "D-HOS-SUP-01")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, snapshot, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	position := Vector{X: centerX(world.state.Player.Bounds), Y: world.state.Player.Bounds.Y}
	if err := world.AddRefillStation(ctx, RefillStation{ID: "reporter", Position: position, Reporting: true}); err != nil {
		t.Fatal(err)
	}
	world.state.Feces = 0
	fecesTick(t, world, FecesCommand{Station: "reporter", Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}}}})
	fecesTicks(t, world, 179)
	candidate := world.FecesView(ctx).PendingWaste
	if candidate == nil || candidate.Attempt.Deed != "D-HOS-SUP-01" || candidate.Attempt.Number != 1 || world.state.Feces != 5 {
		t.Fatalf("report candidate: %+v", candidate)
	}
	transition := campaign.Transition{CampaignID: "01234567-89ab-4cde-8fab-0123456789ab", Sequence: 1}
	proposal, err := world.PrepareWaste(ctx, owner, snapshot, transition)
	if err != nil {
		t.Fatal(err)
	}
	debt, err := proposal.Debt(ctx)
	if err != nil || debt != 999 || snapshot.Credits() != proposal.Credits() {
		t.Fatalf("proposal debt %d: %v", debt, err)
	}
	replayed, err := world.PrepareWaste(ctx, owner, proposal, transition)
	if err != nil || len(replayed.Data(ctx).Incidents) != 1 || replayed.Data(ctx).Incidents[0].ID != proposal.Data(ctx).Incidents[0].ID {
		t.Fatal("waste retry did not retain the committed incident identity")
	}
	if err := world.AddRefillStation(ctx, RefillStation{ID: "other", Position: position, Reporting: true}); err != nil {
		t.Fatal(err)
	}
	world.state.Feces = 0
	fecesTick(t, world, FecesCommand{Station: "other", Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{release(settings.Interact), press(settings.Interact)}}}}})
	fecesTicks(t, world, 179)
	if world.state.Feces != 5 || world.FecesView(ctx).PendingWaste.Contact.Detail != "reporter" {
		t.Fatal("second station blocked refill or replaced the pending contact")
	}
	restored, err := NewWorld(ctx, owner, manifest, proposal, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.feces.reported || restored.state.Feces != 5 || len(restored.feces.smears) != 0 {
		t.Fatal("restoration lost marker or resources")
	}
	if err := restored.AddRefillStation(ctx, RefillStation{ID: "second", Position: position, Reporting: true}); err != nil {
		t.Fatal(err)
	}
	restored.state.Feces = 0
	fecesTick(t, restored, FecesCommand{Station: "second", Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}}}})
	fecesTicks(t, restored, 179)
	if restored.state.Feces != 5 || restored.FecesView(ctx).PendingWaste != nil {
		t.Fatal("restored attempt charged a second station or blocked refill")
	}
	reset, err := owner.Reset(ctx, proposal)
	if err != nil {
		t.Fatal(err)
	}
	resetWorld, err := NewWorld(ctx, owner, manifest, reset, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resetWorld.feces.reported || resetWorld.feces.attempt.Number != 2 {
		t.Fatal("explicit reset did not start a new chargeable attempt")
	}
	if _, err = world.PrepareWaste(ctx, owner, reset, campaign.Transition{CampaignID: transition.CampaignID, Sequence: 2}); err == nil {
		t.Fatal("stale waste candidate accepted against a new attempt")
	}
	if err := resetWorld.AddRefillStation(ctx, RefillStation{ID: "new-attempt", Position: position, Reporting: true}); err != nil {
		t.Fatal(err)
	}
	resetWorld.state.Feces = 0
	fecesTick(t, resetWorld, FecesCommand{Station: "new-attempt", Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}}}})
	fecesTicks(t, resetWorld, 179)
	second, err := resetWorld.PrepareWaste(ctx, owner, reset, campaign.Transition{CampaignID: transition.CampaignID, Sequence: 2})
	if err != nil {
		t.Fatal(err)
	}
	debt, err = second.Debt(ctx)
	if err != nil || debt != 1998 || len(second.Data(ctx).Incidents) != 2 || second.Data(ctx).Incidents[1].Attempt != 2 {
		t.Fatalf("new attempt incident: %+v %v", second.Data(ctx).Incidents, err)
	}
}

func TestFecesWasteCertificationReplay(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	active, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	active, err = owner.Accept(ctx, active, "D-HOS-SUP-01")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, active, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	station := RefillStation{ID: "reporter", Position: Vector{X: centerX(world.state.Player.Bounds)}, Reporting: true}
	if err = world.AddRefillStation(ctx, station); err != nil {
		t.Fatal(err)
	}
	world.state.Feces = 0
	fecesTick(t, world, FecesCommand{Station: station.ID, Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}}}})
	fecesTicks(t, world, 179)
	if world.FecesView(ctx).PendingWaste == nil {
		t.Fatal("refill did not produce a waste candidate")
	}
	objects := make(map[content.ObjectID]content.Object)
	for _, object := range manifest.Objects(ctx) {
		objects[object.ID] = object
	}
	var certification campaign.Certification
	for _, deed := range manifest.Deeds(ctx) {
		if deed.Deed.ID != "D-HOS-SUP-01" {
			continue
		}
		certification = campaign.Certification{Deed: deed.Deed.ID, Attempt: 1, Evidence: deed.Evidence, Trigger: deed.Trigger, HoldMilliseconds: deed.Deed.HoldMilliseconds}
		for _, id := range deed.Deed.Objects {
			object := objects[id]
			if object.Deed == deed.Deed.ID {
				certification.EvidenceObjects = append(certification.EvidenceObjects, id)
			}
			if object.Role == content.RoleTerminal {
				certification.Terminal = id
			}
		}
	}
	if certification.Deed == "" || certification.Terminal == "" {
		t.Fatal("missing certification fixture")
	}
	transition := campaign.Transition{
		CampaignID:    "01234567-89ab-4cde-8fab-0123456789ab",
		Sequence:      1,
		Certification: &campaign.CertificationCandidate{Completed: true, Event: certification},
	}
	proposal, err := world.PrepareWaste(ctx, owner, active, transition)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := proposal.ActiveAttempt(); ok || proposal.Credits() != 1 || len(proposal.Data(ctx).Incidents) != 1 || active.Credits() != 0 {
		t.Fatal("combined proposal did not certify and record one incident")
	}
	restored, err := owner.Restore(ctx, proposal.Data(ctx))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := world.PrepareWaste(ctx, owner, restored, transition)
	if err != nil || !reflect.DeepEqual(replayed.Data(ctx), proposal.Data(ctx)) {
		t.Fatalf("combined retry changed committed result: %v", err)
	}
	changed := transition
	changed.Death = campaign.DeathGameplay
	changed.DeathDetail = "different operation"
	if _, err = world.PrepareWaste(ctx, owner, restored, changed); !errors.Is(err, campaign.ErrConflict) {
		t.Fatalf("changed retry was not rejected by campaign: %v", err)
	}
	changed = transition
	changed.CampaignID = "fedcba98-7654-4321-8fed-cba987654321"
	if _, err = world.PrepareWaste(ctx, owner, restored, changed); err == nil {
		t.Fatal("retry with a different campaign identity succeeded")
	}
	next := campaign.Transition{CampaignID: transition.CampaignID, Sequence: 2}
	if _, err = world.PrepareWaste(ctx, owner, restored, next); err == nil {
		t.Fatal("new operation reused a candidate without its active attempt")
	}
	other, err := owner.Accept(ctx, restored, "D-HOS-SUP-02")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = world.PrepareWaste(ctx, owner, other, next); err == nil {
		t.Fatal("new operation reused a candidate for a different ticket")
	}
}

func TestFecesPhasesAndHeldInput(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	input := FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Throw)}}}}}
	for tick := 1; tick <= 60; tick++ {
		fecesTick(t, world, input)
		attack := world.CombatView(ctx).Attack
		switch tick {
		case 11:
			if attack.Phase != PhaseWindup || world.state.Feces != 5 {
				t.Fatal("throw released during windup")
			}
		case 12:
			if attack.Phase != PhaseActive || world.state.Feces != 4 {
				t.Fatal("throw missed release boundary")
			}
		case 18:
			if attack.Phase != PhaseRecovery {
				t.Fatal("throw missed recovery boundary")
			}
		case 48:
			if attack.ID != 0 {
				t.Fatal("throw recovery ended late")
			}
		}
	}
	if world.state.Feces != 4 {
		t.Fatal("held input produced repeated throws")
	}
}

func TestFecesPreservesCarriedLimb(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftArm)
	carried := world.limbs.carried
	fecesTick(t, world, FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.SelectPayload), press(settings.Throw)}}}}})
	fecesTicks(t, world, 11)
	if world.state.Feces != 4 || world.limbs.carried != carried || world.limbs.payload || len(world.combat.releases) != 1 || world.combat.releases[0].Payload != "feces" {
		t.Fatalf("payload switch lost carry: %+v", world.LimbView(ctx))
	}
}

func TestFecesSmearDoesNotStack(t *testing.T) {
	world := combatWorld(t)
	for shot := 0; shot < 2; shot++ {
		for world.motion.action != FreeAction && world.state.Tick < world.motion.actionUntil {
			fecesTick(t, world, FecesCommand{})
		}
		fecesTick(t, world, FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{release(settings.Throw), press(settings.Throw)}}}}})
		found := false
		for i := 0; i < 100; i++ {
			fecesTick(t, world, FecesCommand{})
			if len(world.feces.impacts) > 0 {
				found = true
				break
			}
		}
		if !found || len(world.feces.smears) != 1 || world.state.Health != 100 {
			t.Fatalf("floor impact stacked: %+v", world.feces.smears)
		}
	}
}

func TestFecesEvadeAtAttackRecoveryBoundary(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	if err := world.AddRefillStation(ctx, RefillStation{ID: "toilet", Position: Vector{X: 28}}); err != nil {
		t.Fatal(err)
	}
	world.state.Feces = 0
	fecesTick(t, world, FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}}}}})
	until := world.motion.actionUntil
	for world.state.Tick < until {
		fecesTick(t, world, FecesCommand{})
	}
	if world.motion.action != AttackAction || world.state.Tick != until {
		t.Fatal("fixture did not stop at the expired attack boundary")
	}
	fecesTick(t, world, FecesCommand{Station: "toilet", Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact), press(settings.Evade)}}}}})
	if world.motion.action != EvadeAction || world.feces.channel != "" || world.state.Feces != 0 {
		t.Fatal("refill beat evade on the first eligible tick")
	}
}

func TestFecesRefillPriorityAndRollback(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	if err := world.AddRefillStation(ctx, RefillStation{ID: "toilet", Position: Vector{X: 28}}); err != nil {
		t.Fatal(err)
	}
	world.state.Feces = 0
	command := FecesCommand{Station: "toilet", Limb: LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact), press(settings.Evade)}}}}}
	fecesTick(t, world, command)
	if world.motion.action != EvadeAction || world.feces.channel != "" {
		t.Fatal("interaction beat a simultaneous evade")
	}
	before := world.state.Tick
	bad := FecesCommand{Targets: []FecesTarget{targetFixture(FecesReceptor), targetFixture(FecesBoss)}}
	bad.Limb.Combat.Visible = Rect{Width: 100, Height: 100}
	if err := world.StepFeces(ctx, bad); err == nil || world.state.Tick != before {
		t.Fatal("duplicate target did not roll back tick")
	}
}

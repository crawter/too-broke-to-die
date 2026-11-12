// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

func objectTick(t *testing.T, world *World, player PlayerCommand, hits ...DamageHit) {
	t.Helper()
	command := ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: player, Hits: hits, Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}}}}}
	if err := world.StepObjects(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func addObject(t *testing.T, world *World, id string, kind ObjectKind, box Rect) {
	t.Helper()
	object := PuzzleObject{ID: id, Kind: kind, Bounds: box, Origin: box, Reachable: Rect{X: -900, Y: -100, Width: 1800, Height: 500}, Required: true}
	if err := world.AddPuzzleObject(context.Background(), object); err != nil {
		t.Fatal(err)
	}
}

func addTarget(t *testing.T, world *World, target InteractionTarget) {
	t.Helper()
	if err := world.AddInteractionTarget(context.Background(), target); err != nil {
		t.Fatal(err)
	}
}

func TestInteractionSelectionBoundariesAndPriority(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	addTarget(t, world, InteractionTarget{ID: "right", Kind: InteractionService, Bounds: Rect{X: 76, Width: 4, Height: 4}})
	if view := world.InteractionView(ctx); !view.Found || view.Target.ID != "right" || view.Target.HoldTicks != 36 || view.Target.Input != settings.Interact {
		t.Fatalf("48-pixel prompt: %+v", view)
	}
	world.objects.interactions[0].Bounds.X += 0.01
	if world.InteractionView(ctx).Found {
		t.Fatal("target beyond 48 selected")
	}
	world.objects.interactions[0].Bounds.X = 48
	addTarget(t, world, InteractionTarget{ID: "left", Kind: InteractionService, Bounds: Rect{X: -24, Width: 4, Height: 4}})
	if got := world.InteractionView(ctx).Target.ID; got != "right" {
		t.Fatalf("facing tie: %s", got)
	}
	world.motion.facing = -1
	if got := world.InteractionView(ctx).Target.ID; got != "left" {
		t.Fatalf("left facing tie: %s", got)
	}
	addObject(t, world, "near-crate", ObjectCrate, Rect{X: 31, Width: 10, Height: 10})
	if got := world.InteractionView(ctx).Target.ID; got != "near-crate" {
		t.Fatalf("nearest loose object was masked by a farther service: %s", got)
	}
	addTarget(t, world, InteractionTarget{ID: "npc", Kind: InteractionNPC, Bounds: Rect{X: 76, Width: 4, Height: 4}})
	if got := world.InteractionView(ctx).Target.ID; got != "npc" {
		t.Fatalf("NPC priority: %s", got)
	}
	world.initLimbs()
	world.motion.limbs &^= LeftArm
	world.limbs.carried = "limb"
	world.limbs.objects = []LimbObject{{ID: "limb", Socket: SocketLeftArm, Generation: world.limbs.generation, Carried: true}}
	if got := world.InteractionView(ctx).Target.ID; got != "limb" {
		t.Fatalf("carried socket priority: %s", got)
	}
}

func TestInteractionChannelCompletionAndInterruption(t *testing.T) {
	ctx := context.Background()
	for _, damageAt := range []int{0, 35, 36} {
		world := combatWorld(t)
		addTarget(t, world, InteractionTarget{ID: "terminal", Kind: InteractionService, Bounds: Rect{X: 30, Width: 4, Height: 4}, Prerequisite: "ticket"})
		for tick := 1; tick <= 36; tick++ {
			command := PlayerCommand{}
			if tick == 1 {
				command.Edges = []PlayerEdge{press(settings.Interact)}
			}
			var hits []DamageHit
			if tick == damageAt {
				hits = []DamageHit{{Attacker: "hazard", Attack: "one", Kind: OrdinaryDamage, Amount: 15, Direction: 1}}
			}
			objectTick(t, world, command, hits...)
			if tick == 35 && damageAt == 0 {
				view := world.InteractionView(ctx)
				if view.Progress != 35 || view.Duration != 36 || view.Target.Prerequisite != "ticket" {
					t.Fatalf("progress: %+v", view)
				}
			}
		}
		completed := false
		for _, event := range world.InteractionView(ctx).Events {
			completed = completed || event.Kind == "completed"
		}
		if completed != (damageAt != 35) {
			t.Fatalf("damage at %d, completed %v", damageAt, completed)
		}
	}
	world := combatWorld(t)
	addTarget(t, world, InteractionTarget{ID: "terminal", Kind: InteractionService, Bounds: Rect{X: 30, Width: 4, Height: 4}})
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}})
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{release(settings.Interact)}})
	for i := 0; i < 36; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	if view := world.InteractionView(ctx); view.Duration != 0 || len(view.Events) != 0 {
		t.Fatalf("released channel: %+v", view)
	}
}

func TestInteractionDoesNotResumeAfterSuspension(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	addTarget(t, world, InteractionTarget{ID: "terminal", Kind: InteractionService, Bounds: Rect{X: 30, Width: 4, Height: 4}})
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}})
	for i := 0; i < 10; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	world.SuspendPlayer(ctx)
	if err := world.ResumePlayer(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		objectTick(t, world, PlayerCommand{})
		for _, event := range world.InteractionView(ctx).Events {
			if event.Kind == "completed" {
				t.Fatal("suspended interaction completed after resume")
			}
		}
	}
}

func TestRefillReleaseAfterPromptChanges(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	world.state.Feces = 0
	if err := world.AddRefillStation(ctx, RefillStation{ID: "station", Position: Vector{X: 28}}); err != nil {
		t.Fatal(err)
	}
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}})
	if world.feces.channel == "" {
		t.Fatal("station did not start")
	}
	if err := world.AddProtectedCivilian(ctx, Civilian{ID: "adult", Bounds: Rect{X: 30, Width: 28, Height: 58}}); err != nil {
		t.Fatal(err)
	}
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{release(settings.Interact)}})
	if world.feces.channel != "" || world.state.Feces != 0 {
		t.Fatal("release was lost when NPC took prompt priority")
	}
}

func TestMovingChannelAndActionPriority(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	addTarget(t, world, InteractionTarget{ID: "moving", Kind: InteractionService, Bounds: Rect{X: 0, Width: 200, Height: 10}, Moving: true, HoldTicks: 36})
	objectTick(t, world, PlayerCommand{Move: 1, Edges: []PlayerEdge{press(settings.Interact), press(settings.Melee)}})
	for i := 1; i < 36; i++ {
		objectTick(t, world, PlayerCommand{Move: 1})
	}
	if world.state.Player.Bounds.X <= 0 || world.combat.nextAttack != 0 {
		t.Fatal("moving channel failed to move or allowed an attack")
	}
	view := world.InteractionView(ctx)
	if len(view.Events) != 1 || view.Events[0].Kind != "completed" {
		t.Fatalf("moving hold did not complete: %+v", view)
	}
	world = combatWorld(t)
	addTarget(t, world, InteractionTarget{ID: "stationary", Kind: InteractionService, Bounds: Rect{X: 0, Width: 200, Height: 10}})
	objectTick(t, world, PlayerCommand{Move: 1, Edges: []PlayerEdge{press(settings.Interact)}})
	if world.InteractionView(ctx).Invalid != "requires_stationary_ground" {
		t.Fatal("stationary channel accepted movement")
	}
}

func TestInteractionInvalidThreatAndEvade(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	addTarget(t, world, InteractionTarget{ID: "panel", Kind: InteractionService, Bounds: Rect{X: 30, Width: 4, Height: 4}, Reason: "requires_ticket"})
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}})
	if view := world.InteractionView(ctx); view.Invalid != "requires_ticket" || world.state.Feces != 5 || world.motion.action != FreeAction {
		t.Fatalf("invalid action: %+v", view)
	}
	world = combatWorld(t)
	addTarget(t, world, InteractionTarget{ID: "panel", Kind: InteractionService, Bounds: Rect{X: 30, Width: 4, Height: 4}})
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Interact), press(settings.Evade)}})
	if world.motion.action != EvadeAction || world.objects.channel.target.ID != "" {
		t.Fatal("evade did not take priority")
	}
	world = combatWorld(t)
	if err := world.AddProtectedCivilian(ctx, Civilian{ID: "adult", Bounds: Rect{X: 30, Width: 28, Height: 58}}); err != nil {
		t.Fatal(err)
	}
	addCombatEnemy(t, world, "enemy", MeleeEnemy, 48)
	world.combat.enemies[0].Phase = EnemyTelegraph
	world.combat.enemies[0].Until = 100
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}})
	if world.InteractionView(ctx).Invalid != "area_threatened" {
		t.Fatal("conversation started during a nearby attack")
	}
}

func TestPushAndObjectCollision(t *testing.T) {
	for _, kind := range []ObjectKind{ObjectCrate, ObjectTrolley} {
		world := combatWorld(t)
		addObject(t, world, "movable", kind, Rect{X: 28, Width: 30, Height: 30})
		view := world.InteractionView(context.Background())
		if !view.Found || view.Target.ID != "movable" || view.Target.Input != settings.MoveRight {
			t.Fatalf("missing pushing prompt: %+v", view)
		}
		start := world.Objects(context.Background())[0].Bounds.X
		for i := 0; i < 60; i++ {
			objectTick(t, world, PlayerCommand{Move: 1})
			if world.Objects(context.Background())[0].Bounds.X-start > float64(i+1)*100*TickSeconds+Tolerance {
				t.Fatal("object exceeded push speed")
			}
		}
		box := world.Objects(context.Background())[0].Bounds
		if box.X <= start || overlaps(box, world.state.Player.Bounds) {
			t.Fatalf("%s did not push safely: %+v player %+v", kind, box, world.state.Player.Bounds)
		}
		world.ResetObjects(context.Background())
		if world.Objects(context.Background())[0].Bounds.X != start {
			t.Fatal("reset did not restore origin")
		}
	}
}

func TestAirborneObjectsCannotBePushed(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	addObject(t, world, "crate", ObjectCrate, Rect{X: 28, Y: 30, Width: 20, Height: 20})
	objectTick(t, world, PlayerCommand{Move: 1})
	if world.Objects(ctx)[0].Bounds.X != 28 {
		t.Fatal("unsupported crate was pushed")
	}
	world = combatWorld(t)
	addObject(t, world, "trolley", ObjectTrolley, Rect{X: 28, Width: 20, Height: 40})
	world.state.Player.Bounds.Y = 10
	world.state.Player.Support = ""
	objectTick(t, world, PlayerCommand{Move: 1})
	if world.Objects(ctx)[0].Bounds.X != 28 {
		t.Fatal("airborne player pushed trolley")
	}
}

func TestPlateWeightsLatchesAndRegeneration(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	addObject(t, world, "plate", ObjectPlate, Rect{X: 0, Width: 40, Height: 4})
	objectTick(t, world, PlayerCommand{})
	if !world.Objects(ctx)[0].Active {
		t.Fatal("player did not activate plate")
	}
	world.state.Player.Bounds.X = 100
	objectTick(t, world, PlayerCommand{})
	if world.Objects(ctx)[0].Active {
		t.Fatal("player departure did not release plate")
	}
	addObject(t, world, "crate", ObjectCrate, Rect{X: 4, Width: 20, Height: 20})
	objectTick(t, world, PlayerCommand{})
	if !world.Objects(ctx)[1].Active {
		t.Fatal("crate did not activate plate")
	}
	world.objects.items[0].Bounds.X = 200
	world.initLimbs()
	world.motion.limbs &^= LeftArm
	world.limbs.objects = []LimbObject{{ID: "old", Socket: SocketLeftArm, Generation: world.limbs.generation, Position: Vector{X: 10, Y: 1}}}
	objectTick(t, world, PlayerCommand{})
	if !world.Objects(ctx)[1].Active {
		t.Fatal("settled limb did not activate plate")
	}
	if err := world.LatchPlate(ctx, "plate"); err != nil {
		t.Fatal(err)
	}
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Regenerate)}})
	for i := 1; i < 180; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	if len(world.LimbView(ctx).Objects) != 0 || !world.Objects(ctx)[1].Active {
		t.Fatal("regeneration lost latched route or retained stale limb")
	}
	world.ResetObjects(ctx)
	if world.Objects(ctx)[1].Latched {
		t.Fatal("reset retained temporary latch")
	}
}

func TestLostObjectAndBreakable(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	addObject(t, world, "crate", ObjectCrate, Rect{X: 80, Width: 20, Height: 20})
	world.objects.items[0].Bounds.X = 1000
	for i := 0; i < 239; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	if world.Objects(ctx)[0].Bounds.X != 1000 {
		t.Fatal("object returned early")
	}
	objectTick(t, world, PlayerCommand{})
	if world.Objects(ctx)[0].Bounds.X != 80 {
		t.Fatal("object not returned within two seconds")
	}
	world = combatWorld(t)
	addObject(t, world, "wall", ObjectBreakable, Rect{X: 34, Width: 10, Height: 40})
	addObject(t, world, "debris", ObjectDecoration, Rect{X: 0, Width: 20, Height: 20})
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}})
	for i := 1; i < 55; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	if world.Objects(ctx)[1].Damage != 1 || world.Objects(ctx)[1].Broken {
		t.Fatalf("missing visible damage stage: %+v", world.Objects(ctx))
	}
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{release(settings.Melee), press(settings.Melee)}})
	for i := 1; i < 55; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	if !world.Objects(ctx)[1].Broken {
		t.Fatal("second attack did not break obstacle")
	}
	world.ResetObjects(ctx)
	if world.Objects(ctx)[1].Broken {
		t.Fatal("temporary break survived reset")
	}
	world.objects.items[1].CertifiedOpen = true
	world.ResetObjects(ctx)
	if !world.Objects(ctx)[1].Broken {
		t.Fatal("certified breakage was reset")
	}
}

func TestPaddleLatchSurvivesRegeneration(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	addObject(t, world, "paddle", ObjectPaddle, Rect{X: 40, Y: 10, Width: 2, Height: 40})
	world.initLimbs()
	world.motion.limbs &^= LeftArm
	world.limbs.objects = []LimbObject{{ID: "throw", Socket: SocketLeftArm, Generation: world.limbs.generation, Airborne: true, Damaging: true, Position: Vector{X: 38, Y: 20}, Velocity: Vector{X: 600}, FlightTick: world.state.Tick}}
	objectTick(t, world, PlayerCommand{})
	if !world.Objects(ctx)[0].Latched || len(world.LimbView(ctx).Objects) != 1 || world.LimbView(ctx).Objects[0].Airborne {
		t.Fatalf("paddle impact: %+v %+v", world.Objects(ctx), world.LimbView(ctx))
	}
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Regenerate)}})
	for i := 1; i < 180; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	if len(world.LimbView(ctx).Objects) != 0 || !world.Objects(ctx)[0].Latched {
		t.Fatal("regeneration erased UTL latch")
	}
	world.ResetObjects(ctx)
	if world.Objects(ctx)[0].Latched {
		t.Fatal("paddle latch survived attempt reset")
	}
}

func TestAuthoredUtilityPaddleAndRegeneration(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	saved, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range manifest.Batches(ctx)[:2] {
		for _, id := range batch.Deeds {
			saved = certifyFixture(t, owner, manifest, saved, id)
		}
	}
	saved, err = owner.Accept(ctx, saved, "D-HOS-UTL-01")
	if err != nil {
		t.Fatal(err)
	}
	saved, err = owner.Checkpoint(ctx, saved, "ANC-HOS-03")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	world.state.Player.Bounds.X, world.state.Player.Bounds.Y = 610, 80
	world.state.Player.Support = "terrain-STEP-2"
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.RemoveLimb)}})
	for i := 1; i < 42; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	objectTick(t, world, PlayerCommand{Aim: Vector{X: 1, Y: 0.5}, Edges: []PlayerEdge{press(settings.Throw)}})
	latched := false
	for i := 0; i < 100; i++ {
		objectTick(t, world, PlayerCommand{})
		for _, object := range world.Objects(ctx) {
			if object.Kind == ObjectPaddle && object.Latched {
				latched = true
			}
		}
		if latched {
			break
		}
	}
	if !latched {
		t.Fatalf("authored paddle did not latch: %+v %+v", world.Objects(ctx), world.LimbView(ctx))
	}
	limb := world.LimbView(ctx).Objects[0]
	if limb.Airborne || limb.Position.Y > 81 {
		t.Fatalf("paddle did not release limb onto reachable support: %+v", limb)
	}
	// Wait for the throw recovery before starting regeneration.
	for world.state.Tick < world.motion.actionUntil {
		objectTick(t, world, PlayerCommand{})
	}
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Regenerate)}})
	for i := 1; i < 180; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	if len(world.LimbView(ctx).Objects) != 0 {
		t.Fatal("regeneration retained the detached limb")
	}
	for _, object := range world.Objects(ctx) {
		if object.Kind == ObjectPaddle && !object.Latched {
			t.Fatal("regeneration erased authored paddle evidence")
		}
	}
}

func TestAuthoredRescueObjectsAndReset(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	saved, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range manifest.Batches(ctx)[0].Deeds {
		saved = certifyFixture(t, owner, manifest, saved, id)
	}
	saved, err = owner.Accept(ctx, saved, "D-HOS-RES-01")
	if err != nil {
		t.Fatal(err)
	}
	saved, err = owner.Checkpoint(ctx, saved, "ANC-HOS-02")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	opening := PuzzleObject{ID: "certified-opening", Kind: ObjectBreakable, Bounds: Rect{X: 600, Width: 20, Height: 40}, Origin: Rect{X: 600, Width: 20, Height: 40}, Reachable: Rect{X: 0, Y: -8, Width: 1536, Height: 568}, CertifiedOpen: true, Ticket: "D-HOS-SUP-01"}
	if err = world.AddPuzzleObject(ctx, opening); err != nil {
		t.Fatal(err)
	}
	opening.ID, opening.Ticket = "uncommitted-opening", "D-HOS-RES-01"
	if err = world.AddPuzzleObject(ctx, opening); err == nil {
		t.Fatal("uncertified ticket opened a required route")
	}
	var crate, plate, gate string
	for _, object := range world.Objects(ctx) {
		switch object.Kind {
		case ObjectCrate:
			crate = object.ID
		case ObjectPlate:
			plate = object.ID
		case ObjectGate:
			gate = object.ID
		}
	}
	if crate == "" || plate == "" || gate == "" {
		t.Fatalf("missing authored RES objects: %+v", world.Objects(ctx))
	}
	// Start directly behind the real authored crate, preserving the same world
	// and its real collision and campaign restoration plan.
	for i := range world.objects.items {
		if world.objects.items[i].ID == crate {
			world.state.Player.Bounds.X = world.objects.items[i].Bounds.X - PlayerWidth
		}
	}
	world.state.Player.Support = "boundary-floor"
	objectTick(t, world, PlayerCommand{Move: 1})
	for _, object := range world.Objects(ctx) {
		if object.ID == crate && object.Bounds.X == object.Origin.X {
			t.Fatal("authored crate did not move")
		}
	}
	_, state := world.Snapshot(ctx).Data(ctx)
	for _, entity := range state.Entities {
		if string(entity.ID) == crate && entity.Role == content.RoleCrate {
			for _, object := range world.Objects(ctx) {
				if object.ID == crate && entity.Position.X != centerX(object.Bounds) {
					t.Fatal("snapshot and object view disagree")
				}
			}
		}
	}
	var plateBox Rect
	for _, object := range world.objects.items {
		if object.ID == plate {
			plateBox = object.Bounds
		}
	}
	for i := range world.objects.items {
		if world.objects.items[i].ID == crate {
			world.objects.items[i].Bounds.X = plateBox.X
		}
	}
	world.state.Player.Bounds.X = 643
	world.state.Player.Velocity = Vector{}
	objectTick(t, world, PlayerCommand{})
	for i := 0; i < 25; i++ {
		objectTick(t, world, PlayerCommand{Move: 1})
	}
	if world.state.Player.Bounds.X <= 672 {
		t.Fatal("weighted plate did not open the actual rescue gate")
	}
	world.state.Player.Bounds.X = 700
	world.state.Player.Velocity = Vector{}
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}})
	for i := 1; i < 36; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	for _, object := range world.Objects(ctx) {
		if object.ID == gate && !object.Latched {
			t.Fatal("gate latch interaction did not complete")
		}
	}
	for i := range world.objects.items {
		if world.objects.items[i].ID == crate {
			world.objects.items[i].Bounds.X = 320
		}
	}
	if err = world.SetPlayerLimbs(ctx, AllLimbs&^LeftArm); err != nil {
		t.Fatal(err)
	}
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Regenerate)}})
	for i := 1; i < 180; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	for _, object := range world.Objects(ctx) {
		if object.ID == gate && !object.Active {
			t.Fatal("regeneration closed the latched rescue gate")
		}
		if object.ID == plate && (object.Active || object.Latched) {
			t.Fatal("gate latch incorrectly kept an unweighted plate active")
		}
	}
	restored, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range restored.Objects(ctx) {
		if object.Bounds != object.Origin || object.Latched {
			t.Fatal("checkpoint retained temporary puzzle work")
		}
	}
}

func TestSlipAndPropBounds(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	world.lethal = []Rect{{X: 80, Y: -20, Width: 30, Height: 100}}
	world.feces.smears = []Smear{{ID: "smear", Position: Vector{X: 20}, Expires: 480}}
	objectTick(t, world, PlayerCommand{Move: 1})
	if len(world.Reactions(ctx)) != 1 || world.Reactions(ctx)[0].Kind != "slip" {
		t.Fatal("moving player did not slip")
	}
	for i := 0; i < 42; i++ {
		objectTick(t, world, PlayerCommand{Move: 1})
		if world.state.Player.Bounds.X+world.state.Player.Bounds.Width > 48+1e-7 {
			t.Fatal("forced slide crossed lethal-edge margin")
		}
	}
	if len(world.Reactions(ctx)) != 0 || world.motion.dead {
		t.Fatal("slip did not recover safely")
	}
	world = combatWorld(t)
	addObject(t, world, "prop", ObjectProp, Rect{X: 25, Width: 20, Height: 30})
	objectTick(t, world, PlayerCommand{Move: 1})
	if len(world.Reactions(ctx)) != 1 || world.Reactions(ctx)[0].Kind != "prop_launch" {
		t.Fatal("marked prop did not launch")
	}
	objectTick(t, world, PlayerCommand{}, DamageHit{Attacker: "enemy", Attack: "hit", Kind: OrdinaryDamage, Amount: 18, Direction: 1})
	if world.state.Health != 82 || world.motion.action != HurtAction || len(world.Reactions(ctx)) != 0 {
		t.Fatal("reaction immunity incorrectly blocked combat damage")
	}
	if math.Abs(world.state.Player.Velocity.X) != 260 {
		t.Fatal("damage did not apply combat knockback")
	}
}

func TestReactionCooldownRecoveryAndPitLip(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	// The pit ends exactly at floor level; its safety margin still applies.
	world.lethal = []Rect{{X: 80, Y: -100, Width: 40, Height: 100}}
	world.feces.smears = []Smear{{ID: "smear", Position: Vector{X: 25}, Expires: 1000}}
	objectTick(t, world, PlayerCommand{Move: 1})
	for i := 0; i < 42; i++ {
		objectTick(t, world, PlayerCommand{Move: 1})
		if world.state.Player.Bounds.X+PlayerWidth > 48+Tolerance {
			t.Fatal("slide crossed a floor-level pit margin")
		}
	}
	if len(world.Reactions(ctx)) != 0 {
		t.Fatal("slip lasted more than 350 ms")
	}
	state := &world.objects.reactions
	start := state.cooldown["player\x00smear"]
	if start != 241 {
		t.Fatalf("wrong smear cooldown: %d", start)
	}
	world.beginReaction("player", "smear", "slip", 1, 42)
	if len(world.Reactions(ctx)) != 0 {
		t.Fatal("same smear retriggered during cooldown")
	}
	world.state.Tick = start
	world.beginReaction("player", "smear", "slip", 1, 42)
	if len(world.Reactions(ctx)) != 1 {
		t.Fatal("same smear remained immune after two seconds")
	}
	world = combatWorld(t)
	addObject(t, world, "prop", ObjectProp, Rect{X: 25, Width: 40, Height: 30})
	objectTick(t, world, PlayerCommand{Move: 1})
	for i := 0; i < 84; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	if len(world.Reactions(ctx)) != 0 {
		t.Fatal("prop reaction exceeded 700 ms")
	}
	world.beginReaction("player", "another-prop", "prop_launch", 1, 84)
	if len(world.Reactions(ctx)) != 0 {
		t.Fatal("forced-reaction immunity did not block chaining")
	}
}

func TestEnemySlipsAndMarkedPropReactions(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"slip", "prop_launch"} {
		world := combatWorld(t)
		addCombatEnemy(t, world, "enemy", MeleeEnemy, 140)
		if kind == "slip" {
			world.feces.smears = []Smear{{ID: "smear", Position: Vector{X: 145}, Expires: 500}}
		} else {
			addObject(t, world, "prop", ObjectProp, Rect{X: 130, Width: 40, Height: 40})
		}
		objectTick(t, world, PlayerCommand{})
		reactions := world.Reactions(ctx)
		if len(reactions) != 1 || reactions[0].Actor != "enemy" || reactions[0].Kind != kind {
			t.Fatalf("enemy %s: %+v", kind, reactions)
		}
		for i := 0; i < 84; i++ {
			objectTick(t, world, PlayerCommand{})
		}
		if len(world.Reactions(ctx)) != 0 || world.combat.enemies[0].Health != 60 {
			t.Fatalf("%s did not recover harmlessly", kind)
		}
	}
}

func TestObjectAndInteractionValidation(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	base := InteractionTarget{ID: "target", Kind: InteractionService, Bounds: Rect{X: 30, Width: 4, Height: 4}}
	invalid := base
	invalid.HoldTicks = 72001
	if err := world.AddInteractionTarget(ctx, invalid); err == nil {
		t.Fatal("unbounded channel accepted")
	}
	invalid = base
	invalid.Input = settings.Throw
	if err := world.AddInteractionTarget(ctx, invalid); err == nil {
		t.Fatal("unsupported channel input accepted")
	}
	base.HoldTicks = 72000
	if err := world.AddInteractionTarget(ctx, base); err != nil {
		t.Fatal(err)
	}
	if err := world.AddInteractionTarget(ctx, base); err == nil {
		t.Fatal("duplicate target accepted")
	}
	object := PuzzleObject{ID: "bad", Kind: ObjectCrate, Bounds: Rect{X: 30, Width: 10, Height: 10}, Origin: Rect{X: 30, Width: 10, Height: 10}, Reachable: Rect{Width: 20, Height: 20}, Required: true}
	if err := world.AddPuzzleObject(ctx, object); err == nil {
		t.Fatal("unreachable required origin accepted")
	}
	object.Reachable.Width = 100
	object.Damage = 1
	if err := world.AddPuzzleObject(ctx, object); err == nil {
		t.Fatal("fabricated damage stage accepted")
	}
}

func TestRequiredObjectReturnsAndResetOverActors(t *testing.T) {
	ctx := context.Background()
	for _, reset := range []bool{false, true} {
		for _, actor := range []string{"player", "enemy", "civilian"} {
			t.Run(fmt.Sprintf("reset=%v/%s", reset, actor), func(t *testing.T) {
				world := combatWorld(t)
				addObject(t, world, "crate", ObjectCrate, Rect{X: 80, Width: 20, Height: 30})
				world.objects.items[0].Bounds.X = 1000
				switch actor {
				case "player":
					world.state.Player.Bounds.X = 80
				case "enemy":
					addCombatEnemy(t, world, "guard", MeleeEnemy, 80)
					world.state.Entities = append(world.state.Entities, Entity{ID: "guard", Kind: "MELEE", Position: Vector{X: 94}})
					world.combat.enemies[0].Phase = EnemyHurt
					world.combat.enemies[0].Until = 1000
					world.combat.enemies[0].HurtUntil = 1000
				case "civilian":
					if err := world.AddProtectedCivilian(ctx, Civilian{ID: "adult", Bounds: Rect{X: 80, Width: 28, Height: 58}}); err != nil {
						t.Fatal(err)
					}
					world.state.Entities = append(world.state.Entities, Entity{ID: "adult", Role: content.RoleActor, Position: Vector{X: 94}})
				}
				if reset {
					world.ResetObjects(ctx)
				} else {
					for i := 0; i < 240; i++ {
						objectTick(t, world, PlayerCommand{})
					}
				}
				object := world.Objects(ctx)[0]
				if object.Bounds != object.Origin || object.Restoring {
					t.Fatalf("object did not return safely on time: %+v", object)
				}
				var body Rect
				switch actor {
				case "player":
					body = world.state.Player.Bounds
				case "enemy":
					body = world.combat.enemies[0].Body.Bounds
				case "civilian":
					body = world.combat.civilians[0].Bounds
				}
				if overlaps(body, object.Bounds) {
					t.Fatalf("restored solid overlaps %s: %+v %+v", actor, body, object.Bounds)
				}
				if actor != "player" {
					_, snapshot := world.Snapshot(ctx).Data(ctx)
					if len(snapshot.Entities) != 1 || snapshot.Entities[0].Position != (Vector{X: centerX(body), Y: body.Y}) {
						t.Fatal("recovered actor position is missing from the snapshot")
					}
				}
				for i := 0; i < 3; i++ {
					objectTick(t, world, PlayerCommand{})
				}
			})
		}
	}
}

func TestRestorationDisplacementAvoidsWallsAndLethalVolumes(t *testing.T) {
	ctx := context.Background()
	for _, obstacle := range []string{"wall", "lethal"} {
		t.Run(obstacle, func(t *testing.T) {
			var world *World
			if obstacle == "wall" {
				world = combatWorld(t, Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 115, Width: 5, Height: 100}})
			} else {
				world = combatWorld(t)
				world.lethal = []Rect{{X: 120, Width: 30, Height: 100}}
			}
			addObject(t, world, "crate", ObjectCrate, Rect{X: 80, Width: 20, Height: 30})
			world.objects.items[0].Bounds.X = 200
			world.state.Player.Bounds.X = 80
			world.ResetObjects(ctx)
			// Right is the shorter displacement but crosses the obstruction.
			if world.state.Player.Bounds.X != 52 || world.motion.dead || world.Objects(ctx)[0].Restoring {
				t.Fatalf("unsafe restoration displacement: %+v", world.state.Player)
			}
			objectTick(t, world, PlayerCommand{})
		})
	}
}

func TestOccupiedOriginWaitsWithoutBlocking(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t,
		Collider{ID: "left-wall", Kind: Solid, Bounds: Rect{X: -4, Width: 4, Height: 100}},
		Collider{ID: "right-wall", Kind: Solid, Bounds: Rect{X: 28, Width: 4, Height: 100}},
		Collider{ID: "ceiling", Kind: Solid, Bounds: Rect{X: 0, Y: 58, Width: 28, Height: 4}},
	)
	addObject(t, world, "crate", ObjectCrate, Rect{X: 0, Width: 28, Height: 58})
	world.objects.items[0].Bounds.X = 1000
	for i := 0; i < 240; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	object := world.Objects(ctx)[0]
	if object.Bounds != object.Origin || !object.Restoring {
		t.Fatalf("occupied origin did not return nonblocking: %+v", object)
	}
	objectTick(t, world, PlayerCommand{})
	// When the obstruction is removed, the same returned object becomes solid.
	grid, err := NewGrid(ctx, []Collider{floorCollider()})
	if err != nil {
		t.Fatal(err)
	}
	world.grid = grid
	objectTick(t, world, PlayerCommand{})
	if object = world.Objects(ctx)[0]; object.Restoring || overlaps(object.Bounds, world.state.Player.Bounds) {
		t.Fatalf("restoration did not resolve after clearance: %+v", object)
	}
	objectTick(t, world, PlayerCommand{})
}

func TestRestoredObjectWaitsForAnotherSolidObject(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	origin := Rect{X: 80, Width: 20, Height: 30}
	addObject(t, world, "first", ObjectCrate, origin)
	addObject(t, world, "returning", ObjectCrate, origin)
	world.objects.items[1].Bounds.X = 1000
	for i := 0; i < 240; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	objects := world.Objects(ctx)
	if objects[1].Bounds != origin || !objects[1].Restoring {
		t.Fatalf("returning object failed to wait at its occupied origin: %+v", objects)
	}
	objectTick(t, world, PlayerCommand{})
	world.objects.items[0].Bounds.X = 140
	objectTick(t, world, PlayerCommand{})
	objects = world.Objects(ctx)
	if objects[1].Restoring || overlaps(objects[0].Bounds, objects[1].Bounds) {
		t.Fatalf("returned object did not become solid after clearance: %+v", objects)
	}
	objectTick(t, world, PlayerCommand{})
}

func TestGateWaitsForOccupiedVolume(t *testing.T) {
	ctx := context.Background()
	for _, occupant := range []string{"player", "enemy", "civilian", "crate"} {
		t.Run(occupant, func(t *testing.T) {
			world := combatWorld(t)
			addObject(t, world, "plate", ObjectPlate, Rect{X: 200, Width: 40, Height: 4})
			gate := PuzzleObject{ID: "gate", Kind: ObjectGate, Bounds: Rect{X: 60, Width: 16, Height: 80}, Origin: Rect{X: 60, Width: 16, Height: 80}, Reachable: Rect{X: -900, Y: -100, Width: 1800, Height: 500}, Plate: "plate"}
			if err := world.AddPuzzleObject(ctx, gate); err != nil {
				t.Fatal(err)
			}
			world.objects.items[0].Active = true
			world.syncObjectPlatforms()
			switch occupant {
			case "player":
				world.state.Player.Bounds.X = 60
			case "enemy":
				addCombatEnemy(t, world, "guard", MeleeEnemy, 60)
				world.combat.enemies[0].Phase = EnemyHurt
				world.combat.enemies[0].Until = 1000
			case "civilian":
				if err := world.AddProtectedCivilian(ctx, Civilian{ID: "adult", Bounds: Rect{X: 60, Width: 28, Height: 58}}); err != nil {
					t.Fatal(err)
				}
			case "crate":
				addObject(t, world, "weight", ObjectCrate, Rect{X: 60, Width: 20, Height: 30})
			}
			objectTick(t, world, PlayerCommand{})
			for _, object := range world.Objects(ctx) {
				if object.ID == "gate" && !object.Active {
					t.Fatal("gate closed over an occupant")
				}
			}
			objectTick(t, world, PlayerCommand{})
			switch occupant {
			case "player":
				world.state.Player.Bounds.X = 100
			case "enemy":
				world.combat.enemies[0].Body.Bounds.X = 100
			case "civilian":
				world.combat.civilians[0].Bounds.X = 100
			case "crate":
				for i := range world.objects.items {
					if world.objects.items[i].ID == "weight" {
						world.objects.items[i].Bounds.X = 100
					}
				}
			}
			objectTick(t, world, PlayerCommand{})
			for _, object := range world.Objects(ctx) {
				if object.ID == "gate" && object.Active {
					t.Fatal("empty gate failed to close")
				}
			}
			objectTick(t, world, PlayerCommand{})
		})
	}
}

func TestResetDoesNotCloseGateOnPlayer(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	addObject(t, world, "plate", ObjectPlate, Rect{X: 200, Width: 40, Height: 4})
	gate := PuzzleObject{ID: "gate", Kind: ObjectGate, Bounds: Rect{X: 60, Width: 16, Height: 80}, Origin: Rect{X: 60, Width: 16, Height: 80}, Reachable: Rect{X: -900, Y: -100, Width: 1800, Height: 500}, Plate: "plate"}
	if err := world.AddPuzzleObject(ctx, gate); err != nil {
		t.Fatal(err)
	}
	world.objects.items[0].Active = true
	world.syncObjectPlatforms()
	world.state.Player.Bounds.X = 60
	world.ResetObjects(ctx)
	if !world.objects.items[0].Active {
		t.Fatal("reset closed gate over the player")
	}
	for i := 0; i < 30; i++ {
		objectTick(t, world, PlayerCommand{Move: 1})
	}
	if world.state.Player.Bounds.X < 76 || world.objects.items[0].Active {
		t.Fatal("player could not leave the reset gate and allow it to close")
	}
}

func TestLimbBounceDoesNotDamageBreakables(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	addObject(t, world, "first", ObjectBreakable, Rect{X: 105, Width: 4, Height: 60})
	addObject(t, world, "second", ObjectBreakable, Rect{X: 88, Width: 4, Height: 60})
	world.initLimbs()
	world.motion.limbs &^= LeftArm
	world.limbs.objects = []LimbObject{{ID: "limb", Socket: SocketLeftArm, Generation: world.limbs.generation, Airborne: true, Damaging: true, Position: Vector{X: 102, Y: 30}, Velocity: Vector{X: 650}}}
	objectTick(t, world, PlayerCommand{})
	if world.objects.items[0].Damage != 1 || !world.limbs.objects[0].Bounced || world.limbs.objects[0].Damaging {
		t.Fatalf("first damaging impact failed: %+v %+v", world.Objects(ctx), world.LimbView(ctx))
	}
	bounced := false
	for i := 0; i < 30; i++ {
		objectTick(t, world, PlayerCommand{})
		for _, event := range world.LimbView(ctx).Events {
			if event.Kind == "impact" && event.Target == objectColliderID("second") {
				bounced = true
			}
		}
		if bounced {
			break
		}
	}
	if !bounced || world.objects.items[1].Damage != 0 || world.objects.items[1].Broken {
		t.Fatalf("decorative bounce damaged another obstacle: %+v", world.Objects(ctx))
	}
	// A subsequent nondamaging contact with the first obstacle cannot advance
	// the stage even if it is a separately simulated settling flight.
	world.limbs.objects[0].Position = Vector{X: 102, Y: 30}
	world.limbs.objects[0].Velocity = Vector{X: 650}
	world.limbs.objects[0].Airborne = true
	world.limbs.objects[0].Damaging = false
	world.limbs.objects[0].Bounced = true
	objectTick(t, world, PlayerCommand{})
	if world.objects.items[0].Damage != 1 || world.objects.items[0].Broken {
		t.Fatal("settling contact advanced the original obstacle's stage")
	}
}

func TestPaddleImpactOnReleaseTick(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	addObject(t, world, "paddle", ObjectPaddle, Rect{X: 15, Y: 20, Width: 1, Height: 30})
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.RemoveLimb)}})
	for i := 1; i < 42; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	objectTick(t, world, PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Throw)}})
	for i := 1; i < 12; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	if !world.Objects(ctx)[0].Latched {
		t.Fatal("damaging release-tick impact did not latch the paddle")
	}
}

func TestAuthoredPaddlePreviewMatchesExecution(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	saved, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range manifest.Batches(ctx)[:2] {
		for _, id := range batch.Deeds {
			saved = certifyFixture(t, owner, manifest, saved, id)
		}
	}
	saved, err = owner.Accept(ctx, saved, "D-HOS-UTL-01")
	if err != nil {
		t.Fatal(err)
	}
	saved, err = owner.Checkpoint(ctx, saved, "ANC-HOS-03")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	world.state.Player.Bounds.X, world.state.Player.Bounds.Y = 610, 80
	world.state.Player.Support = "terrain-STEP-2"
	objectTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.RemoveLimb)}})
	for i := 1; i < 42; i++ {
		objectTick(t, world, PlayerCommand{})
	}
	var paddleID string
	for _, object := range world.Objects(ctx) {
		if object.Kind == ObjectPaddle {
			paddleID = object.ID
		}
	}
	aim := Vector{X: 1, Y: 0.5}
	preview, err := world.PreviewLimbThrow(ctx, aim, nil)
	if err != nil {
		t.Fatal(err)
	}
	if paddleID == "" || preview.Impact == nil || preview.Impact.Collider.ID != objectColliderID(paddleID) {
		t.Fatalf("preview missed authored paddle: %+v", preview.Impact)
	}
	previewPoint := preview.Points[len(preview.Points)-1]
	objectTick(t, world, PlayerCommand{Aim: aim, Edges: []PlayerEdge{press(settings.Throw)}})
	for i := 0; i < 100; i++ {
		objectTick(t, world, PlayerCommand{})
		for _, event := range world.InteractionView(ctx).Events {
			if event.Kind != "paddle_latched" || event.Target != paddleID {
				continue
			}
			if math.Hypot(previewPoint.X-event.Position.X, previewPoint.Y-event.Position.Y) > Tolerance {
				t.Fatalf("preview %v differs from impact %v", previewPoint, event.Position)
			}
			return
		}
	}
	t.Fatal("actual throw did not hit the previewed paddle")
}

func TestObjectTickFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	world := combatWorld(t)
	addObject(t, world, "crate", ObjectCrate, Rect{X: 28, Width: 20, Height: 20})
	before := world.Objects(ctx)[0]
	command := ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Visible: Rect{Width: 100, Height: 100}, Player: PlayerCommand{Move: 2}}}}}
	if err := world.StepObjects(ctx, command); err == nil {
		t.Fatal("invalid movement was accepted")
	}
	if world.state.Tick != 0 || world.Objects(ctx)[0] != before || len(world.state.Platforms) != 1 || world.state.Platforms[0].Collider.Bounds != before.Bounds {
		t.Fatal("failed tick changed the world")
	}
}

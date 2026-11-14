// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"slices"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
	"too-broke-to-die/internal/tbtd/simulation"
)

func TestWorldViewPublishesAuthoritativePreviewAndCopies(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	loadFixture(t, service, store, campaign.Snapshot{})
	frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight, X: 1}, {Action: settings.Aim, X: 1}}})
	view, err := service.view(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := view.Data(ctx)
	previous, current := service.world.Snapshot(ctx).Data(ctx)
	if len(data.Entities) == 0 || data.Entities[0].Kind != "player" || data.Entities[0].X != current.Player.Bounds.X+current.Player.Bounds.Width/2 || data.Entities[0].PreviousX != previous.Player.Bounds.X+previous.Player.Bounds.Width/2 {
		t.Fatalf("player position: %+v", data.Entities)
	}
	if data.World.Background != "ART-PRO-01" || data.World.Tick != current.Tick || len(data.World.Terrain) == 0 {
		t.Fatalf("world view: %+v", data.World)
	}
	if data.Progress.Anchor != "ANC-PRO-01" {
		t.Fatalf("restored default anchor: %s", data.Progress.Anchor)
	}
	preview, err := service.world.PreviewThrow(ctx, simulation.Vector{}, nil)
	if err != nil || len(preview.Points) != len(data.Aim.Trajectory) || !data.Aim.Visible {
		t.Fatalf("preview: %+v %v", data.Aim, err)
	}
	for i, point := range preview.Points {
		if data.Aim.Trajectory[i] != (Point{X: point.X, Y: point.Y}) {
			t.Fatalf("preview point %d changed", i)
		}
	}
	data.World.Terrain[0].X = -999
	data.Aim.Trajectory[0].X = -999
	again := view.Data(ctx)
	if again.World.Terrain[0].X == -999 || again.Aim.Trajectory[0].X == -999 {
		t.Fatal("view exposes mutable world data")
	}
}

func TestMenuLedgerAndTitleExitSelections(t *testing.T) {
	service, store := sessionFixture(t)
	loadFixture(t, service, store, campaign.Snapshot{})
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Pause, Kind: Pressed}}})
	view := choose(t, service, "open_ledger", "")
	if view.State != StateLedger {
		t.Fatalf("open ledger: %s", view.State)
	}
	view = choose(t, service, "close_ledger", "")
	if view.State != StatePause {
		t.Fatalf("close ledger: %s", view.State)
	}
	service2, _ := sessionFixture(t)
	view = choose(t, service2, "exit_game", "")
	if !view.ExitRequested || view.State != StateTitle {
		t.Fatalf("exit: %+v", view)
	}
}

func TestRestorationReadyIsPublishedUntilResume(t *testing.T) {
	service, store := sessionFixture(t)
	choose(t, service, "continue", "")
	request := store.last()
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Status: StorageSucceeded, Revision: 7, RevisionKnown: true, PayloadDigest: oldDigest, Snapshot: campaign.Snapshot{}})
	view := frame(t, service, FrameInput{})
	if view.State != StatePause || !view.RestorationReady {
		t.Fatalf("restoration pause: %+v", view)
	}
	view = choose(t, service, "resume", "")
	if view.State != StateActive || view.RestorationReady {
		t.Fatalf("resume retained restoration prompt: %+v", view)
	}
}

func TestLiveServiceAndEmergencyPublication(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	active, _ := preparedCertification(t)
	loadFixture(t, service, store, active)
	view, err := service.view(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := view.Data(ctx)
	if data.Tracked != "D-HOS-SUP-01" || data.ServiceStatus != "active" || data.ObjectiveID != "TXT-ACT-SUP-PAD" {
		t.Fatalf("active objective: %+v", data)
	}
	jumped := false
	for tick := 0; tick < 1000; tick++ {
		_, world := service.world.Snapshot(ctx).Data(ctx)
		enemy := service.world.CombatView(ctx).Enemies[0]
		if enemy.Body.Bounds.X-world.Player.Bounds.X < 70 && world.Player.Bounds.Y == 0 {
			break
		}
		player := simulation.PlayerCommand{Move: 1}
		if !jumped && world.Player.Bounds.X >= 240 {
			player.Edges = []simulation.PlayerEdge{{Action: settings.Jump, Pressed: true}}
			jumped = true
		}
		simulationServiceTick(t, service, player, nil)
	}
	for tick := 0; tick < 500; tick++ {
		enemies := service.world.CombatView(ctx).Enemies
		if len(enemies) != 1 || enemies[0].Health == 0 {
			break
		}
		_, world := service.world.Snapshot(ctx).Data(ctx)
		distance := enemies[0].Body.Bounds.X - world.Player.Bounds.X
		player := simulation.PlayerCommand{Aim: simulation.Vector{X: 1}}
		if distance < 0 {
			player.Aim.X = -1
		}
		if distance > 65 {
			player.Move = 1
		} else if distance < -65 {
			player.Move = -1
		} else if service.world.PlayerStatus().Action == simulation.FreeAction {
			player.Edges = []simulation.PlayerEdge{{Action: settings.Melee}, {Action: settings.Melee, Pressed: true}}
		}
		simulationServiceTick(t, service, player, nil)
	}
	jumped = false
	for tick := 0; tick < 1000; tick++ {
		_, world := service.world.Snapshot(ctx).Data(ctx)
		if world.Player.Bounds.X < 240 {
			break
		}
		player := simulation.PlayerCommand{Move: -1}
		if !jumped && world.Player.Bounds.X < 450 {
			player.Edges = []simulation.PlayerEdge{{Action: settings.Jump}, {Action: settings.Jump, Pressed: true}}
			jumped = true
		}
		simulationServiceTick(t, service, player, nil)
	}
	for tick := 0; tick < 2200 && service.world.ServiceView(ctx).Status != simulation.ServiceEvidenceReady; tick++ {
		simulationServiceTick(t, service, simulation.PlayerCommand{Move: 1}, nil)
	}
	view, err = service.view(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data = view.Data(ctx)
	if data.Tracked != "D-HOS-SUP-01" || data.ServiceStatus != "evidence_ready" || data.ObjectiveID != "TXT-EVD-READY" {
		t.Fatalf("evidence-ready objective: %+v", data)
	}
	data = frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Ledger, Kind: Pressed}}})
	if data.State != StateLedger || len(data.Tickets) != 100 {
		t.Fatalf("ledger eligibility publication: %s %d", data.State, len(data.Tickets))
	}
	if data.Tickets[0].Deed != data.Tracked || data.Tickets[0].Status != "evidence_ready" {
		t.Fatalf("evidence-ready ticket: %+v", data.Tickets[0])
	}
	service2, store2 := sessionFixture(t)
	progress, err := service2.campaign.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range service2.manifest.Batches(ctx)[0].Deeds {
		progress = certifyAuthored(t, service2, progress, id)
	}
	loadFixture(t, service2, store2, progress)
	view, err = service2.view(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data = view.Data(ctx)
	if data.Emergency != "EM-HOS-SUP" || !data.EmergencyWarning {
		t.Fatalf("emergency warning: %+v", data)
	}
	found := false
	for _, marker := range data.World.Markers {
		if marker.ID == "EM-HOS-SUP" && marker.State == "warning" {
			found = true
		}
	}
	if !found {
		t.Fatal("emergency warning boundary missing")
	}
}

func TestServiceObjectivePresentationIdentity(t *testing.T) {
	for _, test := range []struct {
		view simulation.ServiceView
		want content.PresentationID
	}{
		{simulation.ServiceView{Objective: "weight the plate and latch the gate"}, "TXT-ACT-RES-GATE"},
		{simulation.ServiceView{Objective: "start the evidence recorder"}, "TXT-ACT-PRV-RECORDER"},
		{simulation.ServiceView{Status: simulation.ServicePending}, "TXT-SAVE-PENDING"},
		{simulation.ServiceView{Status: simulation.ServiceFailed}, "TXT-LED-RESET"},
		{simulation.ServiceView{Objective: "unrecognized"}, ""},
	} {
		if got := serviceObjectiveID(test.view); got != test.want {
			t.Fatalf("%+v: got %s, want %s", test.view, got, test.want)
		}
	}
}

func TestNearbyLandingsUseValidatedLandingArea(t *testing.T) {
	platforms := []content.Rect{{X: 0, Y: 152, Width: 1280, Height: 8}}
	jumps := []content.BossJump{
		{Target: 0, Jump: content.BayJump{TakeoffX: -80}},
		{Target: 0, Jump: content.BayJump{TakeoffX: 1360, Direction: -1}},
	}
	left := nearbyLandings(-80, platforms, jumps)
	right := nearbyLandings(1360, platforms, jumps)
	if len(left) != 1 || left[0] != (WorldRect{X: 0, Y: 160, Width: 160, Height: 8}) || len(right) != 1 || right[0] != (WorldRect{X: 1120, Y: 160, Width: 160, Height: 8}) {
		t.Fatalf("landing extents: %+v %+v", left, right)
	}
	if got := nearbyLandings(600, platforms, jumps); len(got) != 0 {
		t.Fatalf("distant takeoff selected: %+v", got)
	}
}

func TestWorldViewPublishesPuzzleObjectBinding(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	committed, _ := preparedCertification(t)
	loadFixture(t, service, store, committed)
	data := frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight, X: 1}}})
	objects := service.world.Objects(ctx)
	found := false
	for _, object := range objects {
		if object.Kind != simulation.ObjectTrolley {
			continue
		}
		for _, entity := range data.Entities {
			if entity.ID == object.ID {
				found = true
				if entity.Kind != "object" || entity.Asset != "ART-OBJ-HOS-SUP-TROLLEY" || entity.X != object.Bounds.X+object.Bounds.Width/2 || entity.Y != object.Bounds.Y {
					t.Fatalf("trolley view: %+v versus %+v", entity, object)
				}
			}
		}
	}
	if !found {
		t.Fatal("active trolley missing from presentation")
	}
}

func TestLiveRescueActorHasAuthoredStateBinding(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	progress, err := service.campaign.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range service.manifest.Batches(ctx)[0].Deeds {
		progress = certifyAuthored(t, service, progress, id)
	}
	progress, err = service.campaign.Accept(ctx, progress, "D-HOS-RES-01")
	if err != nil {
		t.Fatal(err)
	}
	progress, err = service.campaign.Checkpoint(ctx, progress, "ANC-HOS-02")
	if err != nil {
		t.Fatal(err)
	}
	loadFixture(t, service, store, progress)
	view := frame(t, service, FrameInput{})
	for _, entity := range view.Entities {
		if entity.Kind == "rescue" && entity.Asset == "AN-RES-HOS-01-WAIT" && entity.State == "WAIT" {
			return
		}
	}
	t.Fatalf("missing live rescue actor: %+v", view.Entities)
}

func TestEnemyWarningFollowsAuthoritativePhase(t *testing.T) {
	enemy := simulation.Enemy{ID: "orderly", Kind: simulation.MeleeEnemy, Health: 60, Facing: -1, Phase: simulation.EnemyTelegraph, Body: simulation.Body{Bounds: simulation.Rect{X: 100, Width: 28, Height: 58}}}
	marker, ok := enemyWarning(enemy, 5)
	if !ok || marker.State != "warning" || marker.Bounds.X != 52 || marker.Bounds.Width != 48 {
		t.Fatalf("windup marker: %+v %v", marker, ok)
	}
	enemy.Phase, enemy.AttackID, enemy.StrikeTick = simulation.EnemyRecovery, 1, 6
	if enemyVisualState(enemy, 6) != "active" || enemyVisualState(enemy, 7) != "recovery" {
		t.Fatal("enemy release pose did not follow the strike tick")
	}
	marker, ok = enemyWarning(enemy, 6)
	if !ok || marker.State != "active" {
		t.Fatalf("active marker: %+v %v", marker, ok)
	}
	if _, ok = enemyWarning(enemy, 7); ok {
		t.Fatal("recovery retained attack warning")
	}
	enemy.Health, enemy.Phase = 0, simulation.EnemyDefeated
	if _, ok = enemyWarning(enemy, 6); ok {
		t.Fatal("defeated actor retained attack warning")
	}
	if enemyVisualState(enemy, 6) != "defeated" {
		t.Fatal("defeat was replaced by a stale strike pose")
	}
}

func TestWorldViewThrowReleaseFollowsSimulation(t *testing.T) {
	service, store := sessionFixture(t)
	loadFixture(t, service, store, campaign.Snapshot{})
	view := frame(t, service, FrameInput{Elapsed: 9 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Throw, Kind: Pressed}}})
	if view.Player.Action != "throw" || view.Player.AttackReleased || view.Player.Feces != 5 {
		t.Fatalf("windup: %+v", view.Player)
	}
	for i := 0; i < 20 && !view.Player.AttackReleased; i++ {
		view = frame(t, service, FrameInput{Elapsed: 9 * time.Millisecond})
	}
	if !view.Player.AttackReleased || view.Player.Feces != 4 || view.Player.AttackPhase == "windup" {
		t.Fatalf("release: %+v", view.Player)
	}
}

func TestWorldViewDetachedLimbLifecycleAndRecovery(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	loadFixture(t, service, store, campaign.Snapshot{})
	const tick = 8333334 * time.Nanosecond
	view := frame(t, service, FrameInput{Elapsed: tick, Edges: []ActionEdge{{Action: settings.RemoveLimb, Kind: Pressed}}})
	generation := view.World.Generation
	for i := 0; i < 45 && len(service.world.LimbView(ctx).Objects) == 0; i++ {
		view = frame(t, service, FrameInput{Elapsed: tick})
	}
	limbs := service.world.LimbView(ctx)
	if len(limbs.Objects) != 1 || !limbs.Objects[0].Carried || view.Player.Limbs[0] {
		t.Fatalf("removal did not publish carried limb state: %+v %+v", limbs, view.Player)
	}
	view = frame(t, service, FrameInput{Elapsed: tick, Values: []ActionValue{{Action: settings.Aim, X: 1}}, Edges: []ActionEdge{{Action: settings.Throw, Kind: Pressed}}})
	for i := 0; i < 20 && service.world.LimbView(ctx).Carried != ""; i++ {
		view = frame(t, service, FrameInput{Elapsed: tick})
	}
	limbs = service.world.LimbView(ctx)
	if len(limbs.Objects) != 1 || !limbs.Objects[0].Damaging {
		t.Fatalf("throw did not release a damaging limb: %+v", limbs)
	}
	id := limbs.Objects[0].ID
	check := func(state string, markerKind string) {
		t.Helper()
		found := false
		for _, entity := range view.Entities {
			if entity.ID == id && entity.Kind == "limb" && entity.Variant == "LIM-LA" && entity.State == state {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing limb %s: %+v", state, view.Entities)
		}
		for _, marker := range view.World.Markers {
			if marker.ID == id && marker.Kind == markerKind {
				return
			}
		}
		t.Fatalf("missing %s marker: %+v", markerKind, view.World.Markers)
	}
	check("damaging", "projectile")
	for i := 0; i < 300 && service.world.LimbView(ctx).Objects[0].Airborne; i++ {
		view = frame(t, service, FrameInput{Elapsed: tick})
	}
	if service.world.LimbView(ctx).Objects[0].Airborne {
		t.Fatal("limb did not settle")
	}
	check("settled", "limb")
	if err := service.policy.Transition(ctx, OpenPause, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.restore(ctx, ReturnCheckpoint); err != nil {
		t.Fatal(err)
	}
	viewValue, err := service.view(ctx)
	if err != nil {
		t.Fatal(err)
	}
	view = viewValue.Data(ctx)
	if view.World.Generation == generation || len(service.world.LimbView(ctx).Objects) != 0 || !view.Player.Limbs[0] {
		t.Fatalf("restoration retained old world: %+v", view.World)
	}
}

func TestWorldViewPublishesEnemyShotWithoutInventedAsset(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	loadFixture(t, service, store, campaign.Snapshot{})
	if err := service.world.AddEnemy(ctx, "test-courier", simulation.RangedEnemy, simulation.Rect{X: 330, Y: 0, Width: 28, Height: 58}); err != nil {
		t.Fatal(err)
	}
	var view FrameViewData
	var shot simulation.Projectile
	for i := 0; i < 150; i++ {
		view = frame(t, service, FrameInput{Elapsed: 8333334 * time.Nanosecond})
		projectiles := service.world.CombatView(ctx).Projectiles
		if len(projectiles) > 0 {
			shot = projectiles[0]
			break
		}
	}
	if shot.ID == "" || shot.PlayerOwned {
		t.Fatal("enemy never released a projectile")
	}
	for _, entity := range view.Entities {
		if entity.ID == shot.ID {
			if entity.Kind != "projectile" || entity.Variant != "enemy:HOS:RNG" || entity.Asset != "" || entity.X != shot.Position.X || entity.Y != shot.Position.Y {
				t.Fatalf("enemy projectile: %+v", entity)
			}
			return
		}
	}
	t.Fatalf("released projectile missing from view: %+v", view.Entities)
}

func TestWorldViewCarriesReticleInput(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	loadFixture(t, service, store, campaign.Snapshot{})
	reticle := ReticleInput{Pointer: true, Visible: true, X: 300, Y: 250}
	view := frame(t, service, FrameInput{Reticle: reticle})
	if view.Aim.Reticle != reticle {
		t.Fatalf("reticle lost: %+v", view.Aim.Reticle)
	}
	if _, err := NewFrameCommand(ctx, FrameInput{Reticle: ReticleInput{Pointer: true, Visible: true, X: 1280}}); err == nil {
		t.Fatal("out-of-canvas reticle accepted")
	}
	view = frame(t, service, FrameInput{Focus: FocusLost, Reticle: reticle})
	if view.Aim.Reticle.Visible {
		t.Fatal("focus loss retained active reticle")
	}
}

func TestLiveBossArenaPublishesWarningBoundaries(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	progress := authoredProgress(t, service, 1, false)
	progress, err := service.campaign.Checkpoint(ctx, progress, "ANC-HOS-05")
	if err != nil {
		t.Fatal(err)
	}
	loadFixture(t, service, store, progress)
	var view FrameViewData
	for i := 0; i < 300 && service.world.Boss(ctx).Stage == simulation.BossDormant; i++ {
		view = frame(t, service, FrameInput{Elapsed: 8333334 * time.Nanosecond, Values: []ActionValue{{Action: settings.MoveRight, X: 1}}})
	}
	assertDanger := func(expected simulation.BossStage, markerState string) {
		t.Helper()
		boss := service.world.Boss(ctx)
		if boss.Stage != expected || len(boss.Danger) == 0 {
			t.Fatalf("%s without authoritative danger: %+v", expected, boss)
		}
		arena := service.bossArena.View(ctx)
		if !slices.Equal(arena.Danger, boss.Danger) || !slices.Equal(arena.Boss.Danger, boss.Danger) {
			t.Fatalf("%s arena warning disagrees with boss: %+v %+v", expected, arena, boss)
		}
		arena.Danger[0].X += 1
		if arena.Danger[0] == arena.Boss.Danger[0] || !slices.Equal(service.world.Boss(ctx).Danger, boss.Danger) {
			t.Fatal("arena warning slices share state")
		}
		found := false
		for _, marker := range view.World.Markers {
			if marker.ID == "ENC-HOS-05:danger:0" && marker.State == markerState && marker.Bounds == worldRect(boss.Danger[0]) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s warning not published: %+v", expected, view.World.Markers)
		}
		found = false
		for _, entity := range view.Entities {
			if entity.ID == "ENC-HOS-05" && entity.Kind == "boss" && entity.Asset == bossAsset(boss, "HOS") && entity.State == string(expected) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s boss artwork not published: %+v", expected, view.Entities)
		}
	}
	assertDanger(simulation.BossTelegraph, "warning")
	for i := 0; i < 250 && service.world.Boss(ctx).Stage == simulation.BossTelegraph; i++ {
		view = frame(t, service, FrameInput{Elapsed: 8333334 * time.Nanosecond, Values: []ActionValue{{Action: settings.MoveRight}}})
	}
	assertDanger(simulation.BossActive, "active")
	for i := 0; i < 250 && service.world.Boss(ctx).Stage == simulation.BossActive; i++ {
		view = frame(t, service, FrameInput{Elapsed: 8333334 * time.Nanosecond})
	}
	if boss := service.world.Boss(ctx); boss.Stage != simulation.BossRecovery || len(boss.Danger) != 0 {
		t.Fatalf("recovery retained danger: %+v", boss)
	}
	for _, marker := range view.World.Markers {
		if marker.ID == "ENC-HOS-05:danger:0" {
			t.Fatal("recovery retained danger marker")
		}
	}
	defeatedService, defeatedStore := sessionFixture(t)
	defeated, err := defeatedService.campaign.DefeatBoss(ctx, progress, "ENC-HOS-05")
	if err != nil {
		t.Fatal(err)
	}
	loadFixture(t, defeatedService, defeatedStore, defeated)
	view = frame(t, defeatedService, FrameInput{})
	if boss := defeatedService.world.Boss(ctx); boss.Stage != simulation.BossDefeated || len(boss.Danger) != 0 {
		t.Fatalf("defeat retained danger: %+v", boss)
	}
	for _, marker := range view.World.Markers {
		if marker.ID == "ENC-HOS-05:danger:0" {
			t.Fatal("defeated boss retained danger marker")
		}
	}
}

func TestAcceptedDamageProducesIsolatedEffectEvent(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	committed, _ := preparedCertification(t)
	loadFixture(t, service, store, committed)
	hit := simulation.DamageHit{Attacker: "hazard", Attack: "first", Kind: simulation.OrdinaryDamage, Amount: 18, Direction: 1}
	simulationServiceTick(t, service, simulation.PlayerCommand{}, []simulation.DamageHit{hit})
	service.captureEffects(ctx)
	view, err := service.view(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := view.Data(ctx)
	if len(data.World.Effects) != 1 || data.World.Effects[0].Kind != "hit" || data.World.Effects[0].Asset != "FX-BODY" || data.World.Effects[0].Entity != "player" || data.World.Effects[0].Direction != 1 {
		t.Fatalf("accepted damage event: %+v", data.World.Effects)
	}
	if len(data.Texts) != 1 || data.Texts[0].ID != "CAP-HURT" {
		t.Fatalf("accepted damage caption: %+v", data.Texts)
	}
	data.World.Effects[0].Entity = "mutated"
	if view.Data(ctx).World.Effects[0].Entity != "player" {
		t.Fatal("view exposes mutable effects")
	}
	data = view.Data(ctx)
	data.World.Effects[0].Particles = []VisualPart{{Asset: "FX-BODY"}}
	copied, err := NewFrameView(ctx, data, service.committed, service.id, service.revision)
	if err != nil {
		t.Fatal(err)
	}
	data.World.Effects[0].Particles[0].Asset = "changed"
	isolated := copied.Data(ctx)
	isolated.World.Effects[0].Particles[0].Asset = "changed again"
	if copied.Data(ctx).World.Effects[0].Particles[0].Asset != "FX-BODY" {
		t.Fatal("particle frames alias frame inputs or outputs")
	}
	hit.Attack = "second"
	simulationServiceTick(t, service, simulation.PlayerCommand{}, []simulation.DamageHit{hit})
	service.captureEffects(ctx)
	if len(service.effects) != 1 {
		t.Fatalf("immune hit produced another effect: %+v", service.effects)
	}
}

func TestWorldViewReportsInterruptionImmediately(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	loadFixture(t, service, store, campaign.Snapshot{})
	if err := service.world.StartPlayerAction(ctx, simulation.HurtAction, 180*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	view, err := service.view(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := view.Data(ctx)
	if data.Player.Action != "hurt" || data.Player.AttackReleased || data.Player.Health != 100 {
		t.Fatalf("interruption not authoritative: %+v", data.Player)
	}
}

// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"os"
	"reflect"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

func finalBossFixture(t *testing.T, institute bool) (*campaign.Campaign, *content.Campaign, []content.ServiceBay, campaign.Snapshot, *World, *BossArena) {
	t.Helper()
	ctx := context.Background()
	owner, manifest, bays, committed, _, _ := localBossFixture(t, true)
	var err error
	committed, err = owner.DefeatBoss(ctx, committed, "ENC-CHU-05")
	if err != nil {
		t.Fatal(err)
	}
	committed = certifyFixture(t, owner, manifest, committed, "D-CHU-END")
	for _, batch := range manifest.Batches(ctx)[8:12] {
		for _, deed := range batch.Deeds {
			committed = certifyFixture(t, owner, manifest, committed, deed)
		}
	}
	anchor := content.AnchorID("ANC-MIN-05")
	if institute {
		committed, err = owner.DefeatBoss(ctx, committed, "ENC-MIN-05")
		if err != nil {
			t.Fatal(err)
		}
		committed = certifyFixture(t, owner, manifest, committed, "D-MIN-END")
		for _, batch := range manifest.Batches(ctx)[12:16] {
			for _, deed := range batch.Deeds {
				committed = certifyFixture(t, owner, manifest, committed, deed)
			}
		}
		anchor = "ANC-INS-05"
	}
	committed, err = owner.Checkpoint(ctx, committed, anchor)
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	world.state.Player.Bounds.X = 548
	world.state.Player.Support = "boundary-floor"
	if !institute {
		world.state.Player.Bounds.Y = 160
		world.state.Player.Support = "boss-platform-001"
	}
	arena, err := NewBossArena(ctx, world)
	if err != nil {
		t.Fatal(err)
	}
	return owner, manifest, bays, committed, world, arena
}

func TestFinalBossPresentations(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		path, actor, name string
		institute         bool
	}{
		{path: "content/data/bosses/ministry.json", actor: "CHR-BLA", name: "Аркадий Благоев"},
		{path: "content/data/bosses/institute.json", actor: "CHR-LEV", name: "Лев Виральский", institute: true},
	} {
		t.Run(tc.actor, func(t *testing.T) {
			presentation, err := LoadBossPresentation(ctx, os.DirFS("../../.."), tc.path)
			if err != nil || presentation.Actor != tc.actor || presentation.Name != tc.name || !presentation.Adult || presentation.MediaStatus != "missing" {
				t.Fatalf("presentation: %+v %v", presentation, err)
			}
			_, _, _, _, _, arena := finalBossFixture(t, tc.institute)
			if err = arena.SetPresentation(ctx, presentation); err != nil || arena.View(ctx).Presentation == nil {
				t.Fatalf("bind presentation: %v", err)
			}
			presentation.Adult = false
			if err = arena.SetPresentation(ctx, presentation); err == nil {
				t.Fatal("accepted non-adult presentation")
			}
		})
	}
}

func TestMinistryAttackCyclesAndLock(t *testing.T) {
	ctx := context.Background()
	orders := [][]string{{"MIN-A", "MIN-C"}, {"MIN-B", "MIN-A", "MIN-C"}, {"MIN-A", "MIN-B", "MIN-C"}}
	for phase, order := range orders {
		_, _, _, _, world, arena := finalBossFixture(t, false)
		world.state.Player.Bounds.X = 50
		world.boss.phase, world.boss.health, world.boss.stage = phase, []int{420, 294, 147}[phase], BossTelegraph
		for _, id := range order {
			attack := world.boss.attack()
			if attack.ID != id {
				t.Fatalf("phase %d: %s", phase, attack.ID)
			}
			arenaTick(t, arena, PlayerCommand{})
			if len(arena.View(ctx).Danger) == 0 || arena.View(ctx).Warning == "" {
				t.Fatal("warning absent")
			}
			for n := 1; n < msTicks(attack.TelegraphMS)-1; n++ {
				arenaTick(t, arena, PlayerCommand{})
			}
			if world.boss.stage != BossTelegraph {
				t.Fatal("telegraph ended early")
			}
			arenaTick(t, arena, PlayerCommand{})
			locked := append([]Vector(nil), world.boss.locked...)
			for n := 0; n < 300 && world.boss.stage == BossActive; n++ {
				arenaTick(t, arena, PlayerCommand{})
				if !reflect.DeepEqual(locked, world.boss.locked) {
					t.Fatal("retargeted committed attack")
				}
			}
			if world.boss.stage != BossRecovery || world.Boss(ctx).Protected {
				t.Fatalf("no recovery: %+v", arena.View(ctx))
			}
			for n := 0; n < msTicks(attack.RecoveryMS)-1; n++ {
				arenaTick(t, arena, PlayerCommand{})
			}
			if world.boss.stage != BossRecovery {
				t.Fatal("exposure ended early")
			}
			arenaTick(t, arena, PlayerCommand{})
		}
		if world.boss.index != 0 || world.state.Health != 100 || arena.View(ctx).WasteHeight != 40*(phase+1) {
			t.Fatalf("cycle or dry alcove: %+v", arena.View(ctx))
		}
	}
}

func TestMinistryCombatEntryAndTargetSampling(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world, arena := finalBossFixture(t, false)
	world.state.Player.Bounds.Y = 0
	world.state.Player.Support = "boundary-floor"
	arenaTick(t, arena, PlayerCommand{})
	if world.boss.stage != BossDormant {
		t.Fatal("combat began below the gantry")
	}
	world.state.Player.Bounds.Y = 160
	world.state.Player.Velocity = Vector{}
	world.state.Player.Support = "boss-platform-001"
	arenaTick(t, arena, PlayerCommand{})
	if world.boss.stage != BossTelegraph {
		t.Fatal("gantry entry did not start combat")
	}
	for i := 0; i < 83; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	arenaTick(t, arena, PlayerCommand{Move: 1})
	locked := world.Boss(ctx).LockedTargets
	body := world.state.Player.Bounds
	if len(locked) != 1 || locked[0] != (Vector{X: centerX(body), Y: centerY(body)}) {
		t.Fatalf("target was not sampled at the boundary: %+v, %+v", locked, body)
	}
	for i := 0; i < 20; i++ {
		arenaTick(t, arena, PlayerCommand{Move: 1})
	}
	if !reflect.DeepEqual(world.Boss(ctx).LockedTargets, locked) {
		t.Fatal("glob sequence followed later movement")
	}
}

func TestMinistryGlobsAndWaste(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world, arena := finalBossFixture(t, false)
	world.state.Player.Bounds.X = 300
	beginLocalAttack(t, arena, 0, 0)
	locked := world.boss.locked[0]
	world.state.Player.Bounds.X = 50
	for n := 0; n <= 90; n++ {
		arenaTick(t, arena, PlayerCommand{})
		if n == 30 || n == 60 || n == 90 {
			shots := arena.View(ctx).Projectiles
			if len(shots) != n/30 {
				t.Fatalf("release %d: %d", n, len(shots))
			}
			for _, shot := range shots {
				if shot.Attack != "MIN-A" {
					t.Fatalf("lost release identity: %+v", shot)
				}
			}
		}
		if world.boss.locked[0] != locked {
			t.Fatal("glob target moved")
		}
	}
	for phase := 0; phase < 3; phase++ {
		world.boss.phase = phase
		view := arena.View(ctx)
		if view.WasteHeight != 40*(phase+1) || view.WasteHeight >= 160 || view.WasteDamage != 15 {
			t.Fatalf("waste: %+v", view)
		}
	}
	world.boss.phase, world.boss.health, world.boss.stage = 0, 420, BossRecovery
	world.state.Player.Bounds = Rect{X: 400, Y: 0, Width: PlayerWidth, Height: PlayerHeight}
	world.state.Player.Velocity = Vector{}
	world.state.Player.Support = "boundary-floor"
	arenaTick(t, arena, PlayerCommand{})
	if world.state.Health != 85 || world.state.Feces != 5 {
		t.Fatalf("sump damage or feces: %+v", world.state)
	}
	arenaTick(t, arena, PlayerCommand{})
	if world.state.Health != 85 {
		t.Fatal("sump bypassed ordinary immunity")
	}
}

func TestMinistryPressCanBeEscapedFromEitherEdge(t *testing.T) {
	ctx := context.Background()
	for _, right := range []bool{false, true} {
		_, _, _, _, world, arena := finalBossFixture(t, false)
		direction := 1.0
		world.state.Player.Bounds.X = 160
		if right {
			world.state.Player.Bounds.X = 1092
			direction = -1
		}
		beginLocalAttack(t, arena, 0, 1)
		world.state.Player.Velocity.X = -direction * 320
		for n := 0; n < 300 && world.boss.stage == BossActive; n++ {
			arenaTick(t, arena, PlayerCommand{Move: direction})
		}
		if world.state.Health != 100 || world.boss.stage != BossRecovery || (right && world.state.Player.Bounds.X >= 640) || (!right && world.state.Player.Bounds.X < 640) {
			t.Fatalf("could not reach unmarked half: %+v, player %+v", arena.View(ctx), world.state.Player)
		}
	}
}

func TestMinistryAttackContactsAndDryGeometry(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		phase, index, damage int
	}{
		{phase: 0, index: 0, damage: 24},
		{phase: 1, index: 0, damage: 35},
		{phase: 0, index: 1, damage: 24},
	} {
		_, _, bays, _, world, arena := finalBossFixture(t, false)
		geometry := bays[48].Institution.Boss
		if len(geometry.Platforms) != 3 || geometry.Platforms[1].Y+geometry.Platforms[1].Height != 160 || geometry.Platforms[1].Width != 1280 || geometry.Platforms[0].Y+geometry.Platforms[0].Height != 80 || geometry.Platforms[2].Y+geometry.Platforms[2].Height != 80 || geometry.Refills[0].Y != 160 || geometry.Refills[1].Y != 160 || len(geometry.Jumps) != 4 {
			t.Fatalf("dry route or escape stairs: %+v", geometry)
		}
		beginLocalAttack(t, arena, tc.phase, tc.index)
		for n := 0; n < 300 && world.boss.stage == BossActive; n++ {
			arenaTick(t, arena, PlayerCommand{})
		}
		if world.state.Health != 100-tc.damage || world.boss.stage != BossRecovery {
			t.Fatalf("attack %s: health %d, %+v", world.boss.attack().ID, world.state.Health, arena.View(ctx))
		}
	}
}

func TestMinistryIntactMeleeCompletion(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world, arena := finalBossFixture(t, false)
	for tick := 0; tick < 24000 && world.boss.stage != BossPending; tick++ {
		view := world.Boss(ctx)
		body := world.state.Player.Bounds
		player := PlayerCommand{Aim: Vector{X: 1}}
		if view.Stage == BossActive {
			if body.X < 660 {
				player.Move = 1
			}
		} else if body.X > 555 {
			player.Move = -1
		} else if body.X < 540 {
			player.Move = 1
		} else if view.Stage == BossRecovery && (world.motion.action == FreeAction || world.state.Tick >= world.motion.actionUntil) {
			player.Edges = []PlayerEdge{release(settings.Melee), press(settings.Melee)}
		}
		arenaTick(t, arena, player)
		if world.motion.dead {
			t.Fatalf("intact player died at tick %d: %+v", tick, arena.View(ctx))
		}
	}
	if world.boss.stage != BossPending || world.state.Feces != 5 || world.motion.limbs != AllLimbs || len(world.limbs.objects) != 0 {
		t.Fatalf("intact melee: %+v", arena.View(ctx))
	}
}

func TestFinalBossThresholdClearsProjectiles(t *testing.T) {
	ctx := context.Background()
	for _, institute := range []bool{false, true} {
		_, _, _, _, world, arena := finalBossFixture(t, institute)
		world.boss.stage, world.boss.health = BossRecovery, 315
		if institute {
			world.boss.relays[0].Health = 0
		}
		arena.projectiles = []ArenaProjectile{{ID: "obsolete", Position: Vector{X: 400, Y: 200}, Remaining: 100}}
		world.combat.projectiles = append(world.combat.projectiles, Projectile{ID: "old", Attacker: string(world.boss.definition.Encounter), Position: Vector{X: 900, Y: 100}})
		arenaTick(t, arena, PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}})
		for i := 1; i < 15; i++ {
			arenaTick(t, arena, PlayerCommand{})
		}
		if view := arena.View(ctx); view.Boss.Health != 293 || view.Boss.Stage != BossTransition || len(view.Projectiles) != 0 || len(world.combat.projectiles) != 0 || !view.Boss.Protected {
			t.Fatalf("threshold interruption: %+v", view)
		}
		for i := 0; i < 119; i++ {
			arenaTick(t, arena, PlayerCommand{})
		}
		if world.boss.stage != BossTransition {
			t.Fatal("transition ended early")
		}
		arenaTick(t, arena, PlayerCommand{})
		if world.boss.stage != BossTelegraph || world.boss.index != 0 {
			t.Fatalf("transition did not start phase: %+v", world.Boss(ctx))
		}
	}
}

func TestFinalBossVictoryAndLethalHitSameTick(t *testing.T) {
	ctx := context.Background()
	for _, institute := range []bool{false, true} {
		owner, _, _, committed, world, arena := finalBossFixture(t, institute)
		world.boss.stage, world.boss.phase, world.boss.health = BossRecovery, 2, 22
		if institute {
			world.resetBossRelays()
			for i := range world.boss.relays {
				world.boss.relays[i].Health = 0
			}
		}
		arenaTick(t, arena, PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}})
		for i := 1; i < 14; i++ {
			arenaTick(t, arena, PlayerCommand{})
		}
		command := ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{
			Visible: Rect{X: -240, Y: -8, Width: 1760, Height: 568},
			Hits:    []DamageHit{{Attacker: "test-lethal", Attack: "same-tick", Kind: LethalDamage, Amount: 100, Direction: 1}},
		}}}}
		if err := arena.Step(ctx, command); err != nil {
			t.Fatal(err)
		}
		if world.boss.stage != BossPending || !world.motion.dead || world.Boss(ctx).Pending == nil || world.Boss(ctx).Confirmed {
			t.Fatalf("same-tick victory and death: %+v", arena.View(ctx))
		}
		proposal, err := world.PrepareBoss(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
		if err != nil || proposal.Credits() != committed.Credits() {
			t.Fatalf("ordered victory/death proposal: %v", err)
		}
	}
}

func TestMinistryRetryAndAcknowledgedVictory(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world, arena := finalBossFixture(t, false)
	world.boss.stage, world.boss.phase, world.boss.health = BossRecovery, 2, 22
	arenaTick(t, arena, PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}})
	for i := 1; i < 15; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	if view := arena.View(ctx); view.Boss.Stage != BossPending || view.Survived || view.WasteDamage != 0 || view.LiftJammed {
		t.Fatalf("pending: %+v", view)
	}
	proposal, err := world.PrepareBoss(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
	if err != nil || proposal.Credits() != committed.Credits() {
		t.Fatalf("victory proposal: %v", err)
	}
	if err = world.AcknowledgeBoss(ctx, owner, ServiceAcknowledgment{}); err == nil || arena.View(ctx).Survived {
		t.Fatal("failed save confirmed")
	}
	retry, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := NewBossArena(ctx, retry)
	if err != nil || fresh.View(ctx).Boss.Health != 420 || fresh.View(ctx).WasteHeight != 40 || fresh.View(ctx).Boss.Stage != BossDormant {
		t.Fatalf("retry: %+v %v", fresh.View(ctx), err)
	}
	if err = world.AcknowledgeBoss(ctx, owner, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	if view := arena.View(ctx); !view.Survived || !view.LiftJammed || !view.SanitationExercise || view.WasteHeight != 0 {
		t.Fatalf("acknowledged outcome: %+v", view)
	}
	restored, err := NewWorld(ctx, owner, manifest, proposal, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	restoredArena, err := NewBossArena(ctx, restored)
	if err != nil || !restoredArena.View(ctx).LiftJammed || !restoredArena.View(ctx).SanitationExercise {
		t.Fatalf("committed outcome not restored: %v", err)
	}
}

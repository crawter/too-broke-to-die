// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

func bossFixture(t *testing.T) (*campaign.Campaign, *content.Campaign, []content.ServiceBay, campaign.Snapshot, *World) {
	t.Helper()
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	committed, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range manifest.Batches(ctx)[:4] {
		for _, id := range batch.Deeds {
			committed = certifyFixture(t, owner, manifest, committed, id)
		}
	}
	committed, err = owner.Checkpoint(ctx, committed, "ANC-HOS-05")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	world.state.Player.Bounds.X = 548
	world.state.Player.Support = "boundary-floor"
	return owner, manifest, bays, committed, world
}

func bossTick(t *testing.T, world *World, player PlayerCommand, hits ...DamageHit) {
	t.Helper()
	command := ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: player, Hits: hits, Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}}}}}
	if err := world.StepBoss(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

// TestBossSafeAreas exercises protection installed by NewWorld, not a manually
// supplied combat rectangle. Each projectile crosses an actual authored edge.
func TestBossSafeAreas(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, _ := bossFixture(t)
	geometry := bays[0].Institution.Boss
	restore := func(t *testing.T) *World {
		t.Helper()
		world, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
		if err != nil {
			t.Fatal(err)
		}
		// Start at the beginning of an exposed recovery, before the next attack.
		world.boss.stage, world.boss.recovery = BossRecovery, 1
		return world
	}
	areas := []struct {
		name      string
		point     content.GeometryPoint
		rect      content.Rect
		direction float64
	}{
		{name: "checkpoint", point: geometry.Checkpoint, rect: geometry.Protection[0], direction: 1},
		{name: "left refill", point: geometry.Refills[0], rect: geometry.Protection[1], direction: 1},
		{name: "right refill", point: geometry.Refills[1], rect: geometry.Protection[2], direction: -1},
	}
	for index, area := range areas {
		t.Run(area.name, func(t *testing.T) {
			t.Run("incoming projectile", func(t *testing.T) {
				world := restore(t)
				world.state.Player.Bounds.X = area.point.X - PlayerWidth/2
				world.state.Player.Bounds.Y = area.point.Y
				start := area.rect.X + area.rect.Width + 8
				if area.direction < 0 {
					start = area.rect.X - 8
				}
				world.combat.projectiles = []Projectile{{ID: "incoming", Attacker: "ENC-HOS-05", Attack: "incoming", Position: Vector{X: start, Y: area.point.Y + PlayerHeight/2}, Velocity: Vector{X: -area.direction * 12000}}}
				bossTick(t, world, PlayerCommand{})
				if world.state.Health != 100 || len(world.combat.projectiles) != 0 || len(world.CombatView(ctx).Outcomes) != 0 {
					t.Fatalf("projectile entered protected area: health %d, combat %+v", world.state.Health, world.CombatView(ctx))
				}
			})
			t.Run("outgoing throw", func(t *testing.T) {
				world := restore(t)
				world.state.Player.Bounds.X = area.point.X - PlayerWidth/2
				world.state.Player.Bounds.Y = area.point.Y
				bossTick(t, world, PlayerCommand{Aim: Vector{X: area.direction}, Edges: []PlayerEdge{press(settings.Throw)}})
				var impacts []FecesImpact
				for tick := 0; tick < 20 && len(impacts) == 0; tick++ {
					bossTick(t, world, PlayerCommand{})
					impacts = world.FecesView(ctx).Impacts
				}
				if world.state.Feces != 4 || len(impacts) != 1 || impacts[0].Kind != "blocked" || impacts[0].Target != fmt.Sprintf("combat-protection:%d", index) || world.Boss(ctx).Health != 420 || len(world.combat.projectiles) != 0 {
					t.Fatalf("throw escaped protected area: impacts %+v, combat %+v", impacts, world.CombatView(ctx))
				}
			})
			t.Run("outgoing melee", func(t *testing.T) {
				world := restore(t)
				playerX := area.rect.X + area.rect.Width - PlayerWidth/2
				enemyX := area.rect.X + area.rect.Width + 20
				if area.direction < 0 {
					playerX = area.rect.X - PlayerWidth/2
					enemyX = area.rect.X - 24 - PlayerWidth
				}
				world.state.Player.Bounds.X = playerX
				world.state.Player.Bounds.Y = area.point.Y
				if err := world.AddEnemy(ctx, "outside", MeleeEnemy, Rect{X: enemyX, Y: area.point.Y, Width: PlayerWidth, Height: PlayerHeight}); err != nil {
					t.Fatal(err)
				}
				bossTick(t, world, PlayerCommand{Aim: Vector{X: area.direction}, Edges: []PlayerEdge{press(settings.Melee)}})
				for tick := 1; tick < 15; tick++ {
					bossTick(t, world, PlayerCommand{})
				}
				if world.combat.attack.Phase != PhaseActive || len(world.combat.enemies) != 1 || world.combat.enemies[0].Health != 60 {
					t.Fatalf("melee escaped protected area: %+v", world.CombatView(ctx))
				}
			})
		})
	}
	t.Run("unprotected projectile control", func(t *testing.T) {
		world := restore(t)
		world.state.Player.Bounds.X = 548
		world.combat.projectiles = []Projectile{{ID: "incoming", Attacker: "ENC-HOS-05", Attack: "incoming", Position: Vector{X: 600, Y: PlayerHeight / 2}, Velocity: Vector{X: -12000}}}
		bossTick(t, world, PlayerCommand{})
		if world.state.Health != 82 || len(world.combat.outcomes) != 1 || world.combat.outcomes[0].Result != HitAccepted {
			t.Fatalf("control projectile did not hit: %+v", world.CombatView(ctx))
		}
	})
	t.Run("unprotected attack control", func(t *testing.T) {
		world := restore(t)
		world.state.Player.Bounds.X = 548
		bossTick(t, world, PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Throw)}})
		for tick := 0; tick < 40 && world.Boss(ctx).Health == 420; tick++ {
			bossTick(t, world, PlayerCommand{})
		}
		if world.Boss(ctx).Health != 412 || world.state.Feces != 4 {
			t.Fatalf("control throw did not damage exposed boss: %+v", world.Boss(ctx))
		}
	})
}

func TestBossRefillDuringRecovery(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, _ := bossFixture(t)
	geometry := bays[0].Institution.Boss
	for index, point := range geometry.Refills {
		t.Run(fmt.Sprintf("refill-%d", index), func(t *testing.T) {
			world, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
			if err != nil {
				t.Fatal(err)
			}
			world.boss.stage, world.boss.recovery = BossRecovery, 1
			world.state.Player.Bounds.X = point.X - PlayerWidth/2
			world.state.Player.Bounds.Y = point.Y
			world.state.Feces = 0
			station := fmt.Sprintf("station-%s-REFILL-%03d", geometry.Encounter, index)
			bossTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}})
			if world.boss.stage != BossRecovery || world.FecesView(ctx).Channel != station {
				t.Fatalf("refill did not start during recovery: %+v", world.FecesView(ctx))
			}
			for tick := 1; tick < 179; tick++ {
				bossTick(t, world, PlayerCommand{})
			}
			if world.state.Feces != 0 || world.FecesView(ctx).Channel != station {
				t.Fatal("refill completed before its hold boundary")
			}
			bossTick(t, world, PlayerCommand{})
			if world.state.Feces != 5 || world.FecesView(ctx).Channel != "" {
				t.Fatalf("refill failed after the hold: %+v", world.FecesView(ctx))
			}
		})
	}
}

func TestBossTimingLockAndOrder(t *testing.T) {
	_, _, _, _, world := bossFixture(t)
	bossTick(t, world, PlayerCommand{})
	if world.boss.stage != BossTelegraph || world.boss.attack().ID != "HOS-A" {
		t.Fatalf("entry: %+v", world.Boss(context.Background()))
	}
	for i := 0; i < 83; i++ {
		bossTick(t, world, PlayerCommand{})
	}
	if world.boss.stage != BossTelegraph || len(world.boss.locked) != 0 {
		t.Fatal("telegraph ended early")
	}
	bossTick(t, world, PlayerCommand{Move: 1})
	if world.boss.stage != BossActive || len(world.boss.locked) != 1 {
		t.Fatal("target not locked on boundary")
	}
	locked := world.boss.locked[0]
	bossTick(t, world, PlayerCommand{Move: 1})
	if world.boss.stage != BossRecovery || world.boss.locked[0] != locked {
		t.Fatal("active target moved or recovery missing")
	}
	for i := 0; i < 167; i++ {
		bossTick(t, world, PlayerCommand{})
	}
	if world.boss.stage != BossRecovery {
		t.Fatal("recovery ended early")
	}
	bossTick(t, world, PlayerCommand{})
	if world.boss.stage != BossTelegraph || world.boss.attack().ID != "HOS-B" {
		t.Fatalf("wrong second attack: %+v", world.Boss(context.Background()))
	}
}

func TestMultiTargetHookLocksAndQuantizes(t *testing.T) {
	ctx := context.Background()
	_, _, bays := campaignFixture(t)
	world := combatWorld(t)
	if err := world.installBoss(ctx, bays[24].Institution.Boss, campaign.RestorationPlan{ResetPartialBosses: true}, campaign.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	world.boss.stage, world.boss.phase, world.boss.health = BossTelegraph, 1, 294
	if err := world.prepareBossMarkers(ctx, []Vector{{X: 1}}); err == nil {
		t.Fatal("wrong target count accepted")
	}
	for i := 0; i < 83; i++ {
		bossTick(t, world, PlayerCommand{})
	}
	markers := []Vector{{X: 50, Y: 20}, {X: 60, Y: 100}, {X: 70, Y: 180}}
	if err := world.prepareBossMarkers(ctx, markers); err != nil {
		t.Fatal(err)
	}
	bossTick(t, world, PlayerCommand{})
	markers[0].X = 999
	if world.boss.stage != BossActive || world.boss.locked[0].X != 50 {
		t.Fatal("markers were not isolated at the lock boundary")
	}
	if err := world.prepareBossMarkers(ctx, markers); err == nil {
		t.Fatal("active attack retargeted")
	}
	for i := 0; i < 84; i++ {
		bossTick(t, world, PlayerCommand{Move: 1})
	}
	if world.boss.stage != BossActive || world.boss.locked[0].X != 50 {
		t.Fatal("701 ms active interval ended early or targets moved")
	}
	bossTick(t, world, PlayerCommand{})
	if world.boss.stage != BossRecovery {
		t.Fatal("701 ms interval did not round up to 85 ticks")
	}
}

func TestBossActiveDurationHook(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world := bossFixture(t)
	world.boss.stage = BossActive
	if err := world.extendBossActive(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if err := world.extendBossActive(ctx, 2); err == nil {
		t.Fatal("active duration shortened")
	}
	for i := 0; i < 2; i++ {
		bossTick(t, world, PlayerCommand{})
	}
	if world.boss.stage != BossActive || !world.Boss(ctx).Protected {
		t.Fatal("active attack did not remain protected")
	}
	bossTick(t, world, PlayerCommand{})
	if world.boss.stage != BossRecovery {
		t.Fatal("extended attack did not end")
	}
	if err := world.extendBossActive(ctx, 3); err == nil {
		t.Fatal("recovery was turned into an active attack")
	}
}

func TestAllBossPhaseOrders(t *testing.T) {
	ctx := context.Background()
	_, _, bays := campaignFixture(t)
	for _, index := range []int{0, 24, 48, 72} {
		geometry := bays[index].Institution.Boss
		t.Run(string(geometry.Encounter), func(t *testing.T) {
			world := combatWorld(t)
			if err := world.installBoss(ctx, geometry, campaign.RestorationPlan{ResetPartialBosses: true}, campaign.Snapshot{}); err != nil {
				t.Fatal(err)
			}
			for phase := 0; phase < 3; phase++ {
				world.boss.phase, world.boss.index = phase, 0
				world.boss.health = []int{420, 294, 147}[phase]
				world.boss.stage, world.boss.elapsed = BossTelegraph, 0
				world.resetBossRelays()
				order := world.boss.definition.Phases[phase].Attacks
				for attackIndex, id := range order {
					if world.boss.attack().ID != id || world.boss.index != attackIndex {
						t.Fatalf("phase %d attack %d: %+v", phase, attackIndex, world.Boss(ctx))
					}
					for _, stage := range []BossStage{BossTelegraph, BossActive, BossRecovery} {
						if world.boss.stage != stage {
							t.Fatalf("expected %s: %+v", stage, world.Boss(ctx))
						}
						ticks := world.boss.duration()
						for tick := 0; tick < ticks; tick++ {
							bossTick(t, world, PlayerCommand{})
						}
					}
				}
				if world.boss.index != 0 || world.boss.stage != BossTelegraph {
					t.Fatalf("order did not repeat: %+v", world.Boss(ctx))
				}
			}
		})
	}
}

func TestBossProtectionThresholdAndFeces(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world := bossFixture(t)
	bossTick(t, world, PlayerCommand{})
	bossTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}})
	for i := 1; i < 15; i++ {
		bossTick(t, world, PlayerCommand{})
	}
	if world.boss.health != 420 || len(world.boss.hits) != 1 || world.boss.hits[0].Result != "protected" {
		t.Fatalf("protected strike: %+v", world.Boss(ctx))
	}
	for world.boss.stage != BossRecovery {
		bossTick(t, world, PlayerCommand{})
	}
	// Boundary fixture: one ordinary intact strike crosses the threshold.
	world.boss.health = 315
	world.combat.projectiles = append(world.combat.projectiles, Projectile{ID: "old", Attacker: "ENC-HOS-05", Attack: "a", Position: Vector{X: 900, Y: 100}})
	bossTick(t, world, PlayerCommand{Edges: []PlayerEdge{release(settings.Melee), press(settings.Melee)}})
	for i := 1; i < 15; i++ {
		bossTick(t, world, PlayerCommand{})
	}
	if world.boss.health != 293 || world.boss.stage != BossTransition || world.boss.phase != 1 || len(world.combat.projectiles) != 0 || len(world.boss.locked) != 0 {
		t.Fatalf("threshold: %+v", world.Boss(ctx))
	}
	for i := 0; i < 119; i++ {
		bossTick(t, world, PlayerCommand{})
	}
	if world.boss.stage != BossTransition {
		t.Fatal("transition ended early")
	}
	bossTick(t, world, PlayerCommand{})
	if world.boss.stage != BossTelegraph || world.boss.attack().ID != "HOS-A" {
		t.Fatal("phase two did not start with its first attack")
	}
	for world.boss.stage != BossRecovery {
		bossTick(t, world, PlayerCommand{})
	}
	bossTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Throw)}})
	for i := 0; i < 30 && !world.boss.staggered; i++ {
		bossTick(t, world, PlayerCommand{})
	}
	if !world.boss.staggered || world.boss.health != 285 || world.boss.staggerUntil == 0 {
		t.Fatalf("feces impact: %+v", world.Boss(ctx))
	}
	until := world.boss.staggerUntil
	world.bossImpact("feces", 8, true)
	if world.boss.staggerUntil != until {
		t.Fatal("second feces extended stagger")
	}
}

func TestBossThrownImpactsRespectProtection(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		stage     BossStage
		feces     bool
		remaining int
		result    string
		stagger   bool
	}{
		{name: "armored limb", stage: BossTelegraph, remaining: 420, result: "protected"},
		{name: "exposed limb", stage: BossRecovery, remaining: 385, result: "exposed"},
		{name: "armored feces", stage: BossTelegraph, feces: true, remaining: 420, result: "protected"},
		{name: "exposed feces", stage: BossRecovery, feces: true, remaining: 404, result: "exposed", stagger: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, world := bossFixture(t)
			world.boss.stage, world.boss.recovery = tc.stage, 1
			if tc.feces {
				for i := 0; i < 2; i++ {
					world.combat.projectiles = append(world.combat.projectiles, Projectile{ID: fmt.Sprint(i), Attacker: "player", Attack: fmt.Sprint(i), Position: Vector{X: 599, Y: 30}, Velocity: Vector{X: 600}, PlayerOwned: true})
				}
			} else {
				world.limbs.generation = 1
				world.limbs.objects = []LimbObject{{ID: "leg", Socket: SocketLeftLeg, Generation: 1, Position: Vector{X: 599, Y: 30}, Velocity: Vector{X: 600}, Airborne: true, Damaging: true}}
			}
			bossTick(t, world, PlayerCommand{})
			view := world.Boss(ctx)
			if view.Health != tc.remaining || len(view.Hits) == 0 || view.Hits[0].Result != tc.result || (view.StaggerTicks > 0) != tc.stagger || view.Stage != tc.stage {
				t.Fatalf("impact: %+v", view)
			}
			if tc.stagger && (len(view.Hits) != 2 || view.StaggerTicks != 30) {
				t.Fatalf("feces did not share one bounded stagger: %+v", view)
			}
		})
	}
}

func TestBossLateStaggerIsFullAndBounded(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world := bossFixture(t)
	world.boss.stage, world.boss.recovery = BossRecovery, 1
	world.boss.elapsed = world.boss.duration() - 1
	for i := 0; i < 2; i++ {
		world.combat.projectiles = append(world.combat.projectiles, Projectile{ID: fmt.Sprint(i), Attacker: "player", Attack: fmt.Sprint(i), Position: Vector{X: 599, Y: 30}, Velocity: Vector{X: 600}, PlayerOwned: true})
	}
	bossTick(t, world, PlayerCommand{})
	if view := world.Boss(ctx); view.Stage != BossRecovery || view.Remaining != 29 || view.StaggerTicks != 30 {
		t.Fatalf("late stagger: %+v", view)
	}
	for i := 0; i < 29; i++ {
		bossTick(t, world, PlayerCommand{})
	}
	if view := world.Boss(ctx); view.Stage != BossTelegraph || view.StaggerTicks != 0 {
		t.Fatalf("stagger did not end: %+v", view)
	}
}

func TestBossRenewableIntactMelee(t *testing.T) {
	ctx := context.Background()
	_, _, _, committed, world := bossFixture(t)
	bossTick(t, world, PlayerCommand{})
	for tick := 0; tick < 6000 && world.boss.stage != BossPending; tick++ {
		player := PlayerCommand{}
		if world.boss.stage == BossRecovery && (world.motion.action == FreeAction || world.state.Tick >= world.motion.actionUntil) {
			player.Edges = []PlayerEdge{release(settings.Melee), press(settings.Melee)}
		}
		bossTick(t, world, player)
	}
	if world.boss.stage != BossPending || world.state.Health != 100 || world.state.Feces != 5 || world.motion.limbs != AllLimbs || len(world.state.Receipts) != committed.Credits() {
		t.Fatalf("intact melee did not finish the shared engine: %+v", world.Boss(ctx))
	}
}

func TestBossCrossesBothThresholdsInOneTick(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world := bossFixture(t)
	world.boss.stage, world.boss.health = BossRecovery, 315
	world.limbs.generation = 1
	for i := 0; i < 6; i++ {
		world.limbs.objects = append(world.limbs.objects, LimbObject{ID: fmt.Sprint(i), Socket: SocketLeftLeg, Generation: 1, Position: Vector{X: 599, Y: 30}, Velocity: Vector{X: 600}, Airborne: true, Damaging: true})
	}
	world.combat.projectiles = []Projectile{{ID: "boss", Attacker: "ENC-HOS-05", Position: Vector{X: 900, Y: 100}}, {ID: "other", Attacker: "other", Position: Vector{X: 900, Y: 100}}}
	bossTick(t, world, PlayerCommand{})
	if world.boss.health != 105 || world.boss.phase != 1 || world.boss.stage != BossTransition || len(world.combat.projectiles) != 1 || world.combat.projectiles[0].ID != "other" {
		t.Fatalf("first threshold: %+v, projectiles %+v", world.Boss(ctx), world.combat.projectiles)
	}
	for i := 0; i < 120; i++ {
		bossTick(t, world, PlayerCommand{})
	}
	if world.boss.phase != 2 || world.boss.stage != BossTransition || world.boss.elapsed != 0 {
		t.Fatalf("second transition: %+v", world.Boss(ctx))
	}
	for i := 0; i < 120; i++ {
		bossTick(t, world, PlayerCommand{})
	}
	if world.boss.stage != BossTelegraph || world.boss.attack().ID != "HOS-C" {
		t.Fatalf("phase three: %+v", world.Boss(ctx))
	}
}

func TestBossRelayProtectionAndPhaseReset(t *testing.T) {
	ctx := context.Background()
	_, _, bays := campaignFixture(t)
	world := combatWorld(t)
	if err := world.installBoss(ctx, bays[72].Institution.Boss, campaign.RestorationPlan{ResetPartialBosses: true}, campaign.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	world.boss.stage = BossRecovery
	world.boss.recovery = 1
	world.state.Player.Bounds.X = 238
	if !world.Boss(ctx).Protected || world.boss.relays[0].Health != 44 || !world.boss.relays[0].Required || world.boss.relays[1].Required {
		t.Fatalf("initial relays: %+v", world.Boss(ctx))
	}
	for strike := 0; strike < 2; strike++ {
		bossTick(t, world, PlayerCommand{Edges: []PlayerEdge{release(settings.Melee), press(settings.Melee)}})
		for i := 1; i < 55; i++ {
			bossTick(t, world, PlayerCommand{})
		}
	}
	if world.boss.relays[0].Health != 0 || world.Boss(ctx).Protected {
		t.Fatalf("relay not defeated by renewable melee: %+v", world.Boss(ctx))
	}
	world.beginBossTransition()
	if world.boss.relays[0].Required || !world.boss.relays[1].Required || world.boss.relays[1].Health != 44 {
		t.Fatalf("phase two relays: %+v", world.Boss(ctx))
	}
	world.beginBossTransition()
	if !world.boss.relays[0].Required || !world.boss.relays[1].Required || world.boss.relays[0].Health != 44 || world.boss.relays[1].Health != 44 {
		t.Fatalf("phase three relays: %+v", world.Boss(ctx))
	}
}

func TestInstituteUnfinishedAttemptRestoresFromCampaign(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, _ := instituteFixture(t)
	var err error
	committed, err = owner.Abandon(ctx, committed)
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range manifest.Batches(ctx)[12:16] {
		for _, id := range batch.Deeds {
			committed = certifyFixture(t, owner, manifest, committed, id)
		}
	}
	committed, err = owner.Checkpoint(ctx, committed, "ANC-INS-05")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	world.boss.phase, world.boss.health, world.boss.stage = 2, 20, BossTransition
	world.boss.locked = []Vector{{X: 400}}
	world.boss.relays[0].Health = 0
	world.boss.relays[1].Health = 3
	world.combat.projectiles = []Projectile{{ID: "attack", Attacker: "ENC-INS-05"}}
	world.combat.enemies = append(world.combat.enemies, Enemy{ID: "summon", Kind: FollowerEnemy, Health: 60})
	world.encounter.feed.queue = []RecruitmentReplacement{{ID: "queued", Remaining: 3}}
	world.encounter.elapsed = 123
	restored, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	view := restored.Boss(ctx)
	if view.Health != 420 || view.Phase != 1 || view.AttackIndex != 0 || view.Stage != BossDormant || len(view.LockedTargets) != 0 || len(restored.combat.projectiles) != 0 || len(restored.combat.enemies) != 0 || len(restored.encounter.feed.queue) != 0 || restored.encounter.elapsed != 0 || view.Relays[0].Health != 44 || !view.Relays[0].Required || view.Relays[1].Required || restored.state.Health != 100 || restored.state.Feces != 5 || restored.state.Limbs != 4 {
		t.Fatalf("partial institute attempt survived restore: %+v", view)
	}
}

func TestBossVictoryCommitAndRestoration(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world := bossFixture(t)
	// Construct a last-exposure fixture; the completing hit is real melee.
	world.boss.stage, world.boss.phase, world.boss.recovery, world.boss.health = BossRecovery, 2, 1, 22
	world.combat.projectiles = append(world.combat.projectiles, Projectile{ID: "old", Attacker: "ENC-HOS-05", Attack: "a", Position: Vector{X: 900, Y: 100}})
	bossTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}})
	for i := 1; i < 14; i++ {
		bossTick(t, world, PlayerCommand{})
	}
	bossTick(t, world, PlayerCommand{}, DamageHit{Attacker: "hazard", Attack: "lethal", Kind: LethalDamage, Amount: 1, Direction: 1})
	view := world.Boss(ctx)
	if view.Stage != BossPending || view.Pending == nil || view.Confirmed || world.state.Health != 0 || len(world.combat.projectiles) != 0 || !world.state.BossActive {
		t.Fatalf("uncommitted victory: %+v", view)
	}
	if err := world.StepBoss(ctx, ObjectCommand{}); err == nil {
		t.Fatal("pending victory did not freeze")
	}
	transition := campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1}
	proposed, err := world.PrepareBoss(ctx, owner, committed, transition)
	if err != nil {
		t.Fatal(err)
	}
	data := proposed.Data(ctx)
	if proposed.Credits() != committed.Credits() || !slices.Contains(data.Defeated, content.EncounterID("ENC-HOS-05")) || len(data.EncounterEvents) != 1 || len(data.Incidents) == 0 {
		t.Fatalf("ordered victory/death: %+v", data)
	}
	again, err := world.PrepareBoss(ctx, owner, committed, transition)
	if err != nil || !reflect.DeepEqual(again.Data(ctx), data) {
		t.Fatalf("unstable retry: %v", err)
	}
	if err = world.AcknowledgeBoss(ctx, owner, ServiceAcknowledgment{}); err == nil || world.Boss(ctx).Confirmed {
		t.Fatal("failed write confirmed victory")
	}
	if err = world.AcknowledgeBoss(ctx, owner, ServiceAcknowledgment{Succeeded: true, CampaignID: data.CampaignID, Sequence: data.OperationSequence + 1, Snapshot: proposed}); err == nil || world.Boss(ctx).Confirmed {
		t.Fatal("mismatched save confirmed victory")
	}
	if err = world.AcknowledgeBoss(ctx, owner, ServiceAcknowledgment{Succeeded: true, CampaignID: data.CampaignID, Sequence: data.OperationSequence, Snapshot: proposed}); err != nil {
		t.Fatal(err)
	}
	if !world.Boss(ctx).Confirmed || world.state.BossActive {
		t.Fatal("acknowledged victory not published")
	}
	restored, err := NewWorld(ctx, owner, manifest, proposed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Boss(ctx).Confirmed || restored.state.BossActive {
		t.Fatal("committed boss not restored")
	}
	terminal := false
	for _, entity := range restored.state.Entities {
		if entity.ID == "OBJ-HOS-END-TERMINAL" && entity.Protected {
			terminal = true
		}
	}
	if !terminal {
		t.Fatal("closure terminal was not restored")
	}
	bossTick(t, restored, PlayerCommand{})
	unfinished, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	if unfinished.boss.health != 420 || unfinished.boss.phase != 0 || unfinished.boss.stage != BossDormant || len(unfinished.combat.projectiles) != 0 {
		t.Fatalf("unfinished retry: %+v", unfinished.Boss(ctx))
	}
}

// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"reflect"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/settings"
)

func TestInstituteAttackCyclesAndWarnings(t *testing.T) {
	ctx := context.Background()
	orders := [][]string{{"INS-A", "INS-B"}, {"INS-C", "INS-A", "INS-B"}, {"INS-B", "INS-C", "INS-A"}}
	for phase, order := range orders {
		_, _, _, _, world, arena := finalBossFixture(t, true)
		world.state.Player.Bounds.X = 50
		world.boss.phase, world.boss.health, world.boss.stage = phase, []int{420, 294, 147}[phase], BossTelegraph
		world.resetBossRelays()
		for _, id := range order {
			attack := world.boss.attack()
			if attack.ID != id {
				t.Fatalf("phase %d: %+v", phase, world.Boss(ctx))
			}
			for n := 0; n < msTicks(attack.TelegraphMS)-1; n++ {
				arenaTick(t, arena, PlayerCommand{})
			}
			if world.boss.stage != BossTelegraph || len(arena.View(ctx).Danger) == 0 {
				t.Fatal("telegraph ended early or lacked marker")
			}
			arenaTick(t, arena, PlayerCommand{})
			locked := append([]Vector(nil), world.boss.locked...)
			if world.boss.stage != BossActive {
				t.Fatal("attack did not lock")
			}
			for n := 0; n < 150 && world.boss.stage == BossActive; n++ {
				arenaTick(t, arena, PlayerCommand{})
				if !reflect.DeepEqual(locked, world.boss.locked) {
					t.Fatal("attack retargeted")
				}
			}
			if view := arena.View(ctx); view.Boss.Stage != BossRecovery || !view.Boss.Protected || len(view.ProtectedRelays) == 0 || view.Boss.Bounds.Y != 0 {
				t.Fatalf("relay-protected descending cage: %+v", view)
			}
			for n := 0; n < msTicks(attack.RecoveryMS)-1; n++ {
				arenaTick(t, arena, PlayerCommand{})
			}
			if world.boss.stage != BossRecovery {
				t.Fatal("recovery ended early")
			}
			arenaTick(t, arena, PlayerCommand{})
		}
		if world.boss.index != 0 || world.state.Health != 100 {
			t.Fatalf("cycle or safe alcove: %+v", arena.View(ctx))
		}
	}
}

func TestInstituteBinderPathsAndBeamLevels(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world, arena := finalBossFixture(t, true)
	world.state.Player.Bounds.X = 548
	beginLocalAttack(t, arena, 1, 0)
	view := arena.View(ctx)
	if len(view.Danger) != 2 || view.Danger[1].X-(view.Danger[0].X+view.Danger[0].Width) < 160 {
		t.Fatalf("no uncovered floor: %+v", view.Danger)
	}
	arenaTick(t, arena, PlayerCommand{})
	if len(arena.View(ctx).Projectiles) != 2 {
		t.Fatal("two binder trajectories did not release")
	}
	for _, shot := range arena.View(ctx).Projectiles {
		if shot.Velocity.Y >= 0 {
			t.Fatalf("wrong trajectory: %+v", shot)
		}
	}
	floor := instituteMarkers(world.boss.definition.Attacks[0], Rect{X: 500, Y: 0, Width: 28, Height: 58})
	upper := instituteMarkers(world.boss.definition.Attacks[0], Rect{X: 500, Y: 80, Width: 28, Height: 58})
	if floor[0].Y != 0 || upper[0].Y != 80 || overlaps(instituteWarnings(world.boss.definition.Attacks[0], floor)[0], Rect{X: 500, Y: 80, Width: 28, Height: 58}) {
		t.Fatal("beam targeted both levels")
	}
}

func TestInstituteAttackContactsAndStageGeometry(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		phase, index, damage int
	}{
		{phase: 0, index: 0, damage: 24},
		{phase: 0, index: 1, damage: 35},
		{phase: 1, index: 0, damage: 24},
	} {
		_, _, bays, _, world, arena := finalBossFixture(t, true)
		geometry := bays[72].Institution.Boss
		if len(geometry.Platforms) != 4 || len(geometry.Jumps) != 4 || geometry.Platforms[0].Y+geometry.Platforms[0].Height != 80 || geometry.Platforms[1].Y+geometry.Platforms[1].Height != 160 || geometry.Platforms[2].Y+geometry.Platforms[2].Height != 80 || geometry.Platforms[3].Y+geometry.Platforms[3].Height != 160 || geometry.Floor.Width < 1280 {
			t.Fatalf("stage geometry: %+v", geometry)
		}
		world.boss.phase, world.boss.index, world.boss.stage = tc.phase, tc.index, BossTelegraph
		world.boss.health = []int{420, 294, 147}[tc.phase]
		tick := func() {
			t.Helper()
			command := ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Visible: Rect{X: -220, Y: 0, Width: 20, Height: 100}}}}}
			if err := arena.Step(ctx, command); err != nil {
				t.Fatal(err)
			}
		}
		for world.boss.stage == BossTelegraph {
			tick()
		}
		if world.boss.attack().ID == "INS-C" {
			// Walk into the right marked trajectory after it has locked; that
			// trajectory is not sheltered by either tower.
			world.state.Player.Bounds.X = world.boss.locked[1].X - PlayerWidth/2
			world.state.Player.Velocity = Vector{}
		}
		found := false
		for n := 0; n < 150 && world.boss.stage == BossActive; n++ {
			tick()
			for _, outcome := range world.CombatView(ctx).Outcomes {
				if outcome.Hit.Attacker == "ENC-INS-05" && outcome.Hit.Amount == tc.damage && outcome.Result == HitAccepted {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("attack %s did not hit: %+v", world.boss.attack().ID, arena.View(ctx))
		}
	}
}

func TestInstituteRelayMeleeAndTransitions(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world, arena := finalBossFixture(t, true)
	world.state.Player.Bounds.X = 238
	world.boss.stage = BossRecovery
	world.boss.recovery = 1
	for strike := 0; strike < 2; strike++ {
		arenaTick(t, arena, PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{release(settings.Melee), press(settings.Melee)}})
		for i := 1; i < 55; i++ {
			arenaTick(t, arena, PlayerCommand{})
		}
	}
	if world.boss.relays[0].Health != 0 || world.Boss(ctx).Protected || len(arena.View(ctx).ProtectedRelays) != 0 {
		t.Fatalf("relay A: %+v", arena.View(ctx))
	}
	world.boss.health = 293
	arenaTick(t, arena, PlayerCommand{})
	if view := arena.View(ctx); view.Boss.Stage != BossTransition || view.Boss.Relays[0].Required || !view.Boss.Relays[1].Required || view.Boss.Relays[1].Health != 44 || len(view.ProtectedRelays) != 1 || view.ProtectedRelays[0] != "Реле Б" {
		t.Fatalf("phase two: %+v", view)
	}
	for i := 0; i < 119; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	if world.boss.stage != BossTransition {
		t.Fatal("transition ended early")
	}
	arenaTick(t, arena, PlayerCommand{})
	if world.boss.stage != BossTelegraph || world.boss.attack().ID != "INS-C" {
		t.Fatalf("phase order: %+v", world.Boss(ctx))
	}
	world.boss.health = 147
	arenaTick(t, arena, PlayerCommand{})
	if view := arena.View(ctx); view.Boss.Stage != BossTransition || view.Boss.Relays[0].Health != 44 || view.Boss.Relays[1].Health != 44 || len(view.ProtectedRelays) != 2 {
		t.Fatalf("phase three restore: %+v", view)
	}
}

func TestInstituteThrownLimbAndProtectedBoss(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world, arena := finalBossFixture(t, true)
	world.boss.stage = BossRecovery
	world.boss.recovery = 1
	world.limbs.generation = 1
	for i, socket := range []SocketID{SocketLeftArm, SocketRightArm} {
		world.limbs.objects = append(world.limbs.objects, LimbObject{ID: string(socket), Socket: socket, Generation: 1, Position: Vector{X: 287 - float64(i), Y: 30}, Velocity: Vector{X: 600}, Airborne: true, Damaging: true})
	}
	arenaTick(t, arena, PlayerCommand{})
	if world.boss.relays[0].Health != 0 || world.Boss(ctx).Protected {
		t.Fatalf("limbs failed to disable relay: %+v", world.Boss(ctx))
	}
	world.boss.relays[0].Health = 44
	world.state.Player.Bounds.X = 548
	arenaTick(t, arena, PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Melee)}})
	for i := 1; i < 15; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	if world.boss.health != 420 || len(world.boss.hits) != 1 || world.boss.hits[0].Result != "protected" {
		t.Fatalf("relay did not reject hit: %+v", world.Boss(ctx))
	}
}

func TestInstituteReplacementWarningAndCancellation(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world, arena := finalBossFixture(t, true)
	world.state.Player.Bounds.X = 50
	world.boss.stage = BossTelegraph
	arenaTick(t, arena, PlayerCommand{})
	if arena.View(ctx).Followers != 2 {
		t.Fatalf("initial cap: %+v", arena.View(ctx))
	}
	world.combat.enemies[0].Health = 0
	world.combat.enemies[0].Phase = EnemyDefeated
	arenaTick(t, arena, PlayerCommand{})
	view := arena.View(ctx)
	if len(view.FollowerQueue) != 1 || view.FollowerQueue[0].Remaining != 540 || view.FollowerQueue[0].Warning {
		t.Fatalf("replacement queue: %+v", view)
	}
	for i := 0; i < 359; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	if view = arena.View(ctx); view.FollowerQueue[0].Remaining != 181 || view.FollowerQueue[0].Warning {
		t.Fatalf("early warning: %+v", view)
	}
	arenaTick(t, arena, PlayerCommand{})
	if view = arena.View(ctx); !view.FollowerQueue[0].Warning || view.FollowerQueue[0].Remaining != 180 {
		t.Fatalf("missing 1.5s warning: %+v", view)
	}
	for i := 0; i < 180; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	if view = arena.View(ctx); view.Followers != 2 || len(view.FollowerQueue) != 0 {
		t.Fatalf("replacement: %+v", view)
	}
	for i := range world.combat.enemies {
		if world.combat.enemies[i].Health > 0 {
			world.combat.enemies[i].Health, world.combat.enemies[i].Phase = 0, EnemyDefeated
			break
		}
	}
	arenaTick(t, arena, PlayerCommand{})
	if len(arena.View(ctx).FollowerQueue) != 1 {
		t.Fatal("second defeat did not queue")
	}
	world.boss.relays[0].Health = 0
	arenaTick(t, arena, PlayerCommand{})
	if view = arena.View(ctx); len(view.FollowerQueue) != 0 || view.Followers != 1 {
		t.Fatalf("cancellation or living follower: %+v", view)
	}
}

func TestInstituteIntactMeleeCompletion(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world, arena := finalBossFixture(t, true)
	jumped := false
	for tick := 0; tick < 36000 && world.boss.stage != BossPending; tick++ {
		view := world.Boss(ctx)
		body := world.state.Player.Bounds
		player := PlayerCommand{Aim: Vector{X: 1}}
		if view.Stage == BossTelegraph && view.Elapsed == 0 {
			jumped = false
		}
		target := view.Bounds.X - 50
		for _, relay := range view.Relays {
			if relay.Required && relay.Health > 0 {
				target = relay.Bounds.X - 50
				break
			}
		}
		if body.X < target-7 {
			player.Move = 1
		} else if body.X > target+7 {
			player.Move = -1
		} else if world.motion.action == FreeAction || world.state.Tick >= world.motion.actionUntil {
			player.Edges = []PlayerEdge{release(settings.Melee), press(settings.Melee)}
		}
		if view.Stage == BossActive && view.Attack.ID == "INS-A" && !jumped && world.state.Player.Support != "" {
			player.Edges = []PlayerEdge{release(settings.Jump), press(settings.Jump)}
			jumped = true
		}
		if view.Stage == BossActive && view.Attack.ID == "INS-B" && len(view.LockedTargets) > 0 {
			if body.X < view.LockedTargets[0].X {
				player.Move = -1
			} else {
				player.Move = 1
			}
		}
		for _, enemy := range world.combat.enemies {
			if enemy.Health > 0 && enemy.Phase == EnemyTelegraph && enemy.Until <= world.state.Tick+15 && world.state.Player.Support != "" {
				player.Edges = []PlayerEdge{release(settings.Jump), press(settings.Jump)}
				break
			}
		}
		priorHealth := world.state.Health
		arenaTick(t, arena, player)
		if world.state.Health < priorHealth {
			t.Logf("damage at %d: health %d, boss %s/%s, player %+v, hits %+v", tick, world.state.Health, view.Attack.ID, view.Stage, body, world.CombatView(ctx).Outcomes)
		}
		if world.motion.dead {
			t.Fatalf("intact player died at tick %d: %+v, enemies %+v", tick, arena.View(ctx), world.CombatView(ctx).Enemies)
		}
	}
	if world.boss.stage != BossPending || world.state.Feces != 5 || world.motion.limbs != AllLimbs || len(world.limbs.objects) != 0 {
		t.Fatalf("intact melee: %+v", arena.View(ctx))
	}
}

func TestInstituteVictoryNoCreditAndRetry(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world, arena := finalBossFixture(t, true)
	if committed.Credits() != 99 {
		t.Fatalf("expected 99 credits: %d", committed.Credits())
	}
	world.boss.stage, world.boss.phase, world.boss.health = BossRecovery, 2, 22
	world.resetBossRelays()
	for i := range world.boss.relays {
		world.boss.relays[i].Health = 0
	}
	arenaTick(t, arena, PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}})
	for i := 1; i < 15; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	view := arena.View(ctx)
	if view.Boss.Stage != BossPending || view.Boss.Confirmed || !view.SummonsHarmless || view.Followers != 0 || view.BroadcastStopped || view.FinalPanelAccessible {
		t.Fatalf("pending victory: %+v", view)
	}
	proposal, err := world.PrepareBoss(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
	if err != nil || proposal.Credits() != 99 {
		t.Fatalf("victory granted a credit: %v", err)
	}
	if err = world.AcknowledgeBoss(ctx, owner, ServiceAcknowledgment{}); err == nil || arena.View(ctx).Survived {
		t.Fatal("unacknowledged victory confirmed")
	}
	retry, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := NewBossArena(ctx, retry)
	if err != nil || fresh.View(ctx).Boss.Health != 420 || fresh.View(ctx).Boss.Stage != BossDormant || fresh.View(ctx).Boss.Relays[0].Health != 44 || fresh.View(ctx).Followers != 0 || len(fresh.View(ctx).FollowerQueue) != 0 {
		t.Fatalf("retry: %+v %v", fresh.View(ctx), err)
	}
	if err = world.AcknowledgeBoss(ctx, owner, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	if view = arena.View(ctx); !view.Survived || !view.BroadcastStopped || !view.FinalPanelAccessible || !view.SummonsHarmless || view.Followers != 0 {
		t.Fatalf("acknowledged outcome: %+v", view)
	}
	restored, err := NewWorld(ctx, owner, manifest, proposal, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	restoredArena, err := NewBossArena(ctx, restored)
	if err != nil || !restoredArena.View(ctx).BroadcastStopped || !restoredArena.View(ctx).FinalPanelAccessible || len(restoredArena.View(ctx).FollowerQueue) != 0 {
		t.Fatalf("committed outcome not restored: %v", err)
	}
}

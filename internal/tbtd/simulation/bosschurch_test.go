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

func TestChurchAuthoredPresentation(t *testing.T) {
	testBossPresentation(t, true, "content/data/bosses/church.json", "CHR-SAV", "Отец Савва")
}

func TestChurchIntactMeleeCompletion(t *testing.T) {
	_, _, _, _, world, arena := localBossFixture(t, true)
	world.state.Player.Bounds.X = -134 // the authored pre-fight checkpoint
	world.state.Player.Support = "boundary-floor"
	finishWithMelee(t, arena)
}

func TestChurchFirstWarningFrame(t *testing.T) {
	testFirstWarningFrame(t, true)
}

func TestChurchArenaCycles(t *testing.T) {
	testArenaCycles(t, true, [][]string{{"CHU-A", "CHU-B"}, {"CHU-C", "CHU-A", "CHU-B"}, {"CHU-B", "CHU-C", "CHU-A"}})
}

func TestChurchPendulumJump(t *testing.T) {
	testSweepJump(t, true)
}

func TestChurchConfessionJump(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world, arena := localBossFixture(t, true)
	beginLocalAttack(t, arena, 1, 0)
	jumped := false
	for n := 0; n < 436; n++ {
		player := PlayerCommand{}
		for _, shot := range arena.View(ctx).Projectiles {
			if !jumped && shot.Position.Y == 20 && shot.Position.X < world.state.Player.Bounds.X && world.state.Player.Bounds.X-shot.Position.X < 70 {
				player.Edges = []PlayerEdge{press(settings.Jump)}
				jumped = true
			}
		}
		if jumped && world.state.Player.Bounds.Y > 50 {
			player.Edges = []PlayerEdge{release(settings.Jump)}
		}
		arenaTick(t, arena, player)
	}
	if !jumped || world.state.Health != 100 || world.boss.stage != BossRecovery {
		t.Fatalf("could not avoid confession sequence: health %d", world.state.Health)
	}
}

func TestChurchAttackSafeAreas(t *testing.T) {
	testArenaSafeAreas(t, true, [][2]int{{0, 0}, {0, 1}, {1, 0}})
}

func TestChurchLampEscapeAfterLock(t *testing.T) {
	_, _, _, _, world, arena := localBossFixture(t, true)
	beginLocalAttack(t, arena, 0, 1)
	locked := append([]Vector(nil), world.boss.locked...)
	for i := 0; i < 47; i++ {
		arenaTick(t, arena, PlayerCommand{Move: 1})
	}
	if world.boss.stage != BossActive || world.state.Health != 100 || !reflect.DeepEqual(locked, world.boss.locked) {
		t.Fatal("lamp struck early or followed the player")
	}
	arenaTick(t, arena, PlayerCommand{Move: 1})
	if world.boss.stage != BossRecovery || world.state.Health != 100 {
		t.Fatalf("could not leave locked lane: %+v", arena.View(context.Background()))
	}
}

func TestChurchWarningsAndImpacts(t *testing.T) {
	for _, tc := range []struct {
		id       string
		phase    int
		index    int
		damage   int
		recovery int
	}{
		{id: "CHU-A", phase: 0, index: 0, damage: 24, recovery: 180},
		{id: "CHU-B", phase: 0, index: 1, damage: 35, recovery: 216},
		{id: "CHU-C", phase: 1, index: 0, damage: 24, recovery: 192},
	} {
		t.Run(tc.id, func(t *testing.T) {
			_, _, _, _, world, arena := localBossFixture(t, true)
			beginLocalAttack(t, arena, tc.phase, tc.index)
			locked := append([]Vector(nil), world.boss.locked...)
			if world.boss.attack().ID != tc.id || !arena.View(context.Background()).Boss.Protected {
				t.Fatal("wrong attack or missing protection")
			}
			for tick := 0; tick < 450 && world.boss.stage == BossActive; tick++ {
				arenaTick(t, arena, PlayerCommand{})
				if !reflect.DeepEqual(world.boss.locked, locked) {
					t.Fatal("active attack retargeted")
				}
			}
			if world.boss.stage != BossRecovery || world.state.Health != 100-tc.damage || world.boss.duration() != tc.recovery || world.Boss(context.Background()).Protected {
				t.Fatalf("attack result: health %d boss %+v", world.state.Health, world.Boss(context.Background()))
			}
		})
	}
}

func TestConfessionShotsEndAtProtectedBoundaries(t *testing.T) {
	ctx := context.Background()
	for _, x := range []float64{300, 950} {
		_, _, _, _, world, arena := localBossFixture(t, true)
		world.state.Player.Bounds.X = x
		beginLocalAttack(t, arena, 1, 0)
		world.state.Player.Bounds.X = 50 // observe from the protected alcove
		for n := 0; n < 436; n++ {
			arenaTick(t, arena, PlayerCommand{})
			for _, shot := range arena.View(ctx).Projectiles {
				if shot.Position.X <= 112 || shot.Position.X >= 1168 {
					t.Fatalf("projectile crossed arena boundary: %+v", shot)
				}
			}
		}
		if world.boss.stage != BossRecovery || len(arena.View(ctx).Projectiles) != 0 || world.state.Health != 100 {
			t.Fatalf("shots survived recovery boundary: %+v", arena.View(ctx))
		}
	}
}

func TestConfessionReleaseOrderAndInterrupt(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world, arena := localBossFixture(t, true)
	world.state.Player.Bounds.X = 1000
	beginLocalAttack(t, arena, 1, 0)
	// Move out of the committed paths while inspecting the release cadence.
	world.state.Player.Bounds.X = 300
	view := arena.View(ctx)
	if len(view.Danger) != 3 || view.Boss.LockedTargets[0].Y != 20 || view.Boss.LockedTargets[1].Y != 100 || view.Boss.LockedTargets[2].Y != 180 {
		t.Fatalf("sequence warning: %+v", view)
	}
	for tick := 0; tick <= 84; tick++ {
		arenaTick(t, arena, PlayerCommand{})
		want := 1 + tick/42
		if got := len(arena.View(ctx).Projectiles); got != want {
			t.Fatalf("tick %d: got %d projectiles, want %d", tick, got, want)
		}
	}
	shots := arena.View(ctx).Projectiles
	for i, height := range []float64{20, 100, 180} {
		if shots[i].Position.Y != height || shots[i].Velocity.X != -360 {
			t.Fatalf("projectile %d: %+v", i, shots[i])
		}
	}
	shots[0].Position.X = 9999
	if arena.View(ctx).Projectiles[0].Position.X == 9999 {
		t.Fatal("view aliases projectiles")
	}
	// A genuine threshold strike interrupts all obsolete attack state.
	world.state.Player.Bounds.X = 548
	world.state.Player.Velocity = Vector{}
	world.state.Player.Support = "boundary-floor"
	world.boss.stage, world.boss.phase, world.boss.health, world.boss.elapsed = BossRecovery, 1, 169, 0
	arenaTick(t, arena, PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}})
	for i := 1; i < 15; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	if world.boss.stage != BossTransition || world.boss.health != 147 || len(arena.View(ctx).Projectiles) != 0 || len(world.boss.locked) != 0 {
		t.Fatalf("threshold did not interrupt: %+v", arena.View(ctx))
	}
	for i := 0; i < 119; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	if world.boss.stage != BossTransition || !world.Boss(ctx).Protected {
		t.Fatal("transition ended early")
	}
	arenaTick(t, arena, PlayerCommand{})
	if world.boss.stage != BossTelegraph || world.boss.attack().ID != "CHU-B" || arena.View(ctx).Screen != "paid_scandal_advertisements" {
		t.Fatalf("phase three: %+v", arena.View(ctx))
	}
}

func TestChurchAcknowledgedSurvivorAndRetry(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world, arena := localBossFixture(t, true)
	world.boss.stage, world.boss.phase, world.boss.health = BossRecovery, 2, 22
	arenaTick(t, arena, PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}})
	for i := 1; i < 15; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	if world.boss.stage != BossPending || arena.View(ctx).ArchiveOpen || arena.View(ctx).KioskAccessible {
		t.Fatalf("pending was presented as saved: %+v", arena.View(ctx))
	}
	if err := arena.Step(ctx, ObjectCommand{}); err == nil {
		t.Fatal("pending arena advanced")
	}
	proposal, err := world.PrepareBoss(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
	if err != nil || proposal.Credits() != committed.Credits() {
		t.Fatalf("victory proposal: %v", err)
	}
	if err = world.AcknowledgeBoss(ctx, owner, ServiceAcknowledgment{}); err == nil || arena.View(ctx).Survived {
		t.Fatal("unacknowledged result was presented as victory")
	}
	if err = world.AcknowledgeBoss(ctx, owner, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	if view := arena.View(ctx); !view.Survived || !view.ArchiveOpen || !view.KioskAccessible || view.DriveStopped {
		t.Fatalf("aftermath: %+v", view)
	}
	restored, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := NewBossArena(ctx, restored)
	if err != nil || fresh.View(ctx).Boss.Health != 420 || fresh.View(ctx).Boss.Stage != BossDormant || fresh.View(ctx).ArchiveOpen {
		t.Fatalf("unfinished attempt survived retry: %+v %v", fresh.View(ctx), err)
	}
	finished, err := NewWorld(ctx, owner, manifest, proposal, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	finishedArena, err := NewBossArena(ctx, finished)
	if err != nil || !finishedArena.View(ctx).ArchiveOpen || !finishedArena.View(ctx).KioskAccessible {
		t.Fatalf("saved victory not restored: %+v %v", finishedArena.View(ctx), err)
	}
}

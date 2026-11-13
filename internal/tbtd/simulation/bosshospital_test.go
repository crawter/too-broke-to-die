// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

func localBossFixture(t *testing.T, church bool) (*campaign.Campaign, *content.Campaign, []content.ServiceBay, campaign.Snapshot, *World, *BossArena) {
	t.Helper()
	ctx := context.Background()
	owner, manifest, bays, committed, world := bossFixture(t)
	if church {
		var err error
		committed, err = owner.DefeatBoss(ctx, committed, "ENC-HOS-05")
		if err != nil {
			t.Fatal(err)
		}
		committed = certifyFixture(t, owner, manifest, committed, "D-HOS-END")
		for _, batch := range manifest.Batches(ctx)[4:8] {
			for _, deed := range batch.Deeds {
				committed = certifyFixture(t, owner, manifest, committed, deed)
			}
		}
		committed, err = owner.Checkpoint(ctx, committed, "ANC-CHU-05")
		if err != nil {
			t.Fatal(err)
		}
		world, err = NewWorld(ctx, owner, manifest, committed, bays, nil)
		if err != nil {
			t.Fatal(err)
		}
		world.state.Player.Bounds.X = 548
		world.state.Player.Support = "boundary-floor"
	}
	arena, err := NewBossArena(ctx, world)
	if err != nil {
		t.Fatal(err)
	}
	return owner, manifest, bays, committed, world, arena
}

func arenaTick(t *testing.T, arena *BossArena, player PlayerCommand) {
	t.Helper()
	command := ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: player, Visible: Rect{X: -240, Y: -8, Width: 1760, Height: 568}}}}}
	if err := arena.Step(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func beginLocalAttack(t *testing.T, arena *BossArena, phase, index int) {
	t.Helper()
	world := arena.world
	world.boss.phase, world.boss.index, world.boss.stage, world.boss.elapsed = phase, index, BossTelegraph, 0
	world.boss.health = []int{420, 294, 147}[phase]
	for tick := 0; tick < world.boss.duration(); tick++ {
		arenaTick(t, arena, PlayerCommand{})
		if world.boss.stage == BossActive {
			break
		}
	}
	if world.boss.stage != BossActive {
		t.Fatalf("telegraph did not complete: %+v", world.Boss(context.Background()))
	}
}

func testBossPresentation(t *testing.T, church bool, path, actor, name string) {
	t.Helper()
	ctx := context.Background()
	filesystem := os.DirFS("../../..")
	presentation, err := LoadBossPresentation(ctx, filesystem, path)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, world, arena := localBossFixture(t, church)
	if presentation.Encounter != world.Boss(ctx).Encounter || presentation.Actor != actor || presentation.Name != name || !presentation.Adult || presentation.MediaStatus != "missing" {
		t.Fatalf("unexpected authored record: %+v", presentation)
	}
	if err = arena.SetPresentation(ctx, presentation); err != nil {
		t.Fatal(err)
	}
	view := arena.View(ctx)
	if view.Presentation == nil || *view.Presentation != presentation {
		t.Fatal("presentation not bound to arena")
	}
	view.Presentation.Name = "altered"
	if arena.View(ctx).Presentation.Name != name {
		t.Fatal("presentation view aliases arena")
	}
	other := presentation
	if church {
		other.Encounter = "ENC-HOS-05"
	} else {
		other.Encounter = "ENC-CHU-05"
	}
	if err = arena.SetPresentation(ctx, other); err == nil {
		t.Fatal("accepted a mismatched encounter")
	}
	data, err := fs.ReadFile(filesystem, path)
	if err != nil {
		t.Fatal(err)
	}
	original := string(data)
	for _, invalid := range []string{
		strings.Replace(original, `"schema_version": 1`, `"schema_version": 1, "schema_version": 1`, 1),
		strings.Replace(original, `"adult": true`, `"adult": false`, 1),
		strings.Replace(original, `"media_status": "missing"`, `"media_status": "final"`, 1),
		strings.Replace(original, `"actor":`, `"unknown":`, 1),
		original + `{}`,
		original + string([]byte{0xff}),
		strings.Replace(original, name, "\\ud800", 1),
	} {
		fixture := fstest.MapFS{"boss.json": {Data: []byte(invalid)}}
		if _, err = LoadBossPresentation(ctx, fixture, "boss.json"); err == nil {
			t.Fatalf("accepted invalid presentation %q", invalid)
		}
	}
	if _, err = LoadBossPresentation(ctx, filesystem, "../boss.json"); err == nil {
		t.Fatal("accepted unsafe path")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = LoadBossPresentation(cancelled, filesystem, path); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestHospitalAuthoredPresentation(t *testing.T) {
	testBossPresentation(t, false, "content/data/bosses/hospital.json", "CHR-POT", "Ренат Потрошин")
}

func TestHospitalAttackWarningsAndDamage(t *testing.T) {
	for _, tc := range []struct {
		id       string
		phase    int
		index    int
		damage   int
		markers  int
		recovery int
	}{
		{id: "HOS-A", phase: 0, index: 0, damage: 24, markers: 1, recovery: 168},
		{id: "HOS-B", phase: 0, index: 1, damage: 35, markers: 1, recovery: 216},
		{id: "HOS-C", phase: 1, index: 1, damage: 24, markers: 3, recovery: 192},
	} {
		t.Run(tc.id, func(t *testing.T) {
			_, _, _, _, world, arena := localBossFixture(t, false)
			beginLocalAttack(t, arena, tc.phase, tc.index)
			view := arena.View(context.Background())
			if view.Boss.Attack.ID != tc.id || len(view.Boss.LockedTargets) != tc.markers || len(view.Danger) != tc.markers || !view.Boss.Protected {
				t.Fatalf("attack lock: %+v", view)
			}
			locked := append([]Vector(nil), view.Boss.LockedTargets...)
			for ticks := 0; ticks < 360 && world.boss.stage == BossActive; ticks++ {
				arenaTick(t, arena, PlayerCommand{})
				if !reflect.DeepEqual(world.boss.locked, locked) {
					t.Fatal("active target followed player")
				}
			}
			if world.boss.stage != BossRecovery || world.state.Health != 100-tc.damage || world.boss.duration() != tc.recovery || world.Boss(context.Background()).Protected {
				t.Fatalf("attack did not resolve: health %d boss %+v", world.state.Health, world.Boss(context.Background()))
			}
		})
	}
}

// A complete cycle is exercised through the arena executor, not merely the
// shared phase machine. The protected alcove is used as a stationary observer.
func testArenaCycles(t *testing.T, church bool, orders [][]string) {
	t.Helper()
	ctx := context.Background()
	for phase, order := range orders {
		_, _, _, _, world, arena := localBossFixture(t, church)
		world.state.Player.Bounds.X = 50
		world.boss.phase, world.boss.index, world.boss.health = phase, 0, []int{420, 294, 147}[phase]
		world.boss.stage, world.boss.elapsed = BossTelegraph, 0
		for _, id := range order {
			view := arena.View(ctx)
			if view.Boss.Attack.ID != id || view.Boss.Stage != BossTelegraph || !view.Boss.Protected {
				t.Fatalf("phase %d: %+v", phase+1, view)
			}
			for n := 0; n < msTicks(view.Boss.Attack.TelegraphMS)-1; n++ {
				arenaTick(t, arena, PlayerCommand{})
			}
			if world.boss.stage != BossTelegraph || len(arena.View(ctx).Danger) == 0 {
				t.Fatalf("%s warning ended early or marker absent", id)
			}
			arenaTick(t, arena, PlayerCommand{})
			if world.boss.stage != BossActive || !world.Boss(ctx).Protected {
				t.Fatalf("%s did not lock", id)
			}
			locked := append([]Vector(nil), world.boss.locked...)
			for n := 0; n < 500 && world.boss.stage == BossActive; n++ {
				arenaTick(t, arena, PlayerCommand{})
				if !reflect.DeepEqual(locked, world.boss.locked) {
					t.Fatalf("%s retargeted", id)
				}
			}
			if world.boss.stage != BossRecovery || world.Boss(ctx).Protected {
				t.Fatalf("%s did not expose", id)
			}
			for n := 0; n < msTicks(view.Boss.Attack.RecoveryMS)-1; n++ {
				arenaTick(t, arena, PlayerCommand{})
			}
			if world.boss.stage != BossRecovery {
				t.Fatalf("%s recovery ended early", id)
			}
			arenaTick(t, arena, PlayerCommand{})
		}
		if world.boss.index != 0 || world.boss.stage != BossTelegraph || world.state.Health != 100 {
			t.Fatalf("cycle or alcove safety: %+v", arena.View(ctx))
		}
	}
}

func testFirstWarningFrame(t *testing.T, church bool) {
	t.Helper()
	ctx := context.Background()
	_, _, _, _, _, arena := localBossFixture(t, church)
	arenaTick(t, arena, PlayerCommand{})
	if view := arena.View(ctx); view.Boss.Stage != BossTelegraph || view.Boss.Elapsed != 0 || len(view.Danger) == 0 {
		t.Fatalf("entry warning missing: %+v", view)
	}
	for i := 0; i < 1000; i++ {
		arenaTick(t, arena, PlayerCommand{})
		view := arena.View(ctx)
		if view.Boss.Stage == BossTelegraph && view.Boss.AttackIndex == 1 {
			if view.Boss.Elapsed != 0 || len(view.Danger) == 0 {
				t.Fatalf("next warning missing: %+v", view)
			}
			return
		}
	}
	t.Fatal("next attack not reached")
}

func TestHospitalFirstWarningFrame(t *testing.T) {
	testFirstWarningFrame(t, false)
}

func TestHospitalArenaCycles(t *testing.T) {
	testArenaCycles(t, false, [][]string{{"HOS-A", "HOS-B"}, {"HOS-A", "HOS-C", "HOS-B"}, {"HOS-C", "HOS-B", "HOS-A"}})
}

func testArenaSafeAreas(t *testing.T, church bool, attacks [][2]int) {
	t.Helper()
	for _, point := range []float64{-120, 64, 1216} {
		for _, attack := range attacks {
			_, _, _, _, world, arena := localBossFixture(t, church)
			beginLocalAttack(t, arena, attack[0], attack[1])
			world.state.Player.Bounds.X = point - PlayerWidth/2
			world.state.Player.Velocity = Vector{}
			world.state.Player.Support = "boundary-floor"
			for n := 0; n < 500 && world.boss.stage == BossActive; n++ {
				for _, danger := range arena.View(context.Background()).Danger {
					for _, safe := range world.combat.protection {
						if overlaps(danger, safe) {
							t.Fatalf("attack %v entered protected area: %+v", attack, danger)
						}
					}
				}
				arenaTick(t, arena, PlayerCommand{})
			}
			if world.state.Health != 100 || world.boss.stage != BossRecovery {
				t.Fatalf("safe point %.0f attack %v: health %d, %+v", point, attack, world.state.Health, arena.View(context.Background()))
			}
		}
	}
}

func TestHospitalAttackSafeAreas(t *testing.T) {
	testArenaSafeAreas(t, false, [][2]int{{0, 0}, {0, 1}, {1, 1}})
}

func testSweepJump(t *testing.T, church bool) {
	t.Helper()
	_, _, _, _, world, arena := localBossFixture(t, church)
	world.boss.stage, world.boss.elapsed = BossTelegraph, 0
	for n := 0; n < 84; n++ {
		player := PlayerCommand{}
		if n == 64 {
			player.Edges = []PlayerEdge{press(settings.Jump)}
		}
		arenaTick(t, arena, player)
	}
	if world.boss.stage != BossActive || world.state.Player.Bounds.Y <= 48 {
		t.Fatalf("jump did not clear marked sweep: %+v", world.state.Player)
	}
	arenaTick(t, arena, PlayerCommand{})
	if world.state.Health != 100 || world.boss.stage != BossRecovery {
		t.Fatal("sweep damaged a player above its marked floor area")
	}
}

func TestHospitalBoomJump(t *testing.T) {
	testSweepJump(t, false)
}

func TestHospitalTrolleyJump(t *testing.T) {
	_, _, _, _, world, arena := localBossFixture(t, false)
	beginLocalAttack(t, arena, 0, 1)
	jumped := false
	for n := 0; n < 338; n++ {
		player := PlayerCommand{}
		cart := hospitalTrolley(world.boss.locked[0], world.boss.elapsed)
		if !jumped && world.state.Player.Bounds.X-(cart.X+cart.Width) < 90 {
			player.Edges = []PlayerEdge{press(settings.Jump)}
			jumped = true
		}
		arenaTick(t, arena, player)
	}
	if !jumped || world.state.Health != 100 || world.boss.stage != BossRecovery {
		t.Fatalf("could not jump trolley: health %d", world.state.Health)
	}
}

func TestHospitalTrolleySweepsAndStaysInsideArena(t *testing.T) {
	for _, marker := range []Vector{{X: 400}, {X: 900}} {
		for tick := 0; tick <= 338; tick++ {
			cart := hospitalTrolley(marker, tick)
			if cart.X < 112 || cart.X+cart.Width > 1168 {
				t.Fatalf("cart escaped protected boundary: %+v", cart)
			}
			if tick > 0 && tick < 338 {
				previous := hospitalTrolley(marker, tick-1)
				if distance := cart.X - previous.X; distance != 3 && distance != -3 {
					t.Fatalf("wrong cart speed: %v", distance)
				}
			}
		}
	}
	_, _, _, _, world, arena := localBossFixture(t, false)
	beginLocalAttack(t, arena, 0, 1)
	world.boss.elapsed = 100
	cart := hospitalTrolley(world.boss.locked[0], 100)
	world.state.Player.Bounds.X = cart.X + cart.Width + 1.5
	world.state.Player.Velocity = Vector{}
	world.state.Player.Support = "boundary-floor"
	arenaTick(t, arena, PlayerCommand{})
	if world.state.Health != 65 {
		t.Fatalf("moving trolley tunneled through player: health %d", world.state.Health)
	}
}

func TestHospitalMovingFloorRecovery(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world, arena := localBossFixture(t, false)
	world.state.Player.Bounds.X = 50
	world.boss.stage, world.boss.elapsed = BossRecovery, 0
	for i := 0; i < 40; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	if view := arena.View(ctx); view.Boss.Bounds.X != 576 || view.Boss.Bounds.Y != 0 || view.Boss.Protected || !view.SpecimenBurst {
		t.Fatalf("first recovery: %+v", view)
	}
	world.boss.index, world.boss.stage, world.boss.elapsed = 1, BossRecovery, 0
	for i := 0; i < 80; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	if view := arena.View(ctx); view.Boss.Bounds.X != 624 || view.Boss.Bounds.Y != 0 || view.Boss.Protected {
		t.Fatalf("second recovery: %+v", view)
	}
	world.boss.phase, world.boss.stage = 2, BossTransition
	if !arena.View(ctx).OrganShortage {
		t.Fatal("second transition cue missing")
	}
}

func TestHospitalJarEscapeAfterLock(t *testing.T) {
	_, _, _, _, world, arena := localBossFixture(t, false)
	beginLocalAttack(t, arena, 1, 1)
	locked := append([]Vector(nil), world.boss.locked...)
	for i := 0; i < 47; i++ {
		arenaTick(t, arena, PlayerCommand{Move: 1})
	}
	if world.boss.stage != BossActive || world.state.Health != 100 || !reflect.DeepEqual(locked, world.boss.locked) {
		t.Fatal("jars landed early or followed the player")
	}
	arenaTick(t, arena, PlayerCommand{Move: 1})
	if world.boss.stage != BossRecovery || world.state.Health != 100 {
		t.Fatalf("could not leave locked jar circle: %+v", arena.View(context.Background()))
	}
}

func TestHospitalFailedTickRollsBack(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world, arena := localBossFixture(t, false)
	world.boss.phase, world.boss.health, world.boss.stage = 1, 294, BossRecovery
	world.state.Player.Bounds.X = 300
	world.state.Player.Support = "boundary-floor"
	before, body := arena.View(ctx), world.state.Player
	if err := arena.Step(ctx, ObjectCommand{}); err == nil {
		t.Fatal("accepted a tick without valid camera bounds")
	}
	if !reflect.DeepEqual(arena.View(ctx), before) || world.state.Player != body {
		t.Fatal("failed tick changed the rig, conveyor, or boss")
	}
}

func TestHospitalConveyorsAndRetry(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world, arena := localBossFixture(t, false)
	world.boss.phase, world.boss.health, world.boss.stage = 1, 294, BossTransition
	world.state.Player.Bounds.X = 300
	world.state.Player.Support = "boundary-floor"
	start := world.state.Player.Bounds.X
	arenaTick(t, arena, PlayerCommand{})
	if world.state.Player.Bounds.X != start+0.5 || len(arena.View(ctx).Conveyors) != 1 {
		t.Fatalf("left conveyor: %+v", arena.View(ctx))
	}
	world.boss.phase = 2
	world.state.Player.Bounds.X = 900
	world.state.Player.Velocity = Vector{}
	start = world.state.Player.Bounds.X
	arenaTick(t, arena, PlayerCommand{})
	if world.state.Player.Bounds.X != start-0.5 || len(arena.View(ctx).Conveyors) != 2 {
		t.Fatal("right conveyor did not point inward")
	}
	world.boss.health = 20
	world.boss.locked = []Vector{{X: 400}}
	world.combat.projectiles = append(world.combat.projectiles, Projectile{ID: "unfinished", Attacker: "ENC-HOS-05"})
	restored, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := NewBossArena(ctx, restored)
	if err != nil || fresh.View(ctx).Boss.Health != 420 || fresh.View(ctx).Boss.Phase != 1 || len(fresh.View(ctx).Conveyors) != 0 || len(restored.combat.projectiles) != 0 {
		t.Fatalf("retry: %+v %v", fresh.View(ctx), err)
	}
}

// finishWithMelee plays the live encounter without adding health, ammunition,
// damage, or position during the fight. It uses the visible locked attacks.
func finishWithMelee(t *testing.T, arena *BossArena) {
	t.Helper()
	world := arena.world
	var jumpedFor string
	for tick := 0; tick < 16000 && world.boss.stage != BossPending; tick++ {
		boss := world.Boss(context.Background())
		body := world.state.Player.Bounds
		player := PlayerCommand{Aim: Vector{X: 1}}
		key := boss.Attack.ID + ":" + string(boss.Stage)
		if boss.Stage == BossTelegraph && boss.Elapsed == 0 {
			jumpedFor = ""
		}
		switch {
		case boss.Stage == BossActive && (boss.Attack.ID == "HOS-C" || boss.Attack.ID == "CHU-B"):
			player.Move = 1
		case boss.Stage == BossTelegraph && (boss.Attack.ID == "HOS-A" || boss.Attack.ID == "CHU-A") && boss.Remaining <= 20 && jumpedFor != key && body.Y < 1:
			player.Edges = []PlayerEdge{release(settings.Jump), press(settings.Jump)}
			jumpedFor = key
		case boss.Stage == BossActive && boss.Attack.ID == "HOS-B" && len(boss.LockedTargets) == 1 && jumpedFor != key:
			cart := hospitalTrolley(boss.LockedTargets[0], boss.Elapsed)
			if body.Y < 1 && ((cart.X < body.X && body.X-(cart.X+cart.Width) < 100) || (cart.X > body.X && cart.X-(body.X+body.Width) < 100)) {
				player.Edges = []PlayerEdge{release(settings.Jump), press(settings.Jump)}
				jumpedFor = key
			}
		case boss.Stage == BossActive && boss.Attack.ID == "CHU-C":
			for _, shot := range arena.View(context.Background()).Projectiles {
				if shot.Position.Y == 20 && jumpedFor != key && body.Y < 1 && shot.Velocity.X > 0 && shot.Position.X < body.X && body.X-shot.Position.X < 70 {
					player.Edges = []PlayerEdge{release(settings.Jump), press(settings.Jump)}
					jumpedFor = key
				}
			}
			if jumpedFor == key && body.Y > 50 {
				player.Edges = []PlayerEdge{release(settings.Jump)}
			}
		case boss.Stage == BossRecovery:
			if body.X > boss.Bounds.X-45 {
				player.Move = -1
			} else if body.X < boss.Bounds.X-60 {
				player.Move = 1
			} else if world.motion.action == FreeAction || world.state.Tick >= world.motion.actionUntil {
				player.Edges = []PlayerEdge{release(settings.Melee), press(settings.Melee)}
			}
		default:
			if body.X > boss.Bounds.X-45 {
				player.Move = -1
			} else if body.X < boss.Bounds.X-60 {
				player.Move = 1
			}
		}
		arenaTick(t, arena, player)
		if world.motion.dead {
			t.Fatalf("died at tick %d: boss %+v player %+v", tick, world.Boss(context.Background()), world.state.Player)
		}
	}
	if world.boss.stage != BossPending || world.state.Feces != 5 || world.motion.limbs != AllLimbs || len(world.limbs.objects) != 0 {
		t.Fatalf("intact melee failed: %+v, player %+v", world.Boss(context.Background()), world.state.Player)
	}
}

func TestHospitalIntactMeleeCompletion(t *testing.T) {
	_, _, _, _, world, arena := localBossFixture(t, false)
	world.state.Player.Bounds.X = -134 // the authored pre-fight checkpoint
	world.state.Player.Support = "boundary-floor"
	finishWithMelee(t, arena)
}

func TestHospitalProtectedAndUnacknowledgedVictory(t *testing.T) {
	ctx := context.Background()
	owner, _, _, committed, world, arena := localBossFixture(t, false)
	world.boss.stage = BossTelegraph
	arenaTick(t, arena, PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}})
	for i := 1; i < 15; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	if world.boss.health != 420 || len(world.boss.hits) != 1 || world.boss.hits[0].Result != "protected" {
		t.Fatalf("protected hit: %+v", world.Boss(ctx))
	}
	world.boss.stage, world.boss.phase, world.boss.health, world.boss.elapsed = BossRecovery, 2, 22, 0
	world.motion.action, world.combat.attack = FreeAction, PlayerAttack{}
	arenaTick(t, arena, PlayerCommand{Edges: []PlayerEdge{release(settings.Melee), press(settings.Melee)}})
	for i := 1; i < 15; i++ {
		arenaTick(t, arena, PlayerCommand{})
	}
	if world.boss.stage != BossPending || arena.View(ctx).Survived || arena.View(ctx).DriveStopped {
		t.Fatalf("unacknowledged outcome: %+v", arena.View(ctx))
	}
	proposal, err := world.PrepareBoss(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
	if err != nil || proposal.Credits() != committed.Credits() {
		t.Fatalf("victory proposal: %v", err)
	}
	if err = world.AcknowledgeBoss(ctx, owner, ServiceAcknowledgment{}); err == nil || arena.View(ctx).Survived {
		t.Fatal("failed save confirmed outcome")
	}
	if err = world.AcknowledgeBoss(ctx, owner, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	view := arena.View(ctx)
	if !view.Survived || !view.DriveStopped || !view.WardBypass || view.ArchiveOpen {
		t.Fatalf("confirmed outcome: %+v", view)
	}
}

// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/settings"
)

func combatWorld(t *testing.T, colliders ...Collider) *World {
	t.Helper()
	world := movementWorld(t, append([]Collider{floorCollider()}, colliders...)...)
	world.state.Player.Support = "floor"
	world.state.Feces = 5
	return world
}

func combatStep(t *testing.T, world *World, command CombatCommand) {
	t.Helper()
	if command.Visible == (Rect{}) {
		command.Visible = Rect{X: -100, Y: -20, Width: 1000, Height: 200}
	}
	if err := world.StepCombat(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func addCombatEnemy(t *testing.T, world *World, id string, kind EnemyKind, x float64) {
	t.Helper()
	if err := world.AddEnemy(context.Background(), id, kind, Rect{X: x, Width: 28, Height: 58}); err != nil {
		t.Fatal(err)
	}
}

func TestMeleeArmsAndPhases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limbs  LimbMask
		damage int
		reach  float64
	}{
		{name: "two", limbs: AllLimbs, damage: 22, reach: 48},
		{name: "one", limbs: LeftArm | LeftLeg | RightLeg, damage: 16, reach: 48},
		{name: "none", limbs: LeftLeg | RightLeg, damage: 10, reach: 32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world := combatWorld(t)
			if err := world.SetPlayerLimbs(context.Background(), tc.limbs); err != nil {
				t.Fatal(err)
			}
			addCombatEnemy(t, world, "enemy", MeleeEnemy, 50)
			hidden := Rect{X: -500, Y: -20, Width: 100, Height: 100}
			combatStep(t, world, CombatCommand{Visible: hidden, Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}}})
			for i := 1; i < 14; i++ {
				combatStep(t, world, CombatCommand{Visible: hidden})
			}
			if world.combat.attack.Phase != PhaseWindup || world.combat.enemies[0].Health != 60 {
				t.Fatal("windup released early")
			}
			combatStep(t, world, CombatCommand{Visible: hidden})
			if world.combat.attack.Phase != PhaseActive || world.combat.attack.Reach != tc.reach || world.combat.enemies[0].Health != 60-tc.damage {
				t.Fatalf("bad active strike: %+v %+v", world.combat.attack, world.combat.enemies[0])
			}
			for i := 0; i < 10; i++ {
				combatStep(t, world, CombatCommand{Visible: hidden})
			}
			if world.combat.attack.Phase != PhaseRecovery || world.combat.enemies[0].Health != 60-tc.damage {
				t.Fatal("attack repeated or recovery missing")
			}
		})
	}
}

func TestStrikeReachHeightAndFacing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limbs LimbMask
		box   Rect
		aim   Vector
		hit   bool
	}{
		{name: "punch reach", limbs: AllLimbs, box: Rect{X: 70, Width: 28, Height: 58}, hit: true},
		{name: "headbutt misses", limbs: LeftLeg | RightLeg, box: Rect{X: 61, Width: 28, Height: 58}},
		{name: "above strike", limbs: AllLimbs, box: Rect{X: 50, Y: 120, Width: 28, Height: 58}},
		{name: "left facing", limbs: AllLimbs, box: Rect{X: -65, Width: 28, Height: 58}, aim: Vector{X: -1}, hit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world := combatWorld(t)
			if err := world.SetPlayerLimbs(context.Background(), tc.limbs); err != nil {
				t.Fatal(err)
			}
			if err := world.AddEnemy(context.Background(), "target", MeleeEnemy, tc.box); err != nil {
				t.Fatal(err)
			}
			hidden := Rect{X: -500, Y: -20, Width: 100, Height: 100}
			combatStep(t, world, CombatCommand{Visible: hidden, Player: PlayerCommand{Aim: tc.aim, Edges: []PlayerEdge{press(settings.Melee)}}})
			for i := 1; i < 15; i++ {
				combatStep(t, world, CombatCommand{Visible: hidden})
			}
			if (world.combat.enemies[0].Health < 60) != tc.hit {
				t.Fatalf("unexpected hit: %+v", world.combat.enemies[0])
			}
		})
	}
}

func TestThrowReleaseAndDamageOrdering(t *testing.T) {
	for _, tc := range []struct {
		name      string
		damageAt  int
		remaining int
	}{
		{name: "before release", damageAt: 11, remaining: 5},
		{name: "at release", damageAt: 12, remaining: 4},
		{name: "after release", damageAt: 13, remaining: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world := combatWorld(t)
			for tick := 1; tick <= tc.damageAt; tick++ {
				command := CombatCommand{}
				if tick == 1 {
					command.Player.Edges = []PlayerEdge{press(settings.Throw)}
				}
				if tick == tc.damageAt {
					command.Hits = []DamageHit{{Attacker: "hazard", Attack: "1", Kind: OrdinaryDamage, Amount: 15, Direction: -1}}
				}
				combatStep(t, world, command)
			}
			if world.state.Feces != tc.remaining || world.motion.action != HurtAction || world.combat.attack.ID != 0 {
				t.Fatalf("interruption: health %d feces %d action %v", world.state.Health, world.state.Feces, world.motion.action)
			}
			if got := len(world.combat.projectiles); (got != 0) != (tc.remaining == 4) {
				t.Fatalf("released projectiles %d", got)
			}
			view := world.CombatView(context.Background())
			if (len(view.Releases) == 1) != (tc.damageAt == 12) {
				t.Fatalf("boundary release evidence: %+v", view.Releases)
			}
		})
	}
	world := combatWorld(t)
	world.state.Feces = 0
	combatStep(t, world, CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Throw)}}})
	if world.motion.action != FreeAction || world.state.Feces != 0 {
		t.Fatal("empty throw started")
	}
}

func TestHigherPriorityActionsCancelThrow(t *testing.T) {
	ctx := context.Background()
	for _, action := range []ActionKind{RemovalAction, InteractionAction, HurtAction, DeathAction} {
		world := combatWorld(t)
		combatStep(t, world, CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Throw)}}})
		if err := world.StartPlayerAction(ctx, action, time.Second); err != nil {
			t.Fatal(err)
		}
		if world.CombatView(ctx).Attack.ID != 0 || world.state.Feces != 5 {
			t.Fatalf("action %v did not cancel unfinished throw", action)
		}
		if action == DeathAction && (!world.motion.dead || world.state.Health != 0) {
			t.Fatal("death action did not end gameplay")
		}
	}
}

func TestDamageOrderingImmunityAndPickups(t *testing.T) {
	world := combatWorld(t)
	ctx := context.Background()
	if err := world.AddMedicalPickup(ctx, MedicalPickup{ID: "kit", Bounds: Rect{X: 0, Width: 28, Height: 58}}); err != nil {
		t.Fatal(err)
	}
	combatStep(t, world, CombatCommand{})
	if world.combat.pickups[0].Collected {
		t.Fatal("full health consumed pickup")
	}
	combatStep(t, world, CombatCommand{Hits: []DamageHit{
		{Attacker: "z", Attack: "1", Kind: OrdinaryDamage, Amount: 50, Direction: 1},
		{Attacker: "a", Attack: "1", Kind: OrdinaryDamage, Amount: 18, Direction: -1},
		{Attacker: "a", Attack: "1", Kind: OrdinaryDamage, Amount: 18, Direction: -1},
	}})
	if world.state.Health != 100 || !world.combat.pickups[0].Collected || world.state.Player.Velocity != (Vector{X: -260, Y: 220}) {
		t.Fatalf("damage, pickup or knockback: %d %+v", world.state.Health, world.state.Player.Velocity)
	}
	view := world.CombatView(ctx)
	if len(view.Outcomes) != 3 || view.Outcomes[0].Result != HitAccepted || view.Outcomes[1].Result != HitDuplicate || view.Outcomes[2].Result != HitImmune || !view.Immune {
		t.Fatalf("damage order: %+v", view.Outcomes)
	}
	until := view.ImmuneUntil
	for world.state.Tick+1 < until {
		combatStep(t, world, CombatCommand{})
		if !world.CombatView(ctx).Immune {
			t.Fatal("protection indicator ended early")
		}
	}
	combatStep(t, world, CombatCommand{Hits: []DamageHit{{Attacker: "b", Attack: "2", Kind: OrdinaryDamage, Amount: 18, Direction: 1}}})
	if world.state.Health != 82 || world.combat.outcomes[0].Result != HitAccepted {
		t.Fatal("immunity did not expire on boundary")
	}
	combatStep(t, world, CombatCommand{Hits: []DamageHit{{Attacker: "pit", Attack: "death", Kind: LethalDamage, Amount: 1, Direction: 1}}})
	if world.state.Health != 0 || !world.motion.dead || world.combat.outcomes[0].Result != HitAccepted {
		t.Fatal("lethal event was protected")
	}
}

func TestPickupNotConsumedByLethalContact(t *testing.T) {
	world := combatWorld(t)
	world.state.Health = 50
	if err := world.AddMedicalPickup(context.Background(), MedicalPickup{ID: "kit", Bounds: Rect{Width: 28, Height: 58}}); err != nil {
		t.Fatal(err)
	}
	combatStep(t, world, CombatCommand{Hits: []DamageHit{{Attacker: "crusher", Attack: "1", Kind: LethalDamage, Amount: 1, Direction: 1}}})
	if world.state.Health != 0 || world.combat.pickups[0].Collected {
		t.Fatal("lethal contact consumed a pickup")
	}
}

func TestDirectAndSplashIdentityIsDeterministic(t *testing.T) {
	direct := DamageHit{Attacker: "projectile", Attack: "shot", Kind: OrdinaryDamage, Amount: 18, Direction: 1}
	splash := DamageHit{Attacker: "projectile", Attack: "shot", Kind: OrdinaryDamage, Amount: 5, Direction: 1, Splash: true}
	for _, hits := range [][]DamageHit{{direct, splash}, {splash, direct}} {
		world := combatWorld(t)
		combatStep(t, world, CombatCommand{Hits: hits})
		if world.state.Health != 82 || world.combat.outcomes[0].Hit.Splash || world.combat.outcomes[1].Result != HitDuplicate {
			t.Fatalf("direct and splash: %+v", world.combat.outcomes)
		}
	}
}

func TestProjectileContactsAcrossTicks(t *testing.T) {
	for _, splashFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("splash-first-%v", splashFirst), func(t *testing.T) {
			world := combatWorld(t)
			first := DamageHit{Attacker: "projectile", Attack: "shot", Kind: OrdinaryDamage, Amount: 18, Direction: 1, Splash: splashFirst}
			later := first
			later.Splash = !splashFirst
			combatStep(t, world, CombatCommand{Hits: []DamageHit{first}})
			if world.state.Health != 82 || world.combat.outcomes[0].Result != HitAccepted {
				t.Fatal("first contact was not accepted")
			}
			until := world.combat.immuneUntil
			combatStep(t, world, CombatCommand{Hits: []DamageHit{later}})
			if world.combat.outcomes[0].Result != HitDuplicate || world.state.Health != 82 {
				t.Fatal("duplicate during immunity was not rejected")
			}
			for world.state.Tick < until+10 {
				combatStep(t, world, CombatCommand{})
			}
			combatStep(t, world, CombatCommand{Hits: []DamageHit{later}})
			if world.combat.outcomes[0].Result != HitDuplicate || world.state.Health != 82 || world.PlayerStatus().OrdinaryImmune {
				t.Fatal("duplicate after immunity caused damage or restarted immunity")
			}
			distinct := first
			distinct.Attack = "another-shot"
			combatStep(t, world, CombatCommand{Hits: []DamageHit{distinct}})
			if world.combat.outcomes[0].Result != HitAccepted || world.state.Health != 64 {
				t.Fatal("distinct attack was incorrectly deduplicated")
			}
			for world.state.Tick < world.combat.immuneUntil {
				combatStep(t, world, CombatCommand{})
			}
			other := first
			other.Attacker = "different-source"
			combatStep(t, world, CombatCommand{Hits: []DamageHit{other}})
			if world.combat.outcomes[0].Result != HitAccepted || world.state.Health != 46 {
				t.Fatal("a different attacker was incorrectly deduplicated")
			}
		})
	}
	world := combatWorld(t)
	first := DamageHit{Attacker: "source", Attack: "contact", Kind: OrdinaryDamage, Amount: 18, Direction: 1}
	combatStep(t, world, CombatCommand{Hits: []DamageHit{first}})
	rejected := DamageHit{Attacker: "source", Attack: "blocked", Kind: OrdinaryDamage, Amount: 18, Direction: 1}
	combatStep(t, world, CombatCommand{Hits: []DamageHit{rejected}})
	if world.combat.outcomes[0].Result != HitImmune {
		t.Fatal("second event should have been blocked by immunity")
	}
	for world.state.Tick < world.combat.immuneUntil {
		combatStep(t, world, CombatCommand{})
	}
	combatStep(t, world, CombatCommand{Hits: []DamageHit{rejected}})
	if world.combat.outcomes[0].Result != HitDuplicate || world.state.Health != 82 {
		t.Fatal("an immune-rejected event damaged the player later")
	}
	lethal := first
	lethal.Kind = LethalDamage
	combatStep(t, world, CombatCommand{Hits: []DamageHit{lethal}})
	if !world.motion.dead || world.combat.outcomes[0].Result != HitAccepted {
		t.Fatal("ordinary-event deduplication blocked lethal damage")
	}
}

func TestProjectileDeduplicationIsTargetSpecific(t *testing.T) {
	world := combatWorld(t)
	addCombatEnemy(t, world, "enemy", MeleeEnemy, 50)
	hidden := Rect{X: -500, Y: -20, Width: 100, Height: 100}
	for tick := 1; tick <= 15; tick++ {
		command := CombatCommand{Visible: hidden}
		if tick == 1 {
			command.Player.Edges = []PlayerEdge{press(settings.Melee)}
		}
		if tick == 15 {
			command.Hits = []DamageHit{{Attacker: "player", Attack: "1", Kind: OrdinaryDamage, Amount: 8, Direction: -1, Splash: true}}
		}
		combatStep(t, world, command)
	}
	if world.combat.enemies[0].Health != 38 || world.state.Health != 92 || len(world.combat.outcomes) != 2 || world.combat.outcomes[0].Result != HitAccepted || world.combat.outcomes[1].Result != HitAccepted {
		t.Fatalf("one target's hit suppressed another's: %+v", world.combat.outcomes)
	}
	copyState := cloneCombat(world.combat)
	key := [3]string{"player", "1", "player"}
	delete(copyState.receivedHits, key)
	if !world.combat.receivedHits[key] {
		t.Fatal("combat rollback copy aliases retained hit identities")
	}
}

func TestCompletedStrikeBeforeLethalDamage(t *testing.T) {
	for _, boundary := range []int{14, 15} {
		world := combatWorld(t)
		addCombatEnemy(t, world, "enemy", MeleeEnemy, 50)
		for tick := 1; tick <= boundary; tick++ {
			command := CombatCommand{Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}}
			if tick == 1 {
				command.Player.Edges = []PlayerEdge{press(settings.Melee)}
			}
			if tick == boundary {
				command.Hits = []DamageHit{{Attacker: "pit", Attack: "1", Kind: LethalDamage, Amount: 1, Direction: 1}}
			}
			combatStep(t, world, command)
		}
		want := 60
		if boundary == 15 {
			want = 38
		}
		if world.combat.enemies[0].Health != want || !world.motion.dead {
			t.Fatalf("tick %d: enemy %d dead %v", boundary, world.combat.enemies[0].Health, world.motion.dead)
		}
	}
}

func TestEnemyTelegraphsAndDefeat(t *testing.T) {
	for _, kind := range []EnemyKind{MeleeEnemy, FollowerEnemy, RangedEnemy} {
		t.Run(string(kind), func(t *testing.T) {
			world := combatWorld(t)
			x := 50.0
			telegraph := 60
			if kind == RangedEnemy {
				x, telegraph = 300, 78
			}
			addCombatEnemy(t, world, "enemy", kind, x)
			hidden := Rect{X: -500, Y: -20, Width: 100, Height: 100}
			for i := 0; i < 5; i++ {
				combatStep(t, world, CombatCommand{Visible: hidden})
			}
			if world.combat.enemies[0].Phase != EnemyApproach || world.state.Health != 100 {
				t.Fatal("offscreen attack or idle contact damage")
			}
			combatStep(t, world, CombatCommand{})
			if world.combat.enemies[0].Phase != EnemyTelegraph {
				t.Fatal("missing telegraph")
			}
			for i := 1; i < telegraph; i++ {
				combatStep(t, world, CombatCommand{})
			}
			if world.combat.enemies[0].Phase != EnemyTelegraph || world.state.Health != 100 {
				t.Fatal("attack occurred before telegraph ended")
			}
			combatStep(t, world, CombatCommand{})
			if world.combat.enemies[0].Phase != EnemyRecovery {
				t.Fatal("missing recovery")
			}
			if kind != RangedEnemy && world.state.Health != 82 {
				t.Fatal("melee strike did not hit")
			}
			if kind == RangedEnemy && len(world.combat.projectiles) != 1 {
				t.Fatal("ranged projectile missing")
			}
			world.hurtEnemy(&world.combat.enemies[0], 60, 1)
			if world.combat.enemies[0].Phase != EnemyDefeated || len(world.state.Receipts) != 0 {
				t.Fatal("defeat did not stop enemy or created credit")
			}
		})
	}
}

func TestEnemyRecoveryExpiresAtBoundary(t *testing.T) {
	for _, kind := range []EnemyKind{MeleeEnemy, RangedEnemy, FollowerEnemy} {
		t.Run(string(kind), func(t *testing.T) {
			world := combatWorld(t)
			x, recovery := 50.0, uint64(78)
			if kind == RangedEnemy {
				x, recovery = 300, 108
			}
			addCombatEnemy(t, world, "enemy", kind, x)
			combatStep(t, world, CombatCommand{})
			telegraphEnd := world.combat.enemies[0].Until
			for world.state.Tick < telegraphEnd {
				combatStep(t, world, CombatCommand{})
			}
			enemy := world.combat.enemies[0]
			if enemy.Phase != EnemyRecovery || enemy.Until != telegraphEnd+recovery {
				t.Fatalf("incorrect recovery start: %+v", enemy)
			}
			hidden := Rect{X: -500, Y: -20, Width: 100, Height: 100}
			for world.state.Tick+1 < enemy.Until {
				combatStep(t, world, CombatCommand{Visible: hidden})
				if world.combat.enemies[0].Phase != EnemyRecovery {
					t.Fatal("recovery ended early")
				}
			}
			combatStep(t, world, CombatCommand{Visible: hidden})
			if world.state.Tick != enemy.Until || world.combat.enemies[0].Phase != EnemyApproach || world.combat.enemies[0].AttackID != 1 {
				t.Fatal("recovery did not expire at its boundary")
			}
		})
	}
}

func TestEnemyDefeatBodiesAreNonblocking(t *testing.T) {
	for _, kind := range []EnemyKind{MeleeEnemy, RangedEnemy, FollowerEnemy} {
		t.Run(string(kind), func(t *testing.T) {
			world := combatWorld(t)
			addCombatEnemy(t, world, "enemy", kind, 50)
			world.combat.enemies[0].Health = 22
			hidden := Rect{X: -500, Y: -20, Width: 100, Height: 100}
			combatStep(t, world, CombatCommand{Visible: hidden, Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}}})
			for world.state.Tick < 15 {
				combatStep(t, world, CombatCommand{Visible: hidden})
			}
			corpse := world.combat.enemies[0]
			if corpse.Phase != EnemyDefeated || corpse.Health != 0 {
				t.Fatalf("enemy was not defeated: %+v", corpse)
			}
			for i := 0; i < 100; i++ {
				combatStep(t, world, CombatCommand{Visible: hidden, Player: PlayerCommand{Move: 1}})
			}
			if world.state.Player.Bounds.X <= corpse.Body.Bounds.X+corpse.Body.Bounds.Width || world.state.Health != 100 || len(world.state.Receipts) != 0 {
				t.Fatal("defeated enemy blocked movement, damaged the player, or granted credit")
			}
		})
	}
}

func TestRangedSpacingBoundaries(t *testing.T) {
	for _, tc := range []struct {
		x        float64
		velocity float64
	}{
		{x: 239, velocity: 110},
		{x: 240},
		{x: 360},
		{x: 361, velocity: -110},
		{x: -239, velocity: -110},
		{x: -361, velocity: 110},
	} {
		t.Run(fmt.Sprintf("x-%v", tc.x), func(t *testing.T) {
			world := combatWorld(t)
			addCombatEnemy(t, world, "ranged", RangedEnemy, tc.x)
			combatStep(t, world, CombatCommand{Visible: Rect{X: -800, Y: -20, Width: 100, Height: 100}})
			enemy := world.combat.enemies[0]
			if enemy.Phase != EnemyApproach || enemy.Body.Velocity.X != tc.velocity || math.Abs(enemy.Body.Bounds.X-(tc.x+tc.velocity*TickSeconds)) > 1e-9 {
				t.Fatalf("spacing: %+v", enemy)
			}
		})
	}
}

func combatCounterSequence(t *testing.T, kind EnemyKind, jump bool) (State, CombatView) {
	t.Helper()
	var colliders []Collider
	if kind != RangedEnemy && !jump {
		colliders = append(colliders, Collider{ID: "left-stop", Kind: Solid, Bounds: Rect{X: -4, Width: 4, Height: 80}})
	}
	world := combatWorld(t, colliders...)
	x := 50.0
	if kind == RangedEnemy {
		x = 300
	}
	addCombatEnemy(t, world, "enemy", kind, x)
	immuneHit := false
	step := func(command CombatCommand) {
		combatStep(t, world, command)
		for _, outcome := range world.combat.outcomes {
			if outcome.Target == "player" && outcome.Hit.Attacker == "enemy" && outcome.Result == HitImmune {
				immuneHit = true
			}
		}
	}
	step(CombatCommand{})
	if kind == RangedEnemy {
		for world.combat.enemies[0].Phase == EnemyTelegraph && world.state.Tick < 100 {
			step(CombatCommand{Player: PlayerCommand{Move: 1}})
		}
	} else {
		start := uint64(54)
		if jump {
			start = 19
		}
		for world.state.Tick < start {
			step(CombatCommand{})
		}
	}
	counter := settings.Evade
	aim := Vector{X: -1}
	if jump {
		counter = settings.Jump
	} else if kind == RangedEnemy {
		aim.X = 1
	}
	move := 0.0
	if kind == RangedEnemy {
		move = 1
	}
	step(CombatCommand{Player: PlayerCommand{Move: move, Aim: aim, Edges: []PlayerEdge{press(counter)}}})
	if jump {
		landed := false
		for i := 0; i < 100; i++ {
			if world.state.Player.Support != "" {
				landed = true
				break
			}
			move = 0
			if kind == RangedEnemy && world.state.Tick < 94 {
				move = 1
			}
			step(CombatCommand{Player: PlayerCommand{Move: move}})
		}
		if !landed {
			t.Fatal("counter jump did not land")
		}
	} else {
		for i := 0; i < 30 && world.motion.action == EvadeAction; i++ {
			step(CombatCommand{})
		}
		if world.motion.action == EvadeAction || !immuneHit {
			t.Fatal("evade did not finish or protect against the attack")
		}
	}
	if world.state.Health != 100 || world.combat.enemies[0].Phase != EnemyRecovery {
		t.Fatalf("counter failed or missed recovery: health %d, enemy %+v", world.state.Health, world.combat.enemies[0])
	}
	direction := 1.0
	if centerX(world.state.Player.Bounds) > centerX(world.combat.enemies[0].Body.Bounds) {
		direction = -1
	}
	step(CombatCommand{Player: PlayerCommand{Aim: Vector{X: direction}, Edges: []PlayerEdge{press(settings.Melee)}}})
	for i := 1; i < 14; i++ {
		step(CombatCommand{})
	}
	if world.combat.enemies[0].Phase != EnemyRecovery || world.combat.enemies[0].Health != 60 {
		t.Fatal("retaliation or recovery ended before the strike boundary")
	}
	step(CombatCommand{})
	if world.state.Health != 100 || world.combat.enemies[0].Health != 38 || world.combat.enemies[0].Phase != EnemyHurt {
		t.Fatalf("retaliation did not hit during recovery: %+v", world.combat.enemies[0])
	}
	return cloneState(world.state), world.CombatView(context.Background())
}

func TestEnemyCountersAndRetaliation(t *testing.T) {
	for _, kind := range []EnemyKind{MeleeEnemy, RangedEnemy, FollowerEnemy} {
		for _, jump := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-jump-%v", kind, jump), func(t *testing.T) {
				firstState, firstCombat := combatCounterSequence(t, kind, jump)
				secondState, secondCombat := combatCounterSequence(t, kind, jump)
				if !reflect.DeepEqual(firstState, secondState) || !reflect.DeepEqual(firstCombat, secondCombat) {
					t.Fatal("counter sequence was not deterministic")
				}
			})
		}
	}
}

func TestCombatProjectileSweepAndCivilian(t *testing.T) {
	world := combatWorld(t, Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 70, Width: 1, Height: 60}})
	addCombatEnemy(t, world, "behind", MeleeEnemy, 90)
	world.combat.projectiles = []Projectile{{ID: "fast", Attacker: "player", Attack: "1", Position: Vector{X: 40, Y: 29}, Velocity: Vector{X: 12000}, PlayerOwned: true}}
	combatStep(t, world, CombatCommand{Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}})
	if len(world.combat.projectiles) != 0 || world.combat.enemies[0].Health != 60 {
		t.Fatal("projectile passed wall")
	}
	world = combatWorld(t)
	if err := world.AddProtectedCivilian(context.Background(), Civilian{ID: "adult", Bounds: Rect{X: 45, Width: 28, Height: 58}}); err != nil {
		t.Fatal(err)
	}
	combatStep(t, world, CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}}})
	for i := 1; i < 15; i++ {
		combatStep(t, world, CombatCommand{})
	}
	if !world.combat.civilians[0].Incapacitated || len(world.state.Receipts) != 0 {
		t.Fatal("protected adult or credit invariant failed")
	}
	view := world.CombatView(context.Background())
	view.Civilians[0].ID = "changed"
	if world.combat.civilians[0].ID != "adult" {
		t.Fatal("combat view aliases world")
	}
}

func TestAttackBufferRecoveryAndJump(t *testing.T) {
	world := combatWorld(t)
	combatStep(t, world, CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}}})
	for i := 1; i < 45; i++ {
		command := CombatCommand{Player: PlayerCommand{Move: 1}}
		if i == 30 {
			command.Player.Edges = []PlayerEdge{press(settings.Jump)}
		}
		combatStep(t, world, command)
		if i == 30 && (world.state.Player.Velocity.Y <= 0 || world.motion.action != AttackAction) {
			t.Fatal("jump interrupted recovery")
		}
		if math.Abs(world.state.Player.Velocity.X) > 256 {
			t.Fatal("attack movement exceeded 80 percent")
		}
	}
	combatStep(t, world, CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{release(settings.Melee), press(settings.Melee)}}})
	for world.state.Tick < 56 {
		combatStep(t, world, CombatCommand{})
	}
	if world.combat.attack.ID != 2 || world.combat.attack.Start != 55 {
		t.Fatalf("buffered attack not started once: %+v", world.combat.attack)
	}
	for i := 0; i < 70; i++ {
		combatStep(t, world, CombatCommand{})
	}
	if world.combat.nextAttack != 2 {
		t.Fatal("held press repeated")
	}
}

func TestHeadbuttRecoveryBoundaryAndInvalidThrow(t *testing.T) {
	world := combatWorld(t)
	if err := world.SetPlayerLimbs(context.Background(), LeftLeg|RightLeg); err != nil {
		t.Fatal(err)
	}
	combatStep(t, world, CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}}})
	for world.state.Tick < 60 {
		combatStep(t, world, CombatCommand{})
	}
	if world.combat.attack.Phase != PhaseRecovery || world.motion.actionUntil != 61 {
		t.Fatal("headbutt recovery ended early")
	}
	combatStep(t, world, CombatCommand{})
	if world.combat.attack.ID != 0 {
		t.Fatal("headbutt recovery overran")
	}
	combatStep(t, world, CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Throw)}}})
	if world.CombatView(context.Background()).InvalidAction != "throw_requires_arm" || world.state.Feces != 5 {
		t.Fatal("armless throw did not report rejection")
	}
}

func TestEnemyDamageImmunity(t *testing.T) {
	world := combatWorld(t)
	addCombatEnemy(t, world, "enemy", MeleeEnemy, 40)
	world.combat.projectiles = []Projectile{
		{ID: "two", Attacker: "player", Attack: "2", Position: Vector{X: 20, Y: 29}, Velocity: Vector{X: 6000}, PlayerOwned: true},
		{ID: "one", Attacker: "player", Attack: "1", Position: Vector{X: 20, Y: 29}, Velocity: Vector{X: 6000}, PlayerOwned: true},
	}
	hidden := Rect{X: -500, Y: -20, Width: 100, Height: 100}
	combatStep(t, world, CombatCommand{Visible: hidden})
	if world.combat.enemies[0].Health != 52 || len(world.combat.outcomes) != 2 || world.combat.outcomes[0].Hit.Attack != "1" || world.combat.outcomes[1].Result != HitImmune {
		t.Fatalf("enemy simultaneous damage: %+v %+v", world.combat.enemies[0], world.combat.outcomes)
	}
	until := world.combat.enemies[0].ImmuneUntil
	for world.state.Tick+1 < until {
		combatStep(t, world, CombatCommand{Visible: hidden})
	}
	if world.hurtEnemy(&world.combat.enemies[0], 8, 1) {
		t.Fatal("enemy immunity expired early")
	}
	combatStep(t, world, CombatCommand{Visible: hidden})
	if !world.hurtEnemy(&world.combat.enemies[0], 8, 1) || world.combat.enemies[0].Health != 44 {
		t.Fatal("enemy immunity did not expire at boundary")
	}
}

func TestRangedProjectileCollisionAndEnemyKnockback(t *testing.T) {
	wall := Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 150, Width: 1, Height: 100}}
	world := combatWorld(t, wall)
	addCombatEnemy(t, world, "ranged", RangedEnemy, 300)
	combatStep(t, world, CombatCommand{})
	for i := 0; i < 140; i++ {
		combatStep(t, world, CombatCommand{})
	}
	if world.state.Health != 100 || len(world.combat.projectiles) != 0 {
		t.Fatal("ranged projectile passed thin wall")
	}
	other := combatWorld(t)
	addCombatEnemy(t, other, "target", MeleeEnemy, 40)
	other.combat.projectiles = []Projectile{{ID: "shot", Attacker: "player", Attack: "one", Position: Vector{X: 20, Y: 29}, Velocity: Vector{X: 6000}, PlayerOwned: true}}
	combatStep(t, other, CombatCommand{Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}})
	enemy := other.combat.enemies[0]
	if enemy.Health != 52 || enemy.Body.Velocity != (Vector{X: 240, Y: 180}) || enemy.Phase != EnemyHurt || len(other.combat.projectiles) != 0 {
		t.Fatalf("projectile impact: %+v", enemy)
	}
}

func TestAuthoredCombatRestoration(t *testing.T) {
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
	view := world.CombatView(ctx)
	if len(view.Enemies) != 1 || view.Enemies[0].Health != 60 || view.Enemies[0].Kind != MeleeEnemy || len(view.Civilians) == 0 {
		t.Fatalf("missing authored combat actors: %+v", view)
	}
	world.hurtEnemy(&world.combat.enemies[0], 60, 1)
	reset, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reset.CombatView(ctx).Enemies[0].Health != 60 {
		t.Fatal("restoration retained enemy damage")
	}
}

func TestMeleeDoesNotCrossSolidCover(t *testing.T) {
	world := combatWorld(t, Collider{ID: "cover", Kind: Solid, Bounds: Rect{X: 34, Width: 1, Height: 58}})
	addCombatEnemy(t, world, "enemy", MeleeEnemy, 40)
	combatStep(t, world, CombatCommand{Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}, Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}}})
	for i := 1; i < 20; i++ {
		combatStep(t, world, CombatCommand{Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}})
	}
	if world.combat.enemies[0].Health != 60 {
		t.Fatal("melee hit through cover")
	}
	other := combatWorld(t, Collider{ID: "cover", Kind: Solid, Bounds: Rect{X: 34, Width: 1, Height: 58}})
	addCombatEnemy(t, other, "enemy", MeleeEnemy, 40)
	for i := 0; i < 62; i++ {
		combatStep(t, other, CombatCommand{})
	}
	if other.state.Health != 100 {
		t.Fatal("enemy hit through cover")
	}
}

func TestSameTickDefeatCancelsRangedRelease(t *testing.T) {
	world := combatWorld(t)
	addCombatEnemy(t, world, "enemy", RangedEnemy, 300)
	world.combat.enemies[0].Health = 8
	world.combat.enemies[0].Phase = EnemyTelegraph
	world.combat.enemies[0].Until = 1
	world.combat.enemies[0].AttackID = 1
	world.combat.projectiles = []Projectile{{ID: "shot", Attacker: "player", Attack: "1", Position: Vector{X: 290, Y: 29}, Velocity: Vector{X: 3000}, PlayerOwned: true}}
	combatStep(t, world, CombatCommand{})
	if world.combat.enemies[0].Phase != EnemyDefeated || len(world.combat.projectiles) != 0 || world.state.Health != 100 {
		t.Fatal("defeated enemy released a same-tick projectile")
	}
}

func TestProtectedAreasBlockAttacks(t *testing.T) {
	world := combatWorld(t)
	world.combat.protection = []Rect{{X: -20, Y: 0, Width: 60, Height: 100}}
	addCombatEnemy(t, world, "outside", MeleeEnemy, 50)
	combatStep(t, world, CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}}})
	for i := 1; i < 15; i++ {
		combatStep(t, world, CombatCommand{})
	}
	if world.combat.enemies[0].Health != 60 {
		t.Fatal("melee crossed protected area")
	}
	world.combat.projectiles = []Projectile{{ID: "shot", Attacker: "player", Attack: "1", Position: Vector{X: 14, Y: 29}, Velocity: Vector{X: 12000}, PlayerOwned: true}}
	combatStep(t, world, CombatCommand{})
	if len(world.combat.projectiles) != 0 || world.combat.enemies[0].Health != 60 {
		t.Fatal("projectile crossed protected area")
	}
}

func TestInvalidCombatEdgesLeaveStateUnchanged(t *testing.T) {
	world := combatWorld(t)
	combatStep(t, world, CombatCommand{Hits: []DamageHit{{Attacker: "hazard", Attack: "one", Kind: OrdinaryDamage, Amount: 18, Direction: 1}}})
	combatStep(t, world, CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Throw)}}})
	if world.motion.buffer != settings.Throw {
		t.Fatal("fixture did not retain a combat buffer")
	}
	before, previous, motion, combat := cloneState(world.state), cloneState(world.previous), world.motion, cloneCombat(world.combat)
	for _, invalid := range []settings.Action{"", "unknown_action"} {
		command := CombatCommand{
			Visible: Rect{X: -100, Y: -20, Width: 1000, Height: 200},
			Player:  PlayerCommand{Move: -1, Aim: Vector{X: -1}, Edges: []PlayerEdge{release(settings.Throw), press(invalid)}},
		}
		if err := world.StepCombat(context.Background(), command); err == nil {
			t.Fatalf("accepted invalid edge %q", invalid)
		}
		if !reflect.DeepEqual(world.state, before) || !reflect.DeepEqual(world.previous, previous) || world.motion != motion || !reflect.DeepEqual(world.combat, combat) {
			t.Fatalf("invalid edge %q changed world or buffered input", invalid)
		}
	}
	combatStep(t, world, CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.SelectLeftArm)}}})
}

func TestCombatDeterminismAndInvalidTick(t *testing.T) {
	a, b := combatWorld(t), combatWorld(t)
	for _, world := range []*World{a, b} {
		addCombatEnemy(t, world, "z", MeleeEnemy, 70)
		for i := 0; i < 90; i++ {
			command := CombatCommand{}
			if i == 0 {
				command.Player.Edges = []PlayerEdge{press(settings.Melee)}
			}
			combatStep(t, world, command)
		}
	}
	if !reflect.DeepEqual(a.CombatView(context.Background()), b.CombatView(context.Background())) || !reflect.DeepEqual(a.state, b.state) {
		t.Fatal("combat sequence differs")
	}
	before := cloneState(a.state)
	if err := a.StepCombat(context.Background(), CombatCommand{Visible: Rect{Width: 100, Height: 100}, Player: PlayerCommand{Move: math.NaN()}}); err == nil || !reflect.DeepEqual(before, a.state) {
		t.Fatal("invalid input mutated world")
	}
}

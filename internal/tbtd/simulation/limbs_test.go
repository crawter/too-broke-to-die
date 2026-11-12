// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/settings"
)

func limbStep(t *testing.T, world *World, command LimbCommand) {
	t.Helper()
	if command.Combat.Visible == (Rect{}) {
		command.Combat.Visible = Rect{X: -500, Y: -20, Width: 100, Height: 100}
	}
	if err := world.StepLimbs(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func limbTicks(t *testing.T, world *World, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		limbStep(t, world, LimbCommand{})
	}
}

func removeSocket(t *testing.T, world *World, selection settings.Action) {
	t.Helper()
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{release(settings.RemoveLimb), press(selection), press(settings.RemoveLimb)}}}})
	limbTicks(t, world, 41)
}

func TestLimbCapabilityViewAllCombinations(t *testing.T) {
	for mask := LimbMask(0); mask <= AllLimbs; mask++ {
		t.Run(fmt.Sprint(mask), func(t *testing.T) {
			world := combatWorld(t)
			if err := world.SetPlayerLimbs(context.Background(), mask); err != nil {
				t.Fatal(err)
			}
			status := world.PlayerStatus()
			run, jump, evade := legSpeeds(mask)
			if status.Limbs != mask || status.RunSpeed != run || status.JumpSpeed != jump || status.EvadeSpeed != evade || status.CanCarry != (mask&(LeftArm|RightArm) != 0) || status.SelectedSocket != SocketLeftArm {
				t.Fatalf("capabilities: %+v", status)
			}
			if body := world.state.Player.Bounds; body.Width != 28 || body.Height != 58 {
				t.Fatalf("collision box: %+v", body)
			}
			limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}}}})
			arms := mask & (LeftArm | RightArm)
			want := 10
			if arms == LeftArm|RightArm {
				want = 22
			} else if arms != 0 {
				want = 16
			}
			if world.combat.attack.Damage != want {
				t.Fatalf("melee damage %d, want %d", world.combat.attack.Damage, want)
			}
		})
	}
}

func TestRemovalCostAndBoundary(t *testing.T) {
	for _, health := range []int{8, 9} {
		t.Run(fmt.Sprint(health), func(t *testing.T) {
			world := combatWorld(t)
			world.state.Health = health
			limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.RemoveLimb)}}}})
			limbTicks(t, world, 40)
			if world.state.Health != health || world.motion.limbs != AllLimbs || len(world.limbs.objects) != 0 {
				t.Fatal("removal completed before 42 ticks")
			}
			limbStep(t, world, LimbCommand{})
			view := world.LimbView(context.Background())
			if health == 8 {
				if world.state.Health != 8 || view.Attached != AllLimbs || len(view.Objects) != 0 {
					t.Fatal("removal at eight health succeeded")
				}
				return
			}
			if world.state.Health != 1 || view.Attached != AllLimbs&^LeftArm || len(view.Objects) != 1 || view.Carried != view.Objects[0].ID || len(view.Events) != 3 {
				t.Fatalf("removal completion: %+v health %d", view, world.state.Health)
			}
		})
	}
}

func TestRemovalRejectionAndDamageBoundary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(*World)
		command PlayerCommand
	}{
		{name: "moving input", command: PlayerCommand{Move: 1}},
		{name: "sliding", setup: func(world *World) { world.state.Player.Velocity.X = 10 }},
		{name: "airborne", setup: func(world *World) { world.state.Player.Support = ""; world.state.Player.Bounds.Y = 50 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world := combatWorld(t)
			if tc.setup != nil {
				tc.setup(world)
			}
			tc.command.Edges = []PlayerEdge{press(settings.RemoveLimb)}
			limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: tc.command}})
			if world.LimbView(context.Background()).Invalid == "" || world.state.Health != 100 || len(world.limbs.objects) != 0 {
				t.Fatal("invalid removal accepted")
			}
		})
	}
	for _, tick := range []int{41, 42, 43} {
		world := combatWorld(t)
		for i := 1; i <= tick; i++ {
			command := LimbCommand{}
			if i == 1 {
				command.Combat.Player.Edges = []PlayerEdge{press(settings.RemoveLimb)}
			}
			if i == tick {
				command.Combat.Hits = []DamageHit{{Attacker: "hazard", Attack: "one", Kind: OrdinaryDamage, Amount: 15, Direction: 1}}
			}
			limbStep(t, world, command)
		}
		if (len(world.limbs.objects) == 1) != (tick >= 42) {
			t.Fatalf("damage on tick %d, objects %+v", tick, world.limbs.objects)
		}
		want := 85
		if tick >= 42 {
			want -= 8
		}
		if world.state.Health != want || world.motion.action != HurtAction {
			t.Fatalf("damage on tick %d: %d", tick, world.state.Health)
		}
	}
}

func TestLimbOrderedEdgesWithinOneTick(t *testing.T) {
	world := combatWorld(t)
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.RemoveLimb), release(settings.RemoveLimb), press(settings.SelectRightLeg)}}}})
	limbTicks(t, world, 41)
	if world.motion.limbs&LeftArm != 0 || world.motion.limbs&RightLeg == 0 || world.PlayerStatus().SelectedSocket != SocketRightLeg {
		t.Fatalf("ordered removal edges: %+v", world.LimbView(context.Background()))
	}
	world.state.Feces = 0
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Throw), press(settings.SelectPayload), release(settings.Throw)}}}})
	limbTicks(t, world, 11)
	if world.state.Feces != 0 || world.limbs.carried != "" || !world.limbs.objects[0].Airborne {
		t.Fatal("ordered throw edges lost the selected limb")
	}
}

func TestLimbActionPriority(t *testing.T) {
	world := combatWorld(t)
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.RemoveLimb)}}}})
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Evade)}}}})
	if world.motion.action != RemovalAction {
		t.Fatal("evade interrupted a committed removal")
	}
	if err := world.StartPlayerAction(context.Background(), InteractionAction, time.Second); err != nil {
		t.Fatal(err)
	}
	limbTicks(t, world, 42)
	if world.motion.limbs != AllLimbs || world.state.Health != 100 || len(world.limbs.objects) != 0 {
		t.Fatal("higher-priority interaction failed to cancel removal")
	}
}

func TestNoArmInteractionAndCivilianPriority(t *testing.T) {
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftArm)
	removeSocket(t, world, settings.SelectRightArm)
	if err := world.AddProtectedCivilian(context.Background(), Civilian{ID: "npc", Bounds: Rect{X: 30, Width: 28, Height: 58}}); err != nil {
		t.Fatal(err)
	}
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}}})
	view := world.LimbView(context.Background())
	if len(view.Events) != 1 || view.Events[0].Kind != "interaction_requested" || view.Events[0].Target != "npc" || view.Channel != "" || len(view.Objects) != 2 {
		t.Fatalf("civilian did not take priority: %+v", view)
	}
	other := combatWorld(t)
	if err := other.SetPlayerLimbs(context.Background(), LeftLeg|RightLeg); err != nil {
		t.Fatal(err)
	}
	limbStep(t, other, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}}})
	view = other.LimbView(context.Background())
	if len(view.Events) != 1 || view.Events[0].Kind != "interaction_requested" || view.Events[0].Target != "" {
		t.Fatalf("no-arm service interaction was lost: %+v", view)
	}
}

func TestLimbCarrySwitchAndNoArmReplacement(t *testing.T) {
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftArm)
	first := world.limbs.carried
	if first == "" {
		t.Fatal("first arm was not carried")
	}
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.SelectPayload)}}}})
	if world.limbs.carried != first || world.limbs.payload {
		t.Fatal("switching payload lost the carried limb")
	}
	removeSocket(t, world, settings.SelectRightArm)
	view := world.LimbView(context.Background())
	if view.Attached&(LeftArm|RightArm) != 0 || len(view.Objects) != 2 || view.Carried != "" || view.Objects[0].Airborne || view.Objects[1].Airborne {
		t.Fatalf("second arm removal: %+v", view)
	}
	if len(view.Events) != 4 || view.Events[0].Kind != "teeth_assisted" {
		t.Fatalf("missing teeth-assisted removal: %+v", view.Events)
	}
	plate := Rect{X: 0, Y: 0, Width: 30, Height: 10}
	weights, err := world.LimbWeights(context.Background(), plate)
	if err != nil || len(weights) != 2 {
		t.Fatalf("dropped weights %v: %v", weights, err)
	}
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}}})
	limbTicks(t, world, 46)
	if world.motion.limbs&(LeftArm|RightArm) != 0 {
		t.Fatal("replacement completed before 48 ticks")
	}
	limbStep(t, world, LimbCommand{})
	if world.motion.limbs&(LeftArm|RightArm) == 0 || len(world.limbs.objects) != 1 || world.state.Health != 84 {
		t.Fatalf("no-arm replacement failed: %+v", world.LimbView(context.Background()))
	}
	weights, err = world.LimbWeights(context.Background(), plate)
	if err != nil || len(weights) != 1 {
		t.Fatalf("consumed weight still present: %v %v", weights, err)
	}
}

func TestRegenerationBoundariesAndStaleObjects(t *testing.T) {
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftLeg)
	old := world.limbs.objects[0]
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Regenerate)}}}})
	limbTicks(t, world, 178)
	if world.motion.limbs == AllLimbs || len(world.limbs.objects) != 1 {
		t.Fatal("regeneration completed before 180 ticks")
	}
	limbStep(t, world, LimbCommand{})
	view := world.LimbView(context.Background())
	if view.Attached != AllLimbs || len(view.Objects) != 0 || view.Generation == old.Generation || world.state.Health != 92 {
		t.Fatalf("regeneration: %+v", view)
	}
	// Even an obsolete identity cannot be selected by a later channel.
	world.limbs.objects = append(world.limbs.objects, old)
	if world.limbIndex(old.ID) != -1 {
		t.Fatal("old generation resolved")
	}
	if err := world.SetPlayerLimbs(context.Background(), AllLimbs); err != nil || len(world.limbs.objects) != 0 {
		t.Fatal("attached socket retained an obsolete object")
	}
}

func TestRegenerationInterruptions(t *testing.T) {
	for _, damageAt := range []int{179, 180, 181} {
		world := combatWorld(t)
		removeSocket(t, world, settings.SelectLeftLeg)
		for i := 1; i <= damageAt; i++ {
			command := LimbCommand{}
			if i == 1 {
				command.Combat.Player.Edges = []PlayerEdge{press(settings.Regenerate)}
			}
			if i == damageAt {
				command.Combat.Hits = []DamageHit{{Attacker: "hazard", Attack: "one", Kind: OrdinaryDamage, Amount: 15, Direction: 1}}
			}
			limbStep(t, world, command)
		}
		if (world.motion.limbs == AllLimbs) != (damageAt >= 180) {
			t.Fatalf("damage at %d: %+v", damageAt, world.LimbView(context.Background()))
		}
	}
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftLeg)
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Regenerate)}}}})
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{release(settings.Regenerate)}}}})
	limbTicks(t, world, 180)
	if world.motion.limbs == AllLimbs || len(world.limbs.objects) != 1 {
		t.Fatal("released regeneration completed")
	}
}

func TestLimbThrowReleaseAndSweep(t *testing.T) {
	for _, at := range []int{11, 12, 13} {
		world := combatWorld(t)
		removeSocket(t, world, settings.SelectLeftArm)
		for i := 1; i <= at; i++ {
			command := LimbCommand{}
			if i == 1 {
				command.Combat.Player = PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Throw)}}
			}
			if i == at {
				command.Combat.Hits = []DamageHit{{Attacker: "hazard", Attack: "one", Kind: OrdinaryDamage, Amount: 15, Direction: 1}}
			}
			limbStep(t, world, command)
		}
		object := world.limbs.objects[0]
		if object.Airborne != (at >= 12) || object.Carried != (at == 11) || world.state.Feces != 5 {
			t.Fatalf("throw interrupted at %d: %+v", at, object)
		}
	}
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftLeg)
	addCombatEnemy(t, world, "first", MeleeEnemy, 80)
	addCombatEnemy(t, world, "second", MeleeEnemy, 110)
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Throw)}}}})
	limbTicks(t, world, 40)
	if world.combat.enemies[0].Health != 25 || world.combat.enemies[1].Health != 60 {
		t.Fatalf("limb damaged wrong targets: %+v", world.combat.enemies)
	}
	if world.limbs.objects[0].Damaging {
		t.Fatal("limb remained damaging after impact")
	}
	arm := combatWorld(t)
	removeSocket(t, arm, settings.SelectLeftArm)
	arm.state.Feces = 0
	addCombatEnemy(t, arm, "target", MeleeEnemy, 80)
	limbStep(t, arm, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Throw)}}}})
	limbTicks(t, arm, 40)
	if arm.combat.enemies[0].Health != 35 || arm.state.Feces != 0 {
		t.Fatal("arm impact damage or zero-feces throw failed")
	}
	wall := combatWorld(t, Collider{ID: "thin-wall", Kind: Solid, Bounds: Rect{X: 35, Width: 0.1, Height: 100}})
	removeSocket(t, wall, settings.SelectRightArm)
	limbStep(t, wall, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Throw)}}}})
	limbTicks(t, wall, 16)
	object := wall.limbs.objects[0]
	if !object.Bounced || object.Damaging || object.Position.X >= 35 || math.Abs(object.Velocity.X+650*0.35) > 1e-6 {
		t.Fatalf("wall bounce: %+v", object)
	}
}

func TestLimbThrowSuspensionPreservesPayload(t *testing.T) {
	ctx := context.Background()
	for _, pauseAt := range []int{1, 11, 12, 13} {
		t.Run(fmt.Sprint(pauseAt), func(t *testing.T) {
			world := combatWorld(t)
			removeSocket(t, world, settings.SelectLeftArm)
			world.state.Feces = 0
			limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Throw)}}}})
			limbTicks(t, world, pauseAt-1)
			attack, binding, tick := world.combat.attack, world.limbs.throw, world.state.Tick
			object := world.limbs.objects[0]
			if attack.ID == 0 || binding.object != object.ID || attack.Released != (pauseAt >= 12) {
				t.Fatalf("invalid throw fixture: %+v %+v", attack, object)
			}
			world.SuspendPlayer(ctx)
			world.SuspendPlayer(ctx)
			if world.state.Tick != tick || world.combat.attack != attack || world.limbs.throw != binding || world.limbs.objects[0] != object || world.state.Feces != 0 {
				t.Fatal("suspension changed the committed payload or attack")
			}
			if err := world.StepLimbs(ctx, LimbCommand{Combat: CombatCommand{Visible: Rect{Width: 100, Height: 100}}}); err == nil {
				t.Fatal("suspended world advanced")
			}
			if world.state.Tick != tick || world.combat.attack != attack || world.limbs.throw != binding || world.limbs.objects[0] != object || world.state.Feces != 0 {
				t.Fatal("rejected suspended tick changed the throw")
			}
			if err := world.ResumePlayer(ctx, []settings.Action{settings.Throw}); err == nil {
				t.Fatal("resumed with throw held")
			}
			if err := world.ResumePlayer(ctx, nil); err != nil {
				t.Fatal(err)
			}
			if pauseAt < 12 {
				limbTicks(t, world, 11-pauseAt)
				if world.limbs.carried != object.ID || world.combat.attack.Released || world.state.Feces != 0 {
					t.Fatal("resumed throw released early")
				}
				limbStep(t, world, LimbCommand{})
				view := world.CombatView(ctx)
				if len(view.Releases) != 1 || view.Releases[0].ProjectileID != object.ID || view.Releases[0].Payload != string(SocketLeftArm) {
					t.Fatalf("resumed release used wrong payload: %+v", view.Releases)
				}
			}
			if world.limbs.carried != "" || !world.limbs.objects[0].Airborne || world.limbs.objects[0].Attack != attack.ID {
				t.Fatal("released limb was refunded or lost")
			}
			for i := 0; i < 50; i++ {
				limbStep(t, world, LimbCommand{})
				if world.state.Feces != 0 || len(world.combat.projectiles) != 0 || len(world.combat.releases) != 0 || world.limbs.carried != "" || len(world.limbs.objects) != 1 {
					t.Fatal("resume duplicated a projectile or consumed feces")
				}
			}
		})
	}
}

func TestLimbThrowUsesCombatPhasesAndBuffer(t *testing.T) {
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftArm)
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}}}})
	limbTicks(t, world, 43)
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Throw)}}}})
	if world.combat.attack.Throw {
		t.Fatal("buffer interrupted melee recovery")
	}
	limbTicks(t, world, 11)
	if !world.combat.attack.Throw || world.combat.attack.Phase != PhaseWindup || world.limbs.carried == "" {
		t.Fatalf("buffered throw did not use combat windup: %+v", world.CombatView(context.Background()).Attack)
	}
	limbTicks(t, world, 12)
	if !world.combat.attack.Released || world.combat.attack.Phase != PhaseActive || world.limbs.carried != "" || world.state.Feces != 5 {
		t.Fatalf("combat release: %+v", world.CombatView(context.Background()).Attack)
	}
}

func TestLimbThrowSharesTheCombatBuffer(t *testing.T) {
	for _, elapsed := range []uint64{40, 41} {
		world := combatWorld(t)
		removeSocket(t, world, settings.SelectLeftArm)
		limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}}}})
		start := world.combat.attack.Start
		for world.state.Tick-start < elapsed {
			limbStep(t, world, LimbCommand{})
		}
		limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Throw)}}}})
		// A later jump cannot replace the one buffered combat action.
		limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Jump)}}}})
		for world.state.Tick-start <= 55 {
			limbStep(t, world, LimbCommand{})
		}
		if world.combat.attack.Throw != (elapsed == 41) || world.state.Player.Velocity.Y > 0 {
			t.Fatalf("buffer at age %d: attack %+v player %+v", elapsed, world.combat.attack, world.state.Player)
		}
	}
}

func TestRetrievalDistanceAndRelease(t *testing.T) {
	for _, distance := range []float64{48, 48.01} {
		world := combatWorld(t)
		removeSocket(t, world, settings.SelectLeftLeg)
		world.dropCarried(context.Background())
		world.limbs.objects[0].Position = Vector{X: PlayerWidth + distance}
		limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}}})
		if (world.limbs.carried != "") != (distance == 48) {
			t.Fatalf("retrieval at %v: %+v", distance, world.LimbView(context.Background()))
		}
		if distance == 48 {
			limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{release(settings.Interact)}}}})
			limbTicks(t, world, 48)
			if world.limbs.carried == "" || world.motion.limbs&LeftLeg != 0 {
				t.Fatal("release did not cancel replacement while preserving retrieval")
			}
		}
	}
}

func TestRetrievalWhileMovingAndAirborne(t *testing.T) {
	for _, tc := range []struct {
		name     string
		airborne bool
		oneArm   bool
		distance float64
		retrieve bool
	}{
		{name: "moving at range", distance: 48, retrieve: true},
		{name: "moving with one arm", distance: 48, oneArm: true, retrieve: true},
		{name: "moving outside range", distance: 48.01},
		{name: "airborne at range", airborne: true, distance: 48, retrieve: true},
		{name: "airborne outside range", airborne: true, distance: 48.01},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world := combatWorld(t)
			removeSocket(t, world, settings.SelectLeftLeg)
			id := world.limbs.carried
			if tc.oneArm {
				if err := world.SetPlayerLimbs(context.Background(), RightArm|RightLeg); err != nil {
					t.Fatal(err)
				}
			}
			world.dropCarried(context.Background())
			body := &world.state.Player
			if tc.airborne {
				body.Bounds.Y = 30
				body.Support = ""
				body.Velocity.Y = 100
			} else {
				body.Velocity.X = 60
			}
			world.limbs.objects[0].Position = Vector{X: body.Bounds.X + body.Bounds.Width + tc.distance, Y: body.Bounds.Y}
			command := PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}
			if !tc.airborne {
				command.Move = 1
			}
			limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: command}})
			view := world.LimbView(context.Background())
			if (view.Carried == id) != tc.retrieve || view.Channel != "" || view.Attached&LeftLeg != 0 || world.state.Health != 92 {
				t.Fatalf("retrieval: %+v health %d", view, world.state.Health)
			}
			if !tc.retrieve {
				if view.Objects[0].Carried {
					t.Fatal("out-of-range object was picked up")
				}
				return
			}
			if !view.Objects[0].Carried || len(view.Events) != 1 || view.Events[0].Kind != "retrieved" || view.Invalid != "" {
				t.Fatalf("retrieval event or object: %+v", view)
			}
			limbTicks(t, world, 48)
			if world.limbs.carried != id || world.motion.limbs&LeftLeg != 0 || world.limbs.channel.kind != "" {
				t.Fatal("movement started a replacement channel")
			}
			limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{release(settings.Interact), press(settings.Interact)}}}})
			limbTicks(t, world, 47)
			if world.motion.limbs&LeftLeg == 0 || len(world.limbs.objects) != 0 || world.state.Health != 92 {
				t.Fatal("stationary replacement failed after retrieval")
			}
		})
	}
}

func TestMovingRetrievalRequiresArmAndEmptySlot(t *testing.T) {
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftArm)
	removeSocket(t, world, settings.SelectRightArm)
	world.state.Player.Bounds.Y = 30
	world.state.Player.Support = ""
	world.state.Player.Velocity.Y = 100
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}}}})
	if world.limbs.carried != "" || world.limbs.channel.kind != "" || len(world.limbs.objects) != 2 {
		t.Fatal("airborne player retrieved a limb without an arm")
	}

	world = combatWorld(t)
	removeSocket(t, world, settings.SelectLeftLeg)
	removeSocket(t, world, settings.SelectRightLeg)
	carried := world.limbs.carried
	world.state.Player.Velocity.X = 60
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Move: 1, Edges: []PlayerEdge{press(settings.Interact)}}}})
	if world.limbs.carried != carried || world.limbs.objects[0].Carried || world.limbs.channel.kind != "" || len(world.limbs.objects) != 2 {
		t.Fatal("moving interaction acquired a second limb or started replacement")
	}
}

func TestReplacementInterruptedAndConsumed(t *testing.T) {
	for _, tick := range []int{47, 48, 49} {
		world := combatWorld(t)
		removeSocket(t, world, settings.SelectLeftArm)
		id := world.limbs.carried
		for i := 1; i <= tick; i++ {
			command := LimbCommand{}
			if i == 1 {
				command.Combat.Player.Edges = []PlayerEdge{press(settings.Interact)}
			}
			if i == tick {
				command.Combat.Hits = []DamageHit{{Attacker: "hazard", Attack: "one", Kind: OrdinaryDamage, Amount: 15, Direction: 1}}
			}
			limbStep(t, world, command)
		}
		if (world.motion.limbs&LeftArm != 0) != (tick >= 48) || (world.limbIndex(id) < 0) != (tick >= 48) {
			t.Fatalf("replacement interrupted at %d: %+v", tick, world.LimbView(context.Background()))
		}
		if world.state.Health != 77 {
			t.Fatalf("replacement restored health: %d", world.state.Health)
		}
	}
}

func TestCarryWhileThrowingFeces(t *testing.T) {
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectRightLeg)
	id := world.limbs.carried
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.SelectPayload), press(settings.Throw)}}}})
	limbTicks(t, world, 11)
	if world.state.Feces != 4 || world.limbs.carried != id || world.limbs.objects[0].Airborne || world.combat.attack.ID == 0 {
		t.Fatalf("feces throw changed held limb: %+v", world.LimbView(context.Background()))
	}
	view := world.LimbView(context.Background())
	view.Objects[0].ID = "changed"
	if world.limbs.objects[0].ID != id {
		t.Fatal("limb view aliases world state")
	}
}

func TestRegenerationRemovesPlateWeights(t *testing.T) {
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftArm)
	removeSocket(t, world, settings.SelectRightArm)
	plate := Rect{X: 0, Y: 0, Width: 28, Height: 10}
	weights, err := world.LimbWeights(context.Background(), plate)
	if err != nil || len(weights) != 2 {
		t.Fatalf("missing plate weights: %v %v", weights, err)
	}
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Regenerate)}}}})
	limbTicks(t, world, 179)
	weights, err = world.LimbWeights(context.Background(), plate)
	if err != nil || len(weights) != 0 || world.motion.limbs != AllLimbs {
		t.Fatalf("regeneration left plate weighted: %v %v", weights, err)
	}
}

func TestLimbTargetSweepAndInvalidCommand(t *testing.T) {
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftArm)
	first := Collider{ID: "paddle-a", Kind: ProjectileTarget, Bounds: Rect{X: 35, Y: 0, Width: 0.1, Height: 100}}
	second := Collider{ID: "paddle-b", Kind: ProjectileTarget, Bounds: first.Bounds}
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Throw)}}}, Targets: []Collider{second, first}})
	var impact LimbEvent
	for i := 0; i < 20; i++ {
		limbStep(t, world, LimbCommand{Targets: []Collider{second, first}})
		for _, event := range world.limbs.events {
			if event.Kind == "impact" {
				impact = event
			}
		}
	}
	if impact.Target != first.ID || world.limbs.objects[0].Damaging {
		t.Fatalf("target sweep or stable ordering: %+v", impact)
	}
	before := world.LimbView(context.Background())
	invalid := LimbCommand{Combat: CombatCommand{Visible: Rect{Width: 100, Height: 100}}, Targets: []Collider{first, first}}
	if err := world.StepLimbs(context.Background(), invalid); err == nil {
		t.Fatal("duplicate targets accepted")
	}
	after := world.LimbView(context.Background())
	if before.Generation != after.Generation || before.Objects[0] != after.Objects[0] || world.state.Tick == 0 {
		t.Fatal("invalid tick mutated object")
	}
}

func TestRegenerationClearsAirborneLimb(t *testing.T) {
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftArm)
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{Y: 1}, Edges: []PlayerEdge{press(settings.Throw)}}}})
	limbTicks(t, world, 11)
	world.limbs.objects[0].Position.Y = 100000
	generation := world.limbs.generation
	limbTicks(t, world, 36)
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Regenerate)}}}})
	limbTicks(t, world, 178)
	if len(world.limbs.objects) != 1 || !world.limbs.objects[0].Airborne {
		t.Fatal("airborne object disappeared before regeneration")
	}
	limbStep(t, world, LimbCommand{})
	if len(world.limbs.objects) != 0 || world.motion.limbs != AllLimbs || world.limbs.generation != generation+1 {
		t.Fatal("regeneration did not invalidate the airborne generation")
	}
}

func TestLimbFlightTimeoutAndStaleDamage(t *testing.T) {
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectRightLeg)
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{Y: 1}, Edges: []PlayerEdge{press(settings.Throw)}}}})
	limbTicks(t, world, 11)
	object := &world.limbs.objects[0]
	if !object.Airborne || !object.Damaging {
		t.Fatal("limb did not launch")
	}
	// Put the launched projectile well above all fixture terrain so the
	// maximum flight timer, rather than a surface, ends its flight.
	object.Position.Y = 100000
	limbTicks(t, world, 239)
	if !world.limbs.objects[0].Airborne {
		t.Fatal("flight timed out before two seconds")
	}
	limbStep(t, world, LimbCommand{})
	object = &world.limbs.objects[0]
	if object.Airborne || object.Damaging || object.Velocity != (Vector{}) {
		t.Fatalf("maximum flight failed to settle: %+v", object)
	}
	stale := *object
	if err := world.SetPlayerLimbs(context.Background(), AllLimbs); err != nil {
		t.Fatal(err)
	}
	world.limbs.generation++
	stale.Airborne, stale.Damaging = true, true
	stale.Position, stale.Velocity = Vector{X: 30, Y: 29}, Vector{X: 650}
	world.limbs.objects = append(world.limbs.objects, stale)
	addCombatEnemy(t, world, "target", MeleeEnemy, 32)
	limbStep(t, world, LimbCommand{})
	if world.combat.enemies[0].Health != 60 {
		t.Fatal("stale generation damaged an enemy")
	}
}

func TestLimbTickRollbackAndMixedEntryPoints(t *testing.T) {
	world := combatWorld(t)
	removeSocket(t, world, settings.SelectLeftArm)
	before := world.LimbView(context.Background())
	tick := world.state.Tick
	command := CombatCommand{Visible: Rect{Width: 100, Height: 100}}
	if err := world.StepCombat(context.Background(), command); err == nil {
		t.Fatal("mixed combat entry points accepted")
	}
	after := world.LimbView(context.Background())
	if world.state.Tick != tick || before.Objects[0] != after.Objects[0] || before.Carried != after.Carried {
		t.Fatal("rejected combat tick changed limb state")
	}
	limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Edges: []PlayerEdge{press(settings.Throw)}}}})
	limbTicks(t, world, 11)
	before = world.LimbView(context.Background())
	tick = world.state.Tick
	duplicate := Collider{ID: "floor", Kind: ProjectileTarget, Bounds: Rect{X: 40, Y: 0, Width: 1, Height: 100}}
	if err := world.StepLimbs(context.Background(), LimbCommand{Combat: command, Targets: []Collider{duplicate}}); err == nil {
		t.Fatal("collision identity conflict accepted")
	}
	after = world.LimbView(context.Background())
	if world.state.Tick != tick || before.Objects[0] != after.Objects[0] || before.Carried != after.Carried {
		t.Fatal("failed collision tick changed limb state")
	}
}

func TestLimbPreviewMatchesRealSweeps(t *testing.T) {
	for _, tc := range []struct {
		selection settings.Action
		speed     float64
	}{
		{selection: settings.SelectLeftArm, speed: 650},
		{selection: settings.SelectLeftLeg, speed: 600},
	} {
		world := combatWorld(t, Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 50, Y: 0, Width: 0.1, Height: 100}})
		removeSocket(t, world, tc.selection)
		preview, err := world.PreviewLimbThrow(context.Background(), Vector{X: 1}, nil)
		if err != nil || preview.Impact == nil || preview.Impact.Collider.ID != "wall" || len(preview.Points) < 2 {
			t.Fatalf("preview: %+v %v", preview, err)
		}
		if math.Abs(preview.Points[1].X-preview.Points[0].X-tc.speed*TickSeconds) > 1e-9 {
			t.Fatalf("preview speed: %+v", preview.Points[:2])
		}
		limbStep(t, world, LimbCommand{Combat: CombatCommand{Player: PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Throw)}}}})
		var impact bool
		for i := 0; i < 40 && !impact; i++ {
			limbStep(t, world, LimbCommand{})
			for _, event := range world.limbs.events {
				impact = impact || (event.Kind == "impact" && event.Target == "wall")
			}
		}
		actual := world.limbs.objects[0].Position
		predicted := preview.Points[len(preview.Points)-1]
		if !impact || math.Hypot(actual.X-predicted.X, actual.Y-predicted.Y) > Tolerance*1.01 {
			t.Fatalf("preview %v versus impact %v", predicted, actual)
		}
	}
	world := combatWorld(t)
	if _, err := world.PreviewLimbThrow(context.Background(), Vector{X: 1}, nil); err == nil {
		t.Fatal("preview without a carried limb accepted")
	}
}

func TestLimbRestoration(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	saved, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range manifest.Batches(ctx)[0].Deeds {
		saved = certifyFixture(t, owner, manifest, saved, id)
	}
	saved, err = owner.Checkpoint(ctx, saved, "ANC-HOS-02")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	removeSocket(t, world, settings.SelectLeftArm)
	if len(world.limbs.objects) != 1 {
		t.Fatal("fixture did not remove limb")
	}
	restored, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	if restored.motion.limbs != AllLimbs || len(restored.limbs.objects) != 0 || restored.state.Health != 100 || restored.state.Feces != 5 {
		t.Fatal("restoration retained transient body state")
	}
}

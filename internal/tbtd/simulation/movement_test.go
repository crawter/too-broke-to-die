// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

func movementWorld(t *testing.T, colliders ...Collider) *World {
	t.Helper()
	grid := gridFixture(t, colliders...)
	state := State{HasPlayer: true, Health: 100, Limbs: 4, Player: Body{ID: "player", Bounds: Rect{X: 0, Y: 0, Width: PlayerWidth, Height: PlayerHeight}}}
	return &World{grid: grid, state: state, previous: cloneState(state), motion: newPlayerMotion()}
}

func floorCollider() Collider {
	return Collider{ID: "floor", Kind: Solid, Bounds: Rect{X: -1000, Y: -10, Width: 2000, Height: 10}}
}

func movementStep(t *testing.T, world *World, command PlayerCommand) {
	t.Helper()
	if err := world.StepPlayer(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func press(action settings.Action) PlayerEdge   { return PlayerEdge{Action: action, Pressed: true} }
func release(action settings.Action) PlayerEdge { return PlayerEdge{Action: action} }

func TestDurationTicksBoundaries(t *testing.T) {
	for _, tc := range []struct {
		duration time.Duration
		want     int64
	}{
		{0, 0}, {time.Nanosecond, 1}, {25 * time.Millisecond, 3}, {25*time.Millisecond + 1, 4}, {120 * time.Millisecond, 15}, {180 * time.Millisecond, 22}, {650 * time.Millisecond, 78}, {time.Second, 120}, {time.Second + time.Nanosecond, 121},
	} {
		got, err := DurationTicks(tc.duration)
		if err != nil || got != tc.want {
			t.Fatalf("%v: got %d, %v; want %d", tc.duration, got, err, tc.want)
		}
	}
	if _, err := DurationTicks(-1); err == nil {
		t.Fatal("negative duration accepted")
	}
}

func TestLimbCombinationsAccelerationBrakingAndGravity(t *testing.T) {
	for mask := LimbMask(0); mask <= AllLimbs; mask++ {
		world := movementWorld(t, floorCollider())
		world.state.Player.Support = "floor"
		if err := world.SetPlayerLimbs(context.Background(), mask); err != nil {
			t.Fatal(err)
		}
		run, jump, evade := 120.0, 0.0, 0.0
		switch mask & (LeftLeg | RightLeg) {
		case LeftLeg | RightLeg:
			run, jump, evade = 320, 760, 650
		case LeftLeg, RightLeg:
			run, jump, evade = 230, 620, 440
		}
		movementStep(t, world, PlayerCommand{Move: 1})
		if world.state.Player.Velocity.X != 20 || world.state.Player.Bounds.Width != 28 || world.state.Player.Bounds.Height != 58 {
			t.Fatalf("mask %d initial acceleration or bounds: %+v", mask, world.state.Player)
		}
		for i := 0; i < 30; i++ {
			movementStep(t, world, PlayerCommand{Move: 1})
		}
		if math.Abs(world.state.Player.Velocity.X-run) > 1e-8 {
			t.Fatalf("mask %d speed %v != %v", mask, world.state.Player.Velocity.X, run)
		}
		movementStep(t, world, PlayerCommand{})
		if math.Abs(world.state.Player.Velocity.X-(run-25)) > 1e-8 {
			t.Fatalf("mask %d brake %v", mask, world.state.Player.Velocity.X)
		}
		movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Jump)}})
		if (world.state.Player.Velocity.Y > 0) != (jump > 0) || (jump > 0 && math.Abs(world.state.Player.Velocity.Y-(jump-2200.0/120)) > 1e-8) {
			t.Fatalf("mask %d jump %+v", mask, world.state.Player)
		}
		other := movementWorld(t, floorCollider())
		other.state.Player.Support = "floor"
		if err := other.SetPlayerLimbs(context.Background(), mask); err != nil {
			t.Fatal(err)
		}
		movementStep(t, other, PlayerCommand{Edges: []PlayerEdge{press(settings.Evade)}})
		if (other.motion.action == EvadeAction) != (evade > 0) || other.state.Player.Velocity.X != evade {
			t.Fatalf("mask %d evade %v, speed %v", mask, other.motion.action, other.state.Player.Velocity.X)
		}
	}
	if err := movementWorld(t).SetPlayerLimbs(context.Background(), 16); err == nil {
		t.Fatal("invalid socket mask")
	}
	world := movementWorld(t)
	world.state.Player.Bounds.Y = 5000
	movementStep(t, world, PlayerCommand{Move: 1})
	if math.Abs(world.state.Player.Velocity.X-1600.0/120) > 1e-9 || math.Abs(world.state.Player.Velocity.Y+2200.0/120) > 1e-9 {
		t.Fatalf("air acceleration: %+v", world.state.Player.Velocity)
	}
	for i := 0; i < 100; i++ {
		movementStep(t, world, PlayerCommand{})
	}
	if world.state.Player.Velocity.Y != -1000 {
		t.Fatalf("terminal velocity %v", world.state.Player.Velocity.Y)
	}
}

func TestJumpArcReleaseAndNoDoubleJump(t *testing.T) {
	held := movementWorld(t, floorCollider())
	held.state.Player.Support = "floor"
	movementStep(t, held, PlayerCommand{Edges: []PlayerEdge{press(settings.Jump)}})
	previous, current := held.Snapshot(context.Background()).Data(context.Background())
	if previous.Player.Velocity.Y != 0 || current.Player.Velocity.Y <= 0 {
		t.Fatal("jump changed the previous authoritative snapshot")
	}
	maxHeight := held.state.Player.Bounds.Y
	for i := 0; i < 100; i++ {
		movementStep(t, held, PlayerCommand{})
		maxHeight = math.Max(maxHeight, held.state.Player.Bounds.Y)
	}
	if maxHeight < 125 || maxHeight > 132 {
		t.Fatalf("held arc rise %v", maxHeight)
	}
	short := movementWorld(t, floorCollider())
	short.state.Player.Support = "floor"
	movementStep(t, short, PlayerCommand{Edges: []PlayerEdge{press(settings.Jump)}})
	movementStep(t, short, PlayerCommand{Edges: []PlayerEdge{release(settings.Jump)}})
	if short.state.Player.Velocity.Y > 280 {
		t.Fatal("release did not cap upward velocity")
	}
	movementStep(t, short, PlayerCommand{Edges: []PlayerEdge{press(settings.Jump)}})
	if short.state.Player.Velocity.Y > 280 {
		t.Fatal("double jump")
	}
}

func TestForgivenessAndBufferedLanding(t *testing.T) {
	for _, extra := range []int{0, 11, 12, 13} {
		floor := Collider{ID: "floor", Kind: Solid, Bounds: Rect{X: -500, Y: -5, Width: 500, Height: 5}}
		world := movementWorld(t, floor)
		world.state.Player.Bounds.X = -29
		world.state.Player.Support = "floor"
		world.state.Player.Velocity.X = 320
		for world.state.Player.Support != "" {
			movementStep(t, world, PlayerCommand{Move: 1})
		}
		for i := 0; i < extra; i++ {
			movementStep(t, world, PlayerCommand{Move: 1})
		}
		movementStep(t, world, PlayerCommand{Move: 1, Edges: []PlayerEdge{press(settings.Jump)}})
		if (world.state.Player.Velocity.Y > 0) != (extra < 12) {
			t.Fatalf("coyote extra %d: %v", extra, world.state.Player.Velocity.Y)
		}
	}
	for _, age := range []int{14, 15} {
		world := movementWorld(t, floorCollider())
		world.state.Player.Bounds.Y = 500
		movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Jump)}})
		for i := 1; i < age; i++ {
			movementStep(t, world, PlayerCommand{})
		}
		world.state.Player.Bounds.Y = 2
		world.state.Player.Velocity.Y = -400
		movementStep(t, world, PlayerCommand{})
		if (world.state.Player.Velocity.Y > 0) != (age == 14) {
			t.Fatalf("buffer age %d: %+v", age, world.state.Player)
		}
	}
}

func TestDropAndPlatformCarry(t *testing.T) {
	world := movementWorld(t, floorCollider(), Collider{ID: "one", Kind: OneWay, Bounds: Rect{X: -50, Y: 96, Width: 100, Height: 4}})
	world.state.Player.Bounds.Y = 100
	world.state.Player.Support = "one"
	movementStep(t, world, PlayerCommand{Down: true, Edges: []PlayerEdge{press(settings.Jump)}})
	if world.state.Player.DropThrough != "one" || world.state.Player.Velocity.Y >= 0 {
		t.Fatal("drop did not start")
	}
	for i := 1; i < dropTicks; i++ {
		movementStep(t, world, PlayerCommand{Down: true})
	}
	if world.state.Player.DropThrough != "one" {
		t.Fatal("drop expired early")
	}
	movementStep(t, world, PlayerCommand{})
	if world.state.Player.DropThrough != "" || world.state.Player.Bounds.Y >= 100 {
		t.Fatal("drop did not expire")
	}
	lift := movementWorld(t)
	lift.state.Platforms = []Platform{{Collider: Collider{ID: "lift", Kind: OneWay, Bounds: Rect{X: -50, Y: -4, Width: 100, Height: 4}}, Velocity: Vector{X: 240}}}
	lift.state.Player.Support = "lift"
	movementStep(t, lift, PlayerCommand{Edges: []PlayerEdge{press(settings.Jump)}})
	if lift.state.Player.Bounds.X != 2 || lift.state.Player.Velocity.X != 0 || lift.state.Player.Velocity.Y < 700 {
		t.Fatalf("launch inherited lift speed: %+v", lift.state.Player)
	}
}

func TestDropLandsOnAnotherSupportBeforeExclusionExpires(t *testing.T) {
	for _, kind := range []ColliderKind{Solid, OneWay} {
		t.Run(string(kind), func(t *testing.T) {
			world := movementWorld(t,
				Collider{ID: "upper", Kind: OneWay, Bounds: Rect{X: -50, Y: 0, Width: 100, Height: 4}},
				Collider{ID: "lower", Kind: kind, Bounds: Rect{X: -50, Y: -4, Width: 100, Height: 4}},
			)
			world.state.Player.Bounds.Y = 4
			world.state.Player.Support = "upper"
			movementStep(t, world, PlayerCommand{Down: true, Edges: []PlayerEdge{press(settings.Jump)}})
			for i := 0; i < dropTicks && world.state.Player.Support != "lower"; i++ {
				movementStep(t, world, PlayerCommand{})
			}
			if world.state.Player.Support != "lower" || world.state.Tick >= dropTicks || world.state.Player.DropThrough != "upper" {
				t.Fatalf("did not land during exclusion: %+v", world.state.Player)
			}
			movementStep(t, world, PlayerCommand{Move: 1, Edges: []PlayerEdge{release(settings.Jump)}})
			if world.state.Player.Velocity.X != 20 {
				t.Fatalf("other support used air acceleration: %+v", world.state.Player)
			}
			movementStep(t, world, PlayerCommand{Move: 1, Edges: []PlayerEdge{press(settings.Jump)}})
			if world.state.Player.Velocity.Y <= 700 || world.state.Player.DropThrough != "upper" {
				t.Fatalf("could not jump while excluding upper platform: %+v", world.state.Player)
			}
		})
	}
}

func TestBufferedJumpOnLowerPlatformDuringDrop(t *testing.T) {
	world := movementWorld(t,
		Collider{ID: "upper", Kind: OneWay, Bounds: Rect{X: -50, Y: 0, Width: 100, Height: 4}},
		Collider{ID: "lower", Kind: OneWay, Bounds: Rect{X: -50, Y: -4, Width: 100, Height: 4}},
	)
	world.state.Player.Bounds.Y = 4
	world.state.Player.Support = "upper"
	movementStep(t, world, PlayerCommand{Down: true, Edges: []PlayerEdge{press(settings.Jump)}})
	movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{release(settings.Jump)}})
	for world.state.Player.Bounds.Y > 2 && world.state.Tick < dropTicks {
		movementStep(t, world, PlayerCommand{})
	}
	if world.state.Player.Support != "" {
		t.Fatal("fixture did not buffer in the air")
	}
	movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Jump)}})
	if world.state.Player.Support != "lower" || world.state.Player.Velocity.Y <= 700 || world.state.Player.DropThrough != "upper" {
		t.Fatalf("buffered jump was not consumed on lower landing: %+v", world.state.Player)
	}
}

func TestLethalChecksCommittedTravel(t *testing.T) {
	t.Run("platform carry and opposite evade", func(t *testing.T) {
		world := movementWorld(t)
		world.state.Platforms = []Platform{{Collider: Collider{ID: "lift", Kind: OneWay, Bounds: Rect{X: -50, Y: -4, Width: 100, Height: 4}}, Velocity: Vector{X: 4800}}}
		world.state.Player.Support = "lift"
		world.lethal = []Rect{{X: 65, Y: 5, Width: 1, Height: 1}}
		movementStep(t, world, PlayerCommand{Aim: Vector{X: -1}, Edges: []PlayerEdge{press(settings.Evade)}})
		if !world.PlayerStatus().Dead || overlaps(world.state.Player.Bounds, world.lethal[0]) {
			t.Fatalf("missed intermediate carry: %+v", world.state.Player)
		}
	})
	for _, tc := range []struct {
		name   string
		volume Rect
		dead   bool
	}{
		{name: "crossed after wall contact", volume: Rect{X: 29.9, Y: 151.55, Width: 0.05, Height: 0.05}, dead: true},
		{name: "net diagonal crosses but actual slide avoids", volume: Rect{X: 1.4, Y: 95.5, Width: 0.01, Height: 0.01}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world := movementWorld(t, Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 30, Y: 0, Width: 4, Height: 200}})
			world.state.Player.Bounds.Y = 100
			world.state.Player.Velocity = Vector{X: 320, Y: -1000}
			world.lethal = []Rect{tc.volume}
			movementStep(t, world, PlayerCommand{Move: 1})
			if world.PlayerStatus().Dead != tc.dead {
				t.Fatalf("slide crossing: %+v, dead=%v", world.state.Player, world.PlayerStatus().Dead)
			}
		})
	}
	t.Run("rejected platform move does not kill", func(t *testing.T) {
		world := movementWorld(t, Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 33, Y: 0, Width: 4, Height: 100}})
		world.state.Platforms = []Platform{{Collider: Collider{ID: "lift", Kind: Solid, Bounds: Rect{X: -20, Y: -4, Width: 100, Height: 4}}, Velocity: Vector{X: 1200}}}
		world.state.Player.Support = "lift"
		world.lethal = []Rect{{X: 32.5, Y: 10, Width: 0.1, Height: 1}}
		movementStep(t, world, PlayerCommand{})
		if world.PlayerStatus().Dead || world.state.Platforms[0].Velocity.X >= 0 {
			t.Fatalf("rejected carry counted as travel: %+v", world.state.Player)
		}
	})
}

func TestEvasionCooldownWallImmunityAndLethal(t *testing.T) {
	wall := Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 40, Y: 0, Width: 2, Height: 100}}
	world := movementWorld(t, floorCollider(), wall)
	world.state.Player.Support = "floor"
	movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Evade)}})
	for tick := 0; tick < evadeTicks; tick++ {
		if world.PlayerStatus().OrdinaryImmune != (tick < evadeImmunityTicks) {
			t.Fatalf("immunity tick %d", tick)
		}
		if world.state.Player.Bounds.X+PlayerWidth > 40 {
			t.Fatal("evaded through wall")
		}
		if tick+1 < evadeTicks {
			movementStep(t, world, PlayerCommand{})
		}
	}
	movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{release(settings.Evade)}})
	movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Evade)}})
	if world.motion.action == EvadeAction {
		t.Fatal("cooldown bypassed")
	}
	movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{release(settings.Evade)}})
	for world.state.Tick < evadeCooldownTicks {
		movementStep(t, world, PlayerCommand{})
	}
	movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Evade)}})
	if world.motion.action != EvadeAction {
		t.Fatal("cooldown did not expire")
	}
	lethal := movementWorld(t, floorCollider())
	lethal.state.Player.Support = "floor"
	lethal.lethal = []Rect{{X: 30, Y: 0, Width: 2, Height: 60}}
	movementStep(t, lethal, PlayerCommand{Edges: []PlayerEdge{press(settings.Evade)}})
	if !lethal.PlayerStatus().Dead || lethal.PlayerStatus().OrdinaryImmune {
		t.Fatal("pit ignored during evade")
	}
}

func TestActionPriorityBufferAndInterruption(t *testing.T) {
	ctx := context.Background()
	world := movementWorld(t, floorCollider())
	world.state.Player.Support = "floor"
	if err := world.StartPlayerAction(ctx, HurtAction, 180*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Evade)}})
	if world.motion.action != HurtAction {
		t.Fatal("evade interrupted hurt")
	}
	if err := world.StartPlayerAction(ctx, AttackAction, time.Second); err == nil {
		t.Fatal("attack interrupted hurt")
	}
	world.SuspendPlayer(ctx)
	if err := world.StepPlayer(ctx, PlayerCommand{}); err == nil {
		t.Fatal("ticked while suspended")
	}
	if err := world.ResumePlayer(ctx, []settings.Action{settings.Throw}); err == nil {
		t.Fatal("resumed with throw held")
	}
	if err := world.ResumePlayer(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if world.motion.buffer != "" || world.motion.held != 0 {
		t.Fatal("menu retained input")
	}
	free := movementWorld(t, floorCollider())
	free.state.Player.Support = "floor"
	movementStep(t, free, PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}})
	if got := free.ConsumeCombatPress(context.Background()); got != settings.Melee {
		t.Fatalf("combat edge %s", got)
	}
	movementStep(t, free, PlayerCommand{Edges: []PlayerEdge{press(settings.Melee)}})
	if got := free.ConsumeCombatPress(context.Background()); got != "" {
		t.Fatal("held attack repeated")
	}
	movementStep(t, free, PlayerCommand{Edges: []PlayerEdge{release(settings.Melee), press(settings.Melee)}})
	if got := free.ConsumeCombatPress(context.Background()); got != settings.Melee {
		t.Fatal("new press lost")
	}
	if err := free.StartPlayerAction(ctx, AttackAction, time.Second); err != nil {
		t.Fatal(err)
	}
	movementStep(t, free, PlayerCommand{Move: 1, Edges: []PlayerEdge{press(settings.Jump)}})
	if free.state.Player.Velocity.Y <= 0 || free.motion.action != AttackAction {
		t.Fatal("jump canceled attack")
	}
	for i := 0; i < 20; i++ {
		movementStep(t, free, PlayerCommand{Move: 1})
	}
	if free.state.Player.Velocity.X > 256 {
		t.Fatal("attack exceeded speed limit")
	}
	if err := free.StartPlayerAction(ctx, HurtAction, time.Second); err != nil || free.motion.action != HurtAction {
		t.Fatal("hurt did not interrupt attack")
	}
}

func TestMotionDeterminismAndInvalidInput(t *testing.T) {
	a, b := movementWorld(t, floorCollider()), movementWorld(t, floorCollider())
	a.state.Player.Support, b.state.Player.Support = "floor", "floor"
	for i := 0; i < 160; i++ {
		command := PlayerCommand{Move: 1}
		if i == 5 {
			command.Edges = []PlayerEdge{press(settings.Jump)}
		}
		if i == 12 {
			command.Edges = []PlayerEdge{release(settings.Jump)}
		}
		if i == 70 {
			command.Edges = []PlayerEdge{press(settings.Evade)}
		}
		movementStep(t, a, command)
		movementStep(t, b, command)
	}
	if !reflect.DeepEqual(a.state, b.state) || a.motion != b.motion {
		t.Fatal("nondeterministic replay")
	}
	before, motion := cloneState(a.state), a.motion
	if err := a.StepPlayer(context.Background(), PlayerCommand{Move: math.NaN()}); err == nil || !reflect.DeepEqual(a.state, before) || a.motion != motion {
		t.Fatal("invalid command changed state")
	}
}

// routeGeometry uses the encounter's actual floor, platforms and solid
// colliders. Dangerous volumes are separate from blocking collision.
type routeGeometry struct {
	colliders []Collider
	danger    []Rect
}

func (geometry *routeGeometry) addRects(prefix string, rects []content.Rect, kind ColliderKind) {
	for i, rect := range rects {
		geometry.colliders = append(geometry.colliders, Collider{ID: fmt.Sprintf("%s-%03d", prefix, i), Kind: kind, Bounds: rectFromContent(rect)})
	}
}

func (geometry *routeGeometry) addDanger(rects []content.Rect) {
	for _, rect := range rects {
		geometry.danger = append(geometry.danger, rectFromContent(rect))
	}
}

func encounterGeometry(floor, bounds content.Rect, platforms, solids, lethal []content.Rect) routeGeometry {
	geometry := routeGeometry{colliders: []Collider{{ID: "floor", Kind: Solid, Bounds: rectFromContent(floor)}}}
	geometry.addRects("platform", platforms, OneWay)
	geometry.addRects("solid", solids, Solid)
	geometry.addDanger(lethal)
	geometry.colliders = append(geometry.colliders,
		Collider{ID: "boundary-left", Kind: Solid, Bounds: Rect{X: bounds.X - 8, Y: bounds.Y, Width: 8, Height: bounds.Height}},
		Collider{ID: "boundary-right", Kind: Solid, Bounds: Rect{X: bounds.X + bounds.Width, Y: bounds.Y, Width: 8, Height: bounds.Height}},
		Collider{ID: "boundary-ceiling", Kind: Solid, Bounds: Rect{X: bounds.X, Y: bounds.Y + bounds.Height, Width: bounds.Width, Height: 8}},
	)
	return geometry
}

func bayRouteGeometry(bay content.ServiceBay, stage content.BayRouteState) routeGeometry {
	bounds := content.Rect{X: 0, Y: -8, Width: bay.Template.Width, Height: bay.Template.Height + 8}
	geometry := encounterGeometry(bay.Template.Floor, bounds, bay.Safety.Platforms, stage.Solids, stage.Lethal)
	geometry.addDanger(stage.Damage)
	for _, terrain := range bay.Template.Terrain {
		kind := ColliderKind(terrain.Kind)
		switch terrain.Kind {
		case "player_platform":
			kind = OneWay
		case "gate":
			kind = Solid
		case "crusher":
			continue // the jump is attempted during its safe inactive phase
		}
		geometry.colliders = append(geometry.colliders, Collider{ID: "terrain-" + terrain.ID, Kind: kind, Bounds: rectFromContent(terrain.Bounds)})
	}
	if bay.Variant.CoverKind == "player_projectile" {
		geometry.colliders = append(geometry.colliders, Collider{ID: "variant-cover", Kind: Solid, Bounds: rectFromContent(bay.Variant.Cover)})
	}
	return geometry
}

func routePlatform(index int) string {
	if index < 0 {
		return "floor"
	}
	return fmt.Sprintf("platform-%03d", index)
}

var errRouteDanger = errors.New("route entered dangerous volume")

// simulateAuthoredJump drives the actual collision solver, not a ballistic
// approximation. Success requires a completely supported collision box.
func simulateAuthoredJump(t *testing.T, geometry routeGeometry, supportID, targetID string, landing content.Rect, jump content.BayJump) error {
	t.Helper()
	geometry.colliders = append([]Collider(nil), geometry.colliders...)
	geometry.addRects("jump-blocker", jump.Blockers, Solid)
	world := movementWorld(t, geometry.colliders...)
	world.lethal = append([]Rect(nil), geometry.danger...)
	var support, target Collider
	for _, collider := range geometry.colliders {
		if collider.ID == supportID {
			support = collider
		}
		if collider.ID == targetID {
			target = collider
		}
	}
	if support.ID == "" || target.ID == "" {
		return fmt.Errorf("missing support %s or target %s", supportID, targetID)
	}
	direction := 1.0
	if jump.Direction == -1 {
		direction = -1
	}
	world.state.Player.Bounds.X = jump.TakeoffX - PlayerWidth/2
	world.state.Player.Bounds.Y = support.Bounds.Y + support.Bounds.Height
	world.state.Player.Support = supportID
	world.state.Player.Velocity.X = direction * jump.Speed
	actualSupport, err := world.grid.Support(context.Background(), world.state.Player.Bounds, nil, "")
	if err != nil || actualSupport != supportID {
		return fmt.Errorf("takeoff support %q, want %q: %v", actualSupport, supportID, err)
	}
	for tick := 0; tick < 150; tick++ {
		command := PlayerCommand{Move: direction * jump.Speed / 320}
		if tick == 0 {
			command.Edges = []PlayerEdge{press(settings.Jump)}
		}
		if err := world.StepPlayer(context.Background(), command); err != nil {
			return fmt.Errorf("tick %d: %w", tick, err)
		}
		body := world.state.Player
		if world.PlayerStatus().Dead {
			return fmt.Errorf("tick %d: %w", tick, errRouteDanger)
		}
		fullyOnTarget := body.Bounds.X >= target.Bounds.X && body.Bounds.X+body.Bounds.Width <= target.Bounds.X+target.Bounds.Width
		fullyOnLanding := body.Bounds.X >= landing.X && body.Bounds.X+body.Bounds.Width <= landing.X+landing.Width
		atTop := math.Abs(body.Bounds.Y-target.Bounds.Y-target.Bounds.Height) <= Tolerance
		if body.Support == targetID && fullyOnTarget && fullyOnLanding && atTop {
			return nil
		}
		if body.Support != "" && body.Support != targetID {
			return fmt.Errorf("tick %d: landed on %s instead of %s", tick, body.Support, targetID)
		}
		if math.Abs(body.Velocity.X-direction*jump.Speed) > 1e-7 {
			return fmt.Errorf("tick %d: trajectory blocked at %+v", tick, body.Bounds)
		}
	}
	return fmt.Errorf("landing %s not fully reached: %+v", targetID, world.state.Player)
}

func TestAuthoredMandatoryJumpReachability(t *testing.T) {
	_, _, bays := campaignFixture(t)
	seenLocations := map[content.LocationID]bool{}
	for _, bay := range bays {
		for _, stage := range bay.Safety.States {
			geometry := bayRouteGeometry(bay, stage)
			for _, jump := range bay.Template.Jumps {
				supportID := "floor"
				if jump.Support != "" {
					supportID = "terrain-" + jump.Support
				}
				var landing content.Rect
				for _, terrain := range bay.Template.Terrain {
					if terrain.ID == jump.Landing {
						landing = terrain.Bounds
					}
				}
				if err := simulateAuthoredJump(t, geometry, supportID, "terrain-"+jump.Landing, landing, jump); err != nil {
					t.Fatalf("%s %s %s: %v", bay.Deed, stage.Name, jump.ID, err)
				}
			}
			for _, route := range bay.Safety.Jumps {
				landing := bay.Safety.Platforms[route.Target]
				width := math.Min(landing.Width, 128)
				if route.Jump.Direction == -1 {
					landing.X += landing.Width - width
				}
				landing.Width = width
				if err := simulateAuthoredJump(t, geometry, routePlatform(route.Support), routePlatform(route.Target), landing, route.Jump); err != nil {
					t.Fatalf("%s %s %s: %v", bay.Deed, stage.Name, route.Jump.ID, err)
				}
			}
		}
		layout := bay.Institution
		if seenLocations[layout.Location] {
			continue
		}
		seenLocations[layout.Location] = true
		boss := layout.Boss
		geometry := encounterGeometry(boss.Floor, boss.Bounds, boss.Platforms, boss.Solids, boss.Lethal)
		for _, route := range boss.Jumps {
			landing := boss.Platforms[route.Target]
			width := math.Min(landing.Width, 160)
			if route.Jump.Direction == -1 {
				landing.X += landing.Width - width
			}
			landing.Width = width
			if err := simulateAuthoredJump(t, geometry, routePlatform(route.Support), routePlatform(route.Target), landing, route.Jump); err != nil {
				t.Fatalf("%s %s: %v", boss.Encounter, route.Jump.ID, err)
			}
		}
		for _, room := range layout.Prologue {
			geometry := encounterGeometry(room.Floor, room.Bounds, room.Platforms, room.Solids, room.Lethal)
			for _, route := range room.Jumps {
				if err := simulateAuthoredJump(t, geometry, routePlatform(route.Support), routePlatform(route.Target), room.Platforms[route.Target], route.Jump); err != nil {
					t.Fatalf("%s %s: %v", room.Encounter, route.Jump.ID, err)
				}
			}
		}
	}
}

func TestAuthoredRouteObstaclesAndActiveHazard(t *testing.T) {
	_, _, bays := campaignFixture(t)
	var privacy, utilities *content.ServiceBay
	for i := range bays {
		bay := &bays[i]
		if bay.Variant.Number != 1 {
			continue
		}
		if privacy == nil && bay.Template.ID == "BAY-PRV" {
			privacy = bay
		}
		if utilities == nil && bay.Template.ID == "BAY-UTL" {
			utilities = bay
		}
	}
	if privacy == nil || utilities == nil {
		t.Fatal("missing authored fixtures")
	}
	jump := privacy.Template.Jumps[0]
	var landing content.Rect
	for _, terrain := range privacy.Template.Terrain {
		if terrain.ID == jump.Landing {
			landing = terrain.Bounds
		}
	}
	geometry := bayRouteGeometry(*privacy, privacy.Safety.States[0])
	if err := simulateAuthoredJump(t, geometry, "floor", "terrain-"+jump.Landing, landing, jump); err != nil {
		t.Fatalf("unobstructed route: %v", err)
	}
	blocked := bayRouteGeometry(*privacy, privacy.Safety.States[0])
	blocked.colliders = append(blocked.colliders, Collider{ID: "test-wall", Kind: Solid, Bounds: Rect{X: 540, Y: 90, Width: 12, Height: 140}})
	if err := simulateAuthoredJump(t, blocked, "floor", "terrain-"+jump.Landing, landing, jump); err == nil {
		t.Fatal("jump ignored a blocking solid")
	}
	blockedJump := jump
	blockedJump.Blockers = append([]content.Rect(nil), jump.Blockers...)
	blockedJump.Blockers = append(blockedJump.Blockers, content.Rect{X: 540, Y: 90, Width: 12, Height: 140})
	if err := simulateAuthoredJump(t, geometry, "floor", "terrain-"+jump.Landing, landing, blockedJump); err == nil {
		t.Fatal("jump ignored its authored blocker")
	}
	partial := landing
	partial.X += 10
	partial.Width = PlayerWidth
	if err := simulateAuthoredJump(t, geometry, "floor", "terrain-"+jump.Landing, partial, jump); err == nil {
		t.Fatal("partial landing counted as fully supported")
	}
	jump = utilities.Template.Jumps[1]
	for _, terrain := range utilities.Template.Terrain {
		if terrain.ID == jump.Landing {
			landing = terrain.Bounds
		}
	}
	geometry = bayRouteGeometry(*utilities, utilities.Safety.States[0])
	if err := simulateAuthoredJump(t, geometry, "floor", "terrain-"+jump.Landing, landing, jump); err != nil {
		t.Fatalf("inactive hazard route: %v", err)
	}
	if len(utilities.Hazards) == 0 {
		t.Fatal("missing authored hazard")
	}
	geometry.danger = append(geometry.danger, rectFromContent(utilities.Hazards[0].Bounds))
	if err := simulateAuthoredJump(t, geometry, "floor", "terrain-"+jump.Landing, landing, jump); !errors.Is(err, errRouteDanger) {
		t.Fatalf("active authored hazard did not reject jump: %v", err)
	}
}

func TestCombatBufferLastTick(t *testing.T) {
	for _, age := range []int{14, 15} {
		world := movementWorld(t, floorCollider())
		world.state.Player.Support = "floor"
		movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Throw)}})
		for i := 1; i <= age; i++ {
			movementStep(t, world, PlayerCommand{})
		}
		if (world.ConsumeCombatPress(context.Background()) == settings.Throw) != (age == 14) {
			t.Fatalf("throw buffer age %d", age)
		}
	}
}

func TestJumpReachabilityLimits(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rise      float64
		distance  float64
		reachable bool
	}{
		{name: "maximum rise", rise: 100, distance: 100, reachable: true},
		{name: "maximum gap", rise: 0, distance: 180, reachable: true},
		{name: "combined limit needs its own check", rise: 100, distance: 180, reachable: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			support := Collider{ID: "support", Kind: OneWay, Bounds: Rect{X: -500, Y: -4, Width: 514, Height: 4}}
			landing := Collider{ID: "landing", Kind: OneWay, Bounds: Rect{X: tc.distance, Y: tc.rise - 4, Width: 160, Height: 4}}
			world := movementWorld(t, support, landing)
			world.state.Player.Bounds.X = -14
			world.state.Player.Support = "support"
			world.state.Player.Velocity.X = 320
			movementStep(t, world, PlayerCommand{Move: 1, Edges: []PlayerEdge{press(settings.Jump)}})
			fullyLanded := false
			for i := 0; i < 120; i++ {
				if world.state.Player.Support == "landing" {
					fullyLanded = world.state.Player.Bounds.X >= tc.distance
					break
				}
				movementStep(t, world, PlayerCommand{Move: 1})
			}
			if fullyLanded != tc.reachable {
				t.Fatalf("reachability: landed=%v, body=%+v", fullyLanded, world.state.Player)
			}
		})
	}
}

func TestSimultaneousEdgesAndFall(t *testing.T) {
	world := movementWorld(t, floorCollider())
	world.state.Player.Support = "floor"
	movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Jump), press(settings.Evade)}})
	if world.PlayerStatus().Action != EvadeAction || world.state.Player.Velocity.Y > 0 {
		t.Fatal("evade did not take priority over jump")
	}
	fall := movementWorld(t, floorCollider())
	fall.state.Player.Bounds.Y = 1000
	for i := 0; i < 200; i++ {
		movementStep(t, fall, PlayerCommand{})
	}
	if fall.state.Health != 100 || fall.PlayerStatus().Dead || fall.state.Player.Support != "floor" {
		t.Fatal("ordinary fall caused damage")
	}
}

func TestHurtPreventsOneWayDrop(t *testing.T) {
	world := movementWorld(t, Collider{ID: "one", Kind: OneWay, Bounds: Rect{X: -50, Y: -4, Width: 100, Height: 4}})
	world.state.Player.Support = "one"
	if err := world.StartPlayerAction(context.Background(), HurtAction, 180*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	movementStep(t, world, PlayerCommand{Down: true, Edges: []PlayerEdge{press(settings.Jump)}})
	if world.state.Player.DropThrough != "" || world.state.Player.Support != "one" {
		t.Fatal("dropped through platform during hurt stun")
	}
}

func TestCrusherIsLethalDuringEvasion(t *testing.T) {
	wall := Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 28, Y: 0, Width: 4, Height: 100}}
	world := movementWorld(t, floorCollider(), wall)
	world.state.Player.Support = "floor"
	world.state.Platforms = []Platform{{Collider: Collider{ID: "crusher", Kind: Solid, Bounds: Rect{X: -4, Y: 0, Width: 4, Height: 58}}, Velocity: Vector{X: 1200}, Crusher: true}}
	movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Evade)}})
	if !world.state.Player.Crushed || !world.PlayerStatus().Dead {
		t.Fatal("crusher did not kill the evading player")
	}
}

func TestHurtCancelsEvasionAndLimbSpeedChange(t *testing.T) {
	ctx := context.Background()
	world := movementWorld(t, floorCollider())
	world.state.Player.Support = "floor"
	movementStep(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Evade)}})
	if !world.PlayerStatus().OrdinaryImmune {
		t.Fatal("evade did not protect")
	}
	if err := world.StartPlayerAction(ctx, HurtAction, 180*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if world.PlayerStatus().OrdinaryImmune || world.PlayerStatus().Action != HurtAction {
		t.Fatal("hurt failed to interrupt evade")
	}
	world.motion.action = FreeAction
	world.state.Player.Velocity.X = 320
	if err := world.SetPlayerLimbs(ctx, LeftArm|RightArm); err != nil {
		t.Fatal(err)
	}
	movementStep(t, world, PlayerCommand{Move: 1})
	if world.state.Player.Velocity.X > 120 {
		t.Fatal("leg loss left speed above its maximum")
	}
}

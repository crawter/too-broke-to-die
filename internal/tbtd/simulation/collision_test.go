// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"math"
	"os"
	"reflect"
	"slices"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
)

func gridFixture(t *testing.T, colliders ...Collider) *Grid {
	t.Helper()
	grid, err := NewGrid(context.Background(), colliders)
	if err != nil {
		t.Fatal(err)
	}
	return grid
}

func TestGridOrderingAndValidation(t *testing.T) {
	ctx := context.Background()
	items := []Collider{
		{ID: "z", Kind: Solid, Bounds: Rect{X: -130, Y: -4, Width: 300, Height: 4}},
		{ID: "a", Kind: Solid, Bounds: Rect{X: 128, Y: 0, Width: 1, Height: 20}},
		{ID: "far", Kind: OneWay, Bounds: Rect{X: 1000, Width: 30, Height: 4}},
	}
	first := gridFixture(t, items...)
	slices.Reverse(items)
	second := gridFixture(t, items...)
	a, err := first.Query(ctx, Rect{X: 127, Y: -1, Width: 2, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Query(ctx, Rect{X: 127, Y: -1, Width: 2, Height: 2})
	if err != nil || !reflect.DeepEqual(a, b) || len(a) != 2 || a[0].ID != "a" || a[1].ID != "z" {
		t.Fatalf("query order: %v %v", a, err)
	}
	a[0].ID = "changed"
	again, err := first.Query(ctx, Rect{X: 128, Width: 1, Height: 1})
	if err != nil || again[0].ID != "a" {
		t.Fatalf("query aliases grid: %v %v", again, err)
	}
	for _, bad := range [][]Collider{
		{{ID: "x", Kind: Solid, Bounds: Rect{Width: 1, Height: 1}}, {ID: "x", Kind: Solid, Bounds: Rect{Width: 1, Height: 1}}},
		{{ID: "x", Kind: Solid, Bounds: Rect{X: math.NaN(), Width: 1, Height: 1}}},
		{{ID: "x", Kind: "debris", Bounds: Rect{Width: 1, Height: 1}}},
	} {
		if _, err := NewGrid(ctx, bad); err == nil {
			t.Fatalf("accepted invalid colliders: %v", bad)
		}
	}
	if _, err := first.Query(ctx, Rect{X: math.Inf(1), Width: 1, Height: 1}); err == nil {
		t.Fatal("accepted invalid query")
	}
}

func TestSweepsEarliestTieAndThinTargets(t *testing.T) {
	ctx := context.Background()
	colliders := []Collider{
		{ID: "wall", Kind: Solid, Bounds: Rect{X: 80, Y: -4, Width: 1, Height: 8}},
		{ID: "paddle-b", Kind: ProjectileTarget, Bounds: Rect{X: 20, Y: -4, Width: 1, Height: 8}},
		{ID: "paddle-a", Kind: ProjectileTarget, Bounds: Rect{X: 20, Y: -4, Width: 1, Height: 8}},
		{ID: "player-only", Kind: PlayerOnly, Bounds: Rect{X: 10, Y: -4, Width: 2, Height: 8}},
	}
	for _, order := range [][]Collider{colliders, {colliders[3], colliders[2], colliders[1], colliders[0]}} {
		grid := gridFixture(t, order...)
		hit, found, err := grid.Sweep(ctx, Rect{}, Vector{X: 200}, nil, SweepOptions{Projectile: true})
		if err != nil || !found || hit.Collider.ID != "paddle-a" || hit.Time != 0.1 || hit.Normal.X != -1 {
			t.Fatalf("sweep: %+v %v %v", hit, found, err)
		}
		hit, found, err = grid.Sweep(ctx, Rect{}, Vector{X: 200}, nil, SweepOptions{Projectile: true, Ignore: "paddle-a"})
		if err != nil || !found || hit.Collider.ID != "paddle-b" {
			t.Fatalf("ignored target: %+v %v %v", hit, found, err)
		}
	}
	wall := gridFixture(t, colliders[0])
	hit, found, err := wall.Sweep(ctx, Rect{}, Vector{X: 200}, nil, SweepOptions{Projectile: true})
	if err != nil || !found || hit.Collider.ID != "wall" || hit.Time != 0.4 {
		t.Fatalf("thin wall: %+v %v %v", hit, found, err)
	}
	if _, _, err := wall.Sweep(ctx, Rect{}, Vector{X: 1}, []Collider{colliders[0]}, SweepOptions{Projectile: true}); err == nil {
		t.Fatal("duplicate dynamic identity accepted")
	}
}

func TestSolidOneWayAndSupport(t *testing.T) {
	ctx := context.Background()
	grid := gridFixture(t,
		Collider{ID: "floor", Kind: Solid, Bounds: Rect{X: -100, Y: -5, Width: 200, Height: 5}},
		Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 10, Y: 0, Width: 1, Height: 50}},
		Collider{ID: "one", Kind: OneWay, Bounds: Rect{X: -20, Y: 10, Width: 25, Height: 2}},
	)
	body := Rect{X: 0, Y: 0, Width: 2, Height: 2}
	up, err := grid.Move(ctx, body, Vector{Y: 20}, nil, "")
	if err != nil || up.Bounds.Y != 20 {
		t.Fatalf("one-way blocked ascent: %+v %v", up, err)
	}
	down, err := grid.Move(ctx, up.Bounds, Vector{Y: -30}, nil, "")
	if err != nil || down.Bounds.Y != 12 || len(down.Contacts) != 1 || down.Contacts[0].Collider.ID != "one" {
		t.Fatalf("one-way landing: %+v %v", down, err)
	}
	dropped, err := grid.Move(ctx, down.Bounds, Vector{Y: -30}, nil, "one")
	if err != nil || dropped.Bounds.Y != 0 || dropped.Contacts[0].Collider.ID != "floor" {
		t.Fatalf("drop: %+v %v", dropped, err)
	}
	wall, err := grid.Move(ctx, body, Vector{X: 100}, nil, "")
	if err != nil || wall.Bounds.X != 8 || wall.Contacts[0].Collider.ID != "wall" {
		t.Fatalf("wall: %+v %v", wall, err)
	}
	for _, tc := range []struct {
		y    float64
		want string
	}{{12, "one"}, {12.999, "one"}, {13.001, ""}, {0, "floor"}} {
		got, err := grid.Support(ctx, Rect{X: 0, Y: tc.y, Width: 2, Height: 2}, nil, "")
		if err != nil || got != tc.want {
			t.Fatalf("support %v: %q %v", tc.y, got, err)
		}
	}
	if _, err := grid.Move(ctx, Rect{X: 10, Y: 1, Width: 2, Height: 2}, Vector{X: 1}, nil, ""); err == nil {
		t.Fatal("initial solid overlap accepted")
	}
	nudged, err := grid.Move(ctx, Rect{X: 0, Y: -Tolerance / 2, Width: 2, Height: 2}, Vector{X: 1}, nil, "")
	if err != nil || nudged.Bounds.Y != 0 || nudged.Bounds.X != 1 {
		t.Fatalf("tolerance correction: %+v %v", nudged, err)
	}
	if _, err := grid.Move(ctx, Rect{X: 0, Y: -Tolerance * 2, Width: 2, Height: 2}, Vector{X: 1}, nil, ""); err == nil {
		t.Fatal("large penetration accepted")
	}
}

func TestContactResolutionStopsAtBoundaries(t *testing.T) {
	ctx := context.Background()
	grid := gridFixture(t,
		Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 4, Y: -10, Width: 1, Height: 30}},
		Collider{ID: "ceiling", Kind: Solid, Bounds: Rect{X: -10, Y: 6, Width: 30, Height: 1}},
	)
	result, err := grid.Move(ctx, Rect{Width: 1, Height: 1}, Vector{X: 8, Y: 10}, nil, "")
	if err != nil || result.Bounds.X != 3 || result.Bounds.Y != 5 || len(result.Contacts) != 2 || len(result.Contacts) > 4 || result.Diagnostic != "" {
		t.Fatalf("corner movement: %+v %v", result, err)
	}
}

func platformWorld(t *testing.T, crusher bool, wall bool) *World {
	t.Helper()
	var terrain []Collider
	if wall {
		terrain = append(terrain, Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 5, Y: 2, Width: 1, Height: 5}})
	}
	grid := gridFixture(t, terrain...)
	state := State{
		Encounter: "ENC-HOS-01", HasPlayer: true,
		Player:    Body{ID: "player", Bounds: Rect{X: 2, Y: 2, Width: 2, Height: 2}, Support: "platform"},
		Platforms: []Platform{{Collider: Collider{ID: "platform", Kind: Solid, Bounds: Rect{Width: 10, Height: 2}}, Velocity: Vector{X: 240}, Crusher: crusher}},
	}
	return &World{grid: grid, state: state, previous: cloneState(state)}
}

func TestPlatformCarryReversalAndCrusher(t *testing.T) {
	ctx := context.Background()
	world := platformWorld(t, false, false)
	candidates, err := world.Query(ctx, Rect{Width: 1, Height: 1})
	if err != nil || len(candidates) != 1 || candidates[0].ID != "platform" {
		t.Fatalf("platform query: %+v %v", candidates, err)
	}
	candidates[0].Bounds.X = 99
	if world.state.Platforms[0].Collider.Bounds.X != 0 {
		t.Fatal("query exposed platform state")
	}
	if err := world.Step(ctx, TickInput{Velocity: Vector{Y: 120}}); err != nil {
		t.Fatal(err)
	}
	previous, current := world.Snapshot(ctx).Data(ctx)
	if previous.Player.Bounds.X != 2 || current.Player.Bounds.X != 4 || current.Player.Bounds.Y != 3 || current.Player.Velocity != (Vector{Y: 120}) || current.Platforms[0].Collider.Bounds.X != 2 {
		t.Fatalf("carry or launch: %+v", current)
	}
	if err := world.Step(ctx, TickInput{Velocity: Vector{Y: 120}}); err != nil {
		t.Fatal(err)
	}
	_, current = world.Snapshot(ctx).Data(ctx)
	if current.Player.Bounds.X != 4 || current.Player.Bounds.Y != 4 {
		t.Fatalf("airborne rider inherited platform movement: %+v", current)
	}
	world = platformWorld(t, false, true)
	if err := world.Step(ctx, TickInput{}); err != nil {
		t.Fatal(err)
	}
	_, current = world.Snapshot(ctx).Data(ctx)
	if current.Player.Bounds.X != 0 || current.Platforms[0].Collider.Bounds.X != -2 || current.Platforms[0].Velocity.X != -240 || current.Player.Crushed {
		t.Fatalf("failed reversal: %+v", current)
	}
	world = platformWorld(t, true, true)
	if err := world.Step(ctx, TickInput{}); err != nil {
		t.Fatal(err)
	}
	_, current = world.Snapshot(ctx).Data(ctx)
	if !current.Player.Crushed || current.Platforms[0].Velocity.X != 240 {
		t.Fatalf("crusher failed: %+v", current)
	}
}

func TestAscendingOneWayCatchesAirbornePlayer(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name        string
		playerY     float64
		platformY   float64
		velocityY   float64
		input       TickInput
		drop        string
		wantY       float64
		wantVY      float64
		wantSupport string
	}{
		{name: "catch and jump", playerY: 4, velocityY: 480, input: TickInput{Velocity: Vector{Y: 120}}, wantY: 7, wantVY: 120},
		{name: "catch and land", playerY: 4, velocityY: 480, input: TickInput{Velocity: Vector{Y: -120}}, wantY: 6, wantSupport: "lift"},
		{name: "body below top", playerY: 0, velocityY: 480, wantY: 0},
		{name: "platform descending", playerY: 4, velocityY: -480, wantY: 4},
		{name: "drop exclusion", playerY: 4, velocityY: 480, input: TickInput{Velocity: Vector{Y: -120}, DropThrough: "lift"}, drop: "lift", wantY: 3, wantVY: -120},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grid := gridFixture(t)
			state := State{HasPlayer: true}
			state.Player = Body{ID: "player", Bounds: Rect{X: 2, Y: tc.playerY, Width: 2, Height: 2}, DropThrough: tc.drop}
			state.Platforms = []Platform{{Collider: Collider{ID: "lift", Kind: OneWay, Bounds: Rect{Y: tc.platformY, Width: 10, Height: 2}}, Velocity: Vector{Y: tc.velocityY}}}
			world := &World{grid: grid, state: state}
			if err := world.Step(ctx, tc.input); err != nil {
				t.Fatal(err)
			}
			_, got := world.Snapshot(ctx).Data(ctx)
			if got.Player.Bounds.Y != tc.wantY || got.Player.Velocity.Y != tc.wantVY || got.Player.Support != tc.wantSupport {
				t.Fatalf("player after platform movement: %+v", got.Player)
			}
		})
	}
}

func TestTickSharesContactBudgetAcrossPlatformsAndActor(t *testing.T) {
	ctx := context.Background()
	for _, move := range []bool{false, true} {
		grid := gridFixture(t, Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 5, Y: -1, Width: 1, Height: 10}})
		state := State{Encounter: "ENC-HOS-01", HasPlayer: true}
		state.Player = Body{ID: "player", Bounds: Rect{X: 2, Width: 2, Height: 8}}
		state.Platforms = []Platform{
			{Collider: Collider{ID: "a", Kind: Solid, Bounds: Rect{Width: 1, Height: 1}}, Velocity: Vector{X: 360}},
			{Collider: Collider{ID: "b", Kind: Solid, Bounds: Rect{Y: 3, Width: 1, Height: 1}}, Velocity: Vector{X: 360}},
		}
		world := &World{grid: grid, state: state}
		var input TickInput
		if move {
			input.Velocity.X = 120
		}
		if err := world.Step(ctx, input); err != nil {
			t.Fatal(err)
		}
		_, got := world.Snapshot(ctx).Data(ctx)
		if got.Player.Bounds.X != 2 || got.Platforms[0].Collider.Bounds.X != -3 || got.Platforms[1].Collider.Bounds.X != -3 {
			t.Fatalf("unresolved platform movement crossed wall: %+v", got)
		}
		if move {
			want := []Diagnostic{{Name: CollisionIterationLimit, Encounter: "ENC-HOS-01", Entity: "player"}}
			if !reflect.DeepEqual(got.Diagnostics, want) {
				t.Fatalf("missing tick-wide limit diagnostic: %+v", got.Diagnostics)
			}
		} else if len(got.Diagnostics) != 0 {
			t.Fatalf("diagnosed without unresolved motion: %+v", got.Diagnostics)
		}
	}

	grid := gridFixture(t, Collider{ID: "wall", Kind: Solid, Bounds: Rect{X: 5, Y: -1, Width: 1, Height: 10}})
	state := State{Encounter: "ENC-HOS-01", HasPlayer: true}
	state.Player = Body{ID: "player", Bounds: Rect{X: 2, Width: 2, Height: 8}}
	state.Platforms = []Platform{
		{Collider: Collider{ID: "a", Kind: Solid, Bounds: Rect{Width: 1, Height: 1}}, Velocity: Vector{X: 360}},
		{Collider: Collider{ID: "b", Kind: Solid, Bounds: Rect{Y: 3, Width: 1, Height: 1}}, Velocity: Vector{X: 360}},
		{Collider: Collider{ID: "c", Kind: Solid, Bounds: Rect{Y: 6, Width: 1, Height: 1}}, Velocity: Vector{X: 360}},
	}
	world := &World{grid: grid, state: state}
	if err := world.Step(ctx, TickInput{}); err != nil {
		t.Fatal(err)
	}
	_, got := world.Snapshot(ctx).Data(ctx)
	if got.Player.Bounds.X != 2 || got.Platforms[2].Collider.Bounds.X != 0 || len(got.Diagnostics) != 1 || got.Diagnostics[0].Name != CollisionIterationLimit {
		t.Fatalf("platform advanced after budget exhaustion: %+v", got)
	}
}

func TestTickContactBudgetIncludesCarryAndReverse(t *testing.T) {
	ctx := context.Background()
	grid := gridFixture(t,
		Collider{ID: "left", Kind: Solid, Bounds: Rect{X: 1, Y: 3, Width: 1, Height: 1}},
		Collider{ID: "right", Kind: Solid, Bounds: Rect{X: 5, Y: 2, Width: 1, Height: 2}},
	)
	state := State{Encounter: "ENC-HOS-01", HasPlayer: true}
	state.Player = Body{ID: "player", Bounds: Rect{X: 2, Y: 2, Width: 2, Height: 2}, Support: "a-ride"}
	state.Platforms = []Platform{
		{Collider: Collider{ID: "a-ride", Kind: Solid, Bounds: Rect{Width: 10, Height: 2}}, Velocity: Vector{X: 240}},
		{Collider: Collider{ID: "b-push", Kind: Solid, Bounds: Rect{Y: 2.1, Width: 1, Height: 0.5}}, Velocity: Vector{X: 360}},
	}
	world := &World{grid: grid, state: state}
	if err := world.Step(ctx, TickInput{Velocity: Vector{X: 120}}); err != nil {
		t.Fatal(err)
	}
	_, got := world.Snapshot(ctx).Data(ctx)
	if got.Player.Bounds.X != 2 || got.Platforms[0].Collider.Bounds.X != 0 || got.Platforms[1].Collider.Bounds.X != -3 || len(got.Diagnostics) != 1 || got.Diagnostics[0].Name != CollisionIterationLimit {
		t.Fatalf("carry and reverse did not share budget: %+v", got)
	}
}

func TestDropExcludesOnlyCurrentOneWay(t *testing.T) {
	ctx := context.Background()
	grid := gridFixture(t,
		Collider{ID: "floor", Kind: Solid, Bounds: Rect{X: -20, Y: -1, Width: 40, Height: 1}},
		Collider{ID: "one", Kind: OneWay, Bounds: Rect{X: -10, Y: 10, Width: 20, Height: 2}},
	)
	state := State{HasPlayer: true, Player: Body{ID: "player", Bounds: Rect{Y: 12, Width: 2, Height: 2}, Support: "one"}}
	world := &World{grid: grid, state: state}
	if err := world.Step(ctx, TickInput{Velocity: Vector{Y: -120}, DropThrough: "floor"}); err == nil {
		t.Fatal("solid floor excluded")
	}
	if err := world.Step(ctx, TickInput{Velocity: Vector{Y: -120}, DropThrough: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := world.Step(ctx, TickInput{Velocity: Vector{Y: -120}, DropThrough: "one"}); err != nil {
		t.Fatal(err)
	}
	_, current := world.Snapshot(ctx).Data(ctx)
	if current.Player.Bounds.Y != 10 || current.Player.Support != "" {
		t.Fatalf("one-way did not remain excluded: %+v", current.Player)
	}
}

func campaignFixture(t *testing.T) (*campaign.Campaign, *content.Campaign, []content.ServiceBay) {
	t.Helper()
	ctx := context.Background()
	fsys := os.DirFS("../../..")
	manifest, err := content.LoadCampaign(ctx, fsys, "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := campaign.New(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	bays, err := content.LoadServiceBays(ctx, fsys, "content/data/encounters/templates.json", "content/data/encounters/variants.json", manifest)
	if err != nil {
		t.Fatal(err)
	}
	return owner, manifest, bays
}

func certifyFixture(t *testing.T, owner *campaign.Campaign, manifest *content.Campaign, snapshot campaign.Snapshot, id content.DeedID) campaign.Snapshot {
	t.Helper()
	ctx := context.Background()
	active, err := owner.Accept(ctx, snapshot, id)
	if err != nil {
		t.Fatalf("accept %s: %v", id, err)
	}
	objects := make(map[content.ObjectID]content.Object)
	for _, object := range manifest.Objects(ctx) {
		objects[object.ID] = object
	}
	for _, deed := range manifest.Deeds(ctx) {
		if deed.Deed.ID != id {
			continue
		}
		attempt, ok := active.ActiveAttempt()
		if !ok {
			t.Fatal("no active attempt")
		}
		event := campaign.Certification{Deed: id, Attempt: attempt.Number, Evidence: deed.Evidence, Trigger: deed.Trigger, HoldMilliseconds: deed.Deed.HoldMilliseconds}
		for _, objectID := range deed.Deed.Objects {
			object := objects[objectID]
			if object.Deed == id {
				event.EvidenceObjects = append(event.EvidenceObjects, objectID)
			}
			if object.Role == content.RoleTerminal {
				event.Terminal = objectID
			}
		}
		result, _, applyErr := owner.Apply(ctx, active, event)
		if applyErr != nil {
			t.Fatalf("certify %s: %v", id, applyErr)
		}
		return result
	}
	t.Fatalf("missing deed %s", id)
	return campaign.Snapshot{}
}

func TestWorldRestorationAndIsolation(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	world, err := NewWorld(ctx, owner, manifest, campaign.Snapshot{}, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, initial := world.Snapshot(ctx).Data(ctx)
	if initial.Anchor != "ANC-PRO-01" || initial.Player.Bounds != (Rect{X: 18, Width: 28, Height: 58}) || initial.Health != 100 || initial.Limbs != 4 || initial.Feces != 5 || initial.Mode != "ST-PAU" {
		t.Fatalf("prologue spawn: %+v", initial)
	}
	permuted := slices.Clone(bays)
	slices.Reverse(permuted)
	other, err := NewWorld(ctx, owner, manifest, campaign.Snapshot{}, permuted, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, otherState := other.Snapshot(ctx).Data(ctx)
	if !reflect.DeepEqual(initial, otherState) {
		t.Fatal("world construction depends on insertion order")
	}
	if _, err := NewWorld(ctx, owner, manifest, campaign.Snapshot{}, bays, []Platform{{Collider: Collider{ID: "overlap", Kind: Solid, Bounds: Rect{X: 18, Width: 5, Height: 5}}}}); err == nil {
		t.Fatal("overlapping platform spawn accepted")
	}
	if err := world.Step(ctx, TickInput{Velocity: Vector{X: 120}}); err != nil {
		t.Fatal(err)
	}
	prev, current := world.Snapshot(ctx).Data(ctx)
	if prev.Player.Bounds.X != 18 || current.Player.Bounds.X != 19 || current.Tick != 1 {
		t.Fatalf("authoritative snapshots: %+v %+v", prev, current)
	}
	snapshot := world.Snapshot(ctx)
	prev.Player.Bounds.X = 999
	current.Player.Bounds.X = 999
	_, again := snapshot.Data(ctx)
	if again.Player.Bounds.X != 19 {
		t.Fatal("snapshot alias")
	}
	bad := bays[0].Institution.Prologue[0].Spawn
	bays[0].Institution.Prologue[0].Spawn.X = 1
	if _, err := NewWorld(ctx, owner, manifest, campaign.Snapshot{}, bays, nil); err == nil {
		t.Fatal("spawn intersecting boundary accepted")
	}
	bays[0].Institution.Prologue[0].Spawn = bad
	broken := slices.Clone(bays)
	for i := range broken {
		if broken[i].Institution.Location != "LOC-HOS" {
			continue
		}
		layout := broken[i].Institution
		layout.Prologue = slices.Clone(layout.Prologue)
		layout.Prologue[0].Solids = append(slices.Clone(layout.Prologue[0].Solids), content.Rect{X: 20, Y: 0, Width: 4, Height: 4})
		broken[i].Institution = layout
	}
	if _, err := NewWorld(ctx, owner, manifest, campaign.Snapshot{}, broken, nil); err == nil {
		t.Fatal("solid overlapping the spawn accepted")
	}
	hit, found, err := world.SweepTargets(ctx, Rect{X: 40, Y: 10}, Vector{X: 100}, []Collider{{ID: "target", Kind: ProjectileTarget, Bounds: Rect{X: 60, Y: 8, Width: 1, Height: 4}}}, SweepOptions{Projectile: true})
	if err != nil || !found || hit.Collider.ID != "target" {
		t.Fatalf("world projectile target: %+v %v %v", hit, found, err)
	}
	saved, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range manifest.Batches(ctx)[0].Deeds {
		saved = certifyFixture(t, owner, manifest, saved, id)
	}
	world, err = NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, current = world.Snapshot(ctx).Data(ctx)
	if current.Anchor != "ANC-HOS-01" || len(current.Receipts) != 6 || len(current.Emergencies) != 1 || current.Emergencies[0].WarningTicks != 180 || !current.Emergencies[0].SafeBoundary || !current.Emergencies[0].BypassUsable || len(current.Routes) == 0 {
		t.Fatalf("emergency restoration: %+v", current)
	}
	current.Receipts[0] = "BAD"
	current.Emergencies[0].WarningTicks = 1
	_, again = world.Snapshot(ctx).Data(ctx)
	if again.Receipts[0] == "BAD" || again.Emergencies[0].WarningTicks != 180 {
		t.Fatal("nested snapshot alias")
	}
	if err := world.Step(ctx, TickInput{}); err != nil {
		t.Fatal(err)
	}
	_, current = world.Snapshot(ctx).Data(ctx)
	if current.Emergencies[0].WarningTicks != 179 {
		t.Fatal("warning did not start on first tick")
	}
	restored, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, current = restored.Snapshot(ctx).Data(ctx)
	if current.Tick != 0 || current.Emergencies[0].WarningTicks != 180 || current.Player.Bounds.Y != 0 {
		t.Fatalf("transient state survived restoration: %+v", current)
	}
}

func TestActiveAttemptReconstruction(t *testing.T) {
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
	_, state := world.Snapshot(ctx).Data(ctx)
	var actor, terminal, crate, enemy bool
	for i, entity := range state.Entities {
		if i > 0 && state.Entities[i-1].ID >= entity.ID {
			t.Fatalf("unstable entity order: %+v", state.Entities)
		}
		switch {
		case entity.Role == content.RoleActor && entity.Ticket == "D-HOS-RES-01":
			actor = entity.Protected
		case entity.Role == content.RoleTerminal:
			terminal = entity.Protected
		case entity.Role == content.RoleCrate:
			crate = entity.Position.X == 352
		case entity.Kind == "MELEE":
			enemy = entity.Position.X == 960
		}
	}
	if !actor || !terminal || !crate || !enemy {
		t.Fatalf("missing restored attempt components: %+v", state.Entities)
	}
	state.Entities[0].Position.X = 9999
	_, fresh := world.Snapshot(ctx).Data(ctx)
	if fresh.Entities[0].Position.X == 9999 {
		t.Fatal("entities alias live world")
	}
	if err := world.Step(ctx, TickInput{Velocity: Vector{X: 120}}); err != nil {
		t.Fatal(err)
	}
	again, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, reset := again.Snapshot(ctx).Data(ctx)
	if !reflect.DeepEqual(fresh, reset) {
		t.Fatalf("restoration retained partial excursion: %+v", reset)
	}
}

func TestPRVRefillsRestoredAndIsolated(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	saved, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range manifest.Batches(ctx)[:3] {
		for _, id := range batch.Deeds {
			saved = certifyFixture(t, owner, manifest, saved, id)
		}
	}
	other, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, otherState := other.Snapshot(ctx).Data(ctx)
	var otherRefills int
	for _, entity := range otherState.Entities {
		if entity.Role == content.RoleRefill {
			otherRefills++
		}
	}
	if otherRefills != 1 {
		t.Fatalf("non-PRV bay has %d refills", otherRefills)
	}
	saved, err = owner.Checkpoint(ctx, saved, "ANC-HOS-04")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := world.Snapshot(ctx)
	_, state := snapshot.Data(ctx)
	refills := make(map[content.ObjectID]Vector)
	for i, entity := range state.Entities {
		if entity.Role == content.RoleRefill {
			if !entity.Protected {
				t.Fatalf("unprotected refill: %+v", entity)
			}
			refills[entity.ID] = entity.Position
			state.Entities[i].Position.X = -1
		}
	}
	want := map[content.ObjectID]Vector{
		"station-ENC-HOS-04-REFILL":     {X: 208},
		"station-ENC-HOS-04-REFILL-BAY": {X: 608},
	}
	if !reflect.DeepEqual(refills, want) {
		t.Fatalf("PRV refills: %+v", refills)
	}
	_, again := snapshot.Data(ctx)
	_, live := world.Snapshot(ctx).Data(ctx)
	for _, view := range []State{again, live} {
		for _, entity := range view.Entities {
			if position, ok := want[entity.ID]; ok && entity.Position != position {
				t.Fatalf("refill changes escaped snapshot copy: %+v", entity)
			}
		}
	}
	if !reflect.DeepEqual(again, live) {
		t.Fatal("snapshot differs from live world")
	}
	permuted := slices.Clone(bays)
	slices.Reverse(permuted)
	rebuilt, err := NewWorld(ctx, owner, manifest, saved, permuted, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, rebuiltState := rebuilt.Snapshot(ctx).Data(ctx)
	if !reflect.DeepEqual(again, rebuiltState) {
		t.Fatal("refill reconstruction is not stable")
	}
	active, err := owner.Accept(ctx, saved, "D-HOS-PRV-02")
	if err != nil {
		t.Fatal(err)
	}
	activeWorld, err := NewWorld(ctx, owner, manifest, active, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, activeState := activeWorld.Snapshot(ctx).Data(ctx)
	activeRefills := make(map[content.ObjectID]Vector)
	for _, entity := range activeState.Entities {
		if entity.Role == content.RoleRefill {
			activeRefills[entity.ID] = entity.Position
		}
	}
	if !reflect.DeepEqual(activeRefills, want) {
		t.Fatalf("active variant changed refills: %+v", activeRefills)
	}
}

func TestPermanentItemRouteChangesCollision(t *testing.T) {
	ctx := context.Background()
	_, manifest, bays := campaignFixture(t)
	item := campaign.CriticalItemDefinition{ID: "ITEM-TEST-KEY", AcquireAnchor: "ANC-HOS-01", ConsumeAnchor: "ANC-HOS-02", Route: "OBJ-HOS-RES-GATE"}
	owner, err := campaign.NewWithInventory(ctx, manifest, []campaign.CriticalItemDefinition{item})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	acquire := campaign.Transition{CampaignID: "01234567-89ab-4cde-8fab-0123456789ab", Sequence: 1, CriticalItems: []campaign.CriticalItemCandidate{{Item: item.ID, Action: campaign.CriticalAcquire, Completed: true}}}
	saved, err = owner.Prepare(ctx, saved, acquire)
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
	before, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := Rect{X: 640, Width: 28, Height: 58}
	hit, found, err := before.Sweep(ctx, body, Vector{X: 100}, SweepOptions{})
	if err != nil || !found || hit.Collider.ID != "terrain-GATE" {
		t.Fatalf("closed gate: %+v %v %v", hit, found, err)
	}
	consume := campaign.Transition{CampaignID: acquire.CampaignID, Sequence: saved.Data(ctx).OperationSequence + 1, CriticalItems: []campaign.CriticalItemCandidate{{Item: item.ID, Action: campaign.CriticalConsume, Completed: true}}}
	saved, err = owner.Prepare(ctx, saved, consume)
	if err != nil {
		t.Fatal(err)
	}
	after, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, state := after.Snapshot(ctx).Data(ctx)
	if !slices.Contains(state.ItemRoutes, item.Route) || len(state.OwnedItems) != 0 {
		t.Fatalf("item route not applied: %+v", state)
	}
	if hit, found, err = after.Sweep(ctx, body, Vector{X: 100}, SweepOptions{}); err != nil || found {
		t.Fatalf("opened gate remained blocked: %+v %v %v", hit, found, err)
	}
}

func TestDefeatedInstalledAndEndingRestoration(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	saved, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	batches := manifest.Batches(ctx)
	deeds := manifest.Deeds(ctx)
	for institution := 0; institution < 4; institution++ {
		for family := 0; family < 4; family++ {
			for _, id := range batches[institution*4+family].Deeds {
				saved = certifyFixture(t, owner, manifest, saved, id)
			}
		}
		boss := deeds[institution*25+24]
		saved, err = owner.DefeatBoss(ctx, saved, boss.Deed.Encounter)
		if err != nil {
			t.Fatal(err)
		}
		world, constructErr := NewWorld(ctx, owner, manifest, saved, bays, nil)
		if constructErr != nil {
			t.Fatal(constructErr)
		}
		_, state := world.Snapshot(ctx).Data(ctx)
		terminal := false
		for _, entity := range state.Entities {
			if entity.Role == content.RoleTerminal && entity.Protected {
				terminal = true
			}
		}
		if state.Encounter != boss.Deed.Encounter || !slices.Contains(state.Defeated, boss.Deed.Encounter) || state.BossActive || state.Player.Bounds.Width != PlayerWidth || state.Player.Bounds.Height != PlayerHeight || !terminal {
			t.Fatalf("defeated boss restoration: %+v", state)
		}
		if institution == 3 && (!slices.Contains(state.Installed, content.DeedID("D-INS-SUP-06")) || !slices.Contains(state.Installed, content.DeedID("D-INS-UTL-06")) || len(state.Receipts) != 99) {
			t.Fatalf("installed equipment or receipts missing: %+v", state)
		}
		saved = certifyFixture(t, owner, manifest, saved, boss.Deed.ID)
	}
	if saved.Credits() != 100 {
		t.Fatal("wrong receipt count")
	}
	saved, err = owner.IssuePermit(ctx, saved)
	if err != nil {
		t.Fatal(err)
	}
	saved, err = owner.ConsentDecision(ctx, saved, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []campaign.EndingStage{campaign.EndingDeath, campaign.EndingHell, campaign.EndingCeremony, campaign.EndingTableau} {
		world, constructErr := NewWorld(ctx, owner, manifest, saved, bays, nil)
		if constructErr != nil {
			t.Fatal(constructErr)
		}
		_, state := world.Snapshot(ctx).Data(ctx)
		if state.HasPlayer || state.Mode != "ST-SCN" || state.Ending != stage || len(state.Receipts) != 100 {
			t.Fatalf("ending was reconstructed as gameplay: %+v", state)
		}
		if err := world.Step(ctx, TickInput{}); err == nil {
			t.Fatal("ending allowed gameplay tick")
		}
		saved, err = owner.AdvanceEnding(ctx, saved, stage)
		if err != nil {
			t.Fatal(err)
		}
	}
	world, err := NewWorld(ctx, owner, manifest, saved, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, state := world.Snapshot(ctx).Data(ctx)
	if state.HasPlayer || state.Mode != "ST-END" || len(state.Receipts) != 100 {
		t.Fatalf("completed ending: %+v", state)
	}
}

func TestDeterministicTicksAndInvalidInputs(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	first, err := NewWorld(ctx, owner, manifest, campaign.Snapshot{}, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewWorld(ctx, owner, manifest, campaign.Snapshot{}, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []TickInput{{Velocity: Vector{X: 240}}, {Velocity: Vector{X: -120, Y: 120}}, {Velocity: Vector{Y: -240}}, {}}
	for i := 0; i < 40; i++ {
		for _, world := range []*World{first, second} {
			if err := world.Step(ctx, inputs[i%len(inputs)]); err != nil {
				t.Fatal(err)
			}
		}
		ap, ac := first.Snapshot(ctx).Data(ctx)
		bp, bc := second.Snapshot(ctx).Data(ctx)
		if !reflect.DeepEqual(ap, bp) || !reflect.DeepEqual(ac, bc) {
			t.Fatal("identical input diverged")
		}
	}
	_, before := first.Snapshot(ctx).Data(ctx)
	if err := first.Step(ctx, TickInput{Velocity: Vector{X: math.NaN()}}); err == nil {
		t.Fatal("accepted invalid velocity")
	}
	_, after := first.Snapshot(ctx).Data(ctx)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("invalid step changed world")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := first.Step(canceled, TickInput{}); err != context.Canceled {
		t.Fatalf("canceled tick: %v", err)
	}
	_, after = first.Snapshot(ctx).Data(ctx)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("canceled step changed world")
	}
}

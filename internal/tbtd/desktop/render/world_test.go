// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/desktop/input"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

// renderStore is a storage-boundary fixture; session and world remain real.
type renderStore struct {
	request session.ProgressRequest
}

func (store *renderStore) Submit(_ context.Context, request session.ProgressRequest) error {
	store.request = request
	return nil
}

func (store *renderStore) Poll(ctx context.Context, id session.RequestID) (session.ProgressResult, error) {
	return session.NewProgressResult(ctx, session.ProgressResultData{ID: id, Campaign: "00000000-0000-4000-8000-000000000001", Status: session.StorageSucceeded, Revision: 7, RevisionKnown: true, PayloadDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Snapshot: campaign.Snapshot{}}), nil
}

func TestLiveSessionPreparationAndMissingHandoff(t *testing.T) {
	ctx := context.Background()
	fsys := os.DirFS("../../../..")
	catalogue, err := content.LoadCampaign(ctx, fsys, "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := campaign.New(ctx, catalogue)
	if err != nil {
		t.Fatal(err)
	}
	bays, err := content.LoadServiceBays(ctx, fsys, "content/data/encounters/templates.json", "content/data/encounters/variants.json", catalogue)
	if err != nil {
		t.Fatal(err)
	}
	service, err := session.New(ctx, owner, catalogue, bays, &renderStore{})
	if err != nil {
		t.Fatal(err)
	}
	frame := func(input session.FrameInput) session.FrameViewData {
		t.Helper()
		command, commandErr := session.NewFrameCommand(ctx, input)
		if commandErr != nil {
			t.Fatal(commandErr)
		}
		view, frameErr := service.HandleFrame(ctx, command)
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		return view.Data(ctx)
	}
	selectAction := func(action session.MenuAction) session.FrameViewData {
		t.Helper()
		return frame(session.FrameInput{Selections: []session.MenuSelection{{Action: action}}})
	}
	selectAction("continue")
	frame(session.FrameInput{})
	selectAction("resume")
	view := frame(session.FrameInput{Elapsed: 25 * time.Millisecond, Values: []session.ActionValue{{Action: settings.MoveRight, X: 1}}})
	items, err := planWorld(ctx, view)
	if err != nil || view.World.Background != "ART-PRO-01" || len(items) < 2 {
		t.Fatalf("live world plan: %+v %v", view.World, err)
	}
	if err := validateScenery(view.World.Background, 1, view.World.Scenery); err == nil {
		t.Fatal("missing authored room layout was accepted")
	}
	if len(view.Entities) == 0 || len(view.Entities[0].Parts) != 0 {
		t.Fatal("unexpected authored animation handoff on a media-free checkout")
	}
	foundPlayer := false
	for _, item := range items {
		if item.entity == "player" {
			foundPlayer = true
			if handoffErr := validateActorHandoff(ctx, item.asset, &view.Entities[0]); handoffErr == nil || !content.ErrInvalid.Has(handoffErr) {
				t.Fatalf("missing player frame was accepted: %v", handoffErr)
			}
		}
	}
	if !foundPlayer {
		t.Fatal("live player not planned")
	}
	generation := view.World.Generation
	view = frame(session.FrameInput{Focus: session.FocusLost, Elapsed: time.Second})
	if view.State != session.StatePause {
		t.Fatal("focus interruption did not pause")
	}
	frame(session.FrameInput{Focus: session.FocusGained})
	selectAction("return_checkpoint")
	view = selectAction("confirm")
	if view.State != session.StatePause || view.World.Generation == generation || view.World.Tick != 0 {
		t.Fatalf("same-room recovery: %+v", view.World)
	}
	if _, err := planWorld(ctx, view); err != nil {
		t.Fatal(err)
	}
}

func TestCameraAndViewport(t *testing.T) {
	ctx := context.Background()
	var camera Camera
	camera.Follow(ctx, session.Point{X: 500}, 0, 0, false, 0, nil)
	camera.Follow(ctx, session.Point{X: 600}, 0, time.Second, false, 0, nil)
	if camera.X != 500 || camera.Y != 160 {
		t.Fatalf("dead zone: %+v", camera)
	}
	camera.Follow(ctx, session.Point{X: 600}, 1, 100*time.Millisecond, false, 0, nil)
	if camera.X != 560 {
		t.Fatalf("look-ahead: %+v", camera)
	}
	camera.Follow(ctx, session.Point{X: 600}, 1, 100*time.Millisecond, false, 0, nil)
	if camera.X != 620 {
		t.Fatalf("look-ahead limit: %+v", camera)
	}
	camera.Follow(ctx, session.Point{X: 700}, -1, 10*time.Millisecond, true, 200, nil)
	if camera.X != 840 || camera.Y != 160 {
		t.Fatalf("boss framing: %+v", camera)
	}
	viewport := camera.Viewport(1600, 1200)
	if viewport.Y != 150 || viewport.Height != 900 || viewport.Width != 1600 {
		t.Fatalf("letterbox: %+v", viewport)
	}
	if _, ok := viewport.WorldPoint(ctx, input.Point{X: 100, Y: 100}); ok {
		t.Fatal("letterbox bar accepted")
	}
	point, ok := viewport.WorldPoint(ctx, input.Point{X: 800, Y: 600})
	if !ok || point.X != 840 || point.Y != 160 {
		t.Fatalf("viewport inverse: %+v %v", point, ok)
	}
	camera.Follow(ctx, session.Point{X: 500}, 0, 0, false, 0, []session.Point{{X: 1300, Y: 300}})
	if math.Abs(1300-camera.X) > 620 || math.Abs(300-camera.Y) > 340 {
		t.Fatal("mandatory landing not visible")
	}
}

func TestCameraFramesAllLandingBounds(t *testing.T) {
	ctx := context.Background()
	bounds := []session.WorldRect{{X: 0, Y: 0, Width: 100, Height: 80}, {X: 1150, Y: 200, Width: 100, Height: 80}}
	var first, second Camera
	if err := first.FollowBounds(ctx, session.Point{X: 600}, 1, time.Second, false, 0, bounds); err != nil {
		t.Fatal(err)
	}
	if err := second.FollowBounds(ctx, session.Point{X: 600}, 1, time.Second, false, 0, []session.WorldRect{bounds[1], bounds[0]}); err != nil {
		t.Fatal(err)
	}
	if first.X != second.X || first.Y != second.Y {
		t.Fatalf("order-dependent framing: %+v %+v", first, second)
	}
	for _, area := range bounds {
		if area.X < first.X-640 || area.X+area.Width > first.X+640 || area.Y < first.Y-360 || area.Y+area.Height > first.Y+360 {
			t.Fatalf("landing clipped: %+v %+v", first, area)
		}
	}
	before := first
	if err := first.FollowBounds(ctx, session.Point{X: 600}, 0, 0, false, 0, []session.WorldRect{{Width: 1281, Height: 1}}); err == nil || first != before {
		t.Fatal("impossible framing must fail without moving the camera")
	}
	if err := first.FollowBounds(ctx, session.Point{X: 600}, 0, 0, true, 0, []session.WorldRect{{X: 1300, Width: 50, Height: 10}}); err == nil || first != before {
		t.Fatal("off-arena landing accepted by locked camera")
	}
}

func TestAuthoredBayLandingBoundsFit(t *testing.T) {
	ctx := context.Background()
	fsys := os.DirFS("../../../..")
	campaign, err := content.LoadCampaign(ctx, fsys, "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	bays, err := content.LoadServiceBays(ctx, fsys, "content/data/encounters/templates.json", "content/data/encounters/variants.json", campaign)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, bay := range bays {
		if seen[bay.Template.ID] {
			continue
		}
		seen[bay.Template.ID] = true
		for _, jump := range bay.Template.Jumps {
			for _, terrain := range bay.Template.Terrain {
				if terrain.ID != jump.Landing {
					continue
				}
				area := terrain.Bounds
				var camera Camera
				if err := camera.FollowBounds(ctx, session.Point{X: jump.TakeoffX}, 0, 0, false, 0, []session.WorldRect{{X: area.X, Y: area.Y, Width: area.Width, Height: area.Height}}); err != nil {
					t.Fatalf("%s/%s: %v", bay.Template.ID, jump.ID, err)
				}
			}
		}
		for _, jump := range bay.Safety.Jumps {
			if jump.Target < 0 || jump.Target >= len(bay.Safety.Platforms) {
				t.Fatal("invalid bypass target")
			}
			area := bay.Safety.Platforms[jump.Target]
			var camera Camera
			if err := camera.FollowBounds(ctx, session.Point{X: jump.Jump.TakeoffX}, 0, 0, false, 0, []session.WorldRect{{X: area.X, Y: area.Y, Width: area.Width, Height: area.Height}}); err != nil {
				t.Fatalf("%s/%s: %v", bay.Template.ID, jump.Jump.ID, err)
			}
		}
	}
	if len(seen) != 4 {
		t.Fatalf("expected four bay families, got %d", len(seen))
	}
}

func TestBossAndPrologueLandingBounds(t *testing.T) {
	ctx := context.Background()
	fsys := os.DirFS("../../../..")
	manifest, err := content.LoadCampaign(ctx, fsys, "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	bays, err := content.LoadServiceBays(ctx, fsys, "content/data/encounters/templates.json", "content/data/encounters/variants.json", manifest)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[content.LocationID]bool)
	check := func(name string, platforms []content.Rect, jumps []content.BossJump) {
		t.Helper()
		for _, jump := range jumps {
			if jump.Target < 0 || jump.Target >= len(platforms) {
				t.Fatalf("%s: invalid target", name)
			}
			platform := platforms[jump.Target]
			width := math.Min(160, platform.Width)
			x := platform.X
			if jump.Jump.Direction == -1 {
				x += platform.Width - width
			}
			landing := session.WorldRect{X: x, Y: platform.Y + platform.Height, Width: width, Height: platform.Height}
			var camera Camera
			if err := camera.FollowBounds(ctx, session.Point{X: jump.Jump.TakeoffX}, 0, 0, false, 0, []session.WorldRect{landing}); err != nil {
				t.Fatalf("%s/%s: %v", name, jump.Jump.ID, err)
			}
			if landing.X < camera.X-640 || landing.X+landing.Width > camera.X+640 {
				t.Fatalf("%s/%s: clipped landing", name, jump.Jump.ID)
			}
		}
	}
	for _, bay := range bays {
		if seen[bay.Institution.Location] {
			continue
		}
		seen[bay.Institution.Location] = true
		boss := bay.Institution.Boss
		check(string(boss.Encounter), boss.Platforms, boss.Jumps)
		for _, room := range bay.Institution.Prologue {
			check(string(room.Encounter), room.Platforms, room.Jumps)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("expected four institutions, got %d", len(seen))
	}
}

func TestAnimationAndInterpolationUseCurrentState(t *testing.T) {
	entity := session.EntityView{PreviousX: 0, X: 100, PreviousY: 2, Y: 10}
	if got := interpolate(entity, 0.5); got.X != 50 || got.Y != 6 {
		t.Fatalf("interpolation: %+v", got)
	}
	if got := interpolate(entity, 3); got.X != 100 || math.IsNaN(got.X) {
		t.Fatalf("clamped interpolation: %+v", got)
	}
	player := session.PlayerView{Grounded: true, Velocity: session.Point{X: 1}, Limbs: [4]bool{true, true, true, true}}
	if got := playerClip(player); got != "AN-GRI-RUN" {
		t.Fatal(got)
	}
	player.Limbs[2] = false
	if got := playerClip(player); got != "AN-GRI-HOP" {
		t.Fatal(got)
	}
	player.Limbs[3] = false
	if got := playerClip(player); got != "AN-GRI-CRAWL" {
		t.Fatal(got)
	}
	player.Action = "throw"
	if got := playerClip(player); got != "AN-GRI-THROW" {
		t.Fatal(got)
	}
	player.Action = "hurt"
	if got := playerClip(player); got != "AN-GRI-HURT" {
		t.Fatalf("interruption: %s", got)
	}
	player.Action = "interact"
	player.Channel = "regenerate"
	if got := playerClip(player); got != "AN-GRI-REGEN" {
		t.Fatal(got)
	}
	player.Action, player.SelectedSocket = "remove", "LIM-LL"
	player.Limbs[0] = false
	if got := playerClip(player); got != "AN-GRI-REMOVE" {
		t.Fatalf("leg removal misidentified as teeth: %s", got)
	}
	player.SelectedSocket = "LIM-RA"
	if got := playerClip(player); got != "AN-GRI-TEETH" {
		t.Fatalf("second arm removal: %s", got)
	}
}

func TestPlanUsesPreviewAndEssentialOrder(t *testing.T) {
	ctx := context.Background()
	data := session.FrameViewData{
		World:    session.WorldView{Background: "ART-PRO-01", Fraction: 0.5, Terrain: []session.WorldRect{{Width: 20, Height: 10}}, Markers: []session.WorldMarker{{ID: "hazard", Kind: "danger"}, {ID: "safe", Kind: "refuge"}, {ID: "target", Kind: "interaction"}}},
		Player:   session.PlayerView{Action: "throw", AttackPhase: "windup"},
		Entities: []session.EntityView{{ID: "player", Kind: "player", X: 100}},
		Aim:      session.AimView{Visible: true, Trajectory: []session.Point{{X: 1, Y: 2}, {X: 3, Y: 4}}, HasImpact: true, Impact: session.Point{X: 3, Y: 4}, ImpactKind: "receptor"},
	}
	items, err := planWorld(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(items); i++ {
		if items[i].layer < items[i-1].layer {
			t.Fatal("essential feedback was buried under world layers")
		}
	}
	if items[2].asset != "AN-GRI-THROW" || items[2].point.X != 50 || items[len(items)-1].shape != shapeRing {
		t.Fatalf("wrong action, interpolation or impact: %+v", items)
	}
	data.Aim.Trajectory[0].X = 99
	if items[len(items)-2].path[0].X != 1 {
		t.Fatal("plan aliases the authoritative preview")
	}
	data.Aim.ImpactKind = "unknown"
	if _, err := planWorld(ctx, data); err == nil {
		t.Fatal("unknown impact accepted")
	}
	data.World.Background = ""
	if _, err := planWorld(ctx, data); err == nil {
		t.Fatal("missing asset accepted")
	}
}

func TestSceneryExportsAndCameraAlignment(t *testing.T) {
	layers := []session.SceneryLayer{
		{Asset: "ART-BAY-HOS-SUP", Export: 0, Depth: 0, Source: session.WorldRect{Width: 512, Height: 512}, Bounds: session.WorldRect{X: 0, Y: 0, Width: 256, Height: 256}},
		{Asset: "ART-BAY-HOS-SUP", Export: 1, Depth: 2, Source: session.WorldRect{Width: 512, Height: 64}, Bounds: session.WorldRect{X: 256, Y: 0, Width: 256, Height: 32}},
	}
	if err := validateScenery("ART-BAY-HOS-SUP", 2, layers); err != nil {
		t.Fatal(err)
	}
	if err := validateScenery("ART-BAY-HOS-SUP", 3, layers); err == nil {
		t.Fatal("missing scenery export accepted")
	}
	var camera Camera
	camera.Follow(context.Background(), session.Point{X: 300}, 0, 0, false, 0, nil)
	view := camera.Viewport(1280, 720)
	first := screenPoint(view, session.Point{X: layers[1].Bounds.X, Y: layers[1].Bounds.Y})
	view.Target.X += 100
	second := screenPoint(view, session.Point{X: layers[1].Bounds.X, Y: layers[1].Bounds.Y})
	if first.X-second.X != 100 || first.Y != second.Y {
		t.Fatalf("scenery did not move with world camera: %+v %+v", first, second)
	}
}

func TestSpriteFacingMirrorsLayersAroundEntityAnchor(t *testing.T) {
	view := input.Viewport{Width: 1280, Height: 720, Target: input.Point{X: 640, Y: 160}}
	part := session.VisualPart{Source: session.WorldRect{X: 10, Y: 20, Width: 30, Height: 40}, Offset: session.Point{X: 5, Y: 2}, Size: session.Point{X: 30, Y: 40}}
	right := sprite{part: part, point: session.Point{X: 400}, facing: 1}
	source, destination := spriteGeometry(view, right)
	if source.Width != 30 || destination.X != 405 {
		t.Fatalf("right-facing geometry: %+v %+v", source, destination)
	}
	right.facing = -1
	source, destination = spriteGeometry(view, right)
	if source.Width != -30 || destination.X != 365 {
		t.Fatalf("left-facing geometry: %+v %+v", source, destination)
	}
	if part.Offset.X != 5 || part.Source.Width != 30 {
		t.Fatal("facing mutated the authored part")
	}
}

func TestTerrainEdgeUsesWorldTransform(t *testing.T) {
	view := input.Viewport{Width: 1280, Height: 720, Target: input.Point{X: 640, Y: 160}}
	bounds := session.WorldRect{X: 100, Y: 20, Width: 80, Height: 12}
	left, right := terrainEdge(view, bounds)
	if left.X != 100 || right.X != 180 || left.Y != right.Y {
		t.Fatalf("terrain edge: %+v %+v", left, right)
	}
	view.Target.X += 50
	moved, _ := terrainEdge(view, bounds)
	if left.X-moved.X != 50 || left.Y != moved.Y {
		t.Fatalf("terrain edge did not follow camera: %+v", moved)
	}
}

func TestPlanIncludesDynamicBoundEntities(t *testing.T) {
	ctx := context.Background()
	data := session.FrameViewData{World: session.WorldView{Background: "ART-BAY-HOS-SUP", Fraction: 0.5}, Entities: []session.EntityView{
		{ID: "trolley", Kind: "object", Asset: "ART-OBJ-HOS-SUP-TROLLEY", PreviousX: 100, X: 120, State: "broken"},
		{ID: "adult", Kind: "rescue", Asset: "AN-RES-HOS-01-FOLLOW", PreviousX: 10, X: 30},
		{ID: "boss", Kind: "boss", Asset: "AN-BOSS-HOS-A-PREP", X: 100},
		{ID: "projectile", Kind: "projectile", Asset: "FX-FEC", X: 40},
	}}
	items, err := planWorld(ctx, data)
	if err != nil || len(items) != 5 {
		t.Fatalf("dynamic plan: %+v %v", items, err)
	}
	if items[1].point.X != 110 || items[1].state != "broken" || items[2].point.X != 20 || items[3].asset != "AN-BOSS-HOS-A-PREP" || items[4].asset != "FX-FEC" {
		t.Fatalf("wrong dynamic assets or positions: %+v", items)
	}
	data.Entities[0].Asset = ""
	if _, err = planWorld(ctx, data); err == nil {
		t.Fatal("missing object binding accepted")
	}
}

func TestMissingProjectileAndLimbBindingsNameTheirVariants(t *testing.T) {
	ctx := context.Background()
	for _, entity := range []session.EntityView{
		{ID: "shot", Kind: "projectile", Variant: "enemy:HOS:RNG"},
		{ID: "arena-shot", Kind: "projectile", Variant: "arena:CHU-C"},
		{ID: "limb", Kind: "limb", Variant: "LIM-LA", State: "settled"},
	} {
		data := session.FrameViewData{World: session.WorldView{Background: "ART-PRO-01"}, Entities: []session.EntityView{entity}}
		_, err := planWorld(ctx, data)
		if err == nil || !content.ErrInvalid.Has(err) || !strings.Contains(err.Error(), entity.Variant) || !strings.Contains(err.Error(), entity.ID) {
			t.Fatalf("missing binding was not identified for %+v: %v", entity, err)
		}
	}
}

func TestMarkerShapesAndEffectLimits(t *testing.T) {
	seen := make(map[markerShape]bool)
	for _, kind := range []string{"danger", "refuge", "interaction", "receptor"} {
		shape := markerFor(session.WorldMarker{Kind: kind})
		if shape == "" || seen[shape] {
			t.Fatalf("ambiguous marker %s: %s", kind, shape)
		}
		seen[shape] = true
	}
	if markerFor(session.WorldMarker{Kind: "boss", Label: "ЗАЩИТА"}) == markerFor(session.WorldMarker{Kind: "boss", Label: "УЯЗВИМ"}) || impactShape("terrain") == impactShape("receptor") || impactShape("invalid") == impactShape("receptor") {
		t.Fatal("protection or impact distinctions lost")
	}
	phases := make(map[markerShape]bool)
	for _, state := range []string{"dormant", "warning", "active", "inactive"} {
		shape := markerFor(session.WorldMarker{Kind: "danger", State: state})
		if phases[shape] {
			t.Fatalf("danger phase %q shares %q", state, shape)
		}
		phases[shape] = true
	}
	if markerFor(session.WorldMarker{Kind: "immune"}) != shapeHatch {
		t.Fatal("immunity outline missing")
	}
	var pool effectPool
	full := settings.Accessibility{Shake: settings.ShakeFull, Blood: settings.ParticlesFull}
	if pool.addParticles(800) != maxParticles || pool.addParticles(1) != 0 {
		t.Fatal("particle cap")
	}
	for i := 0; i < 50; i++ {
		pool.addStain(session.EffectEvent{Tick: uint64(i)})
	}
	if len(pool.stains) != 48 || pool.stains[0].Tick != 2 {
		t.Fatalf("oldest stain not retired: %+v", pool.stains)
	}
	distance, life := shakeLimit(true, full)
	if distance != 4 || life != 120*time.Millisecond || sprayLifetime(false) != 400*time.Millisecond || sprayLifetime(true) != 600*time.Millisecond {
		t.Fatal("effect bounds")
	}
	full.ReducedMotion = true
	if distance, _ = shakeLimit(true, full); distance != 0 {
		t.Fatal("reduced motion shake")
	}
	pool.reset(context.Background())
	if pool.particles != 0 || len(pool.stains) != 0 {
		t.Fatal("reset retained decorations")
	}
}

func TestReticleUsesUnshakenLetterboxedScreen(t *testing.T) {
	ctx := context.Background()
	var camera Camera
	camera.Follow(ctx, session.Point{X: 600}, 0, 0, false, 0, nil)
	viewport := camera.Viewport(1600, 1200)
	sampler, err := input.New(ctx, settings.Defaults(ctx), func() uint64 { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	sample := input.Snapshot{Focused: true, Pointer: input.Point{X: 300, Y: 400}}
	frame := input.Frame{View: viewport, ThrowOrigin: input.Point{X: 600}, Facing: 1}
	if _, err := sampler.SampleSnapshot(ctx, frame, sample); err != nil {
		t.Fatal(err)
	}
	sample.Pointer = input.Point{X: 500, Y: 500}
	command, err := sampler.SampleSnapshot(ctx, frame, sample)
	if err != nil {
		t.Fatal(err)
	}
	reticle := command.Data(ctx).Reticle
	if !reticle.Pointer || !reticle.Visible {
		t.Fatalf("missing mouse reticle: %+v", reticle)
	}
	aim := session.AimView{Visible: true, Reticle: reticle}
	point, ok := reticlePoint(viewport, aim, session.Point{X: 600})
	if !ok || point.X != 500 || point.Y != 500 {
		t.Fatalf("pointer transform: %+v %v", point, ok)
	}
	viewport.Target.X += 300 // Camera movement or world-only shake cannot move the mouse reticle.
	moved, ok := reticlePoint(viewport, aim, session.Point{X: 600})
	if !ok || moved != point {
		t.Fatalf("reticle moved with camera: %+v", moved)
	}
	sample.Pointer.Y = 20
	command, err = sampler.SampleSnapshot(ctx, frame, sample)
	if err != nil {
		t.Fatal(err)
	}
	aim.Reticle = command.Data(ctx).Reticle
	if !aim.Reticle.Pointer || aim.Reticle.Visible {
		t.Fatalf("letterbox pointer: %+v", aim.Reticle)
	}
	if _, ok = reticlePoint(viewport, aim, session.Point{X: 600}); ok {
		t.Fatal("reticle drawn in letterbox")
	}
	aim.Reticle = session.ReticleInput{}
	aim.Direction = session.Point{X: 1}
	if _, ok = reticlePoint(viewport, aim, session.Point{X: 600}); !ok {
		t.Fatal("controller aim has no reticle")
	}
}

func TestEffectEventsExpireAndShakeOnlyWorld(t *testing.T) {
	ctx := context.Background()
	var pool effectPool
	access := settings.Accessibility{Shake: settings.ShakeFull, Blood: settings.ParticlesFull}
	hit := session.EffectEvent{Tick: 100, Entity: "player", Kind: "hit", Asset: "FX-BODY", Direction: 1}
	if err := pool.update(ctx, 100, []session.EffectEvent{hit}, access); err != nil {
		t.Fatal(err)
	}
	if pool.particles != 1 || len(pool.active) != 1 || len(pool.stains) != 1 || pool.shakeOffset(100, access) != 2 {
		t.Fatalf("hit: %+v", pool)
	}
	if err := pool.update(ctx, 100, []session.EffectEvent{hit}, access); err != nil || len(pool.active) != 1 || len(pool.stains) != 1 {
		t.Fatal("redrawn view replayed effects")
	}
	access.Shake = settings.ShakeOff
	if pool.shakeOffset(101, access) != 0 {
		t.Fatal("shake-off setting ignored")
	}
	access.Shake = settings.ShakeFull
	if pool.shakeOffset(110, access) != 0 {
		t.Fatal("normal shake exceeded 80ms")
	}
	if err := pool.update(ctx, 148, nil, access); err != nil || pool.particles != 0 || len(pool.active) != 0 {
		t.Fatal("normal spray did not expire")
	}
	access.Blood = settings.ParticlesReduced
	removal := session.EffectEvent{Tick: 150, Entity: "player", Kind: "removal", Asset: "FX-BODY", Heavy: true}
	if err := pool.update(ctx, 150, []session.EffectEvent{removal}, access); err != nil || pool.particles != 1 || pool.shakeOffset(150, access) != 4 {
		t.Fatalf("reduced removal: %+v %v", pool, err)
	}
	if pool.shakeOffset(165, access) != 0 {
		t.Fatal("heavy shake exceeded 120ms")
	}
	if err := pool.update(ctx, 222, nil, access); err != nil || pool.particles != 0 {
		t.Fatal("removal spray did not expire")
	}
	if err := pool.update(ctx, 10, nil, access); err != nil || len(pool.stains) != 0 {
		t.Fatal("restored clock retained old room effects")
	}
	if err := pool.update(ctx, 10, []session.EffectEvent{{Tick: 11, Entity: "player", Kind: "hit", Asset: "FX-BODY"}}, access); err == nil {
		t.Fatal("future event accepted")
	}
}

func TestAuthoredEffectsPlanReductionAndStains(t *testing.T) {
	ctx := context.Background()
	part := session.VisualPart{Asset: "FX-BODY", Source: session.WorldRect{Width: 8, Height: 8}, Size: session.Point{X: 4, Y: 4}}
	event := session.EffectEvent{Tick: 10, Entity: "player", Kind: "hit", Asset: "FX-BODY", Part: part, Stain: part, Position: session.Point{X: 40, Y: 20}, Particles: []session.VisualPart{part, part, part, part}}
	var pool effectPool
	full := settings.Accessibility{Blood: settings.ParticlesFull}
	if err := pool.update(ctx, 10, []session.EffectEvent{event}, full); err != nil {
		t.Fatal(err)
	}
	// The pool must not borrow the frame's mutable authored regions.
	event.Particles[0].Asset = ""
	items, err := pool.plan(ctx, full)
	if err != nil || len(items) != 6 || !items[0].stain || items[0].point != event.Position || pool.particles != 5 {
		t.Fatalf("full effect plan: %+v %v", items, err)
	}
	reduced := settings.Accessibility{Blood: settings.ParticlesReduced}
	items, err = pool.plan(ctx, reduced)
	if err != nil {
		t.Fatal(err)
	}
	visible := 0
	for _, item := range items {
		if item.visible && !item.stain {
			visible++
		}
	}
	if visible != 3 {
		t.Fatalf("reduced spray drew %d regions, want 3", visible)
	}
	if err := pool.update(ctx, 58, nil, reduced); err != nil {
		t.Fatal(err)
	}
	items, err = pool.plan(ctx, reduced)
	if err != nil || len(items) != 1 || !items[0].stain || pool.particles != 0 {
		t.Fatalf("spray expiry lost stain: %+v %v", items, err)
	}
	for i := 0; i < 49; i++ {
		pool.addStain(session.EffectEvent{Asset: "FX-BODY", Stain: part, Tick: uint64(i)})
	}
	items, err = pool.plan(ctx, reduced)
	if err != nil || len(items) != maxStains || items[0].visible || !items[maxStains-1].visible || items[0].alpha >= items[maxStains-1].alpha {
		t.Fatalf("stain limit, reduction or fade: %+v %v", items, err)
	}
	pool.stains[0].Stain = session.VisualPart{}
	if _, err := pool.plan(ctx, reduced); err == nil {
		t.Fatal("reduced effects hid an unresolved stain")
	}
	var single effectPool
	for _, tick := range []uint64{11, 12} {
		event := session.EffectEvent{Tick: tick, Entity: "player", Kind: "hit", Asset: "FX-BODY", Part: part, Stain: part}
		if err := single.update(ctx, tick, []session.EffectEvent{event}, full); err != nil {
			t.Fatal(err)
		}
	}
	feces := session.EffectEvent{Tick: 12, Ordinal: 1, Entity: "shot", Kind: "feces", Asset: "FX-FEC", Part: session.VisualPart{Asset: "FX-FEC"}, Particles: []session.VisualPart{{Asset: "FX-FEC"}}}
	if err := single.update(ctx, 12, []session.EffectEvent{feces}, full); err != nil {
		t.Fatal(err)
	}
	items, err = single.plan(ctx, reduced)
	if err != nil || len(items) != 6 || !items[2].visible || items[3].visible || !items[4].visible || !items[5].visible {
		t.Fatalf("monolithic spray reduction or feces affected: %+v %v", items, err)
	}
	renderer := Renderer{room: "ENC-PRO-01", generation: 1, effects: pool}
	renderer.camera.X = 500
	renderer.enterWorld(ctx, session.WorldView{Encounter: "ENC-PRO-01", Generation: 1})
	if renderer.camera.X != 500 || len(renderer.effects.stains) == 0 {
		t.Fatal("ordinary frame reset presentation state")
	}
	renderer.enterWorld(ctx, session.WorldView{Encounter: "ENC-PRO-01", Generation: 2})
	if renderer.camera.X != 0 || len(renderer.effects.stains) != 0 || len(renderer.effects.active) != 0 {
		t.Fatal("same-room recovery retained old effects or camera")
	}
}

func TestReducedMonolithicSprayCadence(t *testing.T) {
	ctx := context.Background()
	part := session.VisualPart{Asset: "FX-BODY", Source: session.WorldRect{Width: 8, Height: 8}, Size: session.Point{X: 4, Y: 4}}
	full := settings.Accessibility{Blood: settings.ParticlesFull}
	reduced := settings.Accessibility{Blood: settings.ParticlesReduced}
	var pool effectPool
	visibleSpray := func(access settings.Accessibility) bool {
		t.Helper()
		items, err := pool.plan(ctx, access)
		if err != nil {
			t.Fatal(err)
		}
		var visible bool
		count := 0
		for _, item := range items {
			if !item.stain {
				count++
				visible = item.visible
			}
		}
		if count != 1 {
			t.Fatalf("expected one live spray, got %d", count)
		}
		return visible
	}
	// All events have even ticks and ordinal zero. Each expires before the
	// next arrives, so active-slice position cannot supply the cadence.
	for i := 0; i < 3; i++ {
		tick := uint64(100 + 100*i)
		event := session.EffectEvent{Tick: tick, Entity: "player", Kind: "hit", Asset: "FX-BODY", Part: part, Stain: part}
		if err := pool.update(ctx, tick, []session.EffectEvent{event}, full); err != nil {
			t.Fatal(err)
		}
		want := i%2 == 0
		if !visibleSpray(full) || visibleSpray(reduced) != want || visibleSpray(reduced) != want {
			t.Fatalf("event %d has unstable full/reduced selection", i)
		}
		stains := len(pool.stains)
		if err := pool.update(ctx, tick, []session.EffectEvent{event, event}, reduced); err != nil {
			t.Fatal(err)
		}
		if len(pool.stains) != stains || len(pool.active) != 1 || visibleSpray(reduced) != want {
			t.Fatalf("duplicate changed event %d selection", i)
		}
		if err := pool.update(ctx, tick+48, []session.EffectEvent{event}, reduced); err != nil {
			t.Fatal(err)
		}
		if len(pool.active) != 0 || pool.particles != 0 {
			t.Fatalf("expired event %d survived", i)
		}
	}
	// Three accepted sprays leave the next one hidden. A restored world must
	// reset the sequence even when the encounter identity is unchanged.
	renderer := Renderer{room: "ENC-PRO-01", generation: 1, effects: pool}
	renderer.enterWorld(ctx, session.WorldView{Encounter: "ENC-PRO-01", Generation: 2})
	pool = renderer.effects
	restored := session.EffectEvent{Tick: 400, Entity: "player", Kind: "hit", Asset: "FX-BODY", Part: part, Stain: part}
	if err := pool.update(ctx, 400, []session.EffectEvent{restored}, reduced); err != nil || !visibleSpray(reduced) {
		t.Fatalf("recovery did not restart the cadence: %v", err)
	}
	// Clock rollback also resets the pool, even without an encounter change.
	restored.Tick = 2
	if err := pool.update(ctx, 2, []session.EffectEvent{restored}, reduced); err != nil || !visibleSpray(reduced) {
		t.Fatalf("clock rollback did not restart the cadence: %v", err)
	}
}

func TestReducedMonolithicCadenceIgnoresOtherEffects(t *testing.T) {
	ctx := context.Background()
	blood := session.VisualPart{Asset: "FX-BODY"}
	feces := session.VisualPart{Asset: "FX-FEC"}
	events := []session.EffectEvent{
		{Tick: 10, Ordinal: 0, Entity: "a", Kind: "hit", Asset: "FX-BODY", Part: blood, Stain: blood, Position: session.Point{X: 1}},
		{Tick: 10, Ordinal: 1, Entity: "b", Kind: "feces", Asset: "FX-FEC", Part: feces, Position: session.Point{X: 2}},
		{Tick: 10, Ordinal: 2, Entity: "c", Kind: "hit", Asset: "FX-BODY", Part: blood, Stain: blood, Particles: []session.VisualPart{blood}, Position: session.Point{X: 3}},
		{Tick: 10, Ordinal: 3, Entity: "d", Kind: "removal", Asset: "FX-BODY", Part: blood, Stain: blood, Position: session.Point{X: 4}},
	}
	var pool effectPool
	access := settings.Accessibility{Blood: settings.ParticlesReduced}
	if err := pool.update(ctx, 10, events, access); err != nil {
		t.Fatal(err)
	}
	items, err := pool.plan(ctx, access)
	if err != nil {
		t.Fatal(err)
	}
	visible := make(map[float64]int)
	for _, item := range items {
		if !item.stain && item.visible {
			visible[item.point.X]++
		}
	}
	if visible[1] != 1 || visible[2] != 1 || visible[3] != 2 || visible[4] != 0 {
		t.Fatalf("interleaved effects changed monolithic cadence: %+v", visible)
	}
}

func TestLayerGeometryAndThreadFailures(t *testing.T) {
	part := session.VisualPart{Asset: "AN-GRI-IDLE", Animation: "AN-GRI-IDLE", Source: session.WorldRect{X: 4, Y: 5, Width: 10, Height: 12}, Size: session.Point{X: 10, Y: 12}}
	entity := session.EntityView{Parts: []session.VisualPart{part}}
	if err := validateActorHandoff(context.Background(), "AN-GRI-IDLE", &entity); err != nil {
		t.Fatal(err)
	}
	if err := validateActorHandoff(context.Background(), "AN-GRI-HURT", &entity); err == nil || !content.ErrInvalid.Has(err) {
		t.Fatal("stale animation frame survived interruption")
	}
	texture := rl.Texture2D{ID: 1, Width: 32, Height: 32}
	if err := validatePart(part, texture); err != nil {
		t.Fatal(err)
	}
	part.Source.Width = 40
	if err := validatePart(part, texture); err == nil {
		t.Fatal("out-of-bounds source accepted")
	}
	part.Source.Width = 10
	part.Size.X = math.NaN()
	if err := validatePart(part, texture); err == nil {
		t.Fatal("non-finite size accepted")
	}
	if _, err := New(context.Background(), nil, nil); err == nil {
		t.Fatal("missing cache accepted")
	}
	var renderer *Renderer
	if _, err := renderer.Draw(context.Background(), session.FrameView{}, 1280, 720, 0, settings.Accessibility{}); err == nil {
		t.Fatal("missing renderer accepted")
	}
	if err := renderer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

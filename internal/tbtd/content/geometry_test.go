// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

const bayTemplatesPath = "content/data/encounters/templates.json"
const bayVariantsPath = "content/data/encounters/variants.json"

func TestServiceBayExpansion(t *testing.T) {
	ctx := context.Background()
	campaign := campaignFixture(t)
	bays, err := LoadServiceBays(ctx, os.DirFS("../../.."), bayTemplatesPath, bayVariantsPath, campaign)
	if err != nil {
		t.Fatal(err)
	}
	if len(bays) != 96 {
		t.Fatalf("got %d tickets", len(bays))
	}
	deeds := campaign.Deeds(ctx)
	seen := map[DeedID]bool{}
	counts := map[string]int{}
	for _, bay := range bays {
		if seen[bay.Deed] || bay.Encounter == "" || bay.Anchor == "" || bay.Emergency == "" {
			t.Fatalf("invalid identity: %+v", bay)
		}
		seen[bay.Deed] = true
		counts[bay.Template.ID]++
		if len(bay.Hazards) != len(bay.Variant.Stations) {
			t.Fatalf("missing hazards for %s", bay.Deed)
		}
		for i, hazard := range bay.Hazards {
			if hazard.Waiting.Width != 160 || hazard.Waiting.X+hazard.Waiting.Width != hazard.Bounds.X || hazard.Waiting.X < 240 {
				t.Fatalf("unsafe waiting area for %s: %+v", bay.Deed, hazard)
			}
			crusher := bay.Template.ID == "BAY-SUP" && i == 0
			if hazard.Lethal != crusher || (crusher && (hazard.Bounds.X != 720 || hazard.Bounds.Width != 96 || hazard.WarningMS != 1200 || hazard.ActiveMS != 500 || hazard.InactiveMS != 2500)) || (!crusher && (hazard.WarningMS != 900 || hazard.ActiveMS != 700 || hazard.InactiveMS != 2000)) {
				t.Fatalf("incorrect hazard plan for %s: %+v", bay.Deed, hazard)
			}
			wantOffset := 0
			if bay.Variant.Number == 5 && i == 1 {
				wantOffset = 1800
			}
			if hazard.OffsetMS != wantOffset {
				t.Fatalf("incorrect hazard offset for %s", bay.Deed)
			}
		}
		if bay.Institution.Location == "" || len(bay.Institution.Bays) != 4 || len(bay.Institution.Boss.Protection) != 3 || len(bay.Safety.States) != 3 || len(bay.Safety.States[1].Damage) != 1 || len(bay.Safety.States[2].Solids) < 1 || len(bay.Safety.States[2].Solids) > 2 {
			t.Fatalf("incomplete location geometry for %s", bay.Deed)
		}
		var matched bool
		for _, deed := range deeds {
			if deed.Deed.ID != bay.Deed {
				continue
			}
			matched = true
			if deed.Deed.Encounter != bay.Encounter || deed.Deed.Anchor != bay.Anchor || deed.Emergency != bay.Emergency || deed.Variant != bay.Variant.Number || len(deed.Deed.Objects) != len(bay.Objects) {
				t.Fatalf("bad campaign binding for %s", bay.Deed)
			}
			for i, object := range bay.Objects {
				if object.Object.ID != deed.Deed.Objects[i] || object.Object.Encounter != bay.Encounter {
					t.Fatalf("bad object binding for %s", bay.Deed)
				}
			}
		}
		if !matched {
			t.Fatalf("unexpected ticket %s", bay.Deed)
		}
	}
	for _, family := range []string{"SUP", "RES", "UTL", "PRV"} {
		if counts["BAY-"+family] != 24 {
			t.Fatalf("wrong count for %s", family)
		}
	}
	again, err := LoadServiceBays(ctx, os.DirFS("../../.."), bayTemplatesPath, bayVariantsPath, campaign)
	if err != nil || !reflect.DeepEqual(bays, again) {
		t.Fatalf("expansion not deterministic: %v", err)
	}
	bays[0].Template.Objects[0].X = -1
	bays[0].Template.Terrain[0].Bounds.X = -1
	bays[0].Template.Passages[0].Bounds.X = -1
	bays[0].Variant.Stations[0] = -1
	bays[12].Template.Jumps[0].Speed = -1
	bays[0].Safety.States[1].Damage[0].X = -1
	bays[0].Institution.Boss.Platforms[0].X = -1
	if bays[1].Template.Objects[0].X < 0 || bays[1].Template.Terrain[0].Bounds.X < 0 || bays[1].Template.Passages[0].Bounds.X < 0 || bays[6].Variant.Stations[0] < 0 || bays[13].Template.Jumps[0].Speed < 0 || bays[1].Safety.States[1].Damage[0].X < 0 || bays[1].Institution.Boss.Platforms[0].X < 0 {
		t.Fatal("expanded bays share mutable slices")
	}
}

func TestServiceBayDataFailures(t *testing.T) {
	templates, err := os.ReadFile("../../../" + bayTemplatesPath)
	if err != nil {
		t.Fatal(err)
	}
	variants, err := os.ReadFile("../../../" + bayVariantsPath)
	if err != nil {
		t.Fatal(err)
	}
	campaign := campaignFixture(t)
	cases := []struct {
		name      string
		templates string
		variants  string
	}{
		{"duplicate template", strings.Replace(string(templates), "BAY-RES", "BAY-SUP", 1), string(variants)},
		{"missing role", strings.Replace(string(templates), `"role":"TROLLEY"`, `"role":"ACTOR"`, 1), string(variants)},
		{"missing vestibule refill", strings.Replace(string(templates), `"role":"REFILL","x":208`, `"role":"CONSOLE","x":208`, 1), string(variants)},
		{"moved checkpoint", strings.Replace(string(templates), `"role":"CHECKPOINT","x":96`, `"role":"CHECKPOINT","x":97`, 1), string(variants)},
		{"missing feature", strings.Replace(string(templates), `"id":"CATWALK"`, `"id":"OTHER"`, 1), string(variants)},
		{"invalid jump reference", strings.Replace(string(templates), `"landing":"STEP-1"`, `"landing":"MISSING"`, 1), string(variants)},
		{"missing return step", strings.Replace(string(templates), `"id":"RETURN-LOW"`, `"id":"MISSING"`, 1), string(variants)},
		{"disconnected return step", strings.Replace(string(templates), `"support":"RETURN-LOW"`, `"support":"STEP-1"`, 1), string(variants)},
		{"unreachable return step", strings.Replace(string(templates), `"takeoff_x":830,"speed":140`, `"takeoff_x":830,"speed":1`, 1), string(variants)},
		{"jump without runway", strings.Replace(string(templates), `"takeoff_x":400`, `"takeoff_x":1`, 1), string(variants)},
		{"unreachable jump", strings.Replace(string(templates), `"speed":260`, `"speed":10`, 1), string(variants)},
		{"hidden landing", strings.Replace(string(templates), `"landing":"STEP-1","camera":{"x":0,"y":-160,"width":1280,"height":720},"occluders":[]`, `"landing":"STEP-1","camera":{"x":0,"y":-160,"width":1280,"height":720},"occluders":[{"x":480,"y":80,"width":128,"height":8}]`, 1), string(variants)},
		{"offscreen landing", strings.Replace(string(templates), `"camera":{"x":0,"y":-160`, `"camera":{"x":700,"y":-160`, 1), string(variants)},
		{"bad fixed origin", strings.Replace(string(templates), `"role":"TROLLEY","x":320`, `"role":"TROLLEY","x":321`, 1), string(variants)},
		{"floating trolley", strings.Replace(string(templates), `"role":"TROLLEY","x":320,"y":0`, `"role":"TROLLEY","x":320,"y":500`, 1), string(variants)},
		{"floating plate", strings.Replace(string(templates), `"role":"PLATE","x":432,"y":0`, `"role":"PLATE","x":432,"y":80`, 1), string(variants)},
		{"floating latch", strings.Replace(string(templates), `"role":"LATCH","x":736,"y":0`, `"role":"LATCH","x":736,"y":80`, 1), string(variants)},
		{"floating switch", strings.Replace(string(templates), `"role":"SWITCH","x":960,"y":0`, `"role":"SWITCH","x":960,"y":80`, 1), string(variants)},
		{"floating recorder", strings.Replace(string(templates), `"role":"RECORDER","x":384,"y":0`, `"role":"RECORDER","x":384,"y":80`, 1), string(variants)},
		{"floating refill", strings.Replace(string(templates), `"id":"REFILL","x":608,"y":0`, `"id":"REFILL","x":608,"y":80`, 1), string(variants)},
		{"terminal outside receiving", strings.Replace(string(templates), `"role":"TERMINAL","x":1344,"y":0`, `"role":"TERMINAL","x":1100,"y":0`, 1), string(variants)},
		{"switch in vestibule", strings.Replace(string(templates), `"role":"SWITCH","x":960`, `"role":"SWITCH","x":100`, 1), string(variants)},
		{"adult before gate", strings.Replace(string(templates), `"role":"ACTOR","x":800`, `"role":"ACTOR","x":500`, 1), string(variants)},
		{"missing floor", strings.Replace(string(templates), `"floor":{"x":0,"y":-8,"width":1536,"height":8},`, ``, 1), string(variants)},
		{"broken floor", strings.Replace(string(templates), `"floor":{"x":0,"y":-8,"width":1536,"height":8}`, `"floor":{"x":0,"y":-8,"width":700,"height":8}`, 1), string(variants)},
		{"floating floor", strings.Replace(string(templates), `"floor":{"x":0,"y":-8,"width":1536,"height":8}`, `"floor":{"x":0,"y":1,"width":1536,"height":8}`, 1), string(variants)},
		{"bad dimensions", strings.Replace(string(templates), `"width":416`, `"width":0`, 1), string(variants)},
		{"moved catwalk", strings.Replace(string(templates), `"id":"CATWALK","x":560`, `"id":"CATWALK","x":561`, 1), string(variants)},
		{"moved receptor", strings.Replace(string(templates), `"role":"CAMERA","x":800`, `"role":"CAMERA","x":801`, 1), string(variants)},
		{"duplicate variant", string(templates), strings.Replace(string(variants), `"number":2`, `"number":1`, 1)},
		{"invalid enemy", string(templates), strings.Replace(string(variants), `"kind":"MELEE"`, `"kind":"BOSS"`, 1)},
		{"invalid station", string(templates), strings.Replace(string(variants), `"stations":[760]`, `"stations":[1600]`, 1)},
		{"incorrect station", string(templates), strings.Replace(string(variants), `"stations":[760]`, `"stations":[761]`, 1)},
		{"missing tutorial", string(templates), strings.Replace(string(variants), `"tutorial":true`, `"tutorial":false`, 1)},
		{"missing alcove", string(templates), strings.Replace(string(variants), `"waiting_alcove":true`, `"waiting_alcove":false`, 1)},
		{"invalid cover geometry", string(templates), strings.Replace(string(variants), `"cover":{"x":880,"y":0,"width":32`, `"cover":{"x":880,"y":0,"width":0`, 1)},
		{"narrow alcove geometry", string(templates), strings.Replace(string(variants), `"alcove":{"x":848,"y":0,"width":160`, `"alcove":{"x":848,"y":0,"width":159`, 1)},
		{"misplaced alcove geometry", string(templates), strings.Replace(string(variants), `"alcove":{"x":848`, `"alcove":{"x":1000`, 1)},
		{"unknown field", strings.Replace(string(templates), `"schema_version": 1`, `"schema_version": 1,"unknown":0`, 1), string(variants)},
		{"duplicate key", strings.Replace(string(templates), `"schema_version": 1`, `"schema_version": 1,"schema_version": 1`, 1), string(variants)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := fstest.MapFS{
				"templates.json": &fstest.MapFile{Data: []byte(tc.templates)},
				"variants.json":  &fstest.MapFile{Data: []byte(tc.variants)},
			}
			_, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign)
			if err == nil || !ErrInvalid.Has(err) {
				t.Fatalf("expected invalid content, got %v", err)
			}
		})
	}
	var source bayTemplateDocument
	if err := json.Unmarshal(templates, &source); err != nil {
		t.Fatal(err)
	}
	for i, template := range source.Templates {
		for j, point := range template.Objects {
			if point.Role == RoleCamera || point.Role == RolePaddle {
				continue
			}
			t.Run(template.ID+"/floating-"+string(point.Role), func(t *testing.T) {
				var mutated bayTemplateDocument
				if err := json.Unmarshal(templates, &mutated); err != nil {
					t.Fatal(err)
				}
				mutated.Templates[i].Objects[j].Y = 1
				data, err := json.Marshal(mutated)
				if err != nil {
					t.Fatal(err)
				}
				files := fstest.MapFS{
					"templates.json": &fstest.MapFile{Data: data},
					"variants.json":  &fstest.MapFile{Data: variants},
				}
				if _, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign); err == nil || !ErrInvalid.Has(err) {
					t.Fatalf("floating object accepted: %v", err)
				}
			})
		}
	}
	if err := validateBayVariant(BayVariant{Number: 1, Mode: "single", Enemies: []BayEnemy{{Kind: "MELEE", X: math.NaN()}}, Stations: []float64{760}}); err == nil {
		t.Fatal("accepted non-finite coordinate")
	}
	if _, err := LoadServiceBays(context.Background(), nil, "templates.json", "variants.json", campaign); err == nil {
		t.Fatal("accepted missing filesystem")
	}
	if _, err := LoadServiceBays(context.Background(), os.DirFS("../../.."), bayTemplatesPath, bayVariantsPath, nil); err == nil {
		t.Fatal("accepted missing campaign")
	}
}

func TestFloorJumpReachabilityAndVisibility(t *testing.T) {
	floor := Rect{X: 0, Y: -8, Width: 1536, Height: 8}
	jump := BayJump{TakeoffX: 200, Speed: 320, Camera: Rect{X: 0, Y: -160, Width: 1280, Height: 720}}
	landing := BayFeature{ID: "LANDING", X: 300, Y: 100, Width: 128, Height: 8}
	cases := []struct {
		name   string
		change func(*BayJump, *BayFeature)
		valid  bool
	}{
		{"maximum rise", func(*BayJump, *BayFeature) {}, true},
		{"above maximum rise", func(_ *BayJump, target *BayFeature) { target.Y = 100.01 }, false},
		{"maximum distance level", func(_ *BayJump, target *BayFeature) { target.X, target.Y = 380, 0 }, true},
		{"above maximum distance", func(_ *BayJump, target *BayFeature) { target.X, target.Y = 380.01, 0 }, false},
		{"combined rise and distance", func(_ *BayJump, target *BayFeature) { target.X = 380 }, false},
		{"narrow ledge at authored speed", func(jump *BayJump, target *BayFeature) {
			jump.Speed = 260
			target.X, target.Y, target.Width = 328, 80, 32
		}, true},
		{"narrow ledge too slow", func(jump *BayJump, target *BayFeature) {
			jump.Speed = 250
			target.X, target.Y, target.Width = 328, 80, 32
		}, false},
		{"narrow ledge too fast", func(jump *BayJump, target *BayFeature) {
			jump.Speed = 270
			target.X, target.Y, target.Width = 328, 80, 32
		}, false},
		{"takeoff overlapping side", func(_ *BayJump, target *BayFeature) { target.X = 210 }, false},
		{"takeoff touching side", func(_ *BayJump, target *BayFeature) { target.X = 214 }, false},
		{"too little speed", func(jump *BayJump, _ *BayFeature) { jump.Speed = 20 }, false},
		{"excessive speed", func(jump *BayJump, _ *BayFeature) { jump.Speed = 321 }, false},
		{"insufficient runup", func(jump *BayJump, _ *BayFeature) { jump.TakeoffX = 1 }, false},
		{"narrow landing", func(_ *BayJump, target *BayFeature) { target.Width = 27 }, false},
		{"landing outside view", func(jump *BayJump, _ *BayFeature) { jump.Camera.Width = 200 }, false},
		{"obscured landing", func(jump *BayJump, _ *BayFeature) {
			jump.Occluders = []Rect{{X: 300, Y: 100, Width: 20, Height: 8}}
		}, false},
		{"trajectory obstructed", func(jump *BayJump, _ *BayFeature) {
			jump.Blockers = []Rect{{X: 260, Y: 100, Width: 8, Height: 100}}
		}, false},
		{"run-up obstructed", func(jump *BayJump, _ *BayFeature) {
			jump.Blockers = []Rect{{X: 160, Y: 0, Width: 8, Height: 58}}
		}, false},
		{"non-finite speed", func(jump *BayJump, _ *BayFeature) { jump.Speed = math.NaN() }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jump, landing := jump, landing
			tc.change(&jump, &landing)
			err := validateFloorJump(floor, jump, landing)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, got %v", tc.valid, err)
			}
		})
	}
}

func TestReverseDropSafety(t *testing.T) {
	floor := Rect{X: 0, Y: -8, Width: 1536, Height: 8}
	jump := BayJump{Camera: Rect{X: 0, Y: -160, Width: 1280, Height: 720}}
	landing := BayFeature{X: 80, Y: 80, Width: 128, Height: 8}
	for _, tc := range []struct {
		name   string
		change func(*BayJump, *BayFeature)
		valid  bool
	}{
		{"clear", func(*BayJump, *BayFeature) {}, true},
		{"excessive fall", func(_ *BayJump, feature *BayFeature) { feature.Y = 101 }, false},
		{"blocked trajectory", func(jump *BayJump, _ *BayFeature) {
			jump.Blockers = []Rect{{X: 45, Y: 30, Width: 16, Height: 50}}
		}, false},
		{"hidden landing", func(jump *BayJump, _ *BayFeature) {
			jump.Occluders = []Rect{{X: 10, Y: 0, Width: 30, Height: 8}}
		}, false},
		{"outside camera", func(jump *BayJump, _ *BayFeature) { jump.Camera.X = 40 }, false},
		{"missing support", func(_ *BayJump, feature *BayFeature) { feature.X = 20 }, false},
		{"mirrored", func(jump *BayJump, feature *BayFeature) {
			jump.Direction = -1
			jump.Camera.X = 256
			feature.X = 1328
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jump, landing := jump, landing
			tc.change(&jump, &landing)
			err := validateReverseDrop(floor, jump, landing)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, got %v", tc.valid, err)
			}
		})
	}
}

func geometryFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	for _, name := range []string{"templates.json", "variants.json", "hospital.json", "church.json", "ministry.json", "institute.json"} {
		data, err := os.ReadFile("../../../content/data/encounters/" + name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	return files
}

func TestGeometryDocumentRoundTrip(t *testing.T) {
	files := geometryFiles(t)
	var templates bayTemplateDocument
	if err := json.Unmarshal(files["templates.json"].Data, &templates); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(templates)
	if err != nil {
		t.Fatal(err)
	}
	files["templates.json"] = &fstest.MapFile{Data: data}
	for _, name := range []string{"hospital.json", "church.json", "ministry.json", "institute.json"} {
		var doc InstitutionGeometry
		if err := json.Unmarshal(files[name].Data, &doc); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	if _, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaignFixture(t)); err != nil {
		t.Fatalf("round-trip rejected: %v", err)
	}
}

func TestInstitutionGeometryMutations(t *testing.T) {
	campaign := campaignFixture(t)
	for _, name := range []string{"hospital.json", "church.json", "ministry.json", "institute.json"} {
		for _, tc := range []struct {
			name   string
			change func(*InstitutionGeometry)
		}{
			{"wrong encounter", func(doc *InstitutionGeometry) { doc.Bays[0].Encounter = "ENC-HOS-99" }},
			{"missing bay", func(doc *InstitutionGeometry) { doc.Bays = doc.Bays[:3] }},
			{"missing connection", func(doc *InstitutionGeometry) { doc.Links = doc.Links[:3] }},
			{"wrong connection", func(doc *InstitutionGeometry) { doc.Links[0].To = "ENC-HOS-99" }},
			{"missing bypass entrance", func(doc *InstitutionGeometry) { doc.Links[1].Entry.X = 1200 }},
			{"wrong transit", func(doc *InstitutionGeometry) { doc.ExitGate = "G-DEATH" }},
			{"wrong closure", func(doc *InstitutionGeometry) { doc.Boss.Closure = "D-HOS-UNKNOWN" }},
			{"missing protection", func(doc *InstitutionGeometry) { doc.Boss.Protection = doc.Boss.Protection[:2] }},
			{"missing feature", func(doc *InstitutionGeometry) { doc.Boss.Features = doc.Boss.Features[:2] }},
			{"disconnected ascent", func(doc *InstitutionGeometry) { doc.Boss.Jumps[0].Support = 1 }},
			{"unsafe emergency station", func(doc *InstitutionGeometry) { doc.Bays[0].Danger.X = 96 }},
			{"unsafe aftermath station", func(doc *InstitutionGeometry) {
				doc.Bays[0].Aftermath.X = 1344
				doc.Bays[0].AftermathBlocking = true
			}},
			{"wrong anchor", func(doc *InstitutionGeometry) { doc.Boss.Anchor = "ANC-HOS-99" }},
			{"broken floor", func(doc *InstitutionGeometry) { doc.Boss.Floor.Width = 300 }},
			{"blocked anchor", func(doc *InstitutionGeometry) {
				doc.Boss.Solids = append(doc.Boss.Solids, Rect{X: -130, Y: 0, Width: 20, Height: 58})
			}},
			{"lethal anchor", func(doc *InstitutionGeometry) {
				doc.Boss.Lethal = append(doc.Boss.Lethal, Rect{X: -130, Y: 0, Width: 20, Height: 58})
			}},
			{"blocked refill", func(doc *InstitutionGeometry) {
				p := doc.Boss.Refills[0]
				doc.Boss.Solids = append(doc.Boss.Solids, Rect{X: p.X - 10, Y: p.Y, Width: 20, Height: 58})
			}},
			{"blocked closure", func(doc *InstitutionGeometry) {
				p := doc.Boss.TerminalPosition
				doc.Boss.Solids = append(doc.Boss.Solids, Rect{X: p.X - 10, Y: p.Y, Width: 20, Height: 58})
			}},
			{"blocked passage", func(doc *InstitutionGeometry) {
				doc.Boss.Solids = append(doc.Boss.Solids, Rect{X: 600, Y: 0, Width: 32, Height: 58})
			}},
			{"missing ascent", func(doc *InstitutionGeometry) { doc.Boss.Jumps = doc.Boss.Jumps[:1] }},
			{"unreachable ascent", func(doc *InstitutionGeometry) { doc.Boss.Jumps[0].Jump.Speed = 1 }},
			{"hidden ascent", func(doc *InstitutionGeometry) {
				p := doc.Boss.Platforms[0]
				doc.Boss.Jumps[0].Jump.Occluders = append(doc.Boss.Jumps[0].Jump.Occluders, Rect{X: p.X, Y: p.Y + p.Height, Width: 28, Height: 8})
			}},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				files := geometryFiles(t)
				var doc InstitutionGeometry
				if err := json.Unmarshal(files[name].Data, &doc); err != nil {
					t.Fatal(err)
				}
				tc.change(&doc)
				data, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				files[name] = &fstest.MapFile{Data: data}
				if _, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign); err == nil || !ErrInvalid.Has(err) {
					t.Fatalf("expected invalid geometry, got %v", err)
				}
			})
		}
	}
}

func TestVariantCoverCollisions(t *testing.T) {
	campaign := campaignFixture(t)
	for _, tc := range []struct {
		name   string
		change func(*BayVariant)
	}{
		{"oversized cover", func(variant *BayVariant) { variant.Cover.Width, variant.Cover.Height = 300, 560 }},
		{"enemy spawn overlap", func(variant *BayVariant) { variant.Cover.Width = 164 }},
		{"obstructed supported route", func(variant *BayVariant) { variant.Cover.Height = 100 }},
		{"obstructed mandatory jump", func(variant *BayVariant) { variant.Cover.Height = 300 }},
		{"cover blocks trolley", func(variant *BayVariant) { variant.CoverKind = "solid" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := geometryFiles(t)
			var doc bayVariantDocument
			if err := json.Unmarshal(files["variants.json"].Data, &doc); err != nil {
				t.Fatal(err)
			}
			tc.change(&doc.Variants[1])
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			files["variants.json"] = &fstest.MapFile{Data: data}
			if _, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign); err == nil || !ErrInvalid.Has(err) {
				t.Fatalf("accepted unsafe cover: %v", err)
			}
		})
	}
}

func TestMinistryGantryObstruction(t *testing.T) {
	campaign := campaignFixture(t)
	for _, tc := range []struct {
		name   string
		change func(*InstitutionGeometry)
	}{
		{"severed gantry", func(doc *InstitutionGeometry) {
			doc.Boss.Solids = append(doc.Boss.Solids, Rect{X: 600, Y: 160, Width: 32, Height: 400})
		}},
		{"detached refill", func(doc *InstitutionGeometry) {
			doc.Boss.Refills[0].Y = 0
			doc.Boss.Protection[1].Y = 0
		}},
		{"detached combat lift", func(doc *InstitutionGeometry) { doc.Boss.Features[2].Y = 0 }},
		{"mismatched gantry", func(doc *InstitutionGeometry) { doc.Boss.Features[1].Y = 80 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := geometryFiles(t)
			var doc InstitutionGeometry
			if err := json.Unmarshal(files["ministry.json"].Data, &doc); err != nil {
				t.Fatal(err)
			}
			tc.change(&doc)
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			files["ministry.json"] = &fstest.MapFile{Data: data}
			if _, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign); err == nil || !ErrInvalid.Has(err) {
				t.Fatalf("accepted disconnected gantry: %v", err)
			}
		})
	}
}

func TestBayRouteStateMutations(t *testing.T) {
	campaign := campaignFixture(t)
	for _, state := range []int{0, 1, 2} {
		for _, tc := range []struct {
			name   string
			change func(*bayTemplateDocument, int)
		}{
			{"missing bypass", func(doc *bayTemplateDocument, _ int) { doc.Safety.Jumps = doc.Safety.Jumps[:5] }},
			{"missing physical catwalk", func(doc *bayTemplateDocument, _ int) { doc.Templates[0].Terrain = doc.Templates[0].Terrain[1:] }},
			{"nonphysical catwalk", func(doc *bayTemplateDocument, _ int) { doc.Templates[0].Terrain[0].Kind = "decoration" }},
			{"missing rescue gate", func(doc *bayTemplateDocument, _ int) { doc.Templates[1].Terrain = nil }},
			{"missing opening barrier", func(doc *bayTemplateDocument, _ int) { doc.Templates[2].Terrain = doc.Templates[2].Terrain[:4] }},
			{"shifted opening barrier", func(doc *bayTemplateDocument, _ int) { doc.Templates[2].Terrain[4].Bounds.X++ }},
			{"missing paddle collider", func(doc *bayTemplateDocument, _ int) { doc.Templates[2].Terrain = doc.Templates[2].Terrain[:5] }},
			{"missing throw ledge", func(doc *bayTemplateDocument, _ int) { doc.Templates[3].Terrain = doc.Templates[3].Terrain[1:] }},
			{"missing camera collider", func(doc *bayTemplateDocument, _ int) { doc.Templates[3].Terrain = doc.Templates[3].Terrain[:1] }},
			{"missing side passage", func(doc *bayTemplateDocument, _ int) { doc.Templates[0].Passages = nil }},
			{"blocked side passage", func(doc *bayTemplateDocument, state int) {
				if state == 0 {
					doc.Safety.States[state].Solids = append(doc.Safety.States[state].Solids, Rect{X: 700, Y: 0, Width: 16, Height: 58})
				} else {
					doc.Safety.States[state].Solids = append(doc.Safety.States[state].Solids, Rect{X: 600, Y: 240, Width: 32, Height: 58})
				}
			}},
			{"duplicate bypass ID", func(doc *bayTemplateDocument, _ int) { doc.Safety.Jumps[1].Jump.ID = doc.Safety.Jumps[0].Jump.ID }},
			{"duplicate action and bypass ID", func(doc *bayTemplateDocument, _ int) { doc.Safety.Jumps[0].Jump.ID = doc.Templates[0].Jumps[0].ID }},
			{"missing bypass ID", func(doc *bayTemplateDocument, _ int) { doc.Safety.Jumps[0].Jump.ID = "" }},
			{"invalid bypass ID", func(doc *bayTemplateDocument, _ int) { doc.Safety.Jumps[0].Jump.ID = " " }},
			{"blocked bypass", func(doc *bayTemplateDocument, state int) {
				doc.Safety.States[state].Solids = append(doc.Safety.States[state].Solids, Rect{X: 600, Y: 240, Width: 32, Height: 58})
			}},
			{"blocked anchor", func(doc *bayTemplateDocument, state int) {
				doc.Safety.States[state].Solids = append(doc.Safety.States[state].Solids, Rect{X: 82, Y: 0, Width: 28, Height: 58})
			}},
			{"lethal refuge", func(doc *bayTemplateDocument, state int) {
				doc.Safety.States[state].Lethal = append(doc.Safety.States[state].Lethal, Rect{X: 1402, Y: 0, Width: 28, Height: 58})
			}},
			{"blocked terminal", func(doc *bayTemplateDocument, state int) {
				doc.Safety.States[state].Solids = append(doc.Safety.States[state].Solids, Rect{X: 1330, Y: 0, Width: 28, Height: 58})
			}},
			{"blocked crusher control", func(doc *bayTemplateDocument, state int) {
				doc.Safety.States[state].Solids = append(doc.Safety.States[state].Solids, Rect{X: 658, Y: 0, Width: 28, Height: 58})
			}},
			{"blocked projectile opening", func(doc *bayTemplateDocument, state int) {
				doc.Safety.States[state].Solids = append(doc.Safety.States[state].Solids, Rect{X: 645, Y: 115, Width: 8, Height: 10})
			}},
			{"blocked receptor lane", func(doc *bayTemplateDocument, state int) {
				doc.Safety.States[state].Solids = append(doc.Safety.States[state].Solids, Rect{X: 740, Y: 119, Width: 12, Height: 2})
			}},
			{"missing refuge protection", func(doc *bayTemplateDocument, _ int) { doc.Safety.RefugeProtect.Width = 10 }},
			{"unreachable bypass", func(doc *bayTemplateDocument, _ int) { doc.Safety.Jumps[2].Jump.Speed = 1 }},
			{"hidden bypass", func(doc *bayTemplateDocument, _ int) {
				doc.Safety.Jumps[0].Jump.Occluders = []Rect{{X: 80, Y: 80, Width: 28, Height: 8}}
			}},
		} {
			t.Run(docStateName(state)+"/"+tc.name, func(t *testing.T) {
				files := geometryFiles(t)
				var doc bayTemplateDocument
				if err := json.Unmarshal(files["templates.json"].Data, &doc); err != nil {
					t.Fatal(err)
				}
				tc.change(&doc, state)
				data, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				files["templates.json"] = &fstest.MapFile{Data: data}
				if _, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign); err == nil || !ErrInvalid.Has(err) {
					t.Fatalf("expected invalid route, got %v", err)
				}
			})
		}
	}
}

func TestPermitLobbyMutations(t *testing.T) {
	campaign := campaignFixture(t)
	for _, tc := range []struct {
		name   string
		change func(*InstitutionGeometry)
	}{
		{"missing lobby", func(doc *InstitutionGeometry) { doc.ReturnLobbies = nil }},
		{"wrong anchor", func(doc *InstitutionGeometry) { doc.ReturnLobbies[0].Anchor = "ANC-MIN-OUT" }},
		{"unprotected counter", func(doc *InstitutionGeometry) { doc.ReturnLobbies[0].Protection.Width = 200 }},
		{"blocked route", func(doc *InstitutionGeometry) {
			doc.ReturnLobbies[0].Solids = []Rect{{X: 200, Y: 0, Width: 20, Height: 58}}
		}},
		{"unsafe anchor", func(doc *InstitutionGeometry) {
			doc.ReturnLobbies[0].Lethal = []Rect{{X: 310, Y: 0, Width: 20, Height: 58}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := geometryFiles(t)
			var doc InstitutionGeometry
			if err := json.Unmarshal(files["ministry.json"].Data, &doc); err != nil {
				t.Fatal(err)
			}
			tc.change(&doc)
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			files["ministry.json"] = &fstest.MapFile{Data: data}
			if _, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign); err == nil || !ErrInvalid.Has(err) {
				t.Fatalf("accepted unsafe permit lobby: %v", err)
			}
		})
	}
}

func TestPrologueGeometry(t *testing.T) {
	campaign := campaignFixture(t)
	files := geometryFiles(t)
	bays, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign)
	if err != nil {
		t.Fatal(err)
	}
	if len(bays[0].Institution.Prologue) != 2 || len(bays[24].Institution.Prologue) != 1 || bays[0].Institution.Prologue[0].Encounter != "ENC-PRO-01" || bays[0].Institution.Prologue[1].Encounter != "ENC-PRO-02" || bays[24].Institution.Prologue[0].Encounter != "ENC-PRO-03" {
		t.Fatal("missing prologue")
	}
	bays[0].Institution.Prologue[0].Jumps[0].Jump.Speed = 1
	if bays[1].Institution.Prologue[0].Jumps[0].Jump.Speed == 1 {
		t.Fatal("prologue geometry shared between tickets")
	}
	for _, tc := range []struct {
		name   string
		file   string
		change func(*InstitutionGeometry)
	}{
		{"missing annex", "hospital.json", func(doc *InstitutionGeometry) { doc.Prologue = doc.Prologue[1:] }},
		{"missing jump ID", "hospital.json", func(doc *InstitutionGeometry) { doc.Prologue[0].Jumps[0].Jump.ID = "" }},
		{"invalid jump ID", "hospital.json", func(doc *InstitutionGeometry) { doc.Prologue[0].Jumps[0].Jump.ID = " " }},
		{"duplicate jump ID", "hospital.json", func(doc *InstitutionGeometry) { doc.Prologue[0].Jumps[1].Jump.ID = doc.Prologue[0].Jumps[0].Jump.ID }},
		{"hidden annex landing", "hospital.json", func(doc *InstitutionGeometry) {
			doc.Prologue[0].Jumps[0].Jump.Occluders = []Rect{{X: 80, Y: 80, Width: 28, Height: 8}}
		}},
		{"blocked annex jump", "hospital.json", func(doc *InstitutionGeometry) {
			doc.Prologue[0].Solids = []Rect{{X: 60, Y: 90, Width: 20, Height: 80}}
		}},
		{"broken annex steps", "hospital.json", func(doc *InstitutionGeometry) { doc.Prologue[0].Platforms[0].Y = 100 }},
		{"wrong heart attack distance", "hospital.json", func(doc *InstitutionGeometry) { doc.Prologue[1].Objective.X++ }},
		{"blocked corridor", "hospital.json", func(doc *InstitutionGeometry) {
			doc.Prologue[1].Solids = []Rect{{X: 200, Y: 0, Width: 20, Height: 58}}
		}},
		{"blocked courtyard anchor", "church.json", func(doc *InstitutionGeometry) {
			doc.Prologue[0].Lethal = []Rect{{X: 690, Y: 0, Width: 20, Height: 58}}
		}},
		{"missing coffin", "church.json", func(doc *InstitutionGeometry) { doc.Prologue[0].Features = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := geometryFiles(t)
			var doc InstitutionGeometry
			if err := json.Unmarshal(files[tc.file].Data, &doc); err != nil {
				t.Fatal(err)
			}
			tc.change(&doc)
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			files[tc.file] = &fstest.MapFile{Data: data}
			if _, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign); err == nil || !ErrInvalid.Has(err) {
				t.Fatalf("accepted invalid prologue: %v", err)
			}
		})
	}
}

func TestEmergencyStructures(t *testing.T) {
	campaign := campaignFixture(t)
	files := geometryFiles(t)
	bays, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign)
	if err != nil {
		t.Fatal(err)
	}
	for _, bay := range bays {
		for _, room := range bay.Institution.Bays {
			if room.Encounter == bay.Encounter {
				if !validRect(room.Space.Refuge) || !validRect(room.Space.Envelope) || !validRect(room.Space.Barrier) || len(bay.Safety.States[1].Solids) == 0 || bay.Safety.States[1].Damage[0] != room.Space.Envelope {
					t.Fatalf("incomplete emergency structures for %s", bay.Deed)
				}
				if room.Space.Kind == "wave" && (room.Space.Speed != 220 || room.Space.End.X <= room.Space.Start.X || room.Space.End.X+room.Space.End.Width != room.Space.Barrier.X) {
					t.Fatalf("invalid wave for %s", bay.Deed)
				}
			}
		}
	}
	for _, name := range []string{"hospital.json", "church.json", "ministry.json", "institute.json"} {
		for i, family := range []string{"SUP", "RES", "UTL", "PRV"} {
			for _, tc := range []struct {
				name   string
				change func(*InstitutionBay)
			}{
				{"missing refuge", func(room *InstitutionBay) { room.Space.Refuge = Rect{} }},
				{"blocked refuge", func(room *InstitutionBay) { room.Space.Refuge.X = room.Space.Envelope.X }},
				{"missing barrier", func(room *InstitutionBay) { room.Space.Barrier = Rect{} }},
				{"barrier not stopping hazard", func(room *InstitutionBay) { room.Space.Barrier.X += 16 }},
				{"barrier blocks refuge route", func(room *InstitutionBay) { room.Space.Barrier.Width = 192 }},
				{"removed envelope", func(room *InstitutionBay) { room.Space.Envelope = Rect{} }},
			} {
				t.Run(name+"/"+family+"/"+tc.name, func(t *testing.T) {
					files := geometryFiles(t)
					var doc InstitutionGeometry
					if err := json.Unmarshal(files[name].Data, &doc); err != nil {
						t.Fatal(err)
					}
					tc.change(&doc.Bays[i])
					data, err := json.Marshal(doc)
					if err != nil {
						t.Fatal(err)
					}
					files[name] = &fstest.MapFile{Data: data}
					if _, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign); err == nil || !ErrInvalid.Has(err) {
						t.Fatalf("accepted invalid emergency structure: %v", err)
					}
				})
			}
		}
	}
}

func TestWaveGeometryMutations(t *testing.T) {
	campaign := campaignFixture(t)
	for _, tc := range []struct {
		file  string
		index int
	}{{"hospital.json", 1}, {"ministry.json", 0}, {"ministry.json", 1}, {"institute.json", 0}} {
		for _, mutate := range []struct {
			name   string
			change func(*EmergencySpace)
		}{
			{"missing stop", func(space *EmergencySpace) { space.Barrier = Rect{} }},
			{"short envelope", func(space *EmergencySpace) { space.Envelope.Width-- }},
			{"wrong speed", func(space *EmergencySpace) { space.Speed = 219 }},
			{"missing end", func(space *EmergencySpace) { space.End = Rect{} }},
			{"unconfined wave", func(space *EmergencySpace) { space.End.X += 20 }},
		} {
			t.Run(tc.file+"/"+docStateName(tc.index)+"/"+mutate.name, func(t *testing.T) {
				files := geometryFiles(t)
				var doc InstitutionGeometry
				if err := json.Unmarshal(files[tc.file].Data, &doc); err != nil {
					t.Fatal(err)
				}
				mutate.change(&doc.Bays[tc.index].Space)
				data, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				files[tc.file] = &fstest.MapFile{Data: data}
				if _, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign); err == nil || !ErrInvalid.Has(err) {
					t.Fatalf("accepted invalid wave: %v", err)
				}
			})
		}
	}
}

func TestEveryInstitutionBayRejectsUnsafeStates(t *testing.T) {
	campaign := campaignFixture(t)
	for _, name := range []string{"hospital.json", "church.json", "ministry.json", "institute.json"} {
		for i, family := range []string{"SUP", "RES", "UTL", "PRV"} {
			for _, state := range []string{"emergency", "aftermath"} {
				t.Run(name+"/"+family+"/"+state, func(t *testing.T) {
					files := geometryFiles(t)
					var doc InstitutionGeometry
					if err := json.Unmarshal(files[name].Data, &doc); err != nil {
						t.Fatal(err)
					}
					if state == "emergency" {
						doc.Bays[i].Danger = Rect{X: 80, Y: 0, Width: 32, Height: 58}
					} else {
						doc.Bays[i].Aftermath = Rect{X: 1330, Y: 0, Width: 28, Height: 58}
						doc.Bays[i].AftermathBlocking = true
					}
					data, err := json.Marshal(doc)
					if err != nil {
						t.Fatal(err)
					}
					files[name] = &fstest.MapFile{Data: data}
					if _, err := LoadServiceBays(context.Background(), files, "templates.json", "variants.json", campaign); err == nil || !ErrInvalid.Has(err) {
						t.Fatalf("accepted unsafe %s geometry: %v", state, err)
					}
				})
			}
		}
	}
}

func docStateName(state int) string {
	return []string{"normal", "emergency", "aftermath"}[state]
}

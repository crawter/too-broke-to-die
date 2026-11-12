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
	bays[0].Variant.Stations[0] = -1
	bays[12].Template.Jumps[0].Speed = -1
	if bays[1].Template.Objects[0].X < 0 || bays[6].Variant.Stations[0] < 0 || bays[13].Template.Jumps[0].Speed < 0 {
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
		{"jump without runway", strings.Replace(string(templates), `"takeoff_x":400`, `"takeoff_x":1`, 1), string(variants)},
		{"unreachable jump", strings.Replace(string(templates), `"speed":260`, `"speed":10`, 1), string(variants)},
		{"hidden landing", strings.Replace(string(templates), `"occluders":[]`, `"occluders":[{"x":480,"y":80,"width":128,"height":8}]`, 1), string(variants)},
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

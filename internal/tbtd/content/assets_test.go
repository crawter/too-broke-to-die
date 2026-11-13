// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func visualFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	for _, name := range []string{"characters", "environments", "animations", "interface"} {
		path := "content/catalogue/" + name + ".json"
		data, err := os.ReadFile(filepath.Join("..", "..", "..", filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		files[path] = &fstest.MapFile{Data: data}
	}
	return files
}

func visualFixture(t *testing.T) (*VisualAssets, *Campaign, *RussianText) {
	t.Helper()
	text, campaign := russianFixture(t)
	assets, err := LoadVisualAssets(context.Background(), visualFiles(t), campaign, text)
	if err != nil {
		t.Fatal(err)
	}
	return assets, campaign, text
}

func mustVisual(t *testing.T, assets *VisualAssets, id string) VisualRecord {
	t.Helper()
	record, ok := assets.Record(id)
	if !ok {
		t.Fatalf("missing %s", id)
	}
	return record
}

func TestVisualInventoryAndIsolation(t *testing.T) {
	ctx := context.Background()
	assets, campaign, text := visualFixture(t)
	repeated, err := LoadVisualAssets(ctx, visualFiles(t), campaign, text)
	if err != nil {
		t.Fatal(err)
	}
	all := assets.Records(ctx)
	if !reflect.DeepEqual(all, repeated.Records(ctx)) {
		t.Fatal("expansion is not deterministic")
	}
	counts := map[string]int{}
	for i, record := range all {
		if i > 0 && all[i-1].ID >= record.ID {
			t.Fatalf("unsorted or duplicate ID %s", record.ID)
		}
		counts[record.Kind]++
		if record.FinalExportPresent || record.Technical.ExportPath != "" || record.Technical.SourcePath != "" || record.Technical.LogicalWidth != 0 || record.Technical.LogicalHeight != 0 {
			t.Fatalf("invented media for %s", record.ID)
		}
		if record.Technical.SourceScale != 2 || record.Technical.AtlasMax != 2048 || record.Technical.Padding != 4 || record.Technical.Extrusion != 4 || !record.Technical.CollisionOverlayPreview {
			t.Fatalf("invalid technical requirements: %+v", record)
		}
	}
	for kind, count := range map[string]int{"character-sheet": 8, "expression": 48, "enemy-set": 8, "rescue-adult": 24, "crowd-kit": 4, "service-bay": 16, "boss-arena": 4, "prologue": 3, "ending": 4, "transit": 4, "emergency": 48, "player-animation": 21, "enemy-animation": 64, "rescue-animation": 120, "boss-animation": 44, "sign-layer": 17, "effect": 4} {
		if counts[kind] != count {
			t.Errorf("%s: got %d want %d", kind, counts[kind], count)
		}
	}
	if !errors.Is(assets.RequireFinalMedia(ctx), ErrFinalMediaMissing) {
		t.Fatal("undelivered media must block final acceptance")
	}
	if _, ok := assets.Record("ART-DOES-NOT-EXIST"); ok {
		t.Fatal("unknown record exists")
	}
	original := mustVisual(t, assets, "AN-GRI-THROW")
	changed := mustVisual(t, assets, "AN-GRI-THROW")
	changed.Animation.Markers[0].Event = "changed"
	changed.Animation.Sockets[0] = "changed"
	changed.Bindings[0] = "changed"
	if !reflect.DeepEqual(original, mustVisual(t, assets, original.ID)) {
		t.Fatal("record aliases catalogue")
	}
	all[0].ID = "changed"
	if assets.Records(ctx)[0].ID == "changed" {
		t.Fatal("record slice aliases catalogue")
	}
}

func TestVisualCampaignBindings(t *testing.T) {
	ctx := context.Background()
	assets, campaign, text := visualFixture(t)
	for _, deed := range campaign.Deeds(ctx) {
		code := strings.TrimPrefix(string(deed.Location), "LOC-")
		if deed.Family == "END" {
			arena := mustVisual(t, assets, "ART-BOSS-"+code)
			milestone := map[string]string{"HOS": "NAR-05", "CHU": "NAR-06", "MIN": "NAR-07", "INS": "NAR-09"}[code]
			ids := []string{string(deed.Location), string(deed.Deed.ID), string(deed.Deed.Consequence), string(deed.Deed.Encounter), string(deed.Deed.Anchor), milestone}
			for _, object := range deed.Deed.Objects {
				ids = append(ids, string(object))
			}
			for _, id := range ids {
				if !slices.Contains(arena.Bindings, id) {
					t.Errorf("%s lacks %s", arena.ID, id)
				}
			}
			continue
		}
		bay := mustVisual(t, assets, "ART-BAY-"+code+"-"+deed.Family)
		for _, id := range []string{string(deed.Location), string(deed.Deed.Encounter), string(deed.Deed.Anchor), string(deed.Deed.ID), string(deed.Deed.Consequence)} {
			if !slices.Contains(bay.Bindings, id) {
				t.Errorf("%s lacks %s", bay.ID, id)
			}
		}
		for _, objectID := range deed.Deed.Objects {
			for _, object := range campaign.Objects(ctx) {
				if object.ID != objectID {
					continue
				}
				finish := mustVisual(t, assets, fmt.Sprintf("ART-OBJ-%s-%s-%s", code, deed.Family, object.Role))
				for _, id := range []string{string(objectID), string(deed.Deed.ID), "TXT-" + string(deed.Deed.ID)} {
					if !slices.Contains(finish.Bindings, id) {
						t.Errorf("%s lacks %s", finish.ID, id)
					}
				}
			}
		}
		if deed.Family == "RES" {
			adult := mustVisual(t, assets, fmt.Sprintf("ART-RES-%s-%02d", code, deed.Variant))
			name := "TXT-OBJ-" + string(deed.Deed.ID) + "-ACTOR"
			entry, ok := text.Entry(name)
			if !ok || entry.Text != deed.Label || !slices.Contains(adult.Bindings, name) {
				t.Errorf("missing named adult for %s", deed.Deed.ID)
			}
			for _, state := range []string{"WAIT", "FOLLOW", "SAFESTOP", "REFUGE", "INCAPACITATED"} {
				mustVisual(t, assets, fmt.Sprintf("AN-RES-%s-%02d-%s", code, deed.Variant, state))
			}
		}
	}
	for _, batch := range campaign.Batches(ctx) {
		for _, stage := range []string{"WARN", "ACTIVE", "AFTER"} {
			record := mustVisual(t, assets, "ART-"+string(batch.Emergency.ID)+"-"+stage)
			if !slices.Contains(record.Bindings, string(batch.Emergency.ID)) || !slices.Contains(record.Bindings, string(batch.Emergency.Anchor)) {
				t.Errorf("bad emergency binding %s", record.ID)
			}
		}
	}
	tableau := mustVisual(t, assets, "ART-END-TABLEAU")
	if tableau.Tableau == nil || tableau.Tableau.AspectWidth != 16 || tableau.Tableau.AspectHeight != 9 || !tableau.Tableau.WingMinimumQuarter || !tableau.Tableau.RibbonAndMinisterLowerCenter || !tableau.Tableau.BossesFlankMinister || !tableau.Tableau.FlamesCrossWingWindows || !tableau.Tableau.NinaEvacuatesOpposite || !tableau.Tableau.ClosingTextClearUpperBand {
		t.Fatal("incomplete mandatory tableau composition")
	}
	tableau.Tableau.WingMinimumQuarter = false
	if !mustVisual(t, assets, "ART-END-TABLEAU").Tableau.WingMinimumQuarter {
		t.Fatal("tableau aliases catalogue")
	}
	for _, binding := range []string{"END-TABLEAU", "NAR-12", "TXT-SIGN-HOS-WING", "TXT-ENDING-TABLEAU", "ART-CHR-GRI-MINISTER", "ART-CHR-POT", "ART-CHR-SAV", "ART-CHR-BLA", "ART-CHR-LEV", "ART-CHR-NIN"} {
		if !slices.Contains(tableau.Bindings, binding) {
			t.Errorf("tableau lacks %s", binding)
		}
	}
	sign := mustVisual(t, assets, "ART-TXT-SIGN-HOS-WING")
	if !slices.Contains(sign.Bindings, "TXT-SIGN-HOS-WING") {
		t.Fatal("sign text is not separately addressable")
	}
}

func TestVisualClipSynchronization(t *testing.T) {
	assets, _, _ := visualFixture(t)
	for _, test := range []struct {
		id       string
		duration int
		event    string
		time     int
	}{
		{"AN-GRI-THROW", 400, "release", 100},
		{"AN-GRI-MELEE", 450, "active_end", 200},
		{"AN-GRI-MELEE-HEADBUTT", 500, "recovery_end", 500},
		{"AN-GRI-REMOVE", 350, "separation", 350},
		{"AN-GRI-REGEN", 1500, "completion", 1500},
		{"AN-GRI-HURT", 180, "immunity_end", 800},
		{"AN-BOSS-HOS-A-PREP", 700, "target_lock", 700},
		{"AN-BOSS-HOS-A-RECOVER", 1400, "minimum_recovery", 1400},
	} {
		clip := mustVisual(t, assets, test.id).Animation
		if clip == nil || clip.DurationMS != test.duration || !slices.Contains(clip.Markers, AnimationMarker{Event: test.event, Milliseconds: test.time, Timing: "milliseconds"}) {
			t.Errorf("incorrect clip %s: %+v", test.id, clip)
		}
	}
}

func TestPrologueAndEnvironmentBindings(t *testing.T) {
	assets, _, _ := visualFixture(t)
	for _, test := range []struct {
		id         string
		narratives []string
	}{
		{"ART-PRO-01", []string{"NAR-01"}},
		{"ART-PRO-02", []string{"NAR-01"}},
		{"ART-PRO-03", []string{"NAR-02", "NAR-03"}},
	} {
		var narratives []string
		for _, binding := range mustVisual(t, assets, test.id).Bindings {
			if strings.HasPrefix(binding, "NAR-") {
				narratives = append(narratives, binding)
			}
		}
		if !slices.Equal(narratives, test.narratives) {
			t.Errorf("%s narratives: %v", test.id, narratives)
		}
	}
	for id, expected := range map[string][]string{
		"ART-SET-HOS":  {"#DDD6BE", "#77A59A", "#A68A52", "#293D40"},
		"ART-SET-CHU":  {"#D8BD7F", "#654358", "#30292D", "#B99A54"},
		"ART-SET-MIN":  {"#D1C19B", "#34495B", "#7F8278", "#69543C"},
		"ART-SET-INS":  {"#705783", "#A7C3AF", "#25262C", "#C67B8E"},
		"ART-SET-HELL": {"#C1B4A2", "#25211F", "#A85636", "#8D7755"},
		"ART-SET-CER":  {"#E7D4A5", "#9B3948", "#45434A", "#E58A3E"},
	} {
		palette := mustVisual(t, assets, id).Palette
		if !slices.Equal(palette, expected) || !validVisualPalette(palette) {
			t.Errorf("%s palette: %v", id, palette)
		}
		palette[0] = "#bad"
		if validVisualPalette(palette) {
			t.Errorf("%s accepted invalid swatch", id)
		}
	}
	if validVisualPalette([]string{"#000000"}) {
		t.Fatal("accepted incomplete palette")
	}
}

func TestAllAttackAnimationBoundaries(t *testing.T) {
	assets, _, _ := visualFixture(t)
	expected := map[string]int{"HOS-A": 1, "HOS-B": 338, "HOS-C": 48, "CHU-A": 1, "CHU-B": 48, "CHU-C": 436, "MIN-A": 270, "MIN-B": 72, "MIN-C": 270, "INS-A": 48, "INS-B": 72, "INS-C": 120}
	for _, boss := range BossDefinitions(context.Background()) {
		for _, attack := range boss.Attacks {
			prep := mustVisual(t, assets, "AN-BOSS-"+attack.ID+"-PREP").Animation
			active := mustVisual(t, assets, "AN-BOSS-"+attack.ID+"-ACTIVE").Animation
			recoverClip := mustVisual(t, assets, "AN-BOSS-"+attack.ID+"-RECOVER").Animation
			if prep.DurationMS != attack.TelegraphMS || !slices.Contains(prep.Markers, AnimationMarker{Event: "target_lock", Milliseconds: attack.TelegraphMS, Timing: "milliseconds"}) {
				t.Errorf("%s telegraph", attack.ID)
			}
			if active.DurationMode != "simulation" || active.DurationMS != attack.ActiveMS || active.TickRate != 120 || active.MinimumTicks != expected[attack.ID] || !slices.Contains(active.Markers, AnimationMarker{Event: "attack_end", Timing: "event"}) {
				t.Errorf("%s active: %+v", attack.ID, active)
			}
			if recoverClip.DurationMode != "minimum" || recoverClip.DurationMS != attack.RecoveryMS || !slices.Contains(recoverClip.Markers, AnimationMarker{Event: "recovery_end", Timing: "event"}) {
				t.Errorf("%s recovery: %+v", attack.ID, recoverClip)
			}
		}
	}
	for id, ticks := range map[string][]int{"CHU-C": {0, 42, 84}, "MIN-A": {30, 60, 90}, "INS-C": {0, 0}, "HOS-B": {0}} {
		clip := mustVisual(t, assets, "AN-BOSS-"+id+"-ACTIVE").Animation
		for i, tick := range ticks {
			if !slices.Contains(clip.Markers, AnimationMarker{Event: fmt.Sprintf("release-%d", i+1), Tick: tick, Timing: "ticks"}) {
				t.Errorf("%s release %d at tick %d missing", id, i+1, tick)
			}
		}
	}
	for _, code := range []string{"HOS", "CHU", "MIN", "INS"} {
		for _, kind := range []string{"MEL", "RNG"} {
			telegraph := 500
			if kind == "RNG" {
				telegraph = 650
			}
			prep := mustVisual(t, assets, "AN-EN-"+code+"-"+kind+"-TELEGRAPH").Animation
			active := mustVisual(t, assets, "AN-EN-"+code+"-"+kind+"-ACTIVE").Animation
			if !slices.Contains(prep.Markers, AnimationMarker{Event: "strike_or_release", Milliseconds: telegraph, Timing: "milliseconds"}) || !slices.Contains(active.Markers, AnimationMarker{Event: "strike_or_release", Timing: "event"}) {
				t.Errorf("%s %s missing release", code, kind)
			}
		}
	}
}

func TestAnimationDeliveryAndValidation(t *testing.T) {
	assets, _, _ := visualFixture(t)
	for _, id := range []string{"AN-GRI-THROW", "AN-GRI-BOTH-ARM-LOSS", "AN-GRI-BOTH-LEG-LOSS"} {
		record := mustVisual(t, assets, id)
		clip := record.Animation
		if !slices.Equal(clip.Layers, []string{"torso", "head", "sockets", "limbs"}) || len(clip.SocketPositions) != 4 {
			t.Fatalf("%s missing modular layers or sockets", id)
		}
		for _, position := range clip.SocketPositions {
			if position.Resolved || position.X != 0 || position.Y != 0 {
				t.Errorf("%s invents delivered coordinates", id)
			}
		}
		clip.SocketPositions[0] = SocketPosition{Socket: "LIM-LA", Resolved: true}
		if err := validateVisualAnimation(record); err != nil {
			t.Fatalf("resolved zero position must be valid: %v", err)
		}
		if mustVisual(t, assets, id).Animation.SocketPositions[0].Resolved {
			t.Fatal("socket coordinates alias catalogue")
		}
		clip.SocketPositions[0].X = math.NaN()
		if err := validateVisualAnimation(record); err == nil {
			t.Fatal("non-finite position accepted")
		}
	}
	for _, id := range []string{"AN-EN-HOS-MEL-ACTIVE", "AN-RES-CHU-01-REFUGE", "AN-BOSS-INS-A-PREP"} {
		clip := mustVisual(t, assets, id).Animation
		if clip.Poses != 0 || clip.PoseCountStatus != "unresolved" || len(clip.PoseRequirements) == 0 {
			t.Errorf("%s lacks pose production requirement", id)
		}
	}
	bad := mustVisual(t, assets, "AN-GRI-THROW")
	bad.Animation.DurationMS = -1
	if err := validateVisualAnimation(bad); err == nil {
		t.Fatal("invalid duration accepted")
	}
	bad = mustVisual(t, assets, "AN-BOSS-CHU-C-ACTIVE")
	bad.Animation.Markers[0].Tick = bad.Animation.MinimumTicks
	if err := validateVisualAnimation(bad); err == nil {
		t.Fatal("out-of-range marker accepted")
	}
}

func TestInterfaceAndEffectRequirements(t *testing.T) {
	ctx := context.Background()
	assets, campaign, text := visualFixture(t)
	directions := map[string]bool{}
	for _, record := range assets.Records(ctx) {
		if !slices.Contains([]string{"hud", "menu", "ledger", "recovery", "marker"}, record.Kind) {
			continue
		}
		if directions[record.Direction] {
			t.Errorf("generic repeated direction for %s", record.ID)
		}
		directions[record.Direction] = true
		var found bool
		for _, binding := range record.Bindings {
			if strings.HasPrefix(binding, "TXT-") || strings.HasPrefix(binding, "TUT-") || strings.HasPrefix(binding, "CAP-") {
				_, found = text.Entry(binding)
				if !found {
					t.Errorf("%s missing text %s", record.ID, binding)
				}
			}
		}
		if !found {
			t.Errorf("%s lacks text binding", record.ID)
		}
	}
	for id, label := range map[string]string{"UI-MENU-DISPLAY": "TXT-SET-DISPLAY", "UI-LEDGER-CERTIFIED": "TXT-LED-CERTIFIED"} {
		if !slices.Contains(mustVisual(t, assets, id).Bindings, label) {
			t.Errorf("%s lacks %s", id, label)
		}
	}
	delete(text.entries, "TXT-SET-DISPLAY")
	if _, err := LoadVisualAssets(ctx, visualFiles(t), campaign, text); !errors.Is(err, ErrMissingReference) {
		t.Fatalf("missing interface text: %v", err)
	}
	for _, id := range []string{"FX-BODY", "FX-FEC", "FX-IMPACT", "FX-HAZARD"} {
		record := mustVisual(t, assets, id)
		effect := record.Effect
		if !validEffectRequirement(effect) || effect.HitSprayMaxMS != 400 || effect.RemovalSprayMaxMS != 600 || effect.DecorativeStainLimit != 48 || effect.RoomParticleLimit != 512 {
			t.Fatalf("%s: %+v", id, effect)
		}
		effect.HitSprayMaxMS = 401
		if validEffectRequirement(effect) {
			t.Fatal("excessive spray accepted")
		}
		if mustVisual(t, assets, id).Effect.HitSprayMaxMS != 400 {
			t.Fatal("effect limits alias catalogue")
		}
	}
}

func TestVisualRejectsInvalidRegisters(t *testing.T) {
	_, campaign, text := visualFixture(t)
	cases := []struct {
		file string
		old  string
		new  string
	}{
		{"characters", `"code":"GRI"`, `"code":"VER"`},
		{"characters", `"crowd_heads": 6`, `"crowd_heads": 5`},
		{"environments", `"#DDD6BE"`, `"#XYZ123"`},
		{"environments", `"WARN","ACTIVE","AFTER"`, `"WARN","ACTIVE","ACTIVE"`},
		{"animations", `"duration_ms":350`, `"duration_ms":-1`},
		{"animations", `"poses":6`, `"poses":0`},
		{"animations", `"active_start","active_end","recovery_end"`, `"active_start","active_start","recovery_end"`},
		{"interface", `"TXT-SIGN-HOS-WING"`, `"TXT-SIGN-UNKNOWN"`},
		{"interface", `"schema_version": 1`, `"schema_version": 1, "schema_version": 1`},
		{"interface", `"schema_version": 1`, `"schema_version": 1, "unexpected": true`},
	}
	for _, test := range cases {
		t.Run(test.file+test.old, func(t *testing.T) {
			files := visualFiles(t)
			path := "content/catalogue/" + test.file + ".json"
			data := string(files[path].Data)
			if !strings.Contains(data, test.old) {
				t.Fatal("fixture mutation not found")
			}
			files[path].Data = []byte(strings.Replace(data, test.old, test.new, 1))
			if _, err := LoadVisualAssets(context.Background(), files, campaign, text); err == nil || !ErrInvalid.Has(err) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
	if _, err := LoadVisualAssets(context.Background(), visualFiles(t), nil, text); err == nil {
		t.Fatal("missing campaign accepted")
	}
	if _, err := LoadVisualAssets(context.Background(), visualFiles(t), &Campaign{}, text); err == nil {
		t.Fatal("incomplete campaign accepted")
	}
	delete(text.entries, "TXT-ENDING-TABLEAU")
	if _, err := LoadVisualAssets(context.Background(), visualFiles(t), campaign, text); err == nil || !errors.Is(err, ErrMissingReference) {
		t.Fatalf("missing tableau text must fail: %v", err)
	}
}

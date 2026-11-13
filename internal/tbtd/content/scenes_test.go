// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

func sceneDefinitions(t *testing.T) Definitions {
	t.Helper()
	catalogue, err := loadFixture(t, fixture, Incremental)
	if err != nil {
		t.Fatal(err)
	}
	d := catalogue.Definitions(context.Background())
	d.Narratives = []Narrative{{ID: "NAR-01", Location: "LOC-HOS"}}
	d.Endings = []Ending{{ID: "END-DEATH", Anchor: "ANC-HOS-01", Narrative: "NAR-01"}}
	d.Scenes = []Scene{
		{ID: "SCN-AAA", Narrative: "NAR-01", Units: []SceneUnit{{ID: "UNIT-ONE", Text: "Текст", TextID: "TXT-HOS", Background: "TXT-HOS"}}},
		{ID: "SCN-DEATH", Ending: "END-DEATH", Units: []SceneUnit{{ID: "UNIT-DEATH", Text: "Финал", TextID: "TXT-HOS"}}},
	}
	return d
}

func TestScenesValidateReferencesAndIsolation(t *testing.T) {
	ctx := context.Background()
	base := sceneDefinitions(t)
	catalogue, err := Validate(ctx, base, Incremental)
	if err != nil {
		t.Fatal(err)
	}
	base.Scenes[0].Units[0].Text = "changed"
	result := catalogue.Definitions(ctx)
	if result.Scenes[0].Units[0].Text != "Текст" {
		t.Fatal("input alias escaped")
	}
	result.Scenes[0].Units[0].Text = "changed again"
	if catalogue.Definitions(ctx).Scenes[0].Units[0].Text != "Текст" {
		t.Fatal("output alias escaped")
	}
	cases := []struct {
		name   string
		change func(*Definitions)
	}{
		{"duplicate scene", func(d *Definitions) { d.Scenes[1].ID = d.Scenes[0].ID }},
		{"duplicate reference", func(d *Definitions) { d.Scenes[1].Ending = ""; d.Scenes[1].Narrative = "NAR-01" }},
		{"both references", func(d *Definitions) { d.Scenes[0].Ending = "END-DEATH" }},
		{"no reference", func(d *Definitions) { d.Scenes[0].Narrative = "" }},
		{"missing narrative", func(d *Definitions) { d.Scenes[0].Narrative = "NAR-02" }},
		{"missing ending", func(d *Definitions) { d.Scenes[1].Ending = "END-HELL" }},
		{"missing text", func(d *Definitions) { d.Scenes[0].Units[0].TextID = "TXT-MISSING" }},
		{"missing background", func(d *Definitions) { d.Scenes[0].Units[0].Background = "ART-MISSING" }},
		{"blank text", func(d *Definitions) { d.Scenes[0].Units[0].Text = "  " }},
		{"invalid UTF-8", func(d *Definitions) { d.Scenes[0].Units[0].Text = string([]byte{0xff}) }},
		{"invalid scene", func(d *Definitions) { d.Scenes[0].ID = "bad" }},
		{"invalid unit", func(d *Definitions) { d.Scenes[0].Units[0].ID = "bad" }},
		{"duplicate unit", func(d *Definitions) { d.Scenes[1].Units[0].ID = "UNIT-ONE" }},
		{"no units", func(d *Definitions) { d.Scenes[0].Units = nil }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			d := cloneDefinitions(ctx, catalogue.Definitions(ctx))
			test.change(&d)
			_, err := Validate(ctx, d, Incremental)
			if err == nil || !ErrInvalid.Has(err) {
				t.Fatalf("accepted invalid scene: %v", err)
			}
		})
	}
	missing := catalogue.Definitions(ctx)
	missing.Scenes[0].Narrative = "NAR-02"
	if _, err := Validate(ctx, missing, Incremental); !errors.Is(err, ErrMissingReference) {
		t.Fatalf("missing reference identity: %v", err)
	}
}

func TestScenesLoadStrictJSON(t *testing.T) {
	ctx := context.Background()
	encoded, err := json.Marshal(sceneDefinitions(t))
	if err != nil {
		t.Fatal(err)
	}
	load := func(data string) error {
		_, err := Load(ctx, fstest.MapFS{"scene.json": &fstest.MapFile{Data: []byte(data)}}, "scene.json", Incremental)
		return err
	}
	// JSON authored documents use arrays rather than null for empty collections.
	document := strings.ReplaceAll(string(encoded), ":null", ":[]")
	if err = load(document); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(document, `"text":"Текст"`, `"text":"Текст","unknown":1`, 1)
	if err = load(bad); err == nil || !ErrInvalid.Has(err) {
		t.Fatalf("unknown unit field: %v", err)
	}
}

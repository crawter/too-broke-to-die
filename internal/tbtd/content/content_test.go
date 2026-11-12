// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"testing/fstest"
)

const fixture = `{
  "schema_version": 1, "content_version": "test-1",
  "locations": [{"id":"LOC-HOS","name":"TXT-HOS"}],
  "encounters": [{"id":"ENC-HOS-01","location":"LOC-HOS",
    "bounds":{"x":0,"y":0,"width":300,"height":100},"solids":[],"lethal":[]}],
  "anchors": [{"id":"ANC-HOS-01","encounter":"ENC-HOS-01","x":20,"y":0}],
  "presentations": [{"id":"TXT-HOS"}]
}`

func loadFixture(t *testing.T, data string, mode Mode) (*Catalogue, error) {
	t.Helper()
	return Load(context.Background(), fstest.MapFS{
		"data.json": &fstest.MapFile{Data: []byte(data)},
	}, "data.json", mode)
}

func TestLoadStrictJSON(t *testing.T) {
	catalogue, err := loadFixture(t, fixture, Incremental)
	if err != nil {
		t.Fatal(err)
	}
	if catalogue.Mode() != Incremental || catalogue.Definitions(context.Background()).Locations[0].ID != "LOC-HOS" {
		t.Fatal("incorrect validated snapshot")
	}
	cases := []struct {
		name string

		data string
	}{
		{"invalid utf8", fixture + string([]byte{0xff})},
		{"truncated", fixture[:len(fixture)-1]},
		{"trailing", fixture + " {}"},
		{"duplicate root key", strings.Replace(fixture, `"schema_version": 1`, `"schema_version": 1, "schema_version": 1`, 1)},
		{"duplicate nested key", strings.Replace(fixture, `"x":0`, `"x":0,"x":1`, 1)},
		{"unknown field", strings.Replace(fixture, `"name":"TXT-HOS"`, `"name":"TXT-HOS","surprise":1`, 1)},
		{"wrong case", strings.Replace(fixture, `"schema_version"`, `"SCHEMA_VERSION"`, 1)},
		{"explicit null", strings.Replace(fixture, `"solids":[]`, `"solids":null`, 1)},
		{"wrong type", strings.Replace(fixture, `"schema_version": 1`, `"schema_version": "1"`, 1)},
		{"invalid coordinate", strings.Replace(fixture, `"x":20`, `"x":1e999`, 1)},
		{"unpaired high surrogate", strings.Replace(fixture, `test-1`, `\ud800`, 1)},
		{"unpaired low surrogate", strings.Replace(fixture, `test-1`, `\udc00`, 1)},
		{"incorrect pair", strings.Replace(fixture, `test-1`, `\ud800\u0061`, 1)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadFixture(t, test.data, Incremental)
			if err == nil || !ErrInvalid.Has(err) {
				t.Fatalf("expected content validation failure, got %v", err)
			}
		})
	}
	for _, version := range []string{`\ud83d\ude00`, `\\ud800`} {
		if _, err := loadFixture(t, strings.Replace(fixture, `test-1`, version, 1), Incremental); err != nil {
			t.Fatalf("valid escaped content version %q: %v", version, err)
		}
	}
}

func TestValidateReferencesAndGeometry(t *testing.T) {
	catalogue, err := loadFixture(t, fixture, Incremental)
	if err != nil {
		t.Fatal(err)
	}
	base := catalogue.Definitions(context.Background())
	cases := []struct {
		name string

		change func(*Definitions)
	}{
		{"duplicate", func(d *Definitions) { d.Locations = append(d.Locations, d.Locations[0]) }},
		{"bad id", func(d *Definitions) { d.Locations[0].ID = "LOC-рус" }},
		{"missing reference", func(d *Definitions) { d.Encounters[0].Location = "LOC-MISSING" }},
		{"missing presentation", func(d *Definitions) { d.Presentations = nil }},
		{"zero width", func(d *Definitions) { d.Encounters[0].Bounds.Width = 0 }},
		{"nan", func(d *Definitions) { d.Anchors[0].X = math.NaN() }},
		{"out of bounds", func(d *Definitions) { d.Anchors[0].X = 13 }},
		{"solid overlap", func(d *Definitions) { d.Encounters[0].Solids = []Rect{{X: 6, Y: 0, Width: 1, Height: 58}} }},
		{"lethal overlap", func(d *Definitions) { d.Encounters[0].Lethal = []Rect{{X: 20, Y: 1, Width: 1, Height: 1}} }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			d := cloneDefinitions(context.Background(), base)
			test.change(&d)
			_, err := Validate(context.Background(), d, Incremental)
			if err == nil || !ErrInvalid.Has(err) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
	base.Anchors[0].X = 14 // The player box may touch the boundary without overlapping it.
	if _, err := Validate(context.Background(), base, Incremental); err != nil {
		t.Fatalf("boundary position: %v", err)
	}
}

func TestIsolationAndDeterminism(t *testing.T) {
	catalogue, err := loadFixture(t, fixture, Incremental)
	if err != nil {
		t.Fatal(err)
	}
	input := catalogue.Definitions(context.Background())
	input.Encounters[0].Solids = []Rect{{X: 200, Y: 0, Width: 5, Height: 5}}
	validated, err := Validate(context.Background(), input, Incremental)
	if err != nil {
		t.Fatal(err)
	}
	input.Encounters[0].Solids[0].X = 20
	returned := validated.Definitions(context.Background())
	returned.Encounters[0].Solids[0].X = 30
	if validated.Definitions(context.Background()).Encounters[0].Solids[0].X != 200 {
		t.Fatal("mutable geometry escaped the catalogue")
	}
	input = catalogue.Definitions(context.Background())
	input.Locations = append(input.Locations, Location{ID: "LOC-MIN", Name: "TXT-MISSING-Z"}, Location{ID: "LOC-CHU", Name: "TXT-MISSING-A"})
	_, first := Validate(context.Background(), input, Incremental)
	input.Locations[1], input.Locations[2] = input.Locations[2], input.Locations[1]
	_, second := Validate(context.Background(), input, Incremental)
	if first == nil || second == nil || first.Error() != second.Error() || !strings.Contains(first.Error(), "LOC-CHU") {
		t.Fatalf("nondeterministic reference validation: %v / %v", first, second)
	}
}

func TestDeedValidation(t *testing.T) {
	catalogue, err := loadFixture(t, fixture, Incremental)
	if err != nil {
		t.Fatal(err)
	}
	d := catalogue.Definitions(context.Background())
	d.Deeds = []Deed{{ID: "D-HOS-SUP-01", Encounter: "ENC-HOS-01", Anchor: "ANC-HOS-01", Consequence: "CON-D-HOS-SUP-01", Title: "TXT-HOS", HoldMilliseconds: 300}}
	d.Consequences = []Consequence{{ID: "CON-D-HOS-SUP-01", Deed: "D-HOS-SUP-01"}}
	if _, err := Validate(context.Background(), d, Incremental); err != nil {
		t.Fatal(err)
	}
	bad := cloneDefinitions(context.Background(), d)
	bad.Encounters = append(bad.Encounters, Encounter{ID: "ENC-HOS-02", Location: "LOC-HOS", Bounds: Rect{Width: 300, Height: 100}})
	bad.Anchors = append(bad.Anchors, Anchor{ID: "ANC-HOS-02", Encounter: "ENC-HOS-02", X: 20})
	bad.Deeds[0].Anchor = "ANC-HOS-02"
	if _, err := Validate(context.Background(), bad, Incremental); err == nil || !ErrInvalid.Has(err) {
		t.Fatalf("accepted anchor from another encounter: %v", err)
	}
	bad = cloneDefinitions(context.Background(), d)
	bad.Deeds[0].HoldMilliseconds = 0
	if _, err := Validate(context.Background(), bad, Incremental); err == nil || !ErrInvalid.Has(err) {
		t.Fatalf("accepted invalid duration: %v", err)
	}
	bad = cloneDefinitions(context.Background(), d)
	bad.Deeds[0].Prerequisites = []DeedID{"D-HOS-SUP-01"}
	if _, err := Validate(context.Background(), bad, Incremental); err == nil || !ErrInvalid.Has(err) {
		t.Fatalf("accepted cycle: %v", err)
	}
	bad = cloneDefinitions(context.Background(), d)
	bad.Deeds[0].ID = "D-HOS-SUP-07"
	if _, err := Validate(context.Background(), bad, Incremental); err == nil || !ErrInvalid.Has(err) {
		t.Fatalf("accepted extra reward identity: %v", err)
	}
}

func inventoryFixture() Definitions {
	d := Definitions{SchemaVersion: 1, ContentVersion: "test", Presentations: []Presentation{{ID: "TXT-NAME"}, {ID: "ART-SHARED"}, {ID: "FONT-NOTO-REGULAR"}, {ID: "FONT-NOTO-SEMIBOLD"}, {ID: "FONT-NOTO-BOLD"}}, Fonts: FontSet{Regular: "FONT-NOTO-REGULAR", SemiBold: "FONT-NOTO-SEMIBOLD", Bold: "FONT-NOTO-BOLD"}}
	var previousClosure DeedID
	var previousDeeds []DeedID
	for _, institution := range []string{"HOS", "CHU", "MIN", "INS"} {
		location := LocationID("LOC-" + institution)
		d.Locations = append(d.Locations, Location{ID: location, Name: "TXT-NAME"})
		for i := 1; i <= 5; i++ {
			encounter := EncounterID(fmt.Sprintf("ENC-%s-%02d", institution, i))
			anchor := AnchorID(fmt.Sprintf("ANC-%s-%02d", institution, i))
			space := Encounter{ID: encounter, Location: location, Bounds: Rect{Width: 300, Height: 100}, Presentation: "ART-SHARED"}
			for _, role := range []ObjectRole{RoleCheckpoint, RoleRefill, RoleConsole, RoleBypass} {
				id := ObjectID(fmt.Sprintf("OBJ-%s-%02d-%s", institution, i, role))
				d.Objects = append(d.Objects, Object{ID: id, Encounter: encounter, Role: role, Presentation: "ART-SHARED"})
				switch role {
				case RoleCheckpoint:
					space.Checkpoint = id
				case RoleRefill:
					space.Refill = id
				case RoleConsole:
					space.RequestConsole = id
				case RoleBypass:
					space.Bypass = id
				}
			}
			d.Encounters = append(d.Encounters, space)
			d.Anchors = append(d.Anchors, Anchor{ID: anchor, Encounter: encounter, X: 20})
		}
		out := AnchorID("ANC-" + institution + "-OUT")
		d.Anchors = append(d.Anchors, Anchor{ID: out, Encounter: EncounterID("ENC-" + institution + "-05"), X: 20})
		var previousBatch []DeedID
		var localDeeds []DeedID
		for i, family := range []string{"SUP", "RES", "UTL", "PRV"} {
			encounter := EncounterID(fmt.Sprintf("ENC-%s-%02d", institution, i+1))
			anchor := AnchorID(fmt.Sprintf("ANC-%s-%02d", institution, i+1))
			d.Emergencies = append(d.Emergencies, Emergency{ID: EmergencyID("EM-" + institution + "-" + family), Encounter: encounter, Anchor: anchor, WarningMilliseconds: 1500, ActiveMilliseconds: 8000, Presentation: "ART-SHARED"})
			var sharedRoles, ticketRoles []ObjectRole
			switch family {
			case "SUP":
				sharedRoles = []ObjectRole{RolePad, RoleTerminal, RoleCrusher}
				ticketRoles = []ObjectRole{RoleTrolley}
			case "RES":
				sharedRoles = []ObjectRole{RolePlate, RoleCrate, RoleGate, RoleLatch, RoleRefuge, RoleTerminal}
				ticketRoles = []ObjectRole{RoleActor}
			case "UTL":
				sharedRoles = []ObjectRole{RoleSwitch, RoleFuse, RoleTerminal}
				ticketRoles = []ObjectRole{RolePaddle}
			case "PRV":
				sharedRoles = []ObjectRole{RoleTerminal}
				ticketRoles = []ObjectRole{RoleRecorder, RoleCamera}
			}
			var shared []ObjectID
			for _, role := range sharedRoles {
				objectID := ObjectID(fmt.Sprintf("OBJ-%s-%s-%s", institution, family, role))
				d.Objects = append(d.Objects, Object{ID: objectID, Encounter: encounter, Role: role, Presentation: "ART-SHARED"})
				shared = append(shared, objectID)
			}
			var prerequisites []DeedID
			if i > 0 {
				prerequisites = previousBatch
			} else if previousClosure != "" {
				prerequisites = []DeedID{previousClosure}
			}
			var batch []DeedID
			for n := 1; n <= 6; n++ {
				id := DeedID(fmt.Sprintf("D-%s-%s-%02d", institution, family, n))
				consequence := ConsequenceID("CON-" + string(id))
				deed := Deed{ID: id, Encounter: encounter, Anchor: anchor, Consequence: consequence, Title: "TXT-NAME", HoldMilliseconds: 300, Prerequisites: append([]DeedID(nil), prerequisites...), Objects: append([]ObjectID(nil), shared...)}
				if institution == "HOS" && i == 0 {
					deed.Narratives = []NarrativeID{"NAR-03"}
				}
				for _, role := range ticketRoles {
					objectID := ObjectID(fmt.Sprintf("OBJ-%s-%s", id, role))
					d.Objects = append(d.Objects, Object{ID: objectID, Encounter: encounter, Deed: id, Role: role, Presentation: "ART-SHARED"})
					deed.Objects = append(deed.Objects, objectID)
				}
				d.Deeds = append(d.Deeds, deed)
				d.Consequences = append(d.Consequences, Consequence{ID: consequence, Deed: id, Presentation: "ART-SHARED"})
				batch = append(batch, id)
				localDeeds = append(localDeeds, id)
			}
			previousBatch = batch
		}
		id := DeedID("D-" + institution + "-END")
		consequence := ConsequenceID("CON-" + string(id))
		encounter := EncounterID("ENC-" + institution + "-05")
		prerequisites := append([]DeedID(nil), localDeeds...)
		if institution == "INS" {
			prerequisites = append(prerequisites, previousDeeds...)
		} else if previousClosure != "" {
			prerequisites = append(prerequisites, previousClosure)
		}
		hold := int64(300)
		if institution == "INS" {
			hold = 1500
		}
		var terminalRole string
		switch institution {
		case "HOS":
			terminalRole = "AUDIT"
		case "CHU":
			terminalRole = "ARCHIVE"
		case "MIN":
			terminalRole = "REVIEW"
		case "INS":
			terminalRole = "PANEL"
		}
		terminal := ObjectID(fmt.Sprintf("OBJ-%s-%s", id, terminalRole))
		d.Objects = append(d.Objects, Object{ID: terminal, Encounter: encounter, Deed: id, Role: RoleTerminal, Presentation: "ART-SHARED"})
		d.Deeds = append(d.Deeds, Deed{ID: id, Encounter: encounter, Anchor: out, Consequence: consequence, Title: "TXT-NAME", HoldMilliseconds: hold, Prerequisites: prerequisites, Defeated: []EncounterID{encounter}, Objects: []ObjectID{terminal}})
		d.Consequences = append(d.Consequences, Consequence{ID: consequence, Deed: id, Presentation: "ART-SHARED"})
		previousDeeds = append(previousDeeds, localDeeds...)
		previousDeeds = append(previousDeeds, id)
		previousClosure = id
	}
	for i := 1; i <= 3; i++ {
		encounter := EncounterID(fmt.Sprintf("ENC-PRO-%02d", i))
		d.Encounters = append(d.Encounters, Encounter{ID: encounter, Location: "LOC-HOS", Bounds: Rect{Width: 300, Height: 100}, Presentation: "ART-SHARED"})
		d.Anchors = append(d.Anchors, Anchor{ID: AnchorID(fmt.Sprintf("ANC-PRO-%02d", i)), Encounter: encounter, X: 20})
	}
	for i := 1; i <= 12; i++ {
		story := Narrative{ID: NarrativeID(fmt.Sprintf("NAR-%02d", i)), Location: "LOC-HOS", Presentation: "ART-SHARED"}
		if i > 1 {
			story.Prerequisites = []NarrativeID{NarrativeID(fmt.Sprintf("NAR-%02d", i-1))}
		}
		d.Narratives = append(d.Narratives, story)
	}
	for _, id := range []AnchorID{"ANC-MIN-PERMIT", "ANC-END-DEATH", "ANC-END-HELL", "ANC-END-CEREMONY", "ANC-END-TABLEAU"} {
		d.Anchors = append(d.Anchors, Anchor{ID: id, Encounter: "ENC-MIN-05", X: 20})
	}
	d.Endings = []Ending{
		{ID: "END-REVIEW", Anchor: "ANC-MIN-PERMIT", Narrative: "NAR-09", Presentation: "ART-SHARED"},
		{ID: "END-CONSENT", Anchor: "ANC-MIN-PERMIT", Narrative: "NAR-10", Presentation: "ART-SHARED"},
		{ID: "END-DEATH", Anchor: "ANC-END-DEATH", Narrative: "NAR-10", Presentation: "ART-SHARED"},
		{ID: "END-HELL", Anchor: "ANC-END-HELL", Narrative: "NAR-11", Presentation: "ART-SHARED"},
		{ID: "END-CEREMONY", Anchor: "ANC-END-CEREMONY", Narrative: "NAR-12", Presentation: "ART-SHARED"},
		{ID: "END-TABLEAU", Anchor: "ANC-END-TABLEAU", Narrative: "NAR-12", Presentation: "ART-SHARED"},
	}
	final := Gate{ID: "G-INS-FINAL", Defeated: []EncounterID{"ENC-INS-05"}}
	permit := Gate{ID: "G-PERMIT", Narratives: []NarrativeID{"NAR-09"}}
	for _, deed := range d.Deeds {
		permit.Deeds = append(permit.Deeds, deed.ID)
		if deed.ID != "D-INS-END" {
			final.Deeds = append(final.Deeds, deed.ID)
		}
	}
	d.Gates = []Gate{
		{ID: "G-HOS-IN", Narratives: []NarrativeID{"NAR-03"}},
		{ID: "G-CHU-IN", Deeds: []DeedID{"D-HOS-END"}},
		{ID: "G-MIN-IN", Deeds: []DeedID{"D-CHU-END"}},
		{ID: "G-INS-IN", Deeds: []DeedID{"D-MIN-END"}},
		final, permit,
		{ID: "G-DEATH", RequirePermit: true, RequireConsent: true},
	}
	return d
}

func TestFullInventory(t *testing.T) {
	d := inventoryFixture()
	catalogue, err := Validate(context.Background(), d, Full)
	if err != nil {
		t.Fatalf("complete inventory: %v", err)
	}
	if catalogue.Mode() != Full || len(catalogue.Definitions(context.Background()).Deeds) != 100 {
		t.Fatal("wrong validation scope or deed count")
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	// Nil optional collections are represented by empty arrays in authored JSON.
	encoded := strings.ReplaceAll(string(data), ":null", ":[]")
	if _, err := loadFixture(t, encoded, Full); err != nil {
		t.Fatalf("complete JSON inventory: %v", err)
	}
	reordered := cloneDefinitions(context.Background(), d)
	for left, right := 0, len(reordered.Deeds[6].Prerequisites)-1; left < right; left, right = left+1, right-1 {
		reordered.Deeds[6].Prerequisites[left], reordered.Deeds[6].Prerequisites[right] = reordered.Deeds[6].Prerequisites[right], reordered.Deeds[6].Prerequisites[left]
	}
	if _, err := Validate(context.Background(), reordered, Full); err != nil {
		t.Fatalf("batch order must not change prerequisites: %v", err)
	}
	view := catalogue.Definitions(context.Background())
	for i := range view.Deeds {
		if view.Deeds[i].ID == "D-HOS-RES-01" {
			view.Deeds[i].Prerequisites[0] = "D-INS-END"
		}
		if view.Deeds[i].ID == "D-HOS-END" {
			view.Deeds[i].Defeated[0] = "ENC-INS-05"
		}
	}
	for i := range view.Gates {
		if view.Gates[i].ID == "G-HOS-IN" {
			view.Gates[i].Narratives[0] = "NAR-12"
		}
	}
	for _, deed := range catalogue.Definitions(context.Background()).Deeds {
		if (deed.ID == "D-HOS-RES-01" && deed.Prerequisites[0] == "D-INS-END") || (deed.ID == "D-HOS-END" && deed.Defeated[0] == "ENC-INS-05") {
			t.Fatal("deed collection escaped the snapshot")
		}
	}
	for _, gate := range catalogue.Definitions(context.Background()).Gates {
		if gate.ID == "G-HOS-IN" && gate.Narratives[0] == "NAR-12" {
			t.Fatal("gate collection escaped the snapshot")
		}
	}
	cases := []struct {
		name string

		change func(*Definitions)
	}{
		{"missing emergency", func(d *Definitions) { d.Emergencies = d.Emergencies[1:] }},
		{"missing checkpoint", func(d *Definitions) { d.Encounters[0].Checkpoint = "" }},
		{"wrong checkpoint role", func(d *Definitions) { d.Encounters[0].Checkpoint = d.Encounters[0].Refill }},
		{"missing ticket object", func(d *Definitions) { d.Deeds[0].Objects = d.Deeds[0].Objects[:len(d.Deeds[0].Objects)-1] }},
		{"unresolved object", func(d *Definitions) { d.Deeds[0].Objects[0] = "OBJ-MISSING" }},
		{"missing batch receipt", func(d *Definitions) { d.Deeds[6].Prerequisites = d.Deeds[6].Prerequisites[:5] }},
		{"ticket order dependency", func(d *Definitions) { d.Deeds[0].Prerequisites = []DeedID{"D-HOS-SUP-02"} }},
		{"future narrative dependency", func(d *Definitions) { d.Deeds[0].Narratives = []NarrativeID{"NAR-03", "NAR-09"} }},
		{"broken narrative sequence", func(d *Definitions) { d.Narratives[8].Prerequisites = nil }},
		{"missing boss", func(d *Definitions) { d.Deeds[24].Defeated = nil }},
		{"wrong closure anchor", func(d *Definitions) { d.Deeds[24].Anchor = "ANC-HOS-05" }},
		{"missing final receipt", func(d *Definitions) { d.Gates[4].Deeds = d.Gates[4].Deeds[:98] }},
		{"invalid final hold", func(d *Definitions) { d.Deeds[99].HoldMilliseconds = 1499 }},
		{"final deed missing receipt", func(d *Definitions) { d.Deeds[99].Prerequisites = d.Deeds[99].Prerequisites[:98] }},
		{"unknown object role", func(d *Definitions) { d.Objects[0].Role = "UNKNOWN" }},
		{"permit extra dependency", func(d *Definitions) { d.Gates[5].RequireConsent = true }},
		{"consent not required", func(d *Definitions) { d.Gates[6].RequireConsent = false }},
		{"missing ending stage", func(d *Definitions) { d.Endings = d.Endings[:5] }},
		{"wrong ending anchor", func(d *Definitions) { d.Endings[3].Anchor = "ANC-END-DEATH" }},
		{"unsafe anchor", func(d *Definitions) { d.Anchors[0].X = 0 }},
		{"extra deed", func(d *Definitions) { d.Deeds = append(d.Deeds, Deed{ID: "D-HOS-SUP-07"}) }},
		{"missing font", func(d *Definitions) { d.Fonts.Bold = "" }},
		{"unknown font", func(d *Definitions) { d.Fonts.Bold = "FONT-MISSING" }},
		{"duplicate font weights", func(d *Definitions) { d.Fonts.Bold = d.Fonts.Regular }},
		{"wrong font kind", func(d *Definitions) { d.Fonts.Bold = "TXT-NAME" }},
		{"unknown presentation", func(d *Definitions) { d.Deeds[0].Title = "TXT-MISSING" }},
		{"missing encounter presentation", func(d *Definitions) { d.Encounters[0].Presentation = "" }},
		{"missing consequence presentation", func(d *Definitions) { d.Consequences[0].Presentation = "" }},
		{"missing narrative presentation", func(d *Definitions) { d.Narratives[0].Presentation = "" }},
		{"missing ending presentation", func(d *Definitions) { d.Endings[0].Presentation = "" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			broken := cloneDefinitions(context.Background(), d)
			test.change(&broken)
			if _, err := Validate(context.Background(), broken, Full); err == nil || !ErrInvalid.Has(err) {
				t.Fatalf("invalid full inventory accepted: %v", err)
			}
		})
	}
}

func TestClosureCertificationTerminals(t *testing.T) {
	for _, institution := range []string{"HOS", "CHU", "MIN", "INS"} {
		t.Run(institution, func(t *testing.T) {
			base := inventoryFixture()
			id := DeedID("D-" + institution + "-END")
			deedIndex := -1
			for i := range base.Deeds {
				if base.Deeds[i].ID == id {
					deedIndex = i
					break
				}
			}
			if deedIndex < 0 || len(base.Deeds[deedIndex].Objects) != 1 {
				t.Fatalf("fixture lacks terminal for %s", id)
			}
			terminal := base.Deeds[deedIndex].Objects[0]
			objectIndex := -1
			for i := range base.Objects {
				if base.Objects[i].ID == terminal {
					objectIndex = i
					break
				}
			}
			if objectIndex < 0 || base.Objects[objectIndex].Role != RoleTerminal {
				t.Fatalf("fixture terminal %s not found", terminal)
			}
			if _, err := Validate(context.Background(), base, Full); err != nil {
				t.Fatalf("correct terminal for %s: %v", id, err)
			}
			shared := cloneDefinitions(context.Background(), base)
			shared.Objects[objectIndex].Deed = ""
			shared.Deeds[deedIndex].Objects = append(shared.Deeds[deedIndex].Objects, ObjectID(fmt.Sprintf("OBJ-%s-05-CONSOLE", institution)))
			if _, err := Validate(context.Background(), shared, Full); err != nil {
				t.Fatalf("shared terminal and additional object for %s: %v", id, err)
			}

			cases := []struct {
				name string

				change func(*Definitions)

				missingReference bool
			}{
				{"missing binding", func(d *Definitions) {
					d.Deeds[deedIndex].Objects = nil
				}, true},
				{"wrong role", func(d *Definitions) {
					d.Objects[objectIndex].Role = RoleConsole
				}, true},
				{"nonterminal only", func(d *Definitions) {
					d.Deeds[deedIndex].Objects = []ObjectID{ObjectID(fmt.Sprintf("OBJ-%s-05-CONSOLE", institution))}
				}, true},
				{"unknown object", func(d *Definitions) {
					d.Deeds[deedIndex].Objects = []ObjectID{"OBJ-MISSING"}
				}, true},
				{"wrong encounter", func(d *Definitions) {
					d.Deeds[deedIndex].Objects = []ObjectID{ObjectID(fmt.Sprintf("OBJ-%s-PRV-TERMINAL", institution))}
				}, false},
				{"wrong deed", func(d *Definitions) {
					d.Objects[objectIndex].Deed = DeedID(fmt.Sprintf("D-%s-SUP-01", institution))
				}, false},
			}
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					broken := cloneDefinitions(context.Background(), base)
					test.change(&broken)
					_, err := Validate(context.Background(), broken, Full)
					if err == nil || !ErrInvalid.Has(err) || !strings.Contains(err.Error(), string(id)) {
						t.Fatalf("invalid terminal for %s: %v", id, err)
					}
					if test.missingReference && !errors.Is(err, ErrMissingReference) {
						t.Fatalf("missing reference identity for %s: %v", id, err)
					}
				})
			}
			partial := cloneDefinitions(context.Background(), base)
			partial.Deeds[deedIndex].Objects = nil
			if _, err := Validate(context.Background(), partial, Incremental); err != nil {
				t.Fatalf("incremental content should allow absent future terminal: %v", err)
			}
		})
	}
}

func TestClosureAnchorLocation(t *testing.T) {
	d := inventoryFixture()
	// The post-confrontation stop may be in a different encounter in the
	// same institution; it need not be inside the boss arena itself.
	for i := range d.Anchors {
		if d.Anchors[i].ID == "ANC-HOS-OUT" {
			d.Anchors[i].Encounter = "ENC-HOS-01"
		}
	}
	if _, err := Validate(context.Background(), d, Full); err != nil {
		t.Fatalf("valid local closure anchor: %v", err)
	}
	for i := range d.Anchors {
		if d.Anchors[i].ID == "ANC-HOS-OUT" {
			d.Anchors[i].Encounter = "ENC-CHU-05"
		}
	}
	if _, err := Validate(context.Background(), d, Incremental); err == nil || !ErrInvalid.Has(err) {
		t.Fatalf("accepted closure anchor in another institution: %v", err)
	}
}

func TestErrorCauses(t *testing.T) {
	_, err := loadFixture(t, `{"schema_version": @}`, Incremental)
	var syntax *json.SyntaxError
	if err == nil || !ErrInvalid.Has(err) || !errors.As(err, &syntax) {
		t.Fatalf("lost JSON syntax cause: %v", err)
	}
	wrongType := strings.Replace(fixture, `"schema_version": 1`, `"schema_version": "1"`, 1)
	_, err = loadFixture(t, wrongType, Incremental)
	var typeError *json.UnmarshalTypeError
	if err == nil || !ErrInvalid.Has(err) || !errors.As(err, &typeError) {
		t.Fatalf("lost JSON type cause: %v", err)
	}
	_, err = loadFixture(t, strings.Replace(fixture, `"name":"TXT-HOS"`, `"name":"TXT-MISSING"`, 1), Incremental)
	if err == nil || !ErrInvalid.Has(err) || !errors.Is(err, ErrMissingReference) {
		t.Fatalf("lost loaded reference cause: %v", err)
	}
	_, err = Validate(context.Background(), Definitions{SchemaVersion: 1, ContentVersion: "test", Locations: []Location{{ID: "LOC-HOS", Name: "TXT-MISSING"}}}, Incremental)
	if err == nil || !ErrInvalid.Has(err) || !errors.Is(err, ErrMissingReference) {
		t.Fatalf("lost validation reference cause: %v", err)
	}
}

func TestMissingAndCancellation(t *testing.T) {
	_, err := loadFixture(t, fixture, Full)
	if err == nil || !ErrInvalid.Has(err) {
		t.Fatalf("partial content accepted as full: %v", err)
	}
	_, err = Load(context.Background(), fstest.MapFS{}, "missing.json", Incremental)
	if err == nil || !Error.Has(err) {
		t.Fatalf("missing file: %v", err)
	}
	_, err = Load(context.Background(), fstest.MapFS{}, "../data.json", Incremental)
	if err == nil || !ErrInvalid.Has(err) {
		t.Fatalf("invalid filesystem path: %v", err)
	}
	_, err = loadFixture(t, strings.Repeat(" ", maxDocumentBytes+1), Incremental)
	if err == nil || !ErrInvalid.Has(err) {
		t.Fatalf("oversized document: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Validate(ctx, Definitions{}, Incremental)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	_, err = loadFixture(t, fixture, Mode(99))
	if err == nil || !ErrInvalid.Has(err) {
		t.Fatalf("unknown validation mode: %v", err)
	}
}

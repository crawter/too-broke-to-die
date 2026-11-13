// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Validate copies and validates the supplied definitions. Incremental mode
// permits absent future records, but never permits a dangling reference.
// Full checks the canonical inventory and authored progression references.
// It does not qualify external media or prove physical route connectivity.
func Validate(ctx context.Context, input Definitions, mode Mode) (_ *Catalogue, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if mode != Incremental && mode != Full {
		return nil, ErrInvalid.New("unknown validation mode %d", mode)
	}
	definitions := cloneDefinitions(ctx, input)
	if err = validateDefinitions(ctx, definitions, mode); err != nil {
		return nil, ErrInvalid.Wrap(err)
	}
	return &Catalogue{definitions: definitions, mode: mode}, nil
}

func index[T any, ID ~string](items []T, id func(T) ID, valid func(string) bool) (map[ID]T, error) {
	result := make(map[ID]T, len(items))
	for _, item := range items {
		key := id(item)
		if !valid(string(key)) {
			return nil, fmt.Errorf("invalid identifier %q", key)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate identifier %s", key)
		}
		result[key] = item
	}
	return result, nil
}

func prefix(prefix string) func(string) bool {
	return func(id string) bool { return validID(id, prefix) }
}

func require[T any, ID ~string](owner, field string, id ID, records map[ID]T) error {
	if _, ok := records[id]; !ok {
		return fmt.Errorf("%s: %s references missing %q: %w", owner, field, id, ErrMissingReference)
	}
	return nil
}

func optional[T any, ID ~string](owner, field string, id ID, records map[ID]T) error {
	if id == "" {
		return nil
	}
	return require(owner, field, id, records)
}

func presentationRef(owner string, id PresentationID, records map[PresentationID]Presentation, mode Mode) error {
	if mode == Full {
		return require(owner, "presentation", id, records)
	}
	return optional(owner, "presentation", id, records)
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func validRect(rect Rect) bool {
	return finite(rect.X) && finite(rect.Y) && finite(rect.Width) && finite(rect.Height) &&
		rect.Width > 0 && rect.Height > 0 && finite(rect.X+rect.Width) && finite(rect.Y+rect.Height)
}

func overlaps(a, b Rect) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
}

func inside(inner, outer Rect) bool {
	return inner.X >= outer.X && inner.Y >= outer.Y && inner.X+inner.Width <= outer.X+outer.Width && inner.Y+inner.Height <= outer.Y+outer.Height
}

func duration(value int64) bool { return value > 0 && value <= 24*60*60*1000 }

func validRole(role ObjectRole) bool {
	switch role {
	case RoleCheckpoint, RoleRefill, RoleConsole, RoleBypass, RoleSwitch, RoleTerminal,
		RoleTrolley, RolePad, RoleCrusher, RoleActor, RolePlate, RoleCrate,
		RoleGate, RoleLatch, RoleRefuge, RolePaddle, RoleFuse, RoleRecorder, RoleCamera:
		return true
	default:
		return false
	}
}

func validateDefinitions(ctx context.Context, d Definitions, mode Mode) (err error) {
	defer mon.Task()(&ctx)(&err)

	if d.SchemaVersion != 1 || strings.TrimSpace(d.ContentVersion) == "" {
		return fmt.Errorf("schema_version must be 1 and content_version must be nonempty")
	}
	// Sort copied records so diagnostics and reference traversal are independent
	// of input order and map iteration. Caller-owned slices remain untouched.
	sort.Slice(d.Locations, func(i, j int) bool { return d.Locations[i].ID < d.Locations[j].ID })
	sort.Slice(d.Encounters, func(i, j int) bool { return d.Encounters[i].ID < d.Encounters[j].ID })
	sort.Slice(d.Objects, func(i, j int) bool { return d.Objects[i].ID < d.Objects[j].ID })
	sort.Slice(d.Anchors, func(i, j int) bool { return d.Anchors[i].ID < d.Anchors[j].ID })
	sort.Slice(d.Narratives, func(i, j int) bool { return d.Narratives[i].ID < d.Narratives[j].ID })
	sort.Slice(d.Deeds, func(i, j int) bool { return d.Deeds[i].ID < d.Deeds[j].ID })
	sort.Slice(d.Consequences, func(i, j int) bool { return d.Consequences[i].ID < d.Consequences[j].ID })
	sort.Slice(d.Emergencies, func(i, j int) bool { return d.Emergencies[i].ID < d.Emergencies[j].ID })
	sort.Slice(d.Endings, func(i, j int) bool { return d.Endings[i].ID < d.Endings[j].ID })
	sort.Slice(d.Scenes, func(i, j int) bool { return d.Scenes[i].ID < d.Scenes[j].ID })
	sort.Slice(d.Gates, func(i, j int) bool { return d.Gates[i].ID < d.Gates[j].ID })
	sort.Slice(d.Presentations, func(i, j int) bool { return d.Presentations[i].ID < d.Presentations[j].ID })

	locations, err := index(d.Locations, func(v Location) LocationID { return v.ID }, locationPattern.MatchString)
	if err != nil {
		return err
	}
	encounters, err := index(d.Encounters, func(v Encounter) EncounterID { return v.ID }, encounterPattern.MatchString)
	if err != nil {
		return err
	}
	objects, err := index(d.Objects, func(v Object) ObjectID { return v.ID }, prefix("OBJ"))
	if err != nil {
		return err
	}
	anchors, err := index(d.Anchors, func(v Anchor) AnchorID { return v.ID }, anchorPattern.MatchString)
	if err != nil {
		return err
	}
	narratives, err := index(d.Narratives, func(v Narrative) NarrativeID { return v.ID }, narrativePattern.MatchString)
	if err != nil {
		return err
	}
	deeds, err := index(d.Deeds, func(v Deed) DeedID { return v.ID }, deedPattern.MatchString)
	if err != nil {
		return err
	}
	consequences, err := index(d.Consequences, func(v Consequence) ConsequenceID { return v.ID }, prefix("CON"))
	if err != nil {
		return err
	}
	emergencies, err := index(d.Emergencies, func(v Emergency) EmergencyID { return v.ID }, emergencyPattern.MatchString)
	if err != nil {
		return err
	}
	endings, err := index(d.Endings, func(v Ending) EndingID { return v.ID }, endingPattern.MatchString)
	if err != nil {
		return err
	}
	gates, err := index(d.Gates, func(v Gate) GateID { return v.ID }, gatePattern.MatchString)
	if err != nil {
		return err
	}
	presentations, err := index(d.Presentations, func(v Presentation) PresentationID { return v.ID }, validPresentationID)
	if err != nil {
		return err
	}
	if err = validateScenes(ctx, d.Scenes, narratives, endings, presentations); err != nil {
		return err
	}
	fontIDs := []PresentationID{d.Fonts.Regular, d.Fonts.SemiBold, d.Fonts.Bold}
	for _, id := range fontIDs {
		if id == "" && mode != Full {
			continue
		}
		if err := require("fonts", "font", id, presentations); err != nil {
			return err
		}
		if !validID(string(id), "FONT") {
			return fmt.Errorf("fonts: %s is not a font identity", id)
		}
	}
	if mode == Full && (fontIDs[0] == fontIDs[1] || fontIDs[0] == fontIDs[2] || fontIDs[1] == fontIDs[2]) {
		return fmt.Errorf("fonts: regular, semibold, and bold must be distinct")
	}

	for _, v := range d.Locations {
		if err := require(string(v.ID), "name", v.Name, presentations); err != nil {
			return err
		}
	}
	for _, v := range d.Encounters {
		if err := require(string(v.ID), "location", v.Location, locations); err != nil {
			return err
		}
		if !validRect(v.Bounds) {
			return fmt.Errorf("%s: invalid bounds", v.ID)
		}
		if err := presentationRef(string(v.ID), v.Presentation, presentations, mode); err != nil {
			return err
		}
		for _, ref := range []struct {
			id ObjectID

			role ObjectRole
		}{
			{v.Checkpoint, RoleCheckpoint}, {v.Refill, RoleRefill},
			{v.RequestConsole, RoleConsole}, {v.Bypass, RoleBypass},
		} {
			if ref.id == "" {
				continue
			}
			if err := require(string(v.ID), string(ref.role), ref.id, objects); err != nil {
				return err
			}
			object := objects[ref.id]
			if object.Encounter != v.ID || object.Role != ref.role || object.Deed != "" {
				return fmt.Errorf("%s: invalid %s object %s", v.ID, ref.role, ref.id)
			}
		}
		for _, group := range [][]Rect{v.Solids, v.Lethal} {
			for _, rect := range group {
				if !validRect(rect) || !inside(rect, v.Bounds) {
					return fmt.Errorf("%s: invalid or out-of-bounds geometry", v.ID)
				}
			}
		}
	}
	for _, v := range d.Objects {
		if err := require(string(v.ID), "encounter", v.Encounter, encounters); err != nil {
			return err
		}
		if err := optional(string(v.ID), "deed", v.Deed, deeds); err != nil {
			return err
		}
		if err := presentationRef(string(v.ID), v.Presentation, presentations, mode); err != nil {
			return err
		}
		if v.Role != "" && !validRole(v.Role) {
			return fmt.Errorf("%s: unknown object role %q", v.ID, v.Role)
		}
		if v.Deed != "" && (!strings.HasPrefix(string(v.ID), "OBJ-"+string(v.Deed)+"-") || deeds[v.Deed].Encounter != v.Encounter) {
			return fmt.Errorf("%s: deed or encounter identity mismatch", v.ID)
		}
	}
	for _, v := range d.Anchors {
		if err := require(string(v.ID), "encounter", v.Encounter, encounters); err != nil {
			return err
		}
		if !finite(v.X) || !finite(v.Y) {
			return fmt.Errorf("%s: non-finite position", v.ID)
		}
		body := Rect{X: v.X - 14, Y: v.Y, Width: 28, Height: 58}
		encounter := encounters[v.Encounter]
		if !inside(body, encounter.Bounds) {
			return fmt.Errorf("%s: spawn outside encounter", v.ID)
		}
		for _, group := range [][]Rect{encounter.Solids, encounter.Lethal} {
			for _, rect := range group {
				if overlaps(body, rect) {
					return fmt.Errorf("%s: unsafe spawn", v.ID)
				}
			}
		}
	}
	for _, v := range d.Narratives {
		if err := require(string(v.ID), "location", v.Location, locations); err != nil {
			return err
		}
		if err := presentationRef(string(v.ID), v.Presentation, presentations, mode); err != nil {
			return err
		}
		if err := refs(string(v.ID), "prerequisite", v.Prerequisites, narratives); err != nil {
			return err
		}
	}
	for _, v := range d.Deeds {
		owner := string(v.ID)
		if err := require(owner, "encounter", v.Encounter, encounters); err != nil {
			return err
		}
		if err := require(owner, "anchor", v.Anchor, anchors); err != nil {
			return err
		}
		anchorEncounter := anchors[v.Anchor].Encounter
		if strings.HasSuffix(owner, "-END") {
			institution := strings.Split(owner, "-")[1]
			if v.Anchor != AnchorID("ANC-"+institution+"-OUT") || v.Encounter != EncounterID("ENC-"+institution+"-05") || encounters[v.Encounter].Location != LocationID("LOC-"+institution) || encounters[anchorEncounter].Location != encounters[v.Encounter].Location {
				return fmt.Errorf("%s: closure anchor %s or encounter %s belongs to another institution", v.ID, v.Anchor, v.Encounter)
			}
		} else if anchorEncounter != v.Encounter {
			return fmt.Errorf("%s: anchor %s belongs to another encounter %s", v.ID, v.Anchor, anchorEncounter)
		}
		if err := require(owner, "consequence", v.Consequence, consequences); err != nil {
			return err
		}
		if err := require(owner, "title", v.Title, presentations); err != nil {
			return err
		}
		if err := refs(owner, "prerequisite", v.Prerequisites, deeds); err != nil {
			return err
		}
		if err := refs(owner, "object", v.Objects, objects); err != nil {
			return err
		}
		if err := refs(owner, "narrative", v.Narratives, narratives); err != nil {
			return err
		}
		if err := refs(owner, "defeated encounter", v.Defeated, encounters); err != nil {
			return err
		}
		if !duration(v.HoldMilliseconds) {
			return fmt.Errorf("%s: invalid hold_ms", v.ID)
		}
		if v.Consequence != ConsequenceID("CON-"+owner) || consequences[v.Consequence].Deed != v.ID {
			return fmt.Errorf("%s: consequence identity mismatch", v.ID)
		}
		for _, id := range v.Objects {
			object := objects[id]
			if object.Encounter != v.Encounter || (object.Deed != "" && object.Deed != v.ID) {
				return fmt.Errorf("%s: object %s belongs to another deed or encounter", v.ID, id)
			}
		}
	}
	for _, v := range d.Consequences {
		if err := require(string(v.ID), "deed", v.Deed, deeds); err != nil {
			return err
		}
		if v.ID != ConsequenceID("CON-"+string(v.Deed)) {
			return fmt.Errorf("%s: deed identity mismatch", v.ID)
		}
		if err := presentationRef(string(v.ID), v.Presentation, presentations, mode); err != nil {
			return err
		}
	}
	for _, v := range d.Emergencies {
		if err := require(string(v.ID), "encounter", v.Encounter, encounters); err != nil {
			return err
		}
		if err := require(string(v.ID), "anchor", v.Anchor, anchors); err != nil {
			return err
		}
		if err := presentationRef(string(v.ID), v.Presentation, presentations, mode); err != nil {
			return err
		}
		if v.WarningMilliseconds != 1500 || v.ActiveMilliseconds != 8000 {
			return fmt.Errorf("%s: invalid emergency duration", v.ID)
		}
		if anchors[v.Anchor].Encounter != v.Encounter {
			return fmt.Errorf("%s: anchor belongs to another encounter", v.ID)
		}
	}
	for _, v := range d.Endings {
		if err := require(string(v.ID), "anchor", v.Anchor, anchors); err != nil {
			return err
		}
		if err := require(string(v.ID), "narrative", v.Narrative, narratives); err != nil {
			return err
		}
		if err := presentationRef(string(v.ID), v.Presentation, presentations, mode); err != nil {
			return err
		}
	}
	for _, v := range d.Gates {
		if err := refs(string(v.ID), "deed", v.Deeds, deeds); err != nil {
			return err
		}
		if err := refs(string(v.ID), "narrative", v.Narratives, narratives); err != nil {
			return err
		}
		if err := refs(string(v.ID), "defeated encounter", v.Defeated, encounters); err != nil {
			return err
		}
	}
	if err := checkDeedCycles(d.Deeds, deeds); err != nil {
		return err
	}
	if err := checkNarrativeCycles(d.Narratives, narratives); err != nil {
		return err
	}
	if mode == Full {
		return validateInventory(ctx, locations, encounters, objects, anchors, narratives, deeds, emergencies, endings, gates)
	}
	return nil
}

func refs[T any, ID ~string](owner, field string, ids []ID, records map[ID]T) error {
	ordered := append([]ID(nil), ids...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	for i, id := range ordered {
		if i > 0 && id == ordered[i-1] {
			return fmt.Errorf("%s: duplicate %s %s", owner, field, id)
		}
		if err := require(owner, field, id, records); err != nil {
			return err
		}
	}
	return nil
}

func checkCycles[ID ~string](ids []ID, dependencies func(ID) []ID) error {
	states := make(map[ID]uint8, len(ids))
	var visit func(ID) error
	visit = func(id ID) error {
		if states[id] == 1 {
			return fmt.Errorf("%s: cyclic prerequisite", id)
		}
		if states[id] == 2 {
			return nil
		}
		states[id] = 1
		next := append([]ID(nil), dependencies(id)...)
		sort.Slice(next, func(i, j int) bool { return next[i] < next[j] })
		for _, dep := range next {
			if err := visit(dep); err != nil {
				return err
			}
		}
		states[id] = 2
		return nil
	}
	for _, id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func checkDeedCycles(items []Deed, records map[DeedID]Deed) error {
	ids := make([]DeedID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return checkCycles(ids, func(id DeedID) []DeedID { return records[id].Prerequisites })
}

func checkNarrativeCycles(items []Narrative, records map[NarrativeID]Narrative) error {
	ids := make([]NarrativeID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return checkCycles(ids, func(id NarrativeID) []NarrativeID { return records[id].Prerequisites })
}

func validateInventory(ctx context.Context, locations map[LocationID]Location, encounters map[EncounterID]Encounter, objects map[ObjectID]Object, anchors map[AnchorID]Anchor, narratives map[NarrativeID]Narrative, deeds map[DeedID]Deed, emergencies map[EmergencyID]Emergency, endings map[EndingID]Ending, gates map[GateID]Gate) (err error) {
	defer mon.Task()(&ctx)(&err)

	if err := validateInstitutions(ctx, locations, encounters, objects, anchors, deeds, emergencies); err != nil {
		return err
	}
	if len(deeds) != 100 {
		return fmt.Errorf("campaign: expected exactly 100 deeds, got %d", len(deeds))
	}
	return validateFinalInventory(ctx, encounters, anchors, narratives, endings, gates)
}

func validateInstitutions(ctx context.Context, locations map[LocationID]Location, encounters map[EncounterID]Encounter, objects map[ObjectID]Object, anchors map[AnchorID]Anchor, deeds map[DeedID]Deed, emergencies map[EmergencyID]Emergency) (err error) {
	defer mon.Task()(&ctx)(&err)

	institutions := []string{"HOS", "CHU", "MIN", "INS"}
	families := []string{"SUP", "RES", "UTL", "PRV"}
	var previousClosure DeedID
	var previousDeeds []DeedID
	for _, institution := range institutions {
		var localDeeds []DeedID
		var previousBatch []DeedID
		if err := require("campaign", "location", LocationID("LOC-"+institution), locations); err != nil {
			return err
		}
		for i := 1; i <= 5; i++ {
			enc := EncounterID(fmt.Sprintf("ENC-%s-%02d", institution, i))
			anchor := AnchorID(fmt.Sprintf("ANC-%s-%02d", institution, i))
			if err := require("campaign", "encounter", enc, encounters); err != nil {
				return err
			}
			if encounters[enc].Location != LocationID("LOC-"+institution) {
				return fmt.Errorf("%s: wrong location", enc)
			}
			space := encounters[enc]
			for _, ref := range []struct {
				id ObjectID

				role ObjectRole
			}{
				{space.Checkpoint, RoleCheckpoint}, {space.Refill, RoleRefill},
			} {
				if err := require(string(enc), string(ref.role), ref.id, objects); err != nil {
					return err
				}
			}
			if i <= 4 {
				if err := require(string(enc), "console", space.RequestConsole, objects); err != nil {
					return err
				}
				if err := require(string(enc), "bypass", space.Bypass, objects); err != nil {
					return err
				}
			}
			if err := require("campaign", "anchor", anchor, anchors); err != nil {
				return err
			}
			if anchors[anchor].Encounter != enc {
				return fmt.Errorf("%s: wrong encounter", anchor)
			}
		}
		out := AnchorID("ANC-" + institution + "-OUT")
		if err := require("campaign", "anchor", out, anchors); err != nil {
			return err
		}
		for familyIndex, family := range families {
			var prerequisites []DeedID
			if familyIndex > 0 {
				prerequisites = previousBatch
			} else if previousClosure != "" {
				prerequisites = []DeedID{previousClosure}
			}
			var batch []DeedID
			enc := EncounterID(fmt.Sprintf("ENC-%s-%02d", institution, familyIndex+1))
			anchor := AnchorID(fmt.Sprintf("ANC-%s-%02d", institution, familyIndex+1))
			em := EmergencyID("EM-" + institution + "-" + family)
			if err := require("campaign", "emergency", em, emergencies); err != nil {
				return err
			}
			if emergencies[em].Encounter != enc || emergencies[em].Anchor != anchor {
				return fmt.Errorf("%s: wrong encounter or anchor", em)
			}
			for n := 1; n <= 6; n++ {
				id := DeedID(fmt.Sprintf("D-%s-%s-%02d", institution, family, n))
				if err := require("campaign", "deed", id, deeds); err != nil {
					return err
				}
				deed := deeds[id]
				if deed.Encounter != enc || deed.Anchor != anchor {
					return fmt.Errorf("%s: wrong encounter or anchor", id)
				}
				if !sameMembers(deed.Prerequisites, prerequisites) || len(deed.Defeated) != 0 {
					return fmt.Errorf("%s: invalid batch prerequisites", id)
				}
				var requiredNarratives []NarrativeID
				if institution == "HOS" && familyIndex == 0 {
					requiredNarratives = []NarrativeID{"NAR-03"}
				}
				if !sameMembers(deed.Narratives, requiredNarratives) {
					return fmt.Errorf("%s: invalid narrative prerequisites", id)
				}
				if deed.HoldMilliseconds != 300 {
					return fmt.Errorf("%s: service hold must be 300 ms", id)
				}
				if err := validateServiceObjects(deed, family, objects); err != nil {
					return err
				}
				batch = append(batch, id)
				localDeeds = append(localDeeds, id)
			}
			previousBatch = batch
		}
		id := DeedID("D-" + institution + "-END")
		if err := require("campaign", "deed", id, deeds); err != nil {
			return err
		}
		closure := deeds[id]
		if closure.Encounter != EncounterID("ENC-"+institution+"-05") || closure.Anchor != out {
			return fmt.Errorf("%s: wrong closure encounter or anchor", id)
		}
		if err := validateServiceObjects(closure, "END", objects); err != nil {
			return err
		}
		prerequisites := append([]DeedID(nil), localDeeds...)
		if institution == "INS" {
			prerequisites = append(prerequisites, previousDeeds...)
		} else if previousClosure != "" {
			prerequisites = append(prerequisites, previousClosure)
		}
		if !sameMembers(closure.Prerequisites, prerequisites) || !sameMembers(closure.Defeated, []EncounterID{closure.Encounter}) || len(closure.Narratives) != 0 {
			return fmt.Errorf("%s: invalid closure prerequisites or boss", id)
		}
		if institution == "INS" && closure.HoldMilliseconds != 1500 {
			return fmt.Errorf("%s: final hold must be 1500 ms", id)
		}
		previousDeeds = append(previousDeeds, localDeeds...)
		previousDeeds = append(previousDeeds, id)
		previousClosure = id
	}
	return nil
}

// validateServiceObjects is measured by the enclosing inventory validation.
func validateServiceObjects(deed Deed, family string, objects map[ObjectID]Object) error {
	var required []ObjectRole
	var ticketSpecific []ObjectRole
	switch family {
	case "SUP":
		required = []ObjectRole{RoleTrolley, RolePad, RoleTerminal, RoleCrusher}
		ticketSpecific = []ObjectRole{RoleTrolley}
	case "RES":
		required = []ObjectRole{RoleActor, RolePlate, RoleCrate, RoleGate, RoleLatch, RoleRefuge, RoleTerminal}
		ticketSpecific = []ObjectRole{RoleActor}
	case "UTL":
		required = []ObjectRole{RolePaddle, RoleSwitch, RoleFuse, RoleTerminal}
		ticketSpecific = []ObjectRole{RolePaddle}
	case "PRV":
		required = []ObjectRole{RoleRecorder, RoleCamera, RoleTerminal}
		ticketSpecific = []ObjectRole{RoleRecorder, RoleCamera}
	case "END":
		required = []ObjectRole{RoleTerminal}
	default:
		return fmt.Errorf("%s: unknown service family %s", deed.ID, family)
	}
	for _, role := range required {
		found := false
		for _, id := range deed.Objects {
			object := objects[id]
			if object.Role == role && (!contains(ticketSpecific, role) || object.Deed == deed.ID) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: required %s object: %w", deed.ID, role, ErrMissingReference)
		}
	}
	return nil
}

func sameMembers[ID comparable](left, right []ID) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[ID]int, len(left))
	for _, id := range left {
		counts[id]++
	}
	for _, id := range right {
		if counts[id] == 0 {
			return false
		}
		counts[id]--
	}
	return true
}

func validateFinalInventory(ctx context.Context, encounters map[EncounterID]Encounter, anchors map[AnchorID]Anchor, narratives map[NarrativeID]Narrative, endings map[EndingID]Ending, gates map[GateID]Gate) (err error) {
	defer mon.Task()(&ctx)(&err)

	for i := 1; i <= 3; i++ {
		enc := EncounterID(fmt.Sprintf("ENC-PRO-%02d", i))
		anchor := AnchorID(fmt.Sprintf("ANC-PRO-%02d", i))
		if err := require("campaign", "encounter", enc, encounters); err != nil {
			return err
		}
		if err := require("campaign", "anchor", anchor, anchors); err != nil {
			return err
		}
		if anchors[anchor].Encounter != enc {
			return fmt.Errorf("%s: wrong encounter", anchor)
		}
	}
	for i := 1; i <= 12; i++ {
		id := NarrativeID(fmt.Sprintf("NAR-%02d", i))
		if err := require("campaign", "narrative", id, narratives); err != nil {
			return err
		}
		if i > 1 && !contains(narratives[id].Prerequisites, NarrativeID(fmt.Sprintf("NAR-%02d", i-1))) {
			return fmt.Errorf("%s: missing preceding milestone", id)
		}
	}
	for _, stage := range []struct {
		id EndingID

		anchor AnchorID

		narrative NarrativeID
	}{
		{"END-REVIEW", "ANC-MIN-PERMIT", "NAR-09"},
		{"END-CONSENT", "", "NAR-10"},
		{"END-DEATH", "ANC-END-DEATH", "NAR-10"},
		{"END-HELL", "ANC-END-HELL", "NAR-11"},
		{"END-CEREMONY", "ANC-END-CEREMONY", "NAR-12"},
		{"END-TABLEAU", "ANC-END-TABLEAU", "NAR-12"},
	} {
		if err := require("campaign", "ending", stage.id, endings); err != nil {
			return err
		}
		ending := endings[stage.id]
		if (stage.anchor != "" && ending.Anchor != stage.anchor) || ending.Narrative != stage.narrative {
			return fmt.Errorf("%s: wrong ending anchor or narrative", stage.id)
		}
		if stage.id == "END-CONSENT" && encounters[anchors[ending.Anchor].Encounter].Location != "LOC-MIN" {
			return fmt.Errorf("%s: consent must be in the ministry", stage.id)
		}
	}
	if encounters[anchors["ANC-MIN-PERMIT"].Encounter].Location != "LOC-MIN" {
		return fmt.Errorf("ANC-MIN-PERMIT: wrong location")
	}
	for _, id := range []GateID{"G-HOS-IN", "G-CHU-IN", "G-MIN-IN", "G-INS-IN", "G-INS-FINAL", "G-PERMIT", "G-DEATH"} {
		if err := require("campaign", "gate", id, gates); err != nil {
			return err
		}
	}
	for _, entry := range []struct {
		id GateID

		deeds []DeedID

		narratives []NarrativeID
	}{
		{"G-HOS-IN", nil, []NarrativeID{"NAR-03"}},
		{"G-CHU-IN", []DeedID{"D-HOS-END"}, nil},
		{"G-MIN-IN", []DeedID{"D-CHU-END"}, nil},
		{"G-INS-IN", []DeedID{"D-MIN-END"}, nil},
	} {
		gate := gates[entry.id]
		if !sameMembers(gate.Deeds, entry.deeds) || !sameMembers(gate.Narratives, entry.narratives) || len(gate.Defeated) != 0 || gate.RequirePermit || gate.RequireConsent {
			return fmt.Errorf("%s: invalid entrance prerequisites", gate.ID)
		}
	}
	final := gates["G-INS-FINAL"]
	if len(final.Deeds) != 99 || contains(final.Deeds, DeedID("D-INS-END")) || !sameMembers(final.Defeated, []EncounterID{"ENC-INS-05"}) || len(final.Narratives) != 0 || final.RequirePermit || final.RequireConsent {
		return fmt.Errorf("G-INS-FINAL: requires the first 99 deeds and defeated ENC-INS-05")
	}
	permit := gates["G-PERMIT"]
	if len(permit.Deeds) != 100 || !sameMembers(permit.Narratives, []NarrativeID{"NAR-09"}) || len(permit.Defeated) != 0 || permit.RequirePermit || permit.RequireConsent {
		return fmt.Errorf("G-PERMIT: requires all deeds and NAR-09")
	}
	death := gates["G-DEATH"]
	if !death.RequirePermit || !death.RequireConsent || len(death.Deeds) != 0 || len(death.Narratives) != 0 || len(death.Defeated) != 0 {
		return fmt.Errorf("G-DEATH: requires permit and final-death consent")
	}
	return nil
}

func contains[ID comparable](values []ID, target ID) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

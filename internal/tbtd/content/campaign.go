// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"unicode/utf8"
)

// ServiceFamily describes the evidence contract shared by six authored tickets.
type ServiceFamily struct {
	ID string `json:"id"`

	Title string `json:"title"`

	Evidence string `json:"evidence"`

	Trigger string `json:"trigger"`

	Failure string `json:"failure"`

	Reset string `json:"reset"`

	TicketRoles []ObjectRole `json:"ticket_roles"`

	SharedRoles []ObjectRole `json:"shared_roles"`
}

type campaignSource struct {
	SchemaVersion int `json:"schema_version"`

	ContentVersion string `json:"content_version"`

	Families []ServiceFamily `json:"families"`

	Institutions []campaignInstitution `json:"institutions"`
}

type campaignInstitution struct {
	ID string `json:"id"`

	Closure DeedID `json:"closure"`

	Action string `json:"action"`

	Outcome string `json:"outcome"`

	Milestone NarrativeID `json:"milestone"`

	Batches []campaignBatchSource `json:"batches"`
}

type campaignBatchSource struct {
	Family string `json:"family"`

	Emergency EmergencyID `json:"emergency"`

	Aftermath string `json:"aftermath"`

	Hazard string `json:"hazard"`

	Recovery string `json:"recovery"`
}

type serviceTextSource struct {
	SchemaVersion int `json:"schema_version"`

	Institutions []serviceTextInstitution `json:"institutions"`
}

type serviceTextInstitution struct {
	ID string `json:"id"`

	Name string `json:"name"`

	ClosureTitle string `json:"closure_title"`

	Tickets []serviceText `json:"tickets"`
}

type serviceText struct {
	ID DeedID `json:"id"`

	Label string `json:"label"`

	Detail string `json:"detail"`
}

// CampaignDeed is a single credit source and its authored service contract.
// Deed contains references for later integration with the full Definitions inventory.
type CampaignDeed struct {
	Deed Deed

	Location LocationID

	InstitutionName string

	Family string

	FamilyID string

	Label string

	Detail string

	Title string

	Variant int

	Evidence string

	Trigger string

	Failure string

	Reset string

	IndividualAftermath string

	Emergency EmergencyID

	Outcome string

	CompletedNarrative NarrativeID

	InstalledPrerequisites []DeedID
}

// CampaignBatch binds all six tickets to one shared emergency, independent of ticket order.
type CampaignBatch struct {
	Institution LocationID

	Family string

	FamilyID string

	Deeds []DeedID

	Emergency Emergency

	IndividualAftermath string

	Hazard string

	Recovery string
}

// Campaign is an immutable, validated snapshot of the bounded credit catalogue.
type Campaign struct {
	deeds []CampaignDeed

	batches []CampaignBatch

	objects []Object

	consequences []Consequence

	gates []Gate
}

// LoadCampaign strictly reads and expands the two authored documents without opening devices.
func LoadCampaign(ctx context.Context, filesystem fs.FS, campaignPath, textPath string) (_ *Campaign, err error) {
	defer mon.Task()(&ctx)(&err)

	var source campaignSource
	if err = readCampaignDocument(ctx, filesystem, campaignPath, &source); err != nil {
		return nil, err
	}
	var text serviceTextSource
	if err = readCampaignDocument(ctx, filesystem, textPath, &text); err != nil {
		return nil, err
	}
	return expandCampaign(ctx, source, text)
}

func readCampaignDocument(ctx context.Context, filesystem fs.FS, name string, target any) (err error) {
	defer mon.Task()(&ctx)(&err)

	if filesystem == nil || !fs.ValidPath(name) {
		return ErrInvalid.New("invalid content filesystem or path %q", name)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	file, err := filesystem.Open(name)
	if err != nil {
		return Error.Wrap(fmt.Errorf("open %q: %w", name, err))
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return Error.Wrap(fmt.Errorf("read or close %q: %w", name, errors.Join(readErr, closeErr)))
	}
	if len(data) > maxDocumentBytes || !utf8.Valid(data) {
		return ErrInvalid.New("%s: invalid UTF-8 or oversized document", name)
	}
	if err = checkSurrogates(data); err != nil {
		return ErrInvalid.Wrap(fmt.Errorf("%s: %w", name, err))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err = checkValue(decoder, reflect.TypeOf(target).Elem(), "$", 0); err != nil {
		return ErrInvalid.Wrap(fmt.Errorf("%s: %w", name, err))
	}
	if _, err = decoder.Token(); err != io.EOF {
		return ErrInvalid.New("%s: trailing or malformed JSON", name)
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return ErrInvalid.Wrap(fmt.Errorf("%s: %w", name, err))
	}
	return nil
}

func expandCampaign(ctx context.Context, source campaignSource, text serviceTextSource) (_ *Campaign, err error) {
	defer mon.Task()(&ctx)(&err)

	institutions := []string{"HOS", "CHU", "MIN", "INS"}
	families := []string{"SUP", "RES", "UTL", "PRV"}
	ticketRoles := [][]ObjectRole{{RoleTrolley}, {RoleActor}, {RolePaddle}, {RoleRecorder, RoleCamera}}
	sharedRoles := [][]ObjectRole{{RolePad, RoleTerminal, RoleCrusher}, {RolePlate, RoleCrate, RoleGate, RoleLatch, RoleRefuge, RoleTerminal}, {RoleSwitch, RoleFuse, RoleTerminal}, {RoleTerminal}}
	if source.SchemaVersion != 1 || text.SchemaVersion != 1 || strings.TrimSpace(source.ContentVersion) == "" || len(source.Institutions) != 4 || len(text.Institutions) != 4 || len(source.Families) != 4 {
		return nil, ErrInvalid.New("campaign: invalid schema or inventory size")
	}
	for i, family := range source.Families {
		if family.ID != families[i] || !sameMembers(family.TicketRoles, ticketRoles[i]) || !sameMembers(family.SharedRoles, sharedRoles[i]) || !allText(family.Title, family.Evidence, family.Trigger, family.Failure, family.Reset) {
			return nil, ErrInvalid.New("campaign: invalid family %q", family.ID)
		}
	}
	campaign := &Campaign{}
	var previousClosure DeedID
	var allPrior []DeedID
	for instIndex, institution := range source.Institutions {
		code := institutions[instIndex]
		labels := text.Institutions[instIndex]
		if institution.ID != code || labels.ID != code || institution.Closure != DeedID("D-"+code+"-END") || len(institution.Batches) != 4 || len(labels.Tickets) != 24 || !allText(labels.Name, labels.ClosureTitle, institution.Action, institution.Outcome) {
			return nil, ErrInvalid.New("campaign: invalid institution %s", code)
		}
		expectedMilestone := NarrativeID("")
		if code == "HOS" {
			expectedMilestone = "NAR-05"
		} else if code == "INS" {
			expectedMilestone = "NAR-09"
		}
		if institution.Milestone != expectedMilestone {
			return nil, ErrInvalid.New("campaign: invalid closure milestone for %s", code)
		}
		var previousBatch []DeedID
		var local []DeedID
		for familyIndex, familyID := range families {
			family := source.Families[familyIndex]
			batchSource := institution.Batches[familyIndex]
			encounter := EncounterID(fmt.Sprintf("ENC-%s-%02d", code, familyIndex+1))
			anchor := AnchorID(fmt.Sprintf("ANC-%s-%02d", code, familyIndex+1))
			emergency := EmergencyID("EM-" + code + "-" + familyID)
			if batchSource.Family != familyID || batchSource.Emergency != emergency || !allText(batchSource.Aftermath, batchSource.Hazard, batchSource.Recovery) {
				return nil, ErrInvalid.New("campaign: invalid batch %s-%s", code, familyID)
			}
			batch := CampaignBatch{Institution: LocationID("LOC-" + code), Family: familyID, FamilyID: "FAM-" + familyID, Emergency: Emergency{ID: emergency, Encounter: encounter, Anchor: anchor, WarningMilliseconds: 1500, ActiveMilliseconds: 8000}, IndividualAftermath: batchSource.Aftermath, Hazard: batchSource.Hazard, Recovery: batchSource.Recovery}
			prerequisites := append([]DeedID(nil), previousBatch...)
			if familyIndex == 0 && previousClosure != "" {
				prerequisites = []DeedID{previousClosure}
			}
			for n := 1; n <= 6; n++ {
				id := DeedID(fmt.Sprintf("D-%s-%s-%02d", code, familyID, n))
				label := labels.Tickets[familyIndex*6+n-1]
				if label.ID != id || !allText(label.Label) || ((familyID == "SUP" || familyID == "RES") && !allText(label.Detail)) {
					return nil, ErrInvalid.New("campaign: invalid ticket %s", id)
				}
				deed := Deed{ID: id, Encounter: encounter, Anchor: anchor, Consequence: ConsequenceID("CON-" + string(id)), Title: PresentationID("TXT-" + string(id)), HoldMilliseconds: 300, Prerequisites: append([]DeedID(nil), prerequisites...)}
				if code == "HOS" && familyIndex == 0 {
					deed.Narratives = []NarrativeID{"NAR-03"}
				}
				for _, role := range family.SharedRoles {
					objectID := ObjectID(fmt.Sprintf("OBJ-%s-%s-%s", code, familyID, role))
					deed.Objects = append(deed.Objects, objectID)
					if n == 1 {
						campaign.objects = append(campaign.objects, Object{ID: objectID, Encounter: encounter, Role: role})
					}
				}
				for _, role := range family.TicketRoles {
					objectID := ObjectID(fmt.Sprintf("OBJ-%s-%s", id, role))
					deed.Objects = append(deed.Objects, objectID)
					campaign.objects = append(campaign.objects, Object{ID: objectID, Encounter: encounter, Deed: id, Role: role})
				}
				campaign.consequences = append(campaign.consequences, Consequence{ID: deed.Consequence, Deed: id})
				campaign.deeds = append(campaign.deeds, CampaignDeed{Deed: deed, Location: batch.Institution, InstitutionName: labels.Name, Family: familyID, FamilyID: "FAM-" + familyID, Label: label.Label, Detail: label.Detail, Title: family.Title + " — " + label.Label, Variant: n, Evidence: family.Evidence, Trigger: family.Trigger, Failure: family.Failure, Reset: family.Reset, IndividualAftermath: batchSource.Aftermath, Emergency: emergency})
				batch.Deeds = append(batch.Deeds, id)
				local = append(local, id)
			}
			campaign.batches = append(campaign.batches, batch)
			previousBatch = batch.Deeds
		}
		prerequisites := append([]DeedID(nil), local...)
		if code == "INS" {
			prerequisites = append(prerequisites, allPrior...)
		} else if previousClosure != "" {
			prerequisites = append(prerequisites, previousClosure)
		}
		encounter := EncounterID("ENC-" + code + "-05")
		hold := int64(300)
		if code == "INS" {
			hold = 1500
		}
		deed := Deed{ID: institution.Closure, Encounter: encounter, Anchor: AnchorID("ANC-" + code + "-OUT"), Consequence: ConsequenceID("CON-" + string(institution.Closure)), Title: PresentationID("TXT-" + string(institution.Closure)), HoldMilliseconds: hold, Prerequisites: prerequisites, Defeated: []EncounterID{encounter}, Objects: []ObjectID{ObjectID("OBJ-" + code + "-END-TERMINAL")}}
		campaign.objects = append(campaign.objects, Object{ID: deed.Objects[0], Encounter: encounter, Role: RoleTerminal})
		campaign.consequences = append(campaign.consequences, Consequence{ID: deed.Consequence, Deed: deed.ID})
		closure := CampaignDeed{Deed: deed, Location: LocationID("LOC-" + code), InstitutionName: labels.Name, Family: "END", FamilyID: "FAM-END", Label: labels.ClosureTitle, Title: "Закрыть предписание — " + labels.ClosureTitle, Evidence: institution.Action, Trigger: institution.Action, Failure: "Missing prerequisites block interaction without failing the attempt.", Reset: "The protected terminal remains available.", Outcome: institution.Outcome, CompletedNarrative: institution.Milestone}
		if code == "INS" {
			closure.InstalledPrerequisites = []DeedID{"D-INS-SUP-06", "D-INS-UTL-06"}
		}
		campaign.deeds = append(campaign.deeds, closure)
		allPrior = append(allPrior, local...)
		allPrior = append(allPrior, institution.Closure)
		previousClosure = institution.Closure
	}
	campaign.gates = []Gate{
		{ID: "G-HOS-IN", Narratives: []NarrativeID{"NAR-03"}},
		{ID: "G-CHU-IN", Deeds: []DeedID{"D-HOS-END"}},
		{ID: "G-MIN-IN", Deeds: []DeedID{"D-CHU-END"}},
		{ID: "G-INS-IN", Deeds: []DeedID{"D-MIN-END"}},
		{ID: "G-INS-FINAL", Deeds: append([]DeedID(nil), allPrior[:99]...), Defeated: []EncounterID{"ENC-INS-05"}},
		{ID: "G-PERMIT", Deeds: append([]DeedID(nil), allPrior...), Narratives: []NarrativeID{"NAR-09"}},
		{ID: "G-DEATH", RequirePermit: true, RequireConsent: true},
	}
	return campaign, nil
}

func allText(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

// Deeds returns independent copies in the canonical route order.
func (campaign *Campaign) Deeds(ctx context.Context) []CampaignDeed {
	defer mon.Task()(&ctx)(nil)

	result := append([]CampaignDeed(nil), campaign.deeds...)
	for i := range result {
		deed := &result[i]
		deed.Deed.Prerequisites = append([]DeedID(nil), deed.Deed.Prerequisites...)
		deed.Deed.Objects = append([]ObjectID(nil), deed.Deed.Objects...)
		deed.Deed.Narratives = append([]NarrativeID(nil), deed.Deed.Narratives...)
		deed.Deed.Defeated = append([]EncounterID(nil), deed.Deed.Defeated...)
		deed.InstalledPrerequisites = append([]DeedID(nil), deed.InstalledPrerequisites...)
	}
	return result
}

// Batches returns independent copies in route order.
func (campaign *Campaign) Batches(ctx context.Context) []CampaignBatch {
	defer mon.Task()(&ctx)(nil)

	result := append([]CampaignBatch(nil), campaign.batches...)
	for i := range result {
		result[i].Deeds = append([]DeedID(nil), result[i].Deeds...)
	}
	return result
}

// Objects returns the symbolic object bindings for integration with authored geometry.
// Geometry and presentation resources are supplied by later content inventories.
func (campaign *Campaign) Objects(ctx context.Context) []Object {
	defer mon.Task()(&ctx)(nil)

	return append([]Object(nil), campaign.objects...)
}

// Consequences returns the stable one-to-one certification consequence bindings.
func (campaign *Campaign) Consequences(ctx context.Context) []Consequence {
	defer mon.Task()(&ctx)(nil)

	return append([]Consequence(nil), campaign.consequences...)
}

// Gates returns independent copies of the entrance, final, permit and death gates.
func (campaign *Campaign) Gates(ctx context.Context) []Gate {
	defer mon.Task()(&ctx)(nil)

	result := append([]Gate(nil), campaign.gates...)
	for i := range result {
		result[i].Deeds = append([]DeedID(nil), result[i].Deeds...)
		result[i].Narratives = append([]NarrativeID(nil), result[i].Narratives...)
		result[i].Defeated = append([]EncounterID(nil), result[i].Defeated...)
	}
	return result
}

// CampaignProgress is a read-only input to prerequisite queries, not a credit balance.
type CampaignProgress struct {
	Certified []DeedID

	Narratives []NarrativeID

	Defeated []EncounterID

	Permit bool

	Consent bool
}

// CanCertify checks catalogue prerequisites; evidence and durable commits belong to later systems.
func (campaign *Campaign) CanCertify(ctx context.Context, id DeedID, progress CampaignProgress) (_ bool, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = campaign.validateProgress(ctx, progress); err != nil {
		return false, err
	}
	for _, entry := range campaign.deeds {
		if entry.Deed.ID == id {
			return !contains(progress.Certified, id) && prerequisitesMet(ctx, entry.Deed.Prerequisites, entry.Deed.Narratives, entry.Deed.Defeated, progress), nil
		}
	}
	return false, ErrInvalid.New("unknown deed %s", id)
}

// GateOpen checks a named gate against distinct receipts and milestones.
func (campaign *Campaign) GateOpen(ctx context.Context, id GateID, progress CampaignProgress) (_ bool, err error) {
	defer mon.Task()(&ctx)(&err)

	if err = campaign.validateProgress(ctx, progress); err != nil {
		return false, err
	}
	for _, gate := range campaign.gates {
		if gate.ID == id {
			return prerequisitesMet(ctx, gate.Deeds, gate.Narratives, gate.Defeated, progress) && (!gate.RequirePermit || progress.Permit) && (!gate.RequireConsent || progress.Consent), nil
		}
	}
	return false, ErrInvalid.New("unknown gate %s", id)
}

// SixthEmergency returns the batch emergency only for an uncertified sixth ticket.
// The caller must commit it atomically with that ticket's certification.
func (campaign *Campaign) SixthEmergency(ctx context.Context, id DeedID, progress CampaignProgress) (_ EmergencyID, err error) {
	defer mon.Task()(&ctx)(&err)

	eligible, err := campaign.CanCertify(ctx, id, progress)
	if err != nil || !eligible {
		return "", err
	}
	for _, batch := range campaign.batches {
		if contains(batch.Deeds, id) {
			count := 0
			for _, member := range batch.Deeds {
				if contains(progress.Certified, member) {
					count++
				}
			}
			if count == 5 {
				return batch.Emergency.ID, nil
			}
			return "", nil
		}
	}
	return "", nil
}

func (campaign *Campaign) validateProgress(ctx context.Context, progress CampaignProgress) (err error) {
	defer mon.Task()(&ctx)(&err)

	seen := make(map[DeedID]bool, len(progress.Certified))
	for _, id := range progress.Certified {
		found := false
		for _, deed := range campaign.deeds {
			if deed.Deed.ID == id {
				found = true
				break
			}
		}
		if !found || seen[id] {
			return ErrInvalid.New("unknown or duplicate certified deed %s", id)
		}
		seen[id] = true
	}
	for _, id := range progress.Narratives {
		if !narrativePattern.MatchString(string(id)) {
			return ErrInvalid.New("invalid narrative %s", id)
		}
	}
	for _, id := range progress.Defeated {
		if !encounterPattern.MatchString(string(id)) {
			return ErrInvalid.New("invalid encounter %s", id)
		}
	}
	return nil
}

func prerequisitesMet(ctx context.Context, deeds []DeedID, narratives []NarrativeID, defeated []EncounterID, progress CampaignProgress) bool {
	defer mon.Task()(&ctx)(nil)

	for _, id := range deeds {
		if !contains(progress.Certified, id) {
			return false
		}
	}
	for _, id := range narratives {
		if !contains(progress.Narratives, id) {
			return false
		}
	}
	for _, id := range defeated {
		if !contains(progress.Defeated, id) {
			return false
		}
	}
	return true
}

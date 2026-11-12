// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"too-broke-to-die/internal/tbtd/content"
)

// RestoredEmergency describes reconstruction behind a safe boundary. The
// warning is restarted in full and begins only after gameplay resumes.
type RestoredEmergency struct {
	EmergencyView

	BehindSafeBoundary bool

	BypassUsable bool
}

// RestorationPlan is a deterministic description for the world constructor.
// It contains no live positions, partial evidence or other excursion state.
type RestorationPlan struct {
	Location content.LocationID

	Anchor content.AnchorID

	State string

	SceneEntry string

	Ending EndingStage

	Routes []content.GateID

	ItemRoutes []content.ObjectID

	OwnedItems []CriticalItemID

	Installed []content.DeedID

	Receipts []Receipt

	Defeated []content.EncounterID

	MandatoryActors []content.ObjectID

	ProtectedTerminals []content.ObjectID

	ActiveTicket content.DeedID

	InitialObjects []content.ObjectID

	InitialObjective string

	Emergencies []RestoredEmergency

	Health int

	Limbs int

	Feces int

	ClearTransient bool

	ClearPositions bool

	ClearProjectiles bool

	ClearSmears bool

	ClearCarriedLimbs bool

	ClearEnemyDamage bool

	ClearPartialEvidence bool

	RestoreObjectOrigins bool

	ResetEnemies bool

	ResetPartialBosses bool
}

// reachableEncounterAnchor uses the authored encounter prerequisites rather
// than requiring a receipt from the encounter or a completed boss victory.
func (campaign *Campaign) reachableEncounterAnchor(ctx context.Context, data State, anchor content.AnchorID) bool {
	defer mon.Task()(&ctx)(nil)
	for _, deed := range campaign.ordered {
		if deed.Family != "END" {
			if deed.Deed.Anchor == anchor && len(missingDeed(deed.Deed, data)) == 0 {
				return true
			}
			continue
		}
		// The closure's OUT anchor still requires its receipt; its encounter
		// anchor is the approach and requires only the pre-victory prerequisites.
		approach := content.AnchorID("ANC-" + strings.TrimPrefix(string(deed.Deed.Encounter), "ENC-"))
		if approach == anchor && len(missingRequirements(deed.Deed.Prerequisites, deed.Deed.Narratives, nil, data)) == 0 {
			return true
		}
	}
	return false
}

// Checkpoint prepares a previously reached safe anchor. Its caller persists the
// proposal before restoring checkpoint resources or showing confirmation.
func (campaign *Campaign) Checkpoint(ctx context.Context, snapshot Snapshot, anchor content.AnchorID) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	if snapshot.data.Consent {
		return Snapshot{}, fmt.Errorf("%w: ending has begun", ErrBlocked)
	}
	if anchor == "" {
		return Snapshot{}, fmt.Errorf("%w: missing checkpoint anchor", ErrInvalid)
	}
	data := snapshot.Data(ctx)
	data.Anchor = anchor
	proposed := campaign.clearWork(ctx, Snapshot{data: data})
	if err = campaign.Validate(ctx, proposed); err != nil {
		return Snapshot{}, err
	}
	return proposed, nil
}

// RestorePlan validates a committed snapshot and describes recovery without
// modifying it, preparing events, increasing attempt numbers or charging fees.
func (campaign *Campaign) RestorePlan(ctx context.Context, snapshot Snapshot) (_ RestorationPlan, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return RestorationPlan{}, err
	}
	data := snapshot.Data(ctx)
	plan := RestorationPlan{
		Anchor: data.Anchor, State: "ST-PAU", Ending: data.Ending,
		Receipts: data.Receipts, Defeated: data.Defeated,
		Health: 100, Limbs: 4, Feces: 5, ClearTransient: true, ResetPartialBosses: true,
		ClearPositions: true, ClearProjectiles: true, ClearSmears: true, ClearCarriedLimbs: true,
		ClearEnemyDamage: true, ClearPartialEvidence: true, RestoreObjectOrigins: true, ResetEnemies: true,
	}
	if data.Consent {
		plan.State = "ST-SCN"
		plan.SceneEntry = string(data.Ending)
		if data.Completed {
			plan.State = "ST-END"
			plan.SceneEntry = ""
		}
	} else if plan.Anchor == "" {
		// The predecessor's zero snapshot and Start shortcut predate explicit
		// prologue anchors; their unambiguous entry points are fixed here.
		plan.Anchor = "ANC-PRO-01"
		if has(data.Narratives, content.NarrativeID("NAR-03")) {
			plan.Anchor = "ANC-PRO-03"
		}
	}
	if !data.Consent && plan.Anchor == "ANC-PRO-02" {
		plan.State, plan.SceneEntry = "ST-SCN", "NAR-02"
	}
	if !data.Consent && plan.Anchor == "ANC-PRO-03" && !has(data.Narratives, content.NarrativeID("NAR-03")) {
		plan.State, plan.SceneEntry = "ST-SCN", "NAR-03"
	}
	switch {
	case strings.HasPrefix(string(plan.Anchor), "ANC-HOS-"):
		plan.Location = "LOC-HOS"
	case strings.HasPrefix(string(plan.Anchor), "ANC-CHU-"):
		plan.Location = "LOC-CHU"
	case strings.HasPrefix(string(plan.Anchor), "ANC-MIN-"):
		plan.Location = "LOC-MIN"
	case strings.HasPrefix(string(plan.Anchor), "ANC-INS-"):
		plan.Location = "LOC-INS"
	case plan.Anchor == "ANC-PRO-01" || plan.Anchor == "ANC-PRO-02":
		plan.Location = "LOC-HOS"
	case plan.Anchor == "ANC-PRO-03":
		plan.Location = "LOC-CHU"
	}
	for _, gate := range []content.GateID{"G-HOS-IN", "G-CHU-IN", "G-MIN-IN", "G-INS-IN", "G-INS-FINAL"} {
		definition := campaign.gates[gate]
		if len(missingRequirements(definition.Deeds, definition.Narratives, definition.Defeated, data)) == 0 {
			plan.Routes = append(plan.Routes, gate)
		}
	}
	for _, record := range data.CriticalItems {
		if record.Consumed == (OccurrenceID{}) {
			plan.OwnedItems = append(plan.OwnedItems, record.Item)
		} else {
			plan.ItemRoutes = append(plan.ItemRoutes, campaign.items[record.Item].Route)
		}
	}
	slices.Sort(plan.OwnedItems)
	slices.Sort(plan.ItemRoutes)
	for _, id := range []content.DeedID{"D-INS-SUP-06", "D-INS-UTL-06"} {
		if receiptIndex(data.Receipts, id) >= 0 {
			plan.Installed = append(plan.Installed, id)
		}
	}
	// Resolve actor and terminal identities from the same validated catalogue
	// as the receipts, rather than trusting saved world effects.
	seen := make(map[content.ObjectID]bool)
	for _, deed := range campaign.ordered {
		if deed.Location != plan.Location {
			continue
		}
		for _, id := range deed.Deed.Objects {
			object := campaign.objects[id]
			if seen[id] {
				continue
			}
			seen[id] = true
			switch object.Role {
			case content.RoleActor:
				plan.MandatoryActors = append(plan.MandatoryActors, id)
			case content.RoleTerminal:
				plan.ProtectedTerminals = append(plan.ProtectedTerminals, id)
			}
		}
	}
	slices.Sort(plan.MandatoryActors)
	slices.Sort(plan.ProtectedTerminals)
	if data.Active != "" && !data.Consent {
		deed := campaign.deeds[data.Active]
		plan.ActiveTicket = data.Active
		plan.InitialObjects = slices.Clone(deed.Deed.Objects)
		plan.InitialObjective = deed.Evidence
	}
	for _, emergency := range data.Emergencies {
		view, viewErr := campaign.EmergencyInfo(ctx, snapshot, emergency.ID)
		if viewErr != nil {
			return RestorationPlan{}, viewErr
		}
		plan.Emergencies = append(plan.Emergencies, RestoredEmergency{EmergencyView: view, BehindSafeBoundary: true, BypassUsable: true})
	}
	return plan, nil
}

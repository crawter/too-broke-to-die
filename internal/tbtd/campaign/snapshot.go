// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"fmt"
	"slices"

	"too-broke-to-die/internal/tbtd/content"
)

// Attempt identifies the latest accepted attempt for a ticket.
type Attempt struct {
	Deed content.DeedID `json:"deed"`

	Number uint64 `json:"number"`

	// Failed describes transient failed work, cleared on restoration or reset.
	Failed string `json:"failed,omitempty"`
}

// Emergency records the committed start of a shared batch emergency. Later
// consequence transitions may advance its stage after durable acknowledgement.
type Emergency struct {
	ID content.EmergencyID `json:"id"`

	Stage string `json:"stage"`
}

// EmergencyActive is the stage committed with a batch's sixth receipt.
const EmergencyActive = "active"

// State is the serializable campaign portion implemented here. It has no
// credit counter; credit is derived exclusively from validated receipts.
type State struct {
	Receipts []Receipt `json:"receipts"`

	Attempts []Attempt `json:"attempts"`

	Active content.DeedID `json:"active"`

	Narratives []content.NarrativeID `json:"narratives"`

	Defeated []content.EncounterID `json:"defeated"`

	EncounterEvents []EncounterEvent `json:"encounter_events"`

	Consequences []content.ConsequenceID `json:"consequences"`

	Emergencies []Emergency `json:"emergencies"`

	Incidents []Incident `json:"incidents"`

	DebtCents int64 `json:"debt_cents"`

	CriticalItems []CriticalItemRecord `json:"critical_items"`

	CampaignID string `json:"campaign_id,omitempty"`

	OperationSequence uint64 `json:"operation_sequence,omitempty"`

	LatestOperation *OperationRecord `json:"latest_operation,omitempty"`

	Anchor content.AnchorID `json:"anchor"`

	Permit bool `json:"permit"`

	Consent bool `json:"consent"`

	Ending EndingStage `json:"ending"`

	// DonationTheft records the deliberate prologue theft before the pursuit.
	DonationTheft bool `json:"donation_theft,omitempty"`

	// SceneEntry identifies an unfinished independent or already-certified closure scene.
	SceneEntry content.NarrativeID `json:"scene_entry,omitempty"`

	// SceneUnit is the stable identity of the unfinished unit, or empty for the first.
	SceneUnit string `json:"scene_unit,omitempty"`

	TableauAcknowledged bool `json:"tableau_acknowledged"`

	Completed bool `json:"completed"`
}

// Snapshot owns its state; exported values are obtained only as deep copies.
// Its zero value is an empty campaign.
type Snapshot struct{ data State }

// Data returns an independent serializable copy.
func (snapshot Snapshot) Data(ctx context.Context) State {
	defer mon.Task()(&ctx)(nil)
	data := snapshot.data
	data.Receipts = make([]Receipt, len(snapshot.data.Receipts))
	for i, receipt := range snapshot.data.Receipts {
		data.Receipts[i] = cloneReceipt(receipt)
	}
	data.Attempts = slices.Clone(data.Attempts)
	data.Narratives = slices.Clone(data.Narratives)
	data.Defeated = slices.Clone(data.Defeated)
	data.EncounterEvents = slices.Clone(data.EncounterEvents)
	data.Consequences = slices.Clone(data.Consequences)
	data.Emergencies = slices.Clone(data.Emergencies)
	data.Incidents = slices.Clone(data.Incidents)
	data.CriticalItems = slices.Clone(data.CriticalItems)
	if data.LatestOperation != nil {
		record := *data.LatestOperation
		data.LatestOperation = &record
	}
	return data
}

// Credits is the number of committed receipt identities, not a stored balance.
func (snapshot Snapshot) Credits() int { return len(snapshot.data.Receipts) }

// Restore validates decoded data before returning an isolated snapshot. It does
// not perform filesystem recovery or claim the supplied data is durable.
func (campaign *Campaign) Restore(ctx context.Context, state State) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	candidate := Snapshot{data: state}
	candidate = Snapshot{data: candidate.Data(ctx)}
	if err = campaign.Validate(ctx, candidate); err != nil {
		return Snapshot{}, err
	}
	return campaign.clearWork(ctx, candidate), nil
}

// Validate checks receipt membership, chronology and the bindings this package owns.
func (campaign *Campaign) Validate(ctx context.Context, snapshot Snapshot) (err error) {
	defer mon.Task()(&ctx)(&err)
	data := snapshot.data
	if len(data.Receipts) > len(campaign.deeds) {
		return fmt.Errorf("%w: too many receipts", ErrInvalid)
	}
	seenAttempts := map[content.DeedID]bool{}
	for _, attempt := range data.Attempts {
		if _, ok := campaign.deeds[attempt.Deed]; !ok || seenAttempts[attempt.Deed] || attempt.Number == 0 {
			return fmt.Errorf("%w: attempt %s", ErrInvalid, attempt.Deed)
		}
		if attempt.Failed != "" && (attempt.Deed != data.Active || attempt.Failed != campaign.deeds[attempt.Deed].Failure) {
			return fmt.Errorf("%w: failed attempt %s", ErrInvalid, attempt.Deed)
		}
		seenAttempts[attempt.Deed] = true
	}
	if err = campaign.validateIncidents(ctx, data); err != nil {
		return err
	}
	debt, err := snapshot.Debt(ctx)
	if err != nil {
		return err
	}
	if debt != data.DebtCents {
		return fmt.Errorf("%w: debt does not match incident history", ErrInvalid)
	}
	if err = campaign.validateStory(ctx, data); err != nil {
		return err
	}
	seenBosses := map[content.EncounterID]bool{}
	for _, id := range data.Defeated {
		var closure content.CampaignDeed
		found := false
		for _, deed := range campaign.ordered {
			if deed.Family == "END" && deed.Deed.Encounter == id {
				closure, found = deed, true
				break
			}
		}
		if !found || seenBosses[id] {
			return fmt.Errorf("%w: boss %s", ErrInvalid, id)
		}
		seenBosses[id] = true
		for _, required := range closure.Deed.Prerequisites {
			if receiptIndex(data.Receipts, required) < 0 {
				return fmt.Errorf("%w: boss %s missing %s", ErrInvalid, id, required)
			}
		}
	}
	prefix := State{Narratives: data.Narratives, Defeated: data.Defeated}
	var consequences []content.ConsequenceID
	var emergencies []Emergency
	for i, receipt := range data.Receipts {
		deed, ok := campaign.deeds[receipt.Certification.Deed]
		if !ok || receiptIndex(prefix.Receipts, receipt.Certification.Deed) >= 0 || receipt.Ordinal != i+1 {
			return fmt.Errorf("%w: receipt %s at %d", ErrInvalid, receipt.Certification.Deed, i+1)
		}
		if len(missingDeed(deed.Deed, prefix)) != 0 {
			return fmt.Errorf("%w: out-of-order receipt %s", ErrInvalid, deed.Deed.ID)
		}
		if receipt.Title != deed.Title || receipt.TitleID != deed.Deed.Title || receipt.Institution != deed.Location || receipt.InstitutionName != deed.InstitutionName || receipt.Anchor != deed.Deed.Anchor || receipt.Consequence != deed.Deed.Consequence || receipt.Certification.Attempt == 0 || receipt.Certification.Attempt != attemptNumber(data.Attempts, deed.Deed.ID) {
			return fmt.Errorf("%w: receipt bindings %s", ErrInvalid, deed.Deed.ID)
		}
		if err = campaign.checkEvidence(deed, receipt.Certification); err != nil {
			return err
		}
		prefix.Receipts = append(prefix.Receipts, receipt)
		consequences = append(consequences, deed.Deed.Consequence)
		for _, batch := range campaign.batches {
			if has(batch.Deeds, deed.Deed.ID) && batchComplete(batch, prefix.Receipts) {
				emergencies = append(emergencies, Emergency{ID: batch.Emergency.ID, Stage: EmergencyActive})
			}
		}
	}
	if !slices.Equal(consequences, data.Consequences) || len(emergencies) != len(data.Emergencies) {
		return fmt.Errorf("%w: consequence or emergency bindings", ErrInvalid)
	}
	for i, emergency := range emergencies {
		actual := data.Emergencies[i]
		if actual.ID != emergency.ID || (actual.Stage != EmergencyActive && actual.Stage != EmergencyAftermath) {
			return fmt.Errorf("%w: emergency %s", ErrInvalid, actual.ID)
		}
	}
	if data.Active != "" {
		deed, ok := campaign.deeds[data.Active]
		if !ok || !seenAttempts[data.Active] || receiptIndex(data.Receipts, data.Active) >= 0 || len(missingDeed(deed.Deed, data)) != 0 {
			return fmt.Errorf("%w: active %s", ErrInvalid, data.Active)
		}
	}
	if data.Anchor != "" {
		valid := (data.DonationTheft && len(data.Narratives) == 0 && data.Anchor == "ANC-PRO-02") ||
			(data.SceneEntry == "NAR-01" && data.Anchor == "ANC-PRO-02") ||
			(data.SceneEntry == "NAR-04" && data.Anchor == "ANC-HOS-01") ||
			(data.SceneEntry == "NAR-08" && data.Anchor == "ANC-INS-01") ||
			(data.Anchor == "ANC-PRO-01" && len(data.Narratives) == 0) ||
			(data.Anchor == "ANC-PRO-02" && has(data.Narratives, content.NarrativeID("NAR-01")) && !has(data.Narratives, content.NarrativeID("NAR-02"))) ||
			(data.Anchor == "ANC-PRO-03" && has(data.Narratives, content.NarrativeID("NAR-02"))) ||
			(data.Anchor == "ANC-HOS-01" && has(data.Narratives, content.NarrativeID("NAR-04"))) ||
			(data.Anchor == "ANC-INS-01" && has(data.Narratives, content.NarrativeID("NAR-08"))) ||
			(data.Anchor == "ANC-MIN-PERMIT" && (data.Permit || (len(data.Receipts) == 100 && has(data.Narratives, content.NarrativeID("NAR-09"))))) ||
			(data.Consent && data.Anchor == endingAnchor(data.Ending)) ||
			campaign.reachableEncounterAnchor(ctx, data, data.Anchor)
		for _, receipt := range data.Receipts {
			if receipt.Anchor == data.Anchor {
				valid = true
			}
		}
		for _, id := range data.Defeated {
			if data.Anchor == content.AnchorID("ANC-"+string(id)[4:]) {
				valid = true
			}
		}
		for _, item := range data.CriticalItems {
			definition := campaign.items[item.Item]
			if data.Anchor == definition.AcquireAnchor || (item.Consumed.Sequence != 0 && data.Anchor == definition.ConsumeAnchor) {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("%w: anchor %s", ErrInvalid, data.Anchor)
		}
	} else if data.Active != "" || len(data.Receipts) != 0 || len(data.Defeated) != 0 || len(data.CriticalItems) != 0 || (len(data.Narratives) != 0 && len(data.Narratives) != 3) {
		return fmt.Errorf("%w: missing anchor", ErrInvalid)
	}
	return nil
}

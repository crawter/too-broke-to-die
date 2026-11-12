// Copyright (C) 2026 crawter
// All rights reserved.

// Package campaign prepares, but does not persist, campaign transitions.
package campaign

import (
	"context"
	"errors"
	"fmt"

	monkit "github.com/spacemonkeygo/monkit/v3"

	"too-broke-to-die/internal/tbtd/content"
)

var mon = monkit.Package()

var (
	// ErrInvalid means a snapshot or event violates the campaign contract.
	ErrInvalid = errors.New("invalid campaign data")
	// ErrConflict means a certification identity was reused with different data.
	ErrConflict = errors.New("campaign identity conflict")
	// ErrBlocked means a transition is missing prerequisites or another ticket is active.
	ErrBlocked = errors.New("campaign transition blocked")
)

// Campaign holds the validated authored catalogue, never a player credit balance.
type Campaign struct {
	deeds   map[content.DeedID]content.CampaignDeed
	ordered []content.CampaignDeed
	batches []content.CampaignBatch
	gates   map[content.GateID]content.Gate
	objects map[content.ObjectID]content.Object
	items   map[CriticalItemID]CriticalItemDefinition
}

// New constructs a campaign from the validated content manifest.
func New(ctx context.Context, manifest *content.Campaign) (_ *Campaign, err error) {
	defer mon.Task()(&ctx)(&err)
	if manifest == nil {
		return nil, fmt.Errorf("%w: nil manifest", ErrInvalid)
	}
	campaign := &Campaign{deeds: make(map[content.DeedID]content.CampaignDeed), gates: make(map[content.GateID]content.Gate), objects: make(map[content.ObjectID]content.Object)}
	campaign.ordered = manifest.Deeds(ctx)
	campaign.batches = manifest.Batches(ctx)
	if len(campaign.ordered) != 100 || len(campaign.batches) != 16 {
		return nil, fmt.Errorf("%w: incomplete manifest", ErrInvalid)
	}
	for _, deed := range campaign.ordered {
		if _, ok := campaign.deeds[deed.Deed.ID]; ok || deed.Deed.ID == "" {
			return nil, fmt.Errorf("%w: duplicate deed %s", ErrInvalid, deed.Deed.ID)
		}
		campaign.deeds[deed.Deed.ID] = deed
	}
	for _, gate := range manifest.Gates(ctx) {
		campaign.gates[gate.ID] = gate
	}
	for _, object := range manifest.Objects(ctx) {
		campaign.objects[object.ID] = object
	}
	return campaign, nil
}

// Start prepares the NAR-03 milestone. The caller must commit the result.
func (campaign *Campaign) Start(ctx context.Context, snapshot Snapshot) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	data := snapshot.Data(ctx)
	if !has(data.Narratives, content.NarrativeID("NAR-03")) {
		data.Narratives = append(data.Narratives, "NAR-03")
	}
	return Snapshot{data: data}, nil
}

// Accept prepares an active ticket. Switching tickets requires Reset first.
// Durable callers should use Prepare with AttemptAccept so retries retain their identity.
func (campaign *Campaign) Accept(ctx context.Context, snapshot Snapshot, id content.DeedID) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	deed, ok := campaign.deeds[id]
	if !ok {
		return Snapshot{}, fmt.Errorf("%w: unknown deed %s", ErrInvalid, id)
	}
	data := snapshot.Data(ctx)
	if data.Active != "" && data.Active != id {
		return Snapshot{}, fmt.Errorf("%w: active %s", ErrBlocked, data.Active)
	}
	if receiptIndex(data.Receipts, id) >= 0 {
		return Snapshot{}, fmt.Errorf("%w: already certified %s", ErrBlocked, id)
	}
	if missing := missingDeed(deed.Deed, data); len(missing) != 0 {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrBlocked, missing)
	}
	if data.Active == id {
		return Snapshot{data: data}, nil
	}
	for i := range data.Attempts {
		if data.Attempts[i].Deed == id {
			if data.Attempts[i].Number == ^uint64(0) {
				return Snapshot{}, fmt.Errorf("%w: attempt overflow", ErrInvalid)
			}
			data.Attempts[i].Number++
			data.Active = id
			return Snapshot{data: data}, nil
		}
	}
	data.Attempts = append(data.Attempts, Attempt{Deed: id, Number: 1})
	data.Active = id
	return Snapshot{data: data}, nil
}

// Reset starts a new attempt for the active ticket without changing receipts.
// Durable callers should use Prepare with AttemptReset; this helper has no retry identity.
func (campaign *Campaign) Reset(ctx context.Context, snapshot Snapshot) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	data := snapshot.Data(ctx)
	if data.Active == "" {
		return Snapshot{}, fmt.Errorf("%w: no active ticket", ErrBlocked)
	}
	for i := range data.Attempts {
		if data.Attempts[i].Deed == data.Active {
			if data.Attempts[i].Number == ^uint64(0) {
				return Snapshot{}, fmt.Errorf("%w: attempt overflow", ErrInvalid)
			}
			data.Attempts[i].Number++
			data.Attempts[i].Failed = ""
			return Snapshot{data: data}, nil
		}
	}
	return Snapshot{}, fmt.Errorf("%w: missing active attempt", ErrInvalid)
}

// Abandon clears an uncertified active ticket before selecting another one.
// The caller must warn and obtain confirmation before switching. Its attempt
// history remains available for subsequent acceptance. Durable callers use
// Prepare with AttemptAbandon to bind the change to an operation identity.
func (campaign *Campaign) Abandon(ctx context.Context, snapshot Snapshot) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	data := snapshot.Data(ctx)
	for i := range data.Attempts {
		if data.Attempts[i].Deed == data.Active {
			data.Attempts[i].Failed = ""
		}
	}
	data.Active = ""
	return Snapshot{data: data}, nil
}

// DefeatBoss prepares a durable boss victory, never a credit.
func (campaign *Campaign) DefeatBoss(ctx context.Context, snapshot Snapshot, id content.EncounterID) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	data := snapshot.Data(ctx)
	var closure content.CampaignDeed
	found := false
	for _, deed := range campaign.ordered {
		if deed.Family == "END" && deed.Deed.Encounter == id {
			closure, found = deed, true
			break
		}
	}
	if !found {
		return Snapshot{}, fmt.Errorf("%w: unknown boss %s", ErrInvalid, id)
	}
	if has(data.Defeated, id) {
		return Snapshot{data: data}, nil
	}
	if data.Active != "" {
		return Snapshot{}, fmt.Errorf("%w: active %s", ErrBlocked, data.Active)
	}
	// The boss approach requires the preceding receipts, not its own victory.
	for _, required := range closure.Deed.Prerequisites {
		if receiptIndex(data.Receipts, required) < 0 {
			return Snapshot{}, fmt.Errorf("%w: %s", ErrBlocked, required)
		}
	}
	data.Defeated = append(data.Defeated, id)
	data.Anchor = content.AnchorID("ANC-" + string(id)[4:])
	return Snapshot{data: data}, nil
}

// Apply prepares one certification. Nothing is published until its caller
// durably commits the returned snapshot.
func (campaign *Campaign) Apply(ctx context.Context, snapshot Snapshot, event Certification) (_ Snapshot, _ Receipt, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, Receipt{}, err
	}
	deed, ok := campaign.deeds[event.Deed]
	if !ok {
		return Snapshot{}, Receipt{}, fmt.Errorf("%w: unknown deed %s", ErrInvalid, event.Deed)
	}
	data := snapshot.Data(ctx)
	if index := receiptIndex(data.Receipts, event.Deed); index >= 0 {
		receipt := data.Receipts[index]
		if !sameCertification(receipt.Certification, event) {
			return Snapshot{}, Receipt{}, fmt.Errorf("%w: %s", ErrConflict, event.Deed)
		}
		return Snapshot{data: data}, cloneReceipt(receipt), nil
	}
	if data.Active != event.Deed || attemptNumber(data.Attempts, event.Deed) != event.Attempt {
		return Snapshot{}, Receipt{}, fmt.Errorf("%w: inactive or wrong attempt %s", ErrBlocked, event.Deed)
	}
	for _, attempt := range data.Attempts {
		if attempt.Deed == event.Deed && attempt.Failed != "" {
			return Snapshot{}, Receipt{}, fmt.Errorf("%w: failed attempt %s", ErrBlocked, event.Deed)
		}
	}
	if missing := missingDeed(deed.Deed, data); len(missing) != 0 {
		return Snapshot{}, Receipt{}, fmt.Errorf("%w: %v", ErrBlocked, missing)
	}
	if err = campaign.checkEvidence(deed, event); err != nil {
		return Snapshot{}, Receipt{}, err
	}
	receipt := Receipt{Certification: cloneCertification(event), Title: deed.Title, TitleID: deed.Deed.Title, Institution: deed.Location, InstitutionName: deed.InstitutionName, Ordinal: len(data.Receipts) + 1, Anchor: deed.Deed.Anchor, Consequence: deed.Deed.Consequence}
	data.Receipts = append(data.Receipts, receipt)
	data.Consequences = append(data.Consequences, deed.Deed.Consequence)
	data.Active = ""
	data.Anchor = deed.Deed.Anchor
	if deed.CompletedNarrative != "" {
		data.Narratives = append(data.Narratives, deed.CompletedNarrative)
	}
	for _, batch := range campaign.batches {
		if has(batch.Deeds, event.Deed) && batchComplete(batch, data.Receipts) {
			data.Emergencies = append(data.Emergencies, Emergency{ID: batch.Emergency.ID, Stage: EmergencyActive})
		}
	}
	result := Snapshot{data: data}
	if err = campaign.Validate(ctx, result); err != nil {
		return Snapshot{}, Receipt{}, err
	}
	return result, cloneReceipt(receipt), nil
}

func has[T comparable](values []T, value T) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func attemptNumber(attempts []Attempt, id content.DeedID) uint64 {
	for _, attempt := range attempts {
		if attempt.Deed == id {
			return attempt.Number
		}
	}
	return 0
}

func receiptIndex(receipts []Receipt, id content.DeedID) int {
	for i := range receipts {
		if receipts[i].Certification.Deed == id {
			return i
		}
	}
	return -1
}

func batchComplete(batch content.CampaignBatch, receipts []Receipt) bool {
	for _, id := range batch.Deeds {
		if receiptIndex(receipts, id) < 0 {
			return false
		}
	}
	return true
}

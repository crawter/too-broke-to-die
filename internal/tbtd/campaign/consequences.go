// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"fmt"

	"too-broke-to-die/internal/tbtd/content"
)

// EmergencyAftermath is the durable destination after escape, waiting or cleanup.
const EmergencyAftermath = "aftermath"

// ConsequenceView resolves a receipt to its authored affected service/person
// and permanent outcome, without creating an additional credit source.
type ConsequenceView struct {
	ID content.ConsequenceID

	Deed content.DeedID

	Affected string

	Detail string

	Aftermath string
}

// EmergencyView supplies reconstruction data. A restored active stage starts a
// fresh warning only after the caller has acknowledged persistence and resumed.
type EmergencyView struct {
	Emergency Emergency

	Cause content.DeedID

	Encounter content.EncounterID

	Anchor content.AnchorID

	WarningMilliseconds int64

	ActiveMilliseconds int64

	Hazard string

	SafeRouteAndAftermath string
}

// Consequence resolves a recorded consequence using the immutable catalogue.
func (campaign *Campaign) Consequence(ctx context.Context, snapshot Snapshot, id content.ConsequenceID) (_ ConsequenceView, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return ConsequenceView{}, err
	}
	for _, receipt := range snapshot.data.Receipts {
		if receipt.Consequence == id {
			deed := campaign.deeds[receipt.Certification.Deed]
			aftermath := deed.IndividualAftermath
			if deed.Family == "END" {
				aftermath = deed.Outcome
			}
			return ConsequenceView{ID: id, Deed: deed.Deed.ID, Affected: deed.Label, Detail: deed.Detail, Aftermath: aftermath}, nil
		}
	}
	return ConsequenceView{}, fmt.Errorf("%w: unrecorded consequence %s", ErrBlocked, id)
}

// EmergencyInfo resolves the sixth receipt and authored safe recovery contract.
func (campaign *Campaign) EmergencyInfo(ctx context.Context, snapshot Snapshot, id content.EmergencyID) (_ EmergencyView, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return EmergencyView{}, err
	}
	for _, emergency := range snapshot.data.Emergencies {
		if emergency.ID != id {
			continue
		}
		for _, batch := range campaign.batches {
			if batch.Emergency.ID != id {
				continue
			}
			count := 0
			for _, receipt := range snapshot.data.Receipts {
				if has(batch.Deeds, receipt.Certification.Deed) {
					count++
					if count == 6 {
						return EmergencyView{Emergency: emergency, Cause: receipt.Certification.Deed, Encounter: batch.Emergency.Encounter, Anchor: batch.Emergency.Anchor, WarningMilliseconds: batch.Emergency.WarningMilliseconds, ActiveMilliseconds: batch.Emergency.ActiveMilliseconds, Hazard: batch.Hazard, SafeRouteAndAftermath: batch.Recovery}, nil
					}
				}
			}
		}
	}
	return EmergencyView{}, fmt.Errorf("%w: unrecorded emergency %s", ErrBlocked, id)
}

// EmergencyTransitionID is the stable identity of the later aftermath event.
func EmergencyTransitionID(id content.EmergencyID) string {
	return string(id) + "-" + EmergencyAftermath
}

func advanceEmergency(ctx context.Context, data *State, id content.EmergencyID) (err error) {
	defer mon.Task()(&ctx)(&err)
	for i := range data.Emergencies {
		if data.Emergencies[i].ID == id {
			data.Emergencies[i].Stage = EmergencyAftermath
			return nil
		}
	}
	return fmt.Errorf("%w: no active emergency %s", ErrBlocked, id)
}

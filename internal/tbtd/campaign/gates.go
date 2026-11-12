// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"fmt"

	"too-broke-to-die/internal/tbtd/content"
)

// MissingDeed reports the stable identities blocking an authored certification.
func (campaign *Campaign) MissingDeed(ctx context.Context, snapshot Snapshot, id content.DeedID) (_ []string, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return nil, err
	}
	deed, ok := campaign.deeds[id]
	if !ok {
		return nil, fmt.Errorf("%w: unknown deed %s", ErrInvalid, id)
	}
	return missingDeed(deed.Deed, snapshot.data), nil
}

// MissingGate reports the stable identities blocking an authored gate.
// The permit gate inspects only the manifest receipt identities and NAR-09.
func (campaign *Campaign) MissingGate(ctx context.Context, snapshot Snapshot, id content.GateID) (_ []string, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return nil, err
	}
	gate, ok := campaign.gates[id]
	if !ok {
		return nil, fmt.Errorf("%w: unknown gate %s", ErrInvalid, id)
	}
	data := snapshot.data
	missing := missingRequirements(gate.Deeds, gate.Narratives, gate.Defeated, data)
	if gate.RequirePermit && !data.Permit {
		missing = append(missing, "PERMIT")
	}
	if gate.RequireConsent && !data.Consent {
		missing = append(missing, "CONSENT")
	}
	return missing, nil
}

// missingDeed and missingRequirements are measured by their callers.
func missingDeed(deed content.Deed, data State) []string {
	return missingRequirements(deed.Prerequisites, deed.Narratives, deed.Defeated, data)
}

func missingRequirements(deeds []content.DeedID, narratives []content.NarrativeID, defeated []content.EncounterID, data State) []string {
	var missing []string
	for _, id := range deeds {
		if receiptIndex(data.Receipts, id) < 0 {
			missing = append(missing, string(id))
		}
	}
	for _, id := range narratives {
		if !has(data.Narratives, id) {
			missing = append(missing, string(id))
		}
	}
	for _, id := range defeated {
		if !has(data.Defeated, id) {
			missing = append(missing, string(id))
		}
	}
	return missing
}

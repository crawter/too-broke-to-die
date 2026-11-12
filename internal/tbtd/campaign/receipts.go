// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"fmt"
	"slices"

	"too-broke-to-die/internal/tbtd/content"
)

// Certification is a completed evidence candidate from the active attempt.
// EvidenceObjects are the ticket-specific authored objects, in manifest order;
// Terminal and Trigger name the actual certification interaction. Simulation
// must establish that the described evidence and hold actually occurred.
type Certification struct {
	Deed content.DeedID `json:"deed"`

	Attempt uint64 `json:"attempt"`

	Evidence string `json:"evidence"`

	EvidenceObjects []content.ObjectID `json:"evidence_objects"`

	Trigger string `json:"trigger"`

	Terminal content.ObjectID `json:"terminal"`

	HoldMilliseconds int64 `json:"hold_ms"`
}

// Receipt records a single certification event and its authored bindings.
type Receipt struct {
	Certification Certification `json:"certification"`

	Title string `json:"title"`

	TitleID content.PresentationID `json:"title_id"`

	Institution content.LocationID `json:"institution"`

	InstitutionName string `json:"institution_name"`

	Ordinal int `json:"ordinal"`

	Anchor content.AnchorID `json:"anchor"`

	Consequence content.ConsequenceID `json:"consequence"`
}

func cloneCertification(event Certification) Certification {
	event.EvidenceObjects = slices.Clone(event.EvidenceObjects)
	return event
}

func cloneReceipt(receipt Receipt) Receipt {
	receipt.Certification = cloneCertification(receipt.Certification)
	return receipt
}

func sameCertification(left, right Certification) bool {
	return left.Deed == right.Deed && left.Attempt == right.Attempt && left.Evidence == right.Evidence && slices.Equal(left.EvidenceObjects, right.EvidenceObjects) && left.Trigger == right.Trigger && left.Terminal == right.Terminal && left.HoldMilliseconds == right.HoldMilliseconds
}

// checkEvidence is a hot per-receipt helper; the enclosing operation is measured.
func (campaign *Campaign) checkEvidence(deed content.CampaignDeed, event Certification) error {
	var objects []content.ObjectID
	var terminal content.ObjectID
	for _, id := range deed.Deed.Objects {
		object, ok := campaign.objects[id]
		if !ok {
			return fmt.Errorf("%w: missing object %s", ErrInvalid, id)
		}
		if object.Deed == deed.Deed.ID {
			objects = append(objects, id)
		}
		if object.Role == content.RoleTerminal {
			terminal = id
		}
	}
	if event.Deed != deed.Deed.ID || event.Attempt == 0 || event.Evidence != deed.Evidence || !slices.Equal(event.EvidenceObjects, objects) || event.Trigger != deed.Trigger || terminal == "" || event.Terminal != terminal || event.HoldMilliseconds < deed.Deed.HoldMilliseconds {
		return fmt.Errorf("%w: evidence or trigger for %s", ErrInvalid, deed.Deed.ID)
	}
	return nil
}

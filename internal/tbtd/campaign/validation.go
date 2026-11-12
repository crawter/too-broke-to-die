// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"fmt"
	"strings"

	"too-broke-to-die/internal/tbtd/content"
)

func endingAnchor(stage EndingStage) content.AnchorID {
	switch stage {
	case EndingDeath:
		return "ANC-END-DEATH"
	case EndingHell:
		return "ANC-END-HELL"
	case EndingCeremony:
		return "ANC-END-CEREMONY"
	case EndingTableau:
		return "ANC-END-TABLEAU"
	default:
		return ""
	}
}

// validateStory checks the entire milestone prefix, not just the milestones
// referenced by the currently authored deed catalogue.
func (campaign *Campaign) validateStory(ctx context.Context, data State) (err error) {
	defer mon.Task()(&ctx)(&err)
	order := []content.NarrativeID{"NAR-01", "NAR-02", "NAR-03", "NAR-04", "NAR-05", "NAR-06", "NAR-07", "NAR-08", "NAR-09", "NAR-10", "NAR-11", "NAR-12"}
	if len(data.Narratives) > len(order) {
		return fmt.Errorf("%w: narrative count", ErrInvalid)
	}
	for i, id := range data.Narratives {
		if id != order[i] {
			return fmt.Errorf("%w: narrative order or missing milestone at %s", ErrInvalid, id)
		}
	}
	for _, binding := range []struct {
		narrative content.NarrativeID
		deed      content.DeedID
	}{
		{"NAR-05", "D-HOS-END"},
		{"NAR-06", "D-CHU-END"},
		{"NAR-07", "D-MIN-END"},
		{"NAR-09", "D-INS-END"},
	} {
		if has(data.Narratives, binding.narrative) != (receiptIndex(data.Receipts, binding.deed) >= 0) {
			return fmt.Errorf("%w: narrative %s and closure %s", ErrInvalid, binding.narrative, binding.deed)
		}
	}
	// A visited institution cannot lose its arrival scene, even when the
	// catalogue's gate only names an earlier closure.
	for _, attempt := range data.Attempts {
		deed, ok := campaign.deeds[attempt.Deed]
		if !ok {
			return fmt.Errorf("%w: unknown attempted deed", ErrInvalid)
		}
		if deed.Location == "LOC-HOS" && !has(data.Narratives, content.NarrativeID("NAR-04")) {
			return fmt.Errorf("%w: missing NAR-04", ErrInvalid)
		}
		if deed.Location == "LOC-INS" && !has(data.Narratives, content.NarrativeID("NAR-08")) {
			return fmt.Errorf("%w: missing NAR-08", ErrInvalid)
		}
	}
	for _, item := range data.CriticalItems {
		definition := campaign.items[item.Item]
		anchors := []content.AnchorID{definition.AcquireAnchor}
		if item.Consumed != (OccurrenceID{}) {
			anchors = append(anchors, definition.ConsumeAnchor)
		}
		for _, anchor := range anchors {
			if strings.HasPrefix(string(anchor), "ANC-HOS-") && !has(data.Narratives, content.NarrativeID("NAR-04")) {
				return fmt.Errorf("%w: item missing NAR-04", ErrInvalid)
			}
			if strings.HasPrefix(string(anchor), "ANC-INS-") && !has(data.Narratives, content.NarrativeID("NAR-08")) {
				return fmt.Errorf("%w: item missing NAR-08", ErrInvalid)
			}
		}
	}
	if data.Permit && (len(data.Receipts) != 100 || !has(data.Narratives, content.NarrativeID("NAR-09"))) {
		return fmt.Errorf("%w: permit prerequisites", ErrInvalid)
	}
	if !data.Consent {
		if data.TableauAcknowledged || data.Completed || has(data.Narratives, content.NarrativeID("NAR-10")) || (data.Permit && data.Ending != EndingReview) || (!data.Permit && data.Ending != "") {
			return fmt.Errorf("%w: premature ending", ErrInvalid)
		}
		return nil
	}
	if !data.Permit || data.Active != "" || endingAnchor(data.Ending) == "" || data.Anchor != endingAnchor(data.Ending) {
		return fmt.Errorf("%w: consent or ending anchor", ErrInvalid)
	}
	deathDone := data.Ending == EndingHell || data.Ending == EndingCeremony || data.Ending == EndingTableau
	hellDone := data.Ending == EndingCeremony || data.Ending == EndingTableau
	if has(data.Narratives, content.NarrativeID("NAR-10")) != deathDone || has(data.Narratives, content.NarrativeID("NAR-11")) != hellDone || data.TableauAcknowledged != data.Completed || has(data.Narratives, content.NarrativeID("NAR-12")) != data.Completed || (data.Completed && data.Ending != EndingTableau) {
		return fmt.Errorf("%w: inconsistent ending stage", ErrInvalid)
	}
	return nil
}

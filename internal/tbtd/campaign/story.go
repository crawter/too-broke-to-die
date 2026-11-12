// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"fmt"

	"too-broke-to-die/internal/tbtd/content"
)

// EndingStage names a durable entry point, not a frame within a scene.
type EndingStage string

const (
	// EndingReview records permit issuance before consent; ordinary play remains available.
	EndingReview EndingStage = "END-REVIEW"

	// EndingDeath identifies entry to the authorized-death scene, before its completion.
	EndingDeath EndingStage = "END-DEATH"

	// EndingHell identifies entry to the Hell scene, after authorized death completes.
	EndingHell EndingStage = "END-HELL"

	// EndingCeremony identifies entry to the ceremony, after the Hell scene completes.
	EndingCeremony EndingStage = "END-CEREMONY"

	// EndingTableau identifies entry to the tableau; Completed separately records its acknowledgment.
	EndingTableau EndingStage = "END-TABLEAU"
)

// CompleteNarrative prepares an independently committed prologue or arrival
// milestone. Closure and ending milestones are committed by their own events.
// Calling it again returns the existing result without moving the anchor.
func (campaign *Campaign) CompleteNarrative(ctx context.Context, snapshot Snapshot, id content.NarrativeID) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	data := snapshot.Data(ctx)
	if data.Consent {
		return Snapshot{}, fmt.Errorf("%w: ending has begun", ErrBlocked)
	}
	var prior content.NarrativeID
	var anchor content.AnchorID
	switch id {
	case "NAR-01":
		anchor = "ANC-PRO-02"
	case "NAR-02":
		prior, anchor = "NAR-01", "ANC-PRO-03"
	case "NAR-03":
		prior, anchor = "NAR-02", "ANC-PRO-03"
	case "NAR-04":
		prior, anchor = "NAR-03", "ANC-HOS-01"
	case "NAR-08":
		prior, anchor = "NAR-07", "ANC-INS-01"
	default:
		return Snapshot{}, fmt.Errorf("%w: narrative %s is not an independent milestone", ErrInvalid, id)
	}
	if has(data.Narratives, id) {
		return Snapshot{data: data}, nil
	}
	if prior != "" && !has(data.Narratives, prior) {
		return Snapshot{}, fmt.Errorf("%w: missing %s", ErrBlocked, prior)
	}
	data.Narratives = append(data.Narratives, id)
	data.Anchor = anchor
	return Snapshot{data: data}, nil
}

// IssuePermit prepares the one subsidized permit. Debt and player resources
// are deliberately not inputs. It leaves ordinary play available until consent.
func (campaign *Campaign) IssuePermit(ctx context.Context, snapshot Snapshot) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	data := snapshot.Data(ctx)
	if data.Permit {
		return Snapshot{data: data}, nil
	}
	missing := missingRequirements(campaign.gates["G-PERMIT"].Deeds, campaign.gates["G-PERMIT"].Narratives, nil, data)
	if len(missing) != 0 {
		return Snapshot{}, fmt.Errorf("%w: permit missing %v", ErrBlocked, missing)
	}
	data.Permit = true
	data.Ending = EndingReview
	data.Anchor = "ANC-MIN-PERMIT"
	return Snapshot{data: data}, nil
}

// ConsentDecision prepares confirmed consent or preserves the unchanged
// snapshot on cancellation. It does not charge a temporary-death fee.
func (campaign *Campaign) ConsentDecision(ctx context.Context, snapshot Snapshot, confirmed bool) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	data := snapshot.Data(ctx)
	if data.Consent {
		if !confirmed {
			return Snapshot{}, fmt.Errorf("%w: consent already committed", ErrBlocked)
		}
		return Snapshot{data: data}, nil
	}
	if !data.Permit {
		return Snapshot{}, fmt.Errorf("%w: permit required", ErrBlocked)
	}
	if !confirmed {
		return Snapshot{data: data}, nil
	}
	data.Consent = true
	data.Ending = EndingDeath
	data.Anchor = "ANC-END-DEATH"
	return Snapshot{data: data}, nil
}

// AdvanceEnding commits the completed scene unit named by stage. Repeating a
// previously completed unit is idempotent; skipping ahead is rejected.
func (campaign *Campaign) AdvanceEnding(ctx context.Context, snapshot Snapshot, stage EndingStage) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	data := snapshot.Data(ctx)
	if !data.Consent {
		return Snapshot{}, fmt.Errorf("%w: consent required", ErrBlocked)
	}
	order := []EndingStage{EndingDeath, EndingHell, EndingCeremony, EndingTableau}
	current, requested := -1, -1
	for i, value := range order {
		if value == data.Ending {
			current = i
		}
		if value == stage {
			requested = i
		}
	}
	if requested < 0 {
		return Snapshot{}, fmt.Errorf("%w: ending stage %s", ErrInvalid, stage)
	}
	if requested < current || (requested == current && data.Completed) {
		return Snapshot{data: data}, nil
	}
	if requested != current {
		return Snapshot{}, fmt.Errorf("%w: ending stage %s", ErrBlocked, stage)
	}
	switch stage {
	case EndingDeath:
		data.Narratives = append(data.Narratives, "NAR-10")
		data.Ending, data.Anchor = EndingHell, "ANC-END-HELL"
	case EndingHell:
		data.Narratives = append(data.Narratives, "NAR-11")
		data.Ending, data.Anchor = EndingCeremony, "ANC-END-CEREMONY"
	case EndingCeremony:
		data.Ending, data.Anchor = EndingTableau, "ANC-END-TABLEAU"
	case EndingTableau:
		data.Narratives = append(data.Narratives, "NAR-12")
		data.TableauAcknowledged, data.Completed = true, true
	}
	return Snapshot{data: data}, nil
}

// prepareStory binds a named milestone to the same retry identity and result
// digest as other durable operations. Already completed milestones are no-ops.
func (campaign *Campaign) prepareStory(ctx context.Context, snapshot Snapshot, transition Transition, digest string) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	change := transition.Story
	var proposed Snapshot
	switch change.Action {
	case StoryNarrative:
		if change.Narrative == "" || change.Stage != "" {
			return Snapshot{}, fmt.Errorf("%w: narrative identity", ErrInvalid)
		}
		proposed, err = campaign.CompleteNarrative(ctx, snapshot, change.Narrative)
	case StoryPermit, StoryConsent:
		if change.Narrative != "" || change.Stage != "" {
			return Snapshot{}, fmt.Errorf("%w: unexpected story identity", ErrInvalid)
		}
		if change.Action == StoryPermit {
			proposed, err = campaign.IssuePermit(ctx, snapshot)
		} else {
			proposed, err = campaign.ConsentDecision(ctx, snapshot, true)
		}
	case StoryEnding:
		if change.Narrative != "" || change.Stage == "" {
			return Snapshot{}, fmt.Errorf("%w: ending identity", ErrInvalid)
		}
		proposed, err = campaign.AdvanceEnding(ctx, snapshot, change.Stage)
	default:
		return Snapshot{}, fmt.Errorf("%w: story action", ErrInvalid)
	}
	if err != nil {
		return Snapshot{}, err
	}
	return campaign.recordNamedOperation(ctx, snapshot, proposed, transition, digest)
}

func (campaign *Campaign) recordNamedOperation(ctx context.Context, snapshot, proposed Snapshot, transition Transition, digest string) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	before, err := digestState(ctx, snapshot.data)
	if err != nil {
		return Snapshot{}, err
	}
	after, err := digestState(ctx, proposed.data)
	if err != nil {
		return Snapshot{}, err
	}
	if before == after {
		return proposed, nil
	}
	data := proposed.Data(ctx)
	data.CampaignID = transition.CampaignID
	data.OperationSequence = transition.Sequence
	after, err = digestState(ctx, data)
	if err != nil {
		return Snapshot{}, err
	}
	data.LatestOperation = &OperationRecord{Sequence: transition.Sequence, Digest: digest, ResultDigest: after}
	proposed = Snapshot{data: data}
	if err = campaign.Validate(ctx, proposed); err != nil {
		return Snapshot{}, err
	}
	return proposed, nil
}

// EndingReplay describes a read-only narrative session. It carries no campaign
// transition and cannot issue rewards, penalties or another permit.
type EndingReplay struct {
	Start EndingStage

	ReadOnly bool
}

// ReplayEnding is available only after the mandatory tableau was acknowledged.
func (campaign *Campaign) ReplayEnding(ctx context.Context, snapshot Snapshot) (_ EndingReplay, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return EndingReplay{}, err
	}
	if !snapshot.data.Completed {
		return EndingReplay{}, fmt.Errorf("%w: ending not complete", ErrBlocked)
	}
	return EndingReplay{Start: EndingDeath, ReadOnly: true}, nil
}

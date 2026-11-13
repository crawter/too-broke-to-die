// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"fmt"
	"regexp"

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
	data.SceneEntry, data.SceneUnit = "", ""
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
	data.SceneEntry, data.SceneUnit = "", ""
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
	data.SceneUnit = ""
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

var sceneUnitPattern = regexp.MustCompile(`^UNIT-[A-Z0-9]+(?:-[A-Z0-9]+)*$`)

func validateSceneState(data State) error {
	if data.SceneUnit != "" && (len(data.SceneUnit) > 160 || !sceneUnitPattern.MatchString(data.SceneUnit)) {
		return fmt.Errorf("%w: invalid scene unit", ErrInvalid)
	}
	if data.SceneEntry == "" {
		if data.SceneUnit != "" && (!data.Consent || data.Completed) {
			return fmt.Errorf("%w: unit without a scene", ErrInvalid)
		}
		return nil
	}
	if data.Consent || data.Completed || data.SceneUnit == "" {
		return fmt.Errorf("%w: independent scene during ending or missing unit", ErrInvalid)
	}
	var index int
	var anchor content.AnchorID
	switch data.SceneEntry {
	case "NAR-01":
		index, anchor = 0, "ANC-PRO-02"
	case "NAR-02":
		index, anchor = 1, "ANC-PRO-02"
	case "NAR-03":
		index, anchor = 2, "ANC-PRO-03"
	case "NAR-04":
		index, anchor = 3, "ANC-HOS-01"
	case "NAR-08":
		index, anchor = 7, "ANC-INS-01"
	case "NAR-05", "NAR-06", "NAR-07", "NAR-09":
		deed, expectedAnchor := closureBinding(data.SceneEntry)
		index := map[content.NarrativeID]int{"NAR-05": 5, "NAR-06": 6, "NAR-07": 7, "NAR-09": 9}[data.SceneEntry]
		if len(data.Narratives) != index || receiptIndex(data.Receipts, deed) < 0 || !has(data.Narratives, data.SceneEntry) || data.Anchor != expectedAnchor || data.Active != "" {
			return fmt.Errorf("%w: closure scene prerequisites", ErrInvalid)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown scene entry", ErrInvalid)
	}
	if len(data.Narratives) != index || data.Anchor != anchor || data.Active != "" {
		return fmt.Errorf("%w: scene entry prerequisites or anchor", ErrInvalid)
	}
	return nil
}

func (campaign *Campaign) prepareSceneEntry(ctx context.Context, snapshot Snapshot, change StoryChange) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	data := snapshot.Data(ctx)
	if change.Unit == "" || len(change.Unit) > 160 || !sceneUnitPattern.MatchString(change.Unit) {
		return Snapshot{}, fmt.Errorf("%w: scene unit", ErrInvalid)
	}
	if data.Consent {
		if change.Action != StorySceneUnit || change.Narrative != "" || change.Stage != data.Ending || data.Completed {
			return Snapshot{}, fmt.Errorf("%w: ending scene entry", ErrBlocked)
		}
		data.SceneUnit = change.Unit
		return Snapshot{data: data}, nil
	}
	if change.Stage != "" || change.Narrative == "" || (data.SceneEntry != "" && data.SceneEntry != change.Narrative) {
		return Snapshot{}, fmt.Errorf("%w: narrative scene entry", ErrBlocked)
	}
	if deed, _ := closureBinding(change.Narrative); deed != "" {
		if change.Action != StorySceneUnit || data.SceneEntry != change.Narrative {
			return Snapshot{}, fmt.Errorf("%w: closure scene is not active", ErrBlocked)
		}
		data.SceneUnit = change.Unit
		return Snapshot{data: data}, nil
	}
	if change.Action == StorySceneEnter && data.SceneEntry != "" {
		return Snapshot{}, fmt.Errorf("%w: scene already entered", ErrBlocked)
	}
	if change.Action == StorySceneUnit && data.SceneEntry == "" && !((change.Narrative == "NAR-02" && data.Anchor == "ANC-PRO-02") || (change.Narrative == "NAR-03" && data.Anchor == "ANC-PRO-03")) {
		return Snapshot{}, fmt.Errorf("%w: scene has not been entered", ErrBlocked)
	}
	// The existing milestone operation owns prerequisites and refuses already
	// completed or non-independent narratives. Its proposal is not committed here.
	if has(data.Narratives, change.Narrative) {
		return Snapshot{}, fmt.Errorf("%w: narrative already completed", ErrBlocked)
	}
	if _, err = campaign.CompleteNarrative(ctx, snapshot, change.Narrative); err != nil {
		return Snapshot{}, err
	}
	switch change.Narrative {
	case "NAR-01", "NAR-02":
		data.Anchor = "ANC-PRO-02"
	case "NAR-03":
		data.Anchor = "ANC-PRO-03"
	case "NAR-04":
		data.Anchor = "ANC-HOS-01"
	case "NAR-08":
		data.Anchor = "ANC-INS-01"
	}
	data.SceneEntry, data.SceneUnit = change.Narrative, change.Unit
	if err = validateSceneState(data); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{data: data}, nil
}

func closureBinding(narrative content.NarrativeID) (content.DeedID, content.AnchorID) {
	switch narrative {
	case "NAR-05":
		return "D-HOS-END", "ANC-HOS-OUT"
	case "NAR-06":
		return "D-CHU-END", "ANC-CHU-OUT"
	case "NAR-07":
		return "D-MIN-END", "ANC-MIN-OUT"
	case "NAR-09":
		return "D-INS-END", "ANC-INS-OUT"
	default:
		return "", ""
	}
}

func (campaign *Campaign) prepareClosure(ctx context.Context, snapshot Snapshot, candidate ClosureCandidate) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	deed, ok := campaign.deeds[candidate.Deed]
	if !ok || deed.Family != "END" || len(candidate.SceneUnit) > 160 || !sceneUnitPattern.MatchString(candidate.SceneUnit) || candidate.HoldMilliseconds < deed.Deed.HoldMilliseconds || len(deed.Deed.Objects) != 1 || candidate.Terminal != deed.Deed.Objects[0] {
		return Snapshot{}, fmt.Errorf("%w: closure interaction", ErrInvalid)
	}
	if _, active := snapshot.ActiveAttempt(); active {
		return Snapshot{}, fmt.Errorf("%w: active ticket", ErrBlocked)
	}
	accepted, err := campaign.Accept(ctx, snapshot, candidate.Deed)
	if err != nil {
		return Snapshot{}, err
	}
	attempt, ok := accepted.ActiveAttempt()
	if !ok {
		return Snapshot{}, fmt.Errorf("%w: missing closure attempt", ErrInvalid)
	}
	event := Certification{Deed: candidate.Deed, Attempt: attempt.Number, Evidence: deed.Evidence, Trigger: deed.Trigger, Terminal: candidate.Terminal, HoldMilliseconds: candidate.HoldMilliseconds}
	completed, _, err := campaign.Apply(ctx, accepted, event)
	if err != nil {
		return Snapshot{}, err
	}
	data := completed.Data(ctx)
	for _, id := range []content.NarrativeID{"NAR-05", "NAR-06", "NAR-07", "NAR-09"} {
		if binding, _ := closureBinding(id); binding == candidate.Deed {
			data.SceneEntry, data.SceneUnit = id, candidate.SceneUnit
			return Snapshot{data: data}, nil
		}
	}
	return Snapshot{}, fmt.Errorf("%w: closure narrative", ErrInvalid)
}

// prepareStory binds a named milestone to the same retry identity and result
// digest as other durable operations. Already completed milestones are no-ops.
func (campaign *Campaign) prepareStory(ctx context.Context, snapshot Snapshot, transition Transition, digest string) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	change := transition.Story
	if snapshot.data.SceneEntry != "" && change.Action != StorySceneUnit && change.Action != StoryNarrative && change.Action != StorySceneFinish {
		return Snapshot{}, fmt.Errorf("%w: unfinished narrative scene", ErrBlocked)
	}
	var proposed Snapshot
	if change.Action != StorySceneEnter && change.Action != StorySceneUnit && change.Unit != "" {
		return Snapshot{}, fmt.Errorf("%w: unexpected scene unit", ErrInvalid)
	}
	switch change.Action {
	case StoryTheft:
		if change.Narrative != "" || change.Stage != "" || snapshot.data.Consent || len(snapshot.data.Narratives) != 0 || snapshot.data.Active != "" || snapshot.data.DonationTheft {
			return Snapshot{}, fmt.Errorf("%w: donation theft unavailable", ErrBlocked)
		}
		data := snapshot.Data(ctx)
		data.DonationTheft, data.Anchor = true, "ANC-PRO-02"
		proposed = Snapshot{data: data}
	case StorySceneEnter, StorySceneUnit:
		proposed, err = campaign.prepareSceneEntry(ctx, snapshot, *change)
	case StorySceneFinish:
		deed, _ := closureBinding(change.Narrative)
		if deed == "" || change.Stage != "" || snapshot.data.SceneEntry != change.Narrative {
			return Snapshot{}, fmt.Errorf("%w: closure scene finish", ErrBlocked)
		}
		data := snapshot.Data(ctx)
		data.SceneEntry, data.SceneUnit = "", ""
		proposed = Snapshot{data: data}
	case StoryNarrative:
		if snapshot.data.SceneEntry != "" && snapshot.data.SceneEntry != change.Narrative {
			return Snapshot{}, fmt.Errorf("%w: different narrative is active", ErrBlocked)
		}
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

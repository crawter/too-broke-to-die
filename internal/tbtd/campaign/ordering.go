// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"

	"too-broke-to-die/internal/tbtd/content"
)

// OperationRecord binds the latest committed sequence to its exact request.
// Persistence must keep this record with the complete proposed snapshot.
type OperationRecord struct {
	Sequence uint64 `json:"sequence"`

	Digest string `json:"digest"`

	ResultDigest string `json:"result_digest"`
}

// CertificationCandidate carries evidence only when the action completed.
type CertificationCandidate struct {
	Completed bool `json:"completed"`

	Event Certification `json:"event"`
}

// EncounterCandidate identifies a completed or unfinished boss action.
type EncounterCandidate struct {
	Completed bool `json:"completed"`

	Encounter content.EncounterID `json:"encounter"`
}

// AttemptAction identifies an explicit attempt transition.
type AttemptAction string

const (
	// AttemptAccept starts an attempt for an available ticket.
	AttemptAccept AttemptAction = "accept"

	// AttemptReset explicitly starts a new attempt for the active ticket.
	AttemptReset AttemptAction = "reset"

	// AttemptAbandon clears the active ticket after the player confirms the warning.
	AttemptAbandon AttemptAction = "abandon"
)

// AttemptChange names the ticket affected by an identified operation.
type AttemptChange struct {
	Action AttemptAction `json:"action"`

	Deed content.DeedID `json:"deed"`
}

// EncounterEvent preserves the occurrence assigned to a boss victory.
type EncounterEvent struct {
	ID OccurrenceID `json:"id"`

	Encounter content.EncounterID `json:"encounter"`
}

// CriticalItemID identifies an explicitly supplied unique campaign item.
type CriticalItemID string

// CriticalItemDefinition binds an authored item to safe anchors and the
// existing route object changed when it is consumed. New supplies no invented
// items; the composition root supplies the authored definitions when available.
type CriticalItemDefinition struct {
	ID CriticalItemID

	AcquireAnchor content.AnchorID

	ConsumeAnchor content.AnchorID

	Route content.ObjectID
}

// CriticalItemAction identifies a unique-item transition.
type CriticalItemAction string

const (
	// CriticalAcquire records ownership of a unique item.
	CriticalAcquire CriticalItemAction = "acquire"

	// CriticalConsume consumes an owned item and changes its bound route.
	CriticalConsume CriticalItemAction = "consume"
)

// CriticalItemCandidate is supplied by simulation only when an action completes.
type CriticalItemCandidate struct {
	Item CriticalItemID `json:"item"`

	Action CriticalItemAction `json:"action"`

	Completed bool `json:"completed"`
}

// CriticalItemRecord retains both occurrence identities; consumption is final.
type CriticalItemRecord struct {
	Item CriticalItemID `json:"item"`

	Acquired OccurrenceID `json:"acquired"`

	Consumed OccurrenceID `json:"consumed"`
}

// DeathCause distinguishes gameplay lethality from non-chargeable returns.
type DeathCause string

const (
	// DeathNone denotes a transition without death or checkpoint return.
	DeathNone DeathCause = ""

	// DeathGameplay records one chargeable temporary gameplay death.
	DeathGameplay DeathCause = "gameplay"

	// DeathHeartAttack is the uncharged scripted prologue heart attack.
	DeathHeartAttack DeathCause = "heart_attack"

	// DeathFinal is the uncharged authorized final death.
	DeathFinal DeathCause = "authorized_final"

	// DeathCheckpoint is an uncharged voluntary checkpoint return.
	DeathCheckpoint DeathCause = "checkpoint_return"

	// DeathRestoration is an uncharged retry or Continue.
	DeathRestoration DeathCause = "restoration"

	// DeathUnclean is an interruption, not a chargeable gameplay death.
	DeathUnclean DeathCause = "unclean_termination"
)

// Transition is one immutable request. Sequence starts at one and is assigned
// only once; retries must reuse this request and its campaign identity.
// Contacts and failure are evaluated against the attempt active at the start
// of the step, even when certification completes in the same step.
type Transition struct {
	CampaignID string `json:"campaign_id"`

	Sequence uint64 `json:"sequence"`

	Attempt *AttemptChange `json:"attempt,omitempty"`

	Certification *CertificationCandidate `json:"certification,omitempty"`

	CriticalItems []CriticalItemCandidate `json:"critical_items,omitempty"`

	Encounter *EncounterCandidate `json:"encounter,omitempty"`

	Aftermath content.EmergencyID `json:"aftermath,omitempty"`

	Contacts []Contact `json:"contacts,omitempty"`

	Failure bool `json:"failure,omitempty"`

	Death DeathCause `json:"death,omitempty"`

	DeathDetail string `json:"death_detail,omitempty"`
}

// Prepare applies completed events, their consequences, and finally one death
// to a private copy. It does not acknowledge persistence, run emergency clocks,
// or publish the proposal. Reapplying the latest identical committed request
// returns the existing snapshot; older or conflicting requests are rejected.
func (campaign *Campaign) Prepare(ctx context.Context, snapshot Snapshot, transition Transition) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	if !campaignIDPattern.MatchString(transition.CampaignID) || transition.Sequence == 0 || (snapshot.data.CampaignID != "" && snapshot.data.CampaignID != transition.CampaignID) {
		return Snapshot{}, fmt.Errorf("%w: campaign or operation identity", ErrInvalid)
	}
	encoded, err := json.Marshal(transition)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: encode operation: %w", ErrInvalid, err)
	}
	sum := sha256.Sum256(encoded)
	digest := hex.EncodeToString(sum[:])
	if transition.Sequence == snapshot.data.OperationSequence && snapshot.data.LatestOperation != nil {
		resultDigest, digestErr := digestState(ctx, snapshot.data)
		if digestErr != nil {
			return Snapshot{}, digestErr
		}
		if snapshot.data.LatestOperation.Digest != digest || snapshot.data.LatestOperation.ResultDigest != resultDigest {
			return Snapshot{}, fmt.Errorf("%w: operation %d", ErrConflict, transition.Sequence)
		}
		return Snapshot{data: snapshot.Data(ctx)}, nil
	}
	if snapshot.data.OperationSequence == math.MaxUint64 || transition.Sequence != snapshot.data.OperationSequence+1 {
		return Snapshot{}, fmt.Errorf("%w: stale or nonconsecutive operation", ErrConflict)
	}
	if transition.Failure && transition.Certification != nil && transition.Certification.Completed {
		return Snapshot{}, fmt.Errorf("%w: completed and failed attempt", ErrInvalid)
	}
	switch transition.Death {
	case DeathNone, DeathHeartAttack, DeathFinal, DeathCheckpoint, DeathRestoration, DeathUnclean:
		if transition.DeathDetail != "" {
			return Snapshot{}, fmt.Errorf("%w: non-gameplay death detail", ErrInvalid)
		}
	case DeathGameplay:
		if strings.TrimSpace(transition.DeathDetail) == "" {
			return Snapshot{}, fmt.Errorf("%w: missing death cause", ErrInvalid)
		}
	default:
		return Snapshot{}, fmt.Errorf("%w: death cause", ErrInvalid)
	}
	active, _ := snapshot.activeAttempt(ctx)
	proposed := Snapshot{data: snapshot.Data(ctx)}
	if transition.Attempt != nil {
		if transition.Certification != nil || len(transition.CriticalItems) != 0 || transition.Encounter != nil || transition.Aftermath != "" || len(transition.Contacts) != 0 || transition.Failure || transition.Death != DeathNone {
			return Snapshot{}, fmt.Errorf("%w: attempt change must be a separate operation", ErrInvalid)
		}
		change := transition.Attempt
		switch change.Action {
		case AttemptAccept:
			proposed, err = campaign.Accept(ctx, proposed, change.Deed)
		case AttemptReset, AttemptAbandon:
			if change.Deed == "" || change.Deed != snapshot.data.Active {
				return Snapshot{}, fmt.Errorf("%w: wrong active ticket", ErrBlocked)
			}
			if change.Action == AttemptReset {
				proposed, err = campaign.Reset(ctx, proposed)
			} else {
				proposed, err = campaign.Abandon(ctx, proposed)
			}
		default:
			return Snapshot{}, fmt.Errorf("%w: attempt action", ErrInvalid)
		}
		if err != nil {
			return Snapshot{}, err
		}
	}
	if transition.Certification != nil && transition.Certification.Completed {
		proposed, _, err = campaign.Apply(ctx, proposed, transition.Certification.Event)
		if err != nil {
			return Snapshot{}, err
		}
	}
	if transition.Encounter != nil && transition.Encounter.Completed {
		proposed, err = campaign.DefeatBoss(ctx, proposed, transition.Encounter.Encounter)
		if err != nil {
			return Snapshot{}, err
		}
	}
	data := proposed.Data(ctx)
	if uint64(len(transition.CriticalItems))+uint64(len(transition.Contacts)) > uint64(math.MaxUint32)-3 {
		return Snapshot{}, fmt.Errorf("%w: too many candidates", ErrInvalid)
	}
	for i, candidate := range transition.CriticalItems {
		id := OccurrenceID{CampaignID: transition.CampaignID, Sequence: transition.Sequence, Ordinal: uint32(i + 1)}
		if err = campaign.applyCriticalItem(ctx, &data, candidate, id); err != nil {
			return Snapshot{}, err
		}
	}
	if transition.Aftermath != "" {
		if err = advanceEmergency(ctx, &data, transition.Aftermath); err != nil {
			return Snapshot{}, err
		}
	}
	// Ordinals follow candidate order, including suppressed contacts, so
	// retries cannot renumber any occurrence.
	offset := uint32(len(transition.CriticalItems))
	if transition.Encounter != nil && transition.Encounter.Completed {
		offset++
		if !has(snapshot.data.Defeated, transition.Encounter.Encounter) {
			data.EncounterEvents = append(data.EncounterEvents, EncounterEvent{ID: OccurrenceID{CampaignID: transition.CampaignID, Sequence: transition.Sequence, Ordinal: offset}, Encounter: transition.Encounter.Encounter})
		}
	}
	for i, contact := range transition.Contacts {
		id := OccurrenceID{CampaignID: transition.CampaignID, Sequence: transition.Sequence, Ordinal: uint32(i+1) + offset}
		if err = addContact(&data, active, contact, id); err != nil {
			return Snapshot{}, err
		}
	}
	ordinal := uint32(len(transition.Contacts)) + offset + 1
	if transition.Failure {
		if err = campaign.failAttempt(ctx, &data, OccurrenceID{CampaignID: transition.CampaignID, Sequence: transition.Sequence, Ordinal: ordinal}); err != nil {
			return Snapshot{}, err
		}
	}
	if transition.Death == DeathGameplay {
		data.Incidents = append(data.Incidents, Incident{ID: OccurrenceID{CampaignID: transition.CampaignID, Sequence: transition.Sequence, Ordinal: ordinal + 1}, Kind: IncidentDeath, Deed: active.Deed, Attempt: active.Number, Detail: transition.DeathDetail, Cents: 4999})
	}
	data.CampaignID = transition.CampaignID
	data.OperationSequence = transition.Sequence
	resultDigest, err := digestState(ctx, data)
	if err != nil {
		return Snapshot{}, err
	}
	data.LatestOperation = &OperationRecord{Sequence: transition.Sequence, Digest: digest, ResultDigest: resultDigest}
	proposed = Snapshot{data: data}
	if err = campaign.Validate(ctx, proposed); err != nil {
		return Snapshot{}, err
	}
	return proposed, nil
}

var criticalItemIDPattern = regexp.MustCompile(`^ITEM-[A-Z0-9]+(?:-[A-Z0-9]+)*$`)

// NewWithInventory adds an explicitly authored unique-item catalogue to the
// campaign. It does not turn service objects into items or grant any credit.
// Definitions must be supplied again when restoring a saved campaign.
func NewWithInventory(ctx context.Context, manifest *content.Campaign, definitions []CriticalItemDefinition) (_ *Campaign, err error) {
	defer mon.Task()(&ctx)(&err)
	campaign, err := New(ctx, manifest)
	if err != nil {
		return nil, err
	}
	campaign.items = make(map[CriticalItemID]CriticalItemDefinition, len(definitions))
	routes := make(map[content.ObjectID]bool, len(definitions))
	for _, definition := range definitions {
		object, ok := campaign.objects[definition.Route]
		if len(definition.ID) > 160 || !criticalItemIDPattern.MatchString(string(definition.ID)) || campaign.items[definition.ID].ID != "" || routes[definition.Route] || !ok || (object.Role != content.RoleGate && object.Role != content.RoleSwitch && object.Role != content.RoleLatch) || !campaign.validItemAnchor(ctx, definition.AcquireAnchor, "") || !campaign.validItemAnchor(ctx, definition.ConsumeAnchor, object.Encounter) {
			return nil, fmt.Errorf("%w: critical item definition %s", ErrInvalid, definition.ID)
		}
		campaign.items[definition.ID] = definition
		routes[definition.Route] = true
	}
	return campaign, nil
}

func (campaign *Campaign) validItemAnchor(ctx context.Context, anchor content.AnchorID, encounter content.EncounterID) bool {
	defer mon.Task()(&ctx)(nil)
	for _, deed := range campaign.ordered {
		if deed.Deed.Anchor == anchor && (encounter == "" || deed.Deed.Encounter == encounter) {
			return true
		}
	}
	return false
}

func (campaign *Campaign) itemAnchorReachable(ctx context.Context, data State, anchor content.AnchorID) bool {
	defer mon.Task()(&ctx)(nil)
	for _, deed := range campaign.ordered {
		if deed.Deed.Anchor == anchor && len(missingDeed(deed.Deed, data)) == 0 {
			return true
		}
	}
	return false
}

// ItemRoutes returns route objects changed by consumed items, derived from the
// validated inventory rather than a separate mutable route balance.
func (campaign *Campaign) ItemRoutes(ctx context.Context, snapshot Snapshot) (_ []content.ObjectID, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return nil, err
	}
	var routes []content.ObjectID
	for _, record := range snapshot.data.CriticalItems {
		if record.Consumed.Sequence != 0 {
			routes = append(routes, campaign.items[record.Item].Route)
		}
	}
	return routes, nil
}

// applyCriticalItem is the per-candidate helper measured by Prepare.
func (campaign *Campaign) applyCriticalItem(ctx context.Context, data *State, candidate CriticalItemCandidate, id OccurrenceID) error {
	definition, ok := campaign.items[candidate.Item]
	if !ok || (candidate.Action != CriticalAcquire && candidate.Action != CriticalConsume) {
		return fmt.Errorf("%w: critical item candidate", ErrInvalid)
	}
	if !candidate.Completed {
		return nil
	}
	index := -1
	for i, record := range data.CriticalItems {
		if record.Item == candidate.Item {
			index = i
			break
		}
	}
	switch candidate.Action {
	case CriticalAcquire:
		if index >= 0 {
			return fmt.Errorf("%w: item already acquired", ErrBlocked)
		}
		if !campaign.itemAnchorReachable(ctx, *data, definition.AcquireAnchor) {
			return fmt.Errorf("%w: item acquisition anchor", ErrBlocked)
		}
		data.CriticalItems = append(data.CriticalItems, CriticalItemRecord{Item: candidate.Item, Acquired: id})
		data.Anchor = definition.AcquireAnchor
	case CriticalConsume:
		if index < 0 || data.CriticalItems[index].Consumed.Sequence != 0 {
			return fmt.Errorf("%w: item not owned", ErrBlocked)
		}
		if !campaign.itemAnchorReachable(ctx, *data, definition.ConsumeAnchor) {
			return fmt.Errorf("%w: item consumption anchor", ErrBlocked)
		}
		data.CriticalItems[index].Consumed = id
		data.Anchor = definition.ConsumeAnchor
	}
	return nil
}

func digestState(ctx context.Context, data State) (_ string, err error) {
	defer mon.Task()(&ctx)(&err)
	data.LatestOperation = nil
	// Restoration clears failed work without changing the durable failure entry.
	data.Attempts = slices.Clone(data.Attempts)
	for i := range data.Attempts {
		data.Attempts[i].Failed = ""
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("%w: encode operation result: %w", ErrInvalid, err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/session"
)

func randomCampaignID(ctx context.Context) (_ session.CampaignID, err error) {
	defer mon.Task()(&ctx)(&err)
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return session.CampaignID(fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])), nil
}

func (store *Store) active(ctx context.Context) (_ session.CampaignID, err error) {
	defer mon.Task()(&ctx)(&err)
	intent, err := store.readSelectorIntent(ctx)
	if err != nil {
		return "", err
	}
	if intent.Pending {
		return "", errSelectorUnresolved
	}
	slots := store.selectors(ctx)
	best, err := newestSelector(slots)
	if err != nil {
		return "", err
	}
	return slots[best].value.Campaign, nil
}

func asProgress(input session.ProgressInput, result CommitResult) session.ProgressResultData {
	output := session.ProgressResultData{ID: input.ID, Campaign: input.Campaign, Operation: input.Operation, Status: result.Status, Code: result.Code, PayloadDigest: result.PayloadDigest, Revision: result.CurrentRevision, RevisionKnown: result.CurrentKnown}
	if result.Status == session.StorageSucceeded {
		output.Campaign = session.CampaignID(result.Revision.CampaignID)
		output.Revision, output.RevisionKnown = result.Revision.Number, true
		output.Snapshot = result.Revision.Snapshot
	}
	return output
}

func (store *Store) processProgress(ctx context.Context, input session.ProgressInput) (result session.ProgressResultData) {
	defer mon.Task()(&ctx)(nil)
	result = session.ProgressResultData{ID: input.ID, Campaign: input.Campaign, Operation: input.Operation}
	if err := ctx.Err(); err != nil {
		result.Status, result.Code = session.StorageFailed, session.StorageDenied
		return result
	}
	if store.selectorPending != nil && input.Kind != session.LoadActive {
		result.Status, result.Code = session.StorageAmbiguous, session.StorageDenied
		return result
	}
	// After NotInstalled, LoadActive is the typed contract's confirmed reload
	// choice. Recheck the slots before discarding; a newly discovered installed
	// result is returned instead of claiming the pending event was lost.
	if store.pending != nil && store.pendingResolved && input.Kind == session.LoadActive {
		resolved := store.discardProgress(ctx, *store.pending)
		if resolved.Status != session.StorageNotInstalled {
			resolved.ID = input.ID
			return resolved
		}
	}
	if store.pending != nil && (input.Kind != session.ResolveProgress && input.Kind != session.CommitProgress || !samePending(ctx, *store.pending, input)) {
		result.Status, result.Code = session.StorageConflicting, session.StorageConflict
		return result
	}
	var err error
	switch input.Kind {
	case session.InspectActive:
		var id session.CampaignID
		id, err = store.active(ctx)
		if err == nil {
			result, err = store.inspectCampaign(ctx, id)
		}
	case session.LoadActive:
		if store.selectorPending != nil {
			err = store.finishSelector(ctx, store.selectorPending)
			if err != nil {
				result.Status = session.StorageAmbiguous
				break
			}
			store.selectorPending = nil
		}
		var id session.CampaignID
		id, err = store.active(ctx)
		if err != nil {
			result.Candidates, err = store.discoverForSelector(ctx, err)
			break
		}
		result, err = store.inspectCampaign(ctx, id)
	case session.LoadCampaign:
		var id session.CampaignID
		id, err = store.active(ctx)
		if err == nil && id != input.Campaign {
			err = session.StorageConflict
		}
		if err == nil {
			result, err = store.inspectCampaign(ctx, id)
		}
	case session.LoadRevision:
		var active session.CampaignID
		active, err = store.active(ctx)
		if errors.Is(err, session.SaveCorrupt) || errors.Is(err, errSelectorUnresolved) {
			// Choosing a specific recovery candidate is also an explicit campaign
			// selection when the active selector is unavailable.
			err = nil
		}
		var revisions *Revisions
		if err == nil {
			revisions, err = store.revisionsFor(ctx, input.Campaign)
		}
		if err == nil {
			var revision Revision
			revision, err = revisions.AcceptRecovery(ctx, input.ExpectedRevision)
			if err == nil && active != input.Campaign {
				var status session.StorageStatus
				status, err = store.selectActive(ctx, input.Campaign)
				if err != nil {
					result = session.ProgressResultData{Campaign: input.Campaign, Status: status}
				}
			}
			if err == nil {
				result = asProgress(input, confirmed(revision))
			}
		}
	case session.CreateCampaign:
		result, err = store.createCampaign(ctx, input)
	case session.SelectCampaign:
		result, err = store.inspectCampaign(ctx, input.Campaign)
		if err == nil {
			var status session.StorageStatus
			status, err = store.selectActive(ctx, input.Campaign)
			if err != nil {
				result = session.ProgressResultData{Campaign: input.Campaign, Status: status}
			}
		}
	case session.CommitProgress, session.ResolveProgress:
		var id session.CampaignID
		id, err = store.active(ctx)
		if err == nil && id != input.Campaign {
			err = session.StorageConflict
		}
		if err == nil {
			var revisions *Revisions
			revisions, err = store.revisionsFor(ctx, id)
			if err == nil {
				var commit CommitResult
				if input.Kind == session.CommitProgress {
					commit, err = revisions.Commit(ctx, input.ExpectedRevision, input.ExpectedDigest, input.Operation, input.Proposal)
				} else {
					commit, err = store.resolveProgress(ctx, revisions, input)
				}
				result = asProgress(input, commit)
				if commit.Status == session.StorageAmbiguous || commit.Status == session.StorageNotInstalled {
					copy := input
					store.pending = &copy
					store.pendingResolved = commit.Status == session.StorageNotInstalled
				} else if commit.Status == session.StorageSucceeded {
					store.pending = nil
					store.pendingResolved = false
				}
			}
		}
	default:
		err = session.InvalidAction
	}
	result.ID = input.ID
	if errors.Is(err, errSelectorUnresolved) {
		result.Status, result.Code = session.StorageAmbiguous, session.StorageConflict
	} else if err != nil && result.Status == "" {
		failure := resultForError(err)
		result.Status, result.Code = failure.Status, failure.Code
	} else if err != nil && result.Code == "" {
		result.Code = resultForError(err).Code
	}
	return result
}

func (store *Store) discoverForSelector(ctx context.Context, selectorErr error) (_ []session.RecoveryCandidate, err error) {
	defer mon.Task()(&ctx)(&err)
	if !errors.Is(selectorErr, errSelectorUnresolved) && resultForError(selectorErr).Status != session.StorageCorrupt {
		return nil, selectorErr
	}
	candidates, err := store.discover(ctx)
	if err != nil {
		return nil, err
	}
	return candidates, selectorErr
}

func (store *Store) createCampaign(ctx context.Context, input session.ProgressInput) (_ session.ProgressResultData, err error) {
	defer mon.Task()(&ctx)(&err)
	// New Game never accepts caller-provided progress or an identity to reuse.
	id, err := randomCampaignID(ctx)
	if err != nil {
		return session.ProgressResultData{}, err
	}
	exists, err := store.campaignExists(ctx, id)
	if err != nil {
		return session.ProgressResultData{}, err
	}
	if exists {
		return session.ProgressResultData{}, session.StorageConflict
	}
	revisions, err := store.revisionsFor(ctx, id)
	if err != nil {
		return session.ProgressResultData{}, err
	}
	initial := Revision{CampaignID: string(id), Compatibility: store.codec.current, Snapshot: campaign.Snapshot{}, Metadata: Metadata{LastWriteUTC: time.Now().UTC(), BuildDescription: "new campaign"}}
	created, err := revisions.Initialize(ctx, initial)
	if err != nil {
		return asProgress(input, created), err
	}
	status, err := store.selectActive(ctx, id)
	if err != nil {
		return session.ProgressResultData{Campaign: id, Status: status}, err
	}
	return asProgress(input, created), nil
}

func (store *Store) discardProgress(ctx context.Context, input session.ProgressInput) (result session.ProgressResultData) {
	defer mon.Task()(&ctx)(nil)
	result = session.ProgressResultData{ID: input.ID, Campaign: input.Campaign, Operation: input.Operation}
	if store.pending == nil || !samePending(ctx, *store.pending, input) {
		result.Status, result.Code = session.StorageConflicting, session.StorageConflict
		return result
	}
	id, err := store.active(ctx)
	if err == nil && id != input.Campaign {
		err = session.StorageConflict
	}
	var revisions *Revisions
	if err == nil {
		revisions, err = store.revisionsFor(ctx, id)
	}
	var resolved CommitResult
	if err == nil {
		var inspection Inspection
		inspection, err = revisions.Inspect(ctx)
		if err == nil && inspection.Pending {
			resolved, err = revisions.Discard(ctx)
		} else if err == nil {
			resolved, err = store.resolveProgress(ctx, revisions, input)
		}
	}
	if err != nil && resolved.Status == "" {
		resolved = resultForError(err)
	}
	if resolved.Status == session.StorageSucceeded || resolved.Status == session.StorageNotInstalled {
		store.pending = nil
		store.pendingResolved = false
	} else if resolved.Status == session.StorageAmbiguous {
		store.pendingResolved = false
	}
	return asProgress(input, resolved)
}

func samePending(ctx context.Context, original, retry session.ProgressInput) bool {
	defer mon.Task()(&ctx)(nil)
	return original.Campaign == retry.Campaign && original.ExpectedRevision == retry.ExpectedRevision && original.ExpectedDigest == retry.ExpectedDigest && original.Operation == retry.Operation && reflect.DeepEqual(original.Proposal.Data(ctx), retry.Proposal.Data(ctx))
}

// resolveProgress never installs a proposal while resolving. After restart it
// checks the independently validated slots and re-encodes the original payload.
func (store *Store) resolveProgress(ctx context.Context, revisions *Revisions, input session.ProgressInput) (_ CommitResult, err error) {
	defer mon.Task()(&ctx)(&err)
	inspection, err := revisions.Inspect(ctx)
	if err != nil {
		return resultForError(err), err
	}
	if inspection.Pending {
		return revisions.Resolve(ctx, false)
	}
	selected, err := revisions.selectSlot(ctx, inspection)
	if err != nil {
		return resultForError(err), err
	}
	base := inspection.Slots[selected].Revision
	if input.ExpectedRevision != math.MaxUint64 && base.Number == input.ExpectedRevision+1 && base.Operation == input.Operation {
		return revisions.Commit(ctx, input.ExpectedRevision, input.ExpectedDigest, input.Operation, input.Proposal)
	}
	if base.Number != input.ExpectedRevision || base.PayloadDigest != input.ExpectedDigest || input.Operation.Sequence != base.Operation.Sequence+1 || !extendsHistory(ctx, base.Snapshot, input.Proposal) {
		return conflictAt(base), session.StorageConflict
	}
	candidate := base
	candidate.Number++
	candidate.Snapshot = input.Proposal
	candidate.Operation = input.Operation
	candidate.Compatibility = store.codec.current
	candidate.Operations = append(slices.Clone(base.Operations), input.Operation)
	_, checked, err := store.codec.Encode(ctx, candidate)
	if err != nil {
		return resultForError(err), err
	}
	return CommitResult{Status: session.StorageNotInstalled, PayloadDigest: checked.PayloadDigest}, nil
}

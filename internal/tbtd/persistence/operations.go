// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/session"
)

type preparedCommit struct {
	expected       uint64
	expectedDigest string
	previous       Revision
	oldTarget      Slot
	target         string
	data           []byte
	revision       Revision
}

func errConflict() error { return session.StorageConflict }

func confirmed(revision Revision) CommitResult {
	return CommitResult{Status: session.StorageSucceeded, Revision: revision, PayloadDigest: revision.PayloadDigest, CurrentRevision: revision.Number, CurrentKnown: true}
}

func conflictAt(revision Revision) CommitResult {
	return CommitResult{Status: session.StorageConflicting, Code: session.StorageConflict, CurrentRevision: revision.Number, CurrentKnown: true}
}

// Commit accepts a proposal and the exact predecessor payload digest captured
// when it was prepared. Retries must retain that digest, not look up a newer
// predecessor. Until resolution or explicit discard, an unsuccessful operation
// prevents another operation from overtaking it. Metadata and guidance are
// inherited from the selected revision, making the payload stable across retries.
func (store *Revisions) Commit(ctx context.Context, expected uint64, expectedDigest string, operation session.OperationID, proposal campaign.Snapshot) (_ CommitResult, err error) {
	defer mon.Task()(&ctx)(&err)
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.pending != nil {
		pending := store.pending
		if expected != pending.expected || expectedDigest != pending.expectedDigest || operation != pending.revision.Operation {
			err = fmt.Errorf("%w: another operation is unresolved", errConflict())
			return resultForError(err), err
		}
		candidate := pending.revision
		candidate.Compatibility = store.codec.current
		candidate.Snapshot = proposal
		_, checked, encodeErr := store.codec.Encode(ctx, candidate)
		if encodeErr != nil && ctx.Err() != nil {
			return CommitResult{Status: session.StorageAmbiguous, Code: storageCode(encodeErr), PayloadDigest: pending.revision.PayloadDigest}, encodeErr
		}
		if encodeErr != nil || checked.PayloadDigest != pending.revision.PayloadDigest {
			err = fmt.Errorf("%w: changed pending payload", errConflict())
			return resultForError(err), err
		}
		return store.resolve(ctx, true)
	}
	inspection := store.inspect(ctx)
	selected, err := store.selectSlot(ctx, inspection)
	if err != nil {
		return resultForError(err), err
	}
	base := inspection.Slots[selected].Revision
	if expected == math.MaxUint64 || !digestPattern.MatchString(expectedDigest) || !validText(operation.Event) || operation.Sequence == 0 {
		err = fmt.Errorf("%w: invalid operation", errConflict())
		return conflictAt(base), err
	}
	state := proposal.Data(ctx)
	if state.CampaignID != store.id || state.OperationSequence != operation.Sequence || state.LatestOperation == nil || state.LatestOperation.Sequence != operation.Sequence {
		err = fmt.Errorf("%w: proposal identity", errConflict())
		return conflictAt(base), err
	}
	// A lost acknowledgment can be retried after restarting the store. Build
	// the same payload from the installed metadata and compare its exact digest.
	if base.Number == expected+1 && base.Operation == operation {
		predecessor := inspection.Slots[1-selected]
		if predecessor.Err == nil {
			if predecessor.Revision.Number != expected || predecessor.Revision.PayloadDigest != expectedDigest {
				err = fmt.Errorf("%w: predecessor digest mismatch", errConflict())
				return conflictAt(base), err
			}
		} else {
			// Explicit recovery can select the independently valid installed
			// result when the predecessor is unreadable. Its digest is then
			// unknown, not verified; only an identical installed operation and
			// payload may be acknowledged, never a new transition.
			recovered := store.recovery != nil && store.recovery.number == base.Number && store.recovery.digest == base.PayloadDigest
			if !recovered || (!errors.Is(predecessor.Err, session.SaveCorrupt) && !errors.Is(predecessor.Err, os.ErrNotExist)) {
				err = fmt.Errorf("%w: predecessor unavailable", errConflict())
				return conflictAt(base), err
			}
		}
		candidate := base
		candidate.Compatibility = store.codec.current
		candidate.Snapshot = proposal
		_, checked, encodeErr := store.codec.Encode(ctx, candidate)
		if encodeErr != nil && ctx.Err() != nil {
			return resultForError(encodeErr), encodeErr
		}
		if encodeErr != nil || checked.PayloadDigest != base.PayloadDigest {
			err = fmt.Errorf("%w: changed committed payload", errConflict())
			return conflictAt(base), err
		}
		if err = store.confirm(ctx, inspection.Slots[selected].Name, base); err != nil {
			store.pending = &preparedCommit{expected: expected, expectedDigest: expectedDigest, previous: inspection.Slots[1-selected].Revision, target: inspection.Slots[selected].Name, data: inspection.Slots[selected].data, revision: base}
			return CommitResult{Status: session.StorageAmbiguous, Code: storageCode(err), PayloadDigest: base.PayloadDigest}, err
		}
		return confirmed(base), nil
	}
	if base.Number != expected || base.PayloadDigest != expectedDigest || base.Operation.Sequence == math.MaxUint64 || operation.Sequence != base.Operation.Sequence+1 {
		err = fmt.Errorf("%w: stale revision or operation", errConflict())
		return conflictAt(base), err
	}
	if !extendsHistory(ctx, base.Snapshot, proposal) {
		err = fmt.Errorf("%w: proposal discards committed history", errConflict())
		return conflictAt(base), err
	}
	candidate := base
	candidate.Number = expected + 1
	candidate.Compatibility = store.codec.current
	candidate.Snapshot = proposal
	candidate.Operation = operation
	candidate.Operations = append(slices.Clone(base.Operations), operation)
	data, checked, err := store.codec.Encode(ctx, candidate)
	if err != nil {
		return resultForError(err), err
	}
	target := 1 - selected
	if inspection.Slots[target].Err != nil {
		if err = store.preserve(ctx, inspection.Slots[target]); err != nil {
			return resultForError(err), err
		}
	}
	store.pending = &preparedCommit{expected: expected, expectedDigest: expectedDigest, previous: base, oldTarget: inspection.Slots[target], target: inspection.Slots[target].Name, data: data, revision: checked}
	return store.writePending(ctx)
}

func (store *Revisions) writePending(ctx context.Context) (_ CommitResult, err error) {
	defer mon.Task()(&ctx)(&err)
	pending := store.pending
	if err = store.install(ctx, pending.target, pending.data, pending.revision); err != nil {
		return CommitResult{Status: session.StorageAmbiguous, Code: storageCode(err), PayloadDigest: pending.revision.PayloadDigest}, err
	}
	if store.ack != nil {
		if err = store.ack(); err != nil {
			return CommitResult{Status: session.StorageAmbiguous, Code: storageCode(err), PayloadDigest: pending.revision.PayloadDigest}, err
		}
	}
	store.pending = nil
	store.recovery = nil
	return confirmed(pending.revision), nil
}

// Resolve inspects both slots before retry, reload, or discard. With retry
// false, NotInstalled permits a later explicit Discard or the same Commit.
// A confirmed installed result is flushed and validated again before return.
func (store *Revisions) Resolve(ctx context.Context, retry bool) (_ CommitResult, err error) {
	defer mon.Task()(&ctx)(&err)
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.resolve(ctx, retry)
}

func (store *Revisions) resolve(ctx context.Context, retry bool) (_ CommitResult, err error) {
	defer mon.Task()(&ctx)(&err)
	if store.pending == nil {
		err = fmt.Errorf("%w: no pending operation", errConflict())
		return resultForError(err), err
	}
	pending := store.pending
	inspection := store.inspect(ctx)
	oldFound := false
	installed := ""
	for _, slot := range inspection.Slots {
		if slot.Err != nil {
			if errors.Is(slot.Err, session.SaveTooNew) || errors.Is(slot.Err, session.ContentIncompatible) {
				return resultForError(slot.Err), slot.Err
			}
			if !errors.Is(slot.Err, os.ErrNotExist) && !errors.Is(slot.Err, session.SaveCorrupt) {
				return CommitResult{Status: session.StorageAmbiguous, Code: storageCode(slot.Err), PayloadDigest: pending.revision.PayloadDigest}, slot.Err
			}
			continue
		}
		if sameRevision(slot.Revision, pending.revision) {
			installed = slot.Name
		} else if sameRevision(slot.Revision, pending.previous) {
			oldFound = true
		} else if slot.Name == pending.target && pending.oldTarget.Err == nil && sameRevision(slot.Revision, pending.oldTarget.Revision) {
			// The older slot has not yet been replaced.
		} else {
			err = fmt.Errorf("%w: unexpected installed revision", errConflict())
			return resultForError(err), err
		}
	}
	if installed != "" {
		if err = store.confirm(ctx, installed, pending.revision); err != nil {
			return CommitResult{Status: session.StorageAmbiguous, Code: storageCode(err), PayloadDigest: pending.revision.PayloadDigest}, err
		}
		store.pending = nil
		// Keep the explicit recovery choice when this is the same selected
		// revision and its other slot remains unavailable. A failed flush must
		// not silently revoke the player's recovery choice on retry.
		keepRecovery := store.recovery != nil && store.recovery.number == pending.revision.Number && store.recovery.digest == pending.revision.PayloadDigest
		otherUnavailable := false
		for _, slot := range inspection.Slots {
			if slot.Name != installed && (errors.Is(slot.Err, session.SaveCorrupt) || errors.Is(slot.Err, os.ErrNotExist)) {
				otherUnavailable = true
			}
		}
		if !keepRecovery || !otherUnavailable {
			store.recovery = nil
		}
		return confirmed(pending.revision), nil
	}
	if !oldFound {
		err = fmt.Errorf("%w: neither pending nor previous revision readable", session.SaveCorrupt)
		return CommitResult{Status: session.StorageAmbiguous, Code: session.SaveCorrupt, PayloadDigest: pending.revision.PayloadDigest}, err
	}
	// If the target is corrupt, installation may have occurred and then been
	// damaged. Do not claim discard is safe, but retrying the same bytes is safe
	// after preserving that file and retaining the independently valid old slot.
	for _, slot := range inspection.Slots {
		if slot.Name == pending.target && errors.Is(slot.Err, session.SaveCorrupt) {
			if !retry {
				err = fmt.Errorf("%w: pending slot damaged", session.SaveCorrupt)
				return CommitResult{Status: session.StorageAmbiguous, Code: session.SaveCorrupt, PayloadDigest: pending.revision.PayloadDigest}, err
			}
			if err = store.preserve(ctx, slot); err != nil {
				return CommitResult{Status: session.StorageAmbiguous, Code: storageCode(err), PayloadDigest: pending.revision.PayloadDigest}, err
			}
		}
	}
	if retry {
		return store.writePending(ctx)
	}
	return CommitResult{Status: session.StorageNotInstalled, PayloadDigest: pending.revision.PayloadDigest}, nil
}

// extendsHistory rejects loss of committed collections. The predecessor digest
// separately binds branches whose retained collections happen to be identical;
// neither check replaces campaign semantic validation.
func extendsHistory(ctx context.Context, before, after campaign.Snapshot) bool {
	defer mon.Task()(&ctx)(nil)
	old := before.Data(ctx)
	next := after.Data(ctx)
	if len(next.Receipts) < len(old.Receipts) || !reflect.DeepEqual(old.Receipts, next.Receipts[:len(old.Receipts)]) ||
		len(next.Incidents) < len(old.Incidents) || !slices.Equal(old.Incidents, next.Incidents[:len(old.Incidents)]) ||
		len(next.Narratives) < len(old.Narratives) || !slices.Equal(old.Narratives, next.Narratives[:len(old.Narratives)]) ||
		len(next.Defeated) < len(old.Defeated) || !slices.Equal(old.Defeated, next.Defeated[:len(old.Defeated)]) ||
		len(next.EncounterEvents) < len(old.EncounterEvents) || !slices.Equal(old.EncounterEvents, next.EncounterEvents[:len(old.EncounterEvents)]) ||
		len(next.Consequences) < len(old.Consequences) || !slices.Equal(old.Consequences, next.Consequences[:len(old.Consequences)]) ||
		(old.Permit && !next.Permit) || (old.Consent && !next.Consent) ||
		(old.Completed && !next.Completed) || (old.TableauAcknowledged && !next.TableauAcknowledged) {
		return false
	}
	for _, attempt := range old.Attempts {
		found := false
		for _, current := range next.Attempts {
			if current.Deed == attempt.Deed && current.Number >= attempt.Number {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	for _, emergency := range old.Emergencies {
		found := false
		for _, current := range next.Emergencies {
			if current.ID == emergency.ID && (current.Stage == emergency.Stage || (emergency.Stage == campaign.EmergencyActive && current.Stage == campaign.EmergencyAftermath)) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	for _, item := range old.CriticalItems {
		found := false
		for _, current := range next.CriticalItems {
			if current.Item == item.Item && current.Acquired == item.Acquired && (item.Consumed.Sequence == 0 || current.Consumed == item.Consumed) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Discard clears a pending operation only after a fresh inspection proves that
// the previous revision remains and the proposed revision is not installed.
func (store *Revisions) Discard(ctx context.Context) (_ CommitResult, err error) {
	defer mon.Task()(&ctx)(&err)
	store.mu.Lock()
	defer store.mu.Unlock()
	result, err := store.resolve(ctx, false)
	if err == nil && result.Status == session.StorageNotInstalled {
		store.pending = nil
	}
	return result, err
}

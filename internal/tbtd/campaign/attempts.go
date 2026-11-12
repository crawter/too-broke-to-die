// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"fmt"

	"too-broke-to-die/internal/tbtd/content"
)

// WorkResetWarning describes the rollback scope before checkpoint return or a
// confirmed ticket switch. Recorded history is never discarded.
const WorkResetWarning = "Незачтённый прогресс попытки будет сброшен. Зачтённые дела и штрафы сохраняются"

// ClearWork prepares the checkpoint, retry or Continue rollback. It retains the
// attempt number and its incident history; no death is inferred from a restore.
func (campaign *Campaign) ClearWork(ctx context.Context, snapshot Snapshot) (_ Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = campaign.Validate(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	return campaign.clearWork(ctx, snapshot), nil
}

func (campaign *Campaign) clearWork(ctx context.Context, snapshot Snapshot) Snapshot {
	defer mon.Task()(&ctx)(nil)
	data := snapshot.data
	data.Attempts = append([]Attempt(nil), data.Attempts...)
	for i := range data.Attempts {
		data.Attempts[i].Failed = ""
	}
	return Snapshot{data: data}
}

// failAttempt records the authored failure, leaving the ticket active and retryable.
func (campaign *Campaign) failAttempt(ctx context.Context, data *State, id OccurrenceID) (err error) {
	defer mon.Task()(&ctx)(&err)
	if data.Active == "" {
		return fmt.Errorf("%w: no active attempt", ErrBlocked)
	}
	deed := campaign.deeds[data.Active]
	if deed.Family == "END" {
		return fmt.Errorf("%w: closure cannot fail", ErrInvalid)
	}
	for i := range data.Attempts {
		attempt := &data.Attempts[i]
		if attempt.Deed == data.Active {
			if attempt.Failed != "" {
				return fmt.Errorf("%w: attempt already failed", ErrBlocked)
			}
			attempt.Failed = deed.Failure
			data.Incidents = append(data.Incidents, Incident{ID: id, Kind: IncidentFailure, Deed: attempt.Deed, Attempt: attempt.Number, Detail: deed.Failure})
			return nil
		}
	}
	return fmt.Errorf("%w: missing attempt", ErrInvalid)
}

// ActiveAttempt returns a copy of the current attempt, if one is active.
func (snapshot Snapshot) ActiveAttempt() (Attempt, bool) {
	return snapshot.activeAttempt(context.Background())
}

func (snapshot Snapshot) activeAttempt(ctx context.Context) (Attempt, bool) {
	defer mon.Task()(&ctx)(nil)
	for _, attempt := range snapshot.data.Attempts {
		if attempt.Deed == snapshot.data.Active && attempt.Deed != content.DeedID("") {
			return attempt, true
		}
	}
	return Attempt{}, false
}

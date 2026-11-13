// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"too-broke-to-die/internal/tbtd/session"
)

func (store *Store) revisionsFor(ctx context.Context, id session.CampaignID) (_ *Revisions, err error) {
	defer mon.Task()(&ctx)(&err)
	if revisions := store.revisions[id]; revisions != nil {
		return revisions, nil
	}
	revisions, err := newRevisions(ctx, store.files, store.codec, string(id))
	if err != nil {
		return nil, err
	}
	store.revisions[id] = revisions
	return revisions, nil
}

func (store *Store) candidate(ctx context.Context, revision Revision, unknown bool) (_ session.RecoveryCandidate, err error) {
	defer mon.Task()(&ctx)(&err)
	plan, err := store.codec.campaign.RestorePlan(ctx, revision.Snapshot)
	if err != nil {
		return session.RecoveryCandidate{}, err
	}
	return session.RecoveryCandidate{Campaign: session.CampaignID(revision.CampaignID), Revision: revision.Number, Anchor: plan.Anchor, Credits: revision.Snapshot.Credits(), UnknownDifference: unknown}, nil
}

func (store *Store) inspectCampaign(ctx context.Context, id session.CampaignID) (_ session.ProgressResultData, err error) {
	defer mon.Task()(&ctx)(&err)
	result := session.ProgressResultData{Campaign: id}
	revisions, err := store.revisionsFor(ctx, id)
	if err != nil {
		return result, err
	}
	inspection, err := revisions.Inspect(ctx)
	if err != nil {
		return result, err
	}
	if inspection.Pending {
		return result, session.StorageConflict
	}
	selected, selectErr := revisions.selectSlot(ctx, inspection)
	if selectErr == nil {
		revision := inspection.Slots[selected].Revision
		result.Status = session.StorageSucceeded
		result.Revision, result.RevisionKnown = revision.Number, true
		result.PayloadDigest, result.Snapshot = revision.PayloadDigest, revision.Snapshot
		return result, nil
	}
	// Incompatible slots are not corruption, even if the other slot is valid.
	if errors.Is(selectErr, session.SaveTooNew) || errors.Is(selectErr, session.ContentIncompatible) {
		return result, selectErr
	}
	result.BothRevisionsInvalid = errors.Is(inspection.Slots[0].Err, session.SaveCorrupt) && errors.Is(inspection.Slots[1].Err, session.SaveCorrupt)
	for _, slot := range inspection.Slots {
		if slot.Err == nil {
			candidate, candidateErr := store.candidate(ctx, slot.Revision, true)
			if candidateErr != nil {
				return result, candidateErr
			}
			result.Candidates = append(result.Candidates, candidate)
		}
	}
	return result, selectErr
}

// discover only enumerates campaign identities under the confined root; it
// never interprets temp files or reconstructs a snapshot from other evidence.
func (store *Store) discover(ctx context.Context) (_ []session.RecoveryCandidate, err error) {
	defer mon.Task()(&ctx)(&err)
	dir, err := store.listing.Open("campaigns")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names, readErr := dir.Readdirnames(-1)
	if err = errors.Join(readErr, dir.Close()); err != nil {
		return nil, err
	}
	sort.Strings(names)
	var candidates []session.RecoveryCandidate
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if !validCampaignID(name) {
			continue
		}
		result, inspectErr := store.inspectCampaign(ctx, session.CampaignID(name))
		if inspectErr == nil {
			revisions, openErr := store.revisionsFor(ctx, session.CampaignID(name))
			if openErr != nil {
				return nil, openErr
			}
			inspection, openErr := revisions.Inspect(ctx)
			if openErr != nil {
				return nil, openErr
			}
			for _, slot := range inspection.Slots {
				if slot.Err == nil && slot.Revision.Number == result.Revision && slot.Revision.PayloadDigest == result.PayloadDigest {
					candidate, candidateErr := store.candidate(ctx, slot.Revision, false)
					if candidateErr != nil {
						return nil, candidateErr
					}
					candidates = append(candidates, candidate)
					break
				}
			}
		} else if errors.Is(inspectErr, session.SaveCorrupt) || errors.Is(inspectErr, os.ErrNotExist) {
			candidates = append(candidates, result.Candidates...)
		} else if !errors.Is(inspectErr, session.SaveTooNew) && !errors.Is(inspectErr, session.ContentIncompatible) {
			return nil, fmt.Errorf("inspect campaign %s: %w", name, inspectErr)
		}
	}
	return candidates, nil
}

func (store *Store) campaignExists(ctx context.Context, id session.CampaignID) (_ bool, err error) {
	defer mon.Task()(&ctx)(&err)
	if !validCampaignID(string(id)) {
		return false, session.InvalidAction
	}
	dir, err := store.listing.Open(filepath.Join("campaigns", string(id)))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, dir.Close()
}

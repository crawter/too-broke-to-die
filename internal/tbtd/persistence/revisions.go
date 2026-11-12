// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"syscall"

	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
)

// Slot describes one independently inspected file. A failed slot never lends
// any fields to another slot. Missing and unreadable files are distinguished.
type Slot struct {
	Name     string
	Revision Revision
	Err      error
	data     []byte
}

// Inspection reports both slots without silently selecting an older revision.
// Callers must present an explicit recovery choice when either slot is invalid.
type Inspection struct {
	Slots   [2]Slot
	Pending bool
}

// CommitResult contains a publishable revision only when Status is succeeded.
// CurrentKnown distinguishes a selected revision zero from an unknown revision;
// on conflicts CurrentRevision reports the selected revision without exposing
// an unacknowledged snapshot.
type CommitResult struct {
	Status          session.StorageStatus
	Code            session.ErrorCode
	Revision        Revision
	PayloadDigest   string
	CurrentRevision uint64
	CurrentKnown    bool
}

// Revisions serializes operations for one campaign. The caller must hold the
// platform profile lock for this object's entire lifetime and must not create a
// second writer under the same lock. The root and codec are borrowed.
type Revisions struct {
	mu       sync.Mutex
	files    revisionFiles
	codec    *Codec
	id       string
	dir      string
	pending  *preparedCommit
	recovery *recoveryChoice
	ack      func() error
}

type recoveryChoice struct {
	number uint64
	digest string
}

// NewRevisions binds a campaign UUID to the platform-confined data root.
func NewRevisions(ctx context.Context, root *platform.Root, codec *Codec, campaignID string) (_ *Revisions, err error) {
	defer mon.Task()(&ctx)(&err)
	if root == nil || codec == nil || !validCampaignID(campaignID) {
		return nil, fmt.Errorf("invalid revision store configuration")
	}
	return newRevisions(ctx, root, codec, campaignID)
}

func newRevisions(ctx context.Context, files revisionFiles, codec *Codec, campaignID string, options ...revisionOption) (_ *Revisions, err error) {
	defer mon.Task()(&ctx)(&err)
	if files == nil || codec == nil || !validCampaignID(campaignID) {
		return nil, fmt.Errorf("invalid revision store configuration")
	}
	store := &Revisions{files: files, codec: codec, id: campaignID, dir: filepath.Join("campaigns", campaignID)}
	for _, option := range options {
		option(store)
	}
	return store, nil
}

func (store *Revisions) slotName(index int) string {
	if index == 0 {
		return filepath.Join(store.dir, "progress.a.json")
	}
	return filepath.Join(store.dir, "progress.b.json")
}

// Inspect independently reads both slots, including incompatible or corrupt
// data. It never interprets a temporary file as a committed revision. While a
// commit is unresolved it returns only Pending, not an unacknowledged snapshot.
func (store *Revisions) Inspect(ctx context.Context) (_ Inspection, err error) {
	defer mon.Task()(&ctx)(&err)
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.pending != nil {
		return Inspection{Pending: true}, nil
	}
	return store.inspect(ctx), nil
}

func (store *Revisions) inspect(ctx context.Context) Inspection {
	defer mon.Task()(&ctx)(nil)
	result := Inspection{Pending: store.pending != nil}
	for i := range result.Slots {
		name := store.slotName(i)
		result.Slots[i].Name = name
		data, err := store.files.ReadFile(ctx, name, MaxRevisionBytes)
		if errors.Is(err, platform.ErrTooLarge) {
			err = &RevisionError{Code: session.SaveCorrupt, Cause: err}
		}
		if err == nil {
			result.Slots[i].Revision, err = store.codec.Decode(ctx, data)
			if err == nil && result.Slots[i].Revision.CampaignID != store.id {
				err = fmt.Errorf("%w: slot campaign identity", session.StorageConflict)
			}
			if err == nil {
				result.Slots[i].data = data
			}
		}
		result.Slots[i].Err = err
	}
	return result
}

// AcceptRecovery explicitly accepts one valid older slot for an already
// selected campaign. The profile owner must not use recovery to select a new
// campaign: Initialize must first validate both initial slots. The other slot
// is preserved before replacement; unsupported data is never overwritten.
func (store *Revisions) AcceptRecovery(ctx context.Context, number uint64) (_ Revision, err error) {
	defer mon.Task()(&ctx)(&err)
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.pending != nil {
		return Revision{}, fmt.Errorf("%w: unresolved operation", session.StorageConflict)
	}
	inspection := store.inspect(ctx)
	var chosen *Revision
	invalid := false
	for _, slot := range inspection.Slots {
		if errors.Is(slot.Err, session.SaveTooNew) || errors.Is(slot.Err, session.ContentIncompatible) {
			return Revision{}, slot.Err
		}
		if slot.Err != nil && !errors.Is(slot.Err, session.SaveCorrupt) && !errors.Is(slot.Err, os.ErrNotExist) {
			return Revision{}, slot.Err
		}
		if slot.Err != nil {
			invalid = true
		}
		if slot.Err == nil && slot.Revision.Number == number {
			if chosen != nil && chosen.PayloadDigest != slot.Revision.PayloadDigest {
				return Revision{}, fmt.Errorf("%w: divergent revisions", session.StorageConflict)
			}
			copy := slot.Revision
			chosen = &copy
		}
	}
	if chosen == nil {
		return Revision{}, fmt.Errorf("%w: recovery revision unavailable", session.SaveCorrupt)
	}
	if !invalid {
		return Revision{}, fmt.Errorf("%w: both slots valid; recovery cannot rewind", session.StorageConflict)
	}
	store.recovery = &recoveryChoice{number: chosen.Number, digest: chosen.PayloadDigest}
	return *chosen, nil
}

// selectSlot requires explicit approval before falling back from damaged data.
func (store *Revisions) selectSlot(ctx context.Context, inspection Inspection) (_ int, err error) {
	defer mon.Task()(&ctx)(&err)
	if inspection.Slots[0].Err == nil && inspection.Slots[1].Err == nil {
		older, newer := inspection.Slots[0].Revision, inspection.Slots[1].Revision
		if older.Number > newer.Number {
			older, newer = newer, older
		}
		if older.Number == newer.Number {
			if !sameRevision(older, newer) {
				return -1, fmt.Errorf("%w: divergent revisions", session.StorageConflict)
			}
		} else if newer.Number-older.Number != 1 || len(newer.Operations) != len(older.Operations)+1 || !slices.Equal(older.Operations, newer.Operations[:len(older.Operations)]) {
			return -1, fmt.Errorf("%w: revisions do not share a predecessor", session.StorageConflict)
		}
	}
	// Compatibility is never downgraded to corruption merely because the
	// other slot is missing or damaged.
	for _, slot := range inspection.Slots {
		if errors.Is(slot.Err, session.SaveTooNew) || errors.Is(slot.Err, session.ContentIncompatible) {
			return -1, slot.Err
		}
	}
	for _, slot := range inspection.Slots {
		if slot.Err != nil && (store.recovery == nil || (!errors.Is(slot.Err, session.SaveCorrupt) && !errors.Is(slot.Err, os.ErrNotExist))) {
			return -1, slot.Err
		}
	}
	best := -1
	for i, slot := range inspection.Slots {
		if slot.Err != nil {
			continue
		}
		if store.recovery != nil && (slot.Revision.Number != store.recovery.number || slot.Revision.PayloadDigest != store.recovery.digest) {
			continue
		}
		if best < 0 || slot.Revision.Number > inspection.Slots[best].Revision.Number {
			best = i
		} else if slot.Revision.Number == inspection.Slots[best].Revision.Number && slot.Revision.PayloadDigest != inspection.Slots[best].Revision.PayloadDigest {
			return -1, fmt.Errorf("%w: divergent revisions", session.StorageConflict)
		}
	}
	if best < 0 {
		return -1, fmt.Errorf("%w: no selected revision", session.SaveCorrupt)
	}
	return best, nil
}

func resultForError(err error) CommitResult {
	switch {
	case errors.Is(err, session.SaveTooNew):
		return CommitResult{Status: session.StorageTooNew, Code: session.SaveTooNew}
	case errors.Is(err, session.ContentIncompatible):
		return CommitResult{Status: session.StorageIncompatible, Code: session.ContentIncompatible}
	case errors.Is(err, session.SaveCorrupt), errors.Is(err, os.ErrNotExist):
		return CommitResult{Status: session.StorageCorrupt, Code: session.SaveCorrupt}
	case errors.Is(err, session.StorageConflict):
		return CommitResult{Status: session.StorageConflicting, Code: session.StorageConflict}
	default:
		return CommitResult{Status: session.StorageFailed, Code: storageCode(err)}
	}
}

// Windows reports full volumes as ERROR_DISK_FULL or ERROR_HANDLE_DISK_FULL;
// their numeric values are Win32 constants, not Unix errno values.
func storageCode(err error) session.ErrorCode {
	if errors.Is(err, syscall.ENOSPC) || (runtime.GOOS == "windows" && (errors.Is(err, syscall.Errno(112)) || errors.Is(err, syscall.Errno(39)))) {
		return session.StorageFull
	}
	return session.StorageDenied
}

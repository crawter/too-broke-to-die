// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Initialize installs identical revision-zero snapshots in both slots. A
// partial initialization can be retried with exactly the same initial value;
// existing different or unreadable files are never replaced. Selection of the
// campaign is the separate profile owner's responsibility.
func (store *Revisions) Initialize(ctx context.Context, initial Revision) (_ CommitResult, err error) {
	defer mon.Task()(&ctx)(&err)
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.pending != nil || initial.CampaignID != store.id || initial.Number != 0 || len(initial.Operations) != 0 || initial.Operation.Sequence != 0 || initial.Operation.Event != "" {
		err = fmt.Errorf("%w: invalid initialization", errConflict())
		return resultForError(err), err
	}
	data, checked, err := store.codec.Encode(ctx, initial)
	if err != nil {
		return resultForError(err), err
	}
	if err = store.files.MkdirAll(ctx, store.dir); err != nil {
		return resultForError(err), err
	}
	for i := 0; i < 2; i++ {
		name := store.slotName(i)
		existing, readErr := store.files.ReadFile(ctx, name, MaxRevisionBytes)
		if readErr == nil {
			if !bytes.Equal(existing, data) {
				err = fmt.Errorf("%w: existing initial slot", errConflict())
				return resultForError(err), err
			}
		} else if errors.Is(readErr, os.ErrNotExist) {
			if err = store.install(ctx, name, data, checked); err != nil {
				return resultForError(err), err
			}
		} else {
			return resultForError(readErr), readErr
		}
		if err = store.confirm(ctx, name, checked); err != nil {
			return resultForError(err), err
		}
	}
	return confirmed(checked), nil
}

func (store *Revisions) install(ctx context.Context, destination string, data []byte, expected Revision) (err error) {
	defer mon.Task()(&ctx)(&err)
	temp, err := store.files.WriteTemp(ctx, store.dir, data, MaxRevisionBytes)
	if err != nil {
		return err
	}
	written, err := store.files.ReadFile(ctx, temp, MaxRevisionBytes)
	if err != nil {
		return err
	}
	checked, err := store.codec.Decode(ctx, written)
	if err != nil {
		return err
	}
	if !bytes.Equal(written, data) || !sameRevision(checked, expected) {
		return fmt.Errorf("temporary revision differs from proposal")
	}
	_, err = store.files.ReplaceFile(ctx, temp, destination)
	if err != nil {
		return err
	}
	return store.confirm(ctx, destination, expected)
}

// confirm repeats both the installed file flush and the directory flush. The
// latter is necessary after a rename that succeeded but whose flush failed.
// MkdirAll flushes the directory and its parents through the platform boundary.
func (store *Revisions) confirm(ctx context.Context, name string, expected Revision) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = store.files.SyncFile(ctx, name); err != nil {
		return err
	}
	if err = store.files.MkdirAll(ctx, store.dir); err != nil {
		return err
	}
	data, err := store.files.ReadFile(ctx, name, MaxRevisionBytes)
	if err != nil {
		return err
	}
	checked, err := store.codec.Decode(ctx, data)
	if err != nil {
		return err
	}
	if !sameRevision(checked, expected) {
		return fmt.Errorf("installed revision differs from proposal")
	}
	return nil
}

func sameRevision(left, right Revision) bool {
	return left.CampaignID == right.CampaignID && left.Number == right.Number && left.Operation == right.Operation && left.Compatibility == right.Compatibility && left.PayloadLength == right.PayloadLength && left.PayloadDigest == right.PayloadDigest
}

// preserve copies the exact invalid bytes into the damaged directory and
// flushes their new directory entry before the original can be replaced.
func (store *Revisions) preserve(ctx context.Context, slot Slot) (err error) {
	defer mon.Task()(&ctx)(&err)
	if errors.Is(slot.Err, os.ErrNotExist) {
		return nil
	}
	if slot.Err == nil {
		return nil
	}
	data, err := store.files.ReadFile(ctx, slot.Name, MaxRevisionBytes)
	if err != nil {
		return err
	}
	dir := filepath.Join(store.dir, "damaged")
	if err = store.files.MkdirAll(ctx, dir); err != nil {
		return err
	}
	temp, err := store.files.WriteTemp(ctx, dir, data, MaxRevisionBytes)
	if err != nil {
		return err
	}
	copy, err := store.files.ReadFile(ctx, temp, MaxRevisionBytes)
	if err != nil {
		return err
	}
	if !bytes.Equal(copy, data) {
		return fmt.Errorf("damaged revision copy differs")
	}
	_, err = store.files.ReplaceFile(ctx, temp, temp+".preserved")
	return err
}

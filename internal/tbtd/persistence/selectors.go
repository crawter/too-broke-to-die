// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
)

var errSelectorUnresolved = errors.New("selector installation unresolved")

const (
	selectorIntentName = "selector.intent.json"
	selectorGuardName  = "selector.guard.json"
	selectorFenceName  = "selector.fence.json"
)

type selector struct {
	Campaign session.CampaignID `json:"campaign"`
	Sequence uint64             `json:"sequence"`
}

type selectorIntent struct {
	Pending  bool     `json:"pending"`
	Target   string   `json:"target"`
	Selected selector `json:"selected"`
}

type pendingSelector struct {
	name   string
	data   []byte
	intent selectorIntent
}

type selectorSlot struct {
	name      string
	value     selector
	data      []byte
	err       error
	oversized bool
}

func (store *Store) selectors(ctx context.Context) (_ [2]selectorSlot) {
	defer mon.Task()(&ctx)(nil)
	var slots [2]selectorSlot
	for i, name := range []string{"active.a.json", "active.b.json"} {
		slots[i].name = name
		data, err := store.files.ReadFile(ctx, name, maxProfileFile)
		if errors.Is(err, platform.ErrTooLarge) {
			slots[i].oversized = true
			err = session.SaveCorrupt
		}
		if err == nil {
			slots[i].data = data
			err = decodeChecked(ctx, data, "TBTD-ACTIVE", &slots[i].value)
			if err == nil && (!validCampaignID(string(slots[i].value.Campaign)) || slots[i].value.Sequence == 0) {
				err = session.SaveCorrupt
			}
		}
		slots[i].err = err
	}
	return slots
}

func newestSelector(slots [2]selectorSlot) (int, error) {
	best := -1
	for i, slot := range slots {
		if slot.err != nil {
			if errors.Is(slot.err, session.SaveTooNew) {
				return -1, slot.err
			}
			if !errors.Is(slot.err, os.ErrNotExist) && !errors.Is(slot.err, session.SaveCorrupt) {
				return -1, slot.err
			}
			continue
		}
		if best < 0 || slot.value.Sequence > slots[best].value.Sequence {
			best = i
		} else if slot.value.Sequence == slots[best].value.Sequence && slot.value != slots[best].value {
			return -1, session.StorageConflict
		}
	}
	if best < 0 {
		return -1, session.SaveCorrupt
	}
	return best, nil
}

// A fence stays pending until a successful selection result is published.
// After a crash before the fence is cleared, conservatively require an
// explicit choice even if the last selection may have succeeded.
func (store *Store) readSelectorIntent(ctx context.Context) (_ selectorIntent, err error) {
	defer mon.Task()(&ctx)(&err)
	names := []string{selectorGuardName, selectorIntentName, selectorFenceName}
	var records [3]selectorIntent
	var failures [3]error
	for i, name := range names {
		records[i], failures[i] = store.readSelectorRecord(ctx, name)
	}
	// A future schema takes precedence even over another damaged record.
	for _, failure := range failures {
		if errors.Is(failure, session.SaveTooNew) {
			return selectorIntent{}, failure
		}
	}
	for _, failure := range failures {
		if failure != nil {
			return selectorIntent{}, failure
		}
	}
	if records[0].Pending {
		return records[0], nil
	}
	if records[1].Pending {
		return records[1], nil
	}
	if records[2].Pending && (store.selectorFence == nil || *store.selectorFence != records[2]) {
		return records[2], nil
	}
	return records[1], nil
}

func (store *Store) readSelectorRecord(ctx context.Context, name string) (_ selectorIntent, err error) {
	defer mon.Task()(&ctx)(&err)
	data, err := store.files.ReadFile(ctx, name, maxProfileFile)
	if errors.Is(err, os.ErrNotExist) {
		return selectorIntent{}, nil
	}
	if errors.Is(err, platform.ErrTooLarge) {
		return selectorIntent{}, session.SaveCorrupt
	}
	if err != nil {
		return selectorIntent{}, err
	}
	var intent selectorIntent
	if err = decodeChecked(ctx, data, "TBTD-SELECTOR-INTENT", &intent); err != nil {
		return selectorIntent{}, err
	}
	if (intent.Target != "active.a.json" && intent.Target != "active.b.json") || !validCampaignID(string(intent.Selected.Campaign)) || intent.Selected.Sequence == 0 {
		return selectorIntent{}, session.SaveCorrupt
	}
	return intent, nil
}

func (store *Store) writeSelectorRecord(ctx context.Context, name string, intent selectorIntent) (_ bool, err error) {
	defer mon.Task()(&ctx)(&err)
	data, err := encodeChecked(ctx, "TBTD-SELECTOR-INTENT", intent)
	if err != nil {
		return false, err
	}
	return store.installProfileFile(ctx, name, data)
}

func (store *Store) writeSelectorIntent(ctx context.Context, intent selectorIntent) (_ bool, err error) {
	defer mon.Task()(&ctx)(&err)
	return store.writeSelectorRecord(ctx, selectorIntentName, intent)
}

// Only an explicit selection or New Game calls this recovery path. Preserve
// every damaged record first, and never replace a future or unreadable schema.
func (store *Store) prepareSelectorIntent(ctx context.Context) (_ selectorIntent, err error) {
	defer mon.Task()(&ctx)(&err)
	names := []string{selectorGuardName, selectorIntentName, selectorFenceName}
	var records [3]selectorIntent
	var failures [3]error
	for i, name := range names {
		records[i], failures[i] = store.readSelectorRecord(ctx, name)
		if failures[i] != nil && !errors.Is(failures[i], session.SaveCorrupt) {
			return selectorIntent{}, failures[i]
		}
	}
	damaged := false
	for i, name := range names {
		if failures[i] == nil {
			continue
		}
		damaged = true
		data, readErr := store.files.ReadFile(ctx, name, maxProfileFile)
		if errors.Is(readErr, platform.ErrTooLarge) {
			err = store.copyOversizedSelector(ctx, name)
		} else if readErr != nil {
			err = readErr
		} else {
			err = store.preserveProfile(ctx, data)
		}
		if err != nil {
			return selectorIntent{}, err
		}
	}
	if damaged {
		return selectorIntent{Pending: true}, nil
	}
	if records[0].Pending {
		return records[0], nil
	}
	if records[1].Pending {
		return records[1], nil
	}
	if records[2].Pending && (store.selectorFence == nil || *store.selectorFence != records[2]) {
		return records[2], nil
	}
	return records[1], nil
}

// Copy oversized slots rather than moving them: either a symlink or a hard
// link could otherwise leave the preserved evidence mutable through an alias.
// The original remains in place until the selector replacement succeeds.
func (store *Store) preserveOversizedSelector(ctx context.Context, slot selectorSlot) (err error) {
	defer mon.Task()(&ctx)(&err)
	return store.copyOversizedSelector(ctx, slot.name)
}

func (store *Store) copyOversizedSelector(ctx context.Context, name string) (err error) {
	defer mon.Task()(&ctx)(&err)
	source, err := store.listing.Open(name)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("oversized selector target is not a regular file")
	}
	temp, err := store.files.WriteTemp(ctx, ".", nil, 0)
	if err != nil {
		return err
	}
	destination, err := store.listing.OpenFile(temp, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	originalDigest, copyErr := copySelectorBytes(ctx, destination, source)
	var syncErr error
	if copyErr == nil {
		syncErr = destination.Sync()
	}
	if err = errors.Join(copyErr, syncErr, destination.Close()); err != nil {
		return err
	}
	check, err := store.listing.Open(temp)
	if err != nil {
		return err
	}
	copiedDigest, checkErr := copySelectorBytes(ctx, io.Discard, check)
	if err = errors.Join(checkErr, check.Close()); err != nil {
		return err
	}
	if copiedDigest != originalDigest {
		return errors.New("preserved selector copy differs")
	}
	_, err = store.files.ReplaceFile(ctx, temp, temp+".preserved")
	return err
}

func copySelectorBytes(ctx context.Context, destination io.Writer, source io.Reader) (_ [32]byte, err error) {
	defer mon.Task()(&ctx)(&err)
	hash := sha256.New()
	writer := io.MultiWriter(destination, hash)
	buffer := make([]byte, 64<<10)
	for {
		if err = ctx.Err(); err != nil {
			return [32]byte{}, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			if _, err = writer.Write(buffer[:count]); err != nil {
				return [32]byte{}, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			var digest [32]byte
			copy(digest[:], hash.Sum(nil))
			return digest, nil
		}
		if readErr != nil {
			return [32]byte{}, readErr
		}
		if count == 0 {
			return [32]byte{}, io.ErrNoProgress
		}
	}
}

// settleSelector keeps a separately durable pending guard while replacing,
// flushing and validating the settled intent. If clearing the guard is itself
// ambiguous, re-arm a pending record before returning the error.
func (store *Store) settleSelector(ctx context.Context, intent selectorIntent) (err error) {
	defer mon.Task()(&ctx)(&err)
	intent.Pending = true
	if _, err = store.writeSelectorRecord(ctx, selectorGuardName, intent); err != nil {
		return err
	}
	settled := intent
	settled.Pending = false
	if _, err = store.writeSelectorIntent(ctx, settled); err != nil {
		return err
	}
	if _, err = store.writeSelectorRecord(ctx, selectorGuardName, settled); err != nil {
		// The fence remains pending if both re-arm writes fail. Never extend the
		// operation or drain deadline for these best-effort writes.
		_, intentErr := store.writeSelectorIntent(ctx, intent)
		if intentErr != nil {
			_, guardErr := store.writeSelectorRecord(ctx, selectorGuardName, intent)
			return errors.Join(err, intentErr, guardErr)
		}
		return err
	}
	return nil
}

// clearSelectorFence is called only after the worker has returned the result.
// A failure to clear can at worst cause explicit recovery on the next launch:
// the selection was already acknowledged before this cleanup began.
func (store *Store) clearSelectorFence(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if store.selectorFence == nil || store.selectorPending != nil {
		return nil
	}
	fence, err := store.readSelectorRecord(ctx, selectorFenceName)
	if err != nil {
		return err
	}
	if fence != *store.selectorFence {
		return session.StorageConflict
	}
	fence.Pending = false
	if _, err = store.writeSelectorRecord(ctx, selectorFenceName, fence); err != nil {
		return err
	}
	store.selectorFence = nil
	return nil
}

// finishSelector retries the same bytes and settles their durable intent only
// after the selector itself has been installed and independently confirmed.
func (store *Store) finishSelector(ctx context.Context, pending *pendingSelector) (err error) {
	defer mon.Task()(&ctx)(&err)
	store.selectorFence = nil
	if _, err = store.writeSelectorIntent(ctx, pending.intent); err != nil {
		return err
	}
	if _, err = store.writeSelectorRecord(ctx, selectorFenceName, pending.intent); err != nil {
		return err
	}
	if _, err = store.installProfileFile(ctx, pending.name, pending.data); err != nil {
		return err
	}
	if err = store.settleSelector(ctx, pending.intent); err != nil {
		return err
	}
	fence := pending.intent
	store.selectorFence = &fence
	return nil
}

// selectActive updates only the older selector. Before replacing it, an intent
// records the operation so a restart cannot mistake a failed rename for an
// acknowledged campaign selection. Explicit selection can resolve an intent.
func (store *Store) selectActive(ctx context.Context, id session.CampaignID) (_ session.StorageStatus, err error) {
	defer mon.Task()(&ctx)(&err)
	previousIntent, err := store.prepareSelectorIntent(ctx)
	if err != nil {
		return resultForError(err).Status, err
	}
	slots := store.selectors(ctx)
	best, selectErr := newestSelector(slots)
	if selectErr != nil && !errors.Is(selectErr, session.SaveCorrupt) {
		return resultForError(selectErr).Status, selectErr
	}
	sequence := uint64(1)
	target := 0
	if best >= 0 {
		if slots[best].value.Campaign == id && !previousIntent.Pending {
			return session.StorageSucceeded, nil
		}
		if slots[best].value.Campaign == id && previousIntent.Pending {
			pending := &pendingSelector{name: slots[best].name, data: slots[best].data, intent: selectorIntent{Pending: true, Target: slots[best].name, Selected: slots[best].value}}
			if err = store.finishSelector(ctx, pending); err != nil {
				store.selectorPending = pending
				return session.StorageAmbiguous, err
			}
			store.selectorPending = nil
			return session.StorageSucceeded, nil
		}
		if slots[best].value.Sequence == math.MaxUint64 {
			return session.StorageConflicting, session.StorageConflict
		}
		sequence = slots[best].value.Sequence + 1
		target = 1 - best
	}
	if slots[target].err != nil && !errors.Is(slots[target].err, os.ErrNotExist) {
		if !errors.Is(slots[target].err, session.SaveCorrupt) {
			return session.StorageFailed, slots[target].err
		}
		if slots[target].oversized {
			err = store.preserveOversizedSelector(ctx, slots[target])
		} else if slots[target].data != nil {
			err = store.preserveProfile(ctx, slots[target].data)
		} else {
			err = session.SaveCorrupt
		}
		if err != nil {
			return session.StorageFailed, err
		}
	}
	selected := selector{Campaign: id, Sequence: sequence}
	data, err := encodeChecked(ctx, "TBTD-ACTIVE", selected)
	if err != nil {
		return session.StorageFailed, err
	}
	intent := selectorIntent{Pending: true, Target: slots[target].name, Selected: selected}
	installed, err := store.writeSelectorIntent(ctx, intent)
	if err != nil {
		if installed || previousIntent.Pending {
			return session.StorageAmbiguous, fmt.Errorf("selector intent unresolved: %w", err)
		}
		return session.StorageFailed, err
	}
	pending := &pendingSelector{name: slots[target].name, data: data, intent: intent}
	store.selectorFence = nil
	if _, err = store.writeSelectorRecord(ctx, selectorFenceName, intent); err != nil {
		store.selectorPending = pending
		return session.StorageAmbiguous, fmt.Errorf("selector fence unresolved: %w", err)
	}
	installed, err = store.installProfileFile(ctx, pending.name, pending.data)
	if err != nil {
		if installed || previousIntent.Pending {
			store.selectorPending = pending
			return session.StorageAmbiguous, fmt.Errorf("selector installation unresolved: %w", err)
		}
		settled := intent
		settled.Pending = false
		if _, clearErr := store.writeSelectorIntent(ctx, settled); clearErr != nil {
			return session.StorageAmbiguous, errors.Join(err, clearErr)
		}
		// Replacement did not happen; the prior selection remains authoritative.
		store.selectorFence = &intent
		return session.StorageFailed, err
	}
	if err = store.settleSelector(ctx, intent); err != nil {
		store.selectorPending = pending
		return session.StorageAmbiguous, fmt.Errorf("selector confirmation unresolved: %w", err)
	}
	store.selectorFence = &intent
	store.selectorPending = nil
	return session.StorageSucceeded, nil
}

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
	"strings"
	"sync"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

type contextKey struct{}

type gateFiles struct {
	*platform.Root
	once    sync.Once
	entered chan struct{}
	release chan struct{}
	value   chan any
}

func (files *gateFiles) ReadFile(ctx context.Context, name string, max int64) ([]byte, error) {
	if name == "settings.json" {
		files.once.Do(func() {
			files.value <- ctx.Value(contextKey{})
			close(files.entered)
			select {
			case <-files.release:
			case <-ctx.Done():
			}
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return files.Root.ReadFile(ctx, name, max)
}

func settingsRequest(t *testing.T, id session.RequestID, kind session.SettingsKind) session.SettingsRequest {
	t.Helper()
	request, err := session.NewSettingsRequest(t.Context(), session.SettingsInput{ID: id, Kind: kind, Values: settings.Defaults(t.Context())})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestWriterQueueDrainAndCancellation(t *testing.T) {
	codec, _, _ := fixture(t)
	store, err := Open(t.Context(), t.TempDir(), codec, 1, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	files := &gateFiles{Root: store.root, entered: make(chan struct{}), release: make(chan struct{}), value: make(chan any, 1)}
	store.files = files
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() { close(files.release) })
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	run := make(chan error, 1)
	go func() { run <- store.Run(runCtx) }()
	initiator, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "trace"))
	view := store.Settings()
	if err := view.Submit(initiator, settingsRequest(t, "first", session.LoadSettings)); err != nil {
		t.Fatal(err)
	}
	<-files.entered
	cancel()
	if got := <-files.value; got != "trace" {
		t.Fatalf("initiating context lost: %v", got)
	}
	if err := view.Submit(t.Context(), settingsRequest(t, "first", session.LoadSettings)); !errors.Is(err, session.StorageConflict) {
		t.Fatalf("duplicate request ID accepted: %v", err)
	}
	if err := view.Submit(t.Context(), settingsRequest(t, "second", session.SaveSettings)); err != nil {
		t.Fatal(err)
	}
	if err := view.Submit(t.Context(), settingsRequest(t, "full", session.LoadSettings)); !errors.Is(err, session.ErrStoreBusy) {
		t.Fatalf("full queue: %v", err)
	}
	pending, err := view.Poll(t.Context(), "second")
	if err != nil || pending.Data(t.Context()).Status != session.StoragePending {
		t.Fatalf("pending: %+v %v", pending.Data(t.Context()), err)
	}
	cancelRun()
	release.Do(func() { close(files.release) })
	if err := <-run; err != nil {
		t.Fatalf("worker: %v", err)
	}
	for _, id := range []session.RequestID{"first", "second"} {
		result, err := view.Poll(t.Context(), id)
		if err != nil || result.Data(t.Context()).Status != session.StorageSucceeded {
			t.Fatalf("accepted work lost %s: %+v %v", id, result.Data(t.Context()), err)
		}
	}
	if err := view.Submit(t.Context(), settingsRequest(t, "closed", session.LoadSettings)); !errors.Is(err, session.ErrStoreClosed) {
		t.Fatalf("submission after shutdown: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseBeforeRunDrainsAcceptedRequests(t *testing.T) {
	store, _, _ := profileFixture(t)
	view := store.Settings()
	if err := view.Submit(t.Context(), settingsRequest(t, "queued", session.SaveSettings)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := view.Poll(t.Context(), "queued")
	if err != nil || result.Data(t.Context()).Status != session.StorageSucceeded {
		t.Fatalf("queued result: %+v %v", result.Data(t.Context()), err)
	}
	if err := store.Run(t.Context()); !errors.Is(err, session.ErrStoreClosed) {
		t.Fatalf("started after close: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

type panicFiles struct{ *platform.Root }

func (files *panicFiles) ReadFile(context.Context, string, int64) ([]byte, error) {
	panic("injected worker failure")
}

func TestUnexpectedWorkerExitIsReported(t *testing.T) {
	codec, _, _ := fixture(t)
	store, err := Open(t.Context(), t.TempDir(), codec, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	store.files = &panicFiles{Root: store.root}
	view := store.Settings()
	if err := view.Submit(t.Context(), settingsRequest(t, "panic", session.LoadSettings)); err != nil {
		t.Fatal(err)
	}
	if err := store.Run(t.Context()); err == nil {
		t.Fatal("unexpected worker exit was hidden")
	}
	result, err := view.Poll(t.Context(), "panic")
	if err != nil || result.Data(t.Context()).Status != session.StorageFailed {
		t.Fatalf("lost active request: %+v %v", result.Data(t.Context()), err)
	}
	if err := store.Close(); err == nil {
		t.Fatal("close hid worker failure")
	}
}

type stuckFiles struct {
	*platform.Root
	entered chan struct{}
	release chan struct{}
}

func (files *stuckFiles) ReadFile(ctx context.Context, name string, max int64) ([]byte, error) {
	if name == "settings.json" {
		close(files.entered)
		<-files.release
	}
	return files.Root.ReadFile(ctx, name, max)
}

func TestDeadlineKeepsProfileLockedUntilWorkerJoins(t *testing.T) {
	codec, _, _ := fixture(t)
	path := t.TempDir()
	store, err := Open(t.Context(), path, codec, 2, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	files := &stuckFiles{Root: store.root, entered: make(chan struct{}), release: make(chan struct{})}
	store.files = files
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(files.release) }) })
	run := make(chan error, 1)
	go func() { run <- store.Run(context.Background()) }()
	if err := store.Settings().Submit(t.Context(), settingsRequest(t, "blocked", session.LoadSettings)); err != nil {
		t.Fatal(err)
	}
	<-files.entered
	if err := store.Close(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing drain deadline: %v", err)
	}
	other, err := platform.OpenRoot(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	lock, lockErr := other.LockProfile(t.Context())
	if lockErr == nil {
		_ = lock.Close()
		_ = other.Close()
		t.Fatal("profile lock released while worker was blocked")
	}
	if !errors.Is(lockErr, platform.ErrProfileInUse) {
		t.Fatalf("profile lock: %v", lockErr)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	release.Do(func() { close(files.release) })
	if err := <-run; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("worker hid deadline: %v", err)
	}
	if err := store.Close(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("repeated close hid deadline: %v", err)
	}
	other, err = platform.OpenRoot(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	lock, err = other.LockProfile(t.Context())
	if err != nil {
		t.Fatalf("profile lock not released after join: %v", err)
	}
	if err := errors.Join(lock.Close(), other.Close()); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverySelectsCampaignBeforePublishing(t *testing.T) {
	store, service, path := profileFixture(t)
	historical := newProfileCampaign(t, store)
	current := newProfileCampaign(t, store)
	if err := os.WriteFile(filepath.Join(path, "campaigns", string(historical.Campaign), "progress.b.json"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	request := session.ProgressInput{ID: "recovery", Kind: session.LoadRevision, Campaign: historical.Campaign, ExpectedRevision: 9}
	blocked := store.processProgress(t.Context(), request)
	if blocked.Status != session.StorageCorrupt || blocked.RevisionKnown {
		t.Fatalf("unavailable recovery candidate accepted: %+v", blocked)
	}
	request.ExpectedRevision = 0
	store.files = &selectorFault{faultFiles: &faultFiles{Root: store.root}}
	blocked = store.processProgress(t.Context(), request)
	if blocked.Status != session.StorageFailed || blocked.RevisionKnown || blocked.Snapshot.Credits() != 0 {
		t.Fatalf("failed selection published recovery: %+v", blocked)
	}
	active := store.processProgress(t.Context(), session.ProgressInput{ID: "current", Kind: session.LoadActive})
	if active.Status != session.StorageSucceeded || active.Campaign != current.Campaign {
		t.Fatalf("failed recovery changed selection: %+v", active)
	}
	recovered := store.processProgress(t.Context(), request)
	if recovered.Status != session.StorageSucceeded || recovered.Campaign != historical.Campaign || recovered.Revision != 0 {
		t.Fatalf("recovery selection: %+v", recovered)
	}
	loaded := store.processProgress(t.Context(), session.ProgressInput{ID: "named", Kind: session.LoadCampaign, Campaign: historical.Campaign})
	if loaded.Status != session.StorageSucceeded || loaded.Campaign != historical.Campaign || loaded.PayloadDigest != recovered.PayloadDigest {
		t.Fatalf("named load disagrees with recovery: %+v", loaded)
	}
	committed := store.processProgress(t.Context(), storyCommit(t, service, recovered))
	if committed.Status != session.StorageSucceeded || committed.Campaign != historical.Campaign || committed.Revision != 1 {
		t.Fatalf("commit after recovery: %+v", committed)
	}
	active = store.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
	if active.Status != session.StorageSucceeded || active.Campaign != historical.Campaign || active.Revision != 1 || active.Snapshot.Credits() != 0 {
		t.Fatalf("loaded a different campaign: %+v", active)
	}
}

func TestRecoverySelectionWithInvalidSelectors(t *testing.T) {
	store, service, path := profileFixture(t)
	initial := newProfileCampaign(t, store)
	if err := os.WriteFile(filepath.Join(path, "campaigns", string(initial.Campaign), "progress.b.json"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"active.a.json", "active.b.json"} {
		if err := os.WriteFile(filepath.Join(path, name), []byte("corrupt"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := store.processProgress(t.Context(), session.ProgressInput{ID: "before", Kind: session.LoadActive})
	if before.Status != session.StorageCorrupt || len(before.Candidates) != 1 || before.Candidates[0].Campaign != initial.Campaign {
		t.Fatalf("missing explicit recovery candidate: %+v", before)
	}
	recovered := store.processProgress(t.Context(), session.ProgressInput{ID: "recover", Kind: session.LoadRevision, Campaign: initial.Campaign, ExpectedRevision: 0})
	if recovered.Status != session.StorageSucceeded || recovered.Campaign != initial.Campaign {
		t.Fatalf("candidate selection: %+v", recovered)
	}
	committed := store.processProgress(t.Context(), storyCommit(t, service, recovered))
	if committed.Status != session.StorageSucceeded || committed.Campaign != initial.Campaign || committed.Revision != 1 {
		t.Fatalf("commit after selector recovery: %+v", committed)
	}
	loaded := store.processProgress(t.Context(), session.ProgressInput{ID: "after", Kind: session.LoadActive})
	if loaded.Status != session.StorageSucceeded || loaded.Campaign != initial.Campaign || loaded.Revision != committed.Revision {
		t.Fatalf("selected identity not retained: %+v", loaded)
	}
}

func TestRecoverySelectorAmbiguitySurvivesReopen(t *testing.T) {
	codec, service, _ := fixture(t)
	path := t.TempDir()
	store, err := Open(t.Context(), path, codec, 4, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	historical := newProfileCampaign(t, store)
	current := newProfileCampaign(t, store)
	if err := os.WriteFile(filepath.Join(path, "campaigns", string(historical.Campaign), "progress.b.json"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	store.files = &installedSelectorFault{Root: store.root}
	request := session.ProgressInput{ID: "recover", Kind: session.LoadRevision, Campaign: historical.Campaign, ExpectedRevision: 0}
	result := store.processProgress(t.Context(), request)
	if result.Status != session.StorageAmbiguous || result.RevisionKnown || result.Snapshot.Credits() != 0 {
		t.Fatalf("unacknowledged recovery published: %+v", result)
	}
	if err := store.Close(); err == nil {
		t.Fatal("unresolved selector was not reported")
	}
	store, err = Open(t.Context(), path, codec, 4, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	loaded := store.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
	if loaded.Status != session.StorageAmbiguous || loaded.Campaign != "" || loaded.RevisionKnown || len(loaded.Candidates) != 2 {
		t.Fatalf("reopen silently changed selection from %s: %+v", current.Campaign, loaded)
	}
	result = store.processProgress(t.Context(), request)
	if result.Status != session.StorageSucceeded || result.Campaign != historical.Campaign || result.Revision != 0 {
		t.Fatalf("explicit recovery after reopen: %+v", result)
	}
	committed := store.processProgress(t.Context(), storyCommit(t, service, result))
	loaded = store.processProgress(t.Context(), session.ProgressInput{ID: "after", Kind: session.LoadActive})
	if committed.Status != session.StorageSucceeded || loaded.Status != session.StorageSucceeded || loaded.Campaign != historical.Campaign || loaded.Revision != committed.Revision {
		t.Fatalf("post-recovery selection or commit mismatch: commit=%+v load=%+v", committed, loaded)
	}
}

func TestSelectorIntentSurvivesReopen(t *testing.T) {
	for _, chooseNew := range []bool{false, true} {
		t.Run(fmt.Sprintf("choose-new-%t", chooseNew), func(t *testing.T) {
			codec, _, _ := fixture(t)
			path := t.TempDir()
			store, err := Open(t.Context(), path, codec, 4, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			first := newProfileCampaign(t, store)
			store.files = &installedSelectorFault{Root: store.root}
			created := store.processProgress(t.Context(), session.ProgressInput{ID: "new", Kind: session.CreateCampaign, Campaign: testCampaignID})
			if created.Status != session.StorageAmbiguous || created.Campaign == first.Campaign {
				t.Fatalf("expected post-rename ambiguity: %+v", created)
			}
			if err := store.Close(); err == nil {
				t.Fatal("unresolved selector not reported")
			}
			store, err = Open(t.Context(), path, codec, 4, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Error(err)
				}
			})
			loaded := store.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
			if loaded.Status != session.StorageAmbiguous || loaded.Campaign != "" || loaded.RevisionKnown || len(loaded.Candidates) != 2 {
				t.Fatalf("reopen silently selected a campaign: %+v", loaded)
			}
			chosen := first.Campaign
			if chooseNew {
				chosen = created.Campaign
			}
			selected := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: chosen})
			if selected.Status != session.StorageSucceeded || selected.Campaign != chosen || selected.Snapshot.Credits() != 0 {
				t.Fatalf("explicit choice: %+v", selected)
			}
			loaded = store.processProgress(t.Context(), session.ProgressInput{ID: "load-again", Kind: session.LoadActive})
			if loaded.Status != session.StorageSucceeded || loaded.Campaign != chosen || loaded.Revision != 0 {
				t.Fatalf("selected campaign not restored: %+v", loaded)
			}
		})
	}
}

func TestFutureProfileEnvelopesRemainUntouched(t *testing.T) {
	store, _, path := profileFixture(t)
	initial := newProfileCampaign(t, store)
	futureSelector := []byte(`{"magic":"TBTD-ACTIVE","schema":2,"different_payload":{"revision":10},"new_field":true}`)
	selectorPath := filepath.Join(path, "active.b.json")
	if err := os.WriteFile(selectorPath, futureSelector, 0600); err != nil {
		t.Fatal(err)
	}
	loaded := store.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
	if loaded.Status != session.StorageTooNew || loaded.Code != session.SaveTooNew {
		t.Fatalf("future selector treated as corruption: %+v", loaded)
	}
	selected := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: initial.Campaign})
	if selected.Status != session.StorageTooNew {
		t.Fatalf("future selector overwritten: %+v", selected)
	}
	after, err := os.ReadFile(selectorPath)
	if err != nil || !bytes.Equal(after, futureSelector) {
		t.Fatalf("future selector modified: %v", err)
	}
	futureSettings := []byte(`{"magic":"TBTD-SETTINGS","schema":2,"changed":{},"additional":123}`)
	settingsPath := filepath.Join(path, "settings.json")
	if err := os.WriteFile(settingsPath, futureSettings, 0600); err != nil {
		t.Fatal(err)
	}
	read := store.processSettings(t.Context(), session.SettingsInput{ID: "settings-load", Kind: session.LoadSettings})
	if read.Status != session.StorageTooNew || read.DefaultsRestored {
		t.Fatalf("future settings reset: %+v", read)
	}
	write := store.processSettings(t.Context(), session.SettingsInput{ID: "settings-save", Kind: session.SaveSettings, Values: settings.Defaults(t.Context())})
	if write.Status != session.StorageTooNew || write.Code != session.SaveTooNew {
		t.Fatalf("future settings write: %+v", write)
	}
	after, err = os.ReadFile(settingsPath)
	if err != nil || !bytes.Equal(after, futureSettings) {
		t.Fatalf("future settings modified: %v", err)
	}
	valid, err := encodeChecked(t.Context(), "TBTD-SETTINGS", settings.Defaults(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, valid, 0600); err != nil {
		t.Fatal(err)
	}
	previousPath := filepath.Join(path, "settings.previous.json")
	if err := os.WriteFile(previousPath, futureSettings, 0600); err != nil {
		t.Fatal(err)
	}
	write = store.processSettings(t.Context(), session.SettingsInput{ID: "previous-save", Kind: session.SaveSettings, Values: settings.Defaults(t.Context())})
	if write.Status != session.StorageTooNew || write.Code != session.SaveTooNew {
		t.Fatalf("future previous settings write: %+v", write)
	}
	after, err = os.ReadFile(previousPath)
	if err != nil || !bytes.Equal(after, futureSettings) {
		t.Fatalf("future previous settings modified: %v", err)
	}
}

func TestFutureProfileSchemaBeyondIntRangeIsPreserved(t *testing.T) {
	store, _, path := profileFixture(t)
	initial := newProfileCampaign(t, store)
	selector := []byte(`{"magic":"TBTD-ACTIVE","schema":184467440737095516160,"future":true}`)
	selectorPath := filepath.Join(path, "active.b.json")
	if err := os.WriteFile(selectorPath, selector, 0600); err != nil {
		t.Fatal(err)
	}
	loaded := store.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
	if loaded.Status != session.StorageTooNew || loaded.Code != session.SaveTooNew {
		t.Fatalf("large future selector schema treated as corrupt: %+v", loaded)
	}
	recovered := store.processProgress(t.Context(), session.ProgressInput{ID: "recover", Kind: session.LoadRevision, Campaign: initial.Campaign, ExpectedRevision: 0})
	if recovered.Status != session.StorageTooNew || recovered.RevisionKnown {
		t.Fatalf("recovery bypassed future selector: %+v", recovered)
	}
	selected := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: initial.Campaign})
	if selected.Status != session.StorageTooNew {
		t.Fatalf("future selector accepted for replacement: %+v", selected)
	}
	after, err := os.ReadFile(selectorPath)
	if err != nil || !bytes.Equal(after, selector) {
		t.Fatalf("future selector modified: %v", err)
	}
	future := []byte(`{"magic":"TBTD-SETTINGS","schema":184467440737095516160,"future":true}`)
	settingsPath := filepath.Join(path, "settings.json")
	if err := os.WriteFile(settingsPath, future, 0600); err != nil {
		t.Fatal(err)
	}
	result := store.processSettings(t.Context(), session.SettingsInput{ID: "save", Kind: session.SaveSettings, Values: settings.Defaults(t.Context())})
	if result.Status != session.StorageTooNew || result.Code != session.SaveTooNew {
		t.Fatalf("large future settings schema treated as corrupt: %+v", result)
	}
	after, err = os.ReadFile(settingsPath)
	if err != nil || !bytes.Equal(after, future) {
		t.Fatalf("future settings modified: %v", err)
	}
}

func TestOversizedSelectorsArePreservedOnExplicitRecovery(t *testing.T) {
	for _, newFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("new-first-%t", newFirst), func(t *testing.T) {
			store, _, path := profileFixture(t)
			initial := newProfileCampaign(t, store)
			originals := [][]byte{bytes.Repeat([]byte("a"), maxProfileFile+17), bytes.Repeat([]byte("b"), maxProfileFile+19)}
			for i, name := range []string{"active.a.json", "active.b.json"} {
				if err := os.WriteFile(filepath.Join(path, name), originals[i], 0600); err != nil {
					t.Fatal(err)
				}
			}
			loaded := store.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
			if loaded.Status != session.StorageCorrupt || len(loaded.Candidates) != 1 || loaded.Candidates[0].Campaign != initial.Campaign {
				t.Fatalf("oversized recovery choices: %+v", loaded)
			}
			selectOld := session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: initial.Campaign}
			newGame := session.ProgressInput{ID: "new", Kind: session.CreateCampaign, Campaign: testCampaignID}
			first, second := selectOld, newGame
			if newFirst {
				first, second = newGame, selectOld
			}
			if result := store.processProgress(t.Context(), first); result.Status != session.StorageSucceeded {
				t.Fatalf("first recovery action: %+v", result)
			}
			if result := store.processProgress(t.Context(), second); result.Status != session.StorageSucceeded {
				t.Fatalf("second recovery action: %+v", result)
			}
			matches, err := filepath.Glob(filepath.Join(path, ".tmp-*.preserved"))
			if err != nil || len(matches) != 2 {
				t.Fatalf("missing preserved selectors: %v (%d)", err, len(matches))
			}
			found := [2]bool{}
			for _, name := range matches {
				data, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				for i, original := range originals {
					found[i] = found[i] || bytes.Equal(data, original)
				}
			}
			if !found[0] || !found[1] {
				t.Fatal("preserved selector bytes differ")
			}
		})
	}
}

func TestOversizedSelectorSymlinkPreservesIndependentBytes(t *testing.T) {
	store, _, path := profileFixture(t)
	initial := newProfileCampaign(t, store)
	original := bytes.Repeat([]byte("z"), maxProfileFile+23)
	target := filepath.Join(path, "selector-source")
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatal(err)
	}
	selectorPath := filepath.Join(path, "active.a.json")
	if err := os.Remove(selectorPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("selector-source", selectorPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	selected := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: initial.Campaign})
	if selected.Status != session.StorageSucceeded {
		t.Fatalf("symlink recovery: %+v", selected)
	}
	matches, err := filepath.Glob(filepath.Join(path, ".tmp-*.preserved"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("preserved copy: %v (%d)", err, len(matches))
	}
	if err := os.WriteFile(target, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("preserved selector follows mutable symlink: %v", err)
	}
}

func TestOversizedSelectorHardlinkPreservesIndependentBytes(t *testing.T) {
	store, _, path := profileFixture(t)
	initial := newProfileCampaign(t, store)
	original := bytes.Repeat([]byte("h"), maxProfileFile+23)
	target := filepath.Join(path, "selector-source")
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatal(err)
	}
	selectorPath := filepath.Join(path, "active.a.json")
	if err := os.Remove(selectorPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, selectorPath); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	selected := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: initial.Campaign})
	if selected.Status != session.StorageSucceeded {
		t.Fatalf("hard-link recovery: %+v", selected)
	}
	matches, err := filepath.Glob(filepath.Join(path, ".tmp-*.preserved"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("preserved copy: %v (%d)", err, len(matches))
	}
	if err := os.WriteFile(target, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("preserved selector shares mutable hard link: %v", err)
	}
}

func TestOversizedSelectorRetainedWhenReplacementFails(t *testing.T) {
	store, _, path := profileFixture(t)
	initial := newProfileCampaign(t, store)
	original := bytes.Repeat([]byte("x"), maxProfileFile+5)
	name := filepath.Join(path, "active.a.json")
	if err := os.WriteFile(name, original, 0600); err != nil {
		t.Fatal(err)
	}
	store.files = &selectorFault{faultFiles: &faultFiles{Root: store.root}}
	result := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: initial.Campaign})
	if result.Status != session.StorageFailed {
		t.Fatalf("failed selector installation acknowledged: %+v", result)
	}
	data, err := os.ReadFile(name)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("original selector lost before replacement: %v", err)
	}
	result = store.processProgress(t.Context(), session.ProgressInput{ID: "retry", Kind: session.SelectCampaign, Campaign: initial.Campaign})
	if result.Status != session.StorageSucceeded || result.Campaign != initial.Campaign {
		t.Fatalf("retry could not recover: %+v", result)
	}
}

type progressGate struct {
	*platform.Root
	once    sync.Once
	entered chan struct{}
	release chan struct{}
	value   chan any
}

func (files *progressGate) WriteTemp(ctx context.Context, dir string, data []byte, max int64) (string, error) {
	if strings.HasPrefix(dir, "campaigns") {
		files.once.Do(func() {
			files.value <- ctx.Value(contextKey{})
			close(files.entered)
			select {
			case <-files.release:
			case <-ctx.Done():
			}
		})
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return files.Root.WriteTemp(ctx, dir, data, max)
}

func TestCampaignCommitDrainsOnCancellationAndClose(t *testing.T) {
	for _, cancelRun := range []bool{true, false} {
		t.Run(fmt.Sprintf("cancel-run-%t", cancelRun), func(t *testing.T) {
			codec, service, _ := fixture(t)
			path := t.TempDir()
			store, err := Open(t.Context(), path, codec, 4, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			initial := newProfileCampaign(t, store)
			proposal, err := service.Prepare(t.Context(), initial.Snapshot, campaign.Transition{CampaignID: string(initial.Campaign), Sequence: 1, Death: campaign.DeathGameplay, DeathDetail: "hazard"})
			if err != nil {
				t.Fatal(err)
			}
			input := session.ProgressInput{ID: "death", Kind: session.CommitProgress, Campaign: initial.Campaign, ExpectedRevision: initial.Revision, ExpectedDigest: initial.PayloadDigest, Operation: session.OperationID{Event: "death-1", Sequence: 1}, Proposal: proposal}
			request, err := session.NewProgressRequest(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			files := &progressGate{Root: store.root, entered: make(chan struct{}), release: make(chan struct{}), value: make(chan any, 1)}
			store.files = files
			revisions, err := store.revisionsFor(t.Context(), initial.Campaign)
			if err != nil {
				t.Fatal(err)
			}
			revisions.files = files
			var release sync.Once
			t.Cleanup(func() {
				release.Do(func() { close(files.release) })
				if err := store.Close(); err != nil {
					t.Error(err)
				}
			})
			initiator, cancelInitiator := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "campaign-trace"))
			defer cancelInitiator()
			if err := store.Submit(initiator, request); err != nil {
				t.Fatal(err)
			}
			finished := make(chan error, 1)
			if cancelRun {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() { finished <- store.Run(ctx) }()
				<-files.entered
				cancelInitiator()
				cancel()
			} else {
				// Close starts the worker when accepted work predates Run.
				go func() { finished <- store.Close() }()
				<-files.entered
				cancelInitiator()
			}
			if value := <-files.value; value != "campaign-trace" {
				t.Fatalf("initiating context lost: %v", value)
			}
			release.Do(func() { close(files.release) })
			if err := <-finished; err != nil {
				t.Fatalf("drain failed: %v", err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			result, err := store.Poll(t.Context(), input.ID)
			got := result.Data(t.Context())
			state := got.Snapshot.Data(t.Context())
			want := proposal.Data(t.Context())
			if err != nil || got.Status != session.StorageSucceeded || got.Campaign != initial.Campaign || got.Operation != input.Operation || got.Revision != 1 || state.DebtCents != 4999 || len(state.Incidents) != 1 || state.Incidents[0].ID != want.Incidents[0].ID || got.Snapshot.Credits() != 0 {
				t.Fatalf("accepted death was lost or duplicated: %+v %v", got, err)
			}
			reopened, err := Open(t.Context(), path, codec, 4, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := reopened.Close(); err != nil {
					t.Error(err)
				}
			})
			loaded := reopened.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
			loadedState := loaded.Snapshot.Data(t.Context())
			if loaded.Status != session.StorageSucceeded || loaded.Campaign != initial.Campaign || loaded.Revision != 1 || loaded.PayloadDigest != got.PayloadDigest || loadedState.DebtCents != 4999 || len(loadedState.Incidents) != 1 || loadedState.Incidents[0].ID != want.Incidents[0].ID {
				t.Fatalf("reopened campaign differs: %+v", loaded)
			}
			if err := reopened.Submit(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
			retry, err := reopened.Poll(t.Context(), input.ID)
			retried := retry.Data(t.Context())
			retriedState := retried.Snapshot.Data(t.Context())
			if err != nil || retried.Status != session.StorageSucceeded || retried.PayloadDigest != got.PayloadDigest || retriedState.DebtCents != 4999 || len(retriedState.Incidents) != 1 || retriedState.Incidents[0].ID != want.Incidents[0].ID {
				t.Fatalf("retry duplicated incident: %+v %v", retried, err)
			}
		})
	}
}

func TestConcurrentSubmissionAndPolling(t *testing.T) {
	store, _, _ := profileFixture(t)
	files := &gateFiles{Root: store.root, entered: make(chan struct{}), release: make(chan struct{}), value: make(chan any, 1)}
	store.files = files
	run := make(chan error, 1)
	go func() { run <- store.Run(context.Background()) }()
	view := store.Settings()
	if err := view.Submit(t.Context(), settingsRequest(t, "handshake", session.LoadSettings)); err != nil {
		t.Fatal(err)
	}
	<-files.entered
	close(files.release)
	var group sync.WaitGroup
	completed := make([]bool, 16)
	errorsSeen := make(chan error, 16)
	for i := 0; i < 16; i++ {
		id := session.RequestID(fmt.Sprintf("request-%d", i))
		request := settingsRequest(t, id, session.LoadSettings)
		group.Add(1)
		go func() {
			defer group.Done()
			if err := view.Submit(context.Background(), request); err != nil {
				errorsSeen <- err
				return
			}
			result, err := view.Poll(context.Background(), id)
			data := result.Data(context.Background())
			if err != nil || data.ID != id || (data.Status != session.StoragePending && data.Status != session.StorageSucceeded) {
				errorsSeen <- fmt.Errorf("poll %s: %w", id, err)
				return
			}
			completed[i] = data.Status == session.StorageSucceeded
		}()
	}
	group.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Error(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-run; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		if completed[i] {
			continue
		}
		id := session.RequestID(fmt.Sprintf("request-%d", i))
		result, err := view.Poll(t.Context(), id)
		if err != nil || result.Data(t.Context()).Status != session.StorageSucceeded {
			t.Fatalf("result %s: %+v %v", id, result.Data(t.Context()), err)
		}
	}
}

type settlementDeadlineFault struct {
	*platform.Root
	failed       bool
	opDeadline   time.Time
	rearmStarted chan time.Time
}

func (files *settlementDeadlineFault) ReplaceFile(ctx context.Context, source, destination string) (platform.ReplaceResult, error) {
	if destination == selectorGuardName && !files.failed {
		data, err := files.Root.ReadFile(ctx, source, maxProfileFile)
		if err != nil {
			return platform.ReplaceResult{}, err
		}
		var record selectorIntent
		if err := decodeChecked(ctx, data, "TBTD-SELECTOR-INTENT", &record); err != nil {
			return platform.ReplaceResult{}, err
		}
		if !record.Pending {
			files.opDeadline, _ = ctx.Deadline()
			result, err := files.Root.ReplaceFile(ctx, source, destination)
			if err != nil {
				return result, err
			}
			files.failed = true
			return result, errInjected
		}
	}
	return files.Root.ReplaceFile(ctx, source, destination)
}

func (files *settlementDeadlineFault) WriteTemp(ctx context.Context, dir string, data []byte, max int64) (string, error) {
	if files.failed {
		var record selectorIntent
		if decodeChecked(ctx, data, "TBTD-SELECTOR-INTENT", &record) == nil && record.Pending {
			deadline, _ := ctx.Deadline()
			select {
			case files.rearmStarted <- deadline:
			default:
			}
			<-ctx.Done()
			return "", ctx.Err()
		}
	}
	return files.Root.WriteTemp(ctx, dir, data, max)
}

func TestSelectorRecoveryUsesOwnedDeadline(t *testing.T) {
	codec, _, _ := fixture(t)
	path := t.TempDir()
	budget := 300 * time.Millisecond
	store, err := Open(t.Context(), path, codec, 4, budget)
	if err != nil {
		t.Fatal(err)
	}
	first := newProfileCampaign(t, store)
	files := &settlementDeadlineFault{Root: store.root, rearmStarted: make(chan time.Time, 2)}
	store.files = files
	request, err := session.NewProgressRequest(t.Context(), session.ProgressInput{ID: "new", Kind: session.CreateCampaign, Campaign: testCampaignID})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Submit(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	run := make(chan error, 1)
	go func() { run <- store.Run(context.Background()) }()
	var recoveryDeadline time.Time
	select {
	case recoveryDeadline = <-files.rearmStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("settlement did not reach re-arm")
	}
	if recoveryDeadline.IsZero() || !recoveryDeadline.Equal(files.opDeadline) {
		t.Errorf("re-arm restarted deadline: operation=%v recovery=%v", files.opDeadline, recoveryDeadline)
	}
	closed := make(chan error, 1)
	go func() { closed <- store.Close() }()
	select {
	case err := <-run:
		if err == nil {
			t.Error("unresolved worker exit not reported")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker exceeded owned lifetime")
	}
	if err := <-closed; err == nil {
		t.Fatal("shutdown hid unresolved selection")
	}
	result, err := store.Poll(t.Context(), "new")
	created := result.Data(t.Context())
	if err != nil || created.Status != session.StorageAmbiguous || created.RevisionKnown || created.Campaign == first.Campaign {
		t.Fatalf("selection acknowledged despite deadline: %+v %v", created, err)
	}
	reopened, err := Open(t.Context(), path, codec, 4, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	loaded := reopened.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
	if loaded.Status != session.StorageAmbiguous || loaded.Campaign != "" || loaded.RevisionKnown || len(loaded.Candidates) != 2 {
		t.Fatalf("deadline silently selected a campaign: %+v", loaded)
	}
}

func TestSelectorRecoveryHonorsEarlierDrainDeadline(t *testing.T) {
	codec, _, _ := fixture(t)
	store, err := Open(t.Context(), t.TempDir(), codec, 4, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	newProfileCampaign(t, store)
	files := &settlementDeadlineFault{Root: store.root, rearmStarted: make(chan time.Time, 2)}
	store.files = files
	request, err := session.NewProgressRequest(t.Context(), session.ProgressInput{ID: "new", Kind: session.CreateCampaign, Campaign: testCampaignID})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Submit(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	// Establish a shorter drain before starting the queued operation. This
	// test-only adjustment makes the two deadlines observably distinct.
	store.stop()
	store.capacity = 4 * time.Second
	closed := make(chan error, 1)
	go func() { closed <- store.Close() }()
	select {
	case <-files.rearmStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("settlement did not reach re-arm")
	}
	select {
	case <-store.done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker outlived the drain deadline")
	}
	if err := <-closed; err == nil {
		t.Fatal("deadline and unresolved selection not reported")
	}
	result, err := store.Poll(t.Context(), "new")
	if err != nil || result.Data(t.Context()).Status != session.StorageAmbiguous || result.Data(t.Context()).RevisionKnown {
		t.Fatalf("deadline acknowledged selection: %+v %v", result.Data(t.Context()), err)
	}
}

type fencePanicFiles struct {
	*platform.Root
	store         *Store
	beforeReplace bool
	observed      chan session.StorageStatus
}

func (files *fencePanicFiles) ReplaceFile(ctx context.Context, source, destination string) (platform.ReplaceResult, error) {
	if destination == selectorFenceName {
		data, err := files.Root.ReadFile(ctx, source, maxProfileFile)
		if err != nil {
			return platform.ReplaceResult{}, err
		}
		var record selectorIntent
		if err := decodeChecked(ctx, data, "TBTD-SELECTOR-INTENT", &record); err != nil {
			return platform.ReplaceResult{}, err
		}
		if !record.Pending {
			files.store.mu.Lock()
			status := files.store.progress["new"].Data(ctx).Status
			files.store.mu.Unlock()
			files.observed <- status
			if !files.beforeReplace {
				result, err := files.Root.ReplaceFile(ctx, source, destination)
				if err != nil {
					return result, err
				}
			}
			panic("injected fence cleanup failure")
		}
	}
	return files.Root.ReplaceFile(ctx, source, destination)
}

func TestFenceCleanupFollowsPublishedOutcome(t *testing.T) {
	for _, before := range []bool{true, false} {
		t.Run(fmt.Sprintf("before-replace-%t", before), func(t *testing.T) {
			codec, _, _ := fixture(t)
			path := t.TempDir()
			store, err := Open(t.Context(), path, codec, 4, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			first := newProfileCampaign(t, store)
			files := &fencePanicFiles{Root: store.root, store: store, beforeReplace: before, observed: make(chan session.StorageStatus, 1)}
			store.files = files
			request, err := session.NewProgressRequest(t.Context(), session.ProgressInput{ID: "new", Kind: session.CreateCampaign, Campaign: testCampaignID})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Submit(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			if err := store.Run(t.Context()); err == nil {
				t.Fatal("cleanup panic was hidden")
			}
			if status := <-files.observed; status != session.StorageSucceeded {
				t.Fatalf("fence cleared before result publication: %s", status)
			}
			result, err := store.Poll(t.Context(), "new")
			created := result.Data(t.Context())
			if err != nil || created.Status != session.StorageSucceeded || created.Campaign == first.Campaign || !created.RevisionKnown {
				t.Fatalf("published result replaced by cleanup failure: %+v %v", created, err)
			}
			reopened, err := Open(t.Context(), path, codec, 4, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := reopened.Close(); err != nil {
					t.Error(err)
				}
			})
			loaded := reopened.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
			if before {
				if loaded.Status != session.StorageAmbiguous || loaded.Campaign != "" || len(loaded.Candidates) != 2 {
					t.Fatalf("pending fence was ignored: %+v", loaded)
				}
			} else if loaded.Status != session.StorageSucceeded || loaded.Campaign != created.Campaign || loaded.Snapshot.Credits() != 0 {
				t.Fatalf("published selection not recovered: %+v", loaded)
			}
		})
	}
}

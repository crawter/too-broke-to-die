// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
)

func TestInvalidSelectorIntentAllowsExplicitRecovery(t *testing.T) {
	for _, name := range []string{selectorIntentName, selectorGuardName} {
		for _, oversized := range []bool{false, true} {
			for _, newGame := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/oversized-%t/new-%t", name, oversized, newGame), func(t *testing.T) {
					store, _, path := profileFixture(t)
					first := newProfileCampaign(t, store)
					second := newProfileCampaign(t, store)
					damaged := []byte("truncated intent")
					if oversized {
						damaged = bytes.Repeat([]byte("q"), maxProfileFile+13)
					}
					intentPath := filepath.Join(path, name)
					if err := os.WriteFile(intentPath, damaged, 0600); err != nil {
						t.Fatal(err)
					}
					loaded := store.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
					if loaded.Status != session.StorageCorrupt || loaded.Campaign != "" || len(loaded.Candidates) != 2 {
						t.Fatalf("damaged intent silently selected a campaign: %+v", loaded)
					}
					request := session.ProgressInput{ID: "choose", Kind: session.SelectCampaign, Campaign: first.Campaign}
					if newGame {
						request.Kind = session.CreateCampaign
					}
					selected := store.processProgress(t.Context(), request)
					if selected.Status != session.StorageSucceeded || selected.Snapshot.Credits() != 0 {
						t.Fatalf("explicit recovery: %+v", selected)
					}
					if (!newGame && selected.Campaign != first.Campaign) || (newGame && (selected.Campaign == first.Campaign || selected.Campaign == second.Campaign)) {
						t.Fatalf("wrong selected identity: %+v", selected)
					}
					matches, err := filepath.Glob(filepath.Join(path, ".tmp-*.preserved"))
					if err != nil || len(matches) != 1 {
						t.Fatalf("missing preserved intent: %v (%d)", err, len(matches))
					}
					copy, err := os.ReadFile(matches[0])
					if err != nil || !bytes.Equal(copy, damaged) {
						t.Fatalf("preserved intent differs: %v", err)
					}
					loaded = store.processProgress(t.Context(), session.ProgressInput{ID: "load-again", Kind: session.LoadActive})
					if loaded.Status != session.StorageSucceeded || loaded.Campaign != selected.Campaign || loaded.Revision != 0 {
						t.Fatalf("recovery did not install selection: %+v", loaded)
					}
					if err := store.Close(); err != nil {
						t.Fatal(err)
					}
					reopened, err := Open(t.Context(), path, store.codec, 4, 5*time.Second)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if err := reopened.Close(); err != nil {
							t.Error(err)
						}
					})
					loaded = reopened.processProgress(t.Context(), session.ProgressInput{ID: "reopen", Kind: session.LoadActive})
					if loaded.Status != session.StorageSucceeded || loaded.Campaign != selected.Campaign || loaded.Snapshot.Credits() != 0 {
						t.Fatalf("reopened selection differs: %+v", loaded)
					}
				})
			}
		}
	}
}

func TestIntentRecoveryPreservesBeforeReplacement(t *testing.T) {
	store, _, path := profileFixture(t)
	initial := newProfileCampaign(t, store)
	damaged := []byte("damaged intent")
	name := filepath.Join(path, selectorIntentName)
	if err := os.WriteFile(name, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	store.files = &faultFiles{Root: store.root, stage: "disk full"}
	result := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: initial.Campaign})
	if result.Status != session.StorageFailed || result.Code != session.StorageFull {
		t.Fatalf("preservation failure acknowledged: %+v", result)
	}
	data, err := os.ReadFile(name)
	if err != nil || !bytes.Equal(data, damaged) {
		t.Fatalf("invalid intent lost when preservation failed: %v", err)
	}
	result = store.processProgress(t.Context(), session.ProgressInput{ID: "retry", Kind: session.SelectCampaign, Campaign: initial.Campaign})
	if result.Status != session.StorageSucceeded || result.Campaign != initial.Campaign {
		t.Fatalf("preservation retry: %+v", result)
	}
}

func TestFutureSelectorIntentIsNeverReplaced(t *testing.T) {
	for _, name := range []string{selectorIntentName, selectorGuardName} {
		t.Run(name, func(t *testing.T) {
			store, _, path := profileFixture(t)
			initial := newProfileCampaign(t, store)
			future := []byte(`{"magic":"TBTD-SELECTOR-INTENT","schema":2,"future":true}`)
			filename := filepath.Join(path, name)
			if err := os.WriteFile(filename, future, 0600); err != nil {
				t.Fatal(err)
			}
			result := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: initial.Campaign})
			if result.Status != session.StorageTooNew || result.Code != session.SaveTooNew {
				t.Fatalf("future intent overwritten: %+v", result)
			}
			data, err := os.ReadFile(filename)
			if err != nil || !bytes.Equal(data, future) {
				t.Fatalf("future intent changed: %v", err)
			}
		})
	}
}

func TestFutureIntentTakesPrecedenceOverDamagedGuard(t *testing.T) {
	store, _, path := profileFixture(t)
	initial := newProfileCampaign(t, store)
	future := []byte(`{"magic":"TBTD-SELECTOR-INTENT","schema":2,"future":true}`)
	if err := os.WriteFile(filepath.Join(path, selectorIntentName), future, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, selectorGuardName), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded := store.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
	if loaded.Status != session.StorageTooNew || loaded.Code != session.SaveTooNew {
		t.Fatalf("future intent treated as corruption: %+v", loaded)
	}
	selected := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: initial.Campaign})
	if selected.Status != session.StorageTooNew {
		t.Fatalf("future intent bypassed: %+v", selected)
	}
	data, err := os.ReadFile(filepath.Join(path, selectorIntentName))
	if err != nil || !bytes.Equal(data, future) {
		t.Fatalf("future intent modified: %v", err)
	}
}

type settlementFailure struct {
	*platform.Root
	target    string
	stage     string
	installed bool
	fired     bool
}

func (files *settlementFailure) ReplaceFile(ctx context.Context, source, destination string) (platform.ReplaceResult, error) {
	settling := false
	if destination == files.target && !files.fired {
		data, err := files.Root.ReadFile(ctx, source, maxProfileFile)
		if err != nil {
			return platform.ReplaceResult{}, err
		}
		var intent selectorIntent
		if err := decodeChecked(ctx, data, "TBTD-SELECTOR-INTENT", &intent); err != nil {
			return platform.ReplaceResult{}, err
		}
		settling = !intent.Pending
	}
	result, err := files.Root.ReplaceFile(ctx, source, destination)
	if err == nil && settling {
		files.installed = true
		if files.stage == "replacement" {
			files.fired = true
			return result, errInjected
		}
	}
	return result, err
}

func (files *settlementFailure) SyncFile(ctx context.Context, name string) error {
	if name == files.target && files.installed && !files.fired && files.stage == "flush" {
		files.fired = true
		return errInjected
	}
	return files.Root.SyncFile(ctx, name)
}

func (files *settlementFailure) ReadFile(ctx context.Context, name string, max int64) ([]byte, error) {
	if name == files.target && files.installed && !files.fired && files.stage == "validation" {
		files.fired = true
		return nil, errInjected
	}
	return files.Root.ReadFile(ctx, name, max)
}

type rearmFailure struct {
	*settlementFailure
	failed bool
}

func (files *rearmFailure) ReplaceFile(ctx context.Context, source, destination string) (platform.ReplaceResult, error) {
	if destination == selectorIntentName && files.fired && !files.failed {
		data, err := files.Root.ReadFile(ctx, source, maxProfileFile)
		if err != nil {
			return platform.ReplaceResult{}, err
		}
		var intent selectorIntent
		if err := decodeChecked(ctx, data, "TBTD-SELECTOR-INTENT", &intent); err != nil {
			return platform.ReplaceResult{}, err
		}
		if intent.Pending {
			files.failed = true
			return platform.ReplaceResult{}, errInjected
		}
	}
	return files.settlementFailure.ReplaceFile(ctx, source, destination)
}

func TestGuardIsRearmedWhenIntentRearmFails(t *testing.T) {
	codec, _, _ := fixture(t)
	path := t.TempDir()
	store, err := Open(t.Context(), path, codec, 4, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first := newProfileCampaign(t, store)
	fault := &rearmFailure{settlementFailure: &settlementFailure{Root: store.root, target: selectorGuardName, stage: "replacement"}}
	store.files = fault
	created := store.processProgress(t.Context(), session.ProgressInput{ID: "new", Kind: session.CreateCampaign, Campaign: testCampaignID})
	if created.Status != session.StorageAmbiguous || !fault.fired || !fault.failed {
		t.Fatalf("expected both failures: %+v, settlement=%t, rearm=%t", created, fault.fired, fault.failed)
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
	if loaded.Status != session.StorageAmbiguous || loaded.Campaign != "" || len(loaded.Candidates) != 2 {
		t.Fatalf("guard rearm did not block silent selection: %+v", loaded)
	}
	selected := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: first.Campaign})
	if selected.Status != session.StorageSucceeded || selected.Campaign != first.Campaign {
		t.Fatalf("explicit selection failed: %+v", selected)
	}
}

func TestSettlingIntentFailureRequiresRecoveryAfterReopen(t *testing.T) {
	for _, target := range []string{selectorIntentName, selectorGuardName} {
		for _, stage := range []string{"replacement", "flush", "validation"} {
			for _, chooseNew := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/choose-new-%t", target, stage, chooseNew), func(t *testing.T) {
					codec, _, _ := fixture(t)
					path := t.TempDir()
					store, err := Open(t.Context(), path, codec, 4, 5*time.Second)
					if err != nil {
						t.Fatal(err)
					}
					first := newProfileCampaign(t, store)
					fault := &settlementFailure{Root: store.root, target: target, stage: stage}
					store.files = fault
					created := store.processProgress(t.Context(), session.ProgressInput{ID: "new", Kind: session.CreateCampaign, Campaign: testCampaignID})
					if !fault.fired || created.Status != session.StorageAmbiguous || created.Campaign == first.Campaign || created.RevisionKnown {
						t.Fatalf("settlement failure acknowledged: %+v, fired=%t", created, fault.fired)
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
						t.Fatalf("explicit recovery: %+v", selected)
					}
					loaded = store.processProgress(t.Context(), session.ProgressInput{ID: "load-again", Kind: session.LoadActive})
					if loaded.Status != session.StorageSucceeded || loaded.Campaign != chosen || loaded.Revision != 0 {
						t.Fatalf("selection not restored: %+v", loaded)
					}
					if err := store.Close(); err != nil {
						t.Fatal(err)
					}
					store, err = Open(t.Context(), path, codec, 4, 5*time.Second)
					if err != nil {
						t.Fatal(err)
					}
					loaded = store.processProgress(t.Context(), session.ProgressInput{ID: "confirmed", Kind: session.LoadActive})
					if loaded.Status != session.StorageSucceeded || loaded.Campaign != chosen || loaded.Revision != 0 {
						t.Fatalf("confirmed selection did not survive reopen: %+v", loaded)
					}
				})
			}
		}
	}
}

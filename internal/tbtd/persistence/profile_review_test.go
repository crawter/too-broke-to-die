// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"too-broke-to-die/internal/tbtd/session"
)

func TestInspectActiveDoesNotChangeProfile(t *testing.T) {
	store, _, path := profileFixture(t)
	created := newProfileCampaign(t, store)
	files := []string{"active.a.json", "active.b.json", "selector.intent.json", "selector.guard.json", "selector.fence.json", filepath.Join("campaigns", string(created.Campaign), "progress.a.json"), filepath.Join("campaigns", string(created.Campaign), "progress.b.json")}
	before := make(map[string][]byte)
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(path, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		before[name] = data
	}
	result := store.processProgress(t.Context(), session.ProgressInput{ID: "inspect", Kind: session.InspectActive})
	if result.Status != session.StorageSucceeded || result.Campaign != created.Campaign || result.Revision != created.Revision {
		t.Fatalf("inspection: %+v", result)
	}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(path, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if !bytes.Equal(data, before[name]) {
			t.Fatalf("inspection modified %s", name)
		}
	}
	store.selectorPending = &pendingSelector{}
	result = store.processProgress(t.Context(), session.ProgressInput{ID: "blocked", Kind: session.InspectActive})
	if result.Status != session.StorageAmbiguous || store.selectorPending == nil {
		t.Fatalf("inspection resolved pending selector: %+v", result)
	}
	store.selectorPending = nil
}

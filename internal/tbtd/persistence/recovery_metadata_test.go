// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"os"
	"path/filepath"
	"testing"

	"too-broke-to-die/internal/tbtd/session"
)

func TestBothInvalidMetadataRequiresTwoCorruptSlots(t *testing.T) {
	for _, both := range []bool{false, true} {
		t.Run(map[bool]string{false: "one", true: "both"}[both], func(t *testing.T) {
			store, _, path := profileFixture(t)
			initial := newProfileCampaign(t, store)
			revisions, err := store.revisionsFor(t.Context(), initial.Campaign)
			if err != nil {
				t.Fatal(err)
			}
			inspection, err := revisions.Inspect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for index, slot := range inspection.Slots {
				if index == 1 && !both {
					continue
				}
				if err = os.WriteFile(filepath.Join(path, slot.Name), []byte("damaged"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result := store.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
			if result.Status != session.StorageCorrupt || result.BothRevisionsInvalid != both {
				t.Fatalf("corrupt result: %+v", result)
			}
		})
	}
}

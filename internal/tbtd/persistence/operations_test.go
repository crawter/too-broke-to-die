// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/session"
)

func TestLostAcknowledgmentRecoveryWithUnavailablePredecessor(t *testing.T) {
	for _, damage := range []string{"corrupt", "missing", "too-new", "incompatible"} {
		t.Run(damage, func(t *testing.T) {
			store, files, service, path, initial := revisionStore(t)
			_, _, manifest := fixture(t)
			initialize(t, store, initial)
			active := prepare(t, service, initial.Snapshot, campaign.Transition{Attempt: &campaign.AttemptChange{Action: campaign.AttemptAccept, Deed: "D-HOS-SUP-01"}})
			first, err := commitTest(t.Context(), store, 0, session.OperationID{Event: "accept", Sequence: 1}, active)
			if err != nil {
				t.Fatal(err)
			}
			inspection, err := store.Inspect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			var predecessor string
			for _, slot := range inspection.Slots {
				if slot.Err != nil {
					t.Fatal(slot.Err)
				}
				if slot.Revision.Number == 1 {
					predecessor = slot.Name
				}
			}
			if predecessor == "" {
				t.Fatal("predecessor slot not found")
			}
			event := candidate(t, manifest, "D-HOS-SUP-01", 1)
			proposal := prepare(t, service, active, campaign.Transition{Certification: &campaign.CertificationCandidate{Completed: true, Event: event}, Death: campaign.DeathGameplay, DeathDetail: "hazard"})
			operation := session.OperationID{Event: "D-HOS-SUP-01", Sequence: 2}
			files.stage = "acknowledgment"
			pending, err := store.Commit(t.Context(), 1, first.PayloadDigest, operation, proposal)
			if !errors.Is(err, errInjected) || pending.Status != session.StorageAmbiguous {
				t.Fatalf("expected lost acknowledgment: %#v %v", pending, err)
			}

			predecessorPath := filepath.Join(path, predecessor)
			original, err := os.ReadFile(predecessorPath)
			if err != nil {
				t.Fatal(err)
			}
			var damaged []byte
			switch damage {
			case "corrupt":
				damaged = []byte("truncated predecessor")
			case "too-new", "incompatible":
				var env envelope
				if err := json.Unmarshal(original, &env); err != nil {
					t.Fatal(err)
				}
				if damage == "too-new" {
					env.Schema = SchemaVersion + 1
				} else {
					env.Compatibility.ManifestDigest = strings.Repeat("b", 64)
				}
				damaged, err = json.Marshal(env)
				if err != nil {
					t.Fatal(err)
				}
			}
			if damage == "missing" {
				if err := os.Remove(predecessorPath); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(predecessorPath, damaged, 0600); err != nil {
				t.Fatal(err)
			}

			// The interrupted writer is not used again; only disk and the
			// original prepared request survive into the new store.
			restarted, err := newRevisions(t.Context(), files, store.codec, testCampaignID)
			if err != nil {
				t.Fatal(err)
			}
			inspection, err = restarted.Inspect(t.Context())
			if err != nil || inspection.Pending {
				t.Fatalf("restart inspection: %#v %v", inspection, err)
			}
			var installed string
			for _, slot := range inspection.Slots {
				if slot.Err == nil && slot.Revision.Number == 2 {
					installed = slot.Name
				}
			}
			if installed == "" {
				t.Fatal("installed result not found")
			}
			installedPath := filepath.Join(path, installed)
			before, err := os.ReadFile(installedPath)
			if err != nil {
				t.Fatal(err)
			}

			if damage == "too-new" || damage == "incompatible" {
				want := session.SaveTooNew
				if damage == "incompatible" {
					want = session.ContentIncompatible
				}
				if _, err := restarted.AcceptRecovery(t.Context(), 2); !errors.Is(err, want) {
					t.Fatalf("unsupported predecessor treated as corruption: %v", err)
				}
				result, err := restarted.Commit(t.Context(), 1, first.PayloadDigest, operation, proposal)
				if !errors.Is(err, want) || result.Status == session.StorageSucceeded {
					t.Fatalf("unsupported predecessor bypassed: %#v %v", result, err)
				}
				actual, err := os.ReadFile(predecessorPath)
				if err != nil || !bytes.Equal(actual, damaged) {
					t.Fatalf("unsupported predecessor changed: %v", err)
				}
				return
			}

			result, err := restarted.Commit(t.Context(), 1, first.PayloadDigest, operation, proposal)
			if err == nil || result.Status != session.StorageCorrupt {
				t.Fatalf("recovery was not explicit: %#v %v", result, err)
			}
			recovered, err := restarted.AcceptRecovery(t.Context(), 2)
			if err != nil || recovered.PayloadDigest != pending.PayloadDigest || recovered.Snapshot.Credits() != 1 {
				t.Fatalf("recovery choice: %#v %v", recovered, err)
			}
			wrongID := session.OperationID{Event: "other", Sequence: operation.Sequence}
			result, err = restarted.Commit(t.Context(), 1, first.PayloadDigest, wrongID, proposal)
			if !errors.Is(err, session.StorageConflict) || result.Status != session.StorageConflicting {
				t.Fatalf("changed identity accepted: %#v %v", result, err)
			}
			changed := prepare(t, service, active, campaign.Transition{Certification: &campaign.CertificationCandidate{Completed: true, Event: event}, Death: campaign.DeathGameplay, DeathDetail: "different hazard"})
			result, err = restarted.Commit(t.Context(), 1, first.PayloadDigest, operation, changed)
			if !errors.Is(err, session.StorageConflict) || result.Status != session.StorageConflicting {
				t.Fatalf("changed payload accepted: %#v %v", result, err)
			}
			result, err = restarted.Commit(t.Context(), 1, "invalid", operation, proposal)
			if !errors.Is(err, session.StorageConflict) || result.Status != session.StorageConflicting {
				t.Fatalf("invalid predecessor identity accepted: %#v %v", result, err)
			}

			// Confirmation must still flush and validate the installed result.
			files.stage = "installed sync"
			result, err = restarted.Commit(t.Context(), 1, first.PayloadDigest, operation, proposal)
			if !errors.Is(err, errInjected) || result.Status != session.StorageAmbiguous {
				t.Fatalf("failed flush acknowledged: %#v %v", result, err)
			}
			result, err = restarted.Commit(t.Context(), 1, first.PayloadDigest, operation, proposal)
			if err != nil || result.Status != session.StorageSucceeded || result.PayloadDigest != pending.PayloadDigest || result.Revision.Snapshot.Credits() != 1 || result.Revision.Snapshot.Data(t.Context()).DebtCents != 4999 || len(result.Revision.Snapshot.Data(t.Context()).Incidents) != 1 {
				t.Fatalf("installed result not resolved exactly once: %#v %v", result, err)
			}
			repeated, err := restarted.Commit(t.Context(), 1, first.PayloadDigest, operation, proposal)
			if err != nil || repeated.Status != session.StorageSucceeded || repeated.PayloadDigest != pending.PayloadDigest {
				t.Fatalf("recovery choice lost after confirmation retry: %#v %v", repeated, err)
			}
			after, err := os.ReadFile(installedPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("installed revision changed: %v", err)
			}
			actual, err := os.ReadFile(predecessorPath)
			if damage == "missing" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("missing predecessor recreated: %v", err)
				}
			} else if err != nil || !bytes.Equal(actual, damaged) {
				t.Fatalf("damaged predecessor changed: %v", err)
			}
			next, nextID := deathProposal(t, service, proposal, "next death")
			result, err = restarted.Commit(t.Context(), 2, pending.PayloadDigest, nextID, next)
			if err != nil || result.Status != session.StorageSucceeded || result.Revision.Number != 3 || result.Revision.Snapshot.Credits() != 1 || result.Revision.Snapshot.Data(t.Context()).DebtCents != 9998 {
				t.Fatalf("next operation after recovery: %#v %v", result, err)
			}
			if damage == "corrupt" {
				entries, err := os.ReadDir(filepath.Join(path, store.dir, "damaged"))
				if err != nil || len(entries) != 1 {
					t.Fatalf("damaged predecessor not preserved: %v", err)
				}
				copy, err := os.ReadFile(filepath.Join(path, store.dir, "damaged", entries[0].Name()))
				if err != nil || !bytes.Equal(copy, damaged) {
					t.Fatalf("preserved predecessor differs: %v", err)
				}
			}
		})
	}
}

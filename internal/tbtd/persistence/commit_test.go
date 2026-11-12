// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
)

type faultFiles struct {
	*platform.Root
	stage     string
	installed bool
}

var errInjected = errors.New("injected filesystem failure")

func (files *faultFiles) WriteTemp(ctx context.Context, dir string, data []byte, max int64) (string, error) {
	stage := files.stage
	switch stage {
	case "create":
		files.stage = ""
		return "", errInjected
	case "disk full":
		files.stage = ""
		return "", syscall.ENOSPC
	case "denied":
		files.stage = ""
		return "", os.ErrPermission
	case "partial", "short":
		files.stage = ""
		if _, err := files.Root.WriteTemp(ctx, dir, data[:len(data)/2], max); err != nil {
			return "", err
		}
		return "", errors.Join(errInjected, io.ErrShortWrite)
	case "sync", "close":
		files.stage = ""
		if _, err := files.Root.WriteTemp(ctx, dir, data, max); err != nil {
			return "", err
		}
		return "", errInjected
	default:
		return files.Root.WriteTemp(ctx, dir, data, max)
	}
}

func (files *faultFiles) ReadFile(ctx context.Context, name string, max int64) ([]byte, error) {
	data, err := files.Root.ReadFile(ctx, name, max)
	if err != nil {
		return data, err
	}
	if files.stage == "temporary validation" && strings.HasPrefix(filepath.Base(name), ".tmp-") {
		files.stage = ""
		return []byte("bad temporary"), nil
	}
	if files.stage == "installed validation" && files.installed && strings.HasPrefix(filepath.Base(name), "progress.") {
		files.stage = ""
		return []byte("bad installed"), nil
	}
	return data, nil
}

func (files *faultFiles) SyncFile(ctx context.Context, name string) error {
	if files.stage == "installed sync" && files.installed {
		files.stage = ""
		return errInjected
	}
	return files.Root.SyncFile(ctx, name)
}

func (files *faultFiles) MkdirAll(ctx context.Context, name string) error {
	if files.stage == "directory flush" && files.installed {
		files.stage = ""
		return errInjected
	}
	return files.Root.MkdirAll(ctx, name)
}

func (files *faultFiles) ReplaceFile(ctx context.Context, source, destination string) (platform.ReplaceResult, error) {
	if files.stage == "replacement" {
		files.stage = ""
		return platform.ReplaceResult{}, errInjected
	}
	result, err := files.Root.ReplaceFile(ctx, source, destination)
	if err == nil && result.Installed && strings.HasPrefix(filepath.Base(destination), "progress.") {
		files.installed = true
		if files.stage == "rename flush" {
			files.stage = ""
			return result, errInjected
		}
	}
	return result, err
}

func revisionStore(t *testing.T) (*Revisions, *faultFiles, *campaign.Campaign, string, Revision) {
	t.Helper()
	codec, service, _ := fixture(t)
	path := t.TempDir()
	root, err := platform.OpenRoot(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	lock, err := root.LockProfile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Close(); err != nil {
			t.Error(err)
		}
	})
	files := &faultFiles{Root: root}
	store, err := newRevisions(t.Context(), files, codec, testCampaignID, afterInstall(func() error {
		if files.stage == "acknowledgment" {
			files.stage = ""
			return errInjected
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Start(t.Context(), campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	initial := revision(codec, snapshot)
	initial.Number = 0
	return store, files, service, path, initial
}

func initialize(t *testing.T, store *Revisions, initial Revision) {
	t.Helper()
	result, err := store.Initialize(t.Context(), initial)
	if err != nil || result.Status != session.StorageSucceeded || result.Revision.Number != 0 {
		t.Fatalf("initialize: %#v, %v", result, err)
	}
}

// commitTest is for tests whose proposal was prepared from the installed slot.
// The branch regression below supplies the digest captured at preparation directly.
func commitTest(ctx context.Context, store *Revisions, expected uint64, operation session.OperationID, proposal campaign.Snapshot) (CommitResult, error) {
	store.mu.Lock()
	digest := ""
	if store.pending != nil && store.pending.expected == expected {
		digest = store.pending.expectedDigest
	} else {
		for _, slot := range store.inspect(ctx).Slots {
			if slot.Err == nil && slot.Revision.Number == expected {
				digest = slot.Revision.PayloadDigest
			}
		}
	}
	store.mu.Unlock()
	return store.Commit(ctx, expected, digest, operation, proposal)
}

func deathProposal(t *testing.T, service *campaign.Campaign, snapshot campaign.Snapshot, detail string) (campaign.Snapshot, session.OperationID) {
	t.Helper()
	proposal := prepare(t, service, snapshot, campaign.Transition{Death: campaign.DeathGameplay, DeathDetail: detail})
	return proposal, session.OperationID{Event: fmt.Sprintf("death-%d", proposal.Data(t.Context()).OperationSequence), Sequence: proposal.Data(t.Context()).OperationSequence}
}

func TestInitialSlotsAndAlternatingCommits(t *testing.T) {
	store, _, service, _, initial := revisionStore(t)
	initialize(t, store, initial)
	inspection, err := store.Inspect(t.Context())
	if err != nil || inspection.Slots[0].Err != nil || inspection.Slots[1].Err != nil || !sameRevision(inspection.Slots[0].Revision, inspection.Slots[1].Revision) {
		t.Fatalf("initial slots: %#v %v", inspection, err)
	}
	snapshot := initial.Snapshot
	for number := uint64(1); number <= 3; number++ {
		proposal, operation := deathProposal(t, service, snapshot, "hazard")
		result, err := commitTest(t.Context(), store, number-1, operation, proposal)
		if err != nil || result.Status != session.StorageSucceeded || result.Revision.Number != number || result.Revision.Snapshot.Data(t.Context()).DebtCents != int64(number)*4999 {
			t.Fatalf("commit %d: %#v %v", number, result, err)
		}
		inspection, _ = store.Inspect(t.Context())
		if inspection.Slots[0].Err != nil || inspection.Slots[1].Err != nil {
			t.Fatalf("invalid slots: %#v", inspection)
		}
		a, b := inspection.Slots[0].Revision.Number, inspection.Slots[1].Revision.Number
		if !((a == number && b == number-1) || (b == number && a == number-1)) {
			t.Fatalf("missing predecessor: %d, %d", a, b)
		}
		repeated, err := commitTest(t.Context(), store, number-1, operation, proposal)
		if err != nil || repeated.PayloadDigest != result.PayloadDigest || repeated.Revision.Snapshot.Data(t.Context()).DebtCents != int64(number)*4999 {
			t.Fatalf("duplicate: %#v %v", repeated, err)
		}
		changed, _ := deathProposal(t, service, snapshot, "different hazard")
		conflict, err := commitTest(t.Context(), store, number-1, operation, changed)
		if !errors.Is(err, session.StorageConflict) || conflict.Status != session.StorageConflicting {
			t.Fatalf("changed digest: %#v %v", conflict, err)
		}
		snapshot = proposal
	}
	stale, err := commitTest(t.Context(), store, 0, session.OperationID{Event: "stale", Sequence: 1}, snapshot)
	if !errors.Is(err, session.StorageConflict) || stale.Status != session.StorageConflicting || !stale.CurrentKnown || stale.CurrentRevision != 3 {
		t.Fatalf("stale: %#v %v", stale, err)
	}
}

func TestFaultsResolveWithoutDuplicateDeath(t *testing.T) {
	for _, stage := range []string{"create", "disk full", "denied", "partial", "short", "sync", "close", "temporary validation", "replacement", "rename flush", "installed sync", "directory flush", "installed validation", "acknowledgment"} {
		t.Run(stage, func(t *testing.T) {
			store, files, service, _, initial := revisionStore(t)
			initialize(t, store, initial)
			// Exercise the actual older-slot replacement, not just equal initial slots.
			first, firstID := deathProposal(t, service, initial.Snapshot, "first")
			if _, err := commitTest(t.Context(), store, 0, firstID, first); err != nil {
				t.Fatal(err)
			}
			for iteration := 0; iteration < 20; iteration++ {
				expected := uint64(iteration + 1)
				proposal, operation := deathProposal(t, service, first, "second")
				files.installed = false
				files.stage = stage
				result, err := commitTest(t.Context(), store, expected, operation, proposal)
				if err == nil || result.Status != session.StorageAmbiguous || result.PayloadDigest == "" {
					t.Fatalf("fault %d reported success: %#v %v", iteration, result, err)
				}
				if (stage == "disk full" && result.Code != session.StorageFull) || (stage == "denied" && result.Code != session.StorageDenied) {
					t.Fatalf("fault classification: %#v %v", result, err)
				}
				other, otherID := deathProposal(t, service, first, "other")
				blocked, err := commitTest(t.Context(), store, expected, otherID, other)
				if !errors.Is(err, session.StorageConflict) || blocked.Status != session.StorageConflicting {
					t.Fatalf("overtook pending: %#v %v", blocked, err)
				}
				resolved, err := store.Resolve(t.Context(), false)
				if err != nil {
					t.Fatalf("resolve: %#v %v", resolved, err)
				}
				if resolved.Status == session.StorageNotInstalled {
					resolved, err = commitTest(t.Context(), store, expected, operation, proposal)
				}
				if err != nil || resolved.Status != session.StorageSucceeded || resolved.PayloadDigest != result.PayloadDigest || resolved.Revision.Snapshot.Data(t.Context()).DebtCents != int64(iteration+2)*4999 || len(resolved.Revision.Snapshot.Data(t.Context()).Incidents) != iteration+2 {
					t.Fatalf("retry: %#v %v", resolved, err)
				}
				first = proposal
			}
		})
	}
}

func TestDiscardAndLostAcknowledgment(t *testing.T) {
	store, files, service, _, initial := revisionStore(t)
	initialize(t, store, initial)
	proposal, operation := deathProposal(t, service, initial.Snapshot, "first")
	files.stage = "create"
	if _, err := commitTest(t.Context(), store, 0, operation, proposal); err == nil {
		t.Fatal("expected failure")
	}
	result, err := store.Discard(t.Context())
	if err != nil || result.Status != session.StorageNotInstalled {
		t.Fatalf("discard: %#v %v", result, err)
	}
	files.stage = "acknowledgment"
	if _, err = commitTest(t.Context(), store, 0, operation, proposal); err == nil {
		t.Fatal("expected lost acknowledgment")
	}
	result, err = store.Discard(t.Context())
	if err != nil || result.Status != session.StorageSucceeded || result.Revision.Snapshot.Data(t.Context()).DebtCents != 4999 {
		t.Fatalf("discard must discover installed success: %#v %v", result, err)
	}
}

func TestRecoveryPreservesDamagedAndTooNew(t *testing.T) {
	for _, kind := range []string{"truncated", "semantic", "newer"} {
		t.Run(kind, func(t *testing.T) {
			store, _, service, path, initial := revisionStore(t)
			initialize(t, store, initial)
			proposal, operation := deathProposal(t, service, initial.Snapshot, "first")
			if _, err := commitTest(t.Context(), store, 0, operation, proposal); err != nil {
				t.Fatal(err)
			}
			second, secondID := deathProposal(t, service, proposal, "second")
			if _, err := commitTest(t.Context(), store, 1, secondID, second); err != nil {
				t.Fatal(err)
			}
			inspection, _ := store.Inspect(t.Context())
			newest := inspection.Slots[0]
			if newest.Revision.Number == 1 {
				newest = inspection.Slots[1]
			}
			filename := filepath.Join(path, newest.Name)
			data, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "truncated":
				data = data[:len(data)/2]
			case "semantic":
				data = rewrap(t, data, func(value *payload) { value.State.DebtCents++ })
			case "newer":
				var env map[string]any
				if err := json.Unmarshal(data, &env); err != nil {
					t.Fatal(err)
				}
				env["schema_version"] = 2
				data, err = json.Marshal(env)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = os.WriteFile(filename, data, 0600); err != nil {
				t.Fatal(err)
			}
			inspection, _ = store.Inspect(t.Context())
			if kind == "newer" {
				if _, err = store.AcceptRecovery(t.Context(), 1); !errors.Is(err, session.SaveTooNew) {
					t.Fatalf("new schema: %v", err)
				}
				return
			}
			if _, err = store.AcceptRecovery(t.Context(), 1); err != nil {
				t.Fatal(err)
			}
			newProposal, newID := deathProposal(t, service, proposal, "recovered")
			if _, err = commitTest(t.Context(), store, 1, newID, newProposal); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(path, store.dir, "damaged"))
			if err != nil || len(entries) != 1 {
				t.Fatalf("preserved files: %v %v", entries, err)
			}
			copy, err := os.ReadFile(filepath.Join(path, store.dir, "damaged", entries[0].Name()))
			if err != nil || !bytes.Equal(copy, data) {
				t.Fatalf("damaged copy differs: %v", err)
			}
		})
	}
}

func TestInitializationInterruptionAndExistingData(t *testing.T) {
	store, files, _, _, initial := revisionStore(t)
	files.stage = "installed sync"
	result, err := store.Initialize(t.Context(), initial)
	if err == nil || result.Status == session.StorageSucceeded {
		t.Fatalf("partial initialization acknowledged: %#v %v", result, err)
	}
	inspection, _ := store.Inspect(t.Context())
	if inspection.Slots[0].Err != nil || !errors.Is(inspection.Slots[1].Err, os.ErrNotExist) {
		t.Fatalf("partial initialization: %#v", inspection)
	}
	changed := initial
	changed.Metadata.BuildDescription = "different"
	if _, err := store.Initialize(t.Context(), changed); !errors.Is(err, session.StorageConflict) {
		t.Fatalf("different initialization accepted: %v", err)
	}
	initialize(t, store, initial)
	initialize(t, store, initial)
}

func TestDivergentProposalCannotEraseCommittedFee(t *testing.T) {
	store, _, service, _, initial := revisionStore(t)
	initialize(t, store, initial)
	first, firstID := deathProposal(t, service, initial.Snapshot, "first")
	if _, err := commitTest(t.Context(), store, 0, firstID, first); err != nil {
		t.Fatal(err)
	}
	branch, _ := deathProposal(t, service, initial.Snapshot, "alternate")
	proposal, operation := deathProposal(t, service, branch, "second")
	result, err := commitTest(t.Context(), store, 1, operation, proposal)
	if !errors.Is(err, session.StorageConflict) || result.Status != session.StorageConflicting {
		t.Fatalf("different history accepted: %#v %v", result, err)
	}
	inspection, _ := store.Inspect(t.Context())
	for _, slot := range inspection.Slots {
		if slot.Err != nil {
			t.Fatal(slot.Err)
		}
		if slot.Revision.Number == 1 && slot.Revision.Snapshot.Data(t.Context()).Incidents[0].Detail != "first" {
			t.Fatal("committed fee was overwritten")
		}
	}
}

func TestPreparedPredecessorDigestRejectsDivergentBranch(t *testing.T) {
	store, _, service, _, initial := revisionStore(t)
	initialize(t, store, initial)
	active := prepare(t, service, initial.Snapshot, campaign.Transition{Attempt: &campaign.AttemptChange{Action: campaign.AttemptAccept, Deed: "D-HOS-SUP-01"}})
	first, err := commitTest(t.Context(), store, 0, session.OperationID{Event: "accept", Sequence: 1}, active)
	if err != nil {
		t.Fatal(err)
	}
	abandoned := prepare(t, service, active, campaign.Transition{Attempt: &campaign.AttemptChange{Action: campaign.AttemptAbandon, Deed: "D-HOS-SUP-01"}})
	checkpoint := prepare(t, service, active, campaign.Transition{Checkpoint: "ANC-PRO-03"})
	op := session.OperationID{Event: "branch", Sequence: 2}
	committed, err := store.Commit(t.Context(), 1, first.PayloadDigest, op, abandoned)
	if err != nil {
		t.Fatal(err)
	}
	alternate := first.Revision
	alternate.Number = 2
	alternate.Snapshot = checkpoint
	alternate.Operation = op
	alternate.Operations = append(append([]session.OperationID(nil), first.Revision.Operations...), op)
	_, alternate = encode(t, store.codec, alternate)
	if alternate.PayloadDigest == committed.PayloadDigest || !reflect.DeepEqual(abandoned.Data(t.Context()).Attempts, checkpoint.Data(t.Context()).Attempts) || !reflect.DeepEqual(abandoned.Data(t.Context()).Narratives, checkpoint.Data(t.Context()).Narratives) {
		t.Fatal("fixture must have distinct branches with identical durable collections")
	}
	proposal, operation := deathProposal(t, service, checkpoint, "branch death")
	if !extendsHistory(t.Context(), abandoned, proposal) {
		t.Fatal("fixture no longer exercises the history-only check")
	}
	request, err := session.NewProgressRequest(t.Context(), session.ProgressInput{ID: "branch", Kind: session.CommitProgress, Campaign: testCampaignID, ExpectedRevision: 2, ExpectedDigest: alternate.PayloadDigest, Operation: operation, Proposal: proposal})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := request.Resolution()
	if err != nil || resolution.Data().ExpectedDigest != alternate.PayloadDigest {
		t.Fatalf("resolution lost prepared predecessor: %v", err)
	}
	input := request.Data()
	result, err := store.Commit(t.Context(), input.ExpectedRevision, input.ExpectedDigest, input.Operation, input.Proposal)
	if !errors.Is(err, session.StorageConflict) || result.Status != session.StorageConflicting || result.CurrentRevision != 2 {
		t.Fatalf("divergent predecessor accepted: %#v %v", result, err)
	}
	inspection, _ := store.Inspect(t.Context())
	for _, slot := range inspection.Slots {
		if slot.Err != nil {
			t.Fatal(slot.Err)
		}
		if slot.Revision.Number == 2 && slot.Revision.PayloadDigest != committed.PayloadDigest {
			t.Fatal("conflicting branch overwrote committed state")
		}
	}
	valid, validID := deathProposal(t, service, abandoned, "real death")
	result, err = store.Commit(t.Context(), 2, "", validID, valid)
	if !errors.Is(err, session.StorageConflict) || result.Status != session.StorageConflicting {
		t.Fatalf("missing predecessor accepted: %#v %v", result, err)
	}
	result, err = store.Commit(t.Context(), 2, committed.PayloadDigest, validID, valid)
	if err != nil || result.Status != session.StorageSucceeded {
		t.Fatalf("correct predecessor rejected: %#v %v", result, err)
	}
	result, err = store.Commit(t.Context(), 2, alternate.PayloadDigest, validID, valid)
	if !errors.Is(err, session.StorageConflict) || result.Status != session.StorageConflicting {
		t.Fatalf("duplicate with changed predecessor accepted: %#v %v", result, err)
	}
}

func TestPendingRetryAcrossCompatibleUpdate(t *testing.T) {
	store, files, service, path, initial := revisionStore(t)
	_, _, manifest := fixture(t)
	initialize(t, store, initial)
	inspection, _ := store.Inspect(t.Context())
	predecessor := inspection.Slots[0].Revision.PayloadDigest
	proposal, operation := deathProposal(t, service, initial.Snapshot, "death")
	written, err := store.Commit(t.Context(), 0, predecessor, operation, proposal)
	if err != nil {
		t.Fatal(err)
	}
	inspection, _ = store.Inspect(t.Context())
	var name string
	for _, slot := range inspection.Slots {
		if slot.Revision.Number == 1 {
			name = slot.Name
		}
	}
	before, err := os.ReadFile(filepath.Join(path, name))
	if err != nil {
		t.Fatal(err)
	}
	current := store.codec.current
	current.ContentVersion = "campaign-2"
	current.ManifestDigest = strings.Repeat("b", 64)
	codec, err := NewCodec(t.Context(), service, manifest, current, []ContentIdentity{contentIdentity(store.codec.current)})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := newRevisions(t.Context(), files, codec, testCampaignID)
	if err != nil {
		t.Fatal(err)
	}
	files.stage = "installed sync"
	failed, err := updated.Commit(t.Context(), 0, predecessor, operation, proposal)
	if err == nil || failed.Status != session.StorageAmbiguous {
		t.Fatalf("expected failed confirmation: %#v %v", failed, err)
	}
	changed, _ := deathProposal(t, service, initial.Snapshot, "different")
	conflict, err := updated.Commit(t.Context(), 0, predecessor, operation, changed)
	if !errors.Is(err, session.StorageConflict) || conflict.Status != session.StorageConflicting {
		t.Fatalf("changed compatible retry accepted: %#v %v", conflict, err)
	}
	result, err := updated.Commit(t.Context(), 0, predecessor, operation, proposal)
	if err != nil || result.Status != session.StorageSucceeded || result.PayloadDigest != written.PayloadDigest || result.Revision.Compatibility != store.codec.current {
		t.Fatalf("compatible pending retry: %#v %v", result, err)
	}
	after, err := os.ReadFile(filepath.Join(path, name))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("compatible retry changed installed bytes: %v", err)
	}
}

func TestLostAcknowledgmentWithReceiptAndRestart(t *testing.T) {
	store, files, service, _, initial := revisionStore(t)
	_, _, manifest := fixture(t)
	initialize(t, store, initial)
	active := prepare(t, service, initial.Snapshot, campaign.Transition{Attempt: &campaign.AttemptChange{Action: campaign.AttemptAccept, Deed: "D-HOS-SUP-01"}})
	activeID := session.OperationID{Event: "accept-1", Sequence: 1}
	if _, err := commitTest(t.Context(), store, 0, activeID, active); err != nil {
		t.Fatal(err)
	}
	event := candidate(t, manifest, "D-HOS-SUP-01", 1)
	proposal := prepare(t, service, active, campaign.Transition{Certification: &campaign.CertificationCandidate{Completed: true, Event: event}, Death: campaign.DeathGameplay, DeathDetail: "hazard"})
	operation := session.OperationID{Event: "D-HOS-SUP-01", Sequence: 2}
	files.stage = "acknowledgment"
	pending, err := commitTest(t.Context(), store, 1, operation, proposal)
	if err == nil || pending.Status != session.StorageAmbiguous {
		t.Fatalf("lost ack: %#v %v", pending, err)
	}
	inspection, err := store.Inspect(t.Context())
	if err != nil || !inspection.Pending || inspection.Slots[0].Name != "" {
		t.Fatalf("unacknowledged data exposed: %#v %v", inspection, err)
	}
	// The interrupted writer is no longer used. Startup never promotes a temp.
	restarted, err := newRevisions(t.Context(), files, store.codec, testCampaignID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := commitTest(t.Context(), restarted, 1, operation, proposal)
	if err != nil || result.Status != session.StorageSucceeded || result.PayloadDigest != pending.PayloadDigest || result.Revision.Snapshot.Credits() != 1 || result.Revision.Snapshot.Data(t.Context()).DebtCents != 4999 {
		t.Fatalf("restart resolution: %#v %v", result, err)
	}
	if len(result.Revision.Snapshot.Data(t.Context()).Incidents) != 1 {
		t.Fatal("death was duplicated")
	}
}

func TestBothCorruptAndIncompatibleAreNotOverwritten(t *testing.T) {
	store, _, _, path, initial := revisionStore(t)
	initialize(t, store, initial)
	inspection, _ := store.Inspect(t.Context())
	for _, slot := range inspection.Slots {
		if err := os.WriteFile(filepath.Join(path, slot.Name), []byte("broken"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.AcceptRecovery(t.Context(), 0); !errors.Is(err, session.SaveCorrupt) {
		t.Fatalf("both corrupt: %v", err)
	}
	for _, slot := range inspection.Slots {
		data, err := os.ReadFile(filepath.Join(path, slot.Name))
		if err != nil || string(data) != "broken" {
			t.Fatalf("damaged slot changed: %q %v", data, err)
		}
	}

	other, _, _, otherPath, otherInitial := revisionStore(t)
	initialize(t, other, otherInitial)
	name := other.slotName(0)
	data, err := os.ReadFile(filepath.Join(otherPath, name))
	if err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err = json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	env.Compatibility.ManifestDigest = strings.Repeat("b", 64)
	data, err = json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(otherPath, name), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = other.AcceptRecovery(t.Context(), 0); !errors.Is(err, session.ContentIncompatible) {
		t.Fatalf("incompatible content: %v", err)
	}
	actual, err := os.ReadFile(filepath.Join(otherPath, name))
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("incompatible data was changed")
	}
}

func TestConcurrentWritersDoNotOvertake(t *testing.T) {
	store, _, service, _, initial := revisionStore(t)
	initialize(t, store, initial)
	left, leftID := deathProposal(t, service, initial.Snapshot, "left")
	right, rightID := deathProposal(t, service, initial.Snapshot, "right")
	type outcome struct {
		result CommitResult
		err    error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	ctx := t.Context()
	for _, request := range []struct {
		snapshot campaign.Snapshot
		id       session.OperationID
	}{{snapshot: left, id: leftID}, {snapshot: right, id: rightID}} {
		go func() {
			<-start
			result, err := commitTest(ctx, store, 0, request.id, request.snapshot)
			results <- outcome{result: result, err: err}
		}()
	}
	close(start)
	success, conflicts := 0, 0
	for range 2 {
		result := <-results
		if result.err == nil && result.result.Status == session.StorageSucceeded {
			success++
		} else if errors.Is(result.err, session.StorageConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent result: %#v", result)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("got %d successes and %d conflicts", success, conflicts)
	}
	inspection, _ := store.Inspect(ctx)
	for _, slot := range inspection.Slots {
		if slot.Err != nil {
			t.Fatal(slot.Err)
		}
		if slot.Revision.Number == 1 && (slot.Revision.Snapshot.Data(ctx).DebtCents != 4999 || len(slot.Revision.Snapshot.Data(ctx).Incidents) != 1) {
			t.Fatal("concurrent commit duplicated a fee")
		}
	}
}

func TestDuplicateConfirmationFailureCanRetryExactBytes(t *testing.T) {
	store, files, service, path, initial := revisionStore(t)
	initialize(t, store, initial)
	proposal, operation := deathProposal(t, service, initial.Snapshot, "death")
	first, err := commitTest(t.Context(), store, 0, operation, proposal)
	if err != nil {
		t.Fatal(err)
	}
	inspection, _ := store.Inspect(t.Context())
	var current string
	for _, slot := range inspection.Slots {
		if slot.Revision.Number == 1 {
			current = slot.Name
		}
	}
	files.stage = "installed sync"
	result, err := commitTest(t.Context(), store, 0, operation, proposal)
	if err == nil || result.Status != session.StorageAmbiguous {
		t.Fatalf("confirmation failure: %#v %v", result, err)
	}
	if err = os.Remove(filepath.Join(path, current)); err != nil {
		t.Fatal(err)
	}
	result, err = store.Resolve(t.Context(), true)
	if err != nil || result.Status != session.StorageSucceeded || result.PayloadDigest != first.PayloadDigest || result.Revision.Snapshot.Data(t.Context()).DebtCents != 4999 {
		t.Fatalf("exact retry: %#v %v", result, err)
	}
}

func TestRestartRecoveryAtCommitBoundaries(t *testing.T) {
	stages := []string{"create", "disk full", "denied", "partial", "short", "sync", "close", "temporary validation", "replacement", "rename flush", "installed sync", "directory flush", "installed validation", "acknowledgment"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			store, files, service, path, initial := revisionStore(t)
			_, _, manifest := fixture(t)
			initialize(t, store, initial)
			active := prepare(t, service, initial.Snapshot, campaign.Transition{Attempt: &campaign.AttemptChange{Action: campaign.AttemptAccept, Deed: "D-HOS-SUP-01"}})
			base, err := commitTest(t.Context(), store, 0, session.OperationID{Event: "accept", Sequence: 1}, active)
			if err != nil {
				t.Fatal(err)
			}
			event := candidate(t, manifest, "D-HOS-SUP-01", 1)
			proposal := prepare(t, service, active, campaign.Transition{Certification: &campaign.CertificationCandidate{Completed: true, Event: event}, Death: campaign.DeathGameplay, DeathDetail: "hazard"})
			operation := session.OperationID{Event: "D-HOS-SUP-01", Sequence: 2}
			files.installed = false
			files.stage = stage
			pending, err := store.Commit(t.Context(), 1, base.PayloadDigest, operation, proposal)
			if err == nil || pending.Status != session.StorageAmbiguous {
				t.Fatalf("interruption not reported: %#v %v", pending, err)
			}
			// Discard the interrupted object's in-memory state. The fresh object
			// receives only the real platform root, not its fault wrapper.
			restarted, err := newRevisions(t.Context(), files.Root, store.codec, testCampaignID)
			if err != nil {
				t.Fatal(err)
			}
			inspection, err := restarted.Inspect(t.Context())
			if err != nil || inspection.Pending {
				t.Fatalf("restart inspection: %#v %v", inspection, err)
			}
			oldFound := false
			for _, slot := range inspection.Slots {
				if slot.Err != nil {
					t.Fatalf("invalid installed slot: %v", slot.Err)
				}
				if sameRevision(slot.Revision, base.Revision) {
					oldFound = true
				}
				if slot.Revision.Number == 2 && (!reflect.DeepEqual(slot.Revision.Snapshot.Data(t.Context()), proposal.Data(t.Context())) || slot.Revision.PayloadDigest != pending.PayloadDigest) {
					t.Fatal("installed new revision is incomplete")
				}
			}
			if !oldFound {
				t.Fatal("previously acknowledged revision was lost")
			}
			selected, err := restarted.selectSlot(t.Context(), inspection)
			if err != nil {
				t.Fatal(err)
			}
			current := inspection.Slots[selected].Revision
			if current.Number != 1 && current.Number != 2 {
				t.Fatalf("selected unexpected revision %d", current.Number)
			}
			if current.Number == 1 && !reflect.DeepEqual(current.Snapshot.Data(t.Context()), active.Data(t.Context())) {
				t.Fatal("selected old revision differs from acknowledged state")
			}
			installed := stage == "rename flush" || stage == "installed sync" || stage == "directory flush" || stage == "installed validation" || stage == "acknowledgment"
			if (current.Number == 2) != installed {
				t.Fatalf("wrong restart selection at %s: %d", stage, current.Number)
			}
			if stage == "sync" || stage == "close" || stage == "temporary validation" || stage == "replacement" {
				entries, err := os.ReadDir(filepath.Join(path, store.dir))
				if err != nil {
					t.Fatal(err)
				}
				validTemp := false
				for _, entry := range entries {
					if !strings.HasPrefix(entry.Name(), ".tmp-") {
						continue
					}
					data, err := os.ReadFile(filepath.Join(path, store.dir, entry.Name()))
					if err != nil {
						t.Fatal(err)
					}
					temp, decodeErr := store.codec.Decode(t.Context(), data)
					if decodeErr == nil && temp.PayloadDigest == pending.PayloadDigest {
						validTemp = true
					}
				}
				if !validTemp || current.Number != 1 {
					t.Fatal("valid temporary file was missing or treated as committed")
				}
			}
			result, err := restarted.Commit(t.Context(), 1, base.PayloadDigest, operation, proposal)
			if err != nil || result.Status != session.StorageSucceeded || result.PayloadDigest != pending.PayloadDigest || result.Revision.Snapshot.Credits() != 1 || result.Revision.Snapshot.Data(t.Context()).DebtCents != 4999 || len(result.Revision.Snapshot.Data(t.Context()).Incidents) != 1 {
				t.Fatalf("restart retry duplicated or lost progress: %#v %v", result, err)
			}
			inspection, _ = restarted.Inspect(t.Context())
			foundOld, foundNew := false, false
			for _, slot := range inspection.Slots {
				if slot.Err != nil {
					t.Fatalf("invalid slot after retry: %#v", inspection)
				}
				foundOld = foundOld || sameRevision(slot.Revision, base.Revision)
				foundNew = foundNew || sameRevision(slot.Revision, result.Revision)
			}
			if !foundOld || !foundNew {
				t.Fatalf("old and new revisions not both retained: %#v", inspection)
			}
		})
	}
}

func TestRestartRecoveryAtInitializationBoundaries(t *testing.T) {
	for _, stage := range []string{"create", "disk full", "denied", "partial", "short", "sync", "close", "temporary validation", "replacement", "rename flush", "installed sync", "directory flush", "installed validation"} {
		t.Run(stage, func(t *testing.T) {
			store, files, _, _, initial := revisionStore(t)
			_, checked := encode(t, store.codec, initial)
			files.stage = stage
			result, err := store.Initialize(t.Context(), initial)
			if err == nil || result.Status == session.StorageSucceeded {
				t.Fatalf("partial initialization acknowledged: %#v %v", result, err)
			}
			restarted, err := newRevisions(t.Context(), files.Root, store.codec, testCampaignID)
			if err != nil {
				t.Fatal(err)
			}
			inspection, _ := restarted.Inspect(t.Context())
			if !errors.Is(inspection.Slots[1].Err, os.ErrNotExist) {
				t.Fatalf("unwritten second slot appeared: %#v", inspection)
			}
			if _, err := restarted.selectSlot(t.Context(), inspection); err == nil {
				t.Fatal("partially initialized campaign selected")
			}
			if inspection.Slots[0].Err != nil && !errors.Is(inspection.Slots[0].Err, os.ErrNotExist) {
				t.Fatalf("initial slot is not old-or-new: %v", inspection.Slots[0].Err)
			}
			if inspection.Slots[0].Err == nil && !sameRevision(inspection.Slots[0].Revision, checked) {
				t.Fatal("partial initialization produced a different revision")
			}
			initialize(t, restarted, initial)
			inspection, _ = restarted.Inspect(t.Context())
			if inspection.Slots[0].Err != nil || inspection.Slots[1].Err != nil || !sameRevision(inspection.Slots[0].Revision, inspection.Slots[1].Revision) {
				t.Fatalf("restart did not install both initial slots: %#v", inspection)
			}
		})
	}
}

func TestCancellationKeepsPendingUntilResolution(t *testing.T) {
	store, files, service, _, initial := revisionStore(t)
	initialize(t, store, initial)
	proposal, operation := deathProposal(t, service, initial.Snapshot, "death")
	files.stage = "create"
	if _, err := commitTest(t.Context(), store, 0, operation, proposal); err == nil {
		t.Fatal("expected injected failure")
	}
	conflict, err := store.Commit(t.Context(), 0, strings.Repeat("0", 64), operation, proposal)
	if !errors.Is(err, session.StorageConflict) || conflict.Status != session.StorageConflicting {
		t.Fatalf("pending retry changed predecessor: %#v %v", conflict, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := commitTest(ctx, store, 0, operation, proposal)
	if !errors.Is(err, context.Canceled) || result.Status != session.StorageAmbiguous {
		t.Fatalf("cancellation lost: %#v %v", result, err)
	}
	result, err = store.Discard(t.Context())
	if err != nil || result.Status != session.StorageNotInstalled {
		t.Fatalf("pending not resolved: %#v %v", result, err)
	}
}

func TestDivergentInitialSlotsConflict(t *testing.T) {
	store, _, service, path, initial := revisionStore(t)
	initialize(t, store, initial)
	different := initial
	different.Metadata.BuildDescription = "different initial data"
	data, _ := encode(t, store.codec, different)
	if err := os.WriteFile(filepath.Join(path, store.slotName(0)), data, 0600); err != nil {
		t.Fatal(err)
	}
	proposal, operation := deathProposal(t, service, initial.Snapshot, "death")
	result, err := commitTest(t.Context(), store, 0, operation, proposal)
	if !errors.Is(err, session.StorageConflict) || result.Status != session.StorageConflicting {
		t.Fatalf("divergent slots: %#v %v", result, err)
	}
}

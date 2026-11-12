// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

func profileFixture(t *testing.T) (*Store, *campaign.Campaign, string) {
	t.Helper()
	codec, service, _ := fixture(t)
	path := t.TempDir()
	store, err := Open(t.Context(), path, codec, 32, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store, service, path
}

func newProfileCampaign(t *testing.T, store *Store) session.ProgressResultData {
	t.Helper()
	result := store.processProgress(t.Context(), session.ProgressInput{ID: "new", Kind: session.CreateCampaign, Campaign: testCampaignID})
	if result.Status != session.StorageSucceeded || result.Campaign == "" || result.Snapshot.Credits() != 0 {
		t.Fatalf("create: %+v", result)
	}
	return result
}

func storyCommit(t *testing.T, service *campaign.Campaign, initial session.ProgressResultData) session.ProgressInput {
	t.Helper()
	ctx := t.Context()
	proposal, err := service.Prepare(ctx, initial.Snapshot, campaign.Transition{CampaignID: string(initial.Campaign), Sequence: 1, Story: &campaign.StoryChange{Action: campaign.StoryNarrative, Narrative: "NAR-01"}})
	if err != nil {
		t.Fatal(err)
	}
	return session.ProgressInput{ID: "story", Kind: session.CommitProgress, Campaign: initial.Campaign, ExpectedRevision: initial.Revision, ExpectedDigest: initial.PayloadDigest, Operation: session.OperationID{Event: "NAR-01", Sequence: 1}, Proposal: proposal}
}

func TestNewCampaignAndExplicitSelectorRecovery(t *testing.T) {
	store, _, path := profileFixture(t)
	first := newProfileCampaign(t, store)
	second := newProfileCampaign(t, store)
	if first.Campaign == second.Campaign || !validCampaignID(string(first.Campaign)) || !validCampaignID(string(second.Campaign)) {
		t.Fatal("new games did not get separate identities")
	}
	active := store.processProgress(t.Context(), session.ProgressInput{ID: "active", Kind: session.LoadActive})
	if active.Status != session.StorageSucceeded || active.Campaign != second.Campaign {
		t.Fatalf("active: %+v", active)
	}
	historical := store.processProgress(t.Context(), session.ProgressInput{ID: "historical", Kind: session.LoadCampaign, Campaign: first.Campaign})
	if historical.Status != session.StorageConflicting || historical.RevisionKnown {
		t.Fatalf("historical campaign loaded without selection: %+v", historical)
	}
	for _, name := range []string{"active.a.json", "active.b.json"} {
		if err := os.WriteFile(filepath.Join(path, name), []byte("damaged"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	active = store.processProgress(t.Context(), session.ProgressInput{ID: "recover", Kind: session.LoadActive})
	if active.Status != session.StorageCorrupt || len(active.Candidates) != 2 || active.Campaign != "" {
		t.Fatalf("selectors silently selected a campaign: %+v", active)
	}
	for _, candidate := range active.Candidates {
		if candidate.Credits != 0 || candidate.Revision != 0 || candidate.Anchor != "ANC-PRO-01" {
			t.Fatalf("fabricated candidate: %+v", candidate)
		}
	}
	selected := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: first.Campaign})
	if selected.Status != session.StorageSucceeded || selected.Campaign != first.Campaign {
		t.Fatalf("explicit select: %+v", selected)
	}
	active = store.processProgress(t.Context(), session.ProgressInput{ID: "active2", Kind: session.LoadActive})
	if active.Status != session.StorageSucceeded || active.Campaign != first.Campaign {
		t.Fatalf("selection not installed: %+v", active)
	}
}

func TestSelectorFailurePreservesPriorSelection(t *testing.T) {
	store, _, _ := profileFixture(t)
	first := newProfileCampaign(t, store)
	files := &faultFiles{Root: store.root, stage: "replacement"}
	store.files = files
	result := store.processProgress(t.Context(), session.ProgressInput{ID: "new2", Kind: session.CreateCampaign, Campaign: testCampaignID})
	if result.Status == session.StorageSucceeded {
		t.Fatal("failed installation acknowledged")
	}
	active := store.processProgress(t.Context(), session.ProgressInput{ID: "active", Kind: session.LoadActive})
	if active.Status != session.StorageSucceeded || active.Campaign != first.Campaign {
		t.Fatalf("failed creation changed selection: %+v", active)
	}
}

type selectorFault struct {
	*faultFiles
	installed bool
}

func (files *selectorFault) ReplaceFile(ctx context.Context, source, destination string) (platform.ReplaceResult, error) {
	if strings.HasPrefix(destination, "active.") {
		if !files.installed {
			files.installed = true
			return platform.ReplaceResult{}, errInjected
		}
	}
	return files.faultFiles.ReplaceFile(ctx, source, destination)
}

func TestSelectorReplacementFailureAfterCreation(t *testing.T) {
	store, _, _ := profileFixture(t)
	first := newProfileCampaign(t, store)
	store.files = &selectorFault{faultFiles: &faultFiles{Root: store.root}}
	result := store.processProgress(t.Context(), session.ProgressInput{ID: "new2", Kind: session.CreateCampaign, Campaign: testCampaignID})
	if result.Status != session.StorageFailed || result.Campaign == first.Campaign {
		t.Fatalf("selector failure: %+v", result)
	}
	active := store.processProgress(t.Context(), session.ProgressInput{ID: "active", Kind: session.LoadActive})
	if active.Status != session.StorageSucceeded || active.Campaign != first.Campaign {
		t.Fatalf("selector failure replaced selection: %+v", active)
	}
}

type installedSelectorFault struct {
	*platform.Root
	failed bool
}

func (files *installedSelectorFault) ReplaceFile(ctx context.Context, source, destination string) (platform.ReplaceResult, error) {
	result, err := files.Root.ReplaceFile(ctx, source, destination)
	if err == nil && strings.HasPrefix(destination, "active.") && !files.failed {
		files.failed = true
		return result, errInjected
	}
	return result, err
}

func TestInstalledSelectorIsResolvedBeforeUse(t *testing.T) {
	store, _, _ := profileFixture(t)
	first := newProfileCampaign(t, store)
	store.files = &installedSelectorFault{Root: store.root}
	created := store.processProgress(t.Context(), session.ProgressInput{ID: "new", Kind: session.CreateCampaign, Campaign: testCampaignID})
	if created.Status != session.StorageAmbiguous || created.Campaign == first.Campaign || created.Campaign == "" {
		t.Fatalf("expected ambiguous selector: %+v", created)
	}
	slots := store.selectors(t.Context())
	oldFound := false
	for _, slot := range slots {
		oldFound = oldFound || (slot.err == nil && slot.value.Campaign == first.Campaign)
	}
	if !oldFound {
		t.Fatal("last confirmed selector was overwritten")
	}
	blocked := store.processSettings(t.Context(), session.SettingsInput{ID: "settings", Kind: session.LoadSettings})
	if blocked.Status != session.StorageConflicting {
		t.Fatalf("operation bypassed ambiguous selector: %+v", blocked)
	}
	resolved := store.processProgress(t.Context(), session.ProgressInput{ID: "active", Kind: session.LoadActive})
	if resolved.Status != session.StorageSucceeded || resolved.Campaign != created.Campaign || resolved.Snapshot.Credits() != 0 {
		t.Fatalf("selector resolution: %+v", resolved)
	}
}

func TestCorruptionAndCompatibilityRecovery(t *testing.T) {
	for _, damage := range []string{"newest", "both", "too-new", "incompatible"} {
		t.Run(damage, func(t *testing.T) {
			store, service, path := profileFixture(t)
			initial := newProfileCampaign(t, store)
			input := storyCommit(t, service, initial)
			committed := store.processProgress(t.Context(), input)
			if committed.Status != session.StorageSucceeded {
				t.Fatalf("commit: %+v", committed)
			}
			revisions, err := store.revisionsFor(t.Context(), initial.Campaign)
			if err != nil {
				t.Fatal(err)
			}
			inspection, err := revisions.Inspect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for _, slot := range inspection.Slots {
				if slot.Revision.Number != 1 && damage != "both" {
					continue
				}
				data := []byte("bad revision")
				if damage == "too-new" || damage == "incompatible" {
					var env envelope
					if err := json.Unmarshal(slot.data, &env); err != nil {
						t.Fatal(err)
					}
					if damage == "too-new" {
						env.Schema++
					} else {
						env.Compatibility.ManifestDigest = strings.Repeat("b", 64)
					}
					data, err = json.Marshal(env)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(path, slot.Name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			result := store.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
			want := session.StorageCorrupt
			if damage == "too-new" {
				want = session.StorageTooNew
			} else if damage == "incompatible" {
				want = session.StorageIncompatible
			}
			if result.Status != want {
				t.Fatalf("unexpected recovery: %+v", result)
			}
			if damage == "newest" {
				if len(result.Candidates) != 1 || result.Candidates[0].Revision != 0 || !result.Candidates[0].UnknownDifference || result.Candidates[0].Credits != 0 {
					t.Fatalf("bad recovery candidate: %+v", result.Candidates)
				}
				recovered := store.processProgress(t.Context(), session.ProgressInput{ID: "choose", Kind: session.LoadRevision, Campaign: initial.Campaign, ExpectedRevision: 0})
				if recovered.Status != session.StorageSucceeded || recovered.Snapshot.Credits() != 0 || recovered.Snapshot.Data(t.Context()).DebtCents != 0 {
					t.Fatalf("explicit recovery: %+v", recovered)
				}
				commit := store.processProgress(t.Context(), storyCommit(t, service, recovered))
				if commit.Status != session.StorageSucceeded {
					t.Fatalf("recovery commit: %+v", commit)
				}
				entries, err := os.ReadDir(filepath.Join(path, "campaigns", string(initial.Campaign), "damaged"))
				if err != nil || len(entries) != 1 {
					t.Fatalf("damage not preserved: %v", err)
				}
			} else if damage == "both" {
				if len(result.Candidates) != 0 {
					t.Fatal("invented recovery candidates")
				}
				other := newProfileCampaign(t, store)
				if other.Campaign == initial.Campaign {
					t.Fatal("damaged campaign reused")
				}
			}
		})
	}
}

func TestCampaignPathCannotEscapeProfile(t *testing.T) {
	store, _, path := profileFixture(t)
	initial := newProfileCampaign(t, store)
	outside := t.TempDir()
	id := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	revision := Revision{CampaignID: id, Compatibility: store.codec.current, Snapshot: campaign.Snapshot{}, Metadata: Metadata{LastWriteUTC: time.Now().UTC()}}
	data, _, err := store.codec.Encode(t.Context(), revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"progress.a.json", "progress.b.json"} {
		if err := os.WriteFile(filepath.Join(outside, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(path, "campaigns", id)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	result := store.processProgress(t.Context(), session.ProgressInput{ID: "escape", Kind: session.SelectCampaign, Campaign: session.CampaignID(id)})
	if result.Status == session.StorageSucceeded {
		t.Fatal("selected a campaign outside the profile root")
	}
	active := store.processProgress(t.Context(), session.ProgressInput{ID: "active", Kind: session.LoadActive})
	if active.Status != session.StorageSucceeded || active.Campaign != initial.Campaign {
		t.Fatalf("path escape changed selection: %+v", active)
	}
	for _, name := range []string{"progress.a.json", "progress.b.json"} {
		after, err := os.ReadFile(filepath.Join(outside, name))
		if err != nil || string(after) != string(data) {
			t.Fatalf("outside file changed: %v", err)
		}
	}
}

func TestSettingsAreIndependent(t *testing.T) {
	store, _, path := profileFixture(t)
	initial := newProfileCampaign(t, store)
	values := settings.Defaults(t.Context())
	values.Audio.Master = 42
	saved := store.processSettings(t.Context(), session.SettingsInput{ID: "save", Kind: session.SaveSettings, Values: values})
	if saved.Status != session.StorageSucceeded {
		t.Fatalf("save settings: %+v", saved)
	}
	if err := os.WriteFile(filepath.Join(path, "settings.json"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded := store.processSettings(t.Context(), session.SettingsInput{ID: "load", Kind: session.LoadSettings})
	if loaded.Status != session.StorageSucceeded || !loaded.DefaultsRestored || loaded.Values.Audio.Master != 80 {
		t.Fatalf("default notice: %+v", loaded)
	}
	saved = store.processSettings(t.Context(), session.SettingsInput{ID: "save2", Kind: session.SaveSettings, Values: values})
	if saved.Status != session.StorageSucceeded {
		t.Fatalf("replace damaged settings: %+v", saved)
	}
	matches, err := filepath.Glob(filepath.Join(path, ".tmp-*.preserved"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("missing settings evidence: %v", err)
	}
	preserved, err := os.ReadFile(matches[0])
	if err != nil || string(preserved) != "bad" {
		t.Fatalf("settings evidence changed: %q %v", preserved, err)
	}
	files := &faultFiles{Root: store.root, stage: "disk full"}
	store.files = files
	saved = store.processSettings(t.Context(), session.SettingsInput{ID: "save3", Kind: session.SaveSettings, Values: values})
	if saved.Status != session.StorageFailed || saved.Code != session.StorageFull {
		t.Fatalf("write failure: %+v", saved)
	}
	active := store.processProgress(t.Context(), session.ProgressInput{ID: "active", Kind: session.LoadActive})
	if active.Status != session.StorageSucceeded || active.Campaign != initial.Campaign || active.Revision != 0 || active.Snapshot.Credits() != 0 {
		t.Fatalf("settings damaged campaign: %+v", active)
	}
}

func TestNotInstalledResolutionAllowsOnlyOriginalRetry(t *testing.T) {
	store, service, _ := profileFixture(t)
	initial := newProfileCampaign(t, store)
	revisions, err := store.revisionsFor(t.Context(), initial.Campaign)
	if err != nil {
		t.Fatal(err)
	}
	revisions.files = &faultFiles{Root: store.root, stage: "replacement"}
	input := storyCommit(t, service, initial)
	ambiguous := store.processProgress(t.Context(), input)
	if ambiguous.Status != session.StorageAmbiguous || ambiguous.RevisionKnown {
		t.Fatalf("replacement failure published progress: %+v", ambiguous)
	}
	input.Kind = session.ResolveProgress
	resolved := store.processProgress(t.Context(), input)
	if resolved.Status != session.StorageNotInstalled || resolved.PayloadDigest != ambiguous.PayloadDigest || resolved.RevisionKnown || resolved.Snapshot.Credits() != 0 {
		t.Fatalf("not-installed resolution: %+v", resolved)
	}
	other := input
	other.ExpectedDigest = strings.Repeat("0", 64)
	blocked := store.processProgress(t.Context(), other)
	if blocked.Status != session.StorageConflicting {
		t.Fatalf("unconfirmed discard bypassed: %+v", blocked)
	}
	input.Kind = session.CommitProgress
	committed := store.processProgress(t.Context(), input)
	if committed.Status != session.StorageSucceeded || committed.Revision != 1 || committed.PayloadDigest != resolved.PayloadDigest || len(committed.Snapshot.Data(t.Context()).Narratives) != 1 {
		t.Fatalf("retry did not use original proposal: %+v", committed)
	}
}

func TestConfirmedReloadResolvesNotInstalledOperation(t *testing.T) {
	store, service, _ := profileFixture(t)
	initial := newProfileCampaign(t, store)
	revisions, err := store.revisionsFor(t.Context(), initial.Campaign)
	if err != nil {
		t.Fatal(err)
	}
	revisions.files = &faultFiles{Root: store.root, stage: "replacement"}
	input := storyCommit(t, service, initial)
	if got := store.processProgress(t.Context(), input); got.Status != session.StorageAmbiguous {
		t.Fatalf("commit: %+v", got)
	}
	input.Kind = session.ResolveProgress
	if got := store.processProgress(t.Context(), input); got.Status != session.StorageNotInstalled {
		t.Fatalf("resolution: %+v", got)
	}
	loaded := store.processProgress(t.Context(), session.ProgressInput{ID: "confirmed-reload", Kind: session.LoadActive})
	if loaded.Status != session.StorageSucceeded || loaded.Revision != 0 || loaded.Snapshot.Credits() != 0 || len(loaded.Snapshot.Data(t.Context()).Narratives) != 0 {
		t.Fatalf("confirmed reload fabricated progress: %+v", loaded)
	}
}

func TestConfirmedDiscardRechecksBeforeAllowingReload(t *testing.T) {
	store, service, _ := profileFixture(t)
	initial := newProfileCampaign(t, store)
	revisions, err := store.revisionsFor(t.Context(), initial.Campaign)
	if err != nil {
		t.Fatal(err)
	}
	revisions.files = &faultFiles{Root: store.root, stage: "replacement"}
	input := storyCommit(t, service, initial)
	request, err := session.NewProgressRequest(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	result := store.processProgress(t.Context(), input)
	if result.Status != session.StorageAmbiguous {
		t.Fatalf("commit: %+v", result)
	}
	resolution, err := request.Resolution()
	if err != nil {
		t.Fatal(err)
	}
	result = store.processProgress(t.Context(), resolution.Data())
	if result.Status != session.StorageNotInstalled {
		t.Fatalf("resolution: %+v", result)
	}
	if err := store.DiscardProgress(t.Context(), resolution); err != nil {
		t.Fatal(err)
	}
	load, err := session.NewProgressRequest(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Submit(t.Context(), load); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	discarded, err := store.Poll(t.Context(), input.ID)
	if err != nil || discarded.Data(t.Context()).Status != session.StorageNotInstalled {
		t.Fatalf("discard: %+v %v", discarded.Data(t.Context()), err)
	}
	loaded, err := store.Poll(t.Context(), "load")
	if err != nil || loaded.Data(t.Context()).Status != session.StorageSucceeded || loaded.Data(t.Context()).Revision != 0 || loaded.Data(t.Context()).Snapshot.Credits() != 0 {
		t.Fatalf("reload: %+v %v", loaded.Data(t.Context()), err)
	}
}

func TestResolveAfterRestartDoesNotDuplicateDeath(t *testing.T) {
	codec, service, _ := fixture(t)
	path := t.TempDir()
	store, err := Open(t.Context(), path, codec, 4, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	initial := newProfileCampaign(t, store)
	proposal, err := service.Prepare(t.Context(), initial.Snapshot, campaign.Transition{CampaignID: string(initial.Campaign), Sequence: 1, Death: campaign.DeathGameplay, DeathDetail: "hazard"})
	if err != nil {
		t.Fatal(err)
	}
	input := session.ProgressInput{ID: "death", Kind: session.CommitProgress, Campaign: initial.Campaign, ExpectedRevision: 0, ExpectedDigest: initial.PayloadDigest, Operation: session.OperationID{Event: "death-1", Sequence: 1}, Proposal: proposal}
	revisions, err := store.revisionsFor(t.Context(), initial.Campaign)
	if err != nil {
		t.Fatal(err)
	}
	revisions.ack = func() error { return errInjected }
	ambiguous := store.processProgress(t.Context(), input)
	if ambiguous.Status != session.StorageAmbiguous || ambiguous.RevisionKnown {
		t.Fatalf("death prematurely acknowledged: %+v", ambiguous)
	}
	if err := store.Close(); err == nil {
		t.Fatal("unresolved death was not reported")
	}
	store, err = Open(t.Context(), path, codec, 4, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	input.Kind = session.ResolveProgress
	resolved := store.processProgress(t.Context(), input)
	state := resolved.Snapshot.Data(t.Context())
	if resolved.Status != session.StorageSucceeded || resolved.Revision != 1 || state.DebtCents != 4999 || len(state.Incidents) != 1 || resolved.Snapshot.Credits() != 0 {
		t.Fatalf("restart resolution: %+v debt=%d incidents=%d", resolved, state.DebtCents, len(state.Incidents))
	}
	input.Kind = session.CommitProgress
	repeated := store.processProgress(t.Context(), input)
	state = repeated.Snapshot.Data(t.Context())
	if repeated.Status != session.StorageSucceeded || repeated.PayloadDigest != resolved.PayloadDigest || state.DebtCents != 4999 || len(state.Incidents) != 1 {
		t.Fatalf("retry duplicated death: %+v", repeated)
	}
}

func TestCommitConflictAndLostAcknowledgment(t *testing.T) {
	store, service, _ := profileFixture(t)
	initial := newProfileCampaign(t, store)
	revisions, err := store.revisionsFor(t.Context(), initial.Campaign)
	if err != nil {
		t.Fatal(err)
	}
	revisions.ack = func() error { return errInjected }
	input := storyCommit(t, service, initial)
	result := store.processProgress(t.Context(), input)
	if result.Status != session.StorageAmbiguous || result.Snapshot.Credits() != 0 || result.RevisionKnown {
		t.Fatalf("published unacknowledged revision: %+v", result)
	}
	blocked := store.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
	if blocked.Status != session.StorageConflicting {
		t.Fatalf("pending operation bypassed: %+v", blocked)
	}
	changed := input
	var prepareErr error
	changed.Proposal, prepareErr = service.Prepare(t.Context(), initial.Snapshot, campaign.Transition{CampaignID: string(initial.Campaign), Sequence: 1, Death: campaign.DeathHeartAttack})
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}
	changedResult := store.processProgress(t.Context(), changed)
	if changedResult.Status != session.StorageConflicting || changedResult.RevisionKnown {
		t.Fatalf("changed pending payload accepted: %+v", changedResult)
	}
	settingsResult := store.processSettings(t.Context(), session.SettingsInput{ID: "settings", Kind: session.SaveSettings, Values: settings.Defaults(t.Context())})
	if settingsResult.Status != session.StorageConflicting {
		t.Fatalf("settings overtook pending commit: %+v", settingsResult)
	}
	input.Kind = session.ResolveProgress
	result = store.processProgress(t.Context(), input)
	if result.Status != session.StorageSucceeded || result.Revision != 1 || len(result.Snapshot.Data(t.Context()).Narratives) != 1 {
		t.Fatalf("lost acknowledgment: %+v", result)
	}
	revisions.ack = nil
	input.Kind = session.CommitProgress
	result = store.processProgress(t.Context(), input)
	if result.Status != session.StorageSucceeded || result.Revision != 1 {
		t.Fatalf("duplicate: %+v", result)
	}
	input.Operation.Event = "other"
	result = store.processProgress(t.Context(), input)
	if result.Status != session.StorageConflicting || !result.RevisionKnown || result.Revision != 1 {
		t.Fatalf("stale operation: %+v", result)
	}
}

// Fail settlement after installation, then fail both re-arm attempts before
// replacement. The independent fence must survive all three failures.
type doubleRearmFailure struct {
	*settlementFailure
	attempts int
}

func (files *doubleRearmFailure) ReplaceFile(ctx context.Context, source, destination string) (platform.ReplaceResult, error) {
	if files.fired && (destination == selectorIntentName || destination == selectorGuardName) {
		data, err := files.Root.ReadFile(ctx, source, maxProfileFile)
		if err != nil {
			return platform.ReplaceResult{}, err
		}
		var record selectorIntent
		if err := decodeChecked(ctx, data, "TBTD-SELECTOR-INTENT", &record); err != nil {
			return platform.ReplaceResult{}, err
		}
		if record.Pending {
			files.attempts++
			return platform.ReplaceResult{}, errInjected
		}
	}
	return files.settlementFailure.ReplaceFile(ctx, source, destination)
}

func TestSelectorFenceSurvivesFailedSettlementAndBothRearms(t *testing.T) {
	for _, stage := range []string{"replacement", "flush", "validation"} {
		t.Run(stage, func(t *testing.T) {
			codec, _, _ := fixture(t)
			path := t.TempDir()
			store, err := Open(t.Context(), path, codec, 4, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			first := newProfileCampaign(t, store)
			fault := &doubleRearmFailure{settlementFailure: &settlementFailure{Root: store.root, target: selectorGuardName, stage: stage}}
			store.files = fault
			created := store.processProgress(t.Context(), session.ProgressInput{ID: "new", Kind: session.CreateCampaign, Campaign: testCampaignID})
			if created.Status != session.StorageAmbiguous || created.RevisionKnown || created.Campaign == first.Campaign || !fault.fired || fault.attempts != 2 {
				t.Fatalf("expected three failures without acknowledgment: %+v, fired=%t attempts=%d", created, fault.fired, fault.attempts)
			}
			if err := store.Close(); err == nil {
				t.Fatal("unresolved selection was not reported")
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
			if loaded.Status != session.StorageAmbiguous || loaded.Campaign != "" || loaded.RevisionKnown || len(loaded.Candidates) != 2 {
				t.Fatalf("unacknowledged selection published: %+v", loaded)
			}
			for _, candidate := range loaded.Candidates {
				if (candidate.Campaign != first.Campaign && candidate.Campaign != created.Campaign) || candidate.Revision != 0 || candidate.Credits != 0 {
					t.Fatalf("fabricated recovery candidate: %+v", candidate)
				}
			}
			selected := reopened.processProgress(t.Context(), session.ProgressInput{ID: "choose", Kind: session.SelectCampaign, Campaign: first.Campaign})
			if selected.Status != session.StorageSucceeded || selected.Campaign != first.Campaign || selected.Snapshot.Credits() != 0 || selected.Snapshot.Data(t.Context()).DebtCents != 0 {
				t.Fatalf("explicit recovery: %+v", selected)
			}
		})
	}
}

func TestSelectorFenceRequiresChoiceAfterUncleanExit(t *testing.T) {
	codec, _, _ := fixture(t)
	path := t.TempDir()
	store, err := Open(t.Context(), path, codec, 4, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first := newProfileCampaign(t, store)
	second := newProfileCampaign(t, store)
	// Model process death without running the orderly worker shutdown.
	if err := store.listing.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.root.Close(); err != nil {
		t.Fatal(err)
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
		t.Fatalf("unclean exit silently chose a campaign: %+v", loaded)
	}
	selected := reopened.processProgress(t.Context(), session.ProgressInput{ID: "choose", Kind: session.SelectCampaign, Campaign: second.Campaign})
	if selected.Status != session.StorageSucceeded || selected.Campaign != second.Campaign || first.Campaign == second.Campaign || selected.Snapshot.Credits() != 0 {
		t.Fatalf("explicit choice: %+v", selected)
	}
}

func TestSelectorFenceCompatibilityAndCorruption(t *testing.T) {
	for _, future := range []bool{false, true} {
		t.Run(fmt.Sprintf("future-%t", future), func(t *testing.T) {
			store, _, path := profileFixture(t)
			first := newProfileCampaign(t, store)
			name := filepath.Join(path, selectorFenceName)
			original, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			damaged := []byte("damaged fence")
			if future {
				damaged = []byte(`{"magic":"TBTD-SELECTOR-INTENT","schema":2,"future":true}`)
			}
			if err := os.WriteFile(name, damaged, 0600); err != nil {
				t.Fatal(err)
			}
			loaded := store.processProgress(t.Context(), session.ProgressInput{ID: "load", Kind: session.LoadActive})
			if future {
				if loaded.Status != session.StorageTooNew {
					t.Fatalf("future fence treated as corruption: %+v", loaded)
				}
				selected := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: first.Campaign})
				if selected.Status != session.StorageTooNew {
					t.Fatalf("future fence replaced: %+v", selected)
				}
				data, err := os.ReadFile(name)
				if err != nil || string(data) != string(damaged) {
					t.Fatalf("future fence changed: %v", err)
				}
				if err := os.WriteFile(name, original, 0600); err != nil {
					t.Fatal(err)
				}
				return
			}
			if loaded.Status != session.StorageCorrupt || loaded.Campaign != "" || len(loaded.Candidates) != 1 {
				t.Fatalf("damaged fence selected a campaign: %+v", loaded)
			}
			selected := store.processProgress(t.Context(), session.ProgressInput{ID: "select", Kind: session.SelectCampaign, Campaign: first.Campaign})
			if selected.Status != session.StorageSucceeded || selected.Campaign != first.Campaign || selected.Snapshot.Credits() != 0 {
				t.Fatalf("explicit recovery: %+v", selected)
			}
			copies, err := filepath.Glob(filepath.Join(path, ".tmp-*.preserved"))
			if err != nil || len(copies) != 1 {
				t.Fatalf("damaged fence was not preserved: %v, copies=%d", err, len(copies))
			}
			data, err := os.ReadFile(copies[0])
			if err != nil || string(data) != string(damaged) {
				t.Fatalf("preserved fence differs: %v", err)
			}
		})
	}
}

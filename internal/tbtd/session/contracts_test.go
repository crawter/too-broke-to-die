// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"errors"
	"math"
	"os"
	"reflect"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

const testID CampaignID = "01234567-89ab-4cde-8fab-0123456789ab"

func preparedCertification(t *testing.T) (campaign.Snapshot, campaign.Snapshot) {
	t.Helper()
	ctx := context.Background()
	manifest, err := content.LoadCampaign(ctx, os.DirFS("../../.."), "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	service, err := campaign.New(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	committed, err := service.Accept(ctx, started, "D-HOS-SUP-01")
	if err != nil {
		t.Fatal(err)
	}
	var deed content.CampaignDeed
	for _, candidate := range manifest.Deeds(ctx) {
		if candidate.Deed.ID == "D-HOS-SUP-01" {
			deed = candidate
			break
		}
	}
	event := campaign.Certification{Deed: deed.Deed.ID, Attempt: 1, Evidence: deed.Evidence, Trigger: deed.Trigger, HoldMilliseconds: deed.Deed.HoldMilliseconds}
	objects := make(map[content.ObjectID]content.Object)
	for _, object := range manifest.Objects(ctx) {
		objects[object.ID] = object
	}
	for _, id := range deed.Deed.Objects {
		object := objects[id]
		if object.Deed == deed.Deed.ID {
			event.EvidenceObjects = append(event.EvidenceObjects, id)
		}
		if object.Role == content.RoleTerminal {
			event.Terminal = id
		}
	}
	proposal, err := service.Prepare(ctx, committed, campaign.Transition{
		CampaignID: string(testID), Sequence: 1,
		Certification: &campaign.CertificationCandidate{Completed: true, Event: event},
	})
	if err != nil {
		t.Fatal(err)
	}
	return committed, proposal
}

func TestFrameCommandCopiesAndShape(t *testing.T) {
	ctx := context.Background()
	input := FrameInput{
		Elapsed: 16 * time.Millisecond,

		Values: []ActionValue{{Action: settings.Aim, X: 1}},

		Edges: []ActionEdge{{Action: settings.Jump, Kind: Pressed}, {Action: settings.Jump, Kind: Released}},

		Focus: FocusLost,

		Devices: []DeviceChange{{Device: settings.Controller, Connected: false, Active: true}},

		CloseRequested: true,

		Selections: []MenuSelection{{Action: "retry_save", Target: "event"}},
	}
	command, err := NewFrameCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Edges[0].Kind = Released
	input.Values[0].X = -1
	input.Devices[0].Active = false
	input.Selections[0].Target = "changed"
	got := command.Data(ctx)
	if got.Values[0].X != 1 || got.Edges[0].Kind != Pressed || !got.Devices[0].Active || got.Selections[0].Target != "event" || got.Focus != FocusLost || !got.CloseRequested {
		t.Fatalf("command lost input or shared it: %+v", got)
	}
	got.Edges[1].Kind = Pressed
	if command.Data(ctx).Edges[1].Kind != Released {
		t.Fatal("returned data aliases command")
	}
	for _, bad := range []FrameInput{
		{Elapsed: -1},
		{Focus: "unknown"},
		{Values: []ActionValue{{Action: settings.Aim, X: math.NaN()}}},
		{Values: []ActionValue{{Action: settings.Jump, X: 1.01}}},
		{Values: []ActionValue{{Action: "unknown"}}},
		{Values: []ActionValue{{Action: settings.Jump}, {Action: settings.Jump}}},
		{Edges: []ActionEdge{{Action: "unknown", Kind: Pressed}}},
		{Edges: []ActionEdge{{Action: settings.Jump, Kind: "repeat"}}},
		{Devices: []DeviceChange{{Device: "unsupported"}}},
		{Selections: []MenuSelection{{Target: "missing action"}}},
	} {
		if _, err := NewFrameCommand(ctx, bad); !errors.Is(err, InvalidAction) {
			t.Fatalf("invalid command accepted: %+v: %v", bad, err)
		}
	}
}

// fakeProgress is a hand-written external storage boundary. The test controls
// the outcome; this is not a filesystem or campaign implementation.
type fakeProgress struct {
	request ProgressRequest

	result ProgressResult
}

func (store *fakeProgress) Submit(_ context.Context, request ProgressRequest) error {
	store.request = request
	return nil
}

func (store *fakeProgress) Poll(_ context.Context, id RequestID) (ProgressResult, error) {
	if store.result.data.ID != id {
		return ProgressResult{}, ErrUnknownRequest
	}
	return store.result, nil
}

func TestProgressIdentityConflictAndAmbiguity(t *testing.T) {
	ctx := context.Background()
	_, proposal := preparedCertification(t)
	identity := OperationID{Event: "D-HOS-SUP-01", Sequence: 1}
	input := ProgressInput{ID: "write-1", Kind: CommitProgress, Campaign: testID, ExpectedRevision: 7, Operation: identity, Proposal: proposal}
	request, err := NewProgressRequest(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	bad := input
	bad.Operation.Sequence = 2
	if _, err := NewProgressRequest(ctx, bad); !errors.Is(err, InvalidAction) {
		t.Fatalf("sequence mismatch: %v", err)
	}
	bad = input
	bad.Campaign = "bad"
	if _, err := NewProgressRequest(ctx, bad); !errors.Is(err, InvalidAction) {
		t.Fatalf("bad campaign: %v", err)
	}
	resolution, err := request.Resolution()
	if err != nil {
		t.Fatal(err)
	}
	resolvedInput := resolution.Data()
	if resolvedInput.Kind != ResolveProgress || resolvedInput.ID != input.ID || resolvedInput.ExpectedRevision != 7 || resolvedInput.Operation != identity || !reflect.DeepEqual(resolvedInput.Proposal.Data(ctx), proposal.Data(ctx)) {
		t.Fatal("resolution changed prepared payload or identity")
	}
	returned := request.Data().Proposal.Data(ctx)
	returned.Receipts[0].Certification.EvidenceObjects[0] = "changed"
	if request.Data().Proposal.Data(ctx).Receipts[0].Certification.EvidenceObjects[0] == "changed" {
		t.Fatal("request exposed nested campaign data")
	}
	store := &fakeProgress{}
	var boundary ProgressStore = store
	if err := boundary.Submit(ctx, request); err != nil {
		t.Fatal(err)
	}
	for _, status := range []StorageStatus{StoragePending, StorageAmbiguous, StorageNotInstalled, StorageConflicting, StorageCorrupt, StorageTooNew, StorageIncompatible, StorageFailed, StorageSucceeded} {
		candidate := []RecoveryCandidate{{Campaign: testID, Revision: 7, Credits: 0, UnknownDifference: true}}
		data := ProgressResultData{ID: input.ID, Campaign: testID, Operation: identity, Status: status, Revision: 8, RevisionKnown: true, PayloadDigest: "digest-from-store", Candidates: candidate}
		if status == StorageConflicting {
			data.Code = StorageConflict
		}
		if status == StorageSucceeded {
			data.Snapshot = proposal
		}
		store.result = NewProgressResult(ctx, data)
		candidate[0].Credits = 99
		result, err := boundary.Poll(ctx, input.ID)
		if err != nil {
			t.Fatal(err)
		}
		got := result.Data(ctx)
		if got.Status != status || got.ID != input.ID || got.Campaign != testID || got.Operation != identity || got.Revision != 8 || !got.RevisionKnown || got.PayloadDigest != "digest-from-store" || got.Candidates[0].Credits != 0 || !got.Candidates[0].UnknownDifference {
			t.Fatalf("incorrect outcome: %+v", got)
		}
		got.Candidates[0].Credits = 42
		if result.Data(ctx).Candidates[0].Credits != 0 {
			t.Fatal("result aliases recovery candidates")
		}
		if status == StorageConflicting && !errors.Is(got.Code, StorageConflict) {
			t.Fatal("conflict identity lost")
		}
	}
	if err := boundary.Submit(ctx, resolution); err != nil || store.request.Data().Operation != identity {
		t.Fatalf("resolve lost identity: %v", err)
	}
	if _, err := boundary.Poll(ctx, "other"); !errors.Is(err, ErrUnknownRequest) {
		t.Fatalf("unknown request: %v", err)
	}
}

func TestExplicitRecoveryRevision(t *testing.T) {
	ctx := context.Background()
	for _, revision := range []uint64{0, 7} {
		input := ProgressInput{ID: "older", Kind: LoadRevision, Campaign: testID, ExpectedRevision: revision}
		request, err := NewProgressRequest(ctx, input)
		if err != nil || request.Data().ExpectedRevision != revision || request.Data().Kind != LoadRevision {
			t.Fatalf("recovery revision %d: %v", revision, err)
		}
		if _, err := request.Resolution(); !errors.Is(err, InvalidAction) {
			t.Fatalf("load is not a pending commit: %v", err)
		}
	}
	for _, input := range []ProgressInput{
		{ID: "older", Kind: LoadRevision, Campaign: "not-a-campaign"},
		{ID: "older", Kind: LoadRevision, Campaign: testID, Operation: OperationID{Event: "wrong", Sequence: 1}},
		{ID: "latest", Kind: LoadCampaign, Campaign: testID, ExpectedRevision: 7},
	} {
		if _, err := NewProgressRequest(ctx, input); !errors.Is(err, InvalidAction) {
			t.Fatalf("invalid recovery request accepted: %+v, %v", input, err)
		}
	}
}

type fakeSettings struct {
	request SettingsRequest

	result SettingsResult
}

func (store *fakeSettings) Submit(_ context.Context, request SettingsRequest) error {
	store.request = request
	return nil
}

func (store *fakeSettings) Poll(_ context.Context, id RequestID) (SettingsResult, error) {
	if store.result.data.ID != id {
		return SettingsResult{}, ErrUnknownRequest
	}
	return store.result, nil
}

func TestSettingsRequestAndResultIsolation(t *testing.T) {
	ctx := context.Background()
	values := settings.Defaults(ctx)
	request, err := NewSettingsRequest(ctx, SettingsInput{ID: "settings-1", Kind: SaveSettings, Values: values})
	if err != nil {
		t.Fatal(err)
	}
	values.Bindings[0].Control = "Z"
	got := request.Data(ctx)
	if got.Values.Bindings[0].Control != "A" {
		t.Fatal("request shares source bindings")
	}
	got.Values.Bindings[0].Control = "Q"
	if request.Data(ctx).Values.Bindings[0].Control != "A" {
		t.Fatal("request shares returned bindings")
	}
	result := NewSettingsResult(ctx, SettingsResultData{ID: "settings-1", Status: StorageCorrupt, Values: settings.Defaults(ctx), DefaultsRestored: true})
	var store SettingsStore = &fakeSettings{result: result}
	if err := store.Submit(ctx, request); err != nil {
		t.Fatal(err)
	}
	polled, err := store.Poll(ctx, "settings-1")
	if err != nil {
		t.Fatal(err)
	}
	resultData := polled.Data(ctx)
	resultData.Values.Bindings[0].Control = "Q"
	if result.Data(ctx).ID != "settings-1" || result.Data(ctx).Status != StorageCorrupt || result.Data(ctx).Values.Bindings[0].Control != "A" || !result.Data(ctx).DefaultsRestored {
		t.Fatal("settings result is not isolated or lost its identity")
	}
	if _, err := store.Poll(ctx, "other"); !errors.Is(err, ErrUnknownRequest) {
		t.Fatalf("unknown settings request: %v", err)
	}
	invalid := settings.Defaults(ctx)
	invalid.Audio.Master = 101
	if _, err := NewSettingsRequest(ctx, SettingsInput{ID: "bad", Kind: SaveSettings, Values: invalid}); !errors.Is(err, settings.ErrInvalid) {
		t.Fatalf("invalid settings accepted: %v", err)
	}
	if _, err := NewSettingsRequest(ctx, SettingsInput{ID: "load", Kind: LoadSettings}); err != nil {
		t.Fatal(err)
	}
}

func TestFrameViewCommittedPendingAndNestedCopies(t *testing.T) {
	ctx := context.Background()
	committed, proposal := preparedCertification(t)
	input := FrameViewData{
		State: StateError,

		Save: SaveError,

		Progress: ProgressView{Credits: 100},

		Pending: PendingEvent{Operation: OperationID{Event: "D-HOS-SUP-01", Sequence: 1}, Label: "service"},

		Error: StorageFull,

		Choices: []RecoveryChoice{RetrySave, ReloadSaved, ExitUnsaved},

		Candidates: []RecoveryCandidate{{Campaign: testID, Revision: 7, UnknownDifference: true}},

		Aim: AimView{Visible: true, Trajectory: []Point{{X: 10, Y: 20}}},

		Entities: []EntityView{{ID: "player", Parts: []VisualPart{{Slot: "arm", Asset: "ART-ARM"}}}},

		Events: []SemanticEvent{{Kind: "warning", Tick: 42, Entity: "player", Ordinal: 1, Parameters: []EventParameter{{Name: "direction", Value: "left"}}}},

		Menu: []MenuItem{{ID: "retry", Focused: true}},
	}
	view, err := NewFrameView(ctx, input, committed, testID, 7)
	if err != nil {
		t.Fatal(err)
	}
	input.Entities[0].Parts[0].Slot = "changed"
	input.Aim.Trajectory[0].X = 99
	input.Events[0].Parameters[0].Value = "changed"
	input.Choices[0] = ExitGame
	got := view.Data(ctx)
	if got.Progress.Credits != 0 || len(got.Progress.Receipts) != 0 || got.Pending.Operation.Event != "D-HOS-SUP-01" || got.Save != SaveError || got.Error != StorageFull || got.Choices[0] != RetrySave || got.Entities[0].Parts[0].Slot != "arm" || got.Events[0].Parameters[0].Value != "left" {
		t.Fatalf("pending view leaked proposal or aliases: %+v", got)
	}
	got.Entities[0].Parts[0].Slot = "again"
	got.Aim.Trajectory[0].X = 100
	got.Events[0].Parameters[0].Value = "again"
	got.Candidates[0].Revision = 99
	if view.Data(ctx).Entities[0].Parts[0].Slot != "arm" || view.Data(ctx).Aim.Trajectory[0].X != 10 || view.Data(ctx).Events[0].Parameters[0].Value != "left" || view.Data(ctx).Candidates[0].Revision != 7 {
		t.Fatal("view exposes nested copies")
	}
	input.State = StateActive
	input.Save = SaveConfirmed
	input.Pending = PendingEvent{}
	confirmed, err := NewFrameView(ctx, input, proposal, testID, 8)
	if err != nil {
		t.Fatal(err)
	}
	progress := confirmed.Data(ctx).Progress
	if progress.Credits != 1 || len(progress.Receipts) != 1 || progress.Receipts[0].Deed != "D-HOS-SUP-01" || progress.Revision != 8 || len(progress.Consequences) != 1 {
		t.Fatalf("acknowledged progress: %+v", progress)
	}
	progress.Receipts[0].Title = "changed"
	progress.Consequences[0] = "changed"
	if confirmed.Data(ctx).Progress.Receipts[0].Title == "changed" || confirmed.Data(ctx).Progress.Consequences[0] == "changed" {
		t.Fatal("committed presentation data is mutable")
	}
}

func TestDurableCuesRequireAcknowledgedProgress(t *testing.T) {
	ctx := context.Background()
	committed, proposal := preparedCertification(t)
	cue := SemanticEvent{Kind: "receipt", Durable: true, Revision: 8, Operation: OperationID{Event: "D-HOS-SUP-01", Sequence: 1}}
	data := FrameViewData{State: StateSaving, Save: SavePending, Events: []SemanticEvent{cue}}
	if _, err := NewFrameView(ctx, data, committed, testID, 7); !errors.Is(err, InvalidAction) {
		t.Fatalf("accepted a future reward cue: %v", err)
	}
	// Claiming the previous revision still cannot make a pending operation
	// visible: its sequence is not in the acknowledged snapshot.
	data.Events[0].Revision = 7
	if _, err := NewFrameView(ctx, data, committed, testID, 7); !errors.Is(err, InvalidAction) {
		t.Fatalf("accepted an uncommitted operation: %v", err)
	}
	data.Events[0] = cue
	view, err := NewFrameView(ctx, data, proposal, testID, 8)
	if err != nil || view.Data(ctx).Events[0].Operation != cue.Operation {
		t.Fatalf("acknowledged cue rejected: %v", err)
	}
	data.Events[0].Operation.Event = ""
	if _, err := NewFrameView(ctx, data, proposal, testID, 8); !errors.Is(err, InvalidAction) {
		t.Fatalf("accepted a cue without identity: %v", err)
	}
}

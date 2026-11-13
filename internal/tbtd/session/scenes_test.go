// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

func withSceneCatalogue(t *testing.T, service *Service) {
	t.Helper()
	ctx := context.Background()
	d := content.Definitions{
		SchemaVersion: 1, ContentVersion: "test",
		Locations:     []content.Location{{ID: "LOC-HOS", Name: "TXT-TEST"}},
		Encounters:    []content.Encounter{{ID: "ENC-HOS-01", Location: "LOC-HOS", Bounds: content.Rect{Width: 300, Height: 100}}},
		Anchors:       []content.Anchor{{ID: "ANC-HOS-01", Encounter: "ENC-HOS-01", X: 20}},
		Presentations: []content.Presentation{{ID: "TXT-TEST"}},
	}
	for _, id := range []content.NarrativeID{"NAR-01", "NAR-02", "NAR-03", "NAR-04", "NAR-05", "NAR-06", "NAR-07", "NAR-08", "NAR-09", "NAR-10", "NAR-11", "NAR-12"} {
		d.Narratives = append(d.Narratives, content.Narrative{ID: id, Location: "LOC-HOS"})
	}
	for _, id := range []content.NarrativeID{"NAR-01", "NAR-02", "NAR-03", "NAR-04", "NAR-05", "NAR-06", "NAR-07", "NAR-08", "NAR-09"} {
		d.Scenes = append(d.Scenes, content.Scene{ID: "SCN-" + string(id), Narrative: id, Units: []content.SceneUnit{
			{ID: "UNIT-" + string(id) + "-1", Text: "Первый", TextID: "TXT-TEST"},
			{ID: "UNIT-" + string(id) + "-2", Text: "Второй", TextID: "TXT-TEST"},
		}})
	}
	for _, item := range []struct {
		stage     content.EndingID
		narrative content.NarrativeID
	}{
		{"END-DEATH", "NAR-10"}, {"END-HELL", "NAR-11"}, {"END-CEREMONY", "NAR-12"}, {"END-TABLEAU", "NAR-12"},
	} {
		d.Endings = append(d.Endings, content.Ending{ID: item.stage, Anchor: "ANC-HOS-01", Narrative: item.narrative})
		units := []content.SceneUnit{{ID: "UNIT-" + string(item.stage) + "-1", Text: "Текст", TextID: "TXT-TEST"}}
		if item.stage != "END-TABLEAU" {
			units = append(units, content.SceneUnit{ID: "UNIT-" + string(item.stage) + "-2", Text: "Далее", TextID: "TXT-TEST"})
		}
		d.Scenes = append(d.Scenes, content.Scene{ID: "SCN-" + string(item.stage), Ending: item.stage, Units: units})
	}
	catalogue, err := content.Validate(ctx, d, content.Incremental)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := NewWithScenes(ctx, service.campaign, service.manifest, service.bays, service.store, catalogue)
	if err != nil {
		t.Fatal(err)
	}
	service.scenes = configured.scenes
}

func acknowledgeStory(t *testing.T, service *Service, store *recordingStore) FrameViewData {
	t.Helper()
	request := store.last()
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageSucceeded, Revision: request.ExpectedRevision + 1, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: request.Proposal})
	return frame(t, service, FrameInput{})
}

func tapScene(t *testing.T, service *Service) FrameViewData {
	t.Helper()
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.MenuConfirm, Kind: Pressed}}})
	return frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.MenuConfirm, Kind: Released}}})
}

func holdSkip(t *testing.T, service *Service) FrameViewData {
	t.Helper()
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.MenuConfirm, Kind: Pressed}}})
	var view FrameViewData
	for i := 0; i < 40; i++ {
		view = frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	return view
}

func TestNarrativeEntryUnitsAndInterruption(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	withSceneCatalogue(t, service)
	loadFixture(t, service, store, campaign.Snapshot{})
	if err := service.startNarrative(ctx, "NAR-01"); err != nil {
		t.Fatal(err)
	}
	request := store.last()
	if service.committed.Data(ctx).SceneEntry != "" || request.Proposal.Data(ctx).SceneUnit != "UNIT-NAR-01-1" {
		t.Fatal("entry published before acknowledgment or not recorded")
	}
	view := acknowledgeStory(t, service, store)
	if view.State != StateScene || view.Scene.Unit != "UNIT-NAR-01-1" || view.Progress.Credits != 0 {
		t.Fatalf("scene entry: %+v", view)
	}
	beforeWorld := service.world.Snapshot(ctx)
	view = frame(t, service, FrameInput{Elapsed: 4 * time.Millisecond})
	if view.Scene.Ticks != 0 {
		t.Fatalf("fractional tick advanced: %+v", view.Scene)
	}
	view = frame(t, service, FrameInput{Elapsed: 5 * time.Millisecond})
	if view.Scene.Ticks != 1 || !reflect.DeepEqual(beforeWorld, service.world.Snapshot(ctx)) {
		t.Fatal("scene clock did not advance independently")
	}
	view = tapScene(t, service)
	if view.State != StateSaving || view.Scene.Unit != "UNIT-NAR-01-1" || service.committed.Data(ctx).SceneUnit != "UNIT-NAR-01-1" {
		t.Fatal("next unit published before saving")
	}
	unitRequest := store.last()
	if unitRequest.Proposal.Data(ctx).SceneUnit != "UNIT-NAR-01-2" {
		t.Fatal("next unit was not recorded")
	}
	view = acknowledgeStory(t, service, store)
	if view.State != StateScene || view.Scene.Unit != "UNIT-NAR-01-2" || view.Scene.Ticks != 0 {
		t.Fatalf("unit acknowledgment: %+v", view)
	}
	// A new session restores the committed entry rather than the previous line.
	restored, restoredStore := sessionFixture(t)
	withSceneCatalogue(t, restored)
	choose(t, restored, "continue", "")
	load := restoredStore.last()
	restoredStore.reply(ProgressResultData{ID: load.ID, Campaign: testID, Status: StorageSucceeded, Revision: 9, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: unitRequest.Proposal})
	view = frame(t, restored, FrameInput{})
	if view.State != StateScene || view.Scene.Unit != "UNIT-NAR-01-2" || view.Scene.Ticks != 0 {
		t.Fatalf("restored entry: %+v", view)
	}
	view = tapScene(t, service)
	if view.State != StateSaving || len(service.committed.Data(ctx).Narratives) != 0 {
		t.Fatal("milestone published prematurely")
	}
	view = acknowledgeStory(t, service, store)
	if view.State != StateScene || view.Scene.Narrative != "NAR-02" || len(view.Progress.Narratives) != 1 || view.Progress.DebtCents != 0 {
		t.Fatalf("NAR-01 completion: %+v", view)
	}
}

func TestSkipHoldCancellationAndPause(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	withSceneCatalogue(t, service)
	loadFixture(t, service, store, campaign.Snapshot{})
	if err := service.startNarrative(ctx, "NAR-01"); err != nil {
		t.Fatal(err)
	}
	acknowledgeStory(t, service, store)
	view := holdSkip(t, service)
	if view.Confirmation != "scene_skip" || view.Scene.Unit != "UNIT-NAR-01-1" || view.Warning == "" {
		t.Fatalf("skip prompt: %+v", view)
	}
	frozen := view.Scene.Ticks
	view = frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	if view.Scene.Ticks != frozen {
		t.Fatal("confirmation advanced scene")
	}
	choose(t, service, "cancel", "")
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.MenuConfirm, Kind: Released}}})
	view = frame(t, service, FrameInput{Devices: []DeviceChange{{Device: settings.Controller, Active: true, Connected: false}}, Elapsed: 25 * time.Millisecond})
	if view.State != StatePause || view.Scene.Ticks != frozen {
		t.Fatalf("controller loss did not freeze: %+v", view)
	}
	view = choose(t, service, "resume", "")
	if view.State != StateScene {
		t.Fatalf("controller-loss resume: %+v", view)
	}
	view = frame(t, service, FrameInput{Focus: FocusLost})
	if view.State != StatePause || view.Scene.Unit != "UNIT-NAR-01-1" {
		t.Fatalf("focus pause: %+v", view)
	}
	frame(t, service, FrameInput{Focus: FocusGained, Elapsed: 25 * time.Millisecond})
	view = choose(t, service, "resume", "")
	if view.State != StateScene || view.Scene.Unit != "UNIT-NAR-01-1" {
		t.Fatalf("resume: %+v", view)
	}
	view = holdSkip(t, service)
	if view.Confirmation != "scene_skip" {
		t.Fatal("second hold did not prompt")
	}
	view = choose(t, service, "confirm", "")
	if view.State != StateSaving || view.Progress.Credits != 0 {
		t.Fatalf("skip bypassed commit: %+v", view)
	}
	view = acknowledgeStory(t, service, store)
	if view.Scene.Narrative != "NAR-02" || view.Progress.DebtCents != 0 {
		t.Fatalf("skip outcome: %+v", view)
	}
}

func TestPrologueAndHospitalArrivalThroughFrames(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	withSceneCatalogue(t, service)
	loadFixture(t, service, store, campaign.Snapshot{})
	move := []ActionValue{{Action: settings.MoveRight, X: 0.625}}
	frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Values: move, Edges: []ActionEdge{{Action: settings.Jump, Kind: Pressed}}})
	for i := 0; i < 100; i++ {
		_, world := service.world.Snapshot(ctx).Data(ctx)
		if world.Player.Support == "prologue-platform-000" {
			break
		}
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	_, world := service.world.Snapshot(ctx).Data(ctx)
	if world.Player.Support != "prologue-platform-000" {
		t.Fatalf("first jump: %+v", world.Player)
	}
	frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Jump, Kind: Released}}})
	for i := 0; i < 30; i++ {
		_, world = service.world.Snapshot(ctx).Data(ctx)
		if world.Player.Bounds.X+world.Player.Bounds.Width/2 >= 160 {
			break
		}
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Jump, Kind: Pressed}}})
	for i := 0; i < 100; i++ {
		_, world = service.world.Snapshot(ctx).Data(ctx)
		if world.Player.Support == "prologue-platform-001" {
			break
		}
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	_, world = service.world.Snapshot(ctx).Data(ctx)
	if world.Player.Support != "prologue-platform-001" {
		t.Fatalf("second jump: %+v", world.Player)
	}
	for i := 0; i < 50; i++ {
		_, world = service.world.Snapshot(ctx).Data(ctx)
		if world.Player.Bounds.X+world.Player.Bounds.Width/2 >= 330 {
			break
		}
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight}}, Edges: []ActionEdge{{Action: settings.Jump, Kind: Released}}})
	for i := 0; i < 20; i++ {
		_, world = service.world.Snapshot(ctx).Data(ctx)
		if world.Player.Velocity.X == 0 {
			break
		}
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	interact := func() FrameViewData {
		t.Helper()
		view := frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Interact, Kind: Pressed}}})
		for i := 0; i < 20 && view.State == StateActive; i++ {
			view = frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
		}
		return view
	}
	view := interact()
	if view.Confirmation != "steal_donations" || view.Warning != "Украсть пожертвования на лечение рака?" {
		t.Fatalf("theft prompt: %+v, target %+v", view, service.world.InteractionView(ctx))
	}
	choose(t, service, "cancel", "")
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Interact, Kind: Released}}})
	choose(t, service, "resume", "")
	view = interact()
	if view.Confirmation != "steal_donations" || len(store.requests) != 1 {
		t.Fatalf("rejected theft changed campaign: state=%s confirmation=%s requests=%d target=%+v", view.State, view.Confirmation, len(store.requests), service.world.InteractionView(ctx))
	}
	view = choose(t, service, "confirm", "")
	if view.State != StateSaving || service.committed.Data(ctx).DonationTheft {
		t.Fatal("theft published before save")
	}
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Interact, Kind: Released}}})
	view = acknowledgeStory(t, service, store)
	if view.State != StatePause || !service.committed.Data(ctx).DonationTheft {
		t.Fatalf("theft acknowledgment: %+v", view)
	}
	choose(t, service, "resume", "")
	for i := 0; i < 100 && service.policy.State() == StateActive; i++ {
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight, X: 1}}})
	}
	if service.policy.State() != StateSaving || store.last().Proposal.Data(ctx).SceneEntry != "NAR-01" {
		t.Fatal("pursuit did not save NAR-01 entry")
	}
	_, world = service.world.Snapshot(ctx).Data(ctx)
	position := world.Player.Bounds.X + world.Player.Bounds.Width/2
	if position < 336 || position >= 339 {
		t.Fatalf("heart attack did not stop on the 320-pixel boundary: %v", position)
	}
	view = acknowledgeStory(t, service, store)
	for _, narrative := range []content.NarrativeID{"NAR-01", "NAR-02", "NAR-03"} {
		if view.Scene.Narrative != narrative {
			t.Fatalf("expected %s: %+v", narrative, view.Scene)
		}
		for i := 0; i < 2; i++ {
			choose(t, service, "scene_advance", "")
			view = acknowledgeStory(t, service, store)
		}
	}
	if view.State != StateActive || len(view.Progress.Narratives) != 3 {
		t.Fatalf("bargain completion: %+v", view)
	}
	frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight}}})
	view = interact()
	if view.State != StateSaving || store.last().Proposal.Data(ctx).SceneEntry != "NAR-04" {
		t.Fatalf("hospital arrival: %+v", view)
	}
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Interact, Kind: Released}}})
	view = acknowledgeStory(t, service, store)
	if view.Scene.Narrative != "NAR-04" || view.Progress.Credits != 0 || view.Progress.DebtCents != 0 {
		t.Fatalf("arrival acknowledgment: %+v", view)
	}
	for i := 0; i < 2; i++ {
		choose(t, service, "scene_advance", "")
		view = acknowledgeStory(t, service, store)
	}
	if view.State != StateActive || len(view.Progress.Narratives) != 4 {
		t.Fatalf("arrival completion: %+v", view)
	}
	for i := 0; i < 40; i++ {
		_, world = service.world.Snapshot(ctx).Data(ctx)
		if world.Player.Bounds.X >= 150 {
			break
		}
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight, X: 1}}})
	}
	frame(t, service, FrameInput{Elapsed: 100 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight}}})
	for i := 0; i < 20; i++ {
		_, world = service.world.Snapshot(ctx).Data(ctx)
		if world.Player.Velocity.X == 0 {
			break
		}
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	view = interact()
	if view.State != StatePause {
		t.Fatalf("service console: %+v", view)
	}
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Interact, Kind: Released}}})
	view = choose(t, service, "accept_request", "D-HOS-SUP-01")
	if view.State != StateSaving {
		t.Fatalf("ticket acceptance: %+v", view)
	}
	view = acknowledgeStory(t, service, store)
	if view.Progress.Active != "D-HOS-SUP-01" || view.Progress.Credits != 0 || view.Progress.DebtCents != 0 {
		t.Fatalf("accepted first ticket: %+v", view)
	}
}

func TestInstituteArrivalThroughPassageAndRecovery(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	withSceneCatalogue(t, service)
	progress := authoredProgress(t, service, 3, true)
	loadFixture(t, service, store, progress)
	view := frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Interact, Kind: Pressed}}})
	for i := 0; i < 20 && view.State == StateActive; i++ {
		view = frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	request := store.last()
	if view.State != StateSaving || request.Proposal.Data(ctx).SceneEntry != "NAR-08" || len(view.Progress.Narratives) != 7 {
		t.Fatalf("institute passage: %+v", view)
	}
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Interact, Kind: Released}}})
	old, oldStore := sessionFixture(t)
	withSceneCatalogue(t, old)
	choose(t, old, "continue", "")
	oldLoad := oldStore.last()
	oldStore.reply(ProgressResultData{ID: oldLoad.ID, Campaign: testID, Status: StorageSucceeded, Revision: 7, RevisionKnown: true, PayloadDigest: oldDigest, Snapshot: progress})
	if oldView := frame(t, old, FrameInput{}); oldView.State != StatePause || oldView.Progress.Credits != 75 || oldView.Scene.Narrative != "" {
		t.Fatalf("old revision: %+v", oldView)
	}
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageAmbiguous, Code: StorageFull, PayloadDigest: newDigest})
	if failed := frame(t, service, FrameInput{}); failed.State != StateError || failed.Progress.Credits != 75 || failed.Scene.Narrative != "" {
		t.Fatalf("entry failure: %+v", failed)
	}
	choose(t, service, "retry_save", "")
	resolution := store.last()
	if resolution.Kind != ResolveProgress || resolution.ID != request.ID || resolution.Operation != request.Operation || !reflect.DeepEqual(resolution.Proposal.Data(ctx), request.Proposal.Data(ctx)) {
		t.Fatal("entry resolution changed identity")
	}
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageNotInstalled, PayloadDigest: newDigest})
	frame(t, service, FrameInput{})
	if retry := store.last(); retry.Kind != CommitProgress || retry.ID != request.ID || retry.Operation != request.Operation {
		t.Fatal("entry retry changed identity")
	}
	view = acknowledgeStory(t, service, store)
	if view.State != StateScene || view.Scene.Narrative != "NAR-08" || view.Progress.Credits != 75 {
		t.Fatalf("institute arrival: %+v", view)
	}
	restored, boundary := sessionFixture(t)
	withSceneCatalogue(t, restored)
	choose(t, restored, "continue", "")
	load := boundary.last()
	boundary.reply(ProgressResultData{ID: load.ID, Campaign: testID, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: request.Proposal})
	view = frame(t, restored, FrameInput{})
	if view.State != StateScene || view.Scene.Unit != "UNIT-NAR-08-1" || view.Progress.Credits != 75 || len(boundary.requests) != 1 {
		t.Fatalf("restored arrival: %+v", view)
	}
	for i := 0; i < 2; i++ {
		choose(t, service, "scene_advance", "")
		view = acknowledgeStory(t, service, store)
	}
	if view.State != StateActive || len(view.Progress.Narratives) != 8 {
		t.Fatalf("arrival completion: %+v", view)
	}
	for i := 0; i < 40; i++ {
		_, world := service.world.Snapshot(ctx).Data(ctx)
		if world.Player.Bounds.X >= 150 {
			break
		}
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight, X: 1}}})
	}
	frame(t, service, FrameInput{Elapsed: 100 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight}}})
	for i := 0; i < 20; i++ {
		_, world := service.world.Snapshot(ctx).Data(ctx)
		if world.Player.Velocity.X == 0 {
			break
		}
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	view = frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Interact, Kind: Pressed}}})
	for i := 0; i < 20 && view.State == StateActive; i++ {
		view = frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	if view.State != StatePause {
		t.Fatalf("institute console: %+v", view)
	}
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Interact, Kind: Released}}})
	if view = choose(t, service, "accept_request", "D-INS-SUP-01"); view.State != StateSaving {
		t.Fatalf("institute request: %+v", view)
	}
	view = acknowledgeStory(t, service, store)
	if view.Progress.Active != "D-INS-SUP-01" || view.Progress.Credits != 75 {
		t.Fatalf("accepted institute ticket: %+v", view)
	}
}

func TestServiceAcceptanceCannotInventArrivalScene(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	withSceneCatalogue(t, service)
	started, err := service.campaign.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	loadFixture(t, service, store, started)
	if err = service.requestService(ctx, campaign.AttemptAccept, "D-HOS-SUP-01"); !errors.Is(err, campaign.ErrBlocked) {
		t.Fatalf("ticket bypassed arrival: %v", err)
	}
	if len(store.requests) != 1 || len(service.committed.Data(ctx).Narratives) != 3 {
		t.Fatal("rejected ticket fabricated a milestone")
	}
}

func TestPermitCounterRequiresRealInteractionAndAcknowledgment(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	withSceneCatalogue(t, service)
	progress := authoredProgress(t, service, 4, true)
	var err error
	progress, err = service.campaign.Checkpoint(ctx, progress, "ANC-MIN-PERMIT")
	if err != nil {
		t.Fatal(err)
	}
	loadFixture(t, service, store, progress)
	frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Interact, Kind: Pressed}}})
	for i := 0; i < 20 && service.policy.State() == StateActive; i++ {
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	if service.policy.State() != StateSaving || service.committed.Data(ctx).Permit || !store.last().Proposal.Data(ctx).Permit {
		t.Fatal("review did not prepare a private permit after the counter hold")
	}
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Interact, Kind: Released}}})
	view := acknowledgeStory(t, service, store)
	if view.State != StateActive || !view.Progress.Permit || view.Progress.Credits != 100 || view.Progress.DebtCents != 0 {
		t.Fatalf("permit acknowledgment: %+v", view)
	}
	count := len(store.requests)
	frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Interact, Kind: Pressed}}})
	for i := 0; i < 20; i++ {
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	if len(store.requests) != count {
		t.Fatal("repeated review created another permit")
	}
}

func TestClosureScenesFollowCommittedTerminalAndDoNotDuplicateRewards(t *testing.T) {
	ctx := context.Background()
	for institution, narrative := range []content.NarrativeID{"NAR-05", "NAR-06", "NAR-07", "NAR-09"} {
		t.Run(string(narrative), func(t *testing.T) {
			service, store := sessionFixture(t)
			withSceneCatalogue(t, service)
			progress := authoredProgress(t, service, institution+1, false)
			closure := service.manifest.Deeds(ctx)[institution*25+24]
			progress, err := service.campaign.DefeatBoss(ctx, progress, closure.Deed.Encounter)
			if err != nil {
				t.Fatal(err)
			}
			loadFixture(t, service, store, progress)
			for i := 0; i < 400; i++ {
				_, world := service.world.Snapshot(ctx).Data(ctx)
				if world.Player.Bounds.X >= 1270 {
					break
				}
				frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight, X: 1}}})
			}
			frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight}}})
			for i := 0; i < 20; i++ {
				_, world := service.world.Snapshot(ctx).Data(ctx)
				if world.Player.Velocity.X == 0 {
					break
				}
				frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
			}
			prompt := service.world.InteractionView(ctx)
			if !prompt.Found || prompt.Target.ID != string(closure.Deed.Objects[0]) {
				t.Fatalf("closure terminal: %+v", prompt)
			}
			view := frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Interact, Kind: Pressed}}})
			for i := 0; i < 80 && view.State == StateActive; i++ {
				view = frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
			}
			request := store.last()
			if view.State != StateSaving || request.Proposal.Data(ctx).SceneEntry != narrative || request.Proposal.Credits() != progress.Credits()+1 || view.Progress.Credits != progress.Credits() {
				t.Fatalf("closure pending: %+v", view)
			}
			frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Interact, Kind: Released}}})
			view = acknowledgeStory(t, service, store)
			if view.State != StateScene || view.Scene.Narrative != narrative || view.Progress.Credits != progress.Credits()+1 {
				t.Fatalf("closure acknowledgment: %+v", view)
			}
			before := service.committed.Data(ctx)
			transition := campaign.Transition{CampaignID: string(testID), Sequence: request.Operation.Sequence, Closure: &campaign.ClosureCandidate{Deed: closure.Deed.ID, Terminal: closure.Deed.Objects[0], HoldMilliseconds: closure.Deed.HoldMilliseconds, SceneUnit: "UNIT-" + string(narrative) + "-1"}}
			retried, retryErr := service.campaign.Prepare(ctx, service.committed, transition)
			if retryErr != nil || !reflect.DeepEqual(retried.Data(ctx), before) {
				t.Fatalf("closure retry changed progress: %v", retryErr)
			}
			restored, boundary := sessionFixture(t)
			withSceneCatalogue(t, restored)
			choose(t, restored, "continue", "")
			load := boundary.last()
			boundary.reply(ProgressResultData{ID: load.ID, Campaign: testID, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: request.Proposal})
			restoredView := frame(t, restored, FrameInput{})
			if restoredView.State != StateScene || restoredView.Scene.Unit != "UNIT-"+string(narrative)+"-1" || restoredView.Progress.Credits != view.Progress.Credits {
				t.Fatalf("closure restored: %+v", restoredView)
			}
			view = holdSkip(t, service)
			if view.Confirmation != "scene_skip" {
				t.Fatalf("closure skip prompt: %+v", view)
			}
			view = choose(t, service, "confirm", "")
			if view.State != StateSaving || view.Progress.Credits != progress.Credits()+1 {
				t.Fatalf("closure skip pending: %+v", view)
			}
			frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.MenuConfirm, Kind: Released}}})
			view = acknowledgeStory(t, service, store)
			after := service.committed.Data(ctx)
			if view.State != StateActive || after.SceneEntry != "" || !reflect.DeepEqual(before.Receipts, after.Receipts) || !reflect.DeepEqual(before.Narratives, after.Narratives) || !reflect.DeepEqual(before.Incidents, after.Incidents) || before.DebtCents != after.DebtCents {
				t.Fatalf("closure skip changed durable rewards: %+v", view)
			}
		})
	}
}

func TestClosureCannotBypassEvidenceOrBoss(t *testing.T) {
	ctx := context.Background()
	service, _ := sessionFixture(t)
	progress := authoredProgress(t, service, 1, false)
	deed := service.manifest.Deeds(ctx)[24]
	candidate := campaign.ClosureCandidate{Deed: deed.Deed.ID, Terminal: deed.Deed.Objects[0], HoldMilliseconds: deed.Deed.HoldMilliseconds, SceneUnit: "UNIT-NAR-05-1"}
	transition := campaign.Transition{CampaignID: string(testID), Sequence: 1, Closure: &candidate}
	if _, err := service.campaign.Prepare(ctx, progress, transition); !errors.Is(err, campaign.ErrBlocked) {
		t.Fatalf("closure before boss: %v", err)
	}
	progress, err := service.campaign.DefeatBoss(ctx, progress, deed.Deed.Encounter)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []campaign.ClosureCandidate{
		{Deed: deed.Deed.ID, Terminal: "OBJ-OTHER", HoldMilliseconds: deed.Deed.HoldMilliseconds, SceneUnit: "UNIT-NAR-05-1"},
		{Deed: deed.Deed.ID, Terminal: deed.Deed.Objects[0], HoldMilliseconds: deed.Deed.HoldMilliseconds - 1, SceneUnit: "UNIT-NAR-05-1"},
		{Deed: deed.Deed.ID, Terminal: deed.Deed.Objects[0], HoldMilliseconds: deed.Deed.HoldMilliseconds, SceneUnit: "bad"},
	} {
		transition.Closure = &invalid
		if _, err = service.campaign.Prepare(ctx, progress, transition); !errors.Is(err, campaign.ErrInvalid) {
			t.Fatalf("invalid closure accepted: %v", err)
		}
	}
	if progress.Credits() != 24 || len(progress.Data(ctx).Narratives) != 4 {
		t.Fatal("rejected closure changed campaign")
	}
}

func TestSceneUnitRecoveryAcrossAmbiguousWrites(t *testing.T) {
	ctx := context.Background()
	fixture, _ := sessionFixture(t)
	hospital, err := fixture.campaign.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	institute := authoredProgress(t, fixture, 3, true)
	ending := authoredProgress(t, fixture, 4, true)
	ending, err = fixture.campaign.IssuePermit(ctx, ending)
	if err != nil {
		t.Fatal(err)
	}
	ending, err = fixture.campaign.ConsentDecision(ctx, ending, true)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		snapshot campaign.Snapshot
		entry    string
	}{}
	firstEntry, err := fixture.campaign.Prepare(ctx, campaign.Snapshot{}, campaign.Transition{CampaignID: string(testID), Sequence: 1, Story: &campaign.StoryChange{Action: campaign.StorySceneEnter, Narrative: "NAR-01", Unit: "UNIT-NAR-01-1"}})
	if err != nil {
		t.Fatal(err)
	}
	secondEntry, err := fixture.campaign.CompleteNarrative(ctx, campaign.Snapshot{}, "NAR-01")
	if err != nil {
		t.Fatal(err)
	}
	thirdEntry, err := fixture.campaign.CompleteNarrative(ctx, secondEntry, "NAR-02")
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases,
		struct {
			name     string
			snapshot campaign.Snapshot
			entry    string
		}{"NAR-01", firstEntry, "UNIT-NAR-01"},
		struct {
			name     string
			snapshot campaign.Snapshot
			entry    string
		}{"NAR-02", secondEntry, "UNIT-NAR-02"},
		struct {
			name     string
			snapshot campaign.Snapshot
			entry    string
		}{"NAR-03", thirdEntry, "UNIT-NAR-03"},
	)
	for _, item := range []struct {
		id       content.NarrativeID
		snapshot campaign.Snapshot
	}{{"NAR-04", hospital}, {"NAR-08", institute}} {
		transition := campaign.Transition{CampaignID: string(testID), Sequence: item.snapshot.Data(ctx).OperationSequence + 1, Story: &campaign.StoryChange{Action: campaign.StorySceneEnter, Narrative: item.id, Unit: "UNIT-" + string(item.id) + "-1"}}
		entered, prepareErr := fixture.campaign.Prepare(ctx, item.snapshot, transition)
		if prepareErr != nil {
			t.Fatal(prepareErr)
		}
		cases = append(cases, struct {
			name     string
			snapshot campaign.Snapshot
			entry    string
		}{string(item.id), entered, "UNIT-" + string(item.id)})
	}
	for institution, narrative := range []content.NarrativeID{"NAR-05", "NAR-06", "NAR-07", "NAR-09"} {
		progress := authoredProgress(t, fixture, institution+1, false)
		deed := fixture.manifest.Deeds(ctx)[institution*25+24]
		progress, err = fixture.campaign.DefeatBoss(ctx, progress, deed.Deed.Encounter)
		if err != nil {
			t.Fatal(err)
		}
		transition := campaign.Transition{CampaignID: string(testID), Sequence: progress.Data(ctx).OperationSequence + 1, Closure: &campaign.ClosureCandidate{Deed: deed.Deed.ID, Terminal: deed.Deed.Objects[0], HoldMilliseconds: deed.Deed.HoldMilliseconds, SceneUnit: "UNIT-" + string(narrative) + "-1"}}
		progress, err = fixture.campaign.Prepare(ctx, progress, transition)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, struct {
			name     string
			snapshot campaign.Snapshot
			entry    string
		}{string(narrative), progress, "UNIT-" + string(narrative)})
	}
	for _, stage := range []campaign.EndingStage{campaign.EndingDeath, campaign.EndingHell, campaign.EndingCeremony} {
		cases = append(cases, struct {
			name     string
			snapshot campaign.Snapshot
			entry    string
		}{string(stage), ending, "UNIT-" + string(stage)})
		ending, err = fixture.campaign.AdvanceEnding(ctx, ending, stage)
		if err != nil {
			t.Fatal(err)
		}
	}
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, store := sessionFixture(t)
			withSceneCatalogue(t, service)
			choose(t, service, "continue", "")
			load := store.last()
			store.reply(ProgressResultData{ID: load.ID, Campaign: testID, Status: StorageSucceeded, Revision: 7, RevisionKnown: true, PayloadDigest: oldDigest, Snapshot: test.snapshot})
			view := frame(t, service, FrameInput{})
			if view.State != StateScene || view.Scene.Unit != test.entry+"-1" {
				t.Fatalf("entry: %+v", view)
			}
			view = choose(t, service, "scene_advance", "")
			request := store.last()
			if view.State != StateSaving || view.Scene.Unit != test.entry+"-1" || request.Proposal.Data(ctx).SceneUnit != test.entry+"-2" {
				t.Fatalf("pending unit: %+v", view)
			}
			// Either complete revision can be recovered after interruption.
			for _, candidate := range []struct {
				snapshot campaign.Snapshot
				revision uint64
				unit     string
			}{{test.snapshot, 7, test.entry + "-1"}, {request.Proposal, 8, test.entry + "-2"}} {
				restored, boundary := sessionFixture(t)
				withSceneCatalogue(t, restored)
				choose(t, restored, "continue", "")
				load := boundary.last()
				boundary.reply(ProgressResultData{ID: load.ID, Campaign: testID, Status: StorageSucceeded, Revision: candidate.revision, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: candidate.snapshot})
				got := frame(t, restored, FrameInput{})
				if got.State != StateScene || got.Scene.Unit != candidate.unit || got.Scene.Ticks != 0 || len(boundary.requests) != 1 {
					t.Fatalf("restored revision %d: %+v", candidate.revision, got)
				}
			}
			status := StorageAmbiguous
			if index%2 == 0 {
				status = StorageFailed
			}
			store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: status, Code: StorageFull, PayloadDigest: newDigest})
			view = frame(t, service, FrameInput{})
			if view.State != StateError || view.Scene.Unit != test.entry+"-1" {
				t.Fatalf("failure: %+v", view)
			}
			choose(t, service, "retry_save", "")
			resolution := store.last()
			if resolution.Kind != ResolveProgress || resolution.ID != request.ID || resolution.Operation != request.Operation || !reflect.DeepEqual(resolution.Proposal.Data(ctx), request.Proposal.Data(ctx)) {
				t.Fatal("resolution changed scene transition")
			}
			store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageNotInstalled, PayloadDigest: newDigest})
			frame(t, service, FrameInput{})
			retry := store.last()
			if retry.Kind != CommitProgress || retry.ID != request.ID || retry.Operation != request.Operation || !reflect.DeepEqual(retry.Proposal.Data(ctx), request.Proposal.Data(ctx)) {
				t.Fatal("retry changed scene transition")
			}
			view = acknowledgeStory(t, service, store)
			if view.State != StateScene || view.Scene.Unit != test.entry+"-2" {
				t.Fatalf("retry acknowledgement: %+v", view)
			}
			count := len(store.requests)
			store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: request.Proposal})
			frame(t, service, FrameInput{})
			if len(store.requests) != count || service.committed.Data(ctx).OperationSequence != request.Operation.Sequence {
				t.Fatal("duplicate acknowledgment changed progress")
			}
		})
	}
}

func TestWindowCloseDuringConsentRestoresPrompt(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	withSceneCatalogue(t, service)
	progress := authoredProgress(t, service, 4, true)
	progress, err := service.campaign.IssuePermit(ctx, progress)
	if err != nil {
		t.Fatal(err)
	}
	loadFixture(t, service, store, progress)
	view := choose(t, service, "consent", "")
	if view.State != StateScene || view.Confirmation != "consent" {
		t.Fatalf("consent: %+v", view)
	}
	view = frame(t, service, FrameInput{CloseRequested: true})
	if view.State != StatePause || view.Confirmation != "quit_to_title" {
		t.Fatalf("close: %+v", view)
	}
	// A second close must not erase the suspended confirmation.
	frame(t, service, FrameInput{CloseRequested: true})
	view = choose(t, service, "cancel", "")
	if view.State != StatePause || view.Confirmation != "consent" {
		t.Fatalf("cancel quit: %+v", view)
	}
	view = choose(t, service, "resume", "")
	if view.State != StateScene || view.Confirmation != "consent" || view.Warning == "" {
		t.Fatalf("resume consent: %+v", view)
	}
	view = choose(t, service, "cancel", "")
	if view.State != StateActive || view.Confirmation != "" || len(store.requests) != 1 || view.Progress.Consent {
		t.Fatalf("cancel consent: %+v", view)
	}
}

func TestEndingConsentCompletionAndReplay(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	withSceneCatalogue(t, service)
	progress := authoredProgress(t, service, 4, true)
	var err error
	progress, err = service.campaign.IssuePermit(ctx, progress)
	if err != nil {
		t.Fatal(err)
	}
	loadFixture(t, service, store, progress)
	if view := choose(t, service, "consent", ""); view.Confirmation != "consent" || view.State != StateScene {
		t.Fatalf("consent prompt: %+v", view)
	}
	worldBefore := service.world.Snapshot(ctx)
	frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	if !reflect.DeepEqual(worldBefore, service.world.Snapshot(ctx)) {
		t.Fatal("gameplay advanced beneath consent")
	}
	frame(t, service, FrameInput{Focus: FocusLost})
	if view := choose(t, service, "confirm", ""); view.State != StatePause || len(store.requests) != 1 {
		t.Fatal("consent confirmed during focus pause")
	}
	frame(t, service, FrameInput{Focus: FocusGained})
	choose(t, service, "resume", "")
	choose(t, service, "cancel", "")
	if len(store.requests) != 1 || service.committed.Data(ctx).Consent {
		t.Fatal("cancel saved consent")
	}
	choose(t, service, "consent", "")
	if view := choose(t, service, "confirm", ""); view.State != StateSaving || view.Progress.Consent {
		t.Fatal("consent published before acknowledgment")
	}
	view := acknowledgeStory(t, service, store)
	if view.State != StateScene || view.Scene.Ending != "END-DEATH" || view.Progress.DebtCents != 0 {
		t.Fatalf("consent: %+v", view)
	}
	for _, stage := range []campaign.EndingStage{campaign.EndingDeath, campaign.EndingHell, campaign.EndingCeremony} {
		if view.Scene.Ending != content.EndingID(stage) {
			t.Fatalf("expected %s, got %+v", stage, view.Scene)
		}
		// A fresh session resumes the committed stage without a new event.
		restored, restoredStore := sessionFixture(t)
		withSceneCatalogue(t, restored)
		choose(t, restored, "continue", "")
		load := restoredStore.last()
		restoredStore.reply(ProgressResultData{ID: load.ID, Campaign: testID, Status: StorageSucceeded, Revision: service.revision, RevisionKnown: true, PayloadDigest: service.digest, Snapshot: service.committed})
		restoredView := frame(t, restored, FrameInput{})
		if restoredView.State != StateScene || restoredView.Scene.Ending != content.EndingID(stage) || len(restoredStore.requests) != 1 {
			t.Fatalf("restore %s: %+v", stage, restoredView)
		}
		if stage != campaign.EndingCeremony {
			view = tapScene(t, service)
			if view.State != StateSaving {
				t.Fatalf("unit not saved: %+v", view)
			}
			view = acknowledgeStory(t, service, store)
			if view.Scene.Ending != content.EndingID(stage) || view.Scene.Unit != "UNIT-"+string(stage)+"-2" {
				t.Fatalf("unit acknowledgement: %+v", view)
			}
		}
		if stage == campaign.EndingCeremony {
			view = holdSkip(t, service)
			if view.Confirmation != "scene_skip" {
				t.Fatal("ceremony skip did not prompt")
			}
			view = choose(t, service, "confirm", "")
			frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.MenuConfirm, Kind: Released}}})
		} else {
			view = tapScene(t, service)
		}
		if view.State != StateSaving {
			t.Fatalf("stage not saved: %+v", view)
		}
		count := len(store.requests)
		choose(t, service, "confirm", "")
		if len(store.requests) != count {
			t.Fatal("duplicate confirmation submitted another transition")
		}
		view = acknowledgeStory(t, service, store)
	}
	if view.Scene.Ending != "END-TABLEAU" || view.Progress.Completed {
		t.Fatalf("ceremony did not lead to tableau: %+v", view)
	}
	view = tapScene(t, service)
	request := store.last()
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageFailed, Code: StorageFull})
	view = frame(t, service, FrameInput{})
	if view.State != StateError || view.Progress.Completed {
		t.Fatal("failed completion reached end")
	}
	choose(t, service, "retry_save", "")
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageSucceeded, Revision: request.ExpectedRevision + 1, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: request.Proposal})
	view = frame(t, service, FrameInput{})
	if view.State != StateEnd || !view.Progress.Completed {
		t.Fatalf("completion acknowledgment: %+v", view)
	}
	before := service.committed.Data(ctx)
	writes := len(store.requests)
	for repeat := 0; repeat < 2; repeat++ {
		view = choose(t, service, "replay_ending", "")
		if view.Scene.Ending != "END-DEATH" || !view.Scene.Replay || view.Scene.ReplayLabel != "Повтор финала" {
			t.Fatalf("replay start: %+v", view)
		}
		for i := 0; i < 7; i++ {
			view = tapScene(t, service)
		}
		if view.State != StateEnd || !reflect.DeepEqual(before, service.committed.Data(ctx)) || len(store.requests) != writes {
			t.Fatalf("replay %d changed campaign or wrote", repeat)
		}
	}
	choose(t, service, "replay_ending", "")
	for _, next := range []content.EndingID{"END-HELL", "END-CEREMONY", "END-TABLEAU"} {
		view = holdSkip(t, service)
		if view.Confirmation != "scene_skip" {
			t.Fatalf("replay skip confirmation: %+v", view)
		}
		view = choose(t, service, "confirm", "")
		frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.MenuConfirm, Kind: Released}}})
		if view.Scene.Ending != next || view.State != StateScene {
			t.Fatalf("replay skip: %+v", view)
		}
	}
	view = holdSkip(t, service)
	if view.Confirmation != "" || view.Scene.Ending != "END-TABLEAU" {
		t.Fatal("tableau was skippable")
	}
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.MenuConfirm, Kind: Released}}})
	if service.policy.State() != StateEnd {
		t.Fatal("tableau acknowledgment did not exit replay")
	}
	choose(t, service, "replay_ending", "")
	view = frame(t, service, FrameInput{Focus: FocusLost})
	if view.State != StatePause {
		t.Fatal("replay interruption did not pause")
	}
	view = choose(t, service, "exit_replay", "")
	if view.State != StateEnd || len(store.requests) != writes || !reflect.DeepEqual(before, service.committed.Data(ctx)) {
		t.Fatal("interrupted replay changed durable progress")
	}
}

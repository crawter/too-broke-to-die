// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"os"
	"strings"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

type sceneFailureStore struct {
	request session.ProgressRequest
	saved   campaign.Snapshot
}

func (store *sceneFailureStore) Submit(_ context.Context, request session.ProgressRequest) error {
	store.request = request
	return nil
}

func (store *sceneFailureStore) Poll(ctx context.Context, id session.RequestID) (session.ProgressResult, error) {
	if store.request.Data().Kind == session.LoadActive {
		return session.NewProgressResult(ctx, session.ProgressResultData{ID: id, Status: session.StorageSucceeded, Campaign: "00000000-0000-4000-8000-000000000001", Revision: 1, RevisionKnown: true, PayloadDigest: strings.Repeat("a", 64), Snapshot: store.saved}), nil
	}
	return session.NewProgressResult(ctx, session.ProgressResultData{ID: id, Status: session.StorageFailed, Code: session.StorageFull}), nil
}

func TestRealSceneAdvanceNamesDiscardedUnit(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	owner, err := campaign.New(ctx, ui.campaign)
	if err != nil {
		t.Fatal(err)
	}
	bays, err := content.LoadServiceBays(ctx, os.DirFS("../../../.."), "content/data/encounters/templates.json", "content/data/encounters/variants.json", ui.campaign)
	if err != nil {
		t.Fatal(err)
	}
	defs := content.Definitions{SchemaVersion: 1, ContentVersion: "test", Locations: []content.Location{{ID: "LOC-HOS", Name: "TXT-TEST"}}, Encounters: []content.Encounter{{ID: "ENC-HOS-01", Location: "LOC-HOS", Bounds: content.Rect{Width: 300, Height: 100}}}, Anchors: []content.Anchor{{ID: "ANC-HOS-01", Encounter: "ENC-HOS-01", X: 20}}, Presentations: []content.Presentation{{ID: "TXT-TEST"}}}
	for _, id := range []content.NarrativeID{"NAR-01", "NAR-02", "NAR-03", "NAR-04", "NAR-05", "NAR-06", "NAR-07", "NAR-08", "NAR-09", "NAR-10", "NAR-11", "NAR-12"} {
		defs.Narratives = append(defs.Narratives, content.Narrative{ID: id, Location: "LOC-HOS"})
	}
	for _, id := range []content.NarrativeID{"NAR-01", "NAR-02", "NAR-03", "NAR-04", "NAR-05", "NAR-06", "NAR-07", "NAR-08", "NAR-09"} {
		defs.Scenes = append(defs.Scenes, content.Scene{ID: "SCN-" + string(id), Narrative: id, Units: []content.SceneUnit{{ID: "UNIT-" + string(id) + "-1", Text: "Первый эпизод", TextID: "TXT-TEST"}, {ID: "UNIT-" + string(id) + "-2", Text: "Гриша теряет сознание", TextID: "TXT-TEST"}}})
	}
	for _, entry := range []struct {
		stage     content.EndingID
		narrative content.NarrativeID
	}{{"END-DEATH", "NAR-10"}, {"END-HELL", "NAR-11"}, {"END-CEREMONY", "NAR-12"}, {"END-TABLEAU", "NAR-12"}} {
		defs.Endings = append(defs.Endings, content.Ending{ID: entry.stage, Anchor: "ANC-HOS-01", Narrative: entry.narrative})
		defs.Scenes = append(defs.Scenes, content.Scene{ID: "SCN-" + string(entry.stage), Ending: entry.stage, Units: []content.SceneUnit{{ID: "UNIT-" + string(entry.stage) + "-1", Text: "Финал", TextID: "TXT-TEST"}}})
	}
	catalogue, err := content.Validate(ctx, defs, content.Incremental)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := owner.Prepare(ctx, campaign.Snapshot{}, campaign.Transition{CampaignID: "00000000-0000-4000-8000-000000000001", Sequence: 1, Story: &campaign.StoryChange{Action: campaign.StorySceneEnter, Narrative: "NAR-01", Unit: "UNIT-NAR-01-1"}})
	if err != nil {
		t.Fatal(err)
	}
	store := &sceneFailureStore{saved: saved}
	service, err := session.NewWithScenes(ctx, owner, ui.campaign, bays, store, catalogue)
	if err != nil {
		t.Fatal(err)
	}
	step := func(input session.FrameInput) session.FrameView {
		t.Helper()
		command, commandErr := session.NewFrameCommand(ctx, input)
		if commandErr != nil {
			t.Fatal(commandErr)
		}
		view, frameErr := service.HandleFrame(ctx, command)
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		return view
	}
	step(session.FrameInput{Selections: []session.MenuSelection{{Action: "continue"}}})
	step(session.FrameInput{})
	step(session.FrameInput{Edges: []session.ActionEdge{{Action: settings.MenuConfirm, Kind: session.Pressed}}})
	step(session.FrameInput{Edges: []session.ActionEdge{{Action: settings.MenuConfirm, Kind: session.Released}}})
	failure := step(session.FrameInput{})
	if failure.Data(ctx).State != session.StateError {
		t.Fatalf("write failure: %+v", failure.Data(ctx))
	}
	confirmation := step(session.FrameInput{Selections: []session.MenuSelection{{Action: "reload_saved"}}})
	screen, err := ui.Plan(ctx, confirmation)
	text := strings.Join(screen.Lines, " ")
	if err != nil || !strings.Contains(text, "Гриша теряет сознание") || !strings.Contains(text, "сердечный приступ") || strings.Contains(text, "SCN-") || strings.Contains(text, "UNIT-") {
		t.Fatalf("discard confirmation: %q %v", text, err)
	}
}

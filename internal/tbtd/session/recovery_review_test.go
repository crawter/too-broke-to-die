// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
)

func TestTitleInspectsSavedMetadataBeforeContinue(t *testing.T) {
	ctx := context.Background()
	for _, ending := range []bool{false, true} {
		service, store := sessionFixture(t)
		var snapshot campaign.Snapshot
		if ending {
			snapshot = authoredProgress(t, service, 4, true)
			var err error
			snapshot, err = service.campaign.IssuePermit(ctx, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err = service.campaign.Prepare(ctx, snapshot, campaign.Transition{CampaignID: string(testID), Sequence: 1, Story: &campaign.StoryChange{Action: campaign.StoryConsent}})
			if err != nil {
				t.Fatal(err)
			}
		} else {
			snapshot, _ = preparedCertification(t)
		}
		initial := frame(t, service, FrameInput{})
		if initial.State != StateTitle || !initial.TitleLoading || store.last().Kind != InspectActive || initial.Progress.Campaign != "" {
			t.Fatalf("lookup: %+v", initial)
		}
		request := store.last()
		store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Status: StorageSucceeded, Revision: 7, RevisionKnown: true, PayloadDigest: oldDigest, Snapshot: snapshot})
		view := frame(t, service, FrameInput{})
		if view.State != StateTitle || view.TitleLoading || view.TitleUnavailable || view.Progress.Campaign != testID || view.Progress.Anchor == "" || view.Progress.Credits != snapshot.Credits() || (ending && view.Progress.Ending != campaign.EndingDeath) {
			t.Fatalf("inspected title: %+v", view)
		}
		if service.world != nil || len(store.requests) != 1 {
			t.Fatal("title inspection restored a world or wrote progress")
		}
	}
	service, store := sessionFixture(t)
	frame(t, service, FrameInput{})
	request := store.last()
	store.reply(ProgressResultData{ID: request.ID, Status: StorageCorrupt})
	view := frame(t, service, FrameInput{})
	if !view.TitleUnavailable || view.TitleLoading || view.Progress.Campaign != "" || view.Progress.Credits != 0 {
		t.Fatalf("unavailable title: %+v", view)
	}
	view = choose(t, service, "continue", "")
	if view.State != StateRestoring || store.last().Kind != LoadActive {
		t.Fatal("unavailable inspection prevented explicit recovery")
	}
}

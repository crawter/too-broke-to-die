// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
)

func TestPendingSceneUnitCarriesAuthoredText(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	withSceneCatalogue(t, service)
	loadFixture(t, service, store, campaign.Snapshot{})
	if err := service.startNarrative(ctx, "NAR-01"); err != nil {
		t.Fatal(err)
	}
	acknowledgeStory(t, service, store)
	view := tapScene(t, service)
	if view.Pending.SceneName != "NAR-01" || view.Pending.UnitText != "Второй" {
		t.Fatalf("pending scene metadata: %+v", view.Pending)
	}
}

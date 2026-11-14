// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"strings"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/session"
)

func TestTitleShowsInspectedSummaryAndLoading(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	ui.adultSeen = true
	loading, err := ui.Plan(ctx, frameView(t, session.FrameViewData{State: session.StateTitle, TitleLoading: true}))
	if err != nil || loading.Items[0].DisabledReason == "" {
		t.Fatalf("loading: %+v %v", loading, err)
	}
	owner, err := campaign.New(ctx, ui.campaign)
	if err != nil {
		t.Fatal(err)
	}
	progress, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	progress, err = owner.Checkpoint(ctx, progress, "ANC-HOS-01")
	if err != nil {
		t.Fatal(err)
	}
	view, err := session.NewFrameView(ctx, session.FrameViewData{State: session.StateTitle}, progress, "00000000-0000-4000-8000-000000000001", 1)
	if err != nil {
		t.Fatal(err)
	}
	screen, err := ui.Plan(ctx, view)
	text := strings.Join(screen.Lines, " ")
	if err != nil || !strings.Contains(text, "Больница") || !strings.Contains(text, "Зачтено") {
		t.Fatalf("title summary: %q %v", text, err)
	}
}

func TestDiscardSceneUsesAuthoredUnitText(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	data := session.FrameViewData{State: session.StateError, Confirmation: "reload_saved", Pending: session.PendingEvent{Label: "SCN-NAR-01/UNIT-NAR-01-2", SceneName: "NAR-01", UnitText: "Второй эпизод"}}
	screen, err := ui.Plan(ctx, frameView(t, data))
	text := strings.Join(screen.Lines, " ")
	if err != nil || !strings.Contains(text, "сердечный приступ") || !strings.Contains(text, "Второй эпизод") || strings.Contains(text, "SCN-") || strings.Contains(text, "UNIT-") {
		t.Fatalf("discard text %q: %v", text, err)
	}
}

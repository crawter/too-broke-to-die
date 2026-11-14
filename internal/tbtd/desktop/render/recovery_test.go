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
)

type corruptStore struct {
	request session.ProgressRequest
	count   int
}

func (store *corruptStore) Submit(_ context.Context, request session.ProgressRequest) error {
	store.request = request
	store.count++
	return nil
}

func (store *corruptStore) Poll(ctx context.Context, id session.RequestID) (session.ProgressResult, error) {
	return session.NewProgressResult(ctx, session.ProgressResultData{ID: id, Status: session.StorageCorrupt}), nil
}

func TestRecoveryMetadataAndSettingsReset(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	data := session.FrameViewData{State: session.StateError, Error: session.SaveCorrupt, BothRevisionsInvalid: true, Choices: []session.RecoveryChoice{session.NewCampaign, session.ExitGame}}
	screen, err := ui.Plan(ctx, frameView(t, data))
	if err != nil || !strings.Contains(strings.Join(screen.Lines, " "), "Обе версии кампании повреждены") {
		t.Fatalf("both invalid: %+v %v", screen, err)
	}
	result := session.NewSettingsResult(ctx, session.SettingsResultData{ID: "settings-load", Status: session.StorageSucceeded, DefaultsRestored: true})
	if err = ui.NotifySettingsResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	if err = ui.NotifySettingsResult(ctx, result); err != nil || len(ui.notices) != 1 || !strings.Contains(ui.notices[0].text, "Прогресс кампании не изменён") {
		t.Fatalf("settings reset: %+v %v", ui.notices, err)
	}
	data = session.FrameViewData{State: session.StatePause, RestorationReady: true}
	screen, err = ui.Plan(ctx, frameView(t, data))
	if err != nil || !strings.Contains(strings.Join(screen.Lines, " "), "Восстановление завершено") {
		t.Fatalf("restoration: %+v %v", screen, err)
	}
}

func TestDiscardConfirmationNamesEventFamilies(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	for _, test := range []struct {
		label string
		want  string
	}{
		{"ENC-HOS-05", "Потрошин"},
		{"NAR-04 entry", "больницу"},
		{"EM-HOS-SUP-aftermath", "трубу"},
		{"SCN-NAR-01/UNIT-NAR-01-2", "Событие прогресса"},
	} {
		data := session.FrameViewData{State: session.StateError, Confirmation: "reload_saved", Pending: session.PendingEvent{Label: test.label}}
		screen, err := ui.Plan(ctx, frameView(t, data))
		if err != nil || !strings.Contains(strings.Join(screen.Lines, " "), test.want) {
			t.Fatalf("%s: %+v %v", test.label, screen, err)
		}
	}
	if got := ui.eventName(ctx, "SCN-SECRET/../../private"); strings.Contains(got, "private") {
		t.Fatalf("unsafe scene identity: %s", got)
	}
}

func TestNewCampaignRecoverySelectionReachesSessionConfirmation(t *testing.T) {
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
	store := &corruptStore{}
	service, err := session.New(ctx, owner, ui.campaign, bays, store)
	if err != nil {
		t.Fatal(err)
	}
	step := func(selections ...session.MenuSelection) session.FrameView {
		t.Helper()
		command, commandErr := session.NewFrameCommand(ctx, session.FrameInput{Selections: selections})
		if commandErr != nil {
			t.Fatal(commandErr)
		}
		view, frameErr := service.HandleFrame(ctx, command)
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		return view
	}
	step(session.MenuSelection{Action: "continue"})
	view := step()
	if view.Data(ctx).State != session.StateError {
		t.Fatal("corrupt load did not enter recovery")
	}
	screen, err := ui.Plan(ctx, view)
	if err != nil {
		t.Fatal(err)
	}
	for index, item := range screen.Items {
		if item.Action != "new_game" {
			continue
		}
		ui.SetFocus(screen, index)
		selection, ok := ui.Activate(screen)
		if !ok {
			t.Fatal("new campaign was disabled")
		}
		view = step(selection)
		if view.Data(ctx).Confirmation != "new_game" || store.count != 1 {
			t.Fatal("new campaign did not wait for confirmation")
		}
		step(session.MenuSelection{Action: "cancel"})
		if store.count != 1 {
			t.Fatal("cancel created a campaign")
		}
		step(selection)
		step(session.MenuSelection{Action: "confirm"})
		if store.count != 2 || store.request.Data().Kind != session.CreateCampaign {
			t.Fatal("confirmation did not request separate campaign creation")
		}
		return
	}
	t.Fatal("new campaign choice missing")
}

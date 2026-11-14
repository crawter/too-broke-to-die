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
	"too-broke-to-die/internal/tbtd/desktop/input"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

type ledgerStore struct {
	snapshot campaign.Snapshot
}

func (store *ledgerStore) Submit(context.Context, session.ProgressRequest) error { return nil }

func (store *ledgerStore) Poll(ctx context.Context, id session.RequestID) (session.ProgressResult, error) {
	return session.NewProgressResult(ctx, session.ProgressResultData{ID: id, Campaign: "00000000-0000-4000-8000-000000000001", Revision: 7, RevisionKnown: true, PayloadDigest: strings.Repeat("a", 64), Status: session.StorageSucceeded, Snapshot: store.snapshot}), nil
}

func TestLedgerStatusesCountsAndEmergencyWarning(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	data := session.FrameViewData{
		Tickets:   []session.TicketView{{Deed: "D-HOS-SUP-01", Status: "attempt_failed", FailureID: "TXT-FAIL-SEAL"}, {Deed: "D-HOS-SUP-02", Status: "blocked", Missing: []string{"D-HOS-SUP-01"}}},
		Progress:  session.ProgressView{Receipts: []session.ReceiptView{{Deed: "D-HOS-SUP-03"}}, Emergencies: []campaign.Emergency{{ID: "EM-HOS-SUP", Stage: campaign.EmergencyActive}}},
		Emergency: "EM-HOS-SUP", EmergencyWarning: true,
	}
	var screen Screen
	if err := ui.ledger(ctx, &screen, data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(screen.Lines, " "), "Доставка 1/6") || !strings.Contains(screen.Items[2].Label, "пломба повреждена") || !strings.Contains(screen.Items[3].Label, "Недоступно") {
		t.Fatalf("ticket status: %+v", screen)
	}
	ui.ledgerTab = 1
	screen = Screen{}
	if err := ui.ledger(ctx, &screen, data); err != nil {
		t.Fatal(err)
	}
	if len(screen.Items) != 3 || !strings.Contains(screen.Items[2].Label, "Предупреждение") {
		t.Fatalf("warning: %+v", screen)
	}
	data.EmergencyWarning = false
	screen = Screen{}
	if err := ui.ledger(ctx, &screen, data); err != nil || !strings.Contains(screen.Items[2].Label, "Авария") {
		t.Fatalf("emergency: %+v %v", screen, err)
	}
	data.Progress.Emergencies[0].Stage = campaign.EmergencyAftermath
	screen = Screen{}
	if err := ui.ledger(ctx, &screen, data); err != nil || !strings.Contains(screen.Items[2].Label, "Последствия зафиксированы") {
		t.Fatalf("aftermath: %+v %v", screen, err)
	}
}

func TestLiveSessionPublishesLedgerIntoInterface(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	owner, err := campaign.New(ctx, ui.campaign)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = owner.Accept(ctx, snapshot, "D-HOS-SUP-01")
	if err != nil {
		t.Fatal(err)
	}
	bays, err := content.LoadServiceBays(ctx, os.DirFS("../../../.."), "content/data/encounters/templates.json", "content/data/encounters/variants.json", ui.campaign)
	if err != nil {
		t.Fatal(err)
	}
	service, err := session.New(ctx, owner, ui.campaign, bays, &ledgerStore{snapshot: snapshot})
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
	step(session.FrameInput{Selections: []session.MenuSelection{{Action: "resume"}}})
	view := step(session.FrameInput{Edges: []session.ActionEdge{{Action: settings.Ledger, Kind: session.Pressed}}})
	data := view.Data(ctx)
	if data.Tracked != "D-HOS-SUP-01" || data.ObjectiveID != "TXT-ACT-SUP-PAD" || len(data.Tickets) != 100 {
		t.Fatalf("live view: %+v", data)
	}
	screen, err := ui.Plan(ctx, view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(screen.Lines, " "), "Доставка 0/6") || !strings.Contains(strings.Join(screen.Lines, " "), "тележку") {
		t.Fatalf("live ledger: %+v", screen)
	}
	if !strings.Contains(screen.Items[2].Label, "Выполняется") {
		t.Fatalf("active ticket: %+v", screen.Items[2])
	}
}

func TestLiveEmergencyWarningReachesInterface(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	owner, err := campaign.New(ctx, ui.campaign)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ui.campaign.Batches(ctx)[0].Deeds {
		snapshot, err = owner.Accept(ctx, snapshot, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, deed := range ui.campaign.Deeds(ctx) {
			if deed.Deed.ID != id {
				continue
			}
			attempt, ok := snapshot.ActiveAttempt()
			if !ok {
				t.Fatal("missing active attempt")
			}
			event := campaign.Certification{Deed: id, Attempt: attempt.Number, Evidence: deed.Evidence, Trigger: deed.Trigger, HoldMilliseconds: deed.Deed.HoldMilliseconds}
			for _, objectID := range deed.Deed.Objects {
				for _, object := range ui.campaign.Objects(ctx) {
					if object.ID != objectID {
						continue
					}
					if object.Deed == id {
						event.EvidenceObjects = append(event.EvidenceObjects, objectID)
					}
					if object.Role == content.RoleTerminal {
						event.Terminal = objectID
					}
				}
			}
			snapshot, _, err = owner.Apply(ctx, snapshot, event)
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	bays, err := content.LoadServiceBays(ctx, os.DirFS("../../../.."), "content/data/encounters/templates.json", "content/data/encounters/variants.json", ui.campaign)
	if err != nil {
		t.Fatal(err)
	}
	service, err := session.New(ctx, owner, ui.campaign, bays, &ledgerStore{snapshot: snapshot})
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
	step(session.FrameInput{Selections: []session.MenuSelection{{Action: "resume"}}})
	view := step(session.FrameInput{Edges: []session.ActionEdge{{Action: settings.Ledger, Kind: session.Pressed}}})
	ui.ledgerTab = 1
	screen, err := ui.Plan(ctx, view)
	if err != nil || !view.Data(ctx).EmergencyWarning || !strings.Contains(screen.Emergency, "паровую трубу") {
		t.Fatalf("live warning: %+v %v", screen, err)
	}
	found := false
	for _, item := range screen.Items {
		if strings.Contains(item.Label, "Предупреждение") {
			found = true
		}
	}
	if !found {
		t.Fatal("ledger omitted the active warning stage")
	}
}

func TestTutorialArchiveUsesRemappedActiveDevice(t *testing.T) {
	ctx := context.Background()
	ui, sampler := interfaceFixture(t)
	values := settings.Defaults(ctx)
	for i := range values.Bindings {
		if values.Bindings[i].Action == settings.Interact && values.Bindings[i].Device == settings.Keyboard {
			values.Bindings[i].Control = "J"
		}
	}
	if err := sampler.SetBindings(ctx, values); err != nil {
		t.Fatal(err)
	}
	ui.ledgerTab = 2
	view := frameView(t, session.FrameViewData{State: session.StateLedger})
	archive := func() map[string]string {
		t.Helper()
		screen, err := ui.Plan(ctx, view)
		if err != nil {
			t.Fatal(err)
		}
		result := make(map[string]string)
		for _, item := range screen.Items {
			if strings.HasPrefix(item.Target, "TUT-") {
				result[item.Target] = item.Label
			}
		}
		return result
	}
	keyboard := archive()
	if len(keyboard) != 14 || !strings.Contains(keyboard["TUT-INT"], "J") {
		t.Fatalf("keyboard archive: %+v", keyboard)
	}
	for _, text := range keyboard {
		if strings.ContainsAny(text, "{}") {
			t.Fatalf("unresolved tutorial: %s", text)
		}
	}
	snapshot := input.Snapshot{Focused: true, Connected: true, Buttons: map[input.Control]bool{{Device: settings.Controller, Name: "X"}: true}}
	if _, err := sampler.SampleSnapshot(ctx, input.Frame{}, snapshot); err != nil {
		t.Fatal(err)
	}
	if sampler.ActiveDevice() != settings.Controller {
		t.Fatal("controller did not become active")
	}
	controller := archive()
	if len(controller) != 14 || !strings.Contains(controller["TUT-INT"], "X") || strings.Contains(controller["TUT-INT"], "J") {
		t.Fatalf("controller archive: %+v", controller)
	}
}

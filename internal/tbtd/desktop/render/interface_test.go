// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/desktop/input"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

func interfaceFixture(t *testing.T) (*Interface, *input.Sampler) {
	t.Helper()
	ctx := context.Background()
	fsys := os.DirFS("../../../..")
	manifest, err := content.LoadCampaign(ctx, fsys, "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	texts, err := content.LoadRussian(ctx, fsys, manifest)
	if err != nil {
		t.Fatal(err)
	}
	values := settings.Defaults(ctx)
	sampler, err := input.New(ctx, values, func() uint64 { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	ui, err := NewInterface(ctx, texts, manifest, sampler, values)
	if err != nil {
		t.Fatal(err)
	}
	return ui, sampler
}

func frameView(t *testing.T, data session.FrameViewData) session.FrameView {
	t.Helper()
	view, err := session.NewFrameView(context.Background(), data, campaign.Snapshot{}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func TestTextReflowAndConfirmationFocus(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	for _, size := range []int{100, 125, 150} {
		values := settings.Defaults(ctx)
		values.Interface.TextSize = size
		if err := ui.SetSettings(ctx, values); err != nil {
			t.Fatal(err)
		}
		measure := func(value string) float64 { return float64(utf8.RuneCountInString(value) * size) }
		lines, err := wrapText("Незачтённый прогресс попытки будет сброшен", 900, measure)
		if err != nil || len(lines) < 2 {
			t.Fatalf("size %d: %v %v", size, lines, err)
		}
		for _, line := range lines {
			if measure(line) > 900 {
				t.Fatalf("overflow: %q", line)
			}
		}
		view := frameView(t, session.FrameViewData{State: session.StatePause, Confirmation: "return_checkpoint", Warning: "Незачтённый прогресс попытки будет сброшен. Зачтённые дела и штрафы сохраняются"})
		ui.key = ""
		screen, err := ui.Plan(ctx, view)
		if err != nil || screen.Focus != 0 || screen.Items[0].Action != "cancel" {
			t.Fatalf("confirmation: %+v %v", screen, err)
		}
		ui.MoveFocus(screen, 1)
		selection, ok := ui.Activate(screen)
		if !ok || selection.Action != "confirm" {
			t.Fatalf("focus: %+v %v", selection, ok)
		}
	}
	if _, err := wrapText("а", 0, func(string) float64 { return 1 }); err == nil {
		t.Fatal("accepted zero width")
	}
}

func TestBindingPromptAndUnknownAction(t *testing.T) {
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
	text, err := ui.Prompt(ctx, "TUT-INT", map[string]settings.Action{"interact": settings.Interact})
	if err != nil || !strings.Contains(text, "J") || strings.Contains(text, "{interact}") {
		t.Fatalf("prompt: %q %v", text, err)
	}
	if _, err = ui.Prompt(ctx, "TUT-INT", map[string]settings.Action{"interact": "unknown"}); err == nil {
		t.Fatal("accepted unbound action")
	}
}

func TestRecoveryChoicesAndPendingProgress(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	data := session.FrameViewData{State: session.StateError, Error: session.StorageFull, Pending: session.PendingEvent{Label: "D-HOS-SUP-01"}, Choices: []session.RecoveryChoice{session.RetrySave, session.ReloadSaved, session.ExitUnsaved}, Progress: session.ProgressView{Credits: 99}}
	view := frameView(t, data)
	if got := view.Data(ctx).Progress.Credits; got != 0 {
		t.Fatalf("proposal leaked: %d", got)
	}
	screen, err := ui.Plan(ctx, view)
	if err != nil || len(screen.Items) != 3 || screen.Items[0].Action != "retry_save" || screen.Items[1].Action != "reload_saved" || screen.Items[2].Action != "exit_unsaved" {
		t.Fatalf("recovery: %+v %v", screen, err)
	}
	if strings.Contains(strings.Join(screen.Lines, " "), "D-HOS-SUP-01") {
		t.Fatal("internal event identity exposed")
	}
	data = session.FrameViewData{State: session.StateError, Error: session.SaveCorrupt, Choices: []session.RecoveryChoice{session.ChooseOlder, session.NewCampaign, session.ExitGame}, Candidates: []session.RecoveryCandidate{{Campaign: "00000000-0000-4000-8000-000000000001", Revision: 3, Anchor: "ANC-HOS-01", Credits: 5, UnknownDifference: true}}}
	screen, err = ui.Plan(ctx, frameView(t, data))
	if err != nil || len(screen.Items) != 3 || !strings.Contains(screen.Items[0].Label, "разница неизвестна") || screen.Items[0].Target != "00000000-0000-4000-8000-000000000001:3" {
		t.Fatalf("older: %+v %v", screen, err)
	}
	data.Candidates = nil
	data.Choices = []session.RecoveryChoice{session.NewCampaign, session.ExitGame}
	screen, err = ui.Plan(ctx, frameView(t, data))
	if err != nil || !strings.Contains(strings.Join(screen.Lines, " "), "Не удалось прочитать сохранение") {
		t.Fatalf("invalid revisions: %+v %v", screen, err)
	}
}

func TestAcknowledgedDeathAndNoFalseFee(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	data := session.FrameViewData{State: session.StateDead, Progress: session.ProgressView{Anchor: "ANC-HOS-01", Credits: 4, Incidents: []session.IncidentView{{Kind: campaign.IncidentDeath, Cents: 4999}}}}
	var screen Screen
	if err := ui.recovery(ctx, &screen, data); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(screen.Lines, " ")
	if !strings.Contains(text, "ТЫ СДОХ, УЁБОК. НАЧИСЛЕН ШТРАФ $49,99") || !strings.Contains(text, "4/100") || screen.Items[0].Action != "retry_death" {
		t.Fatalf("death presentation: %+v", screen)
	}
	data.Progress.Incidents = nil
	if err := ui.recovery(ctx, &Screen{}, data); err == nil {
		t.Fatal("displayed a fee without a committed death")
	}
}

func TestCommittedNoticesDoNotReplayAfterRestoration(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	owner, err := campaign.New(ctx, ui.campaign)
	if err != nil {
		t.Fatal(err)
	}
	before, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	deed := ui.campaign.Deeds(ctx)[0]
	before, err = owner.Accept(ctx, before, deed.Deed.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok := before.ActiveAttempt()
	if !ok {
		t.Fatal("missing attempt")
	}
	event := campaign.Certification{Deed: deed.Deed.ID, Attempt: attempt.Number, Evidence: deed.Evidence, Trigger: deed.Trigger, HoldMilliseconds: deed.Deed.HoldMilliseconds}
	objects := ui.campaign.Objects(ctx)
	for _, id := range deed.Deed.Objects {
		for _, object := range objects {
			if object.ID != id {
				continue
			}
			if object.Deed == deed.Deed.ID {
				event.EvidenceObjects = append(event.EvidenceObjects, id)
			}
			if object.Role == content.RoleTerminal {
				event.Terminal = id
			}
		}
	}
	campaignID := session.CampaignID("00000000-0000-4000-8000-000000000001")
	proposal, err := owner.Prepare(ctx, before, campaign.Transition{CampaignID: string(campaignID), Sequence: 1, Certification: &campaign.CertificationCandidate{Completed: true, Event: event}})
	if err != nil {
		t.Fatal(err)
	}
	makeView := func(data session.FrameViewData, snapshot campaign.Snapshot, revision uint64) session.FrameView {
		t.Helper()
		view, viewErr := session.NewFrameView(ctx, data, snapshot, campaignID, revision)
		if viewErr != nil {
			t.Fatal(viewErr)
		}
		return view
	}
	baseline := makeView(session.FrameViewData{State: session.StateActive}, before, 1)
	pending := makeView(session.FrameViewData{State: session.StateSaving, Save: session.SavePending, Progress: session.ProgressView{Credits: 1}}, before, 1)
	if err = ui.Advance(ctx, baseline, 0); err != nil {
		t.Fatal(err)
	}
	if err = ui.Advance(ctx, pending, time.Second); err != nil || len(ui.notices) != 0 {
		t.Fatalf("pending: %+v %v", ui.notices, err)
	}
	ack := session.SemanticEvent{Kind: "saved", Durable: true, Revision: 2, Operation: session.OperationID{Event: string(deed.Deed.ID), Sequence: 1}}
	committed := makeView(session.FrameViewData{State: session.StateActive, Events: []session.SemanticEvent{ack}}, proposal, 2)
	if err = ui.Advance(ctx, committed, 0); err != nil || len(ui.notices) != 2 {
		t.Fatalf("acknowledged: %+v %v", ui.notices, err)
	}
	if err = ui.Advance(ctx, committed, 0); err != nil || len(ui.notices) != 2 {
		t.Fatalf("duplicate event: %+v %v", ui.notices, err)
	}
	restored, _ := interfaceFixture(t)
	if err = restored.Advance(ctx, makeView(session.FrameViewData{State: session.StatePause}, proposal, 2), 0); err != nil || len(restored.notices) != 0 {
		t.Fatalf("restoration replayed notice: %+v %v", restored.notices, err)
	}
}

func TestRecoveryDoesNotRequireWorldAssets(t *testing.T) {
	if !interfaceOnly(session.FrameViewData{State: session.StateError, World: session.WorldView{Background: "ART-PRO-01"}}) {
		t.Fatal("recovery depends on failed world assets")
	}
	if interfaceOnly(session.FrameViewData{State: session.StateActive, World: session.WorldView{Background: "ART-PRO-01"}}) {
		t.Fatal("active play bypasses the world")
	}
}

func TestRecoveryNoticeIsTranslatedWithoutInternalLabels(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	view := frameView(t, session.FrameViewData{State: session.StatePause, Notice: "Discarded: D-HOS-SUP-01"})
	if err := ui.Advance(ctx, view, 0); err != nil {
		t.Fatal(err)
	}
	screen, err := ui.Plan(ctx, view)
	if err != nil || !strings.Contains(screen.Notice, "Отброшено") || strings.Contains(screen.Notice, "D-HOS-SUP-01") {
		t.Fatalf("recovery notice: %+v %v", screen, err)
	}
	if err = ui.Advance(ctx, view, 0); err != nil || len(ui.notices) != 1 {
		t.Fatalf("repeated frame duplicated notice: %+v %v", ui.notices, err)
	}
}

func TestNoticeQueuePausesAndKeepsConsequencesSeparate(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	deed := ui.campaign.Deeds(ctx)[0]
	after := session.ProgressView{Credits: 1, Receipts: []session.ReceiptView{{Deed: deed.Deed.ID, Title: deed.Title}}, Consequences: []content.ConsequenceID{deed.Deed.Consequence}}
	if err := ui.enqueueProgress(ctx, session.ProgressView{}, after); err != nil {
		t.Fatal(err)
	}
	if len(ui.notices) != 2 || !strings.Contains(ui.notices[0].text, "ЗАЧТЕНО: +1") || !strings.Contains(ui.notices[1].text, "ПОСЛЕДСТВИЯ") {
		t.Fatalf("separate notices: %+v", ui.notices)
	}
	// Set a baseline so the first frame is not interpreted as a restoration.
	ui.initialized = true
	paused := frameView(t, session.FrameViewData{State: session.StateLedger})
	if err := ui.Advance(ctx, paused, time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(ui.notices) != 2 || ui.notices[0].remaining != 6*time.Second {
		t.Fatalf("ledger expired notices: %+v", ui.notices)
	}
	active := frameView(t, session.FrameViewData{State: session.StateActive})
	if err := ui.Advance(ctx, active, 6*time.Second); err != nil {
		t.Fatal(err)
	}
	if len(ui.notices) != 1 || !strings.Contains(ui.notices[0].text, "ПОСЛЕДСТВИЯ") {
		t.Fatalf("queue order: %+v", ui.notices)
	}
	if err := ui.Advance(ctx, active, 6*time.Second); err != nil || len(ui.notices) != 0 {
		t.Fatalf("expiry: %+v %v", ui.notices, err)
	}
}

func TestTrackedObjectiveAndEmergency(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	ui.tracked = "D-HOS-RES-01"
	data := session.FrameViewData{State: session.StateLedger, Emergency: "EM-HOS-SUP"}
	screen, err := ui.Plan(ctx, frameView(t, data))
	if err != nil || !strings.Contains(screen.Emergency, "паровую трубу") || !strings.Contains(strings.Join(screen.Lines, " "), "ворота") {
		t.Fatalf("tracked objective or warning: %+v %v", screen, err)
	}
	if len(screen.Items) != 27 {
		t.Fatalf("expected filters and 25 hospital deeds, got %d", len(screen.Items))
	}
	data.Emergency = "x"
	if _, err = ui.Plan(ctx, frameView(t, data)); err == nil {
		t.Fatal("accepted malformed emergency")
	}
}

func TestSaturatedCaptionsWaitForVisibleTime(t *testing.T) {
	ctx := context.Background()
	measure := func(text string, size float64) float64 { return float64(utf8.RuneCountInString(text)) * size * .5 }
	for _, size := range []int{100, 125, 150} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			ui, _ := interfaceFixture(t)
			values := settings.Defaults(ctx)
			values.Interface.TextSize = size
			if err := ui.SetSettings(ctx, values); err != nil {
				t.Fatal(err)
			}
			cues := []session.TextView{
				{ID: "CAP-WARN-ENEMY", Direction: "left"},
				{ID: "CAP-WARN-ENEMY", Direction: "right"},
				{ID: "CAP-WARN-RANGED", Direction: "left"},
				{ID: "CAP-WARN-RANGED", Direction: "right"},
				{ID: "CAP-WARN-HAZARD", Direction: "above"},
				{ID: "CAP-WARN-CRUSHER", Direction: "near"},
				{ID: "CAP-WARN-EMERGENCY", Direction: "right"},
				{ID: "CAP-SOURCE-QUEUE", Direction: "left"},
				{ID: "CAP-HURT"},
			}
			active := frameView(t, session.FrameViewData{State: session.StateActive, World: session.WorldView{Generation: 1}, Texts: cues})
			empty := frameView(t, session.FrameViewData{State: session.StateActive, World: session.WorldView{Generation: 2}})
			paused := frameView(t, session.FrameViewData{State: session.StatePause, World: session.WorldView{Generation: 2}})
			if err := ui.Advance(ctx, active, 0); err != nil {
				t.Fatal(err)
			}
			layout := func(view session.FrameView) {
				t.Helper()
				screen, err := ui.Plan(ctx, view)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = ui.ActiveLayout(ctx, screen, nil, nil, measure); err != nil {
					t.Fatal(err)
				}
			}
			layout(active)
			visible := 0
			for _, cue := range ui.captions {
				if cue.visible {
					visible++
				}
			}
			if visible == 0 || visible >= len(cues)-1 {
				t.Fatalf("expected warning saturation, got %d", visible)
			}
			if err := ui.Advance(ctx, paused, time.Minute); err != nil {
				t.Fatal(err)
			}
			if len(ui.captions) != len(cues) {
				t.Fatal("pause discarded cues")
			}
			for _, cue := range ui.captions {
				if cue.remaining != 6*time.Second {
					t.Fatal("pause consumed caption time")
				}
			}
			// Session feedback and warnings may already have expired. The UI
			// retains every cue, labels past warnings, and starts no hidden clock.
			if err := ui.Advance(ctx, empty, time.Minute); err != nil {
				t.Fatal(err)
			}
			seen := make(map[session.TextView]bool)
			for round := 0; len(ui.captions) > 0 && round < 30; round++ {
				screen, err := ui.Plan(ctx, empty)
				if err != nil || (screen.CaptionPriority > 0 && !strings.HasPrefix(screen.Captions[0], "Ранее: ")) {
					t.Fatalf("expired warning not identified: %+v %v", screen, err)
				}
				layout(empty)
				for _, cue := range ui.captions {
					if cue.visible {
						seen[cue.key] = true
					} else if cue.remaining != 6*time.Second {
						t.Fatalf("hidden cue lost time: %+v", cue)
					}
				}
				before := len(ui.captions)
				if err = ui.Advance(ctx, empty, 5*time.Second); err != nil {
					t.Fatal(err)
				}
				if len(ui.captions) != before {
					t.Fatal("cue expired before six seconds of presentation")
				}
				layout(empty)
				if err = ui.Advance(ctx, empty, time.Second); err != nil {
					t.Fatal(err)
				}
			}
			if len(ui.captions) != 0 || len(seen) != len(cues) {
				t.Fatalf("lost or stuck cues: seen=%d waiting=%+v", len(seen), ui.captions)
			}
		})
	}
}

func TestStandaloneLayoutRotatesOverflowingWarnings(t *testing.T) {
	ctx := context.Background()
	measure := func(text string, size float64) float64 { return float64(utf8.RuneCountInString(text)) * size * .5 }
	for _, size := range []int{100, 125, 150} {
		ui, _ := interfaceFixture(t)
		values := settings.Defaults(ctx)
		values.Interface.TextSize = size
		if err := ui.SetSettings(ctx, values); err != nil {
			t.Fatal(err)
		}
		screen := Screen{Captions: []string{"Один", "Два", "Три", "Четыре", "Пять", "Шесть", "Семь", "Восемь"}, CaptionPriority: 8}
		seen := make(map[string]bool)
		for phase := range screen.Captions {
			ui.activePage = phase
			rows, err := ui.ActiveLayout(ctx, screen, nil, nil, measure)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				seen[row.Text] = true
			}
		}
		if len(seen) != len(screen.Captions) {
			t.Fatalf("size %d: missing warnings: %v", size, seen)
		}
	}
}

func TestPersistentWarningsRotateWithoutExpiringFeedback(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	values := settings.Defaults(ctx)
	values.Interface.TextSize = 150
	if err := ui.SetSettings(ctx, values); err != nil {
		t.Fatal(err)
	}
	measure := func(text string, size float64) float64 { return float64(utf8.RuneCountInString(text)) * size * .5 }
	cues := []session.TextView{
		{ID: "CAP-WARN-ENEMY", Direction: "left"}, {ID: "CAP-WARN-ENEMY", Direction: "right"},
		{ID: "CAP-WARN-RANGED", Direction: "left"}, {ID: "CAP-WARN-RANGED", Direction: "right"},
		{ID: "CAP-WARN-HAZARD", Direction: "near"}, {ID: "CAP-WARN-CRUSHER", Direction: "near"},
		{ID: "CAP-HURT"},
	}
	active := frameView(t, session.FrameViewData{State: session.StateActive, World: session.WorldView{Generation: 1}, Texts: cues})
	if err := ui.Advance(ctx, active, 0); err != nil {
		t.Fatal(err)
	}
	seen := make(map[session.TextView]bool)
	for round := 0; round < 8; round++ {
		screen, err := ui.Plan(ctx, active)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ui.ActiveLayout(ctx, screen, nil, nil, measure); err != nil {
			t.Fatal(err)
		}
		for _, caption := range ui.captions {
			if caption.visible {
				seen[caption.key] = true
			}
			if caption.key.ID == "CAP-HURT" && caption.remaining != 6*time.Second {
				t.Fatal("hidden feedback expired under live warnings")
			}
		}
		if err = ui.Advance(ctx, active, 6*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	for _, cue := range cues[:len(cues)-1] {
		if !seen[cue] {
			t.Fatalf("persistent warning never reached the band: %+v", cue)
		}
	}
	empty := frameView(t, session.FrameViewData{State: session.StateActive, World: session.WorldView{Generation: 1}})
	if err := ui.Advance(ctx, empty, 0); err != nil {
		t.Fatal(err)
	}
	if len(ui.captions) != 1 || ui.captions[0].key.ID != "CAP-HURT" {
		t.Fatalf("delivered warnings replayed or feedback lost: %+v", ui.captions)
	}
}

func TestNewLiveWarningPreemptsHistoricalBacklog(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	values := settings.Defaults(ctx)
	values.Interface.TextSize = 150
	if err := ui.SetSettings(ctx, values); err != nil {
		t.Fatal(err)
	}
	measure := func(text string, size float64) float64 { return float64(utf8.RuneCountInString(text)) * size * .5 }
	old := []session.TextView{{ID: "CAP-WARN-ENEMY", Direction: "left"}, {ID: "CAP-WARN-RANGED", Direction: "right"}, {ID: "CAP-WARN-HAZARD", Direction: "near"}, {ID: "CAP-HURT"}}
	view := func(cues []session.TextView) session.FrameView {
		return frameView(t, session.FrameViewData{State: session.StateActive, World: session.WorldView{Generation: 1}, Texts: cues})
	}
	if err := ui.Advance(ctx, view(old), 0); err != nil {
		t.Fatal(err)
	}
	if err := ui.Advance(ctx, view(nil), time.Second); err != nil {
		t.Fatal(err)
	}
	fresh := session.TextView{ID: "CAP-WARN-CRUSHER", Direction: "above"}
	if err := ui.Advance(ctx, view([]session.TextView{fresh}), 0); err != nil {
		t.Fatal(err)
	}
	screen, err := ui.Plan(ctx, view([]session.TextView{fresh}))
	if err != nil || len(screen.captionKeys) == 0 || screen.captionKeys[0] != fresh || strings.HasPrefix(screen.Captions[0], "Ранее: ") {
		t.Fatalf("live warning was not first: %+v %v", screen, err)
	}
	if _, err = ui.ActiveLayout(ctx, screen, nil, nil, measure); err != nil {
		t.Fatal(err)
	}
	for _, caption := range ui.captions {
		if caption.key == fresh && !caption.visible {
			t.Fatal("historical warning displaced live danger")
		}
	}
}

func TestLongNoticePagesGetIndependentPresentationTime(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	values := settings.Defaults(ctx)
	values.Interface.TextSize = 150
	if err := ui.SetSettings(ctx, values); err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("Подробные последствия сохранённого события. ", 80)
	ui.notices = []notice{{text: text, remaining: 6 * time.Second}}
	active := frameView(t, session.FrameViewData{State: session.StateActive})
	paused := frameView(t, session.FrameViewData{State: session.StateLedger})
	measure := func(text string, size float64) float64 { return float64(utf8.RuneCountInString(text)) * size * .5 }
	want, err := wrapText(text, 300, func(text string) float64 { return measure(text, 36) })
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for len(ui.notices) > 0 {
		screen, planErr := ui.Plan(ctx, active)
		if planErr != nil {
			t.Fatal(planErr)
		}
		rows, layoutErr := ui.ActiveLayout(ctx, screen, nil, []string{"Зачтено", "Долг"}, measure)
		if layoutErr != nil {
			t.Fatal(layoutErr)
		}
		if ui.notices[0].pages <= 1 {
			t.Fatal("long notice did not paginate")
		}
		for _, row := range rows {
			if row.X == 948 && row.Text != "Зачтено" && row.Text != "Долг" {
				got = append(got, row.Text)
			}
		}
		page := ui.notices[0].page
		if err = ui.Advance(ctx, paused, time.Minute); err != nil || ui.notices[0].page != page {
			t.Fatal("pause advanced notice page")
		}
		if err = ui.Advance(ctx, active, 6*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatal("notice pages lost or duplicated text")
	}
}

func TestCommittedNoticesReserveSpaceAtEveryPagePhase(t *testing.T) {
	ctx := context.Background()
	measure := func(text string, size float64) float64 { return float64(utf8.RuneCountInString(text)) * size * .5 }
	for _, size := range []int{100, 125, 150} {
		for phase := 0; phase < 4; phase++ {
			ui, _ := interfaceFixture(t)
			values := settings.Defaults(ctx)
			values.Interface.TextSize = size
			if err := ui.SetSettings(ctx, values); err != nil {
				t.Fatal(err)
			}
			ui.activePage = phase
			deed := ui.campaign.Deeds(ctx)[0]
			after := session.ProgressView{Credits: 1, Receipts: []session.ReceiptView{{Deed: deed.Deed.ID, Title: deed.Title}}, Consequences: []content.ConsequenceID{deed.Deed.Consequence}}
			if err := ui.enqueueProgress(ctx, session.ProgressView{}, after); err != nil {
				t.Fatal(err)
			}
			active := frameView(t, session.FrameViewData{State: session.StateActive})
			paused := frameView(t, session.FrameViewData{State: session.StateLedger})
			right := []string{"Зачтено", "Долг", strings.Repeat("Очень длинная задача для журнала. ", 80)}
			for _, heading := range []string{"ЗАЧТЕНО: +1", "ПОСЛЕДСТВИЯ"} {
				screen, err := ui.Plan(ctx, active)
				if err != nil || !strings.Contains(screen.Notice, heading) {
					t.Fatalf("size %d phase %d: %+v %v", size, phase, screen, err)
				}
				rows, err := ui.ActiveLayout(ctx, screen, nil, right, measure)
				if err != nil {
					t.Fatal(err)
				}
				want, err := wrapText(screen.Notice, 300, func(text string) float64 { return measure(text, 24*float64(size)/100) })
				if err != nil {
					t.Fatal(err)
				}
				for _, line := range want {
					found := false
					for _, row := range rows {
						found = found || (row.X == 948 && row.Text == line)
					}
					if !found {
						t.Fatalf("size %d phase %d hid notice line %q", size, phase, line)
					}
				}
				if err = ui.Advance(ctx, paused, time.Minute); err != nil || ui.notices[0].remaining != 6*time.Second {
					t.Fatalf("notice expired in ledger: %v", err)
				}
				if err = ui.Advance(ctx, active, 6*time.Second); err != nil {
					t.Fatal(err)
				}
			}
			if len(ui.notices) != 0 {
				t.Fatalf("notices remain: %+v", ui.notices)
			}
		}
	}
}

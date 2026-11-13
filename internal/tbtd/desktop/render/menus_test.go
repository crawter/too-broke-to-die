// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"too-broke-to-die/internal/tbtd/desktop/input"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

func TestSettingsDraftRequiresExplicitApply(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	ui.section, ui.settingsTab = "settings", 2
	view := frameView(t, session.FrameViewData{State: session.StatePause})
	screen, err := ui.Plan(ctx, view)
	if err != nil {
		t.Fatal(err)
	}
	for index, item := range screen.Items {
		if item.Target == "TXT-SET-TEXT-SIZE" {
			ui.SetFocus(screen, index)
			ui.Activate(screen)
			break
		}
	}
	if ui.draft.Interface.TextSize != 125 || ui.values.Interface.TextSize != 100 {
		t.Fatalf("draft leaked to live settings: %+v", ui.draft.Interface)
	}
	if _, ready := ui.TakeSettingsDraft(ctx); ready {
		t.Fatal("draft submitted without apply")
	}
	screen, err = ui.Plan(ctx, view)
	if err != nil {
		t.Fatal(err)
	}
	for index, item := range screen.Items {
		if item.Action == "ui_settings_apply" {
			ui.SetFocus(screen, index)
			ui.Activate(screen)
			break
		}
	}
	draft, ready := ui.TakeSettingsDraft(ctx)
	if !ready || draft.Interface.TextSize != 125 || draft.Validate(ctx) != nil {
		t.Fatalf("apply draft: %+v", draft)
	}
	if _, ready = ui.TakeSettingsDraft(ctx); ready {
		t.Fatal("apply emitted twice")
	}
	if err = ui.SetSettings(ctx, draft); err != nil || ui.values.Interface.TextSize != 125 {
		t.Fatalf("confirmed settings: %+v %v", ui.values.Interface, err)
	}
	ui.draft.Audio.Music = 95
	ui.changeSetting("TXT-SET-MUSIC")
	if ui.draft.Audio.Music != 0 || ui.draft.Validate(ctx) != nil {
		t.Fatal("volume step exceeded the allowed range")
	}
	invalid := draft.Clone(ctx)
	invalid.Interface.TextSize = 101
	if err = ui.SetSettings(ctx, invalid); err == nil || ui.values.Interface.TextSize != 125 {
		t.Fatal("invalid settings changed the confirmed view")
	}
}

func TestMenuNavigationAcrossDevices(t *testing.T) {
	ctx := context.Background()
	ui, sampler := interfaceFixture(t)
	view := frameView(t, session.FrameViewData{State: session.StateTitle})
	measure := func(text string, size float64) float64 { return float64(utf8.RuneCountInString(text)) * size * .5 }
	screen, err := ui.Plan(ctx, view)
	if err != nil || !strings.Contains(strings.Join(screen.Lines, " "), "18+") {
		t.Fatalf("adult notice: %+v %v", screen, err)
	}
	sample := func(snapshot input.Snapshot, pointer input.Point) session.FrameInput {
		t.Helper()
		command, sampleErr := sampler.SampleSnapshot(ctx, input.Frame{}, snapshot)
		if sampleErr != nil {
			t.Fatal(sampleErr)
		}
		command, sampleErr = ui.Navigate(ctx, view, command, pointer, measure)
		if sampleErr != nil {
			t.Fatal(sampleErr)
		}
		return command.Data(ctx)
	}
	keyboard := input.Snapshot{Focused: true, Buttons: map[input.Control]bool{{Device: settings.Keyboard, Name: "Enter"}: true}}
	if len(sample(keyboard, input.Point{}).Selections) != 0 {
		t.Fatal("confirmed before release")
	}
	keyboard.Buttons = nil
	if len(sample(keyboard, input.Point{}).Selections) != 0 || !ui.adultSeen {
		t.Fatal("notice continuation was not handled locally")
	}
	controller := input.Snapshot{Focused: true, Connected: true, Buttons: map[input.Control]bool{{Device: settings.Controller, Name: "DPadDown"}: true}}
	sample(controller, input.Point{})
	screen, err = ui.Plan(ctx, view)
	if err != nil || screen.Focus != 1 || screen.Items[1].Action != "new_game" {
		t.Fatalf("controller focus: %+v %v", screen, err)
	}
	controller.Buttons = nil
	sample(controller, input.Point{})
	controller.Buttons = map[input.Control]bool{{Device: settings.Controller, Name: "A"}: true}
	sample(controller, input.Point{})
	controller.Buttons = nil
	selected := sample(controller, input.Point{})
	if len(selected.Selections) != 1 || selected.Selections[0].Action != "new_game" {
		t.Fatalf("controller selection: %+v", selected.Selections)
	}
	ui.SetFocus(screen, 2)
	screen, err = ui.Plan(ctx, view)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ui.MenuLayout(ctx, screen, measure)
	if err != nil {
		t.Fatal(err)
	}
	var pointer input.Point
	for _, row := range rows {
		if row.ItemIndex == 2 {
			pointer = input.Point{X: row.X + 1, Y: row.Y + 1}
			break
		}
	}
	mouse := input.Snapshot{Focused: true, Connected: true, Pointer: pointer, Buttons: map[input.Control]bool{{Device: settings.Mouse, Name: "Left"}: true}}
	sample(mouse, pointer)
	mouse.Buttons = nil
	sample(mouse, pointer)
	if ui.section != "settings" {
		t.Fatal("mouse did not open settings")
	}
	screen, err = ui.Plan(ctx, view)
	if err != nil || len(screen.Items) < 7 {
		t.Fatalf("settings screen: %+v %v", screen, err)
	}
	ui.settingsTab = 4
	screen, err = ui.Plan(ctx, view)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range screen.Items {
		if strings.Contains(item.Label, "Взаимодействие") && strings.Contains(item.Label, "E") && item.DisabledReason != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("controls screen lacks binding or disabled reason")
	}
}

func TestMenuLayoutKeepsWarningAndFocusedChoiceInsideSafeArea(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	measure := func(text string, size float64) float64 { return float64(utf8.RuneCountInString(text)) * size * .55 }
	for _, size := range []int{100, 125, 150} {
		values := settings.Defaults(ctx)
		values.Interface.TextSize = size
		if err := ui.SetSettings(ctx, values); err != nil {
			t.Fatal(err)
		}
		screen := Screen{Emergency: "Осторожно: опасная зона отмечена", Lines: []string{strings.Repeat("Сохранённая информация о восстановлении. ", 30)}, Items: []Item{{Label: strings.Repeat("Загрузить предыдущее сохранение. ", 30), Action: "choose_older"}}, Focus: 0}
		for page := 0; page < 10; page++ {
			ui.detailPage = page
			rows, err := ui.MenuLayout(ctx, screen, measure)
			if err != nil {
				t.Fatal(err)
			}
			warning, focus := false, false
			for _, row := range rows {
				if row.X < 24 || row.X+row.Width > 1256 || row.Y < 24 || row.Y+24*float64(size)/100*1.2 > 696 {
					t.Fatalf("size %d page %d out of bounds: %+v", size, page, row)
				}
				warning = warning || strings.Contains(row.Text, "Осторожно")
				focus = focus || (row.Focused && row.ItemIndex == 0)
			}
			if !warning || !focus {
				t.Fatalf("size %d page %d hid warning or focus", size, page)
			}
		}
	}
}

// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"testing"
	"unicode/utf8"

	"too-broke-to-die/internal/tbtd/desktop/input"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

func TestMouseCanNavigateEveryMenuChoice(t *testing.T) {
	ctx := context.Background()
	measure := func(text string, size float64) float64 { return float64(utf8.RuneCountInString(text)) * size * .5 }
	for _, state := range []session.AppState{session.StateTitle, session.StateLedger} {
		for _, adult := range []bool{false, true} {
			if state == session.StateLedger && !adult {
				continue
			}
			ui, _ := interfaceFixture(t)
			ui.adultSeen = adult
			view := frameView(t, session.FrameViewData{State: state})
			screen, err := ui.Plan(ctx, view)
			if err != nil {
				t.Fatal(err)
			}
			count := len(screen.Items)
			for index := 0; index < count; index++ {
				screen, err = ui.Plan(ctx, view)
				if err != nil || screen.Focus != index {
					t.Fatalf("%s adult=%t index=%d: %+v %v", state, adult, index, screen, err)
				}
				rows, layoutErr := ui.MenuLayout(ctx, screen, measure)
				if layoutErr != nil {
					t.Fatal(layoutErr)
				}
				var next *LayoutRow
				for i := range rows {
					if rows[i].ItemIndex == -4 {
						next = &rows[i]
					}
				}
				if next == nil {
					t.Fatal("mouse navigation missing")
				}
				command, commandErr := session.NewFrameCommand(ctx, session.FrameInput{Edges: []session.ActionEdge{{Action: settings.MenuSelect, Kind: session.Pressed}}})
				if commandErr != nil {
					t.Fatal(commandErr)
				}
				command, commandErr = ui.Navigate(ctx, view, command, input.Point{X: next.X + 1, Y: next.Y + 1}, measure)
				if commandErr != nil || len(command.Data(ctx).Selections) != 0 {
					t.Fatalf("navigation activated item: %v", commandErr)
				}
			}
			if ui.focus != 0 {
				t.Fatal("navigation did not wrap")
			}
		}
	}
}

func TestActiveLayoutKeepsCaptionsAndPanelsInsideSafeArea(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	measure := func(text string, size float64) float64 { return float64(utf8.RuneCountInString(text)) * size * .5 }
	for _, size := range []int{100, 125, 150} {
		values := settings.Defaults(ctx)
		values.Interface.TextSize = size
		if err := ui.SetSettings(ctx, values); err != nil {
			t.Fatal(err)
		}
		screen := Screen{Emergency: "Опасность", Boss: []string{"Босс", "Здоровье"}, Captions: []string{"Первый звук", "Второй звук"}, Notice: "Последствия"}
		rows, err := ui.ActiveLayout(ctx, screen, []string{"Здоровье", "Конечности"}, []string{"Зачтено", "Долг"}, measure)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		step := 24 * float64(size) / 100 * 1.25
		for i, row := range rows {
			seen[row.Text] = true
			if row.X < 24 || row.Y < 24 || row.X+row.Width > 1256 || row.Y+step > 696.0001 {
				t.Fatalf("size %d: out of bounds %+v", size, row)
			}
			for _, other := range rows[:i] {
				if row.X < other.X+other.Width && other.X < row.X+row.Width && row.Y < other.Y+step-.001 && other.Y < row.Y+step-.001 {
					t.Fatalf("size %d: overlap %+v %+v", size, row, other)
				}
			}
		}
		for _, text := range []string{"Первый звук", "Второй звук", "Опасность", "Босс", "Здоровье", "Зачтено", "Последствия"} {
			if !seen[text] {
				t.Fatalf("size %d missing %q", size, text)
			}
		}
	}
}

func TestActiveCaptionWarningSurvivesFeedbackPaging(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	values := settings.Defaults(ctx)
	values.Interface.TextSize = 150
	if err := ui.SetSettings(ctx, values); err != nil {
		t.Fatal(err)
	}
	ui.activePage = 2
	measure := func(text string, size float64) float64 { return float64(utf8.RuneCountInString(text)) * size * .5 }
	screen := Screen{CaptionPriority: 1, Captions: []string{"Опасность", "Звук один", "Звук два", "Звук три", "Звук четыре", "Звук пять"}}
	rows, err := ui.ActiveLayout(ctx, screen, nil, nil, measure)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		found = found || row.Text == "Опасность"
	}
	if !found {
		t.Fatal("feedback page displaced live warning")
	}
}

func TestVolumeStepIncludesEndpoints(t *testing.T) {
	ui, _ := interfaceFixture(t)
	for _, test := range []struct{ from, want int }{{0, 10}, {80, 90}, {90, 100}, {95, 0}, {100, 0}} {
		ui.draft.Audio.Music = test.from
		ui.changeSetting("TXT-SET-MUSIC")
		if ui.draft.Audio.Music != test.want {
			t.Fatalf("%d: got %d, want %d", test.from, ui.draft.Audio.Music, test.want)
		}
	}
}

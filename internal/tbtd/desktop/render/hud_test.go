// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"strings"
	"testing"

	"too-broke-to-die/internal/tbtd/session"
)

func TestBossCaptionsAndRejectedAction(t *testing.T) {
	ctx := context.Background()
	ui, _ := interfaceFixture(t)
	data := session.FrameViewData{
		State:  session.StateActive,
		Boss:   session.BossView{ID: "ENC-HOS-05", Health: 294, MaximumHealth: 420, Phase: 2, Protected: true},
		Player: session.PlayerView{Health: 100, Channel: "regenerate", ChannelProgress: .5, RejectionID: "TXT-REASON-ARM"},
		Texts:  []session.TextView{{ID: "CAP-WARN-CRUSHER", Direction: "left", Speaker: "CHR-POT"}},
	}
	boss, captions, err := ui.hudExtras(ctx, data)
	if err != nil || len(boss) != 2 || !strings.Contains(boss[0], "Потрошин") || !strings.Contains(boss[0], "ЗАЩИТА") || !strings.Contains(boss[1], "70% / 35%") || !strings.Contains(boss[1], "|") {
		t.Fatalf("boss: %v %v", boss, err)
	}
	if len(captions) != 1 || !strings.Contains(captions[0], "Слева") || !strings.Contains(captions[0], "Потрошин") {
		t.Fatalf("captions: %v", captions)
	}
	left, _, err := ui.hudText(ctx, data)
	if err != nil || !strings.Contains(strings.Join(left, " "), "Восстановить конечности") || !strings.Contains(strings.Join(left, " "), "Бросок требует") {
		t.Fatalf("channel or rejection: %v %v", left, err)
	}
	data.Boss.MaximumHealth = 0
	if _, _, err = ui.hudExtras(ctx, data); err == nil {
		t.Fatal("invalid boss maximum accepted")
	}
}

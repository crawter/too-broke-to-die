// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"strconv"
	"strings"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
)

// hudText contains only live combat values and acknowledged progression.
func (ui *Interface) hudText(ctx context.Context, data session.FrameViewData) (left, right []string, err error) {
	defer mon.Task()(&ctx)(&err)
	add := func(target *[]string, id string, values map[string]string) error {
		text, textErr := ui.text(ctx, id, values)
		if textErr == nil {
			*target = append(*target, text)
		}
		return textErr
	}
	if err = add(&left, "TXT-HUD-HEALTH", map[string]string{"count": strconv.Itoa(data.Player.Health)}); err != nil {
		return nil, nil, err
	}
	bar := "["
	for i := 0; i < 10; i++ {
		if data.Player.Health > i*10 {
			bar += "#"
		} else {
			bar += "-"
		}
	}
	left = append(left, bar+"]")
	for i, id := range []string{"TXT-CTRL-LARM", "TXT-CTRL-RARM", "TXT-CTRL-LLEG", "TXT-CTRL-RLEG"} {
		name, nameErr := ui.text(ctx, id, nil)
		if nameErr != nil {
			return nil, nil, nameErr
		}
		marker := "[+] "
		if !data.Player.Limbs[i] {
			marker = "[X] "
		}
		left = append(left, marker+name)
	}
	if !data.Player.Limbs[2] && !data.Player.Limbs[3] {
		if err = add(&left, "TXT-HUD-NO-LEGS", nil); err != nil {
			return nil, nil, err
		}
	}
	if !data.Player.Limbs[0] && !data.Player.Limbs[1] {
		if err = add(&left, "TXT-HUD-NO-ARMS", nil); err != nil {
			return nil, nil, err
		}
	}
	if data.Player.Health < 25 {
		if err = add(&left, "TXT-HUD-LOW", nil); err != nil {
			return nil, nil, err
		}
	}
	if data.Player.SelectedPayload == "feces" {
		if err = add(&left, "TXT-HUD-FECES", map[string]string{"count": strconv.Itoa(data.Player.Feces)}); err != nil {
			return nil, nil, err
		}
	} else {
		id := map[string]string{"LIM-LA": "TXT-CTRL-LARM", "LIM-RA": "TXT-CTRL-RARM", "LIM-LL": "TXT-CTRL-LLEG", "LIM-RL": "TXT-CTRL-RLEG"}[data.Player.SelectedPayload]
		if id != "" {
			if err = add(&left, id, nil); err != nil {
				return nil, nil, err
			}
		}
	}
	if data.Player.Channel != "" {
		if data.Player.Channel == "remove" {
			if err = add(&left, "TXT-HUD-CHANNEL-COST", nil); err != nil {
				return nil, nil, err
			}
		}
		labels := map[string]string{"remove": "TXT-CTRL-REMOVE", "replace": "TXT-CTRL-INTERACT", "regenerate": "TXT-CTRL-REGEN", "interact": "TXT-CTRL-INTERACT", "refill": "TXT-REASON-REFILL"}
		if id := labels[data.Player.Channel]; id != "" {
			if err = add(&left, id, nil); err != nil {
				return nil, nil, err
			}
		}
		left = append(left, strconv.Itoa(int(data.Player.ChannelProgress*100))+"%")
	}
	if data.Player.RejectionID != "" {
		if err = add(&left, string(data.Player.RejectionID), nil); err != nil {
			return nil, nil, err
		}
	}
	if err = add(&right, "TXT-HUD-CREDIT", map[string]string{"count": strconv.Itoa(data.Progress.Credits)}); err != nil {
		return nil, nil, err
	}
	if err = add(&right, "TXT-HUD-DEBT", map[string]string{"amount": content.FormatDollars(data.Progress.DebtCents)}); err != nil {
		return nil, nil, err
	}
	saveID := map[session.SaveIndicator]string{session.SavePending: "TXT-SAVE-PENDING", session.SaveConfirmed: "TXT-SAVE-OK", session.SaveError: "TXT-SAVE-ERROR"}[data.Save]
	if saveID != "" {
		if err = add(&right, saveID, nil); err != nil {
			return nil, nil, err
		}
	}
	title, objective, err := ui.trackedObjective(ctx, data)
	if err != nil {
		return nil, nil, err
	}
	if title != "" {
		if err = add(&right, "TXT-HUD-OBJECTIVE", map[string]string{"service": title, "action": objective}); err != nil {
			return nil, nil, err
		}
	}
	return left, right, nil
}

// hudExtras resolves boss and sound-caption identities without relying on color.
func (ui *Interface) hudExtras(ctx context.Context, data session.FrameViewData) (boss, captions []string, err error) {
	defer mon.Task()(&ctx)(&err)
	if data.Boss.ID != "" {
		id := map[content.EncounterID]string{"ENC-HOS-05": "TXT-CHR-POT", "ENC-CHU-05": "TXT-CHR-SAV", "ENC-MIN-05": "TXT-CHR-BLA", "ENC-INS-05": "TXT-CHR-LEV"}[data.Boss.ID]
		if id == "" || data.Boss.MaximumHealth <= 0 || data.Boss.Health < 0 || data.Boss.Health > data.Boss.MaximumHealth {
			return nil, nil, content.ErrInvalid.New("invalid boss status")
		}
		name, textErr := ui.text(ctx, id, nil)
		if textErr != nil {
			return nil, nil, textErr
		}
		phase, textErr := ui.text(ctx, "TXT-HUD-PHASE", map[string]string{"count": strconv.Itoa(data.Boss.Phase)})
		if textErr != nil {
			return nil, nil, textErr
		}
		statusID := "TXT-BOSS-EXPOSED"
		if data.Boss.Protected {
			statusID = "TXT-BOSS-PROTECTION"
		}
		status, textErr := ui.text(ctx, statusID, nil)
		if textErr != nil {
			return nil, nil, textErr
		}
		bar := "["
		for i := 0; i < 20; i++ {
			if i == 6 || i == 13 {
				bar += "|"
			}
			if float64(data.Boss.Health)/float64(data.Boss.MaximumHealth) > float64(i)/20 {
				bar += "#"
			} else {
				bar += "-"
			}
		}
		boss = append(boss, name+" — "+phase+" — "+status, bar+"] "+strconv.Itoa(data.Boss.Health)+" / "+strconv.Itoa(data.Boss.MaximumHealth)+" (70% / 35%)")
	}
	if !ui.values.Interface.SoundCaptions {
		return boss, nil, nil
	}
	for _, item := range data.Texts {
		if !strings.HasPrefix(string(item.ID), "CAP-") {
			continue
		}
		text, textErr := ui.text(ctx, string(item.ID), nil)
		if textErr != nil {
			return nil, nil, textErr
		}
		if item.Speaker != "" {
			speaker, speakerErr := ui.text(ctx, "TXT-"+item.Speaker, nil)
			if speakerErr != nil {
				return nil, nil, speakerErr
			}
			text = speaker + ": " + text
		}
		if item.Direction != "" {
			directionID := map[string]string{"left": "CAP-LEFT", "right": "CAP-RIGHT", "above": "CAP-ABOVE", "near": "CAP-NEAR"}[item.Direction]
			if directionID == "" {
				return nil, nil, content.ErrInvalid.New("invalid caption direction")
			}
			direction, directionErr := ui.text(ctx, directionID, nil)
			if directionErr != nil {
				return nil, nil, directionErr
			}
			text, textErr = ui.text(ctx, "CAP-DIRECTION", map[string]string{"direction": direction, "sound": text})
			if textErr != nil {
				return nil, nil, textErr
			}
		}
		captions = append(captions, text)
	}
	return boss, captions, nil
}

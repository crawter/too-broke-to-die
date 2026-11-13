// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/desktop"
	"too-broke-to-die/internal/tbtd/session"
)

func (ui *Interface) anchor(ctx context.Context, id content.AnchorID) (string, error) {
	if id == "" {
		return "", content.ErrInvalid.New("missing saved anchor")
	}
	return ui.text(ctx, "TXT-"+string(id), nil)
}

func (ui *Interface) savedSummary(ctx context.Context, screen *Screen, progress session.ProgressView) (err error) {
	defer mon.Task()(&ctx)(&err)
	if progress.Campaign == "" {
		return nil // The profile has not been read: zero is not a known total.
	}
	anchor, err := ui.anchor(ctx, progress.Anchor)
	if err != nil {
		return err
	}
	credit, err := ui.text(ctx, "TXT-HUD-CREDIT", map[string]string{"count": strconv.Itoa(progress.Credits)})
	if err != nil {
		return err
	}
	if progress.Ending == "" {
		var institution string
		switch {
		case strings.HasPrefix(string(progress.Anchor), "ANC-HOS-"), progress.Anchor == "ANC-PRO-01", progress.Anchor == "ANC-PRO-02":
			institution = "TXT-SIGN-HOS"
		case strings.HasPrefix(string(progress.Anchor), "ANC-CHU-"), progress.Anchor == "ANC-PRO-03":
			institution = "TXT-SIGN-CHU"
		case strings.HasPrefix(string(progress.Anchor), "ANC-MIN-"):
			institution = "TXT-SIGN-MIN"
		case strings.HasPrefix(string(progress.Anchor), "ANC-INS-"):
			institution = "TXT-SIGN-INS"
		}
		if institution != "" {
			name, nameErr := ui.text(ctx, institution, nil)
			if nameErr != nil {
				return nameErr
			}
			screen.Lines = append(screen.Lines, name)
		}
	}
	screen.Lines = append(screen.Lines, anchor, credit)
	if progress.Ending != "" {
		stage, stageErr := ui.text(ctx, "TXT-"+string(progress.Ending), nil)
		if stageErr != nil {
			return stageErr
		}
		screen.Lines = append(screen.Lines, stage)
	}
	return nil
}

// eventName never exposes arbitrary operation descriptions or technical causes.
func (ui *Interface) eventName(ctx context.Context, label string) string {
	defer mon.Task()(&ctx)(nil)
	for _, deed := range ui.campaign.Deeds(ctx) {
		if label == string(deed.Deed.ID) || label == "failed: "+string(deed.Deed.ID) || label == "waste: "+string(deed.Deed.ID) {
			return deed.Title
		}
	}
	if strings.HasPrefix(label, "ANC-") || strings.HasPrefix(label, "END-") {
		if entry, ok := ui.texts.Entry("TXT-" + label); ok {
			return entry.Text
		}
	}
	if strings.HasPrefix(label, "EM-") {
		emergency := strings.TrimSuffix(strings.TrimPrefix(label, "EM-"), "-aftermath")
		if entry, ok := ui.texts.Entry("TXT-DANGER-" + emergency); ok {
			return entry.Text
		}
	}
	bosses := map[string]string{"ENC-HOS-05": "TXT-CHR-POT", "ENC-CHU-05": "TXT-CHR-SAV", "ENC-MIN-05": "TXT-CHR-BLA", "ENC-INS-05": "TXT-CHR-LEV"}
	if id := bosses[label]; id != "" {
		if entry, ok := ui.texts.Entry(id); ok {
			return "Бой: " + entry.Text
		}
	}
	narratives := map[string]string{
		"NAR-01": "Кража и сердечный приступ", "NAR-02": "Воскрешение на похоронах",
		"NAR-03": "Получение журнала", "NAR-04": "Возвращение в больницу",
		"NAR-05": "Закрытие больницы", "NAR-06": "Закрытие церковного архива",
		"NAR-07": "Отчёт министерства", "NAR-08": "Прибытие в институт", "NAR-09": "Восстановление городского узла",
	}
	name := strings.TrimSuffix(strings.TrimSuffix(label, " entry"), " viewed")
	if title := narratives[name]; title != "" {
		return title
	}
	switch {
	case label == "donation theft":
		return "Кража пожертвований"
	case label == "permit":
		return "Разрешение на окончательную смерть"
	case label == "temporary death":
		return "Смерть персонажа"
	case strings.HasPrefix(label, "checkpoint"):
		return "Контрольная точка"
	case label == "consent":
		return "Согласие на окончательную смерть"
	default:
		return "Событие прогресса"
	}
}

// NotifySettingsResult presents a separately owned settings reset without
// changing the campaign or silently applying a settings draft.
func (ui *Interface) NotifySettingsResult(ctx context.Context, result session.SettingsResult) (err error) {
	defer mon.Task()(&ctx)(&err)
	if ui == nil {
		return content.ErrInvalid.New("missing interface")
	}
	data := result.Data(ctx)
	if data.ID == "" || data.Status != session.StorageSucceeded {
		return content.ErrInvalid.New("invalid settings result")
	}
	if data.DefaultsRestored && data.ID != ui.settingsNoticeID {
		text, textErr := ui.text(ctx, "TXT-REC-SETTINGS", nil)
		if textErr != nil {
			return textErr
		}
		ui.notices = append(ui.notices, notice{text: text, remaining: 6 * time.Second})
		ui.settingsNoticeID = data.ID
	}
	return nil
}

func (ui *Interface) pendingEventName(ctx context.Context, pending session.PendingEvent) string {
	defer mon.Task()(&ctx)(nil)
	if pending.SceneName != "" && pending.UnitText != "" {
		return ui.eventName(ctx, pending.SceneName) + " — «" + pending.UnitText + "»"
	}
	return ui.eventName(ctx, pending.Label)
}

func (ui *Interface) recovery(ctx context.Context, screen *Screen, data session.FrameViewData) (err error) {
	defer mon.Task()(&ctx)(&err)
	if data.State == session.StateDead {
		var fee int64
		for i := len(data.Progress.Incidents) - 1; i >= 0; i-- {
			if data.Progress.Incidents[i].Kind == campaign.IncidentDeath {
				fee = data.Progress.Incidents[i].Cents
				break
			}
		}
		if fee != 4999 {
			return content.ErrInvalid.New("death view lacks acknowledged death fee")
		}
		for _, id := range []string{"TXT-DEATH-HEADING", "TXT-DEATH-PRESERVED"} {
			text, err := ui.text(ctx, id, nil)
			if err != nil {
				return err
			}
			screen.Lines = append(screen.Lines, text)
		}
		anchor, err := ui.anchor(ctx, data.Progress.Anchor)
		if err != nil {
			return err
		}
		text, err := ui.text(ctx, "TXT-DEATH-DESTINATION", map[string]string{"anchor": anchor, "count": strconv.Itoa(data.Progress.Credits), "amount": content.FormatDollars(fee)})
		if err != nil {
			return err
		}
		screen.Lines = append(screen.Lines, text)
		for _, item := range []struct {
			id     string
			action session.MenuAction
		}{{"TXT-MENU-RETRY", "retry_death"}, {"TXT-MENU-TITLE", "quit_to_title"}} {
			value, itemErr := ui.item(ctx, item.id, item.action, "")
			if itemErr != nil {
				return itemErr
			}
			screen.Items = append(screen.Items, value)
		}
		return nil
	}
	screen.Lines = append(screen.Lines, desktop.PresentError(data.Error).Message)
	if err := ui.savedSummary(ctx, screen, data.Progress); err != nil {
		return err
	}
	if data.Pending.Label != "" {
		text, err := ui.text(ctx, "TXT-SAVE-DISCARD", map[string]string{"event": ui.pendingEventName(ctx, data.Pending)})
		if err != nil {
			return err
		}
		screen.Lines = append(screen.Lines, text)
	}
	for _, choice := range data.Choices {
		if choice == session.ChooseOlder {
			for _, candidate := range data.Candidates {
				anchor, err := ui.anchor(ctx, candidate.Anchor)
				if err != nil {
					return err
				}
				// Only catalogue identities are safe to display; storage's raw
				// difference is not a player-facing diagnostic channel.
				difference := ""
				if !candidate.UnknownDifference && strings.HasPrefix(candidate.KnownDifference, "TXT-") {
					if entry, ok := ui.texts.Entry(candidate.KnownDifference); ok && !strings.ContainsAny(entry.Text, "{}") {
						difference = entry.Text
					}
				}
				if difference == "" {
					difference, err = ui.text(ctx, "TXT-REC-UNKNOWN", nil)
					if err != nil {
						return err
					}
				}
				label, err := ui.text(ctx, "TXT-REC-OLDER", map[string]string{"anchor": anchor, "count": strconv.Itoa(candidate.Credits), "difference": difference})
				if err != nil {
					return err
				}
				target := fmt.Sprintf("%s:%d", candidate.Campaign, candidate.Revision)
				screen.Items = append(screen.Items, Item{Label: label, Action: "choose_older", Target: target})
			}
			continue
		}
		entry, ok := map[session.RecoveryChoice]struct {
			id     string
			action session.MenuAction
		}{
			session.RetrySave:   {"TXT-SAVE-RETRY", "retry_save"},
			session.ReloadSaved: {"TXT-SAVE-RELOAD", "reload_saved"},
			session.ExitUnsaved: {"TXT-SAVE-EXIT", "exit_unsaved"},
			session.NewCampaign: {"TXT-MENU-NEW", "new_game"},
			session.ExitGame:    {"TXT-MENU-EXIT", "exit"},
		}[choice]
		if !ok {
			return content.ErrInvalid.New("unknown recovery choice")
		}
		item, err := ui.item(ctx, entry.id, entry.action, "")
		if err != nil {
			return err
		}
		screen.Items = append(screen.Items, item)
	}
	if data.BothRevisionsInvalid {
		text, err := ui.text(ctx, "TXT-REC-BROKEN", nil)
		if err != nil {
			return err
		}
		screen.Lines = append(screen.Lines, text)
	}
	return nil
}

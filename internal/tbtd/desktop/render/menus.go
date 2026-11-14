// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/desktop/input"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

// Interface holds presentation-only navigation. It never mutates the campaign
// or chooses a storage outcome. The owner supplies validated content and the
// same input sampler used for frame commands.
type Interface struct {
	texts             *content.RussianText
	campaign          *content.Campaign
	sampler           *input.Sampler
	values            settings.Values
	draft             settings.Values
	settingsDirty     bool
	settingsPending   bool
	focus             int
	detailPage        int
	activePage        int
	activeElapsed     time.Duration
	key               string
	ledgerTab         int
	institution       int
	tracked           content.DeedID
	section           string
	returnSection     string
	settingsTab       int
	adultSeen         bool
	armedKey          string
	armedIndex        int
	armedAction       settings.Action
	previous          session.ProgressView
	initialized       bool
	lastNotice        string
	settingsNoticeID  session.RequestID
	notices           []notice
	captions          []queuedCaption
	captionSource     map[session.TextView]bool
	captionGeneration uint64
	captionState      session.AppState
}

// NewInterface constructs a display-free interface using concrete in-process
// components. The settings value is copied and may later be replaced.
func NewInterface(ctx context.Context, texts *content.RussianText, campaign *content.Campaign, sampler *input.Sampler, values settings.Values) (_ *Interface, err error) {
	defer mon.Task()(&ctx)(&err)
	if texts == nil || campaign == nil || sampler == nil {
		return nil, content.ErrInvalid.New("missing interface dependencies")
	}
	if err = values.Validate(ctx); err != nil {
		return nil, err
	}
	return &Interface{texts: texts, campaign: campaign, sampler: sampler, values: values.Clone(ctx), draft: values.Clone(ctx)}, nil
}

// SetSettings updates presentation preferences after the settings owner has
// validated them. Input bindings continue to come from the sampler itself.
func (ui *Interface) SetSettings(ctx context.Context, values settings.Values) (err error) {
	defer mon.Task()(&ctx)(&err)
	if ui == nil {
		return content.ErrInvalid.New("missing interface")
	}
	if err = values.Validate(ctx); err != nil {
		return err
	}
	ui.values = values.Clone(ctx)
	ui.draft = values.Clone(ctx)
	ui.settingsDirty, ui.settingsPending = false, false
	return nil
}

// Item is one visible focusable action. Disabled items remain navigable and
// expose a reason, but Activate cannot emit their action.
type Item struct {
	Label          string
	Action         session.MenuAction
	Target         string
	DisabledReason string
}

// Screen is an immutable presentation plan derived from a frame's committed
// view. Focus indexes Items and always starts on Cancel for confirmations.
type Screen struct {
	Lines     []string
	Items     []Item
	Focus     int
	Emergency string
	Notice    string
	Boss      []string
	Captions  []string
	// CaptionPriority is the number of leading live warning captions.
	CaptionPriority int
	captionKeys     []session.TextView
}

func (ui *Interface) item(ctx context.Context, id string, action session.MenuAction, target string) (Item, error) {
	label, err := ui.text(ctx, id, nil)
	return Item{Label: label, Action: action, Target: target}, err
}

// Plan builds menu and recovery content without opening a native device.
func (ui *Interface) Plan(ctx context.Context, view session.FrameView) (_ Screen, err error) {
	defer mon.Task()(&ctx)(&err)
	if ui == nil {
		return Screen{}, content.ErrInvalid.New("missing interface")
	}
	data := view.Data(ctx)
	if data.State != session.StateTitle && data.State != session.StatePause {
		ui.section, ui.returnSection = "", ""
	}
	var screen Screen
	if data.State == session.StateActive || data.State == session.StatePause || data.State == session.StateLedger {
		screen.Boss, _, err = ui.hudExtras(ctx, data)
		if err != nil {
			return Screen{}, err
		}
		if ui.values.Interface.SoundCaptions {
			// Live warnings precede feedback; expired, undisplayed warnings are
			// explicitly historical rather than announcing a fictitious attack.
			for priority := 0; priority < 3; priority++ {
				for _, caption := range ui.captions {
					live := caption.warning && ui.captionSource[caption.key]
					if (priority == 0 && !live) || (priority == 1 && (!caption.warning || live)) || (priority == 2 && caption.warning) {
						continue
					}
					text := caption.text
					if caption.warning && !live {
						text = "Ранее: " + text
					}
					screen.Captions = append(screen.Captions, text)
					screen.captionKeys = append(screen.captionKeys, caption.key)
					if caption.warning {
						screen.CaptionPriority++
					}
				}
			}
		}
	}
	if len(ui.notices) > 0 {
		screen.Notice = ui.notices[0].text
	}
	if data.Emergency != "" {
		if !strings.HasPrefix(string(data.Emergency), "EM-") {
			return Screen{}, content.ErrInvalid.New("invalid emergency identity")
		}
		screen.Emergency, err = ui.text(ctx, "TXT-DANGER-"+strings.TrimPrefix(string(data.Emergency), "EM-"), nil)
		if err != nil {
			return Screen{}, err
		}
	}
	add := func(id string, action session.MenuAction, target string) error {
		item, itemErr := ui.item(ctx, id, action, target)
		if itemErr == nil {
			screen.Items = append(screen.Items, item)
		}
		return itemErr
	}
	line := func(id string) error {
		text, textErr := ui.text(ctx, id, nil)
		if textErr == nil {
			screen.Lines = append(screen.Lines, text)
		}
		return textErr
	}
	if data.Confirmation != "" {
		warning := data.Warning
		if data.Confirmation == "reload_saved" || data.Confirmation == "exit_unsaved" {
			warning, err = ui.text(ctx, "TXT-SAVE-DISCARD", map[string]string{"event": ui.pendingEventName(ctx, data.Pending)})
			if err != nil {
				return Screen{}, err
			}
		}
		screen.Lines = append(screen.Lines, warning)
		if data.Confirmation == "return_checkpoint" {
			if err = line("TXT-RETURN-NOFEE"); err != nil {
				return Screen{}, err
			}
		}
		if err = add("TXT-MENU-CANCEL", "cancel", ""); err != nil {
			return Screen{}, err
		}
		if err = add("TXT-MENU-CONFIRM", "confirm", ""); err != nil {
			return Screen{}, err
		}
	} else if (data.State == session.StateTitle && !ui.adultSeen && ui.section == "") || ui.section == "content" {
		if err = line("TXT-NOTICE-ADULT"); err != nil {
			return Screen{}, err
		}
		if ui.section == "content" {
			if err = add("TXT-MENU-CANCEL", "ui_back", ""); err != nil {
				return Screen{}, err
			}
		} else {
			for _, entry := range []struct {
				id, action, target string
			}{{"TXT-MENU-CONTINUE", "ui_adult_continue", ""}, {"TXT-SET-AUDIO", "ui_settings", "1"}, {"TXT-SET-ACCESS", "ui_settings", "3"}, {"TXT-MENU-EXIT", "exit_game", ""}} {
				if err = add(entry.id, session.MenuAction(entry.action), entry.target); err != nil {
					return Screen{}, err
				}
			}
		}
	} else if ui.section == "settings" {
		if err = ui.settingsScreen(ctx, &screen); err != nil {
			return Screen{}, err
		}
	} else {
		switch data.State {
		case session.StateTitle:
			if err = line("TXT-TITLE-SUBTITLE"); err != nil {
				return Screen{}, err
			}
			if !data.TitleLoading {
				if err = ui.savedSummary(ctx, &screen, data.Progress); err != nil {
					return Screen{}, err
				}
			}
			if data.TitleUnavailable {
				screen.Lines = append(screen.Lines, "Сведения о сохранении недоступны. Выберите продолжение для восстановления.")
			}
			if err = add("TXT-MENU-CONTINUE", "continue", ""); err != nil {
				return Screen{}, err
			}
			if data.TitleLoading {
				screen.Items[len(screen.Items)-1].DisabledReason = "Проверка сохранения"
			}
			if err = add("TXT-MENU-NEW", "new_game", ""); err != nil {
				return Screen{}, err
			}
			if data.TitleLoading {
				screen.Items[len(screen.Items)-1].DisabledReason = "Проверка сохранения"
			}
			if err = add("TXT-MENU-SETTINGS", "ui_settings", "0"); err != nil {
				return Screen{}, err
			}
			if err = add("TXT-MENU-EXIT", "exit_game", ""); err != nil {
				return Screen{}, err
			}
		case session.StatePause:
			if data.RestorationReady {
				if err = line("TXT-REC-READY"); err != nil {
					return Screen{}, err
				}
			}
			if err = ui.savedSummary(ctx, &screen, data.Progress); err != nil {
				return Screen{}, err
			}
			for _, entry := range []struct {
				id     string
				action session.MenuAction
			}{{"TXT-MENU-RESUME", "resume"}, {"TXT-MENU-LEDGER", "open_ledger"}, {"TXT-MENU-SETTINGS", "ui_settings"}, {"TXT-MENU-CONTROLS", "ui_controls"}, {"TXT-MENU-RETURN", "return_checkpoint"}, {"TXT-MENU-TITLE", "quit_to_title"}} {
				if err = add(entry.id, entry.action, ""); err != nil {
					return Screen{}, err
				}
			}
		case session.StateSaving:
			if err = line("TXT-SAVE-PENDING"); err != nil {
				return Screen{}, err
			}
		case session.StateRestoring:
			if err = ui.savedSummary(ctx, &screen, data.Progress); err != nil {
				return Screen{}, err
			}
			if err = line("TXT-REC-RESET"); err != nil {
				return Screen{}, err
			}
		case session.StateDead, session.StateError:
			if err = ui.recovery(ctx, &screen, data); err != nil {
				return Screen{}, err
			}
		case session.StateLedger:
			if err = ui.ledger(ctx, &screen, data); err != nil {
				return Screen{}, err
			}
		case session.StateEnd:
			if err = line("TXT-MENU-END"); err != nil {
				return Screen{}, err
			}
			if err = add("TXT-MENU-REPLAY", "replay_ending", ""); err != nil {
				return Screen{}, err
			}
			if err = add("TXT-MENU-NEW", "new_game", ""); err != nil {
				return Screen{}, err
			}
			if err = add("TXT-MENU-TITLE", "quit_to_title", ""); err != nil {
				return Screen{}, err
			}
		case session.StateScene:
			for _, text := range data.Texts {
				screen.Lines = append(screen.Lines, text.Text)
			}
			if data.Scene.Replay {
				if err = line("TXT-MENU-REPLAY"); err != nil {
					return Screen{}, err
				}
			}
		}
	}
	if screen.Notice != "" && data.State != session.StateActive {
		screen.Lines = append(screen.Lines, screen.Notice)
	}
	if data.State != session.StateActive && ui.values.Interface.SoundCaptions {
		for _, item := range data.Texts {
			if item.ID == "CAP-UI-PENDING" || item.ID == "CAP-UI-ERROR" || item.ID == "CAP-UI-SAVED" {
				caption, captionErr := ui.text(ctx, string(item.ID), nil)
				if captionErr != nil {
					return Screen{}, captionErr
				}
				screen.Lines = append(screen.Lines, caption)
			}
		}
	}
	key := fmt.Sprintf("%s/%s/%d/%d/%s/%d/%t", data.State, data.Confirmation, ui.ledgerTab, ui.institution, ui.section, ui.settingsTab, ui.adultSeen)
	if key != ui.key {
		ui.key, ui.focus, ui.detailPage = key, 0, 0
	}
	if ui.focus >= len(screen.Items) {
		ui.focus = max(0, len(screen.Items)-1)
	}
	screen.Focus = ui.focus
	return screen, nil
}

func (ui *Interface) settingsScreen(ctx context.Context, screen *Screen) (err error) {
	defer mon.Task()(&ctx)(&err)
	tabs := []string{"TXT-SET-DISPLAY", "TXT-SET-AUDIO", "TXT-SET-INTERFACE", "TXT-SET-ACCESS", "TXT-SET-CONTROLS"}
	if ui.settingsTab < 0 || ui.settingsTab >= len(tabs) {
		return content.ErrInvalid.New("invalid settings tab")
	}
	for index, id := range tabs {
		item, itemErr := ui.item(ctx, id, "ui_settings_tab", strconv.Itoa(index))
		if itemErr != nil {
			return itemErr
		}
		screen.Items = append(screen.Items, item)
	}
	title, err := ui.text(ctx, tabs[ui.settingsTab], nil)
	if err != nil {
		return err
	}
	screen.Lines = append(screen.Lines, title)
	addValue := func(id, value string) error {
		label, textErr := ui.text(ctx, id, nil)
		if textErr != nil {
			return textErr
		}
		item := Item{Label: label + ": " + value, Action: "ui_setting", Target: id}
		if ui.settingsTab == 0 || ui.settingsTab == 4 {
			item.Action = "ui_noop"
			item.DisabledReason = "Изменение пока недоступно"
		}
		screen.Items = append(screen.Items, item)
		return nil
	}
	onOff := func(value bool) string {
		if value {
			return "Включено"
		}
		return "Выключено"
	}
	switch ui.settingsTab {
	case 0:
		modeID := map[settings.DisplayMode]string{settings.Windowed: "TXT-SET-WINDOWED", settings.Borderless: "TXT-SET-BORDERLESS", settings.Fullscreen: "TXT-SET-FULLSCREEN"}[ui.draft.Display.Mode]
		mode, modeErr := ui.text(ctx, modeID, nil)
		if modeErr != nil {
			return modeErr
		}
		for _, entry := range []struct {
			id, value string
		}{{"TXT-SET-RESOLUTION", fmt.Sprintf("%d x %d — %s", ui.draft.Display.Width, ui.draft.Display.Height, mode)}, {"TXT-SET-VSYNC", onOff(ui.draft.Display.VSync)}} {
			if err = addValue(entry.id, entry.value); err != nil {
				return err
			}
		}
	case 1:
		for _, entry := range []struct {
			id    string
			value int
		}{{"TXT-SET-MASTER", ui.draft.Audio.Master}, {"TXT-SET-MUSIC", ui.draft.Audio.Music}, {"TXT-SET-EFFECTS", ui.draft.Audio.Effects}, {"TXT-SET-AMBIENCE", ui.draft.Audio.Ambience}, {"TXT-SET-VOICE", ui.draft.Audio.Vocal}} {
			if err = addValue(entry.id, fmt.Sprintf("%d%%", entry.value)); err != nil {
				return err
			}
		}
	case 2:
		for _, entry := range []struct {
			id, value string
		}{{"TXT-SET-TEXT-SIZE", fmt.Sprintf("%d%%", ui.draft.Interface.TextSize)}, {"TXT-SET-DIALOGUE-CAPTIONS", onOff(ui.draft.Interface.DialogueCaptions)}, {"TXT-SET-SOUND-CAPTIONS", onOff(ui.draft.Interface.SoundCaptions)}, {"TXT-SET-CAPTION-BACK", fmt.Sprintf("%d%%", ui.draft.Interface.CaptionOpacity)}} {
			if err = addValue(entry.id, entry.value); err != nil {
				return err
			}
		}
	case 3:
		shakeID := map[settings.Shake]string{settings.ShakeOff: "TXT-SET-OFF", settings.ShakeLow: "TXT-SET-LOW", settings.ShakeFull: "TXT-SET-FULL"}[ui.draft.Accessibility.Shake]
		shake, shakeErr := ui.text(ctx, shakeID, nil)
		if shakeErr != nil {
			return shakeErr
		}
		bloodID := map[settings.Particles]string{settings.ParticlesFull: "TXT-SET-FULL", settings.ParticlesReduced: "TXT-SET-REDUCED"}[ui.draft.Accessibility.Blood]
		blood, bloodErr := ui.text(ctx, bloodID, nil)
		if bloodErr != nil {
			return bloodErr
		}
		for _, entry := range []struct {
			id, value string
		}{{"TXT-SET-SHAKE", shake}, {"TXT-SET-BLOOD", blood}, {"TXT-SET-MOTION", onOff(ui.draft.Accessibility.ReducedMotion)}, {"TXT-SET-CONTRAST", onOff(ui.draft.Accessibility.HighContrast)}, {"TXT-SET-HOLD", onOff(ui.draft.Accessibility.HoldAssist)}, {"TXT-SET-RANGE", onOff(ui.draft.Audio.ReducedDynamicRange)}, {"TXT-SET-MONO", onOff(ui.draft.Audio.Mono)}} {
			if err = addValue(entry.id, entry.value); err != nil {
				return err
			}
		}
	case 4:
		for _, binding := range []struct {
			id     string
			action settings.Action
		}{{"TXT-CTRL-LEFT", settings.MoveLeft}, {"TXT-CTRL-RIGHT", settings.MoveRight}, {"TXT-CTRL-DOWN", settings.Down}, {"TXT-CTRL-JUMP", settings.Jump}, {"TXT-CTRL-AIM", settings.Aim}, {"TXT-CTRL-MELEE", settings.Melee}, {"TXT-CTRL-THROW", settings.Throw}, {"TXT-CTRL-EVADE", settings.Evade}, {"TXT-CTRL-INTERACT", settings.Interact}, {"TXT-CTRL-PAYLOAD", settings.SelectPayload}, {"TXT-CTRL-LARM", settings.SelectLeftArm}, {"TXT-CTRL-RARM", settings.SelectRightArm}, {"TXT-CTRL-LLEG", settings.SelectLeftLeg}, {"TXT-CTRL-RLEG", settings.SelectRightLeg}, {"TXT-CTRL-REMOVE", settings.RemoveLimb}, {"TXT-CTRL-REGEN", settings.Regenerate}, {"TXT-CTRL-LEDGER", settings.Ledger}, {"TXT-CTRL-PAUSE", settings.Pause}} {
			var labels []string
			for _, token := range ui.sampler.Tokens(ctx, binding.action, ui.sampler.ActiveDevice()) {
				labels = append(labels, token.Label)
			}
			if len(labels) == 0 {
				return content.ErrInvalid.New("missing control binding %s", binding.action)
			}
			if err = addValue(binding.id, strings.Join(labels, " / ")); err != nil {
				return err
			}
		}
	}
	apply, err := ui.item(ctx, "TXT-SET-APPLY", "ui_settings_apply", "")
	if err != nil {
		return err
	}
	if !ui.settingsDirty {
		apply.DisabledReason = "Нет изменений"
	}
	screen.Items = append(screen.Items, apply)
	contentItem, err := ui.item(ctx, "TXT-SET-CONTENT", "ui_content", "")
	if err != nil {
		return err
	}
	back, err := ui.item(ctx, "TXT-MENU-CANCEL", "ui_back", "")
	if err != nil {
		return err
	}
	screen.Items = append(screen.Items, contentItem, back)
	return nil
}

// TakeSettingsDraft hands an explicit apply request to the settings owner.
// It does not change the live sampler or claim that settings were saved.
func (ui *Interface) TakeSettingsDraft(ctx context.Context) (_ settings.Values, ok bool) {
	defer mon.Task()(&ctx)(nil)
	if ui == nil || !ui.settingsPending {
		return settings.Values{}, false
	}
	ui.settingsPending = false
	return ui.draft.Clone(ctx), true
}

func (ui *Interface) changeSetting(id string) {
	next := func(value int) int {
		if value > 90 {
			return 0
		}
		return value + 10
	}
	switch id {
	case "TXT-SET-MASTER":
		ui.draft.Audio.Master = next(ui.draft.Audio.Master)
	case "TXT-SET-MUSIC":
		ui.draft.Audio.Music = next(ui.draft.Audio.Music)
	case "TXT-SET-EFFECTS":
		ui.draft.Audio.Effects = next(ui.draft.Audio.Effects)
	case "TXT-SET-AMBIENCE":
		ui.draft.Audio.Ambience = next(ui.draft.Audio.Ambience)
	case "TXT-SET-VOICE":
		ui.draft.Audio.Vocal = next(ui.draft.Audio.Vocal)
	case "TXT-SET-TEXT-SIZE":
		switch ui.draft.Interface.TextSize {
		case 100:
			ui.draft.Interface.TextSize = 125
		case 125:
			ui.draft.Interface.TextSize = 150
		default:
			ui.draft.Interface.TextSize = 100
		}
	case "TXT-SET-DIALOGUE-CAPTIONS":
		ui.draft.Interface.DialogueCaptions = !ui.draft.Interface.DialogueCaptions
	case "TXT-SET-SOUND-CAPTIONS":
		ui.draft.Interface.SoundCaptions = !ui.draft.Interface.SoundCaptions
	case "TXT-SET-CAPTION-BACK":
		if ui.draft.Interface.CaptionOpacity == 90 {
			ui.draft.Interface.CaptionOpacity = 100
		} else {
			ui.draft.Interface.CaptionOpacity = 90
		}
	case "TXT-SET-SHAKE":
		switch ui.draft.Accessibility.Shake {
		case settings.ShakeOff:
			ui.draft.Accessibility.Shake = settings.ShakeLow
		case settings.ShakeLow:
			ui.draft.Accessibility.Shake = settings.ShakeFull
		default:
			ui.draft.Accessibility.Shake = settings.ShakeOff
		}
	case "TXT-SET-BLOOD":
		if ui.draft.Accessibility.Blood == settings.ParticlesFull {
			ui.draft.Accessibility.Blood = settings.ParticlesReduced
		} else {
			ui.draft.Accessibility.Blood = settings.ParticlesFull
		}
	case "TXT-SET-MOTION":
		ui.draft.Accessibility.ReducedMotion = !ui.draft.Accessibility.ReducedMotion
	case "TXT-SET-CONTRAST":
		ui.draft.Accessibility.HighContrast = !ui.draft.Accessibility.HighContrast
	case "TXT-SET-HOLD":
		ui.draft.Accessibility.HoldAssist = !ui.draft.Accessibility.HoldAssist
	case "TXT-SET-RANGE":
		ui.draft.Audio.ReducedDynamicRange = !ui.draft.Audio.ReducedDynamicRange
	case "TXT-SET-MONO":
		ui.draft.Audio.Mono = !ui.draft.Audio.Mono
	default:
		return
	}
	ui.settingsDirty = true
}

// MoveFocus moves through the complete list, including disabled rows.
func (ui *Interface) MoveFocus(screen Screen, delta int) {
	if ui == nil || len(screen.Items) == 0 {
		return
	}
	ui.focus = ((ui.focus+delta)%len(screen.Items) + len(screen.Items)) % len(screen.Items)
	ui.detailPage = 0
}

// Activate returns at most one semantic action for the session. UI-local
// filtering and tracking are handled without inventing a campaign transition.
func (ui *Interface) Activate(screen Screen) (session.MenuSelection, bool) {
	if ui == nil || ui.focus < 0 || ui.focus >= len(screen.Items) {
		return session.MenuSelection{}, false
	}
	item := screen.Items[ui.focus]
	if item.DisabledReason != "" {
		return session.MenuSelection{}, false
	}
	switch item.Action {
	case "ui_setting":
		ui.changeSetting(item.Target)
		return session.MenuSelection{}, false
	case "ui_settings_apply":
		ui.settingsPending = ui.settingsDirty
		return session.MenuSelection{}, false
	case "ui_adult_continue":
		ui.adultSeen = true
		return session.MenuSelection{}, false
	case "ui_settings", "ui_controls":
		ui.section = "settings"
		ui.settingsTab = 0
		if item.Action == "ui_controls" {
			ui.settingsTab = 4
		} else if item.Target != "" {
			if value, err := strconv.Atoi(item.Target); err == nil && value >= 0 && value < 5 {
				ui.settingsTab = value
			}
		}
		return session.MenuSelection{}, false
	case "ui_settings_tab":
		if value, err := strconv.Atoi(item.Target); err == nil && value >= 0 && value < 5 {
			ui.settingsTab = value
		}
		return session.MenuSelection{}, false
	case "ui_content":
		ui.returnSection, ui.section = ui.section, "content"
		return session.MenuSelection{}, false
	case "ui_back":
		if ui.section == "content" {
			ui.section, ui.returnSection = ui.returnSection, ""
		} else {
			ui.section = ""
		}
		return session.MenuSelection{}, false
	case "ui_tab":
		ui.ledgerTab = (ui.ledgerTab + 1) % 3
		return session.MenuSelection{}, false
	case "ui_institution":
		ui.institution = (ui.institution + 1) % 4
		return session.MenuSelection{}, false
	case "ui_track":
		ui.tracked = content.DeedID(item.Target)
		return session.MenuSelection{}, false
	case "ui_noop":
		return session.MenuSelection{}, false
	default:
		return session.MenuSelection{Action: item.Action, Target: item.Target}, true
	}
}

type notice struct {
	text      string
	remaining time.Duration
	page      int
	pages     int
	size      int
}

type queuedCaption struct {
	key       session.TextView
	text      string
	warning   bool
	remaining time.Duration
	page      int
	pages     int
	size      int
	visible   bool
	delivered bool
}

func warningCaption(id string) bool {
	return strings.HasPrefix(id, "CAP-WARN-") || strings.HasPrefix(id, "CAP-BOSS-") || id == "CAP-SOURCE-QUEUE" || id == "CAP-RELAY-PROTECT"
}

// advanceCaptions copies cues out of the session's short-lived view. Timers
// start only after a complete page was laid out, and stop for every overlay.
func (ui *Interface) advanceCaptions(ctx context.Context, data session.FrameViewData, elapsed time.Duration) (err error) {
	defer mon.Task()(&ctx)(&err)
	if !ui.values.Interface.SoundCaptions {
		ui.captions, ui.captionSource = nil, nil
		ui.captionState = data.State
		return nil
	}
	if data.World.Generation != 0 && ui.captionGeneration != data.World.Generation {
		// A reconstructed world ends live warnings but must not discard an
		// undisplayed cue from the interrupted world.
		ui.captionSource = nil
		ui.captionGeneration = data.World.Generation
	}
	current := ui.captionSource
	if data.State == session.StateActive || data.State == session.StatePause || data.State == session.StateLedger {
		current = make(map[session.TextView]bool)
		for _, item := range data.Texts {
			id := string(item.ID)
			if !strings.HasPrefix(id, "CAP-") {
				continue
			}
			current[item] = true
			if ui.captionSource[item] {
				continue
			}
			found := false
			for _, caption := range ui.captions {
				if caption.key == item {
					found = true
					break
				}
			}
			if found {
				continue
			}
			_, texts, textErr := ui.hudExtras(ctx, session.FrameViewData{Texts: []session.TextView{item}})
			if textErr != nil {
				return textErr
			}
			if len(texts) == 1 {
				ui.captions = append(ui.captions, queuedCaption{key: item, text: texts[0], warning: warningCaption(id), remaining: 6 * time.Second})
			}
		}
	}
	ui.captionSource = current
	var waiting, rotated []queuedCaption
	for _, caption := range ui.captions {
		if caption.visible && data.State == session.StateActive && ui.captionState == session.StateActive {
			caption.remaining -= min(elapsed, caption.remaining)
		}
		caption.visible = false
		if caption.warning && caption.delivered && !current[caption.key] {
			continue
		}
		if caption.remaining > 0 {
			waiting = append(waiting, caption)
			continue
		}
		if caption.page+1 < caption.pages {
			caption.page++
			caption.remaining = 6 * time.Second
			waiting = append(waiting, caption)
		} else if caption.warning && current[caption.key] {
			// A persistent warning yields to other warnings after a readable
			// interval, then is shown again while its source remains live.
			caption.page = 0
			caption.remaining = 6 * time.Second
			caption.delivered = true
			rotated = append(rotated, caption)
		}
	}
	ui.captions = append(waiting, rotated...)
	ui.captionState = data.State
	return nil
}

// Advance admits notices only for newly acknowledged revisions. Restoration
// establishes a baseline without replaying rewards; overlays freeze expiry.
func (ui *Interface) Advance(ctx context.Context, view session.FrameView, elapsed time.Duration) (err error) {
	defer mon.Task()(&ctx)(&err)
	if ui == nil || elapsed < 0 {
		return content.ErrInvalid.New("invalid notice timing")
	}
	data := view.Data(ctx)
	if ui.initialized && ui.previous.Campaign != data.Progress.Campaign {
		ui.captions, ui.captionSource = nil, nil
	}
	if err = ui.advanceCaptions(ctx, data, elapsed); err != nil {
		return err
	}
	if data.State == session.StateActive {
		// Overflowing HUD panels cycle without changing gameplay or text size.
		ui.activeElapsed += elapsed % (6 * time.Second)
		ui.activePage = (ui.activePage + int((elapsed/(6*time.Second))%1024)) % 1024
		if ui.activeElapsed >= 6*time.Second {
			ui.activeElapsed -= 6 * time.Second
			ui.activePage = (ui.activePage + 1) % 1024
		}
	}
	if data.State == session.StateActive && len(ui.notices) > 0 {
		ui.notices[0].remaining -= elapsed
		if ui.notices[0].remaining <= 0 {
			if ui.notices[0].page+1 < ui.notices[0].pages {
				ui.notices[0].page++
				ui.notices[0].remaining = 6 * time.Second
			} else {
				ui.notices = ui.notices[1:]
			}
		}
	}
	acknowledged := false
	for _, event := range data.Events {
		if event.Durable && event.Kind == "saved" && event.Revision == data.Progress.Revision {
			acknowledged = true
		}
	}
	if ui.initialized && ui.previous.Campaign != "" && ui.previous.Campaign == data.Progress.Campaign && data.Progress.Revision > ui.previous.Revision && acknowledged {
		if err = ui.enqueueProgress(ctx, ui.previous, data.Progress); err != nil {
			return err
		}
	}
	if ui.initialized && ui.previous.Campaign != data.Progress.Campaign {
		ui.notices = nil
	}
	ui.initialized = true
	ui.previous = data.Progress
	if data.Notice != "" && data.Notice != ui.lastNotice {
		text := ""
		switch {
		case strings.HasPrefix(data.Notice, "Discarded: "):
			text = "Отброшено несохранённое событие: " + ui.eventName(ctx, strings.TrimPrefix(data.Notice, "Discarded: "))
		case strings.HasPrefix(data.Notice, "Already saved: "):
			text = "Сохранено: " + ui.eventName(ctx, strings.TrimPrefix(data.Notice, "Already saved: "))
		case data.Notice == "Выберите запрос":
			text = data.Notice
		case data.Notice == "Uncertified attempt work resets; certified services and penalties remain.":
			text, err = ui.text(ctx, "TXT-REC-RESET", nil)
		case data.Notice == "Live position and uncertified work reset; certified services and penalties remain.":
			text, err = ui.text(ctx, "TXT-MENU-QUIT-WARN", nil)
		}
		if err != nil {
			return err
		}
		if text != "" {
			ui.notices = append(ui.notices, notice{text: text, remaining: 6 * time.Second})
		}
	}
	ui.lastNotice = data.Notice
	return nil
}

func (ui *Interface) enqueueProgress(ctx context.Context, before, after session.ProgressView) (err error) {
	defer mon.Task()(&ctx)(&err)
	known := make(map[content.DeedID]bool, len(before.Receipts))
	for _, receipt := range before.Receipts {
		known[receipt.Deed] = true
	}
	for _, receipt := range after.Receipts {
		if known[receipt.Deed] {
			continue
		}
		heading, err := ui.text(ctx, "TXT-RECEIPT-HEADING", nil)
		if err != nil {
			return err
		}
		detail, err := ui.text(ctx, "TXT-RECEIPT-DETAIL", map[string]string{"service": receipt.Title, "count": fmt.Sprint(after.Credits)})
		if err != nil {
			return err
		}
		ui.notices = append(ui.notices, notice{text: heading + "\n" + detail, remaining: 6 * time.Second})
	}
	old := make(map[content.ConsequenceID]bool, len(before.Consequences))
	for _, id := range before.Consequences {
		old[id] = true
	}
	for _, id := range after.Consequences {
		if old[id] {
			continue
		}
		for _, deed := range ui.campaign.Deeds(ctx) {
			if deed.Deed.Consequence != id {
				continue
			}
			heading, err := ui.text(ctx, "TXT-CONSEQUENCE-HEADING", nil)
			if err != nil {
				return err
			}
			textID := "TXT-AFTER-" + strings.TrimPrefix(string(deed.Location), "LOC-") + "-" + deed.Family
			var fields map[string]string
			if deed.Family != "END" {
				fields = map[string]string{"service": deed.Title}
			}
			detail, err := ui.text(ctx, textID, fields)
			if err != nil {
				return err
			}
			ui.notices = append(ui.notices, notice{text: heading + "\n" + detail, remaining: 6 * time.Second})
			break
		}
	}
	return nil
}

// SetFocus selects a row after the desktop's pointer hit test.
func (ui *Interface) SetFocus(screen Screen, index int) bool {
	if ui == nil || index < 0 || index >= len(screen.Items) {
		return false
	}
	ui.focus, ui.detailPage = index, 0
	return true
}

// Navigate translates sampled keyboard, controller and pointer edges into at
// most one selection. Confirmation is emitted on release so Resume observes
// the session's release-before-resume safety rule.
func (ui *Interface) Navigate(ctx context.Context, view session.FrameView, command session.FrameCommand, pointer input.Point, measure func(string, float64) float64) (_ session.FrameCommand, err error) {
	defer mon.Task()(&ctx)(&err)
	if ui == nil {
		return session.FrameCommand{}, content.ErrInvalid.New("missing interface")
	}
	frame := command.Data(ctx)
	screen, err := ui.Plan(ctx, view)
	if err != nil {
		return session.FrameCommand{}, err
	}
	modal := ui.section != "" || view.Data(ctx).Confirmation != "" || (view.Data(ctx).State == session.StateTitle && !ui.adultSeen)
	if frame.Focus == session.FocusLost {
		ui.armedKey = ""
		return command, nil
	}
	for _, device := range frame.Devices {
		if device.Device == settings.Controller && device.Active && !device.Connected {
			ui.armedKey = ""
			return command, nil
		}
	}
	if ui.armedKey != "" && ui.armedKey != ui.key {
		ui.armedKey = ""
	}
	for _, edge := range frame.Edges {
		if edge.Kind == session.Pressed {
			switch edge.Action {
			case settings.MenuUp:
				ui.MoveFocus(screen, -1)
			case settings.MenuDown:
				ui.MoveFocus(screen, 1)
			case settings.MenuLeft:
				ui.ScrollDetails(-1)
			case settings.MenuRight:
				ui.ScrollDetails(1)
			case settings.MenuBack:
				if ui.section == "content" {
					ui.section, ui.returnSection = ui.returnSection, ""
				} else if ui.section != "" {
					ui.section = ""
				} else if view.Data(ctx).Confirmation != "" {
					frame.Selections = append(frame.Selections, session.MenuSelection{Action: "cancel"})
				} else if view.Data(ctx).State == session.StateLedger {
					frame.Selections = append(frame.Selections, session.MenuSelection{Action: "close_ledger"})
				}
			case settings.MenuSelect:
				layout, layoutErr := ui.MenuLayout(ctx, screen, measure)
				if layoutErr != nil {
					return session.FrameCommand{}, layoutErr
				}
				for _, row := range layout {
					height := 24 * float64(ui.values.Interface.TextSize) / 100 * 1.25
					if pointer.X < row.X || pointer.X >= row.X+row.Width || pointer.Y < row.Y || pointer.Y >= row.Y+height {
						continue
					}
					if row.ItemIndex == -2 {
						if row.LastPage {
							ui.detailPage = 0
						} else {
							ui.ScrollDetails(1)
						}
						break
					}
					if row.ItemIndex == -3 || row.ItemIndex == -4 {
						delta := -1
						if row.ItemIndex == -4 {
							delta = 1
						}
						ui.MoveFocus(screen, delta)
						break
					}
					if row.ItemIndex >= 0 {
						ui.SetFocus(screen, row.ItemIndex)
						ui.armedKey, ui.armedIndex, ui.armedAction = ui.key, row.ItemIndex, edge.Action
						break
					}
				}
			case settings.MenuConfirm:
				if len(screen.Items) > 0 {
					ui.armedKey, ui.armedIndex, ui.armedAction = ui.key, ui.focus, edge.Action
				}
			}
		} else if edge.Kind == session.Released && ui.armedKey == ui.key && ui.armedAction == edge.Action {
			ui.focus = ui.armedIndex
			selection, ok := ui.Activate(screen)
			ui.armedKey = ""
			if ok {
				frame.Selections = append(frame.Selections, selection)
			}
		}
	}
	if modal {
		frame.Edges = slices.DeleteFunc(frame.Edges, func(edge session.ActionEdge) bool {
			return edge.Kind == session.Pressed && (edge.Action == settings.Pause || edge.Action == settings.Ledger)
		})
	}
	if len(frame.Selections) > 1 {
		return session.FrameCommand{}, session.InvalidAction
	}
	return session.NewFrameCommand(ctx, frame)
}

// LayoutRow is one logical-pixel text row. ItemIndex is -1 for information,
// -2 for details paging, -3/-4 for previous/next choice, or a menu index.
type LayoutRow struct {
	Text        string
	X, Y, Width float64
	ItemIndex   int
	Focused     bool
	LastPage    bool
}

// ScrollDetails changes the page of long information or the focused item.
func (ui *Interface) ScrollDetails(delta int) {
	if ui != nil {
		ui.detailPage = max(0, ui.detailPage+delta)
	}
}

// MenuLayout paginates vertically after reserving the emergency warning and
// space for the focused choice. The callback measures at the given text size.
func (ui *Interface) MenuLayout(ctx context.Context, screen Screen, measure func(string, float64) float64) (_ []LayoutRow, err error) {
	defer mon.Task()(&ctx)(&err)
	if ui == nil || measure == nil {
		return nil, content.ErrInvalid.New("missing layout input")
	}
	size := 24 * float64(ui.values.Interface.TextSize) / 100
	step := size * 1.25
	wrap := func(text string, width float64) ([]string, error) {
		return wrapText(text, width, func(value string) float64 { return measure(value, size) })
	}
	var rows []LayoutRow
	y := 24.0
	if screen.Emergency != "" {
		lines, wrapErr := wrap(screen.Emergency, 560)
		if wrapErr != nil {
			return nil, wrapErr
		}
		for _, line := range lines {
			rows = append(rows, LayoutRow{Text: line, X: 360, Y: y, Width: 560, ItemIndex: -1})
			y += step
		}
	}
	y = max(100, y+16)
	budget := int((696 - y) / step)
	if budget < 2 {
		return nil, content.ErrInvalid.New("warning leaves no room for menu")
	}
	var details []string
	for _, text := range screen.Lines {
		lines, wrapErr := wrap(text, 720)
		if wrapErr != nil {
			return nil, wrapErr
		}
		details = append(details, lines...)
	}
	var focused []string
	if len(screen.Items) != 0 {
		if screen.Focus < 0 || screen.Focus >= len(screen.Items) {
			return nil, content.ErrInvalid.New("invalid menu focus")
		}
		item := screen.Items[screen.Focus]
		label := item.Label
		if item.DisabledReason != "" {
			label += " — " + item.DisabledReason
		}
		focused, err = wrap(label, 720)
		if err != nil {
			return nil, err
		}
	}
	// Reserve a page indicator, then divide remaining rows between information
	// and the focused item. No focused row can leave the safe area.
	budget--
	otherCap := 0
	if len(screen.Items) > 1 && budget > 3 {
		// A mouse can move focus without activating a preview. Both directions
		// remain available even when the list is longer than the viewport.
		budget -= 2
		otherCap = 1
	}
	detailCap, itemCap := budget, 0
	if len(focused) > 0 {
		itemCap = min(len(focused), max(1, budget/2))
		detailCap = budget - itemCap
		if len(details) < detailCap {
			itemCap = min(len(focused), itemCap+detailCap-len(details))
			detailCap = min(detailCap, len(details))
		}
	}
	pages := 1
	if detailCap > 0 {
		pages = max(pages, (len(details)+detailCap-1)/detailCap)
	}
	if itemCap > 0 {
		pages = max(pages, (len(focused)+itemCap-1)/itemCap)
	}
	page := min(ui.detailPage, pages-1)
	add := func(text string, index int, focused bool) {
		rows = append(rows, LayoutRow{Text: text, X: 280, Y: y, Width: 720, ItemIndex: index, Focused: focused})
		y += step
	}
	if detailCap > 0 && len(details) > 0 {
		start := min(page*detailCap, max(0, len(details)-detailCap))
		for _, line := range details[start:min(len(details), start+detailCap)] {
			add(line, -1, false)
		}
	}
	if itemCap > 0 {
		start := min(page*itemCap, max(0, len(focused)-itemCap))
		for _, line := range focused[start:min(len(focused), start+itemCap)] {
			add(line, screen.Focus, true)
		}
	}
	if otherCap > 0 {
		rows = append(rows,
			LayoutRow{Text: "Назад по списку", X: 280, Y: y, Width: 350, ItemIndex: -3},
			LayoutRow{Text: "Далее по списку", X: 650, Y: y, Width: 350, ItemIndex: -4},
		)
		y += step
		next := (screen.Focus + 1) % len(screen.Items)
		item := screen.Items[next]
		label := item.Label
		if item.DisabledReason != "" {
			label += " — " + item.DisabledReason
		}
		lines, wrapErr := wrap(label, 720)
		if wrapErr != nil {
			return nil, wrapErr
		}
		if len(lines) > 0 {
			// A preview is selectable by mouse; the focused row exposes the
			// complete label on its own pages.
			add(lines[0], next, false)
		}
	}
	if pages > 1 {
		rows = append(rows, LayoutRow{Text: fmt.Sprintf("Страница %d/%d — далее", page+1, pages), X: 280, Y: y, Width: 720, ItemIndex: -2, LastPage: page == pages-1})
	}
	return rows, nil
}

// ActiveLayout plans all live panels in logical pixels before native drawing.
// Long panels cycle through pages on the active presentation clock; warning
// text is never displaced by a caption or a notice.
func (ui *Interface) ActiveLayout(ctx context.Context, screen Screen, left, right []string, measure func(string, float64) float64) (_ []LayoutRow, err error) {
	defer mon.Task()(&ctx)(&err)
	if ui == nil || measure == nil {
		return nil, content.ErrInvalid.New("missing active layout input")
	}
	size := 24 * float64(ui.values.Interface.TextSize) / 100
	step := size * 1.25
	wrap := func(texts []string, width float64) ([]string, error) {
		var result []string
		for _, text := range texts {
			lines, wrapErr := wrapText(text, width, func(value string) float64 { return measure(value, size) })
			if wrapErr != nil {
				return nil, wrapErr
			}
			result = append(result, lines...)
		}
		return result, nil
	}
	var rows []LayoutRow
	addPage := func(lines []string, x, y, width float64, capacity int) {
		if capacity <= 0 || len(lines) == 0 {
			return
		}
		pages := (len(lines) + capacity - 1) / capacity
		start := (ui.activePage % pages) * capacity
		for _, line := range lines[start:min(len(lines), start+capacity)] {
			rows = append(rows, LayoutRow{Text: line, X: x, Y: y, Width: width, ItemIndex: -1})
			y += step
		}
	}
	if screen.CaptionPriority < 0 || screen.CaptionPriority > len(screen.Captions) {
		return nil, content.ErrInvalid.New("invalid caption priority")
	}
	if len(screen.captionKeys) == len(screen.Captions) {
		for i := range ui.captions {
			ui.captions[i].visible = false
		}
	}
	captionCap := min(6, int(216/step))
	// Standalone layout callers do not supply the interface's admission
	// queue. Rotate their overflowing warnings rather than truncating the tail.
	indices := make([]int, 0, len(screen.Captions))
	startWarning := 0
	if len(screen.captionKeys) != len(screen.Captions) && screen.CaptionPriority > 0 {
		warningLines, wrapErr := wrap(screen.Captions[:screen.CaptionPriority], 560)
		if wrapErr != nil {
			return nil, wrapErr
		}
		if len(warningLines) > captionCap {
			startWarning = ui.activePage % screen.CaptionPriority
		}
	}
	for i := 0; i < screen.CaptionPriority; i++ {
		indices = append(indices, (startWarning+i)%screen.CaptionPriority)
	}
	for i := screen.CaptionPriority; i < len(screen.Captions); i++ {
		indices = append(indices, i)
	}
	var captionLines []string
	for _, index := range indices {
		lines, wrapErr := wrap([]string{screen.Captions[index]}, 560)
		if wrapErr != nil {
			return nil, wrapErr
		}
		var queued *queuedCaption
		if len(screen.captionKeys) == len(screen.Captions) {
			for i := range ui.captions {
				if ui.captions[i].key == screen.captionKeys[index] {
					queued = &ui.captions[i]
					break
				}
			}
		}
		pages := max(1, (len(lines)+captionCap-1)/captionCap)
		page := ui.activePage % pages
		if queued != nil {
			if queued.size != ui.values.Interface.TextSize || queued.pages != pages {
				queued.page, queued.remaining = 0, 6*time.Second
				queued.size, queued.pages = ui.values.Interface.TextSize, pages
			}
			page = queued.page
		}
		start := page * captionCap
		part := lines[start:min(len(lines), start+captionCap)]
		if len(part) > captionCap-len(captionLines) {
			// Keep the entire cue waiting; a partly displayed cue must not
			// start its clock or displace an earlier warning.
			break
		}
		captionLines = append(captionLines, part...)
		if queued != nil {
			queued.visible = true
		}
	}
	captionCount := len(captionLines)
	captionY := 696 - float64(captionCount)*step
	for i, line := range captionLines {
		rows = append(rows, LayoutRow{Text: line, X: 360, Y: captionY + float64(i)*step, Width: 560, ItemIndex: -1})
	}
	warning, err := wrap([]string{screen.Emergency}, 560)
	if err != nil {
		return nil, err
	}
	if screen.Emergency == "" {
		warning = nil
	}
	centerEnd := 696.0
	if captionCount > 0 {
		centerEnd = captionY - 8
	}
	if 24+float64(len(warning))*step > centerEnd {
		return nil, content.ErrInvalid.New("emergency warning exceeds active panel")
	}
	for i, line := range warning {
		rows = append(rows, LayoutRow{Text: line, X: 360, Y: 24 + float64(i)*step, Width: 560, ItemIndex: -1})
	}
	boss, err := wrap(screen.Boss, 560)
	if err != nil {
		return nil, err
	}
	bossY := 24 + float64(len(warning))*step
	addPage(boss, 360, bossY, 560, int((centerEnd-bossY)/step))
	leftLines, err := wrap(left, 270)
	if err != nil {
		return nil, err
	}
	rightLines, err := wrap(right, 300)
	if err != nil {
		return nil, err
	}
	capacity := int((696 - 24) / step)
	var noticeLines []string
	if screen.Notice != "" {
		noticeLines, err = wrap([]string{screen.Notice}, 300)
		if err != nil {
			return nil, err
		}
		// Reserve the lower right panel for the notice independently of the
		// objective's global page. Long notices have their own six-second pages.
		noticeCap := capacity - min(2, len(rightLines))
		if noticeCap <= 0 {
			return nil, content.ErrInvalid.New("notice leaves no room in active panel")
		}
		pages := max(1, (len(noticeLines)+noticeCap-1)/noticeCap)
		page := 0
		if len(ui.notices) > 0 && ui.notices[0].text == screen.Notice {
			notice := &ui.notices[0]
			if notice.size != ui.values.Interface.TextSize || notice.pages != pages {
				notice.page, notice.remaining = 0, 6*time.Second
				notice.size, notice.pages = ui.values.Interface.TextSize, pages
			}
			page = notice.page
		}
		start := page * noticeCap
		noticeLines = noticeLines[start:min(len(noticeLines), start+noticeCap)]
	}
	addSide := func(lines []string, x, width float64, available int) {
		// Health and credit/debt remain visible even when supporting details
		// need several pages at the largest text size.
		pinned := min(2, len(lines), available)
		for i, line := range lines[:pinned] {
			rows = append(rows, LayoutRow{Text: line, X: x, Y: 24 + float64(i)*step, Width: width, ItemIndex: -1})
		}
		addPage(lines[pinned:], x, 24+float64(pinned)*step, width, available-pinned)
	}
	addSide(leftLines, 28, 270, capacity)
	addSide(rightLines, 948, 300, capacity-len(noticeLines))
	for i, line := range noticeLines {
		rows = append(rows, LayoutRow{Text: line, X: 948, Y: 24 + float64(capacity-len(noticeLines)+i)*step, Width: 300, ItemIndex: -1})
	}
	return rows, nil
}

// drawInterface renders text at the final viewport resolution. The cached
// Noto font was validated at load; dynamic binding glyphs are checked again.
func (renderer *Renderer) drawInterface(ctx context.Context, view session.FrameView, viewport input.Viewport) (err error) {
	defer mon.Task()(&ctx)(&err)
	ui := renderer.interfaceUI
	screen, err := ui.Plan(ctx, view)
	if err != nil {
		return err
	}
	font, err := renderer.cache.Font(ctx, "FONT-NOTO-REGULAR")
	if err != nil {
		return err
	}
	if font.Texture.ID == 0 || font.CharsCount <= 0 {
		return content.ErrInvalid.New("FONT-NOTO-REGULAR: invalid font")
	}
	data := view.Data(ctx)
	scale := float32(viewport.Width / LogicalWidth)
	size := float32(24) * float32(ui.values.Interface.TextSize) / 100 * scale
	spacing := scale
	type row struct {
		text        string
		x, y, width float32
		focused     bool
	}
	var rows []row
	add := func(text string, x, y, width float32, focused bool) (float32, error) {
		for _, r := range text {
			if r != '?' && r != '\n' && rl.GetGlyphIndex(font, r) == rl.GetGlyphIndex(font, '?') {
				return y, content.ErrInvalid.New("FONT-NOTO-REGULAR: missing glyph U+%04X", r)
			}
		}
		measure := func(s string) float64 { return float64(rl.MeasureTextEx(font, s, size, spacing).X) }
		lines, wrapErr := wrapText(text, float64(width), measure)
		if wrapErr != nil {
			return y, wrapErr
		}
		for _, line := range lines {
			rows = append(rows, row{text: line, x: x, y: y, width: width, focused: focused})
			y += size * 1.25
		}
		return y, nil
	}
	x := float32(viewport.X)
	y := float32(viewport.Y)
	if data.State == session.StateActive {
		left, right, hudErr := ui.hudText(ctx, data)
		if hudErr != nil {
			return hudErr
		}
		layout, layoutErr := ui.ActiveLayout(ctx, screen, left, right, func(text string, logicalSize float64) float64 {
			return float64(rl.MeasureTextEx(font, text, float32(logicalSize)*scale, spacing).X / scale)
		})
		if layoutErr != nil {
			return layoutErr
		}
		for _, line := range layout {
			if _, err = add(line.Text, x+float32(line.X)*scale, y+float32(line.Y)*scale, float32(line.Width)*scale, false); err != nil {
				return err
			}
		}
	}
	if data.State != session.StateActive {
		layout, layoutErr := ui.MenuLayout(ctx, screen, func(text string, logicalSize float64) float64 {
			return float64(rl.MeasureTextEx(font, text, float32(logicalSize)*scale, spacing).X / scale)
		})
		if layoutErr != nil {
			return layoutErr
		}
		for _, line := range layout {
			_, err = add(line.Text, x+float32(line.X)*scale, y+float32(line.Y)*scale, float32(line.Width)*scale, line.Focused)
			if err != nil {
				return err
			}
		}
	}
	for _, row := range rows {
		if strings.TrimSpace(row.text) == "" {
			continue
		}
		height := size * 1.2
		rl.DrawRectangleRec(rl.Rectangle{X: row.x - 4*scale, Y: row.y, Width: row.width + 8*scale, Height: height}, rl.Color{R: 18, G: 18, B: 18, A: 240})
		if row.focused {
			rl.DrawRectangleLinesEx(rl.Rectangle{X: row.x - 4*scale, Y: row.y, Width: row.width + 8*scale, Height: height}, 3*scale, rl.White)
		}
		rl.DrawTextEx(font, row.text, rl.Vector2{X: row.x, Y: row.y}, size, spacing, rl.White)
	}
	return nil
}

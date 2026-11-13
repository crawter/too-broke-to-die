// Copyright (C) 2026 crawter
// All rights reserved.

// Package session defines adapter-independent frame and storage contracts.
package session

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
	"too-broke-to-die/internal/tbtd/simulation"
)

// Service owns one session and its transient world. It is used by one frame
// loop, not concurrently. The store is an asynchronous external boundary.
type Service struct {
	campaign *campaign.Campaign

	manifest *content.Campaign

	bays []content.ServiceBay

	store ProgressStore

	policy *FramePolicy

	world *simulation.World

	committed campaign.Snapshot

	id CampaignID

	revision uint64

	digest string

	serial uint64

	pending *pendingProgress

	save SaveIndicator

	failure ErrorCode

	candidates []RecoveryCandidate

	confirmation string

	confirmationTarget string

	closePending bool

	blocked string

	notice string

	exit bool

	replay bool

	scenes map[string]content.Scene

	scene scenePlayback

	failureHandled bool

	events []SemanticEvent
}

type pendingProgress struct {
	request ProgressRequest

	active ProgressRequest

	inFlight bool

	resolving bool

	discarding bool

	choice RecoveryChoice

	destination AppState

	owner string

	label string

	digest string

	elapsed time.Duration
}

// New constructs a session at the title screen. The caller supplies validated
// authored definitions and retains the store's worker and shutdown lifecycle.
// The definitions must not be modified while the session is in use.
func New(ctx context.Context, owner *campaign.Campaign, manifest *content.Campaign, bays []content.ServiceBay, store ProgressStore) (_ *Service, err error) {
	defer mon.Task()(&ctx)(&err)
	if owner == nil || manifest == nil || store == nil || len(bays) != 96 {
		return nil, fmt.Errorf("%w: missing session dependencies", InvalidAction)
	}
	return &Service{campaign: owner, manifest: manifest, bays: slices.Clone(bays), store: store, policy: NewFramePolicy()}, nil
}

func (service *Service) nextID(ctx context.Context) (_ RequestID, err error) {
	defer mon.Task()(&ctx)(&err)
	if service.serial == math.MaxUint64 {
		return "", fmt.Errorf("%w: request sequence exhausted", Internal)
	}
	service.serial++
	return RequestID(fmt.Sprintf("session-%d", service.serial)), nil
}

// HandleFrame polls storage before applying focus and menu changes, then steps
// at most the policy's permitted tick budget. A required transition interrupts
// the budget immediately and remains private until its acknowledgment.
func (service *Service) HandleFrame(ctx context.Context, command FrameCommand) (_ FrameView, err error) {
	defer mon.Task()(&ctx)(&err)
	if service == nil {
		return FrameView{}, fmt.Errorf("%w: nil session", InvalidAction)
	}
	service.notice = ""
	service.events = nil
	if service.pending != nil && service.pending.inFlight {
		elapsed := command.Data(ctx).Elapsed
		if elapsed > 0 && service.pending.elapsed < 5*time.Second {
			service.pending.elapsed = min(5*time.Second, service.pending.elapsed+min(elapsed, 5*time.Second))
		}
		if err = service.poll(ctx); err != nil {
			return FrameView{}, err
		}
	}
	if len(command.Data(ctx).Selections) > 1 {
		return FrameView{}, fmt.Errorf("%w: one menu selection per frame", InvalidAction)
	}
	if service.confirmation != "" {
		input := command.Data(ctx)
		if service.confirmation != "scene_skip" && service.confirmation != "consent" {
			input.Selections = slices.DeleteFunc(input.Selections, func(selection MenuSelection) bool { return selection.Action == "resume" })
		}
		input.Edges = slices.DeleteFunc(input.Edges, func(edge ActionEdge) bool {
			return edge.Kind == Pressed && (edge.Action == settings.Pause || edge.Action == settings.Ledger)
		})
		command, err = NewFrameCommand(ctx, input)
		if err != nil {
			return FrameView{}, err
		}
	}
	beforeState := service.policy.State()
	confirmHeld := service.policy.input.held[settings.MenuConfirm]
	decision, err := service.policy.BeginFrame(ctx, command)
	if err != nil {
		return FrameView{}, err
	}
	if decision.CloseRequested && service.pending != nil {
		service.closePending = true
	}
	if service.closePending && service.pending == nil {
		decision.CloseRequested = true
		service.closePending = false
	}
	if decision.CloseRequested && service.pending == nil && service.policy.State() == StateActive {
		if err = service.policy.Transition(ctx, OpenPause, ""); err != nil {
			return FrameView{}, err
		}
		service.confirmation = "quit_to_title"
	} else if decision.CloseRequested && service.pending == nil && service.policy.State() == StateScene {
		if err = service.policy.Transition(ctx, OpenPause, ""); err != nil {
			return FrameView{}, err
		}
		service.confirmationTarget = service.confirmation
		service.confirmation = "quit_to_title"
	} else if decision.CloseRequested && service.pending == nil && (service.policy.State() == StatePause || service.policy.State() == StateDead) {
		if service.confirmation != "quit_to_title" {
			service.confirmationTarget = service.confirmation
		}
		service.confirmation = "quit_to_title"
	}
	if !decision.Interrupted {
		for _, selection := range decision.Selections {
			if selection.Action == "resume" {
				continue // handled by FramePolicy before opening a tick budget
			}
			if err = service.selectAction(ctx, selection); err != nil {
				service.policy.game.Interrupt()
				service.policy.scene.Freeze()
				return FrameView{}, err
			}
		}
	}
	if !decision.Interrupted && beforeState == StateScene && decision.State == StateScene && len(decision.Selections) == 0 {
		if err = service.sceneInput(ctx, command.Data(ctx), confirmHeld); err != nil {
			service.policy.scene.Freeze()
			return FrameView{}, err
		}
	} else if beforeState == StateScene && decision.State != StateScene {
		service.scene.pressed, service.scene.hold = false, 0
	}
	if service.confirmation != "" && service.policy.State() == StateScene {
		service.policy.scene.Freeze()
	}
	var stepErr error
	for {
		kind, input, ok := service.policy.NextTick(ctx)
		if !ok {
			break
		}
		if kind == SceneTick {
			service.tickScene()
			continue
		}
		if kind == GameplayTick {
			if err = service.step(ctx, input); err != nil {
				service.policy.game.Interrupt()
				if service.blocked == "" {
					service.blocked = "unsubmitted simulation event"
				}
				if transitionErr := service.policy.Transition(ctx, BeginSave, ""); transitionErr != nil {
					return FrameView{}, fmt.Errorf("step: %w; suspend: %v", err, transitionErr)
				}
				service.fail(ctx, Internal)
				stepErr = err
				break
			}
		}
	}
	view, err := service.view(ctx)
	if err != nil {
		return FrameView{}, err
	}
	return view, stepErr
}

func (service *Service) step(ctx context.Context, input simulation.PlayerCommand) (err error) {
	defer mon.Task()(&ctx)(&err)
	if service.world == nil {
		return fmt.Errorf("%w: no restored world", Internal)
	}
	_, state := service.world.Snapshot(ctx).Data(ctx)
	body := state.Player.Bounds
	combat := simulation.CombatCommand{Player: input, Visible: simulation.Rect{X: body.X - 640, Y: body.Y - 360, Width: 1280, Height: 720}}
	objects := simulation.ObjectCommand{Feces: simulation.FecesCommand{Limb: simulation.LimbCommand{Combat: combat}}}
	boss := service.world.Boss(ctx)
	isBay := false
	for _, bay := range service.bays {
		if bay.Encounter == state.Encounter {
			isBay = true
			break
		}
	}
	switch {
	case boss.Encounter != "":
		err = service.world.StepBoss(ctx, objects)
	case isBay:
		err = service.world.StepEncounter(ctx, simulation.ServiceCommand{Objects: objects})
	default:
		err = service.world.StepObjects(ctx, objects)
	}
	if err != nil {
		return err
	}
	return service.collect(ctx, isBay)
}

func (service *Service) view(ctx context.Context) (_ FrameView, err error) {
	defer mon.Task()(&ctx)(&err)
	data := FrameViewData{State: service.policy.State(), Save: service.save, Error: service.failure, Candidates: slices.Clone(service.candidates), Confirmation: service.confirmation, Notice: service.notice, ExitRequested: service.exit, Events: service.events}
	switch service.confirmation {
	case "return_checkpoint":
		data.Warning = "Незачтённый прогресс попытки будет сброшен. Зачтённые дела и штрафы сохраняются"
	case "quit_to_title":
		data.Warning = "Текущая позиция и незачтённая работа будут сброшены. Зачтённые дела и штрафы сохраняются"
	case "new_game":
		if service.confirmationTarget == "unknown_campaign" {
			data.Warning = "Начать отдельную кампанию? Сохранённый прогресс повреждён или неизвестен; его объём неизвестен."
		} else {
			data.Warning = fmt.Sprintf("Начать новую кампанию? В сохранённой кампании зачтено: %d/100", service.committed.Credits())
		}
	case "reload_saved", "exit_unsaved":
		label := service.blocked
		if service.pending != nil {
			label = service.pending.label
		}
		data.Warning = "Несохранённое событие может быть потеряно: " + label
	case "scene_skip":
		data.Warning = "Пропустить сцену: " + service.scene.definition.ID + "?"
	case "consent":
		data.Warning = "Согласен умереть окончательно. Далее следует канонический финал."
	case "steal_donations":
		data.Warning = "Украсть пожертвования на лечение рака?"
	case "reset_request", "abandon_request":
		data.Warning = "Незачтённый прогресс попытки будет сброшен. Зачтённые дела и штрафы сохраняются"
	}
	if service.pending != nil {
		data.Pending = PendingEvent{Operation: service.pending.request.Data().Operation, Label: service.pending.label}
		data.Slow = service.pending.inFlight && service.pending.elapsed >= 5*time.Second
	} else if service.blocked != "" {
		data.Pending.Label = service.blocked
	}
	if data.State == StateError {
		if service.blocked != "" {
			data.Choices = []RecoveryChoice{ReloadSaved, ExitUnsaved}
		} else if service.pending != nil && service.pending.request.Data().Kind == CommitProgress {
			data.Choices = []RecoveryChoice{RetrySave, ReloadSaved, ExitUnsaved}
		} else if service.recoveryLoadFailed() {
			if len(service.candidates) > 0 {
				data.Choices = append(data.Choices, ChooseOlder)
			}
			data.Choices = append(data.Choices, NewCampaign, ExitGame)
		} else {
			data.Choices = []RecoveryChoice{ExitGame}
		}
	}
	if len(service.scene.definition.Units) != 0 && service.scene.unit < len(service.scene.definition.Units) {
		unit := service.scene.definition.Units[service.scene.unit]
		data.Scene = SceneView{ID: service.scene.definition.ID, Unit: unit.ID, Narrative: service.scene.definition.Narrative, Ending: service.scene.definition.Ending, Ticks: service.scene.ticks, Background: unit.Background, Replay: service.replay}
		if service.replay {
			data.Scene.ReplayLabel = "Повтор финала"
		}
		data.Texts = append(data.Texts, TextView{ID: unit.TextID, Speaker: unit.Speaker, Text: unit.Text, Direction: unit.Direction})
	}
	if service.world != nil {
		previous, current := service.world.Snapshot(ctx).Data(ctx)
		status := service.world.PlayerStatus()
		data.Player = PlayerView{Health: current.Health, Feces: current.Feces, Immune: status.OrdinaryImmune, Limbs: [4]bool{status.Limbs&simulation.LeftArm != 0, status.Limbs&simulation.RightArm != 0, status.Limbs&simulation.LeftLeg != 0, status.Limbs&simulation.RightLeg != 0}}
		if current.HasPlayer {
			data.Entities = append(data.Entities, EntityView{ID: "player", PreviousX: previous.Player.Bounds.X, PreviousY: previous.Player.Bounds.Y, X: current.Player.Bounds.X, Y: current.Player.Bounds.Y})
		}
		for _, entity := range current.Entities {
			data.Entities = append(data.Entities, EntityView{ID: string(entity.ID), X: entity.Position.X, Y: entity.Position.Y, PreviousX: entity.Position.X, PreviousY: entity.Position.Y})
		}
		boss := service.world.Boss(ctx)
		data.Boss = BossView{ID: boss.Encounter, Health: boss.Health, Phase: boss.Phase, Protected: boss.Protected}
	}
	return NewFrameView(ctx, data, service.committed, service.id, service.revision)
}

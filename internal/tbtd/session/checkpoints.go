// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"fmt"
	"math"
	"slices"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/simulation"
)

// checkpointCandidate accepts only the simulation's completed physical hold.
// The campaign validates the anchor and owns the resulting proposal.
func (service *Service) checkpointCandidate(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	_, state := service.world.Snapshot(ctx).Data(ctx)
	for _, event := range service.world.InteractionView(ctx).Events {
		if event.Kind != "completed" {
			continue
		}
		for _, entity := range state.Entities {
			if string(entity.ID) != event.Target || entity.Role != content.RoleCheckpoint {
				continue
			}
			sequence := service.committed.Data(ctx).OperationSequence
			if sequence == math.MaxUint64 {
				return fmt.Errorf("%w: operation sequence exhausted", Internal)
			}
			transition := campaign.Transition{CampaignID: string(service.id), Sequence: sequence + 1, Checkpoint: state.Anchor}
			proposal, prepareErr := service.campaign.Prepare(ctx, service.committed, transition)
			if prepareErr != nil {
				return prepareErr
			}
			if proposal.Data(ctx).OperationSequence != transition.Sequence {
				// The campaign treats an unchanged anchor as a no-op. The
				// physical hold is nevertheless a distinct identified operation;
				// bind it to the same already-validated anchor without changing
				// receipts, history, or fees.
				if service.committed.Data(ctx).Anchor != state.Anchor {
					return fmt.Errorf("%w: checkpoint anchor mismatch", Internal)
				}
				transition.Checkpoint = ""
				proposal, prepareErr = service.campaign.Prepare(ctx, service.committed, transition)
				if prepareErr != nil {
					return prepareErr
				}
			}
			return service.beginCommit(ctx, proposal, OperationID{Event: fmt.Sprintf("%s/%d/checkpoint/%s", service.id, transition.Sequence, state.Anchor), Sequence: transition.Sequence}, "checkpoint", string(state.Anchor), StateActive)
		}
	}
	return nil
}

// pauseAtConsole retains the completed interaction until a ticket is chosen;
// otherwise another tick would erase the simulation's one-tick hold event.
func (service *Service) pauseAtConsole(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	_, state := service.world.Snapshot(ctx).Data(ctx)
	for _, event := range service.world.InteractionView(ctx).Events {
		if event.Kind != "completed" {
			continue
		}
		for _, entity := range state.Entities {
			if string(entity.ID) == event.Target && entity.Protected && entity.Role == content.RoleConsole {
				service.notice = "Выберите запрос"
				return service.policy.Transition(ctx, OpenPause, "")
			}
		}
	}
	return nil
}

func (service *Service) requestService(ctx context.Context, action campaign.AttemptAction, deed content.DeedID) (err error) {
	defer mon.Task()(&ctx)(&err)
	if service.world == nil || service.pending != nil || (service.policy.State() != StateActive && service.policy.State() != StatePause) {
		return fmt.Errorf("%w: request unavailable", InvalidAction)
	}
	if action == campaign.AttemptAccept {
		if len(service.scenes) != 0 {
			state := service.committed.Data(ctx)
			for _, definition := range service.manifest.Deeds(ctx) {
				if definition.Deed.ID != deed {
					continue
				}
				if (definition.Location == "LOC-HOS" && !slices.Contains(state.Narratives, content.NarrativeID("NAR-04"))) || (definition.Location == "LOC-INS" && !slices.Contains(state.Narratives, content.NarrativeID("NAR-08"))) {
					return fmt.Errorf("%w: arrival scene required", campaign.ErrBlocked)
				}
			}
		}
		if attempt, ok := service.committed.ActiveAttempt(); ok && attempt.Deed == deed {
			return nil
		}
	}
	receipt, err := service.world.RequestService(ctx, service.campaign, service.manifest, service.committed, action, deed)
	if err != nil || receipt != nil {
		return err
	}
	sequence := service.committed.Data(ctx).OperationSequence
	if sequence == math.MaxUint64 {
		return fmt.Errorf("%w: operation sequence exhausted", Internal)
	}
	transition := campaign.Transition{CampaignID: string(service.id), Sequence: sequence + 1}
	proposal, err := service.world.PrepareService(ctx, service.campaign, service.committed, transition)
	if err != nil {
		return err
	}
	identity := OperationID{Event: fmt.Sprintf("%s/%d/%s/%s", service.id, transition.Sequence, action, deed), Sequence: transition.Sequence}
	return service.beginCommit(ctx, proposal, identity, "service", string(deed), service.policy.State())
}

func (service *Service) selectAction(ctx context.Context, selection MenuSelection) (err error) {
	defer mon.Task()(&ctx)(&err)
	state := service.policy.State()
	if service.confirmation != "" {
		switch selection.Action {
		case "cancel":
			action, target := service.confirmation, service.confirmationTarget
			service.confirmation, service.confirmationTarget = "", ""
			if action == "quit_to_title" && (target == "consent" || target == "scene_skip") {
				service.confirmation = target
				return nil
			}
			if action == "consent" {
				if state == StatePause {
					service.policy.states.pauseReturn = StateActive
					return nil
				}
				return service.policy.Transition(ctx, FinishScene, "")
			}
			return nil
		case "confirm":
			if (service.confirmation == "scene_skip" || service.confirmation == "consent") && state != StateScene {
				return nil // Resume the interrupted scene before confirming it.
			}
			return service.confirmAction(ctx)
		default:
			return fmt.Errorf("%w: confirmation required", InvalidAction)
		}
	}
	if state == StateSaving || state == StateRestoring || (state == StateTitle && service.pending != nil) {
		// Storage and restoration keep processing window events but do not
		// accept a second gameplay or storage operation.
		return nil
	}
	switch selection.Action {
	case "continue":
		if err = service.policy.Transition(ctx, ContinueGame, ""); err != nil {
			return err
		}
		return service.startLoad(ctx, LoadActive, "", 0)
	case "new_game":
		if state == StateError {
			if !service.recoveryLoadFailed() {
				return fmt.Errorf("%w: unresolved operation", InvalidAction)
			}
			service.confirmation, service.confirmationTarget = "new_game", "unknown_campaign"
			return nil
		}
		if state != StateTitle && state != StateEnd {
			return fmt.Errorf("%w: new game", InvalidAction)
		}
		if state == StateTitle && service.id == "" {
			return service.lookupNewGame(ctx)
		}
		service.confirmation = "new_game"
		return nil
	case "return_checkpoint":
		if state != StatePause {
			return fmt.Errorf("%w: checkpoint return", InvalidAction)
		}
		service.confirmation = "return_checkpoint"
		return nil
	case "quit_to_title":
		if state != StatePause && state != StateDead && state != StateEnd {
			return fmt.Errorf("%w: quit", InvalidAction)
		}
		service.confirmation = "quit_to_title"
		return nil
	case "accept_request":
		return service.requestService(ctx, campaign.AttemptAccept, content.DeedID(selection.Target))
	case "reset_request", "abandon_request":
		if (state != StateActive && state != StatePause) || service.world == nil {
			return fmt.Errorf("%w: request change", InvalidAction)
		}
		if state == StateActive {
			if err = service.policy.Transition(ctx, OpenPause, ""); err != nil {
				return err
			}
		}
		service.confirmation, service.confirmationTarget = string(selection.Action), selection.Target
		return nil
	case "retry_death":
		return service.restore(ctx, RetryDeath)
	case "retry_save":
		return service.recoverCommit(ctx, RetrySave)
	case "reload_saved", "exit_unsaved":
		if state != StateError || (service.blocked == "" && (service.pending == nil || service.pending.request.Data().Kind != CommitProgress)) {
			return fmt.Errorf("%w: no failed write", InvalidAction)
		}
		service.confirmation = string(selection.Action)
		return nil
	case "choose_older":
		return service.selectOlder(ctx, selection.Target)
	case "exit":
		if state != StateError || service.blocked != "" || (service.pending != nil && service.pending.request.Data().Kind == CommitProgress) {
			return fmt.Errorf("%w: exit", InvalidAction)
		}
		service.exit = true
		return nil
	case "replay_ending":
		if _, err = service.campaign.ReplayEnding(ctx, service.committed); err != nil {
			return err
		}
		for _, stage := range []campaign.EndingStage{campaign.EndingDeath, campaign.EndingHell, campaign.EndingCeremony, campaign.EndingTableau} {
			if scene, ok := service.scenes[string(stage)]; !ok || len(scene.Units) == 0 {
				return fmt.Errorf("%w: missing replay scene %s", ContentInvalid, stage)
			}
		}
		if err = service.policy.Transition(ctx, ReplayEnding, ""); err != nil {
			return err
		}
		service.replay = true
		return service.loadScene(ctx, string(campaign.EndingDeath), "")
	case "consent":
		if state != StateActive || !service.committed.Data(ctx).Permit || service.replay || service.world == nil || service.committed.Data(ctx).Anchor != "ANC-MIN-PERMIT" {
			return fmt.Errorf("%w: consent unavailable", InvalidAction)
		}
		target := service.world.InteractionView(ctx)
		if !target.Found || target.Target.ID != simulation.PermitReviewTarget {
			return fmt.Errorf("%w: consent requires the permit counter", InvalidAction)
		}
		if err = service.policy.Transition(ctx, StartScene, ""); err != nil {
			return err
		}
		service.scene = scenePlayback{}
		service.confirmation = "consent"
		return nil
	case "scene_advance":
		return service.advanceScene(ctx)
	case "exit_replay":
		if !service.replay {
			return fmt.Errorf("%w: replay is not active", InvalidAction)
		}
		service.replay = false
		service.scene = scenePlayback{}
		return service.policy.Transition(ctx, EndReplay, "")
	default:
		return fmt.Errorf("%w: menu action %s", InvalidAction, selection.Action)
	}
}

func (service *Service) confirmAction(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	action, target := service.confirmation, service.confirmationTarget
	service.confirmation, service.confirmationTarget = "", ""
	switch action {
	case "steal_donations":
		return service.commitStory(ctx, campaign.StoryChange{Action: campaign.StoryTheft}, StatePause, "donation theft")
	case "scene_skip":
		return service.finishCurrentScene(ctx)
	case "consent":
		return service.commitStory(ctx, campaign.StoryChange{Action: campaign.StoryConsent}, StateScene, "consent")
	case "new_game":
		if service.policy.State() == StateError {
			if target != "unknown_campaign" || !service.recoveryLoadFailed() {
				return fmt.Errorf("%w: unresolved operation", InvalidAction)
			}
			if err = service.policy.Transition(ctx, ExitWithoutSave, ""); err != nil {
				return err
			}
		} else if (service.policy.State() != StateTitle && service.policy.State() != StateEnd) || service.pending != nil || service.id == "" {
			return fmt.Errorf("%w: campaign metadata unavailable", InvalidAction)
		}
		if err = service.policy.Transition(ctx, NewGame, ""); err != nil {
			return err
		}
		return service.startLoad(ctx, CreateCampaign, createPlaceholder, 0)
	case "reset_request":
		return service.requestService(ctx, campaign.AttemptReset, content.DeedID(target))
	case "abandon_request":
		return service.requestService(ctx, campaign.AttemptAbandon, content.DeedID(target))
	case "return_checkpoint":
		service.notice = "Uncertified attempt work resets; certified services and penalties remain."
		return service.restore(ctx, ReturnCheckpoint)
	case "quit_to_title":
		service.notice = "Live position and uncertified work reset; certified services and penalties remain."
		service.scene = scenePlayback{}
		service.replay = false
		return service.policy.Transition(ctx, ReturnTitle, "")
	case "reload_saved", "exit_unsaved":
		if service.blocked != "" {
			service.notice = "Discarded: " + service.blocked
			service.blocked, service.failure, service.save = "", "", SaveNone
			if action == "reload_saved" {
				return service.restore(ctx, ReloadProgress)
			}
			service.exit = true
			return service.policy.Transition(ctx, ExitWithoutSave, "")
		}
		if action == "reload_saved" {
			return service.recoverCommit(ctx, ReloadSaved)
		}
		return service.recoverCommit(ctx, ExitUnsaved)
	default:
		return fmt.Errorf("%w: confirmation", InvalidAction)
	}
}

// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"fmt"
	"strconv"

	"too-broke-to-die/internal/tbtd/simulation"
)

// The profile store allocates the actual UUID. Its create request contract
// requires a syntactically valid placeholder but never installs that identity.
const createPlaceholder CampaignID = "00000000-0000-4000-8000-000000000000"

func (service *Service) startLoad(ctx context.Context, kind ProgressKind, id CampaignID, revision uint64) (err error) {
	defer mon.Task()(&ctx)(&err)
	requestID, err := service.nextID(ctx)
	if err != nil {
		return err
	}
	request, err := NewProgressRequest(ctx, ProgressInput{ID: requestID, Kind: kind, Campaign: id, ExpectedRevision: revision})
	if err != nil {
		return err
	}
	service.pending = &pendingProgress{request: request, label: string(kind)}
	service.save, service.failure = SavePending, ""
	service.submit(ctx, request, false)
	return nil
}

// lookupNewGame reads validated profile metadata before offering replacement.
// It does not construct a world or change the title state.
func (service *Service) lookupNewGame(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	id, err := service.nextID(ctx)
	if err != nil {
		return err
	}
	request, err := NewProgressRequest(ctx, ProgressInput{ID: id, Kind: LoadActive})
	if err != nil {
		return err
	}
	service.pending = &pendingProgress{request: request, label: "new_game_lookup"}
	service.save, service.failure = SavePending, ""
	service.submit(ctx, request, false)
	return nil
}

func (service *Service) loadResult(ctx context.Context, data ProgressResultData) (err error) {
	defer mon.Task()(&ctx)(&err)
	pending := service.pending
	input := pending.request.Data()
	if data.Status != StorageSucceeded {
		service.candidates = data.Candidates
		service.fail(ctx, resultCode(data))
		return nil
	}
	if !campaignIDPattern.MatchString(string(data.Campaign)) || !data.RevisionKnown || !validPayloadDigest(data.PayloadDigest) || data.Operation != (OperationID{}) || (input.Kind == LoadRevision && (data.Campaign != input.Campaign || data.Revision != input.ExpectedRevision)) || (input.Kind == CreateCampaign && data.Campaign == service.id && service.id != "") {
		service.fail(ctx, StorageConflict)
		return nil
	}
	state := data.Snapshot.Data(ctx)
	if state.CampaignID != "" && state.CampaignID != string(data.Campaign) {
		service.fail(ctx, StorageConflict)
		return nil
	}
	if err = service.campaign.Validate(ctx, data.Snapshot); err != nil {
		service.fail(ctx, SaveCorrupt)
		return nil
	}
	if input.Kind == LoadRevision {
		plan, planErr := service.campaign.RestorePlan(ctx, data.Snapshot)
		found := false
		if planErr == nil {
			for _, candidate := range service.candidates {
				if candidate.Campaign == data.Campaign && candidate.Revision == data.Revision && candidate.Anchor == plan.Anchor && candidate.Credits == data.Snapshot.Credits() {
					found = true
				}
			}
		}
		if !found {
			service.fail(ctx, StorageConflict)
			return nil
		}
	}
	if pending.label == "new_game_lookup" {
		service.committed, service.id, service.revision, service.digest = data.Snapshot, data.Campaign, data.Revision, data.PayloadDigest
		service.pending = nil
		service.save, service.failure = SaveNone, ""
		service.confirmation = "new_game"
		return nil
	}
	// Build first: a failed location load must not publish the candidate.
	world, err := simulation.NewWorld(ctx, service.campaign, service.manifest, data.Snapshot, service.bays, nil)
	if err != nil {
		service.fail(ctx, ContentInvalid)
		return nil
	}
	service.committed, service.id, service.revision, service.digest = data.Snapshot, data.Campaign, data.Revision, data.PayloadDigest
	service.world = world
	if err = service.policy.BindWorld(ctx, world); err != nil {
		return err
	}
	service.pending = nil
	service.failure, service.save = "", SaveConfirmed
	service.candidates = nil
	service.failureHandled, service.replay = false, false
	if service.policy.State() == StateSaving {
		if err = service.policy.Transition(ctx, SaveSuccess, StateRestoring); err != nil {
			return err
		}
	}
	return service.finishRestore(ctx)
}

func (service *Service) restore(ctx context.Context, event StateEvent) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = service.policy.Transition(ctx, event, ""); err != nil {
		return err
	}
	return service.finishRestore(ctx)
}

func (service *Service) finishRestore(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	restored, err := service.campaign.ClearWork(ctx, service.committed)
	if err != nil {
		service.fail(ctx, SaveCorrupt)
		return nil
	}
	world, err := simulation.NewWorld(ctx, service.campaign, service.manifest, restored, service.bays, nil)
	if err != nil {
		service.fail(ctx, ContentInvalid)
		return nil
	}
	plan, err := service.campaign.RestorePlan(ctx, restored)
	if err != nil {
		service.fail(ctx, SaveCorrupt)
		return nil
	}
	if err = service.policy.BindWorld(ctx, world); err != nil {
		return err
	}
	service.world, service.committed = world, restored
	service.failureHandled, service.replay = false, false
	switch AppState(plan.State) {
	case StatePause:
		return service.policy.Transition(ctx, RestoreActive, "")
	case StateScene:
		if len(service.scenes) != 0 {
			if err = service.loadScene(ctx, plan.SceneEntry, restored.Data(ctx).SceneUnit); err != nil {
				service.fail(ctx, ContentInvalid)
				return nil
			}
		}
		return service.policy.Transition(ctx, RestoreScene, "")
	case StateEnd:
		return service.policy.Transition(ctx, RestoreCompleted, "")
	default:
		return fmt.Errorf("%w: invalid restoration destination", ContentInvalid)
	}
}

func (service *Service) recoverCommit(ctx context.Context, choice RecoveryChoice) (err error) {
	defer mon.Task()(&ctx)(&err)
	if service.pending == nil || service.pending.request.Data().Kind != CommitProgress || service.policy.State() != StateError {
		return fmt.Errorf("%w: no failed commit", InvalidAction)
	}
	if err = service.policy.Transition(ctx, RetryOperation, ""); err != nil {
		return err
	}
	pending := service.pending
	pending.choice, pending.resolving = choice, true
	resolution, err := pending.request.Resolution()
	if err != nil {
		return err
	}
	service.save, service.failure = SavePending, ""
	service.submit(ctx, resolution, false)
	return nil
}

func (service *Service) notInstalled(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	pending := service.pending
	if pending.discarding {
		service.notice = "Discarded: " + pending.label
		service.pending = nil
		service.save, service.failure = SaveNone, ""
		if err = service.policy.Transition(ctx, SaveFailure, ""); err != nil {
			return err
		}
		if err = service.policy.Transition(ctx, ExitWithoutSave, ""); err != nil {
			return err
		}
		service.exit = true
		return nil
	}
	pending.resolving = false
	switch pending.choice {
	case RetrySave:
		service.submit(ctx, pending.request, false)
	case ReloadSaved:
		id, idErr := service.nextID(ctx)
		if idErr != nil {
			return idErr
		}
		load, loadErr := NewProgressRequest(ctx, ProgressInput{ID: id, Kind: LoadActive})
		if loadErr != nil {
			return loadErr
		}
		service.submit(ctx, load, false)
	case ExitUnsaved:
		resolution, resolveErr := pending.request.Resolution()
		if resolveErr != nil {
			return resolveErr
		}
		service.submit(ctx, resolution, true)
	default:
		service.fail(ctx, StorageConflict)
	}
	return nil
}

func (service *Service) recoveryLoadFailed() bool {
	if service.policy.State() != StateError || service.failure != SaveCorrupt || service.blocked != "" || service.pending == nil || service.pending.inFlight {
		return false
	}
	switch service.pending.request.Data().Kind {
	case LoadActive, LoadCampaign, LoadRevision:
		return true
	default:
		return false
	}
}

func (service *Service) selectOlder(ctx context.Context, target string) (err error) {
	defer mon.Task()(&ctx)(&err)
	if !service.recoveryLoadFailed() {
		return fmt.Errorf("%w: no older recovery", InvalidAction)
	}
	for _, candidate := range service.candidates {
		key := string(candidate.Campaign) + ":" + strconv.FormatUint(candidate.Revision, 10)
		if key == target {
			if err = service.policy.Transition(ctx, ReloadProgress, ""); err != nil {
				return err
			}
			return service.startLoad(ctx, LoadRevision, candidate.Campaign, candidate.Revision)
		}
	}
	return fmt.Errorf("%w: unknown recovery candidate", InvalidAction)
}

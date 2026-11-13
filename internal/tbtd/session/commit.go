// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"fmt"
	"math"
	"reflect"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/simulation"
)

// collect examines the just-completed tick before another can run. Simulation
// owns the evidence and prepares the proposal; the session never invents it.
func (service *Service) collect(ctx context.Context, isBay bool) (err error) {
	defer mon.Task()(&ctx)(&err)
	view := service.world.ServiceView(ctx)
	boss := service.world.Boss(ctx)
	emergency := service.world.EmergencyView(ctx)
	waste := service.world.FecesView(ctx).PendingWaste
	servicePending := view.Status == simulation.ServicePending || (view.Status == simulation.ServiceFailed && !service.failureHandled)
	bossPending := boss.Pending != nil
	emergencyPending := emergency.Candidate != ""
	dead := service.world.PlayerStatus().Dead
	owners := 0
	for _, present := range []bool{servicePending, bossPending, emergencyPending} {
		if present {
			owners++
		}
	}
	// Existing simulation preparation contracts cannot acknowledge several
	// independent owners atomically. Never commit only a subset of a tick.
	if owners > 1 || (waste != nil && (bossPending || emergencyPending || !isBay)) {
		service.blocked = fmt.Sprintf("unsubmitted: service=%s boss=%t emergency=%s waste=%t death=%t", view.Deed, bossPending, emergency.Candidate, waste != nil, dead)
		return fmt.Errorf("%w: simultaneous simulation candidates require atomic owner acknowledgment", Internal)
	}
	if !servicePending && !bossPending && !emergencyPending && waste == nil && !dead {
		if err = service.checkpointCandidate(ctx); err != nil || service.policy.State() != StateActive {
			return err
		}
		return service.pauseAtConsole(ctx)
	}
	state := service.committed.Data(ctx)
	if state.OperationSequence == math.MaxUint64 {
		return fmt.Errorf("%w: operation sequence exhausted", Internal)
	}
	transition := campaign.Transition{CampaignID: string(service.id), Sequence: state.OperationSequence + 1}
	if dead {
		transition.Death, transition.DeathDetail = campaign.DeathGameplay, "gameplay"
	}
	owner, label := "death", "temporary death"
	var proposal campaign.Snapshot
	switch {
	case servicePending:
		owner, label = "service", string(view.Deed)
		if view.Status == simulation.ServiceFailed {
			label = "failed: " + string(view.Deed)
		}
		proposal, err = service.world.PrepareService(ctx, service.campaign, service.committed, transition)
	case bossPending:
		owner, label = "boss", string(boss.Pending.Encounter)
		proposal, err = service.world.PrepareBoss(ctx, service.campaign, service.committed, transition)
	case emergencyPending:
		owner, label = "emergency", emergency.EventID
		proposal, err = service.world.PrepareEmergency(ctx, service.campaign, service.committed, transition)
	case waste != nil:
		owner, label = "service", "waste: "+string(waste.Attempt.Deed)
		proposal, err = service.world.PrepareServiceWaste(ctx, service.campaign, service.committed, transition)
	default:
		proposal, err = service.campaign.Prepare(ctx, service.committed, transition)
	}
	if err != nil {
		return err
	}
	event := candidateIdentity(service.id, transition.Sequence, owner, label, view.Status, waste != nil && !servicePending)
	destination := StateActive
	if dead {
		destination = StateDead
	}
	return service.beginCommit(ctx, proposal, OperationID{Event: event, Sequence: transition.Sequence}, owner, label, destination)
}

func candidateIdentity(id CampaignID, sequence uint64, owner, label string, status simulation.ServiceStatus, waste bool) string {
	if owner == "death" || owner == "boss" || waste || (owner == "service" && status == simulation.ServiceFailed) {
		return fmt.Sprintf("%s/%d/%s", id, sequence, label)
	}
	return label
}

func (service *Service) beginCommit(ctx context.Context, proposal campaign.Snapshot, operation OperationID, owner, label string, destination AppState) (err error) {
	defer mon.Task()(&ctx)(&err)
	if service.pending != nil || service.id == "" || service.revision == math.MaxUint64 || !validPayloadDigest(service.digest) {
		return fmt.Errorf("%w: no writable acknowledged revision", StorageConflict)
	}
	if err = service.campaign.Validate(ctx, proposal); err != nil {
		return err
	}
	id, err := service.nextID(ctx)
	if err != nil {
		return err
	}
	request, err := NewProgressRequest(ctx, ProgressInput{ID: id, Kind: CommitProgress, Campaign: service.id, ExpectedRevision: service.revision, ExpectedDigest: service.digest, Operation: operation, Proposal: proposal})
	if err != nil {
		return err
	}
	if err = service.policy.Transition(ctx, BeginSave, ""); err != nil {
		return err
	}
	service.pending = &pendingProgress{request: request, owner: owner, label: label, destination: destination}
	service.save, service.failure = SavePending, ""
	service.submit(ctx, request, false)
	return nil
}

func (service *Service) submit(ctx context.Context, request ProgressRequest, discard bool) {
	defer mon.Task()(&ctx)(nil)
	pending := service.pending
	pending.active = request
	var err error
	if discard {
		boundary, ok := service.store.(ProgressDiscarder)
		if !ok {
			err = fmt.Errorf("%w: store cannot confirm discard", StorageConflict)
		} else {
			err = boundary.DiscardProgress(ctx, request)
		}
	} else {
		err = service.store.Submit(ctx, request)
	}
	if err != nil {
		pending.inFlight = false
		service.fail(ctx, StorageDenied)
		return
	}
	pending.inFlight, pending.discarding = true, discard
	pending.elapsed = 0
}

func (service *Service) fail(ctx context.Context, code ErrorCode) {
	defer mon.Task()(&ctx)(nil)
	if code == "" {
		code = StorageDenied
	}
	service.failure, service.save = code, SaveError
	if service.policy.State() == StateTitle && service.pending != nil && service.pending.label == "new_game_lookup" {
		if err := service.policy.Transition(ctx, ContinueGame, ""); err != nil {
			service.failure = Internal
			return
		}
	}
	if service.policy.State() == StateSaving || service.policy.State() == StateRestoring {
		if err := service.policy.Transition(ctx, SaveFailure, ""); err != nil {
			service.failure = Internal
		}
	}
}

func (service *Service) poll(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	pending := service.pending
	active := pending.active.Data()
	result, pollErr := service.store.Poll(ctx, active.ID)
	if pollErr != nil {
		pending.inFlight = false
		service.fail(ctx, StorageDenied)
		return nil
	}
	data := result.Data(ctx)
	if data.Status == StoragePending && data.ID == active.ID {
		return nil
	}
	pending.inFlight = false
	if data.ID != active.ID {
		service.fail(ctx, StorageConflict)
		return nil
	}
	original := pending.request.Data()
	if original.Kind == CommitProgress {
		return service.commitResult(ctx, data)
	}
	return service.loadResult(ctx, data)
}

func validPayloadDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, digit := range value {
		if (digit < '0' || digit > '9') && (digit < 'a' || digit > 'f') {
			return false
		}
	}
	return true
}

func (service *Service) commitResult(ctx context.Context, data ProgressResultData) (err error) {
	defer mon.Task()(&ctx)(&err)
	pending := service.pending
	input := pending.request.Data()
	// A confirmed reload uses LoadActive; it may report a newly installed
	// original operation rather than the old revision.
	loading := pending.active.Data().Kind == LoadActive
	if data.Status == StorageSucceeded && loading && data.Operation == (OperationID{}) {
		previous, normalizeErr := service.campaign.Restore(ctx, service.committed.Data(ctx))
		if normalizeErr != nil || data.Campaign != input.Campaign || !data.RevisionKnown || data.Revision != input.ExpectedRevision || data.PayloadDigest != input.ExpectedDigest || service.campaign.Validate(ctx, data.Snapshot) != nil || !reflect.DeepEqual(data.Snapshot.Data(ctx), previous.Data(ctx)) {
			service.fail(ctx, StorageConflict)
			return nil
		}
		service.pending = nil
		service.notice = "Discarded: " + pending.label
		service.save, service.failure = SaveNone, ""
		if err = service.policy.Transition(ctx, SaveFailure, ""); err != nil {
			return err
		}
		return service.restore(ctx, ReloadProgress)
	}
	if data.Campaign != input.Campaign || data.Operation != input.Operation {
		service.fail(ctx, StorageConflict)
		return nil
	}
	if data.PayloadDigest != "" {
		if !validPayloadDigest(data.PayloadDigest) || (pending.digest != "" && pending.digest != data.PayloadDigest) {
			service.fail(ctx, StorageConflict)
			return nil
		}
		pending.digest = data.PayloadDigest
	}
	switch data.Status {
	case StorageSucceeded:
		normalized, normalizeErr := service.campaign.Restore(ctx, input.Proposal.Data(ctx))
		if normalizeErr != nil || !data.RevisionKnown || data.Revision != input.ExpectedRevision+1 || !validPayloadDigest(data.PayloadDigest) || service.campaign.Validate(ctx, data.Snapshot) != nil || !reflect.DeepEqual(normalized.Data(ctx), data.Snapshot.Data(ctx)) {
			service.fail(ctx, StorageConflict)
			return nil
		}
		if err = service.acknowledge(ctx, data); err != nil {
			service.fail(ctx, Internal)
			return err
		}
		return nil
	case StorageNotInstalled:
		if !pending.resolving && !pending.discarding {
			service.fail(ctx, StorageConflict)
			return nil
		}
		return service.notInstalled(ctx)
	default:
		service.fail(ctx, resultCode(data))
		return nil
	}
}

func resultCode(data ProgressResultData) ErrorCode {
	switch data.Status {
	case StorageConflicting:
		return StorageConflict
	case StorageCorrupt:
		return SaveCorrupt
	case StorageTooNew:
		return SaveTooNew
	case StorageIncompatible:
		return ContentIncompatible
	default:
		return data.Code
	}
}

func (service *Service) acknowledge(ctx context.Context, data ProgressResultData) (err error) {
	defer mon.Task()(&ctx)(&err)
	pending := service.pending
	input := pending.request.Data()
	ack := simulation.ServiceAcknowledgment{Succeeded: true, CampaignID: string(service.id), Sequence: input.Operation.Sequence, Snapshot: input.Proposal}
	switch pending.owner {
	case "service":
		if err = service.world.AcknowledgeEncounterService(ctx, service.campaign, service.manifest, service.bays, ack); err != nil {
			return err
		}
		service.failureHandled = service.world.ServiceView(ctx).Status == simulation.ServiceFailed
	case "boss":
		if err = service.world.AcknowledgeBoss(ctx, service.campaign, ack); err != nil {
			return err
		}
	case "emergency":
		if err = service.world.AcknowledgeEmergency(ctx, service.campaign, ack); err != nil {
			return err
		}
	}
	service.committed, service.revision, service.digest = input.Proposal, data.Revision, data.PayloadDigest
	service.events = []SemanticEvent{{Kind: "saved", Durable: true, Revision: data.Revision, Operation: input.Operation}}
	service.pending = nil
	service.failure, service.save = "", SaveConfirmed
	if service.policy.State() == StateError {
		if err = service.policy.Transition(ctx, RetryOperation, ""); err != nil {
			return err
		}
	}
	if err = service.policy.Transition(ctx, SaveSuccess, pending.destination); err != nil {
		return err
	}
	if pending.choice != "" && pending.choice != RetrySave {
		service.notice = "Already saved: " + pending.label
	}
	if pending.owner == "checkpoint" {
		if service.policy.State() == StateActive {
			if err = service.policy.Transition(ctx, OpenPause, ""); err != nil {
				return err
			}
		}
		return service.restore(ctx, ReturnCheckpoint)
	}
	return nil
}

// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"reflect"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/settings"
	"too-broke-to-die/internal/tbtd/simulation"
)

func failedCommit(t *testing.T) (*Service, *recordingStore, campaign.Snapshot, campaign.Snapshot, ProgressInput) {
	t.Helper()
	ctx := context.Background()
	service, store := sessionFixture(t)
	committed, proposal := preparedCertification(t)
	loadFixture(t, service, store, committed)
	operation := OperationID{Event: "D-HOS-SUP-01", Sequence: 1}
	if err := service.beginCommit(ctx, proposal, operation, "test", "D-HOS-SUP-01", StateActive); err != nil {
		t.Fatal(err)
	}
	request := store.last()
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: operation, Status: StorageAmbiguous, Code: StorageFull, PayloadDigest: newDigest})
	if view := frame(t, service, FrameInput{}); view.State != StateError || view.Progress.Credits != 0 || view.Error != StorageFull {
		t.Fatalf("failure: %+v", view)
	}
	return service, store, committed, proposal, request
}

func TestRecoveryReloadAndExitRequireResolutionAndConfirmation(t *testing.T) {
	for _, choice := range []string{"reload_saved", "exit_unsaved"} {
		t.Run(choice, func(t *testing.T) {
			service, store, committed, _, original := failedCommit(t)
			view := choose(t, service, MenuAction(choice), "")
			if view.Confirmation != choice || view.Pending.Label != "D-HOS-SUP-01" || len(store.requests) != 2 {
				t.Fatalf("missing discard confirmation: %+v", view)
			}
			choose(t, service, "confirm", "")
			resolution := store.last()
			if resolution.Kind != ResolveProgress || resolution.ID != original.ID || resolution.Operation != original.Operation || resolution.ExpectedDigest != original.ExpectedDigest || !reflect.DeepEqual(resolution.Proposal.Data(context.Background()), original.Proposal.Data(context.Background())) {
				t.Fatal("recovery changed prepared operation")
			}
			store.reply(ProgressResultData{ID: resolution.ID, Campaign: testID, Operation: original.Operation, Status: StorageNotInstalled, PayloadDigest: newDigest})
			if got := frame(t, service, FrameInput{}); got.State != StateSaving || got.Progress.Credits != 0 {
				t.Fatalf("resolution: %+v", got)
			}
			if choice == "reload_saved" {
				load := store.last()
				if load.Kind != LoadActive || store.discards != 0 {
					t.Fatal("reload did not revalidate the active revision")
				}
				store.reply(ProgressResultData{ID: load.ID, Campaign: testID, Status: StorageSucceeded, Revision: 7, RevisionKnown: true, PayloadDigest: oldDigest, Snapshot: committed})
				view = frame(t, service, FrameInput{})
				if view.State != StatePause || view.Progress.Credits != 0 || view.Notice != "Discarded: D-HOS-SUP-01" || view.Player.Health != 100 {
					t.Fatalf("reload: %+v", view)
				}
			} else {
				if store.discards != 1 || store.last().Kind != ResolveProgress {
					t.Fatal("exit did not recheck discard")
				}
				store.reply(ProgressResultData{ID: original.ID, Campaign: testID, Operation: original.Operation, Status: StorageNotInstalled, PayloadDigest: newDigest})
				view = frame(t, service, FrameInput{})
				if view.State != StateTitle || !view.ExitRequested || view.Progress.Credits != 0 || view.Notice != "Discarded: D-HOS-SUP-01" {
					t.Fatalf("exit: %+v", view)
				}
			}
		})
	}
}

func TestRecoveryReportsInstalledRatherThanDiscarding(t *testing.T) {
	service, store, _, proposal, original := failedCommit(t)
	choose(t, service, "exit_unsaved", "")
	choose(t, service, "confirm", "")
	store.reply(ProgressResultData{ID: original.ID, Campaign: testID, Operation: original.Operation, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: proposal})
	view := frame(t, service, FrameInput{Elapsed: time.Millisecond})
	if view.ExitRequested || view.Progress.Credits != 1 || view.Notice != "Already saved: D-HOS-SUP-01" || store.discards != 0 {
		t.Fatalf("installed result was misreported: %+v", view)
	}
}

func TestResolutionRejectsChangedDigest(t *testing.T) {
	service, store, _, proposal, original := failedCommit(t)
	choose(t, service, "retry_save", "")
	store.reply(ProgressResultData{ID: original.ID, Campaign: testID, Operation: original.Operation, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: oldDigest, Snapshot: proposal})
	view := frame(t, service, FrameInput{})
	if view.State != StateError || view.Error != StorageConflict || view.Progress.Credits != 0 || len(view.Events) != 0 {
		t.Fatalf("resolution accepted a different digest: %+v", view)
	}
}

func TestRetryReusesIdentityAfterNotInstalled(t *testing.T) {
	service, store, _, proposal, original := failedCommit(t)
	choose(t, service, "retry_save", "")
	store.reply(ProgressResultData{ID: original.ID, Campaign: testID, Operation: original.Operation, Status: StorageNotInstalled, PayloadDigest: newDigest})
	frame(t, service, FrameInput{})
	retry := store.last()
	if retry.Kind != CommitProgress || retry.ID != original.ID || retry.Operation != original.Operation || retry.ExpectedRevision != original.ExpectedRevision || retry.ExpectedDigest != original.ExpectedDigest || !reflect.DeepEqual(retry.Proposal.Data(context.Background()), original.Proposal.Data(context.Background())) {
		t.Fatal("retry did not reuse the original payload")
	}
	store.reply(ProgressResultData{ID: retry.ID, Campaign: testID, Operation: original.Operation, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: proposal})
	if view := frame(t, service, FrameInput{}); view.Progress.Credits != 1 || view.State != StateActive {
		t.Fatalf("retry: %+v", view)
	}
}

func TestOlderRecoveryAndIncompatibleSaves(t *testing.T) {
	for _, status := range []StorageStatus{StorageCorrupt, StorageTooNew, StorageIncompatible} {
		t.Run(string(status), func(t *testing.T) {
			service, store := sessionFixture(t)
			choose(t, service, "continue", "")
			request := store.last()
			candidate := RecoveryCandidate{Campaign: testID, Revision: 0, Anchor: "ANC-PRO-01", UnknownDifference: true}
			store.reply(ProgressResultData{ID: request.ID, Status: status, Candidates: []RecoveryCandidate{candidate}})
			view := frame(t, service, FrameInput{})
			if view.State != StateError || view.Progress.Credits != 0 {
				t.Fatalf("bad load: %+v", view)
			}
			if status != StorageCorrupt {
				if len(view.Choices) != 1 || view.Choices[0] != ExitGame || view.Error == SaveCorrupt {
					t.Fatalf("incompatible classified as corruption: %+v", view)
				}
				return
			}
			if len(view.Candidates) != 1 || !view.Candidates[0].UnknownDifference || view.Candidates[0].Anchor != "ANC-PRO-01" {
				t.Fatalf("missing recovery details: %+v", view)
			}
			choose(t, service, "choose_older", string(testID)+":0")
			load := store.last()
			if load.Kind != LoadRevision || load.ExpectedRevision != 0 || load.Campaign != testID {
				t.Fatalf("wrong recovery request: %+v", load)
			}
			store.reply(ProgressResultData{ID: load.ID, Campaign: testID, Status: StorageSucceeded, Revision: 0, RevisionKnown: true, PayloadDigest: oldDigest, Snapshot: campaign.Snapshot{}})
			if view = frame(t, service, FrameInput{}); view.State != StatePause || view.Progress.Credits != 0 {
				t.Fatalf("older revision: %+v", view)
			}
		})
	}
}

func TestConsoleHoldStopsTheFrameBeforeRequestReset(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	committed, _ := preparedCertification(t)
	loadFixture(t, service, store, committed)
	for count := 0; count < 40; count++ {
		_, current := service.world.Snapshot(ctx).Data(ctx)
		if current.Player.Bounds.X >= 150 {
			break
		}
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight, X: 1}}})
	}
	frame(t, service, FrameInput{Elapsed: 100 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight}}})
	for count := 0; count < 20; count++ {
		_, current := service.world.Snapshot(ctx).Data(ctx)
		if current.Player.Velocity.X == 0 {
			break
		}
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	interaction := service.world.InteractionView(ctx)
	if !interaction.Found || interaction.Target.ID != "station-ENC-HOS-01-CONSOLE" {
		t.Fatalf("console not in reach: %+v", interaction)
	}
	frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Interact, Kind: Pressed}}})
	for count := 0; count < 20 && service.policy.State() == StateActive; count++ {
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	}
	if service.policy.State() != StatePause {
		_, state := service.world.Snapshot(ctx).Data(ctx)
		t.Fatalf("console completion did not interrupt the tick budget: %+v, %+v", state.Player, service.world.InteractionView(ctx))
	}
	view := choose(t, service, "reset_request", "D-HOS-SUP-01")
	if view.Confirmation != "reset_request" {
		t.Fatalf("reset warning: %+v", view)
	}
	view = choose(t, service, "confirm", "")
	if view.State != StateSaving {
		t.Fatalf("reset not saved: %+v", view)
	}
	request := store.last()
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: request.Proposal})
	view = frame(t, service, FrameInput{})
	attempt, ok := service.committed.ActiveAttempt()
	if view.State != StatePause || !ok || attempt.Number != 2 || view.Progress.Credits != 0 {
		t.Fatalf("reset acknowledgment: %+v, %+v", view, attempt)
	}
}

func TestRepeatedCheckpointHoldsAreAcknowledgedSeparately(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	committed, _ := preparedCertification(t)
	loadFixture(t, service, store, committed)
	for activation := 0; activation < 2; activation++ {
		frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond, Edges: []ActionEdge{{Action: settings.Interact, Kind: Pressed}}})
		for count := 0; count < 20 && service.policy.State() == StateActive; count++ {
			frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
		}
		if service.policy.State() != StateSaving {
			t.Fatal("checkpoint hold did not enter saving")
		}
		request := store.last()
		if request.Operation.Sequence != uint64(activation+1) || request.Proposal.Data(ctx).Anchor != "ANC-HOS-01" {
			t.Fatalf("checkpoint operation: %+v", request)
		}
		view := frame(t, service, FrameInput{Elapsed: time.Second})
		if view.State != StateSaving || view.Progress.Revision != uint64(7+activation) || view.Progress.DebtCents != 0 {
			t.Fatalf("unacknowledged checkpoint: %+v", view)
		}
		store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageSucceeded, Revision: uint64(8 + activation), RevisionKnown: true, PayloadDigest: newDigest, Snapshot: request.Proposal})
		view = frame(t, service, FrameInput{})
		if view.State != StatePause || view.Progress.Revision != uint64(8+activation) || view.Progress.DebtCents != 0 || view.Player.Health != 100 || view.Player.Feces != 5 {
			t.Fatalf("checkpoint acknowledgment: %+v", view)
		}
		frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Interact, Kind: Released}}})
		choose(t, service, "resume", "")
	}
}

func TestVoluntaryReturnResetsExcursionWithoutFee(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	loadFixture(t, service, store, campaign.Snapshot{})
	_, initial := service.world.Snapshot(ctx).Data(ctx)
	frame(t, service, FrameInput{Elapsed: 50 * time.Millisecond, Values: []ActionValue{{Action: settings.MoveRight, X: 1}}})
	_, moved := service.world.Snapshot(ctx).Data(ctx)
	if moved.Tick == 0 || moved.Player.Bounds.X <= initial.Player.Bounds.X {
		t.Fatal("fixture did not move")
	}
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Pause, Kind: Pressed}}})
	view := choose(t, service, "return_checkpoint", "")
	if view.Confirmation != "return_checkpoint" || view.Warning != "Незачтённый прогресс попытки будет сброшен. Зачтённые дела и штрафы сохраняются" {
		t.Fatalf("missing rollback warning: %+v", view)
	}
	view = choose(t, service, "confirm", "")
	_, restored := service.world.Snapshot(ctx).Data(ctx)
	if view.State != StatePause || restored.Player.Bounds != initial.Player.Bounds || restored.Tick != 0 || view.Progress.DebtCents != 0 || view.Player.Feces != 5 || len(store.requests) != 1 {
		t.Fatalf("return did not restore without charging: %+v", view)
	}
}

func TestDeathIsAcknowledgedOnceAndRetryRestoresResources(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	loadFixture(t, service, store, campaign.Snapshot{})
	command := simulation.ObjectCommand{Feces: simulation.FecesCommand{Limb: simulation.LimbCommand{Combat: simulation.CombatCommand{Visible: simulation.Rect{X: -500, Y: -500, Width: 2000, Height: 2000}, Hits: []simulation.DamageHit{{Attacker: "fixture", Attack: "lethal", Kind: simulation.LethalDamage, Amount: 100, Direction: 1}}}}}}
	if err := service.world.StepObjects(ctx, command); err != nil {
		t.Fatal(err)
	}
	if err := service.collect(ctx, false); err != nil {
		t.Fatal(err)
	}
	request := store.last()
	view := frame(t, service, FrameInput{Elapsed: time.Second})
	if view.State != StateSaving || view.Progress.DebtCents != 0 || len(view.Progress.Incidents) != 0 {
		t.Fatalf("unacknowledged death: %+v", view)
	}
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: request.Proposal})
	view = frame(t, service, FrameInput{})
	if view.State != StateDead || view.Progress.DebtCents != 4999 || len(view.Progress.Incidents) != 1 {
		t.Fatalf("death acknowledgment: %+v", view)
	}
	view = choose(t, service, "retry_death", "")
	if view.State != StatePause || view.Player.Health != 100 || view.Player.Feces != 5 || view.Progress.DebtCents != 4999 || len(view.Progress.Incidents) != 1 {
		t.Fatalf("death restoration: %+v", view)
	}
	if len(store.requests) != 2 {
		t.Fatal("restoration created another operation")
	}
	choose(t, service, "quit_to_title", "")
	choose(t, service, "confirm", "")
	choose(t, service, "continue", "")
	load := store.last()
	store.reply(ProgressResultData{ID: load.ID, Campaign: testID, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: request.Proposal})
	view = frame(t, service, FrameInput{})
	if view.State != StatePause || view.Progress.DebtCents != 4999 || len(view.Progress.Incidents) != 1 || len(view.Events) != 0 {
		t.Fatalf("continue duplicated death: %+v", view)
	}
}

func TestBothInvalidRequiresExplicitSeparateNewGame(t *testing.T) {
	service, store := sessionFixture(t)
	choose(t, service, "continue", "")
	request := store.last()
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Status: StorageCorrupt})
	view := frame(t, service, FrameInput{})
	if view.State != StateError || len(view.Candidates) != 0 || len(view.Choices) != 2 || view.Choices[0] != NewCampaign {
		t.Fatalf("both-invalid recovery: %+v", view)
	}
	choose(t, service, "new_game", "")
	if len(store.requests) != 1 {
		t.Fatal("new game started before confirmation")
	}
	choose(t, service, "confirm", "")
	request = store.last()
	if request.Kind != CreateCampaign {
		t.Fatal("new game did not request a separate identity")
	}
	other := CampaignID("12345678-90ab-4cde-8fab-0123456789ab")
	store.reply(ProgressResultData{ID: request.ID, Campaign: other, Status: StorageSucceeded, Revision: 0, RevisionKnown: true, PayloadDigest: newDigest})
	view = frame(t, service, FrameInput{})
	if view.State != StatePause || view.Progress.Campaign != other || view.Progress.Credits != 0 {
		t.Fatalf("new campaign: %+v", view)
	}
}

func TestNewGameIsExplicitAndUsesStoreIdentity(t *testing.T) {
	service, store := sessionFixture(t)
	loadFixture(t, service, store, campaign.Snapshot{})
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Pause, Kind: Pressed}}})
	choose(t, service, "quit_to_title", "")
	choose(t, service, "confirm", "")
	view := choose(t, service, "new_game", "")
	if view.Confirmation != "new_game" || view.Progress.Campaign != testID {
		t.Fatalf("missing new-game warning: %+v", view)
	}
	choose(t, service, "cancel", "")
	if len(store.requests) != 1 {
		t.Fatal("cancel wrote a campaign")
	}
	choose(t, service, "new_game", "")
	choose(t, service, "confirm", "")
	request := store.last()
	if request.Kind != CreateCampaign {
		t.Fatal("new game did not request a separate campaign")
	}
	other := CampaignID("12345678-90ab-4cde-8fab-0123456789ab")
	store.reply(ProgressResultData{ID: request.ID, Campaign: other, Status: StorageSucceeded, Revision: 0, RevisionKnown: true, PayloadDigest: newDigest})
	view = frame(t, service, FrameInput{})
	if view.State != StatePause || view.Progress.Campaign != other || view.Progress.Credits != 0 {
		t.Fatalf("new identity: %+v", view)
	}
}

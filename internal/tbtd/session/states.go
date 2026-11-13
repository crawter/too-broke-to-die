// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"fmt"
)

// StateEvent is an explicit application transition, not a gameplay result.
// SaveSuccess must name the acknowledged destination; it cannot itself certify
// a campaign event or establish that storage succeeded.
type StateEvent string

const (
	// ContinueGame starts restoration of the selected campaign.
	ContinueGame StateEvent = "continue"

	// NewGame starts the durable creation of a separate campaign.
	NewGame StateEvent = "new_game"

	// OpenPause freezes active gameplay or the current scene.
	OpenPause StateEvent = "pause"

	// ResumeGame explicitly returns to the paused gameplay or scene.
	ResumeGame StateEvent = "resume"

	// OpenLedger opens the ledger over gameplay or pause.
	OpenLedger StateEvent = "open_ledger"

	// CloseLedger returns to the state underneath the ledger.
	CloseLedger StateEvent = "close_ledger"

	// StartScene enters an authored scene from gameplay.
	StartScene StateEvent = "start_scene"

	// FinishScene returns from an authored scene to gameplay.
	FinishScene StateEvent = "finish_scene"

	// BeginSave freezes the current state for required durable work.
	BeginSave StateEvent = "begin_save"

	// SaveSuccess applies an acknowledged destination, not a proposed result.
	SaveSuccess StateEvent = "save_success"

	// SaveFailure opens recovery for the failed save or restoration.
	SaveFailure StateEvent = "save_failure"

	// RetryOperation retries the failed save or restoration operation.
	RetryOperation StateEvent = "retry_operation"

	// ReloadProgress begins explicit restoration from saved progress.
	ReloadProgress StateEvent = "reload_progress"

	// ExitWithoutSave returns to title after the caller confirms unsaved exit.
	ExitWithoutSave StateEvent = "exit_without_save"

	// RetryDeath begins restoration after an acknowledged temporary death.
	RetryDeath StateEvent = "retry_death"

	// ReturnCheckpoint begins restoration from the pause menu.
	ReturnCheckpoint StateEvent = "return_checkpoint"

	// RestoreActive finishes restoration in pause before active gameplay.
	RestoreActive StateEvent = "restore_active"

	// RestoreScene finishes restoration at a narrative anchor.
	RestoreScene StateEvent = "restore_scene"

	// RestoreCompleted finishes restoration at the completed-campaign menu.
	RestoreCompleted StateEvent = "restore_completed"

	// ReplayEnding opens the read-only ending sequence.
	ReplayEnding StateEvent = "replay_ending"

	// ReturnTitle returns to the title menu without starting a save.
	ReturnTitle StateEvent = "return_title"
)

// StatePolicy tracks overlay and save return states independently. Its zero
// value is the title screen. It does not own campaign or storage data.
type StatePolicy struct {
	state AppState

	pauseReturn AppState

	ledgerReturn AppState

	saveReturn AppState

	retryState AppState
}

// State returns the current application state.
func (policy *StatePolicy) State() AppState {
	if policy.state == "" {
		return StateTitle
	}
	return policy.state
}

// GameplayAllowed and SceneAllowed govern independent fixed clocks.
func (policy *StatePolicy) GameplayAllowed() bool { return policy.State() == StateActive }

// SceneAllowed reports whether the scene clock may advance.
func (policy *StatePolicy) SceneAllowed() bool { return policy.State() == StateScene }

// Transition validates a state event without making any persistence claim.
// On failure it leaves the state and all return states unchanged.
func (policy *StatePolicy) Transition(ctx context.Context, event StateEvent, destination AppState) (err error) {
	defer mon.Task()(&ctx)(&err)
	state := policy.State()
	next := *policy
	next.state = state
	valid := true
	switch event {
	case ContinueGame:
		valid = state == StateTitle
		next.state = StateRestoring
	case NewGame:
		valid = state == StateTitle || state == StateEnd
		next.saveReturn, next.state = state, StateSaving
	case OpenPause:
		valid = state == StateActive || state == StateScene
		next.pauseReturn, next.state = state, StatePause
	case ResumeGame:
		valid = state == StatePause && (policy.pauseReturn == StateActive || policy.pauseReturn == StateScene)
		next.state = policy.pauseReturn
	case OpenLedger:
		valid = state == StateActive || state == StatePause
		next.ledgerReturn, next.state = state, StateLedger
	case CloseLedger:
		valid = state == StateLedger && (policy.ledgerReturn == StateActive || policy.ledgerReturn == StatePause)
		next.state = policy.ledgerReturn
	case StartScene:
		valid = state == StateActive
		next.state = StateScene
	case FinishScene:
		valid = state == StateScene
		next.state = StateActive
	case BeginSave:
		valid = state == StateActive || state == StateScene || state == StatePause || state == StateRestoring
		next.saveReturn, next.state = state, StateSaving
	case SaveSuccess:
		valid = state == StateSaving && saveDestination(policy.saveReturn, destination)
		next.state = destination
		next.retryState = ""
		if policy.saveReturn == StateRestoring && destination == StatePause {
			next.pauseReturn = StateActive
		}
	case SaveFailure:
		valid = state == StateSaving || state == StateRestoring
		next.retryState, next.state = state, StateError
	case RetryOperation:
		valid = state == StateError && (policy.retryState == StateSaving || policy.retryState == StateRestoring)
		next.state, next.retryState = policy.retryState, ""
	case ReloadProgress:
		valid = state == StateError
		next.state = StateRestoring
	case ExitWithoutSave:
		valid = state == StateError
		next.state = StateTitle
	case RetryDeath:
		valid = state == StateDead
		next.state = StateRestoring
	case ReturnCheckpoint:
		valid = state == StatePause
		next.state = StateRestoring
	case RestoreActive:
		valid = state == StateRestoring
		next.pauseReturn, next.state = StateActive, StatePause
	case RestoreScene:
		valid = state == StateRestoring
		next.state = StateScene
	case RestoreCompleted:
		valid = state == StateRestoring
		next.state = StateEnd
	case ReplayEnding:
		valid = state == StateEnd
		next.state = StateScene
	case ReturnTitle:
		valid = state == StatePause || state == StateDead || state == StateEnd
		next.state = StateTitle
	default:
		valid = false
	}
	if !valid {
		return fmt.Errorf("%w: transition %s from %s to %s", InvalidAction, event, state, destination)
	}
	*policy = next
	return nil
}

func saveDestination(source, destination AppState) bool {
	switch source {
	case StateTitle, StateEnd:
		return destination == StateRestoring
	case StateActive:
		return destination == StateActive || destination == StateScene || destination == StateDead
	case StateScene:
		return destination == StateScene || destination == StateActive || destination == StateEnd
	case StatePause:
		return destination == StatePause || destination == StateRestoring || destination == StateTitle
	case StateRestoring:
		return destination == StateRestoring || destination == StatePause || destination == StateScene || destination == StateEnd
	default:
		return false
	}
}

// focusPause also handles a ledger opened over live play: closing it must
// reveal a pause, not return directly to gameplay after focus returns.
func (policy *StatePolicy) focusPause(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if policy.State() == StateLedger && policy.ledgerReturn == StateActive {
		policy.ledgerReturn = StatePause
		policy.pauseReturn = StateActive
		return nil
	}
	if policy.State() == StateActive || policy.State() == StateScene {
		return policy.Transition(ctx, OpenPause, "")
	}
	return nil
}

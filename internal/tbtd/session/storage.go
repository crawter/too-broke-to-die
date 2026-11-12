// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

// RequestID correlates an asynchronous submission with its result.
type RequestID string

// CampaignID identifies a locally generated campaign, not an account or path.
type CampaignID string

// OperationID identifies a prepared campaign transition. Event is a stable
// deed, milestone, emergency-stage or occurrence identity; Sequence is the
// campaign's once-assigned operation sequence.
type OperationID struct {
	Event string

	Sequence uint64
}

// ProgressKind identifies an operation at the storage boundary.
type ProgressKind string

const (
	// LoadActive loads the campaign selected by the active profile selector.
	LoadActive ProgressKind = "load_active"

	// LoadCampaign loads the selected valid revision for a named campaign.
	LoadCampaign ProgressKind = "load_campaign"

	// LoadRevision explicitly selects a validated recovery candidate, including
	// revision zero if an initial revision has that number.
	LoadRevision ProgressKind = "load_revision"

	// CreateCampaign initializes a separate campaign before selecting it.
	CreateCampaign ProgressKind = "create_campaign"

	// SelectCampaign explicitly selects an existing campaign for the profile.
	SelectCampaign ProgressKind = "select_campaign"

	// CommitProgress writes a prepared snapshot at its expected revision.
	CommitProgress ProgressKind = "commit"

	// ResolveProgress inspects the original commit after an ambiguous result.
	ResolveProgress ProgressKind = "resolve"
)

// ProgressInput supplies a request. A commit's proposal is a campaign-owned,
// immutable snapshot prepared once, not a public committed state. Create
// installs both initial revisions before selecting the separate campaign.
type ProgressInput struct {
	ID RequestID

	Kind ProgressKind

	Campaign CampaignID

	// ExpectedRevision is the expected current revision for a commit, or the
	// exact candidate revision for LoadRevision. Other operations leave it zero.
	ExpectedRevision uint64

	// ExpectedDigest is the selected predecessor's payload digest captured when
	// the proposal was prepared. The store validates it before writing; retries
	// and resolution must retain it rather than re-reading the current slot.
	ExpectedDigest string

	Operation OperationID

	Proposal campaign.Snapshot
}

// ProgressRequest owns a prepared payload. A retry submits this same value;
// Resolution changes only the requested operation, not identity or payload.
type ProgressRequest struct{ input ProgressInput }

var campaignIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// NewProgressRequest checks request identity and proposal metadata. Domain
// validation and the payload digest remain the store's responsibility.
func NewProgressRequest(ctx context.Context, input ProgressInput) (_ ProgressRequest, err error) {
	defer mon.Task()(&ctx)(&err)
	if strings.TrimSpace(string(input.ID)) == "" {
		return ProgressRequest{}, fmt.Errorf("%w: missing request identity", InvalidAction)
	}
	switch input.Kind {
	case LoadActive:
		if input.Campaign != "" || input.Operation != (OperationID{}) || input.ExpectedRevision != 0 {
			return ProgressRequest{}, fmt.Errorf("%w: active load identity", InvalidAction)
		}
	case LoadCampaign, CreateCampaign, SelectCampaign:
		if !campaignIDPattern.MatchString(string(input.Campaign)) || input.Operation != (OperationID{}) || input.ExpectedRevision != 0 {
			return ProgressRequest{}, fmt.Errorf("%w: campaign request identity", InvalidAction)
		}
	case LoadRevision:
		if !campaignIDPattern.MatchString(string(input.Campaign)) || input.Operation != (OperationID{}) {
			return ProgressRequest{}, fmt.Errorf("%w: recovery revision identity", InvalidAction)
		}
	case CommitProgress:
		state := input.Proposal.Data(ctx)
		if !campaignIDPattern.MatchString(string(input.Campaign)) || strings.TrimSpace(input.Operation.Event) == "" || input.Operation.Sequence == 0 || state.CampaignID != string(input.Campaign) || state.OperationSequence != input.Operation.Sequence || state.LatestOperation == nil || state.LatestOperation.Sequence != input.Operation.Sequence {
			return ProgressRequest{}, fmt.Errorf("%w: proposal identity", InvalidAction)
		}
	default:
		return ProgressRequest{}, fmt.Errorf("%w: progress request kind", InvalidAction)
	}
	return ProgressRequest{input: input}, nil
}

// Data returns a value whose proposal remains isolated by campaign.Snapshot.
func (request ProgressRequest) Data() ProgressInput { return request.input }

// Resolution requests inspection of installed revisions before retry, reload
// or discard. It is valid only for an already prepared commit.
func (request ProgressRequest) Resolution() (ProgressRequest, error) {
	if request.input.Kind != CommitProgress && request.input.Kind != ResolveProgress {
		return ProgressRequest{}, fmt.Errorf("%w: no pending commit", InvalidAction)
	}
	request.input.Kind = ResolveProgress
	return request, nil
}

// StorageStatus distinguishes a pending request from an acknowledged result.
type StorageStatus string

const (
	// StoragePending means accepted work has not yet produced a final result.
	StoragePending StorageStatus = "pending"

	// StorageSucceeded means the result was validated and acknowledged.
	StorageSucceeded StorageStatus = "succeeded"

	// StorageFailed means the operation failed without a successful acknowledgment.
	StorageFailed StorageStatus = "failed"

	// StorageConflicting means an identity, payload, or expected revision conflicts.
	StorageConflicting StorageStatus = "conflicting"

	// StorageCorrupt means the checked data failed structural or semantic validation.
	StorageCorrupt StorageStatus = "corrupt"

	// StorageTooNew means the saved schema requires a newer compatible version.
	StorageTooNew StorageStatus = "too_new"

	// StorageIncompatible is a known schema with content requiring a mapping.
	StorageIncompatible StorageStatus = "incompatible"

	// StorageAmbiguous means installation or acknowledgment remains unresolved.
	StorageAmbiguous StorageStatus = "ambiguous"

	// StorageNotInstalled means resolution verified only the old revision and
	// the original worker can no longer install its proposal. The owner can
	// now retry the original request or confirm its discard.
	StorageNotInstalled StorageStatus = "not_installed"
)

// RecoveryCandidate describes one independently validated complete revision.
// UnknownDifference is true when an unreadable revision prevents comparison;
// no receipts are inferred from the damaged revision.
type RecoveryCandidate struct {
	Campaign CampaignID

	Revision uint64

	Anchor content.AnchorID

	Credits int

	KnownDifference string

	UnknownDifference bool
}

// ProgressResultData is a storage outcome, never an OS error or JSON blob.
// Snapshot is publishable only on StorageSucceeded after the store has
// validated and acknowledged it. PayloadDigest is the store's exact digest,
// supplied after encoding; it must be checked when resolving an ambiguity.
type ProgressResultData struct {
	ID RequestID

	Campaign CampaignID

	Operation OperationID

	Status StorageStatus

	Code ErrorCode

	Revision uint64

	// RevisionKnown distinguishes an actual revision zero from an unknown
	// selected revision on a failed, pending or conflicting operation.
	RevisionKnown bool

	PayloadDigest string

	Snapshot campaign.Snapshot

	Candidates []RecoveryCandidate
}

// ProgressResult owns its recovery choices.
type ProgressResult struct{ data ProgressResultData }

// NewProgressResult copies all mutable values provided by a store.
func NewProgressResult(ctx context.Context, data ProgressResultData) ProgressResult {
	defer mon.Task()(&ctx)(nil)
	data.Candidates = slices.Clone(data.Candidates)
	return ProgressResult{data: data}
}

// Data returns an independent copy of the result and its recovery choices.
func (result ProgressResult) Data(ctx context.Context) ProgressResultData {
	defer mon.Task()(&ctx)(nil)
	data := result.data
	data.Candidates = slices.Clone(data.Candidates)
	return data
}

// SettingsKind identifies a separate settings operation.
type SettingsKind string

const (
	// LoadSettings reads the separately stored player preferences.
	LoadSettings SettingsKind = "load"

	// SaveSettings writes validated player preferences independently of progress.
	SaveSettings SettingsKind = "save"
)

// SettingsInput contains no campaign identity or revision.
type SettingsInput struct {
	ID RequestID

	Kind SettingsKind

	Values settings.Values
}

// SettingsRequest owns the settings draft including all binding records.
type SettingsRequest struct{ input SettingsInput }

// NewSettingsRequest validates saves before copying their values. A load does
// not require a settings value; corruption is reported as a typed result.
func NewSettingsRequest(ctx context.Context, input SettingsInput) (_ SettingsRequest, err error) {
	defer mon.Task()(&ctx)(&err)
	if strings.TrimSpace(string(input.ID)) == "" || (input.Kind != LoadSettings && input.Kind != SaveSettings) {
		return SettingsRequest{}, fmt.Errorf("%w: settings request", InvalidAction)
	}
	if input.Kind == SaveSettings {
		if err = input.Values.Validate(ctx); err != nil {
			return SettingsRequest{}, fmt.Errorf("%w: %w", InvalidAction, err)
		}
	}
	input.Values = input.Values.Clone(ctx)
	return SettingsRequest{input: input}, nil
}

// Data returns an independent copy of the settings draft.
func (request SettingsRequest) Data(ctx context.Context) SettingsInput {
	defer mon.Task()(&ctx)(nil)
	input := request.input
	input.Values = input.Values.Clone(ctx)
	return input
}

// SettingsResultData reports a settings operation independently of progress.
// DefaultsRestored asks presentation to show the settings-corruption notice.
type SettingsResultData struct {
	ID RequestID

	Status StorageStatus

	Code ErrorCode

	Values settings.Values

	DefaultsRestored bool
}

// SettingsResult owns its settings value.
type SettingsResult struct{ data SettingsResultData }

// NewSettingsResult copies mutable bindings from the store.
func NewSettingsResult(ctx context.Context, data SettingsResultData) SettingsResult {
	defer mon.Task()(&ctx)(nil)
	data.Values = data.Values.Clone(ctx)
	return SettingsResult{data: data}
}

// Data returns an independent settings value.
func (result SettingsResult) Data(ctx context.Context) SettingsResultData {
	defer mon.Task()(&ctx)(nil)
	data := result.data
	data.Values = data.Values.Clone(ctx)
	return data
}

var (
	// ErrStoreBusy means the nonblocking queue did not accept the request.
	ErrStoreBusy = errors.New("storage queue full")
	// ErrStoreClosed means no new request was accepted during shutdown.
	ErrStoreClosed = errors.New("storage closed")
	// ErrUnknownRequest means the store has no request with this identity.
	ErrUnknownRequest = errors.New("unknown storage request")
)

// ProgressStore is an external asynchronous boundary. Submit returns promptly:
// nil means ownership has transferred, while an error means nothing was
// accepted. Poll is nonblocking and returns pending until a result is available.
// Results retain request, campaign, operation, revision and digest identities.
// On conflict, Revision is the actual selected revision when known, not the
// expected revision. Success identifies the installed revision and exact
// payload digest. The store persists and compares the complete OperationID as
// well as the proposal digest; Event alone never identifies an aggregate
// transition containing multiple simultaneous events. Repeated identical
// requests must not produce another event; changed payloads or stale revisions
// conflict. ResolveProgress inspects the original proposal and identity. It
// returns its existing success if installed or StorageNotInstalled if only the
// old revision is present; the caller may then retry or confirm discard.
// Ambiguous work blocks subsequent campaign work
// until Resolution inspects both valid slots. Reload and unsaved exit resolve
// ambiguity first. The consumer publishes only an acknowledged success.
//
// The initiating ctx carries tracing and cancellation to the acceptance point.
// Once accepted, the store owns a supervised bounded draining lifetime derived
// from that context (preserving values/tracing without ordinary cancellation).
// Its lifecycle owner stops acceptance, drains or resolves work with a finite
// deadline, joins the worker, and reports failure before releasing resources.
// Campaign and settings submissions share one serialized writer; settings must
// not overtake a pending campaign operation.
type ProgressStore interface {
	Submit(ctx context.Context, request ProgressRequest) error
	Poll(ctx context.Context, id RequestID) (ProgressResult, error)
}

// SettingsStore is a separate typed view of the same owned writer; a settings
// failure must never roll back campaign progress. Implementations may expose
// separate facades because Go methods cannot overload Submit or Poll.
type SettingsStore interface {
	Submit(ctx context.Context, request SettingsRequest) error
	Poll(ctx context.Context, id RequestID) (SettingsResult, error)
}

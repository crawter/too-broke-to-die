// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"fmt"
	"slices"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
)

// AppState names an application state, not a renderer screen object.
type AppState string

const (
	// StateTitle is the title and profile menu.
	StateTitle AppState = "ST-TITLE"

	// StateActive advances gameplay simulation.
	StateActive AppState = "ST-ACT"

	// StatePause is the paused overlay and requires explicit resume.
	StatePause AppState = "ST-PAU"

	// StateLedger displays the ledger over its source state.
	StateLedger AppState = "ST-LED"

	// StateScene advances an authored narrative sequence on its own clock.
	StateScene AppState = "ST-SCN"

	// StateSaving freezes gameplay while a durable event is processed.
	StateSaving AppState = "ST-SAV"

	// StateDead displays the acknowledged temporary-death result.
	StateDead AppState = "ST-DEAD"

	// StateRestoring loads a committed campaign and recovery location.
	StateRestoring AppState = "ST-RES"

	// StateError blocks gameplay for storage or recovery decisions.
	StateError AppState = "ST-ERR"

	// StateEnd is the menu for a durably completed campaign.
	StateEnd AppState = "ST-END"
)

// SaveIndicator distinguishes an in-flight write from durable acknowledgment.
type SaveIndicator string

const (
	// SaveNone means no save status is currently displayed.
	SaveNone SaveIndicator = "none"

	// SavePending means a durable write is still in progress.
	SavePending SaveIndicator = "pending"

	// SaveConfirmed means the durable write was acknowledged.
	SaveConfirmed SaveIndicator = "saved"

	// SaveError means a required write failed and needs recovery.
	SaveError SaveIndicator = "error"
)

// RecoveryChoice identifies an explicit player decision. Reload and unsaved
// exit require confirmation and resolution of any ambiguous operation first.
type RecoveryChoice string

const (
	// RetrySave retries the same prepared operation after resolving ambiguity.
	RetrySave RecoveryChoice = "retry_save"

	// ReloadSaved restores saved progress after confirming any discarded event.
	ReloadSaved RecoveryChoice = "reload_saved"

	// ExitUnsaved leaves without the uncommitted event after confirmation.
	ExitUnsaved RecoveryChoice = "exit_unsaved"

	// ChooseOlder explicitly selects an independently validated older revision.
	ChooseOlder RecoveryChoice = "choose_older"

	// NewCampaign starts a separate campaign after the required confirmation.
	NewCampaign RecoveryChoice = "new_campaign"

	// ExitGame exits rather than selecting a recovery or new campaign.
	ExitGame RecoveryChoice = "exit"
)

// ReceiptView is presentation data derived from a committed receipt.
type ReceiptView struct {
	Deed content.DeedID

	Title string

	Institution content.LocationID

	InstitutionName string

	Evidence string

	Ordinal int
}

// IncidentView is an acknowledged penalty or failed-attempt record.
type IncidentView struct {
	ID campaign.OccurrenceID

	Kind campaign.IncidentKind

	Deed content.DeedID

	Victim string

	Detail string

	Cents int64
}

// ProgressView contains no mutable campaign state or independent credit
// balance. Credits is derived from the acknowledged snapshot's receipt set.
type ProgressView struct {
	Campaign CampaignID

	Revision uint64

	Credits int

	DebtCents int64

	Anchor content.AnchorID

	Receipts []ReceiptView

	Consequences []content.ConsequenceID

	Emergencies []campaign.Emergency

	Incidents []IncidentView

	Active content.DeedID

	Narratives []content.NarrativeID

	Ending campaign.EndingStage

	Permit bool

	Consent bool

	Completed bool
}

// VisualPart is a renderer-facing attachment or animation selection.
type VisualPart struct {
	Slot string

	Asset content.PresentationID

	Animation content.PresentationID

	// Export and Source select an explicitly authored rectangle in the cache's
	// manifest-order exports. Offset and Size are logical world pixels relative
	// to the entity's bottom-centre pivot; they never affect collision.
	Export int

	Source WorldRect

	Offset Point

	Size Point
}

// EntityView provides permitted transient drawing data. Interpolation is visual
// only; neither its positions nor its parts are the authoritative world.
type EntityView struct {
	ID string

	Asset content.PresentationID

	PreviousX float64

	PreviousY float64

	X float64

	Y float64

	Parts []VisualPart

	// Kind and State are authoritative semantic identities, not sprite handles.
	Kind string

	State string

	// Variant identifies a projectile or detached socket without inventing an
	// asset ID where the producer has not supplied a binding.
	Variant string

	Facing int

	Width float64

	Height float64
}

// EventParameter is a semantic text or audio cue argument, not a native handle.
type EventParameter struct {
	Name string

	Value string
}

// SemanticEvent allows presentation to deduplicate transient cues by tick,
// entity and ordinal, and durable cues by revision and operation identity.
// A durable reward cue must be emitted only after acknowledgment. Restoration
// reconstructs state without replaying historical reward sounds.
type SemanticEvent struct {
	Kind string

	// Durable cues may be published only for an acknowledged revision.
	Durable bool

	Tick uint64

	Entity string

	Ordinal uint32

	Revision uint64

	Operation OperationID

	Parameters []EventParameter
}

// PlayerView is permitted transient status, never a durable combat snapshot.
type PlayerView struct {
	Health int

	Limbs [4]bool

	SelectedPayload string

	SelectedSocket string

	Feces int

	Immune bool

	Channel string

	ChannelProgress float64

	// RejectionID is the catalogue identity for the last rejected action.
	RejectionID content.PresentationID

	Action string

	AttackPhase string

	AttackStart uint64

	AttackReleased bool

	Velocity Point

	Grounded bool

	Facing int
}

// Point is a world-space presentation coordinate, not a native graphics type.
type Point struct {
	X float64

	Y float64
}

// AimView is the transient preview produced by the authoritative simulation.
type AimView struct {
	Visible bool

	// Reticle is a logical-canvas mouse position, independent of camera shake.
	Reticle ReticleInput

	// Direction is the current normalized world-space controller aim.
	Direction Point

	Trajectory []Point

	Impact Point

	HasImpact bool

	// ImpactKind distinguishes terrain, valid receptors and other blockers.
	ImpactKind string
}

// BossView describes the current transient encounter without exposing it.
type BossView struct {
	ID content.EncounterID

	Health int

	MaximumHealth int

	Phase int

	Protected bool
}

// WorldRect uses world coordinates with positive Y upward.
type WorldRect struct {
	X, Y, Width, Height float64
}

// WorldMarker describes an authoritative, non-decorative world indication.
type WorldMarker struct {
	ID, Kind, State, Label string
	Bounds                 WorldRect
}

// EffectEvent records a transient simulation event, not gameplay state.
// Tick and Ordinal identify it within one restored world.
type EffectEvent struct {
	Tick    uint64
	Ordinal uint32
	Entity  string
	Kind    string
	Asset   content.PresentationID
	Part    VisualPart
	// Particles and Stain are separately authored regions, not generated art.
	// Their offsets are relative to Position. Stain is required for blood hits.
	Particles []VisualPart
	Stain     VisualPart
	Position  Point
	Direction int
	Heavy     bool
}

// SceneryLayer binds an authored texture region to its world-space bounds.
// Depth is 0 for distant scenery, 1 for rear machinery, or 2 for terrain.
type SceneryLayer struct {
	Asset  content.PresentationID
	Export int
	Source WorldRect
	Bounds WorldRect
	Depth  int
}

// WorldView contains the current encounter presentation inputs. It never owns
// simulation objects; all collections are copied at the FrameView boundary.
type WorldView struct {
	Location  content.LocationID
	Encounter content.EncounterID
	// Generation changes when the transient world is reconstructed, even at
	// the same encounter and tick after recovery.
	Generation uint64
	Background content.PresentationID
	Tick       uint64
	Fraction   float64
	BossLocked bool
	Terrain    []WorldRect
	Scenery    []SceneryLayer
	Effects    []EffectEvent
	Markers    []WorldMarker
	Landings   []WorldRect
}

// TextView presents a semantic dialogue, objective, notice or sound caption.
type TextView struct {
	ID content.PresentationID

	Speaker string

	Text string

	Direction string
}

// MenuItem describes focus and a disabled reason without native UI objects.
type MenuItem struct {
	ID string

	Label string

	Focused bool

	DisabledReason string
}

// PendingEvent describes uncommitted work without pretending it is a receipt
// or an assessed fee. It remains visible on a failure and discard confirmation.
type PendingEvent struct {
	Operation OperationID

	Label string

	// SceneName and UnitText are taken from the validated authored scene for a
	// pending scene-unit transition, not from storage diagnostics.
	SceneName string
	UnitText  string
}

// SceneView identifies the current authored unit and its fixed-clock time.
type SceneView struct {
	ID string

	Unit string

	Narrative content.NarrativeID

	Ending content.EndingID

	Ticks uint64

	Background content.PresentationID

	Replay bool

	ReplayLabel string
}

// TicketView contains authoritative eligibility and the current attempt status.
type TicketView struct {
	Deed      content.DeedID
	Status    string
	Missing   []string
	FailureID content.PresentationID
}

// FrameViewData contains application, recovery, world and cue data. Progress
// passed in this structure is ignored by NewFrameView: the constructor derives
// it from the supplied acknowledged snapshot. Slices are copied on both sides.
type FrameViewData struct {
	State AppState

	Save SaveIndicator

	// TitleLoading and TitleUnavailable describe the read-only profile lookup.
	TitleLoading     bool
	TitleUnavailable bool

	Progress ProgressView

	Pending PendingEvent

	// Confirmation names the destructive or rollback action awaiting explicit consent.
	Confirmation string

	// Warning explains the confirmation's rollback scope and saved total.
	Warning string

	// Slow reports that an accepted storage operation has taken at least five seconds.
	Slow bool

	// Notice describes a resolved recovery or rollback without claiming unsaved success.
	Notice string

	// ExitRequested becomes true only after a confirmed safe exit.
	ExitRequested bool

	Error ErrorCode

	Choices []RecoveryChoice

	Candidates []RecoveryCandidate

	// BothRevisionsInvalid is an explicit storage inspection outcome.
	BothRevisionsInvalid bool

	// RestorationReady remains true until the restored pause is resumed.
	RestorationReady bool

	Player PlayerView

	Boss BossView

	Scene SceneView

	Aim AimView

	World WorldView

	Tracked content.DeedID

	// ObjectiveID is the validated catalogue identity for the live next action.
	ObjectiveID content.PresentationID

	// ServiceStatus is the authoritative transient attempt state.
	ServiceStatus string

	// Emergency identifies the current live danger, including its warning interval.
	Emergency content.EmergencyID

	EmergencyWarning bool

	Tickets []TicketView

	Texts []TextView

	Menu []MenuItem

	Entities []EntityView

	Events []SemanticEvent
}

// FrameView is a presentation-facing value with private owned collections.
type FrameView struct{ data FrameViewData }

// NewFrameView constructs a view from the last acknowledged snapshot, never a
// private proposal. The session owner must pass the acknowledged campaign and
// revision even while displaying a pending or failed event. The constructor
// derives totals and receipt rows and makes independent copies of all slices.
func NewFrameView(ctx context.Context, data FrameViewData, committed campaign.Snapshot, id CampaignID, revision uint64) (_ FrameView, err error) {
	defer mon.Task()(&ctx)(&err)
	state := committed.Data(ctx)
	for _, event := range data.Events {
		if event.Durable && (event.Revision == 0 || event.Revision > revision || event.Operation.Event == "" || event.Operation.Sequence == 0 || event.Operation.Sequence > state.OperationSequence) {
			return FrameView{}, fmt.Errorf("%w: unacknowledged durable event", InvalidAction)
		}
	}
	debt, err := committed.Debt(ctx)
	if err != nil {
		return FrameView{}, err
	}
	progress := ProgressView{Campaign: id, Revision: revision, Credits: committed.Credits(), DebtCents: debt, Anchor: state.Anchor}
	for _, receipt := range state.Receipts {
		progress.Receipts = append(progress.Receipts, ReceiptView{
			Deed: receipt.Certification.Deed, Title: receipt.Title, Institution: receipt.Institution,
			InstitutionName: receipt.InstitutionName, Evidence: receipt.Certification.Evidence, Ordinal: receipt.Ordinal,
		})
	}
	progress.Consequences = slices.Clone(state.Consequences)
	progress.Emergencies = slices.Clone(state.Emergencies)
	progress.Active = state.Active
	progress.Narratives = slices.Clone(state.Narratives)
	progress.Ending = state.Ending
	progress.Permit = state.Permit
	progress.Consent = state.Consent
	progress.Completed = state.Completed
	for _, incident := range state.Incidents {
		progress.Incidents = append(progress.Incidents, IncidentView{ID: incident.ID, Kind: incident.Kind, Deed: incident.Deed, Victim: incident.Victim, Detail: incident.Detail, Cents: incident.Cents})
	}
	data.Progress = progress
	return FrameView{data: cloneFrameView(data)}, nil
}

// Data returns an independent presentation copy, including nested parts and
// cue parameters. Mutating it cannot change the view or the live campaign.
func (view FrameView) Data(ctx context.Context) FrameViewData {
	defer mon.Task()(&ctx)(nil)
	return cloneFrameView(view.data)
}

func cloneFrameView(data FrameViewData) FrameViewData {
	data.Progress.Receipts = slices.Clone(data.Progress.Receipts)
	data.Progress.Consequences = slices.Clone(data.Progress.Consequences)
	data.Progress.Emergencies = slices.Clone(data.Progress.Emergencies)
	data.Progress.Incidents = slices.Clone(data.Progress.Incidents)
	data.Progress.Narratives = slices.Clone(data.Progress.Narratives)
	data.Aim.Trajectory = slices.Clone(data.Aim.Trajectory)
	data.World.Terrain = slices.Clone(data.World.Terrain)
	data.World.Scenery = slices.Clone(data.World.Scenery)
	data.World.Effects = slices.Clone(data.World.Effects)
	for i := range data.World.Effects {
		data.World.Effects[i].Particles = slices.Clone(data.World.Effects[i].Particles)
	}
	data.World.Markers = slices.Clone(data.World.Markers)
	data.World.Landings = slices.Clone(data.World.Landings)
	data.Texts = slices.Clone(data.Texts)
	data.Tickets = slices.Clone(data.Tickets)
	for i := range data.Tickets {
		data.Tickets[i].Missing = slices.Clone(data.Tickets[i].Missing)
	}
	data.Menu = slices.Clone(data.Menu)
	data.Choices = slices.Clone(data.Choices)
	data.Candidates = slices.Clone(data.Candidates)
	data.Entities = slices.Clone(data.Entities)
	for i := range data.Entities {
		data.Entities[i].Parts = slices.Clone(data.Entities[i].Parts)
	}
	data.Events = slices.Clone(data.Events)
	for i := range data.Events {
		data.Events[i].Parameters = slices.Clone(data.Events[i].Parameters)
	}
	return data
}

// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
)

// Definitions is an authored document. It is input to Validate, not a validated view.
// Durations use integer milliseconds, and geometry uses world pixels.
type Definitions struct {
	SchemaVersion int `json:"schema_version"`

	ContentVersion string `json:"content_version"`

	Locations []Location `json:"locations"`

	Encounters []Encounter `json:"encounters"`

	Objects []Object `json:"objects"`

	Anchors []Anchor `json:"anchors"`

	Narratives []Narrative `json:"narratives"`

	Deeds []Deed `json:"deeds"`

	Consequences []Consequence `json:"consequences"`

	Emergencies []Emergency `json:"emergencies"`

	Endings []Ending `json:"endings"`

	Gates []Gate `json:"gates"`

	Presentations []Presentation `json:"presentations"`

	Fonts FontSet `json:"fonts,omitempty"`
}

// Rect is an axis-aligned rectangle in world pixels.
type Rect struct {
	X float64 `json:"x"`

	Y float64 `json:"y"`

	Width float64 `json:"width"`

	Height float64 `json:"height"`
}

// Location identifies an institution independently of its displayed name.
type Location struct {
	ID LocationID `json:"id"`

	Name PresentationID `json:"name"`
}

// Encounter contains the bounds and hazardous or solid geometry relevant to spawns.
// Station IDs refer to shared objects in this encounter; the full inventory
// requires checkpoint and refill stations, and consoles and bypasses in bays.
type Encounter struct {
	ID EncounterID `json:"id"`

	Location LocationID `json:"location"`

	Bounds Rect `json:"bounds"`

	Solids []Rect `json:"solids"`

	Lethal []Rect `json:"lethal"`

	Presentation PresentationID `json:"presentation,omitempty"`

	Checkpoint ObjectID `json:"checkpoint,omitempty"`

	Refill ObjectID `json:"refill,omitempty"`

	RequestConsole ObjectID `json:"request_console,omitempty"`

	Bypass ObjectID `json:"bypass,omitempty"`
}

// ObjectRole identifies an object's gameplay purpose independently of its label.
type ObjectRole string

// RoleCheckpoint identifies a recovery checkpoint.
const RoleCheckpoint ObjectRole = "CHECKPOINT"

// RoleRefill identifies a resource refill station.
const RoleRefill ObjectRole = "REFILL"

// RoleConsole identifies a request console.
const RoleConsole ObjectRole = "CONSOLE"

// RoleBypass identifies a bypass route.
const RoleBypass ObjectRole = "BYPASS"

// RoleSwitch identifies a utility bypass switch.
const RoleSwitch ObjectRole = "SWITCH"

// RoleTerminal identifies a certification terminal or meter.
const RoleTerminal ObjectRole = "TERMINAL"

// RoleTrolley identifies a ticket's sealed trolley.
const RoleTrolley ObjectRole = "TROLLEY"

// RolePad identifies a receiving pad.
const RolePad ObjectRole = "PAD"

// RoleCrusher identifies a marked crusher.
const RoleCrusher ObjectRole = "CRUSHER"

// RoleActor identifies the named adult of a rescue ticket.
const RoleActor ObjectRole = "ACTOR"

// RolePlate identifies a pressure plate.
const RolePlate ObjectRole = "PLATE"

// RoleCrate identifies a movable crate.
const RoleCrate ObjectRole = "CRATE"

// RoleGate identifies a service gate.
const RoleGate ObjectRole = "GATE"

// RoleLatch identifies a gate latch.
const RoleLatch ObjectRole = "LATCH"

// RoleRefuge identifies a rescue refuge.
const RoleRefuge ObjectRole = "REFUGE"

// RolePaddle identifies a ticket's impact paddle.
const RolePaddle ObjectRole = "PADDLE"

// RoleFuse identifies a utility fuse.
const RoleFuse ObjectRole = "FUSE"

// RoleRecorder identifies a ticket's evidence recorder.
const RoleRecorder ObjectRole = "RECORDER"

// RoleCamera identifies a ticket's camera receptor.
const RoleCamera ObjectRole = "CAMERA"

// Object is an authored interactive object within an encounter.
type Object struct {
	ID ObjectID `json:"id"`

	Encounter EncounterID `json:"encounter"`

	Deed DeedID `json:"deed,omitempty"`

	Role ObjectRole `json:"role,omitempty"`

	Presentation PresentationID `json:"presentation,omitempty"`
}

// Anchor places the player's bottom-center point in a safe encounter.
type Anchor struct {
	ID AnchorID `json:"id"`

	Encounter EncounterID `json:"encounter"`

	X float64 `json:"x"`

	Y float64 `json:"y"`
}

// Narrative is a stable story milestone.
type Narrative struct {
	ID NarrativeID `json:"id"`

	Location LocationID `json:"location"`

	Prerequisites []NarrativeID `json:"prerequisites"`

	Presentation PresentationID `json:"presentation,omitempty"`
}

// Deed is one authored, non-repeatable credit source. Prerequisites and
// Narratives identify prior receipts and milestones; Defeated identifies boss
// victories. Objects can include ticket-specific and shared encounter objects.
type Deed struct {
	ID DeedID `json:"id"`

	Encounter EncounterID `json:"encounter"`

	Anchor AnchorID `json:"anchor"`

	Consequence ConsequenceID `json:"consequence"`

	Prerequisites []DeedID `json:"prerequisites"`

	Objects []ObjectID `json:"objects"`

	Narratives []NarrativeID `json:"narratives"`

	Defeated []EncounterID `json:"defeated,omitempty"`

	Title PresentationID `json:"title"`

	HoldMilliseconds int64 `json:"hold_ms"`
}

// Consequence identifies the aftermath of a particular certification.
type Consequence struct {
	ID ConsequenceID `json:"id"`

	Deed DeedID `json:"deed"`

	Presentation PresentationID `json:"presentation,omitempty"`
}

// Emergency describes the shared sixth-certification emergency of a batch.
type Emergency struct {
	ID EmergencyID `json:"id"`

	Encounter EncounterID `json:"encounter"`

	Anchor AnchorID `json:"anchor"`

	WarningMilliseconds int64 `json:"warning_ms"`

	ActiveMilliseconds int64 `json:"active_ms"`

	Presentation PresentationID `json:"presentation,omitempty"`
}

// Ending describes one saved stage of the canonical ending.
type Ending struct {
	ID EndingID `json:"id"`

	Anchor AnchorID `json:"anchor"`

	Narrative NarrativeID `json:"narrative"`

	Presentation PresentationID `json:"presentation,omitempty"`
}

// Gate declares required durable identities, not a mutable credit balance.
type Gate struct {
	ID GateID `json:"id"`

	Deeds []DeedID `json:"deeds"`

	Narratives []NarrativeID `json:"narratives"`

	Defeated []EncounterID `json:"defeated"`

	RequirePermit bool `json:"require_permit,omitempty"`

	RequireConsent bool `json:"require_consent,omitempty"`
}

// FontSet references the three required Noto Sans presentation identities.
// The records do not assert that final font files or licenses have been delivered.
type FontSet struct {
	Regular PresentationID `json:"regular"`

	SemiBold PresentationID `json:"semi_bold"`

	Bold PresentationID `json:"bold"`
}

// Presentation declares an identity, not an assertion that a final export exists.
type Presentation struct {
	ID PresentationID `json:"id"`
}

// Mode controls the scope of definition validation, not release qualification.
type Mode uint8

const (
	// Incremental validates all supplied definitions and their references.
	Incremental Mode = iota
	// Full requires the canonical authored inventory and progression references.
	// It does not qualify external media or physical route connectivity.
	Full
)

// Catalogue owns a validated snapshot; callers receive independent copies.
type Catalogue struct {
	definitions Definitions

	mode Mode
}

// Mode reports the scope actually checked, not product or media readiness.
func (catalogue *Catalogue) Mode() Mode { return catalogue.mode }

// Definitions returns a deep copy of the validated authored document.
func (catalogue *Catalogue) Definitions(ctx context.Context) Definitions {
	defer mon.Task()(&ctx)(nil)

	return cloneDefinitions(ctx, catalogue.definitions)
}

func cloneDefinitions(ctx context.Context, source Definitions) Definitions {
	defer mon.Task()(&ctx)(nil)

	copy := source
	copy.Locations = append([]Location(nil), source.Locations...)
	copy.Encounters = append([]Encounter(nil), source.Encounters...)
	for i := range copy.Encounters {
		copy.Encounters[i].Solids = append([]Rect(nil), source.Encounters[i].Solids...)
		copy.Encounters[i].Lethal = append([]Rect(nil), source.Encounters[i].Lethal...)
	}
	copy.Objects = append([]Object(nil), source.Objects...)
	copy.Anchors = append([]Anchor(nil), source.Anchors...)
	copy.Narratives = append([]Narrative(nil), source.Narratives...)
	for i := range copy.Narratives {
		copy.Narratives[i].Prerequisites = append([]NarrativeID(nil), source.Narratives[i].Prerequisites...)
	}
	copy.Deeds = append([]Deed(nil), source.Deeds...)
	for i := range copy.Deeds {
		copy.Deeds[i].Prerequisites = append([]DeedID(nil), source.Deeds[i].Prerequisites...)
		copy.Deeds[i].Objects = append([]ObjectID(nil), source.Deeds[i].Objects...)
		copy.Deeds[i].Narratives = append([]NarrativeID(nil), source.Deeds[i].Narratives...)
		copy.Deeds[i].Defeated = append([]EncounterID(nil), source.Deeds[i].Defeated...)
	}
	copy.Consequences = append([]Consequence(nil), source.Consequences...)
	copy.Emergencies = append([]Emergency(nil), source.Emergencies...)
	copy.Endings = append([]Ending(nil), source.Endings...)
	copy.Gates = append([]Gate(nil), source.Gates...)
	for i := range copy.Gates {
		copy.Gates[i].Deeds = append([]DeedID(nil), source.Gates[i].Deeds...)
		copy.Gates[i].Narratives = append([]NarrativeID(nil), source.Gates[i].Narratives...)
		copy.Gates[i].Defeated = append([]EncounterID(nil), source.Gates[i].Defeated...)
	}
	copy.Presentations = append([]Presentation(nil), source.Presentations...)
	return copy
}

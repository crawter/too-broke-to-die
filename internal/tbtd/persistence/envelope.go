// Copyright (C) 2026 crawter
// All rights reserved.

// Package persistence validates complete campaign revisions independently of
// filesystem installation. A checksum detects accidental damage, not tampering.
package persistence

import (
	"encoding/json"
	"time"

	monkit "github.com/spacemonkeygo/monkit/v3"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
)

var mon = monkit.Package()

const (
	// SchemaVersion is the only supported save schema.
	SchemaVersion = 1

	// MaxRevisionBytes bounds a complete revision; it does not truncate history.
	MaxRevisionBytes = 64 << 20
	magic            = "TBTD-SAVE"
)

// Compatibility identifies the content and migration contract used to write a revision.
type Compatibility struct {
	ProductVersion string `json:"product_version"`

	ContentVersion string `json:"content_version"`

	ManifestDigest string `json:"manifest_digest"`

	RequiredMigration uint64 `json:"required_migration"`
}

// ContentIdentity is an explicit declaration that the same domain identities
// and rules can be interpreted without migration. It does not rename IDs.
type ContentIdentity struct {
	ContentVersion string

	ManifestDigest string

	RequiredMigration uint64
}

// Guidance contains durable player guidance, not partial gameplay evidence.
type Guidance struct {
	Tracked content.DeedID `json:"tracked"`

	Tutorials []content.PresentationID `json:"tutorials"`

	FirstLaunchNotice bool `json:"first_launch_notice"`
}

// Metadata is informational and does not affect progression or compatibility.
type Metadata struct {
	LastWriteUTC time.Time `json:"last_write_utc"`

	BuildDescription string `json:"build_description"`
}

// Revision is one complete independently validated campaign. Initial snapshots
// may have no internal campaign ID until their first identified operation.
type Revision struct {
	CampaignID string

	Number uint64

	Compatibility Compatibility

	Snapshot campaign.Snapshot

	// Operations lists every committed aggregate identity in sequence order.
	// Operation is the latest such identity, or zero for a new campaign.
	Operations []session.OperationID

	Operation session.OperationID

	Guidance Guidance

	Metadata Metadata

	PayloadDigest string

	PayloadLength uint64
}

type envelope struct {
	Magic string `json:"magic"`

	Schema int `json:"schema_version"`

	CampaignID string `json:"campaign_id"`

	Revision uint64 `json:"revision"`

	PayloadLength uint64 `json:"payload_length"`

	PayloadDigest string `json:"payload_sha256"`

	Compatibility Compatibility `json:"compatibility"`

	Payload json.RawMessage `json:"payload"`
}

type recovery struct {
	Location content.LocationID `json:"location"`

	Anchor content.AnchorID `json:"anchor"`

	Mode string `json:"mode"`

	SceneEntry string `json:"scene_entry"`
}

// ordering preserves the campaign's operation-result digest, which currently
// includes insertion order. Wire records are sorted; these permutations restore
// their original order before invoking the campaign contract.
type ordering struct {
	Attempts []int `json:"attempts"`

	Defeated []int `json:"defeated"`

	Consequences []int `json:"consequences"`

	Emergencies []int `json:"emergencies"`

	EncounterEvents []int `json:"encounter_events"`

	Incidents []int `json:"incidents"`

	CriticalItems []int `json:"critical_items"`
}

type operation struct {
	Event string `json:"event"`

	Sequence uint64 `json:"sequence"`
}

type payload struct {
	State campaign.State `json:"state"`

	Operation operation `json:"operation"`

	Operations []operation `json:"operations"`

	Order ordering `json:"order"`

	Recovery recovery `json:"recovery"`

	Guidance Guidance `json:"guidance"`

	Metadata Metadata `json:"metadata"`
}

// RevisionError reports why this particular revision cannot be used. Schema,
// campaign and compatibility fields are populated only when safely readable;
// none imply that the revision is playable.
type RevisionError struct {
	Code session.ErrorCode

	Schema int

	CampaignID string

	Compatibility Compatibility

	Cause error
}

func (failure *RevisionError) Error() string {
	if failure.Cause == nil {
		return failure.Code.Error()
	}
	return failure.Code.Error() + ": " + failure.Cause.Error()
}

func (failure *RevisionError) Unwrap() error { return failure.Cause }

// Is allows callers to distinguish corruption, too-new schemas and content
// incompatibility with errors.Is without depending on diagnostic text.
func (failure *RevisionError) Is(target error) bool { return target == failure.Code }

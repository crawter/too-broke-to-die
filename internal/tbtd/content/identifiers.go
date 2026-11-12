// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"regexp"
	"strings"
)

// LocationID identifies a campaign location independently of its display name.
type LocationID string

// EncounterID identifies an authored encounter independently of its filename.
type EncounterID string

// ObjectID identifies an authored interactive object or actor.
type ObjectID string

// AnchorID identifies a named recovery point.
type AnchorID string

// NarrativeID identifies a durable story milestone.
type NarrativeID string

// DeedID identifies one non-repeatable credit source.
type DeedID string

// ConsequenceID identifies the aftermath associated with a deed.
type ConsequenceID string

// EmergencyID identifies a shared batch emergency.
type EmergencyID string

// EndingID identifies a canonical ending stage.
type EndingID string

// GateID identifies a campaign access or progression gate.
type GateID string

// PresentationID identifies a presentation resource independently of its path.
type PresentationID string

var (
	identifierPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+$`)
	deedPattern       = regexp.MustCompile(`^D-(HOS|CHU|MIN|INS)-(END|(SUP|RES|UTL|PRV)-0[1-6])$`)
	locationPattern   = regexp.MustCompile(`^LOC-(HOS|CHU|MIN|INS)$`)
	encounterPattern  = regexp.MustCompile(`^ENC-((HOS|CHU|MIN|INS)-0[1-5]|PRO-0[1-3])$`)
	anchorPattern     = regexp.MustCompile(`^ANC-((HOS|CHU|MIN|INS)-(0[1-5]|OUT)|PRO-0[1-3]|MIN-PERMIT|END-(DEATH|HELL|CEREMONY|TABLEAU))$`)
	narrativePattern  = regexp.MustCompile(`^NAR-(0[1-9]|1[0-2])$`)
	emergencyPattern  = regexp.MustCompile(`^EM-(HOS|CHU|MIN|INS)-(SUP|RES|UTL|PRV)$`)
	endingPattern     = regexp.MustCompile(`^END-(REVIEW|CONSENT|DEATH|HELL|CEREMONY|TABLEAU)$`)
	gatePattern       = regexp.MustCompile(`^G-((HOS|CHU|MIN|INS)-IN|INS-FINAL|PERMIT|DEATH)$`)
)

func validID(id, prefix string) bool {
	return len(id) <= 160 && strings.HasPrefix(id, prefix+"-") && identifierPattern.MatchString(id)
}

func validPresentationID(id string) bool {
	for _, prefix := range []string{"ART", "AN", "FX", "UI", "MUS", "AMB", "SFX", "STG", "VOC", "TXT", "DLG", "TUT", "CAP", "FONT"} {
		if validID(id, prefix) {
			return true
		}
	}
	return false
}

// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"

	"too-broke-to-die/internal/tbtd/content"
)

// OccurrenceID is assigned once by the session for a pending operation.
type OccurrenceID struct {
	CampaignID string `json:"campaign_id"`

	Sequence uint64 `json:"sequence"`

	Ordinal uint32 `json:"ordinal"`
}

// IncidentKind distinguishes the durable history from transient contact.
type IncidentKind string

const (
	// IncidentDeath is one temporary gameplay death and its fee.
	IncidentDeath IncidentKind = "death"

	// IncidentWaste is the first reporting-station charge in an attempt.
	IncidentWaste IncidentKind = "waste"

	// IncidentAssault is the first registered assault on a victim in an attempt.
	IncidentAssault IncidentKind = "assault"

	// IncidentFailure records a failed evidence condition without a fee.
	IncidentFailure IncidentKind = "failure"
)

// Incident preserves the first causal contact and its exact fee. Markers are
// derived from this history, including after the active attempt is reset.
type Incident struct {
	ID OccurrenceID `json:"id"`

	Kind IncidentKind `json:"kind"`

	Deed content.DeedID `json:"deed,omitempty"`

	Attempt uint64 `json:"attempt,omitempty"`

	Victim string `json:"victim,omitempty"`

	Detail string `json:"detail,omitempty"`

	Cents int64 `json:"cents"`
}

// Contact is a reporting-station use or a registered civilian assault. Detail
// identifies the reporting station or the causal action, respectively.
type Contact struct {
	Kind IncidentKind `json:"kind"`

	Victim string `json:"victim,omitempty"`

	Detail string `json:"detail"`
}

var campaignIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func addCents(total, amount int64) (int64, error) {
	if total < 0 || amount < 0 || total > math.MaxInt64-amount {
		return 0, fmt.Errorf("%w: debt overflow or negative amount", ErrInvalid)
	}
	return total + amount, nil
}

// Debt derives integer cents from the full incident history, never a balance.
func (snapshot Snapshot) Debt(ctx context.Context) (_ int64, err error) {
	defer mon.Task()(&ctx)(&err)
	var total int64
	for _, incident := range snapshot.data.Incidents {
		total, err = addCents(total, incident.Cents)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func (campaign *Campaign) validateIncidents(ctx context.Context, data State) (err error) {
	defer mon.Task()(&ctx)(&err)
	if data.OperationSequence == 0 {
		if data.CampaignID != "" || data.LatestOperation != nil || len(data.Incidents) != 0 || len(data.EncounterEvents) != 0 || len(data.CriticalItems) != 0 {
			return fmt.Errorf("%w: operation metadata", ErrInvalid)
		}
	} else if !campaignIDPattern.MatchString(data.CampaignID) || data.LatestOperation == nil || data.LatestOperation.Sequence != data.OperationSequence || !digestPattern.MatchString(data.LatestOperation.Digest) || !digestPattern.MatchString(data.LatestOperation.ResultDigest) {
		return fmt.Errorf("%w: operation metadata", ErrInvalid)
	}
	seen := make(map[OccurrenceID]bool)
	seenEncounters := make(map[content.EncounterID]bool)
	for _, event := range data.EncounterEvents {
		id := event.ID
		if id.CampaignID != data.CampaignID || id.Sequence == 0 || id.Sequence > data.OperationSequence || id.Ordinal == 0 || seen[id] || seenEncounters[event.Encounter] || !has(data.Defeated, event.Encounter) {
			return fmt.Errorf("%w: encounter occurrence", ErrInvalid)
		}
		seen[id] = true
		seenEncounters[event.Encounter] = true
	}
	seenItems := make(map[CriticalItemID]bool)
	for _, record := range data.CriticalItems {
		definition, ok := campaign.items[record.Item]
		acquired := record.Acquired
		if !ok || seenItems[record.Item] || acquired.CampaignID != data.CampaignID || acquired.Sequence == 0 || acquired.Sequence > data.OperationSequence || acquired.Ordinal == 0 || seen[acquired] || !campaign.itemAnchorReachable(ctx, data, definition.AcquireAnchor) {
			return fmt.Errorf("%w: critical item acquisition", ErrInvalid)
		}
		seenItems[record.Item] = true
		seen[acquired] = true
		consumed := record.Consumed
		if consumed == (OccurrenceID{}) {
			continue
		}
		if consumed.CampaignID != data.CampaignID || consumed.Sequence > data.OperationSequence || consumed.Ordinal == 0 || seen[consumed] || consumed.Sequence < acquired.Sequence || (consumed.Sequence == acquired.Sequence && consumed.Ordinal <= acquired.Ordinal) || !campaign.itemAnchorReachable(ctx, data, definition.ConsumeAnchor) {
			return fmt.Errorf("%w: critical item consumption", ErrInvalid)
		}
		seen[consumed] = true
	}
	markers := make(map[string]bool)
	deathSequences := make(map[uint64]bool)
	var total int64
	for _, incident := range data.Incidents {
		id := incident.ID
		if id.CampaignID != data.CampaignID || id.Sequence == 0 || id.Sequence > data.OperationSequence || id.Ordinal == 0 || seen[id] {
			return fmt.Errorf("%w: incident identity", ErrInvalid)
		}
		seen[id] = true
		if incident.Deed != "" {
			if _, ok := campaign.deeds[incident.Deed]; !ok || incident.Attempt == 0 || incident.Attempt > attemptNumber(data.Attempts, incident.Deed) {
				return fmt.Errorf("%w: incident attempt", ErrInvalid)
			}
		} else if incident.Attempt != 0 {
			return fmt.Errorf("%w: incident attempt", ErrInvalid)
		}
		switch incident.Kind {
		case IncidentDeath:
			if incident.Cents != 4999 || incident.Victim != "" || strings.TrimSpace(incident.Detail) == "" || deathSequences[id.Sequence] {
				return fmt.Errorf("%w: death incident", ErrInvalid)
			}
			deathSequences[id.Sequence] = true
		case IncidentWaste, IncidentAssault:
			if incident.Deed == "" || strings.TrimSpace(incident.Detail) == "" {
				return fmt.Errorf("%w: contact context", ErrInvalid)
			}
			if (incident.Kind == IncidentWaste && (incident.Cents != 999 || incident.Victim != "")) || (incident.Kind == IncidentAssault && (incident.Cents != 1999 || strings.TrimSpace(incident.Victim) == "")) {
				return fmt.Errorf("%w: contact fee", ErrInvalid)
			}
			marker := fmt.Sprintf("%s/%d/%s/%s", incident.Deed, incident.Attempt, incident.Kind, incident.Victim)
			if markers[marker] {
				return fmt.Errorf("%w: duplicate charge", ErrInvalid)
			}
			markers[marker] = true
		case IncidentFailure:
			if incident.Deed == "" || incident.Cents != 0 || incident.Victim != "" || campaign.deeds[incident.Deed].Family == "END" || incident.Detail != campaign.deeds[incident.Deed].Failure {
				return fmt.Errorf("%w: failure incident", ErrInvalid)
			}
		default:
			return fmt.Errorf("%w: incident kind", ErrInvalid)
		}
		var err error
		total, err = addCents(total, incident.Cents)
		if err != nil {
			return err
		}
	}
	for _, attempt := range data.Attempts {
		if attempt.Failed == "" {
			continue
		}
		found := false
		for _, incident := range data.Incidents {
			if incident.Kind == IncidentFailure && incident.Deed == attempt.Deed && incident.Attempt == attempt.Number {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%w: missing failure history", ErrInvalid)
		}
	}
	return nil
}

// addContact is the per-contact helper measured by the enclosing Prepare task.
func addContact(data *State, attempt Attempt, contact Contact, id OccurrenceID) error {
	if attempt.Deed == "" || strings.TrimSpace(contact.Detail) == "" {
		return fmt.Errorf("%w: contact without attempt or cause", ErrInvalid)
	}
	var cents int64
	switch contact.Kind {
	case IncidentWaste:
		if contact.Victim != "" {
			return fmt.Errorf("%w: waste victim", ErrInvalid)
		}
		cents = 999
	case IncidentAssault:
		if strings.TrimSpace(contact.Victim) == "" {
			return fmt.Errorf("%w: assault victim", ErrInvalid)
		}
		cents = 1999
	default:
		return fmt.Errorf("%w: contact kind", ErrInvalid)
	}
	for _, existing := range data.Incidents {
		if existing.Deed == attempt.Deed && existing.Attempt == attempt.Number && existing.Kind == contact.Kind && existing.Victim == contact.Victim {
			return nil
		}
	}
	data.Incidents = append(data.Incidents, Incident{ID: id, Kind: contact.Kind, Deed: attempt.Deed, Attempt: attempt.Number, Victim: contact.Victim, Detail: contact.Detail, Cents: cents})
	return nil
}

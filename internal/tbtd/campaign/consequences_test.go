// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"too-broke-to-die/internal/tbtd/content"
)

const testCampaignID = "01234567-89ab-4cde-8fab-0123456789ab"

func next(snapshot Snapshot) Transition {
	return Transition{CampaignID: testCampaignID, Sequence: snapshot.data.OperationSequence + 1}
}

func prepare(t *testing.T, campaign *Campaign, snapshot Snapshot, event Transition) Snapshot {
	t.Helper()
	result, err := campaign.Prepare(context.Background(), snapshot, event)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	return result
}

func startedAttempt(t *testing.T) (*Campaign, *content.Campaign, Snapshot) {
	t.Helper()
	ctx := context.Background()
	campaign, manifest := fixture(t)
	started, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	active, err := campaign.Accept(ctx, started, "D-HOS-SUP-01")
	if err != nil {
		t.Fatal(err)
	}
	return campaign, manifest, active
}

func debt(t *testing.T, snapshot Snapshot) int64 {
	t.Helper()
	amount, err := snapshot.Debt(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return amount
}

func TestPenaltiesAttemptRestorationAndReset(t *testing.T) {
	ctx := context.Background()
	campaign, _, active := startedAttempt(t)
	first := next(active)
	first.Contacts = []Contact{
		{Kind: IncidentWaste, Detail: "refill A"},
		{Kind: IncidentWaste, Detail: "refill B"},
		{Kind: IncidentAssault, Victim: "nina", Detail: "limb"},
		{Kind: IncidentAssault, Victim: "nina", Detail: "contact again"},
		{Kind: IncidentAssault, Victim: "pyotr", Detail: "projectile"},
	}
	first.Death = DeathGameplay
	first.DeathDetail = "steam"
	proposal := prepare(t, campaign, active, first)
	if active.Credits() != 0 || debt(t, active) != 0 || len(active.Data(ctx).Incidents) != 0 {
		t.Fatal("proposal changed the committed input")
	}
	if got := debt(t, proposal); got != 999+1999+1999+4999 {
		t.Fatalf("first fees: %d", got)
	}
	if incidents := proposal.Data(ctx).Incidents; len(incidents) != 4 || incidents[0].Detail != "refill A" || incidents[3].ID.Ordinal != 7 {
		t.Fatalf("causal history: %+v", incidents)
	}
	// A committed replay neither duplicates charges nor changes any identity.
	replay := prepare(t, campaign, proposal, first)
	if !reflect.DeepEqual(replay.Data(ctx), proposal.Data(ctx)) {
		t.Fatal("replay changed result")
	}
	conflict := first
	conflict.DeathDetail = "different hazard"
	if _, err := campaign.Prepare(ctx, proposal, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting operation: %v", err)
	}
	restored, err := campaign.Restore(ctx, proposal.Data(ctx))
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok := restored.ActiveAttempt()
	if !ok || attempt.Number != 1 {
		t.Fatalf("restore attempt: %+v", attempt)
	}
	again := next(restored)
	again.Contacts = []Contact{{Kind: IncidentWaste, Detail: "third station"}, {Kind: IncidentAssault, Victim: "nina", Detail: "again"}}
	restored = prepare(t, campaign, restored, again)
	if debt(t, restored) != debt(t, proposal) {
		t.Fatal("restore charged existing markers")
	}
	if _, err = campaign.Prepare(ctx, restored, first); !errors.Is(err, ErrConflict) {
		t.Fatalf("old sequence reused: %v", err)
	}
	reset, err := campaign.Reset(ctx, restored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = campaign.Prepare(ctx, reset, again); !errors.Is(err, ErrConflict) {
		t.Fatalf("replayed an operation after its result changed: %v", err)
	}
	attempt, _ = reset.ActiveAttempt()
	if attempt.Number != 2 || debt(t, reset) != debt(t, restored) {
		t.Fatalf("reset lost history: %+v", attempt)
	}
	newCharges := next(reset)
	newCharges.Contacts = []Contact{{Kind: IncidentWaste, Detail: "refill B"}, {Kind: IncidentAssault, Victim: "nina", Detail: "limb"}}
	newCharges.Death = DeathGameplay
	newCharges.DeathDetail = "another death"
	charged := prepare(t, campaign, reset, newCharges)
	if debt(t, charged) != debt(t, proposal)+999+1999+4999 || len(charged.Data(ctx).Incidents) != 7 {
		t.Fatalf("new attempt fee: %+v", charged.Data(ctx).Incidents)
	}
	if WorkResetWarning != "Незачтённый прогресс попытки будет сброшен. Зачтённые дела и штрафы сохраняются" {
		t.Fatal("missing rollback warning")
	}
	abandoned, err := campaign.Abandon(ctx, charged)
	if err != nil {
		t.Fatal(err)
	}
	other, err := campaign.Accept(ctx, abandoned, "D-HOS-SUP-02")
	if err != nil || debt(t, other) != debt(t, charged) {
		t.Fatalf("switch lost incidents: %v", err)
	}
}

func TestDeathExclusionsAndInvalidContacts(t *testing.T) {
	ctx := context.Background()
	campaign, _, snapshot := startedAttempt(t)
	for _, cause := range []DeathCause{DeathNone, DeathHeartAttack, DeathFinal, DeathCheckpoint, DeathRestoration, DeathUnclean} {
		event := next(snapshot)
		event.Death = cause
		snapshot = prepare(t, campaign, snapshot, event)
		if debt(t, snapshot) != 0 || len(snapshot.Data(ctx).Incidents) != 0 {
			t.Fatalf("excluded death charged: %s", cause)
		}
	}
	for _, contact := range []Contact{
		{Kind: IncidentWaste},
		{Kind: IncidentWaste, Detail: "station", Victim: "nina"},
		{Kind: IncidentAssault, Detail: "hit"},
		{Kind: IncidentDeath, Detail: "hit"},
	} {
		event := next(snapshot)
		event.Contacts = []Contact{contact}
		if _, err := campaign.Prepare(ctx, snapshot, event); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid contact %+v: %v", contact, err)
		}
	}
	for _, event := range []Transition{
		{CampaignID: testCampaignID, Sequence: snapshot.data.OperationSequence + 1, Death: DeathGameplay},
		{CampaignID: testCampaignID, Sequence: snapshot.data.OperationSequence + 1, Death: DeathCause("unknown")},
		{CampaignID: "not-a-uuid", Sequence: snapshot.data.OperationSequence + 1},
	} {
		if _, err := campaign.Prepare(ctx, snapshot, event); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid transition: %v", err)
		}
	}
	if _, err := addCents(math.MaxInt64-4999, 4999); err != nil {
		t.Fatal(err)
	}
	for _, amounts := range [][2]int64{{math.MaxInt64 - 4998, 4999}, {-1, 1}, {1, -1}} {
		if _, err := addCents(amounts[0], amounts[1]); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted overflowing or negative debt: %v", amounts)
		}
	}
	overflow := Snapshot{data: State{Incidents: []Incident{{Cents: math.MaxInt64}, {Cents: 1}}}}
	if _, err := overflow.Debt(ctx); !errors.Is(err, ErrInvalid) {
		t.Fatalf("debt wrapped: %v", err)
	}
}

func TestIncidentValidationAndDeathWithoutTicket(t *testing.T) {
	ctx := context.Background()
	campaign, _ := fixture(t)
	started, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	contact := next(started)
	contact.Contacts = []Contact{{Kind: IncidentWaste, Detail: "station"}}
	if _, err = campaign.Prepare(ctx, started, contact); !errors.Is(err, ErrInvalid) {
		t.Fatalf("charge without attempt: %v", err)
	}
	death := next(started)
	death.Death = DeathGameplay
	death.DeathDetail = "fall"
	snapshot := prepare(t, campaign, started, death)
	if debt(t, snapshot) != 4999 || snapshot.Data(ctx).Incidents[0].Deed != "" {
		t.Fatal("death outside a ticket was not charged once")
	}
	for name, mutate := range map[string]func(*State){
		"fee": func(state *State) { state.Incidents[0].Cents = 1 },

		"identity": func(state *State) { state.Incidents[0].ID.Ordinal = 0 },

		"campaign": func(state *State) { state.Incidents[0].ID.CampaignID = "another" },

		"duplicate": func(state *State) { state.Incidents = append(state.Incidents, state.Incidents[0]) },

		"attempt": func(state *State) { state.Incidents[0].Attempt = 1 },

		"operation": func(state *State) { state.LatestOperation.Digest = "bad" },
	} {
		t.Run(name, func(t *testing.T) {
			data := snapshot.Data(ctx)
			mutate(&data)
			if _, err := campaign.Restore(ctx, data); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid incident accepted: %v", err)
			}
		})
	}
}

func TestFailureRestoreAndExplicitRetry(t *testing.T) {
	ctx := context.Background()
	campaign, manifest, snapshot := startedAttempt(t)
	event := next(snapshot)
	event.Failure = true
	event.Contacts = []Contact{{Kind: IncidentWaste, Detail: "refill"}}
	failed := prepare(t, campaign, snapshot, event)
	attempt, ok := failed.ActiveAttempt()
	if !ok || attempt.Failed == "" || len(failed.Data(ctx).Incidents) != 2 {
		t.Fatalf("failure not recorded: %+v", attempt)
	}
	if _, _, err := campaign.Apply(ctx, failed, candidate(t, manifest, attempt.Deed, attempt.Number)); !errors.Is(err, ErrBlocked) {
		t.Fatalf("failed evidence certified: %v", err)
	}
	restored, err := campaign.Restore(ctx, failed.Data(ctx))
	if err != nil {
		t.Fatal(err)
	}
	attempt, _ = restored.ActiveAttempt()
	if attempt.Number != 1 || attempt.Failed != "" || debt(t, restored) != 999 || len(restored.Data(ctx).Incidents) != 2 {
		t.Fatal("restore did not reset work while preserving failure history")
	}
	replayed := prepare(t, campaign, restored, event)
	if !reflect.DeepEqual(replayed.Data(ctx), restored.Data(ctx)) {
		t.Fatal("restoration caused the recorded failure to recur")
	}
	completed, _, err := campaign.Apply(ctx, restored, candidate(t, manifest, attempt.Deed, attempt.Number))
	if err != nil || completed.Credits() != 1 || debt(t, completed) != 999 {
		t.Fatalf("retry could not certify: %v", err)
	}
	reset, err := campaign.Reset(ctx, failed)
	if err != nil {
		t.Fatal(err)
	}
	attempt, _ = reset.ActiveAttempt()
	if attempt.Number != 2 || attempt.Failed != "" || debt(t, reset) != 999 {
		t.Fatal("explicit reset did not start a clean durable attempt")
	}
	if _, _, err = campaign.Apply(ctx, reset, candidate(t, manifest, attempt.Deed, 2)); err != nil {
		t.Fatalf("reset attempt cannot certify: %v", err)
	}
}

func TestReverseBatchEmergencyAndAftermath(t *testing.T) {
	ctx := context.Background()
	campaign, manifest := fixture(t)
	snapshot, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	batch := manifest.Batches(ctx)[0]
	for i := len(batch.Deeds) - 1; i >= 0; i-- {
		snapshot, _ = certify(t, campaign, manifest, snapshot, batch.Deeds[i])
		if len(snapshot.Data(ctx).Emergencies) != map[bool]int{true: 1, false: 0}[i == 0] {
			t.Fatalf("wrong emergency count on %d", i)
		}
	}
	view, err := campaign.EmergencyInfo(ctx, snapshot, batch.Emergency.ID)
	if err != nil || view.Cause != batch.Deeds[0] || view.WarningMilliseconds != 1500 || view.ActiveMilliseconds != 8000 || view.SafeRouteAndAftermath == "" || view.Anchor != batch.Emergency.Anchor {
		t.Fatalf("emergency view: %+v %v", view, err)
	}
	consequence, err := campaign.Consequence(ctx, snapshot, content.ConsequenceID("CON-"+string(batch.Deeds[0])))
	if err != nil || consequence.Deed != batch.Deeds[0] || consequence.Affected == "" || consequence.Aftermath == "" {
		t.Fatalf("consequence view: %+v %v", consequence, err)
	}
	restored, err := campaign.Restore(ctx, snapshot.Data(ctx))
	if err != nil {
		t.Fatal(err)
	}
	view, err = campaign.EmergencyInfo(ctx, restored, batch.Emergency.ID)
	if err != nil || view.Emergency.Stage != EmergencyActive || view.WarningMilliseconds != 1500 {
		t.Fatalf("restoration did not reconstruct full warning: %+v %v", view, err)
	}
	event := next(restored)
	event.Aftermath = batch.Emergency.ID
	if EmergencyTransitionID(event.Aftermath) != "EM-HOS-SUP-aftermath" {
		t.Fatal("bad aftermath identity")
	}
	finished := prepare(t, campaign, restored, event)
	if restored.Data(ctx).Emergencies[0].Stage != EmergencyActive || finished.Data(ctx).Emergencies[0].Stage != EmergencyAftermath || finished.Credits() != 6 {
		t.Fatal("aftermath lost receipt or mutated input")
	}
	replay := prepare(t, campaign, finished, event)
	if !reflect.DeepEqual(replay.Data(ctx), finished.Data(ctx)) {
		t.Fatal("aftermath replay changed state")
	}
	later := next(finished)
	later.Aftermath = batch.Emergency.ID
	finished = prepare(t, campaign, finished, later)
	if len(finished.Data(ctx).Emergencies) != 1 {
		t.Fatal("duplicate emergency")
	}
	bad := finished.Data(ctx)
	bad.Emergencies[0].Stage = "unknown"
	if _, err = campaign.Restore(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid emergency stage: %v", err)
	}
}

func TestDebtDoesNotBlockFullRoute(t *testing.T) {
	ctx := context.Background()
	campaign, manifest, snapshot := startedAttempt(t)
	event := next(snapshot)
	event.Contacts = []Contact{{Kind: IncidentWaste, Detail: "reporting station"}, {Kind: IncidentAssault, Victim: "civilian", Detail: "contact"}}
	event.Death = DeathGameplay
	event.DeathDetail = "hazard"
	snapshot = prepare(t, campaign, snapshot, event)
	for institution := 0; institution < 4; institution++ {
		for family := 0; family < 4; family++ {
			for _, id := range manifest.Batches(ctx)[institution*4+family].Deeds {
				snapshot, _ = certify(t, campaign, manifest, snapshot, id)
			}
		}
		closure := manifest.Deeds(ctx)[institution*25+24]
		var err error
		snapshot, err = campaign.DefeatBoss(ctx, snapshot, closure.Deed.Encounter)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, _ = certify(t, campaign, manifest, snapshot, closure.Deed.ID)
	}
	missing, err := campaign.MissingGate(ctx, snapshot, "G-PERMIT")
	if err != nil || len(missing) != 0 || snapshot.Credits() != 100 || debt(t, snapshot) != 999+1999+4999 {
		t.Fatalf("penalties blocked canonical route: missing=%v credit=%d err=%v", missing, snapshot.Credits(), err)
	}
}

func TestSameStepCompletionBossAndDeath(t *testing.T) {
	ctx := context.Background()
	campaign, manifest := fixture(t)
	snapshot, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	batches := manifest.Batches(ctx)
	for i := 0; i < 4; i++ {
		limit := 6
		if i == 3 {
			limit = 5
		}
		for _, id := range batches[i].Deeds[:limit] {
			snapshot, _ = certify(t, campaign, manifest, snapshot, id)
		}
	}
	id := batches[3].Deeds[5]
	active, err := campaign.Accept(ctx, snapshot, id)
	if err != nil {
		t.Fatal(err)
	}
	before := active.Data(ctx)
	certification := candidate(t, manifest, id, 1)
	event := next(active)
	event.Certification = &CertificationCandidate{Completed: true, Event: certification}
	event.Encounter = &EncounterCandidate{Completed: true, Encounter: "ENC-HOS-05"}
	event.Death = DeathGameplay
	event.DeathDetail = "lethal catastrophe"
	proposed := prepare(t, campaign, active, event)
	data := proposed.Data(ctx)
	if active.Credits() != 23 || !reflect.DeepEqual(before, active.Data(ctx)) || proposed.Credits() != 24 {
		t.Fatal("proposal changed committed receipt total")
	}
	if data.Anchor != "ANC-HOS-05" || len(data.Defeated) != 1 || data.Defeated[0] != "ENC-HOS-05" || len(data.EncounterEvents) != 1 || data.EncounterEvents[0].ID != (OccurrenceID{CampaignID: testCampaignID, Sequence: 1, Ordinal: 1}) || len(data.Emergencies) != 4 || data.Emergencies[3].Stage != EmergencyActive || len(data.Consequences) != 24 || debt(t, proposed) != 4999 {
		t.Fatalf("incomplete ordered outcome: %+v", data)
	}
	bad := proposed.Data(ctx)
	bad.EncounterEvents[0].ID = bad.Incidents[0].ID
	if _, err = campaign.Restore(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reused occurrence identity: %v", err)
	}
	replay := prepare(t, campaign, proposed, event)
	if !reflect.DeepEqual(replay.Data(ctx), data) || len(data.Incidents) != 1 || data.Incidents[0].Deed != id {
		t.Fatal("death duplicated or lost cause")
	}
	unfinished := next(active)
	unfinished.Certification = &CertificationCandidate{Event: certification}
	unfinished.Encounter = &EncounterCandidate{Encounter: "ENC-HOS-05"}
	unfinished.Death = DeathGameplay
	unfinished.DeathDetail = "unfinished action"
	partial := prepare(t, campaign, active, unfinished)
	if partial.Credits() != 23 || len(partial.Data(ctx).Defeated) != 0 || len(partial.Data(ctx).Emergencies) != 3 || partial.Data(ctx).Anchor != before.Anchor || debt(t, partial) != 4999 {
		t.Fatalf("unfinished action granted evidence: %+v", partial.Data(ctx))
	}
	ready, _, err := campaign.Apply(ctx, active, certification)
	if err != nil {
		t.Fatal(err)
	}
	bossOnly := next(ready)
	bossOnly.Certification = &CertificationCandidate{Event: candidate(t, manifest, "D-HOS-END", 1)}
	bossOnly.Encounter = &EncounterCandidate{Completed: true, Encounter: "ENC-HOS-05"}
	bossOnly.Death = DeathGameplay
	bossOnly.DeathDetail = "boss victory and lethal hit"
	bossResult := prepare(t, campaign, ready, bossOnly)
	if bossResult.Credits() != 24 || bossResult.Data(ctx).Anchor != "ANC-HOS-05" || len(bossResult.Data(ctx).Defeated) != 1 || debt(t, bossResult) != 4999 {
		t.Fatalf("unfinished closure or completed boss mishandled: %+v", bossResult.Data(ctx))
	}
}

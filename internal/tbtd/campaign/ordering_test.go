// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"too-broke-to-die/internal/tbtd/content"
)

func TestIdentifiedAttemptTransitions(t *testing.T) {
	ctx := context.Background()
	campaign, _ := fixture(t)
	started, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	accept := next(started)
	accept.Attempt = &AttemptChange{Action: AttemptAccept, Deed: "D-HOS-SUP-01"}
	accepted := prepare(t, campaign, started, accept)
	if _, active := started.ActiveAttempt(); active {
		t.Fatal("accept mutated committed input")
	}
	if attempt, ok := accepted.ActiveAttempt(); !ok || attempt.Number != 1 {
		t.Fatalf("bad acceptance: %+v", attempt)
	}
	replay := prepare(t, campaign, accepted, accept)
	if !reflect.DeepEqual(replay.Data(ctx), accepted.Data(ctx)) {
		t.Fatal("accept replay changed state")
	}
	conflicting := accept
	conflicting.Attempt = &AttemptChange{Action: AttemptAccept, Deed: "D-HOS-SUP-02"}
	if _, err := campaign.Prepare(ctx, accepted, conflicting); !errors.Is(err, ErrConflict) {
		t.Fatalf("accept payload conflict: %v", err)
	}
	fine := next(accepted)
	fine.Contacts = []Contact{{Kind: IncidentWaste, Detail: "station"}}
	charged := prepare(t, campaign, accepted, fine)
	reset := next(charged)
	reset.Attempt = &AttemptChange{Action: AttemptReset, Deed: "D-HOS-SUP-01"}
	resetResult := prepare(t, campaign, charged, reset)
	if attempt, _ := resetResult.ActiveAttempt(); attempt.Number != 2 || debt(t, resetResult) != 999 {
		t.Fatalf("reset lost history or failed to increment: %+v", attempt)
	}
	if attempt, _ := charged.ActiveAttempt(); attempt.Number != 1 {
		t.Fatal("reset mutated committed input")
	}
	replay = prepare(t, campaign, resetResult, reset)
	if !reflect.DeepEqual(replay.Data(ctx), resetResult.Data(ctx)) {
		t.Fatal("acknowledged reset was applied twice")
	}
	restored, err := campaign.Restore(ctx, resetResult.Data(ctx))
	if err != nil {
		t.Fatal(err)
	}
	replay = prepare(t, campaign, restored, reset)
	if attempt, _ := replay.ActiveAttempt(); attempt.Number != 2 {
		t.Fatal("restore or replay incremented the attempt")
	}
	conflicting = reset
	conflicting.Attempt = &AttemptChange{Action: AttemptAbandon, Deed: "D-HOS-SUP-01"}
	if _, err := campaign.Prepare(ctx, restored, conflicting); !errors.Is(err, ErrConflict) {
		t.Fatalf("reset identity reused: %v", err)
	}
	if _, err := campaign.Prepare(ctx, restored, accept); !errors.Is(err, ErrConflict) {
		t.Fatalf("old attempt operation replayed: %v", err)
	}
	second := next(restored)
	second.Attempt = &AttemptChange{Action: AttemptReset, Deed: "D-HOS-SUP-01"}
	secondResult := prepare(t, campaign, restored, second)
	if attempt, _ := secondResult.ActiveAttempt(); attempt.Number != 3 {
		t.Fatal("new reset did not start a new attempt")
	}
	wrong := next(secondResult)
	wrong.Attempt = &AttemptChange{Action: AttemptReset, Deed: "D-HOS-SUP-02"}
	if _, err := campaign.Prepare(ctx, secondResult, wrong); !errors.Is(err, ErrBlocked) {
		t.Fatalf("reset wrong ticket: %v", err)
	}
	abandon := next(secondResult)
	abandon.Attempt = &AttemptChange{Action: AttemptAbandon, Deed: "D-HOS-SUP-01"}
	switched := prepare(t, campaign, secondResult, abandon)
	acceptOther := next(switched)
	acceptOther.Attempt = &AttemptChange{Action: AttemptAccept, Deed: "D-HOS-SUP-02"}
	switched = prepare(t, campaign, switched, acceptOther)
	if attempt, ok := switched.ActiveAttempt(); !ok || attempt.Deed != "D-HOS-SUP-02" || debt(t, switched) != 999 {
		t.Fatalf("switch lost history: %+v", attempt)
	}
}

func TestCriticalItemsAtomicAcquisitionConsumptionAndDeath(t *testing.T) {
	ctx := context.Background()
	_, manifest := fixture(t)
	definition := CriticalItemDefinition{ID: "ITEM-TEST-KEY", AcquireAnchor: "ANC-HOS-01", ConsumeAnchor: "ANC-HOS-02", Route: "OBJ-HOS-RES-GATE"}
	campaign, err := NewWithInventory(ctx, manifest, []CriticalItemDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	started, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	unfinished := next(started)
	unfinished.CriticalItems = []CriticalItemCandidate{{Item: definition.ID, Action: CriticalAcquire}}
	partial := prepare(t, campaign, started, unfinished)
	if len(partial.Data(ctx).CriticalItems) != 0 || partial.Data(ctx).Anchor != "" || partial.Credits() != 0 {
		t.Fatal("unfinished item produced durable progress")
	}
	acquire := next(partial)
	acquire.CriticalItems = []CriticalItemCandidate{{Item: definition.ID, Action: CriticalAcquire, Completed: true}}
	acquire.Death = DeathGameplay
	acquire.DeathDetail = "pickup hazard"
	acquired := prepare(t, campaign, partial, acquire)
	if len(partial.Data(ctx).CriticalItems) != 0 || acquired.Data(ctx).Anchor != definition.AcquireAnchor || acquired.Credits() != 0 || debt(t, acquired) != 4999 {
		t.Fatal("acquisition, safe anchor or death was not atomic")
	}
	item := acquired.Data(ctx).CriticalItems[0]
	if item.Item != definition.ID || item.Acquired.Sequence != acquire.Sequence || item.Acquired.Ordinal != 1 || item.Consumed != (OccurrenceID{}) {
		t.Fatalf("bad acquisition: %+v", item)
	}
	restored, err := campaign.Restore(ctx, acquired.Data(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if replay := prepare(t, campaign, restored, acquire); !reflect.DeepEqual(replay.Data(ctx), restored.Data(ctx)) {
		t.Fatal("acquisition replay changed state")
	}
	consume := next(restored)
	consume.CriticalItems = []CriticalItemCandidate{{Item: definition.ID, Action: CriticalConsume, Completed: true}}
	if _, err := campaign.Prepare(ctx, restored, consume); !errors.Is(err, ErrBlocked) {
		t.Fatalf("consumption bypassed route prerequisites: %v", err)
	}
	for _, id := range manifest.Batches(ctx)[0].Deeds {
		restored, _ = certify(t, campaign, manifest, restored, id)
	}
	consume = next(restored)
	consume.CriticalItems = []CriticalItemCandidate{{Item: definition.ID, Action: CriticalConsume}}
	unfinishedConsume := prepare(t, campaign, restored, consume)
	if unfinishedConsume.Data(ctx).CriticalItems[0].Consumed != (OccurrenceID{}) || unfinishedConsume.Data(ctx).Anchor != restored.Data(ctx).Anchor {
		t.Fatal("unfinished consumption changed ownership or anchor")
	}
	if routes, err := campaign.ItemRoutes(ctx, unfinishedConsume); err != nil || len(routes) != 0 {
		t.Fatalf("unfinished consumption changed route: %v %v", routes, err)
	}
	restored = unfinishedConsume
	consume = next(restored)
	consume.CriticalItems = []CriticalItemCandidate{{Item: definition.ID, Action: CriticalConsume, Completed: true}}
	consume.Death = DeathGameplay
	consume.DeathDetail = "route hazard"
	consumed := prepare(t, campaign, restored, consume)
	routes, err := campaign.ItemRoutes(ctx, consumed)
	if err != nil || !reflect.DeepEqual(routes, []content.ObjectID{definition.Route}) || consumed.Data(ctx).Anchor != definition.ConsumeAnchor || consumed.Credits() != 6 || debt(t, consumed) != 9998 {
		t.Fatalf("consumption was not atomic: %v %v", routes, err)
	}
	if routes, err = campaign.ItemRoutes(ctx, restored); err != nil || len(routes) != 0 || restored.Data(ctx).CriticalItems[0].Consumed != (OccurrenceID{}) {
		t.Fatal("proposal mutated committed ownership or route")
	}
	replay := prepare(t, campaign, consumed, consume)
	if !reflect.DeepEqual(replay.Data(ctx), consumed.Data(ctx)) {
		t.Fatal("consumption replay duplicated item or death")
	}
	conflict := consume
	conflict.CriticalItems = []CriticalItemCandidate{{Item: definition.ID, Action: CriticalAcquire, Completed: true}}
	if _, err = campaign.Prepare(ctx, consumed, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("item operation conflict: %v", err)
	}
	duplicate := next(consumed)
	duplicate.CriticalItems = consume.CriticalItems
	if _, err = campaign.Prepare(ctx, consumed, duplicate); !errors.Is(err, ErrBlocked) {
		t.Fatalf("consumed item reused: %v", err)
	}
	copy := consumed.Data(ctx)
	copy.CriticalItems[0].Consumed = OccurrenceID{}
	if routes, err = campaign.ItemRoutes(ctx, consumed); err != nil || len(routes) != 1 {
		t.Fatal("inventory copy mutated snapshot")
	}
	forged := consumed.Data(ctx)
	forged.CriticalItems[0].Consumed = forged.CriticalItems[0].Acquired
	if _, err = campaign.Restore(ctx, forged); !errors.Is(err, ErrInvalid) {
		t.Fatalf("forged consumption accepted: %v", err)
	}
}

func TestCriticalItemAcquireAndConsumeInOneOperation(t *testing.T) {
	ctx := context.Background()
	_, manifest := fixture(t)
	definition := CriticalItemDefinition{ID: "ITEM-TEST-KEY", AcquireAnchor: "ANC-HOS-01", ConsumeAnchor: "ANC-HOS-02", Route: "OBJ-HOS-RES-GATE"}
	campaign, err := NewWithInventory(ctx, manifest, []CriticalItemDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range manifest.Batches(ctx)[0].Deeds {
		snapshot, _ = certify(t, campaign, manifest, snapshot, id)
	}
	event := next(snapshot)
	event.CriticalItems = []CriticalItemCandidate{
		{Item: definition.ID, Action: CriticalAcquire, Completed: true},
		{Item: definition.ID, Action: CriticalConsume, Completed: true},
	}
	event.Death = DeathGameplay
	event.DeathDetail = "lethal route hazard"
	result := prepare(t, campaign, snapshot, event)
	data := result.Data(ctx)
	if len(data.CriticalItems) != 1 || data.CriticalItems[0].Acquired.Ordinal != 1 || data.CriticalItems[0].Consumed.Ordinal != 2 || data.Anchor != definition.ConsumeAnchor || len(data.Incidents) != 1 || data.Incidents[0].ID.Ordinal != 4 || debt(t, result) != 4999 {
		t.Fatalf("item ordering or occurrence identities: %+v", data)
	}
	if len(snapshot.Data(ctx).CriticalItems) != 0 || result.Credits() != snapshot.Credits() {
		t.Fatal("proposal mutated input or awarded credit")
	}
}

func TestCriticalItemWithCertificationAndLethalDamage(t *testing.T) {
	ctx := context.Background()
	_, manifest := fixture(t)
	definition := CriticalItemDefinition{ID: "ITEM-TEST-KEY", AcquireAnchor: "ANC-HOS-01", ConsumeAnchor: "ANC-HOS-02", Route: "OBJ-HOS-RES-GATE"}
	campaign, err := NewWithInventory(ctx, manifest, []CriticalItemDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	active, err := campaign.Accept(ctx, snapshot, "D-HOS-SUP-01")
	if err != nil {
		t.Fatal(err)
	}
	event := next(active)
	event.Certification = &CertificationCandidate{Completed: true, Event: candidate(t, manifest, "D-HOS-SUP-01", 1)}
	event.CriticalItems = []CriticalItemCandidate{{Item: definition.ID, Action: CriticalAcquire, Completed: true}}
	event.Death = DeathGameplay
	event.DeathDetail = "simultaneous hazard"
	result := prepare(t, campaign, active, event)
	data := result.Data(ctx)
	if active.Credits() != 0 || result.Credits() != 1 || len(data.Consequences) != 1 || len(data.CriticalItems) != 1 || data.Anchor != definition.AcquireAnchor || debt(t, result) != 4999 {
		t.Fatalf("same-step completion was not atomic: %+v", data)
	}
	if data.CriticalItems[0].Acquired.Sequence != event.Sequence || data.Incidents[0].ID == data.CriticalItems[0].Acquired {
		t.Fatal("missing or duplicate occurrence identity")
	}
}

func TestFinalCertificationKeepsAnchorWithCriticalItems(t *testing.T) {
	ctx := context.Background()
	_, manifest := fixture(t)
	definition := CriticalItemDefinition{ID: "ITEM-TEST-KEY", AcquireAnchor: "ANC-HOS-01", ConsumeAnchor: "ANC-HOS-02", Route: "OBJ-HOS-RES-GATE"}
	campaign, err := NewWithInventory(ctx, manifest, []CriticalItemDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot = routeTo99(t, campaign, manifest, snapshot)
	active, err := campaign.Accept(ctx, snapshot, "D-INS-END")
	if err != nil {
		t.Fatal(err)
	}
	event := next(active)
	event.Certification = &CertificationCandidate{Completed: true, Event: candidate(t, manifest, "D-INS-END", 1)}
	event.CriticalItems = []CriticalItemCandidate{
		{Item: definition.ID, Action: CriticalAcquire, Completed: true},
		{Item: definition.ID, Action: CriticalConsume, Completed: true},
	}
	event.Death, event.DeathDetail = DeathGameplay, "simultaneous hazard"
	result := prepare(t, campaign, active, event)
	data := result.Data(ctx)
	if data.Anchor != "ANC-INS-OUT" || result.Credits() != 100 || data.Receipts[99].Certification.Deed != "D-INS-END" || data.Consequences[99] != "CON-D-INS-END" || !has(data.Narratives, content.NarrativeID("NAR-09")) || len(data.CriticalItems) != 1 || data.CriticalItems[0].Consumed.Sequence != event.Sequence || debt(t, result) != 4999 {
		t.Fatalf("final transaction lost an outcome: %+v", data)
	}
	routes, err := campaign.ItemRoutes(ctx, result)
	if err != nil || !reflect.DeepEqual(routes, []content.ObjectID{definition.Route}) {
		t.Fatalf("lost consumed route: %v %v", routes, err)
	}
	if active.Credits() != 99 || len(active.Data(ctx).CriticalItems) != 0 {
		t.Fatal("proposal changed committed state")
	}
	restored, err := campaign.Restore(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	replayed := prepare(t, campaign, restored, event)
	if !reflect.DeepEqual(replayed.Data(ctx), data) {
		t.Fatal("final transaction replay changed outcome")
	}
}

func TestCriticalItemCatalogueAndCandidates(t *testing.T) {
	ctx := context.Background()
	_, manifest := fixture(t)
	valid := CriticalItemDefinition{ID: "ITEM-TEST-KEY", AcquireAnchor: "ANC-HOS-01", ConsumeAnchor: "ANC-HOS-02", Route: "OBJ-HOS-RES-GATE"}
	for _, definitions := range [][]CriticalItemDefinition{
		{valid, valid},
		{{ID: "unknown", AcquireAnchor: valid.AcquireAnchor, ConsumeAnchor: valid.ConsumeAnchor, Route: valid.Route}},
		{{ID: valid.ID, AcquireAnchor: "ANC-UNKNOWN", ConsumeAnchor: valid.ConsumeAnchor, Route: valid.Route}},
		{{ID: valid.ID, AcquireAnchor: valid.AcquireAnchor, ConsumeAnchor: "ANC-HOS-01", Route: valid.Route}},
		{{ID: valid.ID, AcquireAnchor: valid.AcquireAnchor, ConsumeAnchor: valid.ConsumeAnchor, Route: "OBJ-HOS-RES-TERMINAL"}},
	} {
		if _, err := NewWithInventory(ctx, manifest, definitions); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid item definitions accepted: %+v: %v", definitions, err)
		}
	}
	campaign, err := NewWithInventory(ctx, manifest, []CriticalItemDefinition{valid})
	if err != nil {
		t.Fatal(err)
	}
	started, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []CriticalItemCandidate{
		{Item: "ITEM-NOPE", Action: CriticalAcquire, Completed: true},
		{Item: valid.ID, Action: "bad", Completed: true},
		{Item: valid.ID, Action: CriticalConsume, Completed: true},
	} {
		event := next(started)
		event.CriticalItems = []CriticalItemCandidate{candidate}
		if _, err := campaign.Prepare(ctx, started, event); err == nil {
			t.Fatalf("invalid item candidate accepted: %+v", candidate)
		}
	}
}

// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"too-broke-to-die/internal/tbtd/content"
)

// routeTo99 uses actual authored tickets, evidence, bosses and closure gates.
func routeTo99(t *testing.T, campaign *Campaign, manifest *content.Campaign, snapshot Snapshot) Snapshot {
	t.Helper()
	ctx := context.Background()
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
			t.Fatalf("boss %s: %v", closure.Deed.Encounter, err)
		}
		if institution < 3 {
			snapshot, _ = certify(t, campaign, manifest, snapshot, closure.Deed.ID)
		}
	}
	return snapshot
}

func TestPrologueMilestonesAndInvalidNarrative(t *testing.T) {
	ctx := context.Background()
	campaign, manifest := fixture(t)
	var snapshot Snapshot
	if _, err := campaign.CompleteNarrative(ctx, snapshot, "NAR-02"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("skipped theft: %v", err)
	}
	for _, step := range []struct {
		id content.NarrativeID

		anchor content.AnchorID
	}{
		{"NAR-01", "ANC-PRO-02"},
		{"NAR-02", "ANC-PRO-03"},
		{"NAR-03", "ANC-PRO-03"},
		{"NAR-04", "ANC-HOS-01"},
	} {
		var err error
		snapshot, err = campaign.CompleteNarrative(ctx, snapshot, step.id)
		if err != nil || snapshot.Data(ctx).Anchor != step.anchor {
			t.Fatalf("%s: %v", step.id, err)
		}
		repeated, err := campaign.CompleteNarrative(ctx, snapshot, step.id)
		if err != nil || !reflect.DeepEqual(repeated.Data(ctx), snapshot.Data(ctx)) {
			t.Fatalf("repeat %s: %v", step.id, err)
		}
	}
	if _, err := campaign.CompleteNarrative(ctx, snapshot, "NAR-09"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("independent closure narrative: %v", err)
	}
	if debt(t, snapshot) != 0 || snapshot.Credits() != 0 {
		t.Fatal("prologue produced a fee or credit")
	}
	snapshot = routeTo99(t, campaign, manifest, snapshot)
	for _, missing := range []content.NarrativeID{"NAR-01", "NAR-04", "NAR-05", "NAR-06", "NAR-07", "NAR-08"} {
		t.Run(string(missing), func(t *testing.T) {
			data := snapshot.Data(ctx)
			index := slices.Index(data.Narratives, missing)
			data.Narratives = slices.Delete(data.Narratives, index, index+1)
			if _, err := campaign.Restore(ctx, data); !errors.Is(err, ErrInvalid) {
				t.Fatalf("missing institution milestone %s accepted: %v", missing, err)
			}
		})
	}
	bad := snapshot.Data(ctx)
	bad.Narratives = append(bad.Narratives, "NAR-08")
	if _, err := campaign.Restore(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate narrative accepted: %v", err)
	}
}

func TestRequiredInstitutionMilestoneAtClosureBoundary(t *testing.T) {
	ctx := context.Background()
	campaign, manifest := fixture(t)
	snapshot, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for institution := 0; institution < 2; institution++ {
		for family := 0; family < 4; family++ {
			for _, id := range manifest.Batches(ctx)[institution*4+family].Deeds {
				snapshot, _ = certify(t, campaign, manifest, snapshot, id)
			}
		}
		closure := manifest.Deeds(ctx)[institution*25+24]
		snapshot, err = campaign.DefeatBoss(ctx, snapshot, closure.Deed.Encounter)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, _ = certify(t, campaign, manifest, snapshot, closure.Deed.ID)
	}
	data := snapshot.Data(ctx)
	if data.Narratives[len(data.Narratives)-1] != "NAR-06" {
		t.Fatal("church closure did not record its narrative")
	}
	data.Narratives = data.Narratives[:len(data.Narratives)-1]
	if _, err = campaign.Restore(ctx, data); !errors.Is(err, ErrInvalid) {
		t.Fatalf("accepted church closure without NAR-06: %v", err)
	}
}

func TestPreparedNarrativeIdentities(t *testing.T) {
	ctx := context.Background()
	campaign, _ := fixture(t)
	var snapshot Snapshot
	event := next(snapshot)
	event.Story = &StoryChange{Action: StoryNarrative, Narrative: "NAR-01"}
	first := prepare(t, campaign, snapshot, event)
	if first.Data(ctx).Anchor != "ANC-PRO-02" || first.Data(ctx).OperationSequence != 1 {
		t.Fatal("story milestone and operation not committed together")
	}
	plan, err := campaign.RestorePlan(ctx, first)
	if err != nil || plan.State != "ST-SCN" || plan.SceneEntry != "NAR-02" {
		t.Fatalf("unfinished scene recovery: %+v %v", plan, err)
	}
	restored, err := campaign.Restore(ctx, first.Data(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if replay := prepare(t, campaign, restored, event); !reflect.DeepEqual(replay.Data(ctx), restored.Data(ctx)) {
		t.Fatal("replayed story milestone changed state")
	}
	mixed := next(first)
	mixed.Story = &StoryChange{Action: StoryNarrative, Narrative: "NAR-02"}
	mixed.Death, mixed.DeathDetail = DeathGameplay, "unexpected"
	if _, err = campaign.Prepare(ctx, first, mixed); !errors.Is(err, ErrInvalid) {
		t.Fatalf("accepted a mixed story event: %v", err)
	}
	if _, err = campaign.CompleteNarrative(ctx, first, "NAR-08"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("accepted early institute milestone: %v", err)
	}
}

func TestPermitAndEndingBoundaries(t *testing.T) {
	ctx := context.Background()
	campaign, manifest := fixture(t)
	snapshot, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	// Real distinct deaths produce a large debt without changing eligibility.
	for i := 0; i < 40; i++ {
		death := next(snapshot)
		death.Death, death.DeathDetail = DeathGameplay, "hazard"
		snapshot = prepare(t, campaign, snapshot, death)
	}
	snapshot = routeTo99(t, campaign, manifest, snapshot)
	if snapshot.Credits() != 99 {
		t.Fatal("route did not reach exactly 99")
	}
	if _, err = campaign.IssuePermit(ctx, snapshot); !errors.Is(err, ErrBlocked) {
		t.Fatalf("permit at 99: %v", err)
	}
	if _, err = campaign.ConsentDecision(ctx, snapshot, true); !errors.Is(err, ErrBlocked) {
		t.Fatalf("consent without permit: %v", err)
	}
	snapshot, _ = certify(t, campaign, manifest, snapshot, "D-INS-END")
	if snapshot.Credits() != 100 || debt(t, snapshot) != 40*4999 {
		t.Fatal("final receipt or debt lost")
	}
	fullPlan, err := campaign.RestorePlan(ctx, snapshot)
	if err != nil || !slices.Equal(fullPlan.Installed, []content.DeedID{"D-INS-SUP-06", "D-INS-UTL-06"}) || len(fullPlan.Defeated) != 4 || !slices.Contains(fullPlan.Routes, content.GateID("G-INS-IN")) || len(fullPlan.Receipts) != 100 {
		t.Fatalf("full permanent state not restored: %+v %v", fullPlan, err)
	}
	issue := next(snapshot)
	issue.Story = &StoryChange{Action: StoryPermit}
	permit := prepare(t, campaign, snapshot, issue)
	if !permit.Data(ctx).Permit || permit.Data(ctx).Anchor != "ANC-MIN-PERMIT" {
		t.Fatal("permit was not issued at its safe anchor")
	}
	if replay := prepare(t, campaign, permit, issue); !reflect.DeepEqual(replay.Data(ctx), permit.Data(ctx)) {
		t.Fatal("permit retry changed the recorded operation")
	}
	conflict := issue
	conflict.Story = &StoryChange{Action: StoryConsent}
	if _, err = campaign.Prepare(ctx, permit, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("story identity reused with different payload: %v", err)
	}
	duplicate := next(permit)
	duplicate.Story = &StoryChange{Action: StoryPermit}
	if repeated := prepare(t, campaign, permit, duplicate); !reflect.DeepEqual(repeated.Data(ctx), permit.Data(ctx)) {
		t.Fatal("repeated permit issued a new event")
	}
	if snapshot.Data(ctx).Permit {
		t.Fatal("permit proposal changed committed snapshot")
	}
	again, err := campaign.IssuePermit(ctx, permit)
	if err != nil || !reflect.DeepEqual(again.Data(ctx), permit.Data(ctx)) {
		t.Fatalf("permit duplicated: %v", err)
	}
	cancelled, err := campaign.ConsentDecision(ctx, permit, false)
	if err != nil || !reflect.DeepEqual(cancelled.Data(ctx), permit.Data(ctx)) {
		t.Fatalf("cancel: %v", err)
	}
	// Ordinary revisits and gameplay remain possible after cancellation.
	checkpoint := next(cancelled)
	checkpoint.Checkpoint = "ANC-HOS-01"
	cancelled = prepare(t, campaign, cancelled, checkpoint)
	if replay := prepare(t, campaign, cancelled, checkpoint); !reflect.DeepEqual(replay.Data(ctx), cancelled.Data(ctx)) {
		t.Fatal("checkpoint retry changed state")
	}
	revisit, err := campaign.RestorePlan(ctx, cancelled)
	if err != nil || revisit.Anchor != "ANC-HOS-01" || revisit.Location != "LOC-HOS" || revisit.State != "ST-PAU" || revisit.Health != 100 || revisit.Limbs != 4 || revisit.Feces != 5 {
		t.Fatalf("revisit checkpoint recovery: %+v %v", revisit, err)
	}
	event := next(cancelled)
	event.Death, event.DeathDetail = DeathGameplay, "revisit"
	cancelled = prepare(t, campaign, cancelled, event)
	if debt(t, cancelled) != 41*4999 {
		t.Fatal("ordinary revisit not available")
	}
	confirm := next(cancelled)
	confirm.Story = &StoryChange{Action: StoryConsent}
	consent := prepare(t, campaign, cancelled, confirm)
	if !consent.Data(ctx).Consent || consent.Data(ctx).Anchor != "ANC-END-DEATH" || debt(t, consent) != debt(t, cancelled) {
		t.Fatal("confirmed consent lost its stage or charged a fee")
	}
	if replay := prepare(t, campaign, consent, confirm); !reflect.DeepEqual(replay.Data(ctx), consent.Data(ctx)) {
		t.Fatal("consent retry changed the recorded event")
	}
	if _, err = campaign.Prepare(ctx, consent, next(consent)); !errors.Is(err, ErrBlocked) {
		t.Fatalf("ordinary operation after consent: %v", err)
	}
	if _, err = campaign.Accept(ctx, consent, "D-HOS-SUP-01"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("ordinary play after consent: %v", err)
	}
	if _, err = campaign.Checkpoint(ctx, consent, "ANC-HOS-01"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("ordinary checkpoint after consent: %v", err)
	}
	if _, err = campaign.AdvanceEnding(ctx, consent, EndingHell); !errors.Is(err, ErrBlocked) {
		t.Fatalf("skipped death: %v", err)
	}
	if _, err = campaign.ReplayEnding(ctx, consent); !errors.Is(err, ErrBlocked) {
		t.Fatalf("premature replay: %v", err)
	}
	for _, stage := range []EndingStage{EndingDeath, EndingHell, EndingCeremony, EndingTableau} {
		plan, planErr := campaign.RestorePlan(ctx, consent)
		if planErr != nil || plan.State != "ST-SCN" || plan.Ending != stage || plan.Anchor != endingAnchor(stage) || plan.SceneEntry != string(stage) {
			t.Fatalf("restore stage %s: %+v %v", stage, plan, planErr)
		}
		before := consent
		advance := next(consent)
		advance.Story = &StoryChange{Action: StoryEnding, Stage: stage}
		consent = prepare(t, campaign, consent, advance)
		if replay := prepare(t, campaign, consent, advance); !reflect.DeepEqual(replay.Data(ctx), consent.Data(ctx)) {
			t.Fatalf("ending retry changed %s", stage)
		}
		if before.Data(ctx).Completed || before.Credits() != consent.Credits() || debt(t, before) != debt(t, consent) {
			t.Fatalf("advance %s mutated input or added events", stage)
		}
		repeat, repeatErr := campaign.AdvanceEnding(ctx, consent, stage)
		if repeatErr != nil || !reflect.DeepEqual(repeat.Data(ctx), consent.Data(ctx)) {
			t.Fatalf("repeat %s: %v", stage, repeatErr)
		}
	}
	if !consent.Data(ctx).Completed || !consent.Data(ctx).TableauAcknowledged {
		t.Fatal("tableau acknowledgment not committed")
	}
	plan, err := campaign.RestorePlan(ctx, consent)
	if err != nil || plan.State != "ST-END" {
		t.Fatalf("completed restore: %+v %v", plan, err)
	}
	before := consent.Data(ctx)
	replay, err := campaign.ReplayEnding(ctx, consent)
	if err != nil || !replay.ReadOnly || replay.Start != EndingDeath || !reflect.DeepEqual(before, consent.Data(ctx)) {
		t.Fatalf("read-only replay: %+v %v", replay, err)
	}
}

func TestEndingValidationRejectsInconsistentState(t *testing.T) {
	ctx := context.Background()
	campaign, manifest := fixture(t)
	snapshot, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot = routeTo99(t, campaign, manifest, snapshot)
	snapshot, _ = certify(t, campaign, manifest, snapshot, "D-INS-END")
	snapshot, err = campaign.IssuePermit(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = campaign.ConsentDecision(ctx, snapshot, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*State){
		"no permit": func(data *State) { data.Permit = false },

		"ordinary anchor": func(data *State) { data.Anchor = "ANC-INS-OUT" },

		"unknown stage": func(data *State) { data.Ending = "END-UNKNOWN" },

		"skipped hell": func(data *State) { data.Ending, data.Anchor = EndingCeremony, "ANC-END-CEREMONY" },

		"unacknowledged completion": func(data *State) { data.Completed = true },

		"early tableau acknowledgment": func(data *State) { data.TableauAcknowledged = true },

		"missing final receipt": func(data *State) { data.Receipts = data.Receipts[:99] },

		"missing NAR-09": func(data *State) { data.Narratives = data.Narratives[:8] },
	} {
		t.Run(name, func(t *testing.T) {
			data := snapshot.Data(ctx)
			mutate(&data)
			if _, err := campaign.Restore(ctx, data); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted inconsistent ending: %v", err)
			}
		})
	}
}

func TestRestorationPlanIsDeterministicAndPreservesHistory(t *testing.T) {
	ctx := context.Background()
	campaign, manifest := fixture(t)
	snapshot, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range manifest.Batches(ctx)[0].Deeds {
		snapshot, _ = certify(t, campaign, manifest, snapshot, id)
	}
	snapshot, err = campaign.Accept(ctx, snapshot, "D-HOS-RES-01")
	if err != nil {
		t.Fatal(err)
	}
	failure := next(snapshot)
	failure.Failure = true
	failure.Contacts = []Contact{{Kind: IncidentWaste, Detail: "reporting station"}}
	snapshot = prepare(t, campaign, snapshot, failure)
	before := snapshot.Data(ctx)
	checked, err := campaign.Checkpoint(ctx, snapshot, "ANC-HOS-01")
	if err != nil {
		t.Fatal(err)
	}
	checkedAttempt, _ := checked.ActiveAttempt()
	if checkedAttempt.Failed != "" || checkedAttempt.Number != 1 || debt(t, checked) != 999 || len(checked.Data(ctx).Incidents) != 2 || !reflect.DeepEqual(before, snapshot.Data(ctx)) {
		t.Fatal("checkpoint failed to clear only transient work")
	}
	if _, err = campaign.Checkpoint(ctx, snapshot, "ANC-INS-01"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unreached checkpoint accepted: %v", err)
	}
	plan, err := campaign.RestorePlan(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Location != "LOC-HOS" || plan.State != "ST-PAU" || plan.Anchor != "ANC-HOS-01" || plan.Health != 100 || plan.Limbs != 4 || plan.Feces != 5 || !plan.ClearTransient || !plan.ResetPartialBosses || plan.ActiveTicket != "D-HOS-RES-01" || len(plan.InitialObjects) == 0 || plan.InitialObjective == "" || len(plan.MandatoryActors) == 0 || len(plan.ProtectedTerminals) == 0 {
		t.Fatalf("incomplete restore plan: %+v", plan)
	}
	if !plan.ClearPositions || !plan.ClearProjectiles || !plan.ClearSmears || !plan.ClearCarriedLimbs || !plan.ClearEnemyDamage || !plan.ClearPartialEvidence || !plan.RestoreObjectOrigins || !plan.ResetEnemies {
		t.Fatal("transient restoration policy incomplete")
	}
	if len(plan.Emergencies) != 1 || plan.Emergencies[0].Emergency.Stage != EmergencyActive || plan.Emergencies[0].WarningMilliseconds != 1500 || !plan.Emergencies[0].BehindSafeBoundary || !plan.Emergencies[0].BypassUsable || plan.Emergencies[0].SafeRouteAndAftermath == "" {
		t.Fatalf("unsafe emergency restoration: %+v", plan.Emergencies)
	}
	restored, err := campaign.Restore(ctx, snapshot.Data(ctx))
	if err != nil {
		t.Fatal(err)
	}
	attempt, _ := restored.ActiveAttempt()
	if attempt.Number != 1 || attempt.Failed != "" || debt(t, restored) != 999 || restored.Credits() != 6 {
		t.Fatal("restoration lost durable state or kept failed work")
	}
	again, err := campaign.RestorePlan(ctx, restored)
	if err != nil || !reflect.DeepEqual(plan, again) || !reflect.DeepEqual(snapshot.Data(ctx), before) {
		t.Fatalf("nondeterministic or mutating restoration: %v", err)
	}
	plan.Receipts[0].Certification.EvidenceObjects[0] = "BAD"
	plan.InitialObjects[0] = "BAD"
	plan.MandatoryActors[0] = "BAD"
	if err = campaign.Validate(ctx, snapshot); err != nil {
		t.Fatalf("plan aliases snapshot: %v", err)
	}
	snapshot = restored
	for _, batch := range manifest.Batches(ctx)[1:4] {
		for _, id := range batch.Deeds {
			snapshot, _ = certify(t, campaign, manifest, snapshot, id)
		}
	}
	snapshot, err = campaign.DefeatBoss(ctx, snapshot, "ENC-HOS-05")
	if err != nil {
		t.Fatal(err)
	}
	plan, err = campaign.RestorePlan(ctx, snapshot)
	if err != nil || !slices.Equal(plan.Defeated, []content.EncounterID{"ENC-HOS-05"}) || !plan.ResetPartialBosses || plan.Anchor != "ANC-HOS-05" {
		t.Fatalf("boss recovery: %+v %v", plan, err)
	}
}

func TestCheckpointAtUnlockedEntrancesAndBossApproaches(t *testing.T) {
	ctx := context.Background()
	campaign, manifest := fixture(t)
	var initial Snapshot
	if _, err := campaign.Checkpoint(ctx, initial, "ANC-HOS-01"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("hospital entrance before the bargain: %v", err)
	}
	snapshot, err := campaign.Start(ctx, initial)
	if err != nil {
		t.Fatal(err)
	}
	verify := func(t *testing.T, current Snapshot, anchor content.AnchorID, location content.LocationID) {
		t.Helper()
		before := current.Data(ctx)
		checkpoint, checkpointErr := campaign.Checkpoint(ctx, current, anchor)
		if checkpointErr != nil {
			t.Fatalf("checkpoint %s: %v", anchor, checkpointErr)
		}
		if !reflect.DeepEqual(current.Data(ctx), before) {
			t.Fatalf("checkpoint %s changed committed snapshot", anchor)
		}
		restored, restoreErr := campaign.Restore(ctx, checkpoint.Data(ctx))
		if restoreErr != nil {
			t.Fatalf("restore %s: %v", anchor, restoreErr)
		}
		plan, planErr := campaign.RestorePlan(ctx, restored)
		if planErr != nil || plan.Anchor != anchor || plan.Location != location || plan.State != "ST-PAU" || plan.Health != 100 || plan.Limbs != 4 || plan.Feces != 5 {
			t.Fatalf("plan %s: %+v %v", anchor, plan, planErr)
		}
		before.Anchor = anchor
		if !reflect.DeepEqual(restored.Data(ctx), before) || len(plan.Receipts) != current.Credits() || !slices.Equal(plan.Defeated, current.Data(ctx).Defeated) {
			t.Fatalf("checkpoint %s fabricated or lost progress", anchor)
		}
	}
	codes := []string{"HOS", "CHU", "MIN", "INS"}
	for institution, code := range codes {
		location := content.LocationID("LOC-" + code)
		if institution < len(codes)-1 {
			locked := content.AnchorID("ANC-" + codes[institution+1] + "-01")
			if _, err = campaign.Checkpoint(ctx, snapshot, locked); !errors.Is(err, ErrInvalid) {
				t.Fatalf("unopened next institution %s: %v", locked, err)
			}
		}
		for family := 0; family < 4; family++ {
			anchor := content.AnchorID(fmt.Sprintf("ANC-%s-%02d", code, family+1))
			// No ticket from this batch has yet been certified.
			verify(t, snapshot, anchor, location)
			batch := manifest.Batches(ctx)[institution*4+family]
			for i, id := range batch.Deeds {
				if i == len(batch.Deeds)-1 {
					nextAnchor := content.AnchorID(fmt.Sprintf("ANC-%s-%02d", code, family+2))
					if _, err = campaign.Checkpoint(ctx, snapshot, nextAnchor); !errors.Is(err, ErrInvalid) {
						t.Fatalf("approach %s before all prerequisite receipts: %v", nextAnchor, err)
					}
				}
				snapshot, _ = certify(t, campaign, manifest, snapshot, id)
			}
		}
		boss := content.EncounterID("ENC-" + code + "-05")
		approach := content.AnchorID("ANC-" + code + "-05")
		out := content.AnchorID("ANC-" + code + "-OUT")
		if has(snapshot.Data(ctx).Defeated, boss) {
			t.Fatalf("%s was already defeated", boss)
		}
		verify(t, snapshot, approach, location)
		if _, err = campaign.Checkpoint(ctx, snapshot, out); !errors.Is(err, ErrInvalid) {
			t.Fatalf("closure anchor before victory %s: %v", out, err)
		}
		snapshot, err = campaign.DefeatBoss(ctx, snapshot, boss)
		if err != nil {
			t.Fatal(err)
		}
		verify(t, snapshot, approach, location)
		if _, err = campaign.Checkpoint(ctx, snapshot, out); !errors.Is(err, ErrInvalid) {
			t.Fatalf("closure anchor before receipt %s: %v", out, err)
		}
		snapshot, _ = certify(t, campaign, manifest, snapshot, content.DeedID("D-"+code+"-END"))
		verify(t, snapshot, out, location)
	}
}

func TestInventoryRestorationNeverRecreatesConsumedItems(t *testing.T) {
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
	acquire := next(snapshot)
	acquire.CriticalItems = []CriticalItemCandidate{{Item: definition.ID, Action: CriticalAcquire, Completed: true}}
	owned := prepare(t, campaign, snapshot, acquire)
	plan, err := campaign.RestorePlan(ctx, owned)
	if err != nil || !slices.Equal(plan.OwnedItems, []CriticalItemID{definition.ID}) || len(plan.ItemRoutes) != 0 || plan.Anchor != definition.AcquireAnchor {
		t.Fatalf("acquired item restore: %+v %v", plan, err)
	}
	for _, id := range manifest.Batches(ctx)[0].Deeds {
		owned, _ = certify(t, campaign, manifest, owned, id)
	}
	consume := next(owned)
	consume.CriticalItems = []CriticalItemCandidate{{Item: definition.ID, Action: CriticalConsume, Completed: true}}
	consumed := prepare(t, campaign, owned, consume)
	plan, err = campaign.RestorePlan(ctx, consumed)
	if err != nil || len(plan.OwnedItems) != 0 || !slices.Equal(plan.ItemRoutes, []content.ObjectID{definition.Route}) || plan.Anchor != definition.ConsumeAnchor {
		t.Fatalf("consumed item restore: %+v %v", plan, err)
	}
	data := consumed.Data(ctx)
	data.CriticalItems[0].Item = "ITEM-UNKNOWN"
	if _, err = campaign.Restore(ctx, data); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown item accepted: %v", err)
	}
	data = consumed.Data(ctx)
	data.CriticalItems[0].Consumed = data.CriticalItems[0].Acquired
	if _, err = campaign.Restore(ctx, data); !errors.Is(err, ErrInvalid) {
		t.Fatalf("impossible item history accepted: %v", err)
	}
}

func TestSnapshotValidationDebtAttemptsAndAnchors(t *testing.T) {
	ctx := context.Background()
	campaign, manifest, snapshot := startedAttempt(t)
	event := next(snapshot)
	event.Contacts = []Contact{{Kind: IncidentWaste, Detail: "station"}, {Kind: IncidentAssault, Victim: "nina", Detail: "hit"}}
	snapshot = prepare(t, campaign, snapshot, event)
	snapshot, _ = certify(t, campaign, manifest, snapshot, "D-HOS-SUP-01")
	for name, mutate := range map[string]func(*State){
		"debt mismatch": func(data *State) { data.DebtCents++ },

		"negative debt": func(data *State) { data.DebtCents = -1 },

		"unknown incident kind": func(data *State) { data.Incidents[0].Kind = "unknown" },

		"duplicate incident": func(data *State) { data.Incidents = append(data.Incidents, data.Incidents[0]) },

		"attempt marker": func(data *State) { data.Incidents[0].Attempt++ },

		"duplicate charge marker": func(data *State) {
			incident := data.Incidents[0]
			incident.ID.Ordinal = 100
			data.Incidents = append(data.Incidents, incident)
			data.DebtCents += incident.Cents
		},

		"missing anchor": func(data *State) { data.Anchor = "" },

		"unsafe anchor": func(data *State) { data.Anchor = "ANC-INS-05" },

		"bad ordinal": func(data *State) { data.Receipts[0].Ordinal = 2 },

		"unknown receipt": func(data *State) { data.Receipts[0].Certification.Deed = "D-UNKNOWN" },
	} {
		t.Run(name, func(t *testing.T) {
			data := snapshot.Data(ctx)
			mutate(&data)
			if _, err := campaign.Restore(ctx, data); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted invalid state: %v", err)
			}
		})
	}
}

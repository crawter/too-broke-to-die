// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"testing"

	"too-broke-to-die/internal/tbtd/content"
)

func fixture(t *testing.T) (*Campaign, *content.Campaign) {
	t.Helper()
	ctx := context.Background()
	manifest, err := content.LoadCampaign(ctx, os.DirFS("../../.."), "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	campaign, err := New(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	return campaign, manifest
}

func candidate(t *testing.T, manifest *content.Campaign, id content.DeedID, attempt uint64) Certification {
	t.Helper()
	ctx := context.Background()
	objects := map[content.ObjectID]content.Object{}
	for _, object := range manifest.Objects(ctx) {
		objects[object.ID] = object
	}
	for _, deed := range manifest.Deeds(ctx) {
		if deed.Deed.ID != id {
			continue
		}
		event := Certification{Deed: id, Attempt: attempt, Evidence: deed.Evidence, Trigger: deed.Trigger, HoldMilliseconds: deed.Deed.HoldMilliseconds}
		for _, objectID := range deed.Deed.Objects {
			object := objects[objectID]
			if object.Deed == id {
				event.EvidenceObjects = append(event.EvidenceObjects, objectID)
			}
			if object.Role == content.RoleTerminal {
				event.Terminal = objectID
			}
		}
		return event
	}
	t.Fatalf("unknown deed %s", id)
	return Certification{}
}

func certify(t *testing.T, campaign *Campaign, manifest *content.Campaign, snapshot Snapshot, id content.DeedID) (Snapshot, Certification) {
	t.Helper()
	ctx := context.Background()
	active, err := campaign.Accept(ctx, snapshot, id)
	if err != nil {
		t.Fatalf("accept %s: %v", id, err)
	}
	event := candidate(t, manifest, id, attemptNumber(active.Data(ctx).Attempts, id))
	result, receipt, err := campaign.Apply(ctx, active, event)
	if err != nil {
		t.Fatalf("certify %s: %v", id, err)
	}
	if snapshot.Credits()+1 != result.Credits() || receipt.Ordinal != result.Credits() {
		t.Fatalf("bad credit transition %s: %d -> %d ordinal %d", id, snapshot.Credits(), result.Credits(), receipt.Ordinal)
	}
	return result, event
}

func TestRouteAndReverseBatches(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordered", true: "reverse"}[reverse], func(t *testing.T) {
			ctx := context.Background()
			campaign, manifest := fixture(t)
			var snapshot Snapshot
			if missing, err := campaign.MissingGate(ctx, snapshot, "G-HOS-IN"); err != nil || !slices.Equal(missing, []string{"NAR-03"}) {
				t.Fatalf("hospital entrance: %v %v", missing, err)
			}
			snapshot, err := campaign.Start(ctx, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			batches := manifest.Batches(ctx)
			deeds := manifest.Deeds(ctx)
			var prior []Snapshot
			for institution := 0; institution < 4; institution++ {
				for family := 0; family < 4; family++ {
					batch := batches[institution*4+family]
					for n := 0; n < 6; n++ {
						index := n
						if reverse {
							index = 5 - n
						}
						id := batch.Deeds[index]
						before := snapshot
						prior = append(prior, snapshot)
						var event Certification
						snapshot, event = certify(t, campaign, manifest, snapshot, id)
						if before.Credits()+1 != snapshot.Credits() {
							t.Fatal("input snapshot changed")
						}
						data := snapshot.Data(ctx)
						if len(data.Emergencies) != institution*4+family+map[bool]int{false: 0, true: 1}[n == 5] {
							t.Fatalf("wrong emergency count for %s", id)
						}
						if n == 5 && data.Emergencies[len(data.Emergencies)-1] != (Emergency{ID: batch.Emergency.ID, Stage: EmergencyActive}) {
							t.Fatalf("missing active emergency for %s", id)
						}
						repeated, receipt, err := campaign.Apply(ctx, snapshot, event)
						if err != nil || repeated.Credits() != snapshot.Credits() || receipt.Ordinal != snapshot.Credits() {
							t.Fatalf("repeat %s: %+v %v", id, receipt, err)
						}
					}
				}
				closure := deeds[institution*25+24]
				missing, err := campaign.MissingDeed(ctx, snapshot, closure.Deed.ID)
				if err != nil || !slices.Contains(missing, string(closure.Deed.Encounter)) {
					t.Fatalf("closure missing boss: %v %v", missing, err)
				}
				gateIDs := []content.GateID{"G-CHU-IN", "G-MIN-IN", "G-INS-IN", "G-INS-FINAL"}
				missing, err = campaign.MissingGate(ctx, snapshot, gateIDs[institution])
				if err != nil || len(missing) == 0 {
					t.Fatalf("gate %s opened before closure or boss: %v %v", gateIDs[institution], missing, err)
				}
				old := snapshot
				snapshot, err = campaign.DefeatBoss(ctx, snapshot, closure.Deed.Encounter)
				if err != nil || snapshot.Credits() != old.Credits() {
					t.Fatalf("boss added credit: %v", err)
				}
				if institution == 3 {
					missing, err = campaign.MissingGate(ctx, snapshot, "G-INS-FINAL")
					if err != nil || len(missing) != 0 || snapshot.Credits() != 99 {
						t.Fatalf("final gate at 99: %v %v", missing, err)
					}
					missing, err = campaign.MissingGate(ctx, snapshot, "G-PERMIT")
					if err != nil || !slices.Contains(missing, "D-INS-END") || !slices.Contains(missing, "NAR-09") {
						t.Fatalf("permit at 99: %v %v", missing, err)
					}
					active, err := campaign.Accept(ctx, snapshot, "D-INS-END")
					if err != nil {
						t.Fatal(err)
					}
					short := candidate(t, manifest, "D-INS-END", 1)
					short.HoldMilliseconds = 1499
					if _, _, err = campaign.Apply(ctx, active, short); !errors.Is(err, ErrInvalid) || active.Credits() != 99 {
						t.Fatalf("short final panel: %v", err)
					}
				}
				prior = append(prior, snapshot)
				var closureEvent Certification
				snapshot, closureEvent = certify(t, campaign, manifest, snapshot, closure.Deed.ID)
				replayed, closureReceipt, err := campaign.Apply(ctx, snapshot, closureEvent)
				if err != nil || replayed.Credits() != snapshot.Credits() || closureReceipt.Ordinal != snapshot.Credits() {
					t.Fatalf("repeat closure %s: %v", closure.Deed.ID, err)
				}
				if snapshot.Credits() != (institution+1)*25 {
					t.Fatalf("institution %d: %d", institution, snapshot.Credits())
				}
				missing, err = campaign.MissingGate(ctx, snapshot, gateIDs[institution])
				if err != nil || len(missing) != 0 {
					t.Fatalf("gate %s remained blocked: %v %v", gateIDs[institution], missing, err)
				}
			}
			data := snapshot.Data(ctx)
			if data.Receipts[99].Certification.Deed != "D-INS-END" || data.Anchor != "ANC-INS-OUT" || !slices.Contains(data.Narratives, content.NarrativeID("NAR-09")) || data.Consequences[99] != "CON-D-INS-END" {
				t.Fatalf("final bindings: %+v", data.Receipts[99])
			}
			missing, err := campaign.MissingGate(ctx, snapshot, "G-PERMIT")
			if err != nil || len(missing) != 0 {
				t.Fatalf("permit: %v %v", missing, err)
			}
			// Isolate each missing identity in the requirement calculation. Such
			// holes are not necessarily valid restorable chronological snapshots.
			for _, gate := range manifest.Gates(ctx) {
				if gate.ID != "G-PERMIT" && gate.ID != "G-INS-FINAL" {
					continue
				}
				for i, receipt := range data.Receipts {
					if gate.ID == "G-INS-FINAL" && receipt.Certification.Deed == "D-INS-END" {
						continue
					}
					partial := data
					partial.Receipts = append(slices.Clone(data.Receipts[:i]), data.Receipts[i+1:]...)
					missing := missingRequirements(gate.Deeds, gate.Narratives, gate.Defeated, partial)
					if !slices.Equal(missing, []string{string(receipt.Certification.Deed)}) {
						t.Fatalf("%s with one missing source: %v", gate.ID, missing)
					}
				}
			}
			// Every authored prerequisite is named before its own receipt, using
			// genuine reachable snapshots rather than fabricated partial progress.
			index := make(map[content.DeedID]int)
			for i, receipt := range data.Receipts {
				index[receipt.Certification.Deed] = i
			}
			for _, deed := range deeds {
				for _, prerequisite := range deed.Deed.Prerequisites {
					missing, err := campaign.MissingDeed(ctx, prior[index[prerequisite]], deed.Deed.ID)
					if err != nil || !slices.Contains(missing, string(prerequisite)) {
						t.Fatalf("%s missing %s: %v %v", deed.Deed.ID, prerequisite, missing, err)
					}
				}
			}
			for i, old := range prior {
				id := string(data.Receipts[i].Certification.Deed)
				missing, err := campaign.MissingGate(ctx, old, "G-PERMIT")
				if err != nil || !slices.Contains(missing, id) {
					t.Fatalf("permit missing source %d: %v %v", i, missing, err)
				}
				if i < 99 {
					missing, err = campaign.MissingGate(ctx, old, "G-INS-FINAL")
					if err != nil || !slices.Contains(missing, id) {
						t.Fatalf("final missing source %d: %v %v", i, missing, err)
					}
				}
			}
			// These excursion and penalty values are deliberately outside the
			// campaign gate contract and cannot affect the same valid snapshot.
			player := struct {
				Snapshot Snapshot

				DebtCents int64

				Health int

				Ammo int

				Limbs int
			}{Snapshot: snapshot, DebtCents: 1 << 62, Health: 1}
			missing, err = campaign.MissingGate(ctx, player.Snapshot, "G-PERMIT")
			if err != nil || len(missing) != 0 {
				t.Fatalf("unrelated player conditions blocked permit: %v %v", missing, err)
			}
		})
	}
}

func TestCertificationValidationAndIsolation(t *testing.T) {
	ctx := context.Background()
	campaign, manifest := fixture(t)
	var empty Snapshot
	if _, err := New(ctx, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil manifest: %v", err)
	}
	if _, err := campaign.Accept(ctx, empty, "D-HOS-SUP-01"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("before NAR-03: %v", err)
	}
	started, err := campaign.Start(ctx, empty)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = campaign.Accept(ctx, started, "D-UNKNOWN"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown deed: %v", err)
	}
	if _, _, err = campaign.Apply(ctx, started, Certification{Deed: "D-UNKNOWN"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown certification: %v", err)
	}
	if _, err = campaign.MissingGate(ctx, started, "G-UNKNOWN"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown gate: %v", err)
	}
	if _, err = campaign.DefeatBoss(ctx, started, "ENC-UNKNOWN"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown boss: %v", err)
	}
	if _, err = campaign.DefeatBoss(ctx, started, "ENC-HOS-05"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("early boss: %v", err)
	}
	active, err := campaign.Accept(ctx, started, "D-HOS-SUP-01")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = campaign.Accept(ctx, active, "D-HOS-SUP-02"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("second active: %v", err)
	}
	event := candidate(t, manifest, "D-HOS-SUP-01", 1)
	for name, mutate := range map[string]func(*Certification){
		"evidence": func(event *Certification) { event.Evidence = "wrong" },

		"object": func(event *Certification) {
			event.EvidenceObjects = []content.ObjectID{"OBJ-D-HOS-SUP-02-TROLLEY"}
		},

		"trigger": func(event *Certification) { event.Trigger = "wrong" },

		"terminal": func(event *Certification) { event.Terminal = "OBJ-HOS-RES-TERMINAL" },

		"hold": func(event *Certification) { event.HoldMilliseconds-- },
	} {
		t.Run(name, func(t *testing.T) {
			bad := cloneCertification(event)
			mutate(&bad)
			if _, _, err := campaign.Apply(ctx, active, bad); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted bad candidate: %v", err)
			}
		})
	}
	badAttempt := event
	badAttempt.Attempt++
	if _, _, err = campaign.Apply(ctx, active, badAttempt); !errors.Is(err, ErrBlocked) {
		t.Fatalf("wrong attempt: %v", err)
	}
	result, receipt, err := campaign.Apply(ctx, active, event)
	if err != nil {
		t.Fatal(err)
	}
	if active.Credits() != 0 || receipt.Title == "" || receipt.Institution != "LOC-HOS" {
		t.Fatal("bad input isolation or receipt")
	}
	event.EvidenceObjects[0] = "BAD"
	receipt.Certification.EvidenceObjects[0] = "BAD"
	copy := result.Data(ctx)
	copy.Receipts[0].Certification.EvidenceObjects[0] = "BAD"
	copy.Receipts[0].Title = "BAD"
	copy.Consequences[0] = "BAD"
	copy.Attempts[0].Number = 50
	copy.Narratives[0] = "NAR-09"
	if err = campaign.Validate(ctx, result); err != nil {
		t.Fatalf("returned copies mutated snapshot: %v", err)
	}
	original := result.Data(ctx).Receipts[0].Certification
	conflict := cloneCertification(original)
	conflict.Trigger = "different"
	if _, _, err = campaign.Apply(ctx, result, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("identity conflict: %v", err)
	}
	reset, err := campaign.Reset(ctx, active)
	if err != nil {
		t.Fatal(err)
	}
	again, err := campaign.Accept(ctx, reset, "D-HOS-SUP-01")
	if err != nil || attemptNumber(again.Data(ctx).Attempts, "D-HOS-SUP-01") != 2 {
		t.Fatalf("reset attempt: %v", err)
	}
	abandoned, err := campaign.Abandon(ctx, again)
	if err != nil {
		t.Fatal(err)
	}
	other, err := campaign.Accept(ctx, abandoned, "D-HOS-SUP-02")
	if err != nil || other.Credits() != 0 {
		t.Fatalf("switch ticket: %v", err)
	}
}

func TestRestoreRejectsInvalidReceipts(t *testing.T) {
	ctx := context.Background()
	campaign, manifest := fixture(t)
	snapshot, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range manifest.Batches(ctx)[0].Deeds {
		snapshot, _ = certify(t, campaign, manifest, snapshot, id)
	}
	snapshot, _ = certify(t, campaign, manifest, snapshot, "D-HOS-RES-01")
	state := snapshot.Data(ctx)
	restored, err := campaign.Restore(ctx, state)
	if err != nil || !reflect.DeepEqual(restored.Data(ctx), state) {
		t.Fatalf("restore: %v", err)
	}
	state.Receipts[0].Title = "mutated after restore"
	if err = campaign.Validate(ctx, restored); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*State){
		"duplicate": func(data *State) {
			data.Receipts[1] = data.Receipts[0]
			data.Receipts[1].Ordinal = 2
		},

		"unknown": func(data *State) { data.Receipts[0].Certification.Deed = "D-NOPE" },

		"ordinal": func(data *State) { data.Receipts[0].Ordinal = 2 },

		"out of order": func(data *State) {
			data.Receipts[0], data.Receipts[6] = data.Receipts[6], data.Receipts[0]
			data.Receipts[0].Ordinal = 1
			data.Receipts[6].Ordinal = 7
		},

		"title": func(data *State) { data.Receipts[0].Title = "bad" },

		"institution": func(data *State) { data.Receipts[0].Institution = "LOC-INS" },

		"consequence": func(data *State) { data.Consequences = nil },

		"emergency": func(data *State) { data.Emergencies = nil },

		"anchor": func(data *State) { data.Anchor = "ANC-NOPE" },

		"boss": func(data *State) { data.Defeated = append(data.Defeated, "ENC-HOS-05") },

		"ending": func(data *State) { data.Permit = true },
	} {
		t.Run(name, func(t *testing.T) {
			data := snapshot.Data(ctx)
			mutate(&data)
			if _, err := campaign.Restore(ctx, data); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted invalid snapshot: %v", err)
			}
		})
	}
}

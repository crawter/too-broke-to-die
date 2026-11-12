// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
)

const testCampaignID = "01234567-89ab-4cde-8fab-0123456789ab"

func fixture(t *testing.T) (*Codec, *campaign.Campaign, *content.Campaign) {
	t.Helper()
	ctx := context.Background()
	manifest, err := content.LoadCampaign(ctx, os.DirFS("../../.."), "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	service, err := campaign.New(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	compatibility := Compatibility{ProductVersion: "test", ContentVersion: "campaign-1", ManifestDigest: strings.Repeat("a", 64)}
	codec, err := NewCodec(ctx, service, manifest, compatibility, nil)
	if err != nil {
		t.Fatal(err)
	}
	return codec, service, manifest
}

func revision(codec *Codec, snapshot campaign.Snapshot) Revision {
	var operation session.OperationID
	var history []session.OperationID
	for sequence := uint64(1); sequence <= snapshot.Data(context.Background()).OperationSequence; sequence++ {
		operation = session.OperationID{Event: fmt.Sprintf("test-operation-%d", sequence), Sequence: sequence}
		history = append(history, operation)
	}
	return Revision{
		CampaignID:    testCampaignID,
		Number:        7,
		Compatibility: codec.current,
		Snapshot:      snapshot,
		Operation:     operation,
		Operations:    history,
		Guidance:      Guidance{Tracked: "D-HOS-SUP-01", Tutorials: []content.PresentationID{"TUT-REC", "TUT-MOV"}, FirstLaunchNotice: true},
		Metadata:      Metadata{LastWriteUTC: time.Date(2026, 11, 12, 10, 20, 30, 0, time.UTC), BuildDescription: "test build"},
	}
}

func encode(t *testing.T, codec *Codec, input Revision) ([]byte, Revision) {
	t.Helper()
	data, result, err := codec.Encode(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	return data, result
}

func candidate(t *testing.T, manifest *content.Campaign, id content.DeedID, attempt uint64) campaign.Certification {
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
		event := campaign.Certification{Deed: id, Attempt: attempt, Evidence: deed.Evidence, Trigger: deed.Trigger, HoldMilliseconds: deed.Deed.HoldMilliseconds}
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
	t.Fatalf("missing deed %s", id)
	return campaign.Certification{}
}

func prepare(t *testing.T, service *campaign.Campaign, snapshot campaign.Snapshot, transition campaign.Transition) campaign.Snapshot {
	t.Helper()
	transition.CampaignID = testCampaignID
	transition.Sequence = snapshot.Data(context.Background()).OperationSequence + 1
	result, err := service.Prepare(context.Background(), snapshot, transition)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func richSnapshot(t *testing.T, service *campaign.Campaign, manifest *content.Campaign) (campaign.Snapshot, campaign.Transition) {
	t.Helper()
	ctx := context.Background()
	snapshot, err := service.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot = prepare(t, service, snapshot, campaign.Transition{Attempt: &campaign.AttemptChange{Action: campaign.AttemptAccept, Deed: "D-HOS-SUP-02"}})
	snapshot = prepare(t, service, snapshot, campaign.Transition{Contacts: []campaign.Contact{{Kind: campaign.IncidentWaste, Detail: "station"}, {Kind: campaign.IncidentAssault, Victim: "victim", Detail: "contact"}}})
	snapshot = prepare(t, service, snapshot, campaign.Transition{Failure: true})
	snapshot = prepare(t, service, snapshot, campaign.Transition{Attempt: &campaign.AttemptChange{Action: campaign.AttemptReset, Deed: "D-HOS-SUP-02"}})
	event := candidate(t, manifest, "D-HOS-SUP-02", 2)
	snapshot = prepare(t, service, snapshot, campaign.Transition{Certification: &campaign.CertificationCandidate{Completed: true, Event: event}})
	snapshot = prepare(t, service, snapshot, campaign.Transition{Attempt: &campaign.AttemptChange{Action: campaign.AttemptAccept, Deed: "D-HOS-SUP-01"}})
	last := campaign.Transition{CampaignID: testCampaignID, Sequence: snapshot.Data(ctx).OperationSequence + 1, Death: campaign.DeathGameplay, DeathDetail: "hazard"}
	snapshot, err = service.Prepare(ctx, snapshot, last)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, last
}

func TestRoundTripChecksumAndOperationRetry(t *testing.T) {
	ctx := context.Background()
	codec, service, manifest := fixture(t)
	snapshot, last := richSnapshot(t, service, manifest)
	before := snapshot.Data(ctx)
	input := revision(codec, snapshot)
	encoded, written := encode(t, codec, input)
	var env envelope
	if err := json.Unmarshal(encoded, &env); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(env.Payload)
	if env.PayloadLength != uint64(len(env.Payload)) || env.PayloadDigest != hex.EncodeToString(sum[:]) || written.PayloadDigest != env.PayloadDigest || written.PayloadLength != env.PayloadLength {
		t.Fatal("digest or length does not cover exact payload bytes")
	}
	decoded, err := codec.Decode(ctx, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, decoded.Snapshot.Data(ctx)) || !reflect.DeepEqual(before, snapshot.Data(ctx)) {
		t.Fatal("round trip lost state or mutated source")
	}
	if decoded.Snapshot.Credits() != 1 || decoded.Snapshot.Data(ctx).DebtCents != 999+1999+4999 || decoded.Operation != input.Operation || !slices.Equal(decoded.Operations, input.Operations) || !decoded.Guidance.FirstLaunchNotice || !decoded.Metadata.LastWriteUTC.Equal(input.Metadata.LastWriteUTC) {
		t.Fatal("durable progress or metadata lost")
	}
	replayed, err := service.Prepare(ctx, decoded.Snapshot, last)
	if err != nil || !reflect.DeepEqual(replayed.Data(ctx), before) {
		t.Fatalf("retry after restoration failed: %v", err)
	}
	if strings.Contains(string(env.Payload), `"health"`) || strings.Contains(string(env.Payload), `"credit`) || strings.Contains(string(env.Payload), `"projectile`) {
		t.Fatal("transient or mutable credit state serialized")
	}
}

func TestDeterministicUnorderedCollections(t *testing.T) {
	ctx := context.Background()
	codec, service, _ := fixture(t)
	snapshot, err := service.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []content.DeedID{"D-HOS-SUP-02", "D-HOS-SUP-01"} {
		snapshot, err = service.Accept(ctx, snapshot, id)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err = service.Abandon(ctx, snapshot)
		if err != nil {
			t.Fatal(err)
		}
	}
	original := snapshot.Data(ctx)
	reversed := snapshot.Data(ctx)
	slices.Reverse(reversed.Attempts)
	other, err := service.Restore(ctx, reversed)
	if err != nil {
		t.Fatal(err)
	}
	first := revision(codec, snapshot)
	second := revision(codec, other)
	slices.Reverse(second.Guidance.Tutorials)
	a, _ := encode(t, codec, first)
	b, _ := encode(t, codec, second)
	if !bytes.Equal(a, b) {
		t.Fatal("insertion order changed canonical encoding")
	}
	if !reflect.DeepEqual(snapshot.Data(ctx), original) || first.Guidance.Tutorials[0] != "TUT-REC" {
		t.Fatal("encoding mutated input")
	}
}

func TestSortedReceiptWirePreservesChronology(t *testing.T) {
	ctx := context.Background()
	codec, service, manifest := fixture(t)
	snapshot, err := service.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []content.DeedID{"D-HOS-SUP-02", "D-HOS-SUP-01"} {
		snapshot, err = service.Accept(ctx, snapshot, id)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, _, err = service.Apply(ctx, snapshot, candidate(t, manifest, id, 1))
		if err != nil {
			t.Fatal(err)
		}
	}
	original := snapshot.Data(ctx)
	encoded, restored := encode(t, codec, revision(codec, snapshot))
	var env envelope
	if err = json.Unmarshal(encoded, &env); err != nil {
		t.Fatal(err)
	}
	var value payload
	if err = json.Unmarshal(env.Payload, &value); err != nil {
		t.Fatal(err)
	}
	if value.State.Receipts[0].Certification.Deed != "D-HOS-SUP-01" || value.State.Consequences[0] > value.State.Consequences[1] {
		t.Fatal("wire identifier collections are not sorted")
	}
	got := restored.Snapshot.Data(ctx)
	if !reflect.DeepEqual(got.Receipts, original.Receipts) || !reflect.DeepEqual(got.Consequences, original.Consequences) {
		t.Fatal("sorting changed certification chronology")
	}
}

// rewrap recomputes the checksum so tests reach structural and semantic checks.
func rewrap(t *testing.T, encoded []byte, change func(*payload)) []byte {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(encoded, &env); err != nil {
		t.Fatal(err)
	}
	var value payload
	if err := json.Unmarshal(env.Payload, &value); err != nil {
		t.Fatal(err)
	}
	change(&value)
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	env.Payload, env.PayloadLength, env.PayloadDigest = body, uint64(len(body)), hex.EncodeToString(sum[:])
	result, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRejectSemanticDamage(t *testing.T) {
	ctx := context.Background()
	codec, service, manifest := fixture(t)
	snapshot, _ := richSnapshot(t, service, manifest)
	encoded, _ := encode(t, codec, revision(codec, snapshot))
	before := snapshot.Data(ctx)
	cases := map[string]func(*payload){
		"duplicate receipt": func(p *payload) { p.State.Receipts = append(p.State.Receipts, p.State.Receipts[0]) },

		"unknown receipt": func(p *payload) { p.State.Receipts[0].Certification.Deed = "D-HOS-SUP-99" },

		"duplicate incident": func(p *payload) {
			p.State.Incidents = append(p.State.Incidents, p.State.Incidents[0])
			p.Order.Incidents = append(p.Order.Incidents, len(p.Order.Incidents))
		},

		"invalid anchor": func(p *payload) { p.State.Anchor = "ANC-BOGUS" },

		"impossible ending": func(p *payload) { p.State.Completed = true },

		"debt mismatch": func(p *payload) { p.State.DebtCents++ },

		"debt overflow": func(p *payload) { p.State.Incidents[0].Cents = math.MaxInt64 },

		"duplicate marker": func(p *payload) {
			p.State.Incidents[1] = p.State.Incidents[0]
			p.State.Incidents[1].ID.Ordinal++
		},

		"different campaign": func(p *payload) { p.State.CampaignID = "11111111-1111-4111-8111-111111111111" },

		"false recovery": func(p *payload) { p.Recovery.Anchor = "ANC-INS-OUT" },

		"duplicate tutorial": func(p *payload) { p.Guidance.Tutorials = append(p.Guidance.Tutorials, p.Guidance.Tutorials[0]) },

		"unknown tracked": func(p *payload) { p.Guidance.Tracked = "D-HOS-SUP-99" },

		"bad permutation": func(p *payload) { p.Order.Attempts[0] = p.Order.Attempts[1] },

		"bad result digest": func(p *payload) { p.State.LatestOperation.ResultDigest = strings.Repeat("0", 64) },

		"bad operation identity": func(p *payload) { p.Operation.Sequence++ },

		"missing operation": func(p *payload) { p.Operations = p.Operations[1:] },

		"duplicate operation": func(p *payload) { p.Operations[1] = p.Operations[0] },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			damaged := rewrap(t, encoded, mutate)
			if _, err := codec.Decode(ctx, damaged); !errors.Is(err, session.SaveCorrupt) {
				t.Fatalf("accepted semantic damage: %v", err)
			}
		})
	}
	if !reflect.DeepEqual(snapshot.Data(ctx), before) {
		t.Fatal("validation mutated original")
	}
	if _, err := codec.Decode(ctx, encoded); err != nil {
		t.Fatalf("damaged revisions affected intact revision: %v", err)
	}
}

// rawEnvelope retains the exact body, including whitespace and duplicate keys.
func rawEnvelope(t *testing.T, encoded, body []byte) []byte {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(encoded, &env); err != nil {
		t.Fatal(err)
	}
	marker := []byte(`"payload":`)
	index := bytes.Index(encoded, marker)
	if index < 0 {
		t.Fatal("missing payload field")
	}
	prefix := string(encoded[:index+len(marker)])
	sum := sha256.Sum256(body)
	prefix = strings.Replace(prefix, fmt.Sprintf(`"payload_length":%d`, env.PayloadLength), fmt.Sprintf(`"payload_length":%d`, len(body)), 1)
	prefix = strings.Replace(prefix, env.PayloadDigest, hex.EncodeToString(sum[:]), 1)
	return append(append([]byte(prefix), body...), '}')
}

func TestExactPayloadAndStrictJSON(t *testing.T) {
	ctx := context.Background()
	codec, _, _ := fixture(t)
	encoded, _ := encode(t, codec, revision(codec, campaign.Snapshot{}))
	var env envelope
	if err := json.Unmarshal(encoded, &env); err != nil {
		t.Fatal(err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, env.Payload, "", "  "); err != nil {
		t.Fatal(err)
	}
	changed := rawEnvelope(t, encoded, pretty.Bytes())
	decoded, err := codec.Decode(ctx, changed)
	if err != nil {
		t.Fatalf("exact noncanonical payload rejected: %v", err)
	}
	sum := sha256.Sum256(pretty.Bytes())
	if decoded.PayloadLength != uint64(pretty.Len()) || decoded.PayloadDigest != hex.EncodeToString(sum[:]) {
		t.Fatal("digest did not use original whitespace bytes")
	}
	cases := map[string]string{
		"duplicate field": strings.Replace(string(env.Payload), `"anchor":`, `"anchor":"","anchor":`, 1),
		"wrong case":      strings.Replace(string(env.Payload), `"receipts":`, `"RECEIPTS":`, 1),
		"unknown field":   strings.Replace(string(env.Payload), `"receipts":`, `"health":100,"receipts":`, 1),
		"missing field":   strings.Replace(string(env.Payload), `"debt_cents":0,`, "", 1),
		"null number":     strings.Replace(string(env.Payload), `"debt_cents":0`, `"debt_cents":null`, 1),
		"invalid number":  strings.Replace(string(env.Payload), `"debt_cents":0`, `"debt_cents":9223372036854775808`, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := codec.Decode(ctx, rawEnvelope(t, encoded, []byte(body))); !errors.Is(err, session.SaveCorrupt) {
				t.Fatalf("accepted invalid JSON: %v", err)
			}
		})
	}
}

func TestMalformedAndCompatibility(t *testing.T) {
	ctx := context.Background()
	codec, service, _ := fixture(t)
	encoded, _ := encode(t, codec, revision(codec, campaign.Snapshot{}))
	cases := map[string][]byte{
		"empty":             nil,
		"truncated":         encoded[:len(encoded)-1],
		"invalid UTF8":      append(slices.Clone(encoded), 0xff),
		"trailing":          append(slices.Clone(encoded), []byte(` {}`)...),
		"duplicate":         []byte(strings.Replace(string(encoded), `"magic":`, `"magic":"TBTD-SAVE","magic":`, 1)),
		"wrong case":        []byte(strings.Replace(string(encoded), `"magic":`, `"MAGIC":`, 1)),
		"missing revision":  []byte(strings.Replace(string(encoded), `"revision":7,`, "", 1)),
		"unknown field":     []byte(strings.Replace(string(encoded), `"magic":`, `"extra":0,"magic":`, 1)),
		"length mismatch":   []byte(strings.Replace(string(encoded), `"payload_length":`, `"payload_length":1`, 1)),
		"checksum mismatch": []byte(strings.Replace(string(encoded), `test build`, `test buile`, 1)),
		"unicode":           []byte(strings.Replace(string(encoded), `test build`, `\ud800`, 1)),
		"oversize":          make([]byte, MaxRevisionBytes+1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := codec.Decode(ctx, data); !errors.Is(err, session.SaveCorrupt) {
				t.Fatalf("expected corruption, got %v", err)
			}
		})
	}
	future := []byte(fmt.Sprintf(`{"magic":"TBTD-SAVE","schema_version":2,"campaign_id":%q,"future":{"field":true}}`, testCampaignID))
	_, err := codec.Decode(ctx, future)
	var detail *RevisionError
	if !errors.Is(err, session.SaveTooNew) || !errors.As(err, &detail) || detail.Schema != 2 || detail.CampaignID != testCampaignID {
		t.Fatalf("unknown schema not preserved: %v", err)
	}
	changed := bytes.Replace(encoded, []byte(strings.Repeat("a", 64)), []byte(strings.Repeat("b", 64)), 1)
	_, err = codec.Decode(ctx, changed)
	if !errors.Is(err, session.ContentIncompatible) || !errors.As(err, &detail) || detail.Compatibility.ManifestDigest != strings.Repeat("b", 64) {
		t.Fatalf("content mismatch not preserved: %v", err)
	}
	_, _, manifest := fixture(t)
	mapped, err := NewCodec(ctx, service, manifest, codec.current, []ContentIdentity{{ContentVersion: "campaign-1", ManifestDigest: strings.Repeat("b", 64)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mapped.Decode(ctx, changed); err != nil {
		t.Fatalf("declared compatible content rejected: %v", err)
	}
	newMigration := bytes.Replace(encoded, []byte(`"required_migration":0`), []byte(`"required_migration":1`), 1)
	if _, err = codec.Decode(ctx, newMigration); !errors.Is(err, session.ContentIncompatible) {
		t.Fatalf("undeclared migration accepted: %v", err)
	}
}

func TestEncodeRejectsInvalidInputs(t *testing.T) {
	ctx := context.Background()
	codec, service, manifest := fixture(t)
	base := revision(codec, campaign.Snapshot{})
	badID := base
	badID.CampaignID = "00000000-0000-0000-0000-000000000000"
	badVersion := base
	badVersion.Compatibility.ManifestDigest = strings.Repeat("b", 64)
	badMetadata := base
	badMetadata.Metadata.LastWriteUTC = time.Time{}
	badUTF8 := base
	badUTF8.Metadata.BuildDescription = string([]byte{0xff})
	badGuidance := base
	badGuidance.Guidance.Tutorials = []content.PresentationID{"TUT-REC", "TUT-REC"}
	badOperation := base
	badOperation.Operation = session.OperationID{Event: "unexpected", Sequence: 1}
	for _, input := range []Revision{badID, badVersion, badMetadata, badUTF8, badGuidance, badOperation} {
		if _, _, err := codec.Encode(ctx, input); err == nil {
			t.Fatal("accepted invalid revision input")
		}
	}
	if _, err := NewCodec(ctx, nil, manifest, codec.current, nil); err == nil {
		t.Fatal("accepted nil campaign")
	}
	if _, err := NewCodec(ctx, service, nil, codec.current, nil); err == nil {
		t.Fatal("accepted nil manifest")
	}
	if _, err := NewCodec(ctx, service, manifest, codec.current, []ContentIdentity{{ContentVersion: "old", ManifestDigest: "invalid"}}); err == nil {
		t.Fatal("accepted invalid compatibility mapping")
	}
	encoded, _ := encode(t, codec, base)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := codec.Encode(cancelled, base); !errors.Is(err, context.Canceled) {
		t.Fatalf("encode cancellation lost: %v", err)
	}
	if _, err := codec.Decode(cancelled, encoded); !errors.Is(err, context.Canceled) || errors.Is(err, session.SaveCorrupt) {
		t.Fatalf("decode cancellation mislabeled as corruption: %v", err)
	}
}

func TestFailedAttemptIsTransient(t *testing.T) {
	ctx := context.Background()
	codec, service, _ := fixture(t)
	snapshot, err := service.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot = prepare(t, service, snapshot, campaign.Transition{Attempt: &campaign.AttemptChange{Action: campaign.AttemptAccept, Deed: "D-HOS-SUP-01"}})
	snapshot = prepare(t, service, snapshot, campaign.Transition{Failure: true})
	before := snapshot.Data(ctx)
	if before.Attempts[0].Failed == "" {
		t.Fatal("fixture lacks failed work")
	}
	encoded, restored := encode(t, codec, revision(codec, snapshot))
	if bytes.Contains(encoded, []byte(`"failed"`)) || restored.Snapshot.Data(ctx).Attempts[0].Failed != "" || len(restored.Snapshot.Data(ctx).Incidents) != 1 || !reflect.DeepEqual(before, snapshot.Data(ctx)) {
		t.Fatal("transient work or durable failure history handled incorrectly")
	}
}

func TestCriticalItemRestoration(t *testing.T) {
	ctx := context.Background()
	base, _, manifest := fixture(t)
	definition := campaign.CriticalItemDefinition{ID: "ITEM-TEST-KEY", AcquireAnchor: "ANC-HOS-01", ConsumeAnchor: "ANC-HOS-02", Route: "OBJ-HOS-RES-GATE"}
	service, err := campaign.NewWithInventory(ctx, manifest, []campaign.CriticalItemDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := NewCodec(ctx, service, manifest, base.current, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range manifest.Batches(ctx)[0].Deeds {
		snapshot, err = service.Accept(ctx, snapshot, id)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, _, err = service.Apply(ctx, snapshot, candidate(t, manifest, id, 1))
		if err != nil {
			t.Fatal(err)
		}
	}
	acquire := campaign.CriticalItemCandidate{Item: definition.ID, Action: campaign.CriticalAcquire, Completed: true}
	snapshot = prepare(t, service, snapshot, campaign.Transition{CriticalItems: []campaign.CriticalItemCandidate{acquire}})
	_, owned := encode(t, codec, revision(codec, snapshot))
	plan, err := service.RestorePlan(ctx, owned.Snapshot)
	if err != nil || !slices.Contains(plan.OwnedItems, definition.ID) || len(plan.ItemRoutes) != 0 {
		t.Fatalf("owned item lost: %+v %v", plan, err)
	}
	consume := campaign.CriticalItemCandidate{Item: definition.ID, Action: campaign.CriticalConsume, Completed: true}
	snapshot = prepare(t, service, snapshot, campaign.Transition{CriticalItems: []campaign.CriticalItemCandidate{consume}})
	_, restored := encode(t, codec, revision(codec, snapshot))
	plan, err = service.RestorePlan(ctx, restored.Snapshot)
	if err != nil || len(plan.OwnedItems) != 0 || !slices.Contains(plan.ItemRoutes, definition.Route) || restored.Snapshot.Credits() != 6 {
		t.Fatalf("consumed item or route lost: %+v %v", plan, err)
	}
}

func TestEndingRestorationAndIndependentRevisions(t *testing.T) {
	ctx := context.Background()
	codec, service, manifest := fixture(t)
	snapshot, err := service.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for institution := 0; institution < 4; institution++ {
		for family := 0; family < 4; family++ {
			for _, id := range manifest.Batches(ctx)[institution*4+family].Deeds {
				snapshot, err = service.Accept(ctx, snapshot, id)
				if err != nil {
					t.Fatal(err)
				}
				snapshot, _, err = service.Apply(ctx, snapshot, candidate(t, manifest, id, 1))
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		closure := manifest.Deeds(ctx)[institution*25+24]
		snapshot, err = service.DefeatBoss(ctx, snapshot, closure.Deed.Encounter)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err = service.Accept(ctx, snapshot, closure.Deed.ID)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, _, err = service.Apply(ctx, snapshot, candidate(t, manifest, closure.Deed.ID, 1))
		if err != nil {
			t.Fatal(err)
		}
	}
	old, _ := encode(t, codec, revision(codec, snapshot))
	snapshot = prepare(t, service, snapshot, campaign.Transition{Story: &campaign.StoryChange{Action: campaign.StoryPermit}})
	snapshot = prepare(t, service, snapshot, campaign.Transition{Story: &campaign.StoryChange{Action: campaign.StoryConsent}})
	for _, stage := range []campaign.EndingStage{campaign.EndingDeath, campaign.EndingHell, campaign.EndingCeremony, campaign.EndingTableau} {
		encoded, decoded := encode(t, codec, revision(codec, snapshot))
		plan, planErr := service.RestorePlan(ctx, decoded.Snapshot)
		if planErr != nil || plan.Ending != stage || plan.State != "ST-SCN" || plan.SceneEntry != string(stage) {
			t.Fatalf("ending %s: %+v %v", stage, plan, planErr)
		}
		if _, err = codec.Decode(ctx, encoded[:len(encoded)-1]); !errors.Is(err, session.SaveCorrupt) {
			t.Fatal("damaged ending revision accepted")
		}
		older, oldErr := codec.Decode(ctx, old)
		if oldErr != nil || older.Snapshot.Data(ctx).Consent || older.Snapshot.Credits() != 100 {
			t.Fatalf("older revision borrowed ending state: %v", oldErr)
		}
		snapshot = prepare(t, service, snapshot, campaign.Transition{Story: &campaign.StoryChange{Action: campaign.StoryEnding, Stage: stage}})
	}
	_, decoded := encode(t, codec, revision(codec, snapshot))
	plan, err := service.RestorePlan(ctx, decoded.Snapshot)
	if err != nil || plan.State != "ST-END" || !decoded.Snapshot.Data(ctx).Completed {
		t.Fatalf("completed ending: %+v %v", plan, err)
	}
}

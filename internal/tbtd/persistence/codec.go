// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
)

// Encode produces a complete schema-one revision and its exact payload digest.
// It validates a private copy and never publishes or modifies the proposal.
func (codec *Codec) Encode(ctx context.Context, revision Revision) (_ []byte, _ Revision, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return nil, Revision{}, err
	}
	if codec == nil || !validCampaignID(revision.CampaignID) || revision.Compatibility != codec.current {
		return nil, Revision{}, fmt.Errorf("invalid revision identity or write compatibility")
	}
	if err = codec.campaign.Validate(ctx, revision.Snapshot); err != nil {
		return nil, Revision{}, fmt.Errorf("invalid revision: %w", err)
	}
	if err = codec.validateGuidance(ctx, revision.Guidance); err != nil {
		return nil, Revision{}, err
	}
	if err = validateMetadata(revision.Metadata); err != nil {
		return nil, Revision{}, err
	}
	state := revision.Snapshot.Data(ctx)
	if state.CampaignID != "" && state.CampaignID != revision.CampaignID {
		return nil, Revision{}, fmt.Errorf("campaign identity mismatch")
	}
	if err = validateOperations(revision.Operation, revision.Operations, state.OperationSequence); err != nil {
		return nil, Revision{}, err
	}
	for i := range state.Attempts {
		state.Attempts[i].Failed = "" // excursion work is not durable
	}
	plan, err := codec.campaign.RestorePlan(ctx, revision.Snapshot)
	if err != nil {
		return nil, Revision{}, err
	}
	guidance := revision.Guidance
	guidance.Tutorials = slices.Clone(guidance.Tutorials)
	slices.Sort(guidance.Tutorials)
	value := payload{State: state, Operation: operation{Event: revision.Operation.Event, Sequence: revision.Operation.Sequence}, Recovery: recovery{Location: plan.Location, Anchor: plan.Anchor, Mode: plan.State, SceneEntry: plan.SceneEntry}, Guidance: guidance, Metadata: revision.Metadata}
	for _, id := range revision.Operations {
		value.Operations = append(value.Operations, operation{Event: id.Event, Sequence: id.Sequence})
	}
	if !validUTF8Value(reflect.ValueOf(value)) || !validUTF8Value(reflect.ValueOf(revision.Compatibility)) {
		return nil, Revision{}, fmt.Errorf("invalid UTF-8 in revision")
	}
	value.Order = sortState(ctx, &value.State, state.LatestOperation != nil)
	payloadBytes, err := json.Marshal(value)
	if err != nil {
		return nil, Revision{}, fmt.Errorf("encode payload: %w", err)
	}
	sum := sha256.Sum256(payloadBytes)
	env := envelope{Magic: magic, Schema: SchemaVersion, CampaignID: revision.CampaignID, Revision: revision.Number, PayloadLength: uint64(len(payloadBytes)), PayloadDigest: hex.EncodeToString(sum[:]), Compatibility: revision.Compatibility, Payload: payloadBytes}
	encoded, err := json.Marshal(env)
	if err != nil {
		return nil, Revision{}, fmt.Errorf("encode envelope: %w", err)
	}
	if len(encoded) > MaxRevisionBytes {
		return nil, Revision{}, fmt.Errorf("revision exceeds size limit")
	}
	// Independently check the exact bytes written, including the operation
	// result digest, rather than assuming the source snapshot was consistent.
	checked, err := codec.Decode(ctx, encoded)
	if err != nil {
		return nil, Revision{}, err
	}
	return encoded, checked, nil
}

// Decode checks exactly one bounded revision. It never consults another slot,
// merges fields, guesses a migration, or turns an error into a new campaign.
func (codec *Codec) Decode(ctx context.Context, encoded []byte) (_ Revision, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return Revision{}, err
	}
	failure := &RevisionError{Code: session.SaveCorrupt}
	fail := func(cause error) (Revision, error) {
		failure.Cause = cause
		return Revision{}, failure
	}
	if codec == nil || len(encoded) == 0 || len(encoded) > MaxRevisionBytes || !utf8.Valid(encoded) {
		return fail(fmt.Errorf("invalid revision size or UTF-8"))
	}
	if err = checkUnicode(encoded); err != nil {
		return fail(err)
	}
	// First inspect generic JSON so an unknown future schema can be identified
	// without requiring it to have schema one's fields or payload layout.
	if err = checkJSON(encoded, nil); err != nil {
		return fail(err)
	}
	var header map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &header); err != nil || header == nil {
		return fail(fmt.Errorf("invalid revision header"))
	}
	var headerMagic, campaignID string
	var schema int
	if json.Unmarshal(header["magic"], &headerMagic) != nil || json.Unmarshal(header["schema_version"], &schema) != nil || json.Unmarshal(header["campaign_id"], &campaignID) != nil || headerMagic != magic || schema < 1 || !validCampaignID(campaignID) {
		return fail(fmt.Errorf("invalid revision header"))
	}
	failure.Schema, failure.CampaignID = schema, campaignID
	if schema > SchemaVersion {
		failure.Code = session.SaveTooNew
		return fail(fmt.Errorf("unsupported schema %d", schema))
	}
	if err = checkJSON(encoded, reflect.TypeOf(envelope{})); err != nil {
		return fail(err)
	}
	var env envelope
	if err = json.Unmarshal(encoded, &env); err != nil {
		return fail(fmt.Errorf("invalid envelope: %w", err))
	}
	failure.Compatibility = env.Compatibility
	if !validCompatibility(env.Compatibility) || !digestPattern.MatchString(env.PayloadDigest) || env.PayloadLength != uint64(len(env.Payload)) {
		return fail(fmt.Errorf("invalid envelope metadata or payload length"))
	}
	sum := sha256.Sum256(env.Payload)
	if hex.EncodeToString(sum[:]) != env.PayloadDigest {
		return fail(fmt.Errorf("payload checksum mismatch"))
	}
	if !codec.compatible[contentIdentity(env.Compatibility)] {
		failure.Code = session.ContentIncompatible
		return fail(fmt.Errorf("content requires an explicit mapping"))
	}
	if err = checkJSON(env.Payload, reflect.TypeOf(payload{})); err != nil {
		return fail(err)
	}
	var value payload
	if err = json.Unmarshal(env.Payload, &value); err != nil {
		return fail(fmt.Errorf("invalid payload: %w", err))
	}
	if err = ctx.Err(); err != nil {
		return Revision{}, err
	}
	if value.State.CampaignID != "" && value.State.CampaignID != env.CampaignID {
		return fail(fmt.Errorf("campaign identity mismatch"))
	}
	operationID := session.OperationID{Event: value.Operation.Event, Sequence: value.Operation.Sequence}
	var operations []session.OperationID
	for _, id := range value.Operations {
		operations = append(operations, session.OperationID{Event: id.Event, Sequence: id.Sequence})
	}
	if err = validateOperations(operationID, operations, value.State.OperationSequence); err != nil {
		return fail(err)
	}
	if err = restoreOrder(ctx, &value.State, value.Order); err != nil {
		return fail(err)
	}
	// Failed work is transient; a stored failure incident remains durable.
	for _, attempt := range value.State.Attempts {
		if attempt.Failed != "" {
			return fail(fmt.Errorf("transient attempt work in revision"))
		}
	}
	if err = codec.validateGuidance(ctx, value.Guidance); err != nil {
		return fail(err)
	}
	if err = validateMetadata(value.Metadata); err != nil {
		return fail(err)
	}
	snapshot, err := codec.campaign.Restore(ctx, value.State)
	if err != nil {
		return fail(fmt.Errorf("invalid campaign: %w", err))
	}
	if err = checkResultDigest(ctx, snapshot); err != nil {
		return fail(err)
	}
	plan, err := codec.campaign.RestorePlan(ctx, snapshot)
	if err != nil {
		return fail(err)
	}
	if value.Recovery != (recovery{Location: plan.Location, Anchor: plan.Anchor, Mode: plan.State, SceneEntry: plan.SceneEntry}) {
		return fail(fmt.Errorf("recovery does not match campaign"))
	}
	return Revision{CampaignID: env.CampaignID, Number: env.Revision, Compatibility: env.Compatibility, Snapshot: snapshot, Operation: operationID, Operations: operations, Guidance: value.Guidance, Metadata: value.Metadata, PayloadDigest: env.PayloadDigest, PayloadLength: env.PayloadLength}, nil
}

// validateOperations checks the complete committed history; it never invents
// missing events from a sequence number or from campaign effects.
func validateOperations(latest session.OperationID, history []session.OperationID, sequence uint64) error {
	if uint64(len(history)) != sequence {
		return fmt.Errorf("incomplete operation history")
	}
	for i, id := range history {
		if id.Sequence != uint64(i)+1 || !validText(id.Event) {
			return fmt.Errorf("invalid operation identity")
		}
	}
	if sequence == 0 {
		if latest != (session.OperationID{}) {
			return fmt.Errorf("unexpected operation identity")
		}
		return nil
	}
	if latest != history[len(history)-1] {
		return fmt.Errorf("latest operation identity mismatch")
	}
	return nil
}

// validUTF8Value checks source strings before json.Marshal can replace invalid
// UTF-8. It traverses only exported fields of the fixed persistence DTOs.
func validUTF8Value(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.String:
		return utf8.ValidString(value.String())
	case reflect.Pointer:
		return value.IsNil() || validUTF8Value(value.Elem())
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if value.Type().Field(i).IsExported() && !validUTF8Value(value.Field(i)) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if !validUTF8Value(value.Index(i)) {
				return false
			}
		}
	}
	return true
}

// checkResultDigest mirrors campaign's operation result contract. Its digest
// protects retries from replaying a changed proposal; it is not authentication.
func checkResultDigest(ctx context.Context, snapshot campaign.Snapshot) (err error) {
	defer mon.Task()(&ctx)(&err)
	state := snapshot.Data(ctx)
	if state.LatestOperation == nil {
		return nil
	}
	want := state.LatestOperation.ResultDigest
	state.LatestOperation = nil
	for i := range state.Attempts {
		state.Attempts[i].Failed = ""
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode operation result: %w", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("operation result digest mismatch")
	}
	return nil
}

// sorted retains nil versus empty slices, which is significant to the current
// campaign result digest. Original indexes restore order for identified events.
func sorted[T any](ctx context.Context, items []T, less func(T, T) bool, preserve bool) []int {
	defer mon.Task()(&ctx)(nil)
	indexes := make([]int, len(items))
	for i := range indexes {
		indexes[i] = i
	}
	sort.Slice(indexes, func(i, j int) bool { return less(items[indexes[i]], items[indexes[j]]) })
	copyItems := slices.Clone(items)
	for i, original := range indexes {
		items[i] = copyItems[original]
	}
	if !preserve || len(indexes) == 0 {
		return nil
	}
	return indexes
}

func occurrenceLess(left, right campaign.OccurrenceID) bool {
	if left.CampaignID != right.CampaignID {
		return left.CampaignID < right.CampaignID
	}
	if left.Sequence != right.Sequence {
		return left.Sequence < right.Sequence
	}
	return left.Ordinal < right.Ordinal
}

func sortState(ctx context.Context, state *campaign.State, preserve bool) ordering {
	defer mon.Task()(&ctx)(nil)
	// Receipt ordinals are the authoritative certification chronology. Other
	// chronology-sensitive collections carry explicit inverse permutations.
	sort.Slice(state.Receipts, func(i, j int) bool {
		return state.Receipts[i].Certification.Deed < state.Receipts[j].Certification.Deed
	})
	return ordering{
		Consequences:    sorted(ctx, state.Consequences, func(a, b content.ConsequenceID) bool { return a < b }, true),
		Emergencies:     sorted(ctx, state.Emergencies, func(a, b campaign.Emergency) bool { return a.ID < b.ID }, true),
		Attempts:        sorted(ctx, state.Attempts, func(a, b campaign.Attempt) bool { return a.Deed < b.Deed }, preserve),
		Defeated:        sorted(ctx, state.Defeated, func(a, b content.EncounterID) bool { return a < b }, preserve),
		EncounterEvents: sorted(ctx, state.EncounterEvents, func(a, b campaign.EncounterEvent) bool { return a.Encounter < b.Encounter }, preserve),
		Incidents:       sorted(ctx, state.Incidents, func(a, b campaign.Incident) bool { return occurrenceLess(a.ID, b.ID) }, preserve),
		CriticalItems:   sorted(ctx, state.CriticalItems, func(a, b campaign.CriticalItemRecord) bool { return a.Item < b.Item }, preserve),
	}
}

func restoreSlice[T any](ctx context.Context, items []T, indexes []int, required bool) (err error) {
	defer mon.Task()(&ctx)(&err)
	if !required {
		if len(indexes) != 0 {
			return fmt.Errorf("unexpected order metadata")
		}
		return nil
	}
	if len(indexes) != len(items) {
		return fmt.Errorf("missing order metadata")
	}
	seen := make([]bool, len(items))
	copyItems := slices.Clone(items)
	for i, original := range indexes {
		if original < 0 || original >= len(items) || seen[original] {
			return fmt.Errorf("invalid order permutation")
		}
		seen[original] = true
		items[original] = copyItems[i]
	}
	return nil
}

func restoreOrder(ctx context.Context, state *campaign.State, order ordering) (err error) {
	defer mon.Task()(&ctx)(&err)
	sort.Slice(state.Receipts, func(i, j int) bool { return state.Receipts[i].Ordinal < state.Receipts[j].Ordinal })
	if err = restoreSlice(ctx, state.Consequences, order.Consequences, true); err != nil {
		return err
	}
	if err = restoreSlice(ctx, state.Emergencies, order.Emergencies, true); err != nil {
		return err
	}
	required := state.LatestOperation != nil
	if err = restoreSlice(ctx, state.Attempts, order.Attempts, required); err != nil {
		return err
	}
	if err = restoreSlice(ctx, state.Defeated, order.Defeated, required); err != nil {
		return err
	}
	if err = restoreSlice(ctx, state.EncounterEvents, order.EncounterEvents, required); err != nil {
		return err
	}
	if err = restoreSlice(ctx, state.Incidents, order.Incidents, required); err != nil {
		return err
	}
	return restoreSlice(ctx, state.CriticalItems, order.CriticalItems, required)
}

var rawType = reflect.TypeOf(json.RawMessage{})
var timeType = reflect.TypeOf(time.Time{})

// checkJSON rejects duplicate and case-folded keys, missing required fields,
// excess nesting and trailing values before encoding/json can normalize them.
// A nil schema performs a generic scan for a future-schema header.
func checkJSON(data []byte, schema reflect.Type) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := checkValue(decoder, schema, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}

// checkValue is measured by Decode and runs only once per JSON token.
func checkValue(decoder *json.Decoder, schema reflect.Type, depth int) error {
	if depth > 64 {
		return fmt.Errorf("JSON nesting too deep")
	}
	if schema != nil && schema.Kind() == reflect.Pointer {
		schema = schema.Elem()
	}
	if schema == rawType {
		schema = nil
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		if schema == nil || schema.Kind() == reflect.Slice {
			return nil
		}
		return fmt.Errorf("unexpected null")
	}
	delim, compound := token.(json.Delim)
	if !compound {
		if schema != nil && schema != timeType && (schema.Kind() == reflect.Struct || schema.Kind() == reflect.Slice) {
			return fmt.Errorf("expected JSON container")
		}
		return nil
	}
	switch delim {
	case '{':
		if schema != nil && (schema.Kind() != reflect.Struct || schema == timeType) {
			return fmt.Errorf("unexpected object")
		}
		fields := map[string]reflect.Type{}
		required := map[string]bool{}
		if schema != nil {
			for i := 0; i < schema.NumField(); i++ {
				field := schema.Field(i)
				parts := strings.Split(field.Tag.Get("json"), ",")
				if parts[0] == "" || parts[0] == "-" {
					continue
				}
				fields[parts[0]] = field.Type
				required[parts[0]] = !slices.Contains(parts[1:], "omitempty")
			}
		}
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return fmt.Errorf("invalid or duplicate object key")
			}
			seen[key] = true
			field, exists := fields[key]
			if schema != nil && !exists {
				return fmt.Errorf("unknown field %q", key)
			}
			if err := checkValue(decoder, field, depth+1); err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
		}
		for key, needed := range required {
			if needed && !seen[key] {
				return fmt.Errorf("missing field %q", key)
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("unterminated object")
		}
		return nil
	case '[':
		if schema != nil && schema.Kind() != reflect.Slice {
			return fmt.Errorf("unexpected array")
		}
		var element reflect.Type
		if schema != nil {
			element = schema.Elem()
		}
		for decoder.More() {
			if err := checkValue(decoder, element, depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("unterminated array")
		}
		return nil
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
}

// JSON permits UTF-16 escapes, but encoding/json otherwise replaces lone
// surrogates. Reject those rather than silently changing durable identities.
func checkUnicode(data []byte) error {
	inside := false
	for i := 0; i < len(data); i++ {
		if !inside {
			if data[i] == '"' {
				inside = true
			}
			continue
		}
		if data[i] == '"' {
			inside = false
			continue
		}
		if data[i] != '\\' || i+1 >= len(data) {
			continue
		}
		i++
		if data[i] != 'u' {
			continue
		}
		code, ok := hexUnit(data[i+1:])
		if !ok {
			return fmt.Errorf("invalid Unicode escape")
		}
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return fmt.Errorf("unpaired Unicode surrogate")
		}
		if code >= 0xd800 && code <= 0xdbff {
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return fmt.Errorf("unpaired Unicode surrogate")
			}
			low, ok := hexUnit(data[i+3:])
			if !ok || low < 0xdc00 || low > 0xdfff {
				return fmt.Errorf("unpaired Unicode surrogate")
			}
			i += 6
		}
	}
	return nil
}

func hexUnit(data []byte) (uint16, bool) {
	if len(data) < 4 {
		return 0, false
	}
	var result uint16
	for _, digit := range data[:4] {
		result <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			result += uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			result += uint16(digit - 'a' + 10)
		case digit >= 'A' && digit <= 'F':
			result += uint16(digit - 'A' + 10)
		default:
			return 0, false
		}
	}
	return result, true
}

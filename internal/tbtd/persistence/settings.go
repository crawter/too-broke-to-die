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
	"os"
	"reflect"
	"unicode/utf8"

	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

const maxProfileFile = 1 << 20

type checkedFile struct {
	Magic   string          `json:"magic"`
	Schema  int             `json:"schema"`
	Digest  string          `json:"sha256"`
	Payload json.RawMessage `json:"payload"`
}

func encodeChecked(ctx context.Context, magic string, value any) (_ []byte, err error) {
	defer mon.Task()(&ctx)(&err)
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	return json.Marshal(checkedFile{Magic: magic, Schema: 1, Digest: hex.EncodeToString(sum[:]), Payload: payload})
}

func decodeChecked(ctx context.Context, data []byte, magic string, value any) (err error) {
	defer mon.Task()(&ctx)(&err)
	if len(data) == 0 || len(data) > maxProfileFile || !utf8.Valid(data) {
		return session.SaveCorrupt
	}
	if err = checkUnicode(data); err != nil {
		return fmt.Errorf("%w: %v", session.SaveCorrupt, err)
	}
	// Inspect a generic header before requiring the schema-one field layout.
	// Future versions may add, remove, or change every other envelope field.
	if err = checkJSON(data, nil); err != nil {
		return fmt.Errorf("%w: %v", session.SaveCorrupt, err)
	}
	var header map[string]json.RawMessage
	if err = json.Unmarshal(data, &header); err != nil || header == nil {
		return session.SaveCorrupt
	}
	var foundMagic string
	if json.Unmarshal(header["magic"], &foundMagic) != nil || foundMagic != magic {
		return session.SaveCorrupt
	}
	// Compare the integer token without converting it to a machine-sized int:
	// an otherwise valid future version must not become corrupt on overflow.
	schema := bytes.TrimSpace(header["schema"])
	if len(schema) == 0 || schema[0] < '1' || schema[0] > '9' {
		return session.SaveCorrupt
	}
	for _, digit := range schema[1:] {
		if digit < '0' || digit > '9' {
			return session.SaveCorrupt
		}
	}
	if len(schema) > 1 || schema[0] > '1' {
		return session.SaveTooNew
	}
	if err = checkJSON(data, reflect.TypeOf(checkedFile{})); err != nil {
		return fmt.Errorf("%w: %v", session.SaveCorrupt, err)
	}
	var file checkedFile
	if err = json.Unmarshal(data, &file); err != nil {
		return session.SaveCorrupt
	}
	sum := sha256.Sum256(file.Payload)
	if file.Digest != hex.EncodeToString(sum[:]) {
		return session.SaveCorrupt
	}
	if err = checkJSON(file.Payload, nil); err != nil {
		return fmt.Errorf("%w: %v", session.SaveCorrupt, err)
	}
	if err = json.Unmarshal(file.Payload, value); err != nil {
		return fmt.Errorf("%w: %v", session.SaveCorrupt, err)
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(canonical, file.Payload) {
		return session.SaveCorrupt
	}
	return nil
}

// installProfileFile validates the exact temporary bytes and confirms the
// installed copy. An error after rename is never reported as a success.
func (store *Store) installProfileFile(ctx context.Context, name string, data []byte) (installed bool, err error) {
	defer mon.Task()(&ctx)(&err)
	temp, err := store.files.WriteTemp(ctx, ".", data, maxProfileFile)
	if err != nil {
		return false, err
	}
	written, err := store.files.ReadFile(ctx, temp, maxProfileFile)
	if err != nil || !bytes.Equal(written, data) {
		return false, errors.Join(err, errors.New("temporary profile file differs"))
	}
	result, err := store.files.ReplaceFile(ctx, temp, name)
	if err != nil {
		return result.Installed, err
	}
	if err = store.files.SyncFile(ctx, name); err != nil {
		return true, err
	}
	written, err = store.files.ReadFile(ctx, name, maxProfileFile)
	if err != nil || !bytes.Equal(written, data) {
		return true, errors.Join(err, errors.New("installed profile file differs"))
	}
	return true, nil
}

// preserveProfile saves exact bytes under a fresh name before replacement.
func (store *Store) preserveProfile(ctx context.Context, data []byte) (err error) {
	defer mon.Task()(&ctx)(&err)
	temp, err := store.files.WriteTemp(ctx, ".", data, maxProfileFile)
	if err != nil {
		return err
	}
	copy, err := store.files.ReadFile(ctx, temp, maxProfileFile)
	if err != nil || !bytes.Equal(copy, data) {
		return errors.Join(err, errors.New("preserved profile copy differs"))
	}
	_, err = store.files.ReplaceFile(ctx, temp, temp+".preserved")
	return err
}

func (store *Store) readSettings(ctx context.Context, name string) (_ settings.Values, _ []byte, err error) {
	defer mon.Task()(&ctx)(&err)
	data, err := store.files.ReadFile(ctx, name, maxProfileFile)
	if errors.Is(err, platform.ErrTooLarge) {
		return settings.Values{}, nil, session.SaveCorrupt
	}
	if err != nil {
		return settings.Values{}, nil, err
	}
	var values settings.Values
	if err = decodeChecked(ctx, data, "TBTD-SETTINGS", &values); err != nil {
		return settings.Values{}, data, err
	}
	if err = values.Validate(ctx); err != nil {
		return settings.Values{}, data, fmt.Errorf("%w: %v", session.SaveCorrupt, err)
	}
	return values, data, nil
}

func (store *Store) processSettings(ctx context.Context, input session.SettingsInput) (result session.SettingsResultData) {
	defer mon.Task()(&ctx)(nil)
	result.ID = input.ID
	if store.pending != nil || store.selectorPending != nil {
		result.Status, result.Code = session.StorageConflicting, session.StorageConflict
		return result
	}
	if err := ctx.Err(); err != nil {
		result.Status, result.Code = session.StorageFailed, session.StorageDenied
		return result
	}
	values, old, err := store.readSettings(ctx, "settings.json")
	if input.Kind == session.LoadSettings {
		switch {
		case err == nil:
			result.Values, result.Status = values, session.StorageSucceeded
		case errors.Is(err, os.ErrNotExist):
			result.Values, result.Status = settings.Defaults(ctx), session.StorageSucceeded
		case errors.Is(err, session.SaveCorrupt):
			result.Values, result.Status, result.DefaultsRestored = settings.Defaults(ctx), session.StorageSucceeded, true
		case errors.Is(err, session.SaveTooNew):
			result.Status, result.Code = session.StorageTooNew, session.SaveTooNew
		default:
			result.Status, result.Code = session.StorageFailed, storageCode(err)
		}
		return result
	}
	if input.Kind != session.SaveSettings {
		result.Status, result.Code = session.StorageFailed, session.InvalidAction
		return result
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, session.SaveCorrupt) {
		failure := resultForError(err)
		result.Status, result.Code = failure.Status, failure.Code
		return result
	}
	if errors.Is(err, session.SaveCorrupt) {
		if old == nil {
			result.Status, result.Code = session.StorageFailed, session.SaveCorrupt
			return result
		}
		if err = store.preserveProfile(ctx, old); err != nil {
			result.Status, result.Code = session.StorageFailed, storageCode(err)
			return result
		}
	} else if err == nil {
		_, previous, previousErr := store.readSettings(ctx, "settings.previous.json")
		if errors.Is(previousErr, session.SaveCorrupt) && previous != nil {
			previousErr = store.preserveProfile(ctx, previous)
		}
		if previousErr != nil && !errors.Is(previousErr, os.ErrNotExist) {
			failure := resultForError(previousErr)
			result.Status, result.Code = failure.Status, failure.Code
			return result
		}
		if _, err = store.installProfileFile(ctx, "settings.previous.json", old); err != nil {
			result.Status, result.Code = session.StorageFailed, storageCode(err)
			return result
		}
	}
	if err = input.Values.Validate(ctx); err == nil {
		var data []byte
		data, err = encodeChecked(ctx, "TBTD-SETTINGS", input.Values)
		if err == nil {
			_, err = store.installProfileFile(ctx, "settings.json", data)
		}
	}
	if err != nil {
		result.Status, result.Code = session.StorageFailed, storageCode(err)
		return result
	}
	result.Status, result.Values = session.StorageSucceeded, input.Values
	return result
}

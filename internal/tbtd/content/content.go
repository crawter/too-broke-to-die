// Copyright (C) 2026 crawter
// All rights reserved.

// Package content loads and validates authored, adapter-independent definitions.
package content

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"unicode/utf8"

	monkit "github.com/spacemonkeygo/monkit/v3"
	"github.com/zeebo/errs"
)

var mon = monkit.Package()

var (
	// Error identifies unexpected content I/O failures.
	Error = errs.Class("content")
	// ErrInvalid identifies invalid authored data, including missing references.
	ErrInvalid = errs.Class("content invalid")
	// ErrMissingReference identifies an unresolved authored content identity.
	ErrMissingReference = errors.New("missing content reference")
)

const maxDocumentBytes = 8 << 20

// Load reads one UTF-8 JSON document from the supplied read-only filesystem.
// The path is only a source location; it never forms a gameplay identity.
func Load(ctx context.Context, filesystem fs.FS, name string, mode Mode) (_ *Catalogue, err error) {
	defer mon.Task()(&ctx)(&err)

	if filesystem == nil || !fs.ValidPath(name) {
		return nil, ErrInvalid.New("invalid content filesystem or path %q", name)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	file, err := filesystem.Open(name)
	if err != nil {
		return nil, Error.Wrap(fmt.Errorf("open content %q: %w", name, err))
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, Error.Wrap(fmt.Errorf("read content %q: %w", name, errs.Combine(readErr, closeErr)))
	}
	if len(data) > maxDocumentBytes {
		return nil, ErrInvalid.New("%s: content exceeds %d bytes", name, maxDocumentBytes)
	}
	if !utf8.Valid(data) {
		return nil, ErrInvalid.New("%s: invalid UTF-8", name)
	}
	if err = checkJSON(ctx, data); err != nil {
		return nil, ErrInvalid.Wrap(fmt.Errorf("%s: %w", name, err))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var definitions Definitions
	if err = decoder.Decode(&definitions); err != nil {
		return nil, ErrInvalid.Wrap(fmt.Errorf("%s: invalid definitions: %w", name, err))
	}
	return Validate(ctx, definitions, mode)
}

// checkJSON checks exact field spelling and duplicate object keys at every depth.
// Reflection is limited to the authored DTO schema; no runtime domain decisions
// or unchecked conversion depend on it.
func checkJSON(ctx context.Context, data []byte) (err error) {
	defer mon.Task()(&ctx)(&err)

	if err := checkSurrogates(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := checkValue(decoder, reflect.TypeOf(Definitions{}), "$", 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return fmt.Errorf("trailing input: %w", err)
	}
	return nil
}

// checkSurrogates prevents encoding/json from silently replacing malformed
// UTF-16 escape sequences with U+FFFD. Other syntax is checked by the decoder.
func checkSurrogates(data []byte) error {
	inString := false
	for i := 0; i < len(data); i++ {
		if !inString {
			if data[i] == '"' {
				inString = true
			}
			continue
		}
		if data[i] == '"' {
			inString = false
			continue
		}
		if data[i] != '\\' || i+1 >= len(data) {
			continue
		}
		i++
		if data[i] != 'u' {
			continue
		}
		value, ok := hexCodeUnit(data[i+1:])
		if !ok {
			return fmt.Errorf("invalid Unicode escape")
		}
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return fmt.Errorf("unpaired low surrogate")
		}
		if value >= 0xd800 && value <= 0xdbff {
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return fmt.Errorf("unpaired high surrogate")
			}
			low, ok := hexCodeUnit(data[i+3:])
			if !ok || low < 0xdc00 || low > 0xdfff {
				return fmt.Errorf("unpaired high surrogate")
			}
			i += 6
		}
	}
	return nil
}

func hexCodeUnit(data []byte) (uint16, bool) {
	if len(data) < 4 {
		return 0, false
	}
	var value uint16
	for _, digit := range data[:4] {
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value += uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value += uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value += uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func checkValue(decoder *json.Decoder, schema reflect.Type, path string, depth int) error {
	if depth > 64 {
		return fmt.Errorf("%s: nesting too deep", path)
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if token == nil {
		return fmt.Errorf("%s: null is not an authored value", path)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil // The typed decoder checks scalar types.
	}
	switch delimiter {
	case '{':
		if schema.Kind() != reflect.Struct {
			return fmt.Errorf("%s: unexpected object", path)
		}
		fields := make(map[string]reflect.Type, schema.NumField())
		for i := 0; i < schema.NumField(); i++ {
			field := schema.Field(i)
			fields[strings.Split(field.Tag.Get("json"), ",")[0]] = field.Type
		}
		seen := make(map[string]bool)
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return fmt.Errorf("%s: %w", path, keyErr)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("%s: expected object key", path)
			}
			if seen[key] {
				return fmt.Errorf("%s: duplicate key %q", path, key)
			}
			seen[key] = true
			fieldType, exists := fields[key]
			if !exists {
				return fmt.Errorf("%s: unknown field %q", path, key)
			}
			if err := checkValue(decoder, fieldType, path+"."+key, depth+1); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		if schema.Kind() != reflect.Slice {
			return fmt.Errorf("%s: unexpected array", path)
		}
		for index := 0; decoder.More(); index++ {
			if err := checkValue(decoder, schema.Elem(), fmt.Sprintf("%s[%d]", path, index), depth+1); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("%s: unexpected delimiter %q", path, delimiter)
	}
}

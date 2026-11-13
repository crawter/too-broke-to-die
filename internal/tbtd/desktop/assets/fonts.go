// Copyright (C) 2026 crawter
// All rights reserved.

package assets

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"

	"too-broke-to-die/internal/tbtd/content"
)

func glyphInventory(ctx context.Context, filesystem fs.FS, manifest *content.Manifest, extra []string) (_ []rune, err error) {
	defer mon.Task()(&ctx)(&err)
	set := make(map[rune]bool)
	add := func(text string) {
		for _, r := range text {
			if !unicode.IsControl(r) {
				set[r] = true
			}
		}
	}
	for r := rune(32); r <= 126; r++ {
		set[r] = true
	}
	for r := 'А'; r <= 'я'; r++ {
		set[r] = true
	}
	add("Ёё«»№₽$")
	for _, text := range extra {
		add(text)
	}
	seen := make(map[string]bool)
	var collect func(any)
	collect = func(value any) {
		switch value := value.(type) {
		case string:
			add(value)
		case []any:
			for _, item := range value {
				collect(item)
			}
		case map[string]any:
			for _, item := range value {
				collect(item)
			}
		}
	}
	readText := func(id, path string, meta *content.FileMetadata) error {
		if seen[path] {
			return nil
		}
		data, readErr := readExport(ctx, filesystem, id, path, meta)
		if readErr != nil {
			return readErr
		}
		var value any
		if decodeErr := json.Unmarshal(data, &value); decodeErr != nil {
			return assetFailure(id, decodeErr)
		}
		collect(value)
		seen[path] = true
		return nil
	}
	for _, record := range manifest.Records {
		if record.Kind == "text" {
			if err = readText(record.ID, record.ExportPath, record.File); err != nil {
				return nil, err
			}
		}
	}
	for _, identity := range manifest.Campaign {
		for _, file := range identity.SourceFiles {
			if file != nil && file.Path == "content/data/ru-services.json" {
				if err = readText(identity.ID, file.Path, file); err != nil {
					return nil, err
				}
			}
		}
	}
	if !seen["content/data/ru-services.json"] {
		return nil, content.ErrInvalid.New("missing service text glyph source")
	}
	glyphs := make([]rune, 0, len(set))
	for r := range set {
		glyphs = append(glyphs, r)
	}
	sort.Slice(glyphs, func(i, j int) bool { return glyphs[i] < glyphs[j] })
	return glyphs, nil
}

func (cache *Cache) loadFonts(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = cache.check(); err != nil {
		return err
	}
	for _, weight := range []string{"Regular", "SemiBold", "Bold"} {
		id := "FONT-NOTO-" + strings.ToUpper(weight)
		var found *content.ManifestRecord
		for i := range cache.records {
			if cache.records[i].ID == id {
				found = &cache.records[i]
				break
			}
		}
		if found == nil || found.Kind != "font" || found.LoadGroup != "shared" || !found.Final || found.Font == nil || found.Font.Family != "Noto Sans" || found.Font.Weight != weight {
			return assetFailure(id, errors.New("required final Noto Sans face unresolved"))
		}
		extension := strings.ToLower(path.Ext(found.ExportPath))
		if extension != ".ttf" && extension != ".otf" {
			return assetFailure(id, errors.New("unsupported font format"))
		}
		data, readErr := readExport(ctx, cache.filesystem, id, found.ExportPath, found.File)
		if readErr != nil {
			return readErr
		}
		if err = checkCoverage(data, cache.glyphs); err != nil {
			return assetFailure(id, err)
		}
		if err = checkFontName(data, found.Font.Weight); err != nil {
			return assetFailure(id, err)
		}
		font, loadErr := cache.native.font(extension, data, cache.glyphs)
		if loadErr != nil {
			return assetFailure(id, loadErr)
		}
		cache.fonts[id] = font
	}
	return nil
}

// checkFontName rejects a different face even if it happens to cover Cyrillic.
func checkFontName(data []byte, weight string) error {
	if len(data) < 12 {
		return errors.New("missing font name table")
	}
	count := int(binary.BigEndian.Uint16(data[4:6]))
	if 12+16*count > len(data) {
		return errors.New("invalid font directory")
	}
	var names []byte
	for i := 0; i < count; i++ {
		entry := data[12+16*i : 28+16*i]
		if string(entry[:4]) != "name" {
			continue
		}
		start, size := uint64(binary.BigEndian.Uint32(entry[8:12])), uint64(binary.BigEndian.Uint32(entry[12:16]))
		if names != nil || start+size > uint64(len(data)) {
			return errors.New("invalid font name table")
		}
		names = data[start : start+size]
	}
	if len(names) < 6 {
		return errors.New("missing font name table")
	}
	n := int(binary.BigEndian.Uint16(names[2:4]))
	base := int(binary.BigEndian.Uint16(names[4:6]))
	if 6+12*n > len(names) || base < 6+12*n || base > len(names) {
		return errors.New("invalid font names")
	}
	family, style := false, false
	for i := 0; i < n; i++ {
		entry := names[6+12*i : 18+12*i]
		platform := binary.BigEndian.Uint16(entry[:2])
		id := binary.BigEndian.Uint16(entry[6:8])
		if id != 1 && id != 2 && id != 16 && id != 17 {
			continue
		}
		length := int(binary.BigEndian.Uint16(entry[8:10]))
		offset := base + int(binary.BigEndian.Uint16(entry[10:12]))
		if offset+length > len(names) {
			return errors.New("truncated font name")
		}
		var value string
		switch platform {
		case 0, 3:
			if length%2 != 0 {
				return errors.New("invalid Unicode font name")
			}
			units := make([]uint16, length/2)
			for j := range units {
				units[j] = binary.BigEndian.Uint16(names[offset+2*j:])
			}
			value = string(utf16.Decode(units))
		case 1:
			value = string(names[offset : offset+length])
		default:
			continue
		}
		if (id == 1 || id == 16) && (value == "Noto Sans" || value == "Noto Sans "+weight) {
			family = true
		}
		if (id == 2 || id == 17) && strings.EqualFold(strings.ReplaceAll(value, " ", ""), weight) {
			style = true
		}
	}
	if !family || !style {
		return fmt.Errorf("font is not Noto Sans %s", weight)
	}
	return nil
}

// checkCoverage checks the font's Unicode cmap, rather than accepting raylib's
// fallback glyph. Bounds are checked before every untrusted table access.
func checkCoverage(data []byte, glyphs []rune) error {
	if len(data) < 12 || (string(data[:4]) != "\x00\x01\x00\x00" && string(data[:4]) != "OTTO" && string(data[:4]) != "true") {
		return errors.New("invalid sfnt font")
	}
	count := int(binary.BigEndian.Uint16(data[4:6]))
	if count == 0 || count > 4096 || 12+16*count > len(data) {
		return errors.New("invalid font directory")
	}
	var cmap []byte
	var maxGlyph uint16
	for i := 0; i < count; i++ {
		e := data[12+16*i : 28+16*i]
		start := uint64(binary.BigEndian.Uint32(e[8:12]))
		length := uint64(binary.BigEndian.Uint32(e[12:16]))
		if length == 0 || start+length > uint64(len(data)) {
			return errors.New("invalid font table bounds")
		}
		switch string(e[:4]) {
		case "cmap":
			if cmap != nil {
				return errors.New("duplicate cmap")
			}
			cmap = data[start : start+length]
		case "maxp":
			if length >= 6 {
				maxGlyph = binary.BigEndian.Uint16(data[start+4 : start+6])
			}
		}
	}
	if len(cmap) < 4 || maxGlyph == 0 || binary.BigEndian.Uint16(cmap[:2]) != 0 {
		return errors.New("missing Unicode cmap or glyph count")
	}
	n := int(binary.BigEndian.Uint16(cmap[2:4]))
	if n == 0 || 4+8*n > len(cmap) {
		return errors.New("invalid cmap directory")
	}
	var subtables [][]byte
	for i := 0; i < n; i++ {
		entry := cmap[4+8*i : 12+8*i]
		platform, encoding := binary.BigEndian.Uint16(entry[:2]), binary.BigEndian.Uint16(entry[2:4])
		if platform != 0 && !(platform == 3 && (encoding == 1 || encoding == 10)) {
			continue
		}
		offset := uint64(binary.BigEndian.Uint32(entry[4:8]))
		if offset+4 > uint64(len(cmap)) {
			return errors.New("truncated cmap")
		}
		sub := cmap[offset:]
		format := binary.BigEndian.Uint16(sub[:2])
		var length uint64
		switch format {
		case 4:
			length = uint64(binary.BigEndian.Uint16(sub[2:4]))
		case 12:
			if len(sub) < 16 {
				return errors.New("truncated cmap format 12")
			}
			length = uint64(binary.BigEndian.Uint32(sub[4:8]))
		default:
			continue
		}
		if length < 4 || length > uint64(len(sub)) {
			return errors.New("invalid cmap length")
		}
		subtables = append(subtables, sub[:length])
	}
	for _, r := range glyphs {
		if r < 0 || r > unicode.MaxRune {
			return fmt.Errorf("invalid codepoint U+%04X", r)
		}
		covered := false
		for _, sub := range subtables {
			glyph := cmapGlyph(sub, uint32(r))
			if glyph > 0 && glyph < uint32(maxGlyph) {
				covered = true
				break
			}
		}
		if !covered {
			return fmt.Errorf("missing glyph U+%04X", r)
		}
	}
	return nil
}

func cmapGlyph(sub []byte, code uint32) uint32 {
	if len(sub) < 16 {
		return 0
	}
	switch binary.BigEndian.Uint16(sub[:2]) {
	case 12:
		count := uint64(binary.BigEndian.Uint32(sub[12:16]))
		if count > uint64((len(sub)-16)/12) {
			return 0
		}
		for i := uint64(0); i < count; i++ {
			entry := sub[16+i*12 : 28+i*12]
			start, end := binary.BigEndian.Uint32(entry[:4]), binary.BigEndian.Uint32(entry[4:8])
			glyph := binary.BigEndian.Uint32(entry[8:12])
			if code >= start && code <= end && glyph <= ^uint32(0)-(code-start) {
				return glyph + code - start
			}
		}
	case 4:
		if code > 0xffff {
			return 0
		}
		segments := int(binary.BigEndian.Uint16(sub[6:8])) / 2
		if segments == 0 || 16+8*segments > len(sub) {
			return 0
		}
		endAt, startAt, deltaAt, rangeAt := 14, 16+2*segments, 16+4*segments, 16+6*segments
		for i := 0; i < segments; i++ {
			end := uint32(binary.BigEndian.Uint16(sub[endAt+2*i:]))
			start := uint32(binary.BigEndian.Uint16(sub[startAt+2*i:]))
			if code < start || code > end {
				continue
			}
			delta := uint32(binary.BigEndian.Uint16(sub[deltaAt+2*i:]))
			offset := int(binary.BigEndian.Uint16(sub[rangeAt+2*i:]))
			if offset == 0 {
				return (code + delta) & 0xffff
			}
			index := rangeAt + 2*i + offset + int(code-start)*2
			if index+2 > len(sub) {
				return 0
			}
			glyph := uint32(binary.BigEndian.Uint16(sub[index : index+2]))
			if glyph != 0 {
				return (glyph + delta) & 0xffff
			}
			return 0
		}
	}
	return 0
}

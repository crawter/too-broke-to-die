// Copyright (C) 2026 crawter
// All rights reserved.

package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"

	rl "github.com/gen2brain/raylib-go/raylib"

	"too-broke-to-die/internal/tbtd/content"
)

type fakeNative struct {
	next       uint32
	fail       string
	loads      []string
	unloads    []uint32
	fontLoads  int
	targetFail bool
}

func (fake *fakeNative) texture(data []byte) (rl.Texture2D, error) {
	fake.loads = append(fake.loads, "texture")
	if fake.fail == "texture" {
		return rl.Texture2D{}, errors.New("injected texture failure")
	}
	fake.next++
	return rl.Texture2D{ID: fake.next}, nil
}

func (fake *fakeNative) font(_ string, _ []byte, _ []rune) (rl.Font, error) {
	fake.fontLoads++
	if fake.fail == "font" && fake.fontLoads == 2 {
		return rl.Font{}, errors.New("injected font failure")
	}
	fake.next++
	return rl.Font{Texture: rl.Texture2D{ID: fake.next}}, nil
}

func (fake *fakeNative) target(_, _ int32) (rl.RenderTexture2D, error) {
	if fake.targetFail {
		return rl.RenderTexture2D{}, errors.New("injected target failure")
	}
	fake.next++
	return rl.RenderTexture2D{ID: fake.next}, nil
}

func (fake *fakeNative) unloadTexture(texture rl.Texture2D) {
	fake.unloads = append(fake.unloads, texture.ID)
}

func (fake *fakeNative) unloadFont(font rl.Font) {
	fake.unloads = append(fake.unloads, font.Texture.ID)
}
func (fake *fakeNative) unloadTarget(target rl.RenderTexture2D) {
	fake.unloads = append(fake.unloads, target.ID)
}

func fontFixture(weight string) []byte {
	data := make([]byte, 44+60)
	copy(data, "\x00\x01\x00\x00")
	binary.BigEndian.PutUint16(data[4:], 2)
	copy(data[12:], "cmap")
	binary.BigEndian.PutUint32(data[20:], 44)
	binary.BigEndian.PutUint32(data[24:], 52)
	copy(data[28:], "maxp")
	binary.BigEndian.PutUint32(data[36:], 96)
	binary.BigEndian.PutUint32(data[40:], 8)
	binary.BigEndian.PutUint16(data[46:], 1)
	binary.BigEndian.PutUint16(data[48:], 3)
	binary.BigEndian.PutUint16(data[50:], 10)
	binary.BigEndian.PutUint32(data[52:], 12)
	binary.BigEndian.PutUint16(data[56:], 12)
	binary.BigEndian.PutUint32(data[60:], 40)
	binary.BigEndian.PutUint32(data[68:], 2)
	binary.BigEndian.PutUint32(data[72:], 32)
	binary.BigEndian.PutUint32(data[76:], 126)
	binary.BigEndian.PutUint32(data[80:], 1)
	binary.BigEndian.PutUint32(data[84:], 0x400)
	binary.BigEndian.PutUint32(data[88:], 0x4ff)
	binary.BigEndian.PutUint32(data[92:], 100)
	binary.BigEndian.PutUint16(data[100:], 400)
	// Add a name table with test-only ASCII family and style records.
	result := append(append([]byte(nil), data[:44]...), make([]byte, 16)...)
	result = append(result, data[44:]...)
	binary.BigEndian.PutUint16(result[4:], 3)
	binary.BigEndian.PutUint32(result[20:], 60)
	binary.BigEndian.PutUint32(result[36:], 112)
	copy(result[44:], "name")
	binary.BigEndian.PutUint32(result[52:], uint32(len(result)))
	names := make([]byte, 30)
	binary.BigEndian.PutUint16(names[2:], 2)
	binary.BigEndian.PutUint16(names[4:], 30)
	binary.BigEndian.PutUint16(names[6:], 1)
	binary.BigEndian.PutUint16(names[12:], 1)
	binary.BigEndian.PutUint16(names[14:], uint16(len("Noto Sans")))
	binary.BigEndian.PutUint16(names[18:], 1)
	binary.BigEndian.PutUint16(names[24:], 2)
	binary.BigEndian.PutUint16(names[26:], uint16(len(weight)))
	binary.BigEndian.PutUint16(names[28:], uint16(len("Noto Sans")))
	names = append(names, []byte("Noto Sans"+weight)...)
	binary.BigEndian.PutUint32(result[56:], uint32(len(names)))
	return append(result, names...)
}

func pngFixture(t *testing.T, red uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: red, A: 254})
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func metadata(path string, data []byte) *content.FileMetadata {
	sum := sha256.Sum256(data)
	return &content.FileMetadata{Path: path, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

func fixture(t *testing.T) (fstest.MapFS, []content.ManifestRecord) {
	t.Helper()
	files := fstest.MapFS{}
	var records []content.ManifestRecord
	for _, weight := range []string{"Regular", "SemiBold", "Bold"} {
		id := "FONT-NOTO-" + strings.ToUpper(weight)
		font := fontFixture(weight)
		path := "content/fonts/" + id + ".ttf"
		files[path] = &fstest.MapFile{Data: font}
		records = append(records, content.ManifestRecord{ID: id, Kind: "font", LoadGroup: "shared", ExportPath: path, File: metadata(path, font), Final: true, Font: &content.FontRequirement{Family: "Noto Sans", Weight: weight}})
	}
	for i, item := range []struct{ id, group string }{{"UI-TEST", "shared"}, {"ART-HOS", "location-hos"}, {"ART-CHU", "location-chu"}, {"ART-END", "ending"}} {
		path := "content/art/" + item.id + ".png"
		data := pngFixture(t, uint8(i))
		files[path] = &fstest.MapFile{Data: data}
		records = append(records, content.ManifestRecord{ID: item.id, Kind: "visual", LoadGroup: item.group, Format: "RGBA PNG", ExportPath: path, File: metadata(path, data), Final: true})
	}
	return files, records
}

func newFixture(t *testing.T) (*Cache, fstest.MapFS, *fakeNative, *uint64) {
	t.Helper()
	files, records := fixture(t)
	native := &fakeNative{}
	thread := uint64(7)
	cache, err := newCache(t.Context(), files, records, func() uint64 { return thread }, []rune{'A', 'Я', 'Ё'}, native)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		thread = 7
		if err := cache.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return cache, files, native, &thread
}

func TestGroupsAndOwnership(t *testing.T) {
	cache, _, native, thread := newFixture(t)
	ctx := t.Context()
	ui, err := cache.Texture(ctx, "UI-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.LoadLocation(ctx, "LOC-HOS"); err != nil {
		t.Fatal(err)
	}
	hospital, err := cache.Texture(ctx, "ART-HOS")
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.LoadLocation(ctx, "LOC-CHU"); err != nil {
		t.Fatal(err)
	}
	again, err := cache.Texture(ctx, "UI-TEST")
	if err != nil || ui[0].ID != again[0].ID {
		t.Fatalf("shared texture not reused: %v", err)
	}
	if _, err := cache.Texture(ctx, "ART-HOS"); err == nil {
		t.Fatal("old group still exposed")
	}
	if len(native.unloads) != 1 || native.unloads[0] != hospital[0].ID {
		t.Fatalf("obsolete handle not released: %v", native.unloads)
	}
	if _, err := cache.Font(ctx, "FONT-NOTO-REGULAR"); err != nil {
		t.Fatal(err)
	}
	*thread = 8
	if err := cache.Close(ctx); !errors.Is(err, ErrThread) {
		t.Fatalf("wrong-thread close: %v", err)
	}
	if err := cache.LoadLocation(ctx, "ending"); !errors.Is(err, ErrThread) {
		t.Fatalf("wrong-thread load: %v", err)
	}
	*thread = 7
	if err := cache.LoadLocation(ctx, "ending"); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(ctx); err != nil {
		t.Fatal(err)
	}
	count := len(native.unloads)
	if err := cache.Close(ctx); err != nil || len(native.unloads) != count {
		t.Fatalf("repeated close unloaded again: %v", err)
	}
	if _, err := cache.Font(ctx, "FONT-NOTO-REGULAR"); !errors.Is(err, ErrClosed) {
		t.Fatalf("borrow after close: %v", err)
	}
}

func TestTransitRetiresLocation(t *testing.T) {
	cache, files, native, _ := newFixture(t)
	data := pngFixture(t, 51)
	path := "content/art/transit.png"
	files[path] = &fstest.MapFile{Data: data}
	cache.records = append(cache.records, content.ManifestRecord{ID: "ART-TRANSIT-HOS", Kind: "visual", LoadGroup: "location-hos", Visual: &content.VisualRecord{Kind: "transit"}, Format: "RGBA PNG", Final: true, ExportPath: path, File: metadata(path, data)})
	if err := cache.LoadLocation(t.Context(), "LOC-HOS"); err != nil {
		t.Fatal(err)
	}
	transit, err := cache.Texture(t.Context(), "ART-TRANSIT-HOS")
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.LoadTransit(t.Context(), "LOC-HOS"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Texture(t.Context(), "ART-HOS"); err == nil {
		t.Fatal("working location retained in transit")
	}
	again, err := cache.Texture(t.Context(), "ART-TRANSIT-HOS")
	if err != nil || again[0].ID != transit[0].ID || len(native.unloads) != 1 {
		t.Fatalf("transit not reused: %v, unloaded %v", err, native.unloads)
	}
	if err := cache.LoadTransit(t.Context(), "LOC-CHU"); err == nil {
		t.Fatal("missing transit group accepted")
	}
	if _, err := cache.Texture(t.Context(), "ART-TRANSIT-HOS"); err != nil {
		t.Fatal("failed transit replacement discarded previous group")
	}
}

func TestSharedExportReuseAndMetadataIsolation(t *testing.T) {
	files, records := fixture(t)
	for _, record := range records {
		if record.ID == "ART-HOS" {
			record.ID = "ART-HOS-SECOND"
			records = append(records, record)
			break
		}
	}
	native := &fakeNative{}
	cache, err := newCache(t.Context(), files, records, func() uint64 { return 1 }, []rune{'A'}, native)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cache.Close(context.Background()) })
	// Mutating the caller's metadata after construction must not alter the cache.
	for i := range records {
		if records[i].ID == "ART-HOS" {
			records[i].File.Size++
		}
	}
	if err := cache.LoadLocation(t.Context(), "LOC-HOS"); err != nil {
		t.Fatal(err)
	}
	first, err := cache.Texture(t.Context(), "ART-HOS")
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.Texture(t.Context(), "ART-HOS-SECOND")
	if err != nil || first[0].ID != second[0].ID || len(native.loads) != 2 {
		t.Fatalf("shared export was not reused: %v", err)
	}
	if err := cache.LoadLocation(t.Context(), "LOC-NONEXISTENT"); err == nil {
		t.Fatal("unknown group accepted")
	}
}

func TestGroupFailuresRetainOldResources(t *testing.T) {
	for _, change := range []string{"missing", "size", "digest", "format", "corrupt", "native"} {
		t.Run(change, func(t *testing.T) {
			cache, files, native, _ := newFixture(t)
			if err := cache.LoadLocation(t.Context(), "LOC-HOS"); err != nil {
				t.Fatal(err)
			}
			old, _ := cache.Texture(t.Context(), "ART-HOS")
			var record *content.ManifestRecord
			for i := range cache.records {
				if cache.records[i].ID == "ART-CHU" {
					record = &cache.records[i]
				}
			}
			switch change {
			case "missing":
				delete(files, record.ExportPath)
			case "size":
				record.File.Size++
			case "digest":
				record.File.SHA256 = strings.Repeat("0", 64)
			case "format":
				record.Format = "JPEG"
			case "corrupt":
				files[record.ExportPath] = &fstest.MapFile{Data: []byte("not a PNG")}
				record.File = metadata(record.ExportPath, files[record.ExportPath].Data)
			case "native":
				native.fail = "texture"
			}
			if err := cache.LoadLocation(t.Context(), "LOC-CHU"); err == nil || !strings.Contains(err.Error(), "ART-CHU") {
				t.Fatalf("expected identified failure: %v", err)
			}
			after, err := cache.Texture(t.Context(), "ART-HOS")
			if err != nil || after[0].ID != old[0].ID {
				t.Fatalf("old group lost: %v", err)
			}
		})
	}
}

func TestPartialAcquisitionRollback(t *testing.T) {
	files, records := fixture(t)
	native := &fakeNative{fail: "font"}
	_, err := newCache(t.Context(), files, records, func() uint64 { return 3 }, []rune{'A'}, native)
	if err == nil || !strings.Contains(err.Error(), "FONT-NOTO-SEMIBOLD") || len(native.unloads) != 1 {
		t.Fatalf("font rollback: %v, unloaded %v", err, native.unloads)
	}
	cache, files, native, _ := newFixture(t)
	data := pngFixture(t, 37)
	files["content/art/extra.png"] = &fstest.MapFile{Data: data}
	cache.records = append(cache.records, content.ManifestRecord{ID: "ART-FIRST", Kind: "visual", LoadGroup: "location-hos", Format: "RGBA PNG", ExportPath: "content/art/extra.png", File: metadata("content/art/extra.png", data), Final: true})
	cache.records = append(cache.records, content.ManifestRecord{ID: "ART-MISSING", Kind: "visual", LoadGroup: "location-hos", Format: "RGBA PNG", Final: true})
	if err := cache.LoadLocation(t.Context(), "LOC-HOS"); err == nil {
		t.Fatal("missing export accepted")
	}
	if len(native.unloads) != 2 {
		t.Fatalf("partial textures leaked: %v", native.unloads)
	}
}

func TestNativeFontAcquisition(t *testing.T) {
	// Pin Go-owned fixture arrays while the real raylib glyph lookup reads them.
	// No native device or window is initialized by these lookup functions.
	entries := []rl.GlyphInfo{{Value: '?'}, {Value: 'А'}, {Value: 'ё'}}
	rectangles := make([]rl.Rectangle, len(entries))
	var pins runtime.Pinner
	pins.Pin(&entries[0])
	pins.Pin(&rectangles[0])
	t.Cleanup(pins.Unpin)
	font := rl.Font{BaseSize: 32, CharsCount: int32(len(entries)), Chars: &entries[0], Recs: &rectangles[0], Texture: rl.Texture2D{ID: 10}}

	for _, test := range []struct {
		name          string
		defaultID     uint32
		font          rl.Font
		data          []byte
		glyphs        []rune
		wantError     string
		wantLoads     int
		wantUnloads   int
		wantGlyphCall int
	}{
		{name: "complete including first glyph", defaultID: 100, font: font, data: []byte{1}, glyphs: []rune{'?', 'А', 'ё'}, wantLoads: 1, wantGlyphCall: 3},
		{name: "fallback is not coverage", defaultID: 100, font: font, data: []byte{1}, glyphs: []rune{'?', 'Ж'}, wantError: "U+0416", wantLoads: 1, wantUnloads: 1, wantGlyphCall: 2},
		{name: "missing question mark is not index zero", defaultID: 100, font: rl.Font{BaseSize: 32, CharsCount: 2, Chars: &entries[1], Recs: &rectangles[1], Texture: font.Texture}, data: []byte{1}, glyphs: []rune{'?'}, wantError: "U+003F", wantLoads: 1, wantUnloads: 1, wantGlyphCall: 1},
		{name: "zero texture with allocated glyphs", defaultID: 100, font: rl.Font{BaseSize: font.BaseSize, CharsCount: font.CharsCount, Chars: font.Chars, Recs: font.Recs}, data: []byte{1}, glyphs: []rune{'А'}, wantError: "allocation failed", wantLoads: 1, wantUnloads: 1},
		{name: "invalid font with allocations", defaultID: 100, font: rl.Font{CharsCount: font.CharsCount, Chars: font.Chars, Recs: font.Recs, Texture: font.Texture}, data: []byte{1}, glyphs: []rune{'А'}, wantError: "allocation failed", wantLoads: 1, wantUnloads: 1},
		{name: "default font is borrowed", defaultID: 100, font: rl.Font{Texture: rl.Texture2D{ID: 100}}, data: []byte{1}, glyphs: []rune{'А'}, wantError: "default font", wantLoads: 1},
		{name: "uninitialized default prevents acquisition", defaultID: 0, font: font, data: []byte{1}, glyphs: []rune{'А'}, wantError: "not initialized"},
		{name: "empty data", defaultID: 100, font: font, glyphs: []rune{'А'}, wantError: "empty"},
		{name: "empty glyph inventory", defaultID: 100, font: font, data: []byte{1}, wantError: "empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var loads, glyphCalls int
			var unloaded []rl.Font
			native := fontFunctions{
				defaultFont: func() rl.Font { return rl.Font{Texture: rl.Texture2D{ID: test.defaultID}} },
				load: func(extension string, data []byte, size int32, glyphs []rune) rl.Font {
					loads++
					if extension != ".ttf" || size != 32 || !bytes.Equal(data, test.data) || !slices.Equal(glyphs, test.glyphs) {
						t.Error("native load received incorrect inputs")
					}
					return test.font
				},
				validFont:    rl.IsFontValid,
				validTexture: func(texture rl.Texture2D) bool { return texture.ID != 0 },
				glyphInfo: func(font rl.Font, codepoint rune) rl.GlyphInfo {
					glyphCalls++
					return rl.GetGlyphInfo(font, codepoint)
				},
				unload: func(font rl.Font) { unloaded = append(unloaded, font) },
			}
			got, err := loadNativeFont(".ttf", test.data, test.glyphs, native)
			if (test.wantError == "" && err != nil) || (test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError))) {
				t.Fatalf("unexpected result: %v", err)
			}
			if loads != test.wantLoads || len(unloaded) != test.wantUnloads || glyphCalls != test.wantGlyphCall {
				t.Fatalf("loads=%d unloads=%d glyph checks=%d", loads, len(unloaded), glyphCalls)
			}
			if len(unloaded) > 0 && (unloaded[0].Chars != test.font.Chars || unloaded[0].Recs != test.font.Recs || unloaded[0].Texture.ID != test.font.Texture.ID) {
				t.Fatal("cleanup did not receive the complete acquired font")
			}
			if err == nil && got.Texture.ID != test.font.Texture.ID {
				t.Fatal("successful acquisition did not transfer the font")
			}
		})
	}
}

func TestFontCoverageAndTargets(t *testing.T) {
	data := fontFixture("Regular")
	if err := checkFontName(data, "Regular"); err != nil {
		t.Fatal(err)
	}
	if err := checkFontName(data, "Bold"); err == nil {
		t.Fatal("wrong font weight accepted")
	}
	if err := checkCoverage(data, []rune{'A', 'Я', 'Ё'}); err != nil {
		t.Fatal(err)
	}
	if err := checkCoverage(data, []rune{'₽'}); err == nil {
		t.Fatal("missing glyph accepted")
	}
	for _, length := range []int{0, 20, 57, len(data) - 1} {
		if err := checkCoverage(data[:length], []rune{'A'}); err == nil {
			t.Fatalf("accepted truncated font %d", length)
		}
	}
	cache, _, native, _ := newFixture(t)
	first, err := cache.ReplaceTarget(t.Context(), "screen", 1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	native.targetFail = true
	if _, err := cache.ReplaceTarget(t.Context(), "screen", 1920, 1080); err == nil || cache.targets["screen"].ID != first.ID {
		t.Fatalf("failed replacement lost target: %v", err)
	}
	native.targetFail = false
	if _, err := cache.ReplaceTarget(t.Context(), "screen", 1920, 1080); err != nil {
		t.Fatal(err)
	}
	if len(native.unloads) != 1 || native.unloads[0] != first.ID {
		t.Fatalf("old target not unloaded: %v", native.unloads)
	}
}

func inventoryFixture() (fstest.MapFS, *content.Manifest, map[string]string) {
	files := fstest.MapFS{}
	manifest := &content.Manifest{}
	ids := make(map[string]string)
	for _, item := range []struct{ name, id, text string }{
		{"ru-interface", "TXT-INTERFACE", "α"},
		{"ru-dialogue", "DLG-DIALOGUE", "β"},
		{"ru-tutorials", "TUT-TUTORIAL", "γ"},
		{"ru-captions", "CAP-CAPTION", "δ"},
		{"ru-signs", "TXT-SIGN", "ε"},
	} {
		path := "content/data/" + item.name + ".json"
		data := []byte(`{"entries":[{"text":"` + item.text + `"}],"☃":1}`)
		files[path] = &fstest.MapFile{Data: data}
		ids[path] = item.id
		manifest.Records = append(manifest.Records, content.ManifestRecord{ID: item.id, Kind: "text", ExportPath: path, File: metadata(path, data)})
	}
	servicePath := "content/data/ru-services.json"
	service := []byte(`{"institutions":[{"tickets":[{"label":"Ω"}]}]}`)
	files[servicePath] = &fstest.MapFile{Data: service}
	ids[servicePath] = "D-HOS-SUP-01"
	manifest.Campaign = []content.CampaignIdentity{{ID: ids[servicePath], SourceFiles: []*content.FileMetadata{metadata(servicePath, service)}}}
	return files, manifest, ids
}

func TestGlyphInventorySources(t *testing.T) {
	files, manifest, _ := inventoryFixture()
	// Several records can share a text export; the inventory must remain a set.
	manifest.Records = append(manifest.Records, manifest.Records[0])
	manifest.Records = append(manifest.Records, content.ManifestRecord{ID: "ART-IGNORED", Kind: "visual"})
	manifest.Campaign = append(manifest.Campaign, content.CampaignIdentity{ID: "OTHER", SourceFiles: []*content.FileMetadata{nil, {Path: "content/data/other.json"}}})
	extra := []string{"binding ★\n\t", "★"}
	glyphs, err := glyphInventory(t.Context(), files, manifest, extra)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []rune{'α', 'β', 'γ', 'δ', 'ε', 'Ω', '★', 'Ё', 'ё', '№', '₽', '$'} {
		if !slices.Contains(glyphs, r) {
			t.Errorf("missing source glyph %U", r)
		}
	}
	for _, r := range []rune{'\n', '\t', '☃'} {
		if slices.Contains(glyphs, r) {
			t.Errorf("unexpected control or JSON key glyph %U", r)
		}
	}
	if !slices.IsSorted(glyphs) {
		t.Fatal("glyph inventory is not sorted")
	}
	for i := 1; i < len(glyphs); i++ {
		if glyphs[i-1] == glyphs[i] {
			t.Fatalf("duplicate glyph %U", glyphs[i])
		}
	}
	reordered := *manifest
	order := append([]content.ManifestRecord(nil), manifest.Records...)
	slices.Reverse(order)
	reordered.Records = order
	other, err := glyphInventory(t.Context(), files, &reordered, extra)
	if err != nil || !slices.Equal(glyphs, other) {
		t.Fatalf("inventory changes with record order: %v", err)
	}
}

func TestGlyphInventorySourceFailures(t *testing.T) {
	_, _, paths := inventoryFixture()
	for path, id := range paths {
		for _, failure := range []string{"missing", "corrupt JSON", "size mismatch", "digest mismatch"} {
			t.Run(id+"/"+failure, func(t *testing.T) {
				files, manifest, _ := inventoryFixture()
				var file *content.FileMetadata
				if path == "content/data/ru-services.json" {
					file = manifest.Campaign[0].SourceFiles[0]
				} else {
					for _, record := range manifest.Records {
						if record.ExportPath == path {
							file = record.File
						}
					}
				}
				if file == nil {
					t.Fatal("fixture metadata missing")
				}
				switch failure {
				case "missing":
					delete(files, path)
				case "corrupt JSON":
					data := []byte("{")
					files[path] = &fstest.MapFile{Data: data}
					*file = *metadata(path, data)
				case "size mismatch":
					file.Size++
				case "digest mismatch":
					file.SHA256 = strings.Repeat("0", 64)
				}
				_, err := glyphInventory(t.Context(), files, manifest, nil)
				if err == nil || !content.ErrInvalid.Has(err) || !strings.Contains(err.Error(), id) {
					t.Fatalf("expected identified content failure for %s: %v", id, err)
				}
			})
		}
	}
	files, manifest, _ := inventoryFixture()
	manifest.Campaign[0].SourceFiles = nil
	if _, err := glyphInventory(t.Context(), files, manifest, nil); err == nil || !strings.Contains(err.Error(), "service text") {
		t.Fatalf("missing service source accepted: %v", err)
	}
	cancel, stop := context.WithCancel(t.Context())
	stop()
	if _, err := glyphInventory(cancel, files, manifest, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not preserved: %v", err)
	}
}

func TestGlyphInventoryShippedText(t *testing.T) {
	root, err := os.OpenRoot("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	filesystem := root.FS()
	manifest, err := content.GenerateManifest(t.Context(), filesystem)
	if err != nil {
		t.Fatal(err)
	}
	glyphs, err := glyphInventory(t.Context(), filesystem, manifest, []string{"binding ★"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(glyphs, '★') {
		t.Fatal("additional binding text missing")
	}
	var checkValues func(any)
	checkValues = func(value any) {
		switch value := value.(type) {
		case string:
			for _, r := range value {
				if !unicode.IsControl(r) && !slices.Contains(glyphs, r) {
					t.Errorf("shipped text glyph %U missing", r)
				}
			}
		case []any:
			for _, item := range value {
				checkValues(item)
			}
		case map[string]any:
			for _, item := range value {
				checkValues(item)
			}
		}
	}
	for _, path := range []string{"content/data/ru-interface.json", "content/data/ru-dialogue.json", "content/data/ru-tutorials.json", "content/data/ru-captions.json", "content/data/ru-signs.json", "content/data/ru-services.json"} {
		data, err := fs.ReadFile(filesystem, path)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		checkValues(value)
	}
}

func TestReadExportBoundaries(t *testing.T) {
	data := []byte("test")
	files := fstest.MapFS{"content/test": &fstest.MapFile{Data: data}}
	meta := metadata("content/test", data)
	if got, err := readExport(t.Context(), files, "ART-TEST", "content/test", meta); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("valid export: %v", err)
	}
	for _, path := range []string{"../test", "outside/test", "content/missing"} {
		bad := *meta
		bad.Path = path
		if _, err := readExport(t.Context(), files, "ART-TEST", path, &bad); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	cancel, stop := context.WithCancel(t.Context())
	stop()
	if _, err := readExport(cancel, files, "ART-TEST", "content/test", meta); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestConfinedFilesystem(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "content"), 0700); err != nil {
		t.Fatal(err)
	}
	data := pngFixture(t, 11)
	if err := os.WriteFile(filepath.Join(directory, "content", "sprite.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "content", "escape.png")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	path := "content/sprite.png"
	if _, err := readExport(t.Context(), root.FS(), "ART-TEST", path, metadata(path, data)); err != nil {
		t.Fatal(err)
	}
	path = "content/escape.png"
	if _, err := readExport(t.Context(), root.FS(), "ART-TEST", path, metadata(path, data)); err == nil {
		t.Fatal("followed symlink outside content root")
	}
}

func TestProductionMissingFontIsContentFailure(t *testing.T) {
	root, err := os.OpenRoot("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	var filesystem fs.FS = root.FS()
	manifest, err := content.GenerateManifest(t.Context(), filesystem)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(t.Context(), filesystem, manifest, func() uint64 { return 1 }, nil)
	if err == nil || !strings.Contains(err.Error(), "FONT-NOTO-REGULAR") {
		t.Fatalf("missing final font not reported: %v", err)
	}
}

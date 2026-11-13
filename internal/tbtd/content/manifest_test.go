// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// The display-free check command runs Go tests rather than scripts/check.sh.
// Run the real generator in that lane, then verify its output against the registers.
func TestGenerateManifestArtifact(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "go", "run", "./cmd/tbtdcontent")
	cmd.Dir = "../../.."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate manifest: %v: %s", err, output)
	}
	root, err := os.OpenRoot("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	manifest := generated(t, root.FS())
	want, err := ManifestBytes(t.Context(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(root.FS(), "content/manifest.json")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("generated manifest differs from registers: %v", err)
	}
}

type overlayFS struct {
	base  fs.FS
	files fstest.MapFS
}

func (overlay overlayFS) Open(name string) (fs.File, error) {
	if _, ok := overlay.files[name]; ok {
		return overlay.files.Open(name)
	}
	return overlay.base.Open(name)
}

func manifestFS() overlayFS {
	return overlayFS{base: os.DirFS("../../.."), files: fstest.MapFS{}}
}

func generated(t *testing.T, filesystem fs.FS) *Manifest {
	t.Helper()
	manifest, err := GenerateManifest(context.Background(), filesystem)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func manifestEntry(t *testing.T, manifest *Manifest, id string) ManifestRecord {
	t.Helper()
	for _, record := range manifest.Records {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("missing record %s", id)
	return ManifestRecord{}
}

func TestManifestInventoryAndDeterminism(t *testing.T) {
	ctx := context.Background()
	filesystem := manifestFS()
	first := generated(t, filesystem)
	second := generated(t, filesystem)
	a, err := ManifestBytes(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ManifestBytes(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("manifest bytes differ")
	}
	counts := map[string]int{}
	for i, record := range first.Records {
		if i > 0 && first.Records[i-1].ID >= record.ID {
			t.Fatalf("records not sorted: %s", record.ID)
		}
		counts[record.Kind]++
		if record.Kind != "text" && (record.File != nil || record.ExportPath != "") {
			t.Fatalf("fabricated export: %s", record.ID)
		}
		if record.Provenance == nil || record.Provenance.Authorship != "" || len(record.Provenance.Evidence) != 0 {
			t.Fatalf("fabricated provenance: %s", record.ID)
		}
	}
	for kind, want := range map[string]int{"music": 18, "stinger": 8, "ambience": 6, "effect": 100, "vocal": 35, "font": 3, "audio-tail": 18, "audio-mix": 4, "audio-score": 1} {
		if counts[kind] != want {
			t.Errorf("%s: got %d, want %d", kind, counts[kind], want)
		}
	}
	visual, _, _ := visualFixture(t)
	if counts["visual"] != len(visual.Records(ctx)) {
		t.Fatal("visual register was not incorporated")
	}
	for _, code := range []string{"HOS", "CHU", "MIN", "INS"} {
		for _, attack := range []string{"A", "B", "C"} {
			for _, phase := range []string{"PREP", "ACTIVE", "RECOVER"} {
				id := "SFX-BOSS-" + code + "-" + attack + "-" + phase
				record := manifestEntry(t, first, id)
				if record.Audio.Caption != "CAP-BOSS-"+code+"-"+attack+"-"+phase || record.LoadGroup != "location-"+strings.ToLower(code) {
					t.Errorf("bad boss binding: %+v", record)
				}
			}
		}
	}
	for _, test := range []struct {
		event, owner, scene string
	}{
		{"DEATH", "NAR-10", "END-DEATH"},
		{"HELL", "NAR-11", "END-HELL"},
		{"APPOINTMENT", "NAR-11", "END-HELL"},
		{"POWER", "NAR-12", "END-CEREMONY"},
		{"RIBBON", "NAR-12", "END-CEREMONY"},
		{"APPLAUSE", "NAR-12", "END-CEREMONY"},
		{"FIRE", "NAR-12", "END-CEREMONY"},
	} {
		record := manifestEntry(t, first, "SFX-END-"+test.event)
		if record.Audio.EventOwner != test.owner+":"+test.event || !slices.Contains(record.Bindings, test.owner) || !slices.Contains(record.Bindings, test.scene) {
			t.Errorf("wrong ending event owner or scene: %+v", record)
		}
	}
	for _, surface := range []string{"TILE", "WOOD", "CONCRETE", "METAL"} {
		if got := manifestEntry(t, first, "SFX-MOVE-"+surface).Audio.Variations; got != 4 {
			t.Errorf("%s variations = %d", surface, got)
		}
	}
	if record := manifestEntry(t, first, "ART-END-TABLEAU"); record.Visual == nil || record.Visual.Tableau == nil {
		t.Error("mandatory final tableau missing")
	}
	if record := manifestEntry(t, first, "TXT-ENDING-TABLEAU"); record.File == nil || record.SourcePath == "" {
		t.Error("delivered text source missing its metadata")
	}
	if err := ValidateManifest(ctx, filesystem, first, false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateManifest(ctx, filesystem, first, true); err == nil {
		t.Fatal("strict validation accepted missing final media and rights")
	}
}

func TestCampaignIdentityMappings(t *testing.T) {
	ctx := context.Background()
	filesystem := manifestFS()
	manifest := generated(t, filesystem)
	campaign, err := LoadCampaign(ctx, filesystem, "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	ids := make(map[string]CampaignIdentity)
	for i, item := range manifest.Campaign {
		if i > 0 && manifest.Campaign[i-1].ID >= item.ID {
			t.Fatalf("campaign IDs not strictly sorted: %s", item.ID)
		}
		counts[item.Kind]++
		ids[item.ID] = item
		if len(item.SourceFiles) == 0 {
			t.Fatalf("%s has no authoritative source", item.ID)
		}
		for _, source := range item.SourceFiles {
			actual, err := fingerprint(ctx, filesystem, source.Path)
			if err != nil || actual == nil || *actual != *source {
				t.Fatalf("%s: invalid source metadata: %v", item.ID, err)
			}
		}
	}
	for kind, want := range map[string]int{"deed": 100, "consequence": 100, "object": len(campaign.Objects(ctx)), "emergency": 16, "gate": 7, "location": 4, "encounter": 23, "anchor": 32, "narrative": 12, "ending": 6} {
		if counts[kind] != want {
			t.Errorf("%s count = %d, want %d", kind, counts[kind], want)
		}
	}
	for _, deed := range campaign.Deeds(ctx) {
		item, ok := ids[string(deed.Deed.ID)]
		if !ok || item.Kind != "deed" || !slices.Contains(item.Bindings, string(deed.Deed.Consequence)) || !slices.Contains(item.Bindings, string(deed.Deed.Encounter)) || !slices.Contains(item.Bindings, string(deed.Deed.Anchor)) {
			t.Fatalf("missing or incorrect deed mapping %s", deed.Deed.ID)
		}
	}
	broken := *manifest
	broken.Campaign = append([]CampaignIdentity(nil), manifest.Campaign...)
	broken.Campaign[0].Bindings = []string{"D-HOS-SUP-99"}
	if err := validateManifest(ctx, filesystem, &broken, false, nil); err == nil {
		t.Fatal("dangling campaign reference accepted")
	}
}

func TestManifestDeliveredFileAndProvenance(t *testing.T) {
	ctx := context.Background()
	filesystem := manifestFS()
	data := waveFixture(16) // Synthetic audio is a test fixture, not production media.
	filesystem.files["content/audio/fixture.wav"] = &fstest.MapFile{Data: data}
	filesystem.files["content/evidence/fixture.txt"] = &fstest.MapFile{Data: []byte("fixture evidence")}
	rights := provenanceSource{SchemaVersion: 1, ReviewPolicy: "Evidence review only", Deliveries: []deliveryEntry{{AssetID: "SFX-MOVE-TILE", SourcePath: "content/source/fixture.wav", ExportPath: "content/audio/fixture.wav", AdditionalExports: []string{}}}, Sources: []provenanceEntry{{AssetID: "SFX-MOVE-TILE", Authorship: "fixture author", SourceMaterial: "fixture", License: "fixture terms", Permission: "fixture permission", PerformerConsent: "not applicable: no performer", Attribution: "fixture credit", Review: "fixture reviewed", Evidence: []string{"content/evidence/fixture.txt"}}}}
	encoded, err := json.Marshal(rights)
	if err != nil {
		t.Fatal(err)
	}
	filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
	manifest := generated(t, filesystem)
	record := manifestEntry(t, manifest, "SFX-MOVE-TILE")
	sum := sha256.Sum256(data)
	if record.File == nil || record.File.Size != int64(len(data)) || record.File.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("bad digest: %+v", record.File)
	}
	if err := ValidateManifest(ctx, filesystem, manifest, false); err != nil {
		t.Fatal(err)
	}
	for i := range manifest.Records {
		if manifest.Records[i].ID == record.ID {
			manifest.Records[i].File.Size++
			break
		}
	}
	if err := ValidateManifest(ctx, filesystem, manifest, false); err == nil {
		t.Fatal("changed metadata accepted")
	}
	for i := range manifest.Records {
		if manifest.Records[i].ID == record.ID {
			manifest.Records[i].File.Size--
			break
		}
	}
	filesystem.files["content/audio/fixture.wav"] = &fstest.MapFile{Data: []byte("different bytes")}
	if err := ValidateManifest(ctx, filesystem, manifest, false); err == nil {
		t.Fatal("changed file accepted")
	}
}

func TestConfinedFileReferences(t *testing.T) {
	rootPath := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(rootPath, "content")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := fingerprint(context.Background(), root.FS(), "content/secret"); err == nil {
		t.Fatal("symlink escaped the root")
	}
}

func TestManifestInvalidSourcesAndPaths(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name string
		data string
	}{
		{"unknown asset", `{"schema_version":1,"review_policy":"review","deliveries":[{"asset_id":"SFX-NOPE","source_path":"","export_path":"content/audio/x.wav"}],"sources":[]}`},
		{"traversal", `{"schema_version":1,"review_policy":"review","deliveries":[{"asset_id":"SFX-MOVE-TILE","source_path":"","export_path":"content/../x"}],"sources":[]}`},
		{"windows path", `{"schema_version":1,"review_policy":"review","deliveries":[{"asset_id":"SFX-MOVE-TILE","source_path":"","export_path":"content/\\x"}],"sources":[]}`},
		{"duplicate key", `{"schema_version":1,"schema_version":1,"review_policy":"review","deliveries":[],"sources":[]}`},
		{"unknown field", `{"schema_version":1,"review_policy":"review","deliveries":[],"sources":[],"other":true}`},
		{"duplicate delivery", `{"schema_version":1,"review_policy":"review","deliveries":[{"asset_id":"SFX-MOVE-TILE","source_path":"","export_path":"content/audio/x.wav"},{"asset_id":"SFX-MOVE-TILE","source_path":"","export_path":"content/audio/y.wav"}],"sources":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			filesystem := manifestFS()
			filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: []byte(test.data)}
			if _, err := GenerateManifest(ctx, filesystem); err == nil {
				t.Fatal("invalid register accepted")
			}
		})
	}
	for _, test := range []struct {
		name, path, old, replacement string
	}{
		{"duplicate music identity", "content/catalogue/audio.json", "MUS-HOS-CBT", "MUS-HOS-EXP"},
		{"missing effect family", "content/catalogue/audio.json", `"family":"WARN"`, `"family":"WORN"`},
		{"missing Cyrillic glyphs", "content/catalogue/fonts.json", "А–Я", "ASCII"},
	} {
		t.Run(test.name, func(t *testing.T) {
			filesystem := manifestFS()
			data, err := fs.ReadFile(filesystem, test.path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), test.old) {
				t.Fatalf("fixture missing %q", test.old)
			}
			filesystem.files[test.path] = &fstest.MapFile{Data: []byte(strings.Replace(string(data), test.old, test.replacement, 1))}
			if _, err := GenerateManifest(ctx, filesystem); err == nil {
				t.Fatal("malformed source register accepted")
			}
		})
	}
	filesystem := manifestFS()
	manifest := generated(t, filesystem)
	duplicate := *manifest
	duplicate.Records = append(append([]ManifestRecord(nil), manifest.Records...), manifest.Records[0])
	if err := ValidateManifest(ctx, filesystem, &duplicate, false); err == nil {
		t.Fatal("duplicate record accepted")
	}
	broken := *manifest
	broken.Records = append([]ManifestRecord(nil), manifest.Records...)
	broken.Records[0].Bindings = []string{"CAP-NOT-EXIST"}
	if err := ValidateManifest(ctx, filesystem, &broken, false); err == nil {
		t.Fatal("dangling caption accepted")
	}
	if reflect.DeepEqual(manifest.Records[0].Bindings, broken.Records[0].Bindings) {
		t.Fatal("test mutated original")
	}
}

func waveFixture(bits uint16) []byte {
	payload := int(bits / 8)
	data := make([]byte, 44+payload)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], 1)
	binary.LittleEndian.PutUint32(data[24:28], 48000)
	binary.LittleEndian.PutUint32(data[28:32], 48000*uint32(payload))
	binary.LittleEndian.PutUint16(data[32:34], uint16(payload))
	binary.LittleEndian.PutUint16(data[34:36], bits)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:44], uint32(payload))
	return data
}

// These tiny files are synthetic, complete containers used only by tests.
type vorbisBits struct {
	data []byte
	bits uint
}

func (bits *vorbisBits) put(value uint32, count uint) {
	for i := uint(0); i < count; i++ {
		if bits.bits%8 == 0 {
			bits.data = append(bits.data, 0)
		}
		bits.data[len(bits.data)-1] |= byte((value >> i) & 1 << (bits.bits % 8))
		bits.bits++
	}
}

func oggFixture() []byte {
	return oggFixtureWithGranule(96)
}

func oggFixtureWithGranule(granule uint64) []byte {
	ident := make([]byte, 30)
	copy(ident, "\x01vorbis")
	ident[11] = 2
	binary.LittleEndian.PutUint32(ident[12:16], 48000)
	block := byte(6)
	if granule > 97 {
		block = 13
	}
	ident[28], ident[29] = block<<4|block, 1
	comment := append([]byte("\x03vorbis"), make([]byte, 8)...)
	comment = append(comment, 1)
	var bits vorbisBits
	// One codebook, floor, empty residue, mapping and short-block mode.
	bits.put(0, 8)
	bits.put(0x564342, 24)
	bits.put(1, 16)
	bits.put(1, 24)
	bits.put(0, 1)
	bits.put(0, 1)
	bits.put(0, 5)
	bits.put(0, 4)
	bits.put(0, 6)
	bits.put(0, 16)
	bits.put(0, 6)
	bits.put(1, 16)
	bits.put(0, 5)
	bits.put(0, 2)
	bits.put(0, 4)
	bits.put(0, 6)
	bits.put(0, 16)
	bits.put(0, 24)
	bits.put(0, 24)
	bits.put(0, 24)
	bits.put(0, 6)
	bits.put(0, 8)
	bits.put(0, 3)
	bits.put(0, 1)
	bits.put(0, 6)
	bits.put(0, 16)
	bits.put(0, 1)
	bits.put(0, 1)
	bits.put(0, 2)
	bits.put(0, 8)
	bits.put(0, 8)
	bits.put(0, 8)
	bits.put(0, 6)
	bits.put(0, 1)
	bits.put(0, 16)
	bits.put(0, 16)
	bits.put(0, 8)
	bits.put(1, 1)
	setup := append([]byte("\x05vorbis"), bits.data...)
	var stream []byte
	pages := []struct {
		flags   byte
		granule uint64
		packets [][]byte
	}{
		{2, 0, [][]byte{ident}},
		{0, 0, [][]byte{comment, setup}},
	}
	count := 4
	if block == 13 {
		count = int((granule+4095)/4096) + 1
	}
	for done := 0; done < count; {
		n := min(255, count-done)
		packets := make([][]byte, n)
		for i := range packets {
			packets[i] = []byte{0}
		}
		done += n
		flags := byte(0)
		position := uint64(done-1) * (uint64(1) << (block - 1))
		if done == count {
			flags, position = 4, granule
		}
		pages = append(pages, struct {
			flags   byte
			granule uint64
			packets [][]byte
		}{flags: flags, granule: position, packets: packets})
	}
	for sequence, page := range pages {
		header := make([]byte, 27+len(page.packets))
		copy(header, "OggS")
		header[5] = page.flags
		binary.LittleEndian.PutUint64(header[6:14], page.granule)
		binary.LittleEndian.PutUint32(header[14:18], 1)
		binary.LittleEndian.PutUint32(header[18:22], uint32(sequence))
		header[26] = byte(len(page.packets))
		for i, packet := range page.packets {
			header[27+i] = byte(len(packet))
			header = append(header, packet...)
		}
		var crc uint32
		for _, value := range header {
			crc ^= uint32(value) << 24
			for bit := 0; bit < 8; bit++ {
				if crc&0x80000000 != 0 {
					crc = crc<<1 ^ 0x04c11db7
				} else {
					crc <<= 1
				}
			}
		}
		binary.LittleEndian.PutUint32(header[22:26], crc)
		stream = append(stream, header...)
	}
	return stream
}

func fontFixture() []byte {
	cmap := make([]byte, 274)
	binary.BigEndian.PutUint16(cmap[2:4], 1)
	binary.BigEndian.PutUint16(cmap[4:6], 3)
	binary.BigEndian.PutUint16(cmap[6:8], 1)
	binary.BigEndian.PutUint32(cmap[8:12], 12)
	binary.BigEndian.PutUint16(cmap[14:16], 262)
	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head[:4], 0x00010000)
	binary.BigEndian.PutUint32(head[12:16], 0x5f0f3cf5)
	binary.BigEndian.PutUint16(head[18:20], 1000)
	hhea := make([]byte, 36)
	binary.BigEndian.PutUint32(hhea[:4], 0x00010000)
	binary.BigEndian.PutUint16(hhea[34:36], 1)
	maxp := make([]byte, 32)
	binary.BigEndian.PutUint32(maxp[:4], 0x00010000)
	binary.BigEndian.PutUint16(maxp[4:6], 1)
	name := make([]byte, 6)
	binary.BigEndian.PutUint16(name[4:6], 6)
	tables := []struct {
		tag  string
		data []byte
	}{{"cmap", cmap}, {"glyf", []byte{0}}, {"head", head}, {"hhea", hhea}, {"hmtx", make([]byte, 4)}, {"loca", make([]byte, 4)}, {"maxp", maxp}, {"name", name}}
	data := make([]byte, 12+16*len(tables))
	binary.BigEndian.PutUint32(data[:4], 0x00010000)
	binary.BigEndian.PutUint16(data[4:6], uint16(len(tables)))
	for i, item := range tables {
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
		entry := data[12+i*16 : 28+i*16]
		copy(entry, item.tag)
		binary.BigEndian.PutUint32(entry[8:12], uint32(len(data)))
		binary.BigEndian.PutUint32(entry[12:16], uint32(len(item.data)))
		data = append(data, item.data...)
	}
	return data
}

func TestStrictInventoryWithIsolatedFixtures(t *testing.T) {
	ctx := context.Background()
	filesystem := manifestFS()
	for _, name := range []string{"one", "two", "three", "four"} {
		filesystem.files["content/fixture/"+name] = &fstest.MapFile{Data: waveFixture(16)}
	}
	filesystem.files["content/fixture/master"] = &fstest.MapFile{Data: waveFixture(24)}
	filesystem.files["content/fixture/source"] = &fstest.MapFile{Data: []byte("isolated source fixture")}
	filesystem.files["content/fixture/evidence"] = &fstest.MapFile{Data: []byte("isolated evidence fixture")}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	filesystem.files["content/fixture/image"] = &fstest.MapFile{Data: picture.Bytes()}
	filesystem.files["content/fixture/ogg"] = &fstest.MapFile{Data: oggFixture()}
	for _, seconds := range []int{48, 64, 96, 120} {
		filesystem.files[fmt.Sprintf("content/fixture/%d.ogg", seconds)] = &fstest.MapFile{Data: oggFixtureWithGranule(uint64(seconds * 48000))}
	}
	filesystem.files["content/fixture/font"] = &fstest.MapFile{Data: fontFixture()}
	filesystem.files["licenses/fixture.txt"] = &fstest.MapFile{Data: []byte("fixture notice, not production rights evidence")}
	manifest := generated(t, filesystem)
	rights := provenanceSource{SchemaVersion: 1, ReviewPolicy: "isolated fixture; not production evidence"}
	for _, component := range manifest.Components {
		rights.Components = append(rights.Components, componentEntry{ID: component.ID, Kind: component.Kind, Name: component.Name, Authorship: "fixture", SourceMaterial: "fixture", License: "fixture", Permission: "fixture", PerformerConsent: "not applicable: no performer", Attribution: "fixture", Evidence: []string{"content/fixture/evidence"}, Notices: []string{"licenses/fixture.txt"}, Review: "fixture review", ReviewStatus: "reviewed"})
	}
	for _, record := range manifest.Records {
		entry := provenanceEntry{AssetID: record.ID, Final: true, Authorship: "test author", SourceMaterial: "test", License: "test", Permission: "test", PerformerConsent: "test consent", Attribution: "test", Review: "test review", ReviewStatus: "reviewed", Evidence: []string{"content/fixture/evidence"}}
		if record.Kind == "font" {
			entry.Notices = []string{"licenses/fixture.txt"}
		}
		rights.Sources = append(rights.Sources, entry)
		if record.Kind == "text" {
			continue
		}
		delivery := deliveryEntry{AssetID: record.ID, SourcePath: "content/fixture/source", ExportPath: "content/fixture/one", AdditionalExports: []string{}}
		switch record.Kind {
		case "visual":
			delivery.ExportPath = "content/fixture/image"
			delivery.VisualHandoff = []VisualHandoff{{LogicalWidth: 1, LogicalHeight: 1, Pivot: "fixture pivot", Sockets: []HandoffSocket{}}}
			if record.Visual.Animation != nil {
				for _, socket := range record.Visual.Animation.Sockets {
					delivery.VisualHandoff[0].Sockets = append(delivery.VisualHandoff[0].Sockets, HandoffSocket{Socket: socket, Resolved: true})
				}
			}
		case "font":
			delivery.ExportPath = "content/fixture/font"
		case "audio-score":
			delivery.ExportPath = "content/fixture/source"
		case "music":
			delivery.ExportPath = fmt.Sprintf("content/fixture/%d.ogg", record.Audio.DurationSeconds)
		case "ambience":
			delivery.ExportPath = "content/fixture/ogg"
		}
		if record.Audio != nil {
			delivery.SourcePath = "content/fixture/master"
			for j := 1; j < max(record.Audio.Variations, record.Audio.Takes); j++ {
				delivery.AdditionalExports = append(delivery.AdditionalExports, "content/fixture/"+[]string{"one", "two", "three", "four"}[j])
			}
			if record.Kind == "ambience" {
				delivery.LoopEndSample = 64
			} else if record.Kind == "music" && record.ID != "MUS-PRO" && record.ID != "MUS-CEREMONY" {
				delivery.LoopEndSample = int64(record.Audio.DurationSeconds) * 48000
			}
			for _, marker := range record.Audio.Markers {
				marker.Resolved = true
				marker.Sample = 1
				delivery.Markers = append(delivery.Markers, marker)
			}
		}
		rights.Deliveries = append(rights.Deliveries, delivery)
	}
	encoded, err := json.Marshal(rights)
	if err != nil {
		t.Fatal(err)
	}
	filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
	manifest = generated(t, filesystem)
	if err := ValidateManifest(ctx, filesystem, manifest, true); err != nil {
		t.Fatalf("isolated complete fixture: %v", err)
	}
	for i := range rights.Components {
		if rights.Components[i].ID != "COMP-DECODERS" {
			continue
		}
		original := rights.Components[i].Notices
		for _, paths := range [][]string{{}, {"licenses/missing.txt"}} {
			rights.Components[i].Notices = paths
			encoded, err = json.Marshal(rights)
			if err != nil {
				t.Fatal(err)
			}
			filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
			incomplete := generated(t, filesystem)
			if err := ValidateManifest(ctx, filesystem, incomplete, false); err != nil {
				t.Fatal(err)
			}
			if err := ValidateManifest(ctx, filesystem, incomplete, true); err == nil || !strings.Contains(err.Error(), "COMP-DECODERS") || !strings.Contains(err.Error(), "notice") {
				t.Fatalf("missing component notice was not rejected: %v", err)
			}
		}
		rights.Components[i].Notices = original
		break
	}
	for i := range rights.Sources {
		if rights.Sources[i].AssetID != "FONT-NOTO-BOLD" {
			continue
		}
		original := rights.Sources[i].Notices
		rights.Sources[i].Notices = nil
		encoded, err = json.Marshal(rights)
		if err != nil {
			t.Fatal(err)
		}
		filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
		incomplete := generated(t, filesystem)
		if err := ValidateManifest(ctx, filesystem, incomplete, false); err != nil {
			t.Fatal(err)
		}
		if err := ValidateManifest(ctx, filesystem, incomplete, true); err == nil || !strings.Contains(err.Error(), "FONT-NOTO-BOLD: missing required font license notice") {
			t.Fatalf("missing font notice was not rejected: %v", err)
		}
		rights.Sources[i].Notices = original
		break
	}
	for _, id := range []string{"MUS-SCORE-ADMIN", "MUS-MIX-HOS", "MUS-HOS-EXP-TAIL"} {
		original := rights.Deliveries
		filtered := make([]deliveryEntry, 0, len(original)-1)
		for _, delivery := range original {
			if delivery.AssetID != id {
				filtered = append(filtered, delivery)
			}
		}
		if len(filtered) != len(original)-1 {
			t.Fatalf("missing handoff fixture %s", id)
		}
		rights.Deliveries = filtered
		encoded, err = json.Marshal(rights)
		if err != nil {
			t.Fatal(err)
		}
		filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
		incomplete := generated(t, filesystem)
		if err := ValidateManifest(ctx, filesystem, incomplete, false); err != nil {
			t.Fatal(err)
		}
		if err := ValidateManifest(ctx, filesystem, incomplete, true); err == nil || !strings.Contains(err.Error(), id) {
			t.Fatalf("missing audio handoff %s was not rejected: %v", id, err)
		}
		rights.Deliveries = original
	}
	for i := range manifest.Records {
		if manifest.Records[i].ID == "SFX-MOVE-TILE" {
			original := manifest.Records[i].Provenance
			forged := *original
			forged.Authorship = "forged author"
			manifest.Records[i].Provenance = &forged
			if err := ValidateManifest(ctx, filesystem, manifest, true); err == nil {
				t.Fatal("unregistered rights evidence accepted")
			}
			manifest.Records[i].Provenance = original
			break
		}
	}
	for i := range rights.Deliveries {
		if rights.Deliveries[i].AssetID != "MUS-FUNERAL" {
			continue
		}
		original := rights.Deliveries[i]
		rights.Deliveries[i].Markers = append([]TransitionMarker(nil), original.Markers...)
		for j := range rights.Deliveries[i].Markers {
			if rights.Deliveries[i].Markers[j].Name == "resurrection_stop" {
				rights.Deliveries[i].Markers[j].Resolved = false
				rights.Deliveries[i].Markers[j].Sample = 0
			}
		}
		encoded, err = json.Marshal(rights)
		if err != nil {
			t.Fatal(err)
		}
		filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
		incomplete := generated(t, filesystem)
		if err := ValidateManifest(ctx, filesystem, incomplete, false); err != nil {
			t.Fatal(err)
		}
		if err := ValidateManifest(ctx, filesystem, incomplete, true); err == nil || !strings.Contains(err.Error(), "MUS-FUNERAL: unresolved transition marker resurrection_stop") {
			t.Fatalf("missing resurrection stop not rejected: %v", err)
		}
		rights.Deliveries[i] = original
		rights.Deliveries[i].LoopStartSample = 1
		encoded, err = json.Marshal(rights)
		if err != nil {
			t.Fatal(err)
		}
		filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
		shortLoop := generated(t, filesystem)
		if err := ValidateManifest(ctx, filesystem, shortLoop, true); err == nil || !strings.Contains(err.Error(), "MUS-FUNERAL: loop duration differs") {
			t.Fatalf("short loop accepted: %v", err)
		}
		rights.Deliveries[i] = original
		rights.Deliveries[i].LoopEndSample = 48 * 48000
		encoded, err = json.Marshal(rights)
		if err != nil {
			t.Fatal(err)
		}
		filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
		atEnd := generated(t, filesystem)
		if err := ValidateManifest(ctx, filesystem, atEnd, true); err != nil {
			t.Fatalf("loop ending at the sample boundary rejected: %v", err)
		}
		rights.Deliveries[i].LoopEndSample = 48*48000 + 1
		encoded, err = json.Marshal(rights)
		if err != nil {
			t.Fatal(err)
		}
		filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
		if _, err := GenerateManifest(ctx, filesystem); err == nil || !strings.Contains(err.Error(), "MUS-FUNERAL: invalid delivered loop boundaries") {
			t.Fatalf("out-of-bounds loop accepted: %v", err)
		}
		rights.Deliveries[i] = original
		break
	}
	for i := range rights.Deliveries {
		if rights.Deliveries[i].AssetID != "MUS-PRO" {
			continue
		}
		originalPath := rights.Deliveries[i].ExportPath
		for _, delta := range []int64{-1, 1} {
			path := fmt.Sprintf("content/fixture/duration-%d.ogg", delta)
			filesystem.files[path] = &fstest.MapFile{Data: oggFixtureWithGranule(uint64(48*48000 + delta))}
			rights.Deliveries[i].ExportPath = path
			encoded, err = json.Marshal(rights)
			if err != nil {
				t.Fatal(err)
			}
			filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
			incorrect := generated(t, filesystem)
			if err := ValidateManifest(ctx, filesystem, incorrect, false); err != nil {
				t.Fatal(err)
			}
			if err := ValidateManifest(ctx, filesystem, incorrect, true); err == nil || !strings.Contains(err.Error(), "MUS-PRO: delivered cue duration differs") {
				t.Fatalf("cue duration delta %d accepted: %v", delta, err)
			}
		}
		rights.Deliveries[i].ExportPath = originalPath
		original := append([]TransitionMarker(nil), rights.Deliveries[i].Markers...)
		rights.Deliveries[i].Markers = rights.Deliveries[i].Markers[:1]
		encoded, err = json.Marshal(rights)
		if err != nil {
			t.Fatal(err)
		}
		filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
		incomplete := generated(t, filesystem)
		pro := manifestEntry(t, incomplete, "MUS-PRO")
		if len(pro.Audio.Markers) != 2 || pro.Audio.Markers[1].Name != "heart_attack_break" || pro.Audio.Markers[1].Resolved {
			t.Fatal("missing break did not remain explicitly unresolved")
		}
		if err := ValidateManifest(ctx, filesystem, incomplete, false); err != nil {
			t.Fatal(err)
		}
		if err := ValidateManifest(ctx, filesystem, incomplete, true); err == nil || !strings.Contains(err.Error(), "MUS-PRO: unresolved transition marker heart_attack_break") {
			t.Fatalf("missing break not rejected: %v", err)
		}
		rights.Deliveries[i].Markers = append([]TransitionMarker(nil), original...)
		for j := range rights.Deliveries[i].Markers {
			if rights.Deliveries[i].Markers[j].Name == "heart_attack_break" {
				rights.Deliveries[i].Markers[j].Sample = 48 * 48000
			}
		}
		encoded, err = json.Marshal(rights)
		if err != nil {
			t.Fatal(err)
		}
		filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
		if _, err := GenerateManifest(ctx, filesystem); err == nil || !strings.Contains(err.Error(), "invalid transition marker") {
			t.Fatalf("out-of-bounds marker accepted: %v", err)
		}
		rights.Deliveries[i].Markers = original
		break
	}
	for i := range rights.Deliveries {
		if rights.Deliveries[i].AssetID == "SFX-MOVE-TILE" {
			rights.Deliveries[i].AdditionalExports = rights.Deliveries[i].AdditionalExports[:2]
			break
		}
	}
	encoded, err = json.Marshal(rights)
	if err != nil {
		t.Fatal(err)
	}
	filesystem.files["content/catalogue/provenance.json"] = &fstest.MapFile{Data: encoded}
	manifest = generated(t, filesystem)
	if err := ValidateManifest(ctx, filesystem, manifest, false); err != nil {
		t.Fatalf("ordinary validation rejected incomplete delivery: %v", err)
	}
	if err := ValidateManifest(ctx, filesystem, manifest, true); err == nil || !strings.Contains(err.Error(), "SFX-MOVE-TILE: missing required variations or takes") {
		t.Fatalf("missing fourth variation was not specifically rejected: %v", err)
	}
}

func TestDeliveredTechnicalBoundaries(t *testing.T) {
	ctx := context.Background()
	filesystem := manifestFS()
	ogg := oggFixture()
	filesystem.files["content/fixture/complete.ogg"] = &fstest.MapFile{Data: ogg}
	if samples, err := vorbisSamples(ctx, filesystem, "content/fixture/complete.ogg"); err != nil || samples != 96 {
		t.Fatalf("complete Ogg sample count = %d: %v", samples, err)
	}
	filesystem.files["content/fixture/forged.ogg"] = &fstest.MapFile{Data: oggFixtureWithGranule(97)}
	if _, err := vorbisSamples(ctx, filesystem, "content/fixture/forged.ogg"); err == nil {
		t.Fatal("accepted a granule exceeding the packet capacity")
	}
	if err := checkVorbisPacket([]byte("\x05vorbis\x01"), 2); err == nil {
		t.Fatal("accepted an incomplete setup packet with a fake framing bit")
	}
	secondPage := 27 + int(ogg[26]) + int(ogg[27])
	setupStart := secondPage + 27 + int(ogg[secondPage+26]) + int(ogg[secondPage+27])
	setup := ogg[setupStart : setupStart+int(ogg[secondPage+28])]
	if err := checkVorbisPacket(setup, 2); err != nil {
		t.Fatalf("complete setup rejected: %v", err)
	}
	for length := 7; length < len(setup); length++ {
		if err := checkVorbisPacket(setup[:length], 2); err == nil {
			t.Errorf("accepted setup truncated to %d bytes", length)
		}
	}
	for _, length := range []int{27, 58, len(ogg) - 1} {
		filesystem.files["content/fixture/truncated.ogg"] = &fstest.MapFile{Data: ogg[:length]}
		if _, err := vorbisSamples(ctx, filesystem, "content/fixture/truncated.ogg"); err == nil {
			t.Errorf("accepted Ogg truncated to %d bytes", length)
		}
	}
	corrupt := append([]byte(nil), ogg...)
	corrupt[len(corrupt)-1] ^= 1
	filesystem.files["content/fixture/corrupt.ogg"] = &fstest.MapFile{Data: corrupt}
	if _, err := vorbisSamples(ctx, filesystem, "content/fixture/corrupt.ogg"); err == nil {
		t.Fatal("accepted Ogg with bad checksum")
	}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	imageBytes := picture.Bytes()
	filesystem.files["content/fixture/complete.png"] = &fstest.MapFile{Data: imageBytes}
	if err := validateMediaHeader(ctx, filesystem, "content/fixture/complete.png", "visual", "ART-TEST", false); err != nil {
		t.Fatal(err)
	}
	filesystem.files["content/fixture/truncated.png"] = &fstest.MapFile{Data: imageBytes[:len(imageBytes)-12]}
	if err := validateMediaHeader(ctx, filesystem, "content/fixture/truncated.png", "visual", "ART-TEST", false); err == nil {
		t.Fatal("accepted truncated PNG")
	}
	font := fontFixture()
	if err := validateFont(font); err != nil {
		t.Fatal(err)
	}
	for _, length := range []int{12, 64, len(font) - 1} {
		if err := validateFont(font[:length]); err == nil {
			t.Errorf("accepted font truncated to %d bytes", length)
		}
	}
	wav := waveFixture(16)
	filesystem.files["content/fixture/short.wav"] = &fstest.MapFile{Data: wav}
	if err := validateMediaHeader(ctx, filesystem, "content/fixture/short.wav", "stinger", "STG-CREDIT", false); err != nil {
		t.Fatal(err)
	}
	bad := append([]byte(nil), wav...)
	binary.LittleEndian.PutUint32(bad[28:32], 1)
	filesystem.files["content/fixture/bad.wav"] = &fstest.MapFile{Data: bad}
	if err := validateMediaHeader(ctx, filesystem, "content/fixture/bad.wav", "effect", "SFX-BODY-HURT", false); err == nil {
		t.Fatal("bad PCM rate accepted")
	}
	long := make([]byte, 44+96000)
	copy(long, wav)
	binary.LittleEndian.PutUint32(long[4:8], uint32(len(long)-8))
	binary.LittleEndian.PutUint32(long[40:44], 96000)
	filesystem.files["content/fixture/long.wav"] = &fstest.MapFile{Data: long}
	if err := validateMediaHeader(ctx, filesystem, "content/fixture/long.wav", "stinger", "STG-CREDIT", false); err == nil {
		t.Fatal("one-second receipt stinger accepted")
	}
	manifest := generated(t, filesystem)
	visual := manifestEntry(t, manifest, "ART-END-TABLEAU")
	if err := validateVisualHandoff(visual, false); err != nil {
		t.Fatal(err)
	}
	if err := validateVisualHandoff(visual, true); err == nil {
		t.Fatal("missing geometry accepted")
	}
	visual.VisualHandoff = &VisualHandoff{LogicalWidth: 1, LogicalHeight: 1, Pivot: "fixture", Sockets: []HandoffSocket{}}
	if err := validateVisualHandoff(visual, true); err != nil {
		t.Fatal(err)
	}
	visual.VisualHandoff.LogicalWidth = 0
	if err := validateVisualHandoff(visual, false); err == nil {
		t.Fatal("zero dimensions accepted")
	}
}

func TestComponentProvenanceAndNotices(t *testing.T) {
	ctx := context.Background()
	filesystem := manifestFS()
	tool := componentEntry{ID: "COMP-MIX-TOOL", Kind: "tool", Name: "fixture tool", Authorship: "fixture", SourceMaterial: "fixture", License: "fixture terms", Permission: "fixture", PerformerConsent: "not applicable: no performer", Attribution: "fixture", Evidence: []string{"content/fixture/evidence"}, Notices: []string{"licenses/fixture.txt", "THIRD-PARTY-NOTICES.txt"}, Review: "fixture review", ReviewStatus: "reviewed"}
	filesystem.files["content/fixture/evidence"] = &fstest.MapFile{Data: []byte("fixture")}
	filesystem.files["THIRD-PARTY-NOTICES.txt"] = &fstest.MapFile{Data: []byte("fixture attribution")}
	manifest := &Manifest{}
	if err := applyComponents(ctx, filesystem, manifest, []componentEntry{tool}); err != nil {
		t.Fatal(err)
	}
	var record *ComponentProvenance
	for i := range manifest.Components {
		if manifest.Components[i].ID == tool.ID {
			record = &manifest.Components[i]
		}
	}
	if record == nil || record.Provenance.NoticeFiles[0] != nil {
		t.Fatal("unknown notice must remain unresolved")
	}
	if err := validateProvenance(ctx, filesystem, record.Provenance, false); err != nil {
		t.Fatal(err)
	}
	if err := validateProvenance(ctx, filesystem, record.Provenance, true); err == nil {
		t.Fatal("missing notice accepted")
	}
	filesystem.files["licenses/fixture.txt"] = &fstest.MapFile{Data: []byte("fixture notice")}
	manifest = &Manifest{}
	if err := applyComponents(ctx, filesystem, manifest, []componentEntry{tool}); err != nil {
		t.Fatal(err)
	}
	for _, component := range manifest.Components {
		if component.ID == tool.ID {
			if err := validateProvenance(ctx, filesystem, component.Provenance, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := applyComponents(ctx, filesystem, &Manifest{}, []componentEntry{tool, tool}); err == nil {
		t.Fatal("duplicate component accepted")
	}
	tool.Notices = []string{"licenses/../unsafe"}
	if err := applyComponents(ctx, filesystem, &Manifest{}, []componentEntry{tool}); err == nil {
		t.Fatal("unsafe notice accepted")
	}
}

func TestProvenanceEvidenceAndLoops(t *testing.T) {
	ctx := context.Background()
	filesystem := manifestFS()
	p := &Provenance{Authorship: "author", SourceMaterial: "original", License: "terms", Permission: "permission", PerformerConsent: "not applicable: no performer", Attribution: "credit", Review: "reviewed", ReviewStatus: "reviewed", Evidence: []string{"content/evidence/missing"}}
	if err := validateProvenance(ctx, filesystem, p, false); err != nil {
		t.Fatal(err)
	}
	if err := validateProvenance(ctx, filesystem, p, true); err == nil {
		t.Fatal("missing evidence accepted")
	}
	filesystem.files["content/evidence/missing"] = &fstest.MapFile{Data: []byte("test")}
	if err := validateProvenance(ctx, filesystem, p, true); err != nil {
		t.Fatal(err)
	}
	p.PerformerConsent = "unknown"
	if err := validateProvenance(ctx, filesystem, p, true); err == nil {
		t.Fatal("unknown consent accepted")
	}
	p.PerformerConsent = "not applicable: no performer"
	p.ReviewStatus = "concerns"
	if err := validateProvenance(ctx, filesystem, p, true); err == nil {
		t.Fatal("unresolved review concerns accepted")
	}
	p.ReviewStatus = "unsupported"
	if err := validateProvenance(ctx, filesystem, p, false); err == nil {
		t.Fatal("malformed review status accepted")
	}
	manifest := generated(t, filesystem)
	for i := range manifest.Records {
		if manifest.Records[i].ID == "MUS-TITLE" {
			negative := int64(-1)
			end := int64(10)
			manifest.Records[i].Audio.LoopStartSample = &negative
			manifest.Records[i].Audio.LoopEndSample = &end
			break
		}
	}
	if err := ValidateManifest(ctx, filesystem, manifest, false); err == nil {
		t.Fatal("negative loop accepted")
	}
}

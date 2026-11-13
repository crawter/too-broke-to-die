// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// FileMetadata is calculated from a delivered file, never from its name.
type FileMetadata struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// TransitionMarker locates a named cue event in decoded runtime samples.
// An unresolved marker has Resolved false and Sample zero.
type TransitionMarker struct {
	Name     string `json:"name"`
	Sample   int64  `json:"sample"`
	Resolved bool   `json:"resolved"`
}

// AudioRequirement specifies a production target rather than a measured mix.
// Nil loop points are unresolved sample boundaries, not zero-length loops.
type AudioRequirement struct {
	EventOwner      string             `json:"event_owner"`
	Caption         string             `json:"caption"`
	Direction       string             `json:"direction"`
	Variations      int                `json:"variations"`
	Takes           int                `json:"takes"`
	DurationSeconds int                `json:"duration_seconds"`
	SourceFormat    string             `json:"source_format"`
	RuntimeFormat   string             `json:"runtime_format"`
	Channels        string             `json:"channels"`
	SampleRate      int                `json:"sample_rate"`
	TargetLoudness  string             `json:"target_loudness"`
	LoopStartSample *int64             `json:"loop_start_sample"`
	LoopEndSample   *int64             `json:"loop_end_sample"`
	Markers         []TransitionMarker `json:"markers"`
	Transition      string             `json:"transition"`
	Tail            string             `json:"tail"`
}

// FontRequirement describes the independently shipped font weight.
type FontRequirement struct {
	Family            string `json:"family"`
	Weight            string `json:"weight"`
	Glyphs            string `json:"glyphs"`
	SizesPX           []int  `json:"sizes_px"`
	IntendedLicense   string `json:"intended_license"`
	LicenseObligation string `json:"license_obligation"`
}

// HandoffSocket identifies a resolved visual attachment point.
type HandoffSocket struct {
	Socket   string  `json:"socket"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Resolved bool    `json:"resolved"`
}

// VisualHandoff records measured logical geometry supplied by the producer.
// Sockets are empty for non-modular artwork; they never affect collision.
type VisualHandoff struct {
	LogicalWidth  int             `json:"logical_width"`
	LogicalHeight int             `json:"logical_height"`
	Pivot         string          `json:"pivot"`
	Sockets       []HandoffSocket `json:"sockets"`
}

// ManifestRecord is an independent presentation identity with explicit delivery state.
type ManifestRecord struct {
	ID                string            `json:"id"`
	Kind              string            `json:"kind"`
	Bindings          []string          `json:"bindings"`
	LoadGroup         string            `json:"load_group"`
	Format            string            `json:"format"`
	SourcePath        string            `json:"source_path"`
	SourceFile        *FileMetadata     `json:"source_file"`
	ExportPath        string            `json:"export_path"`
	File              *FileMetadata     `json:"file"`
	AdditionalExports []string          `json:"additional_exports"`
	AdditionalFiles   []*FileMetadata   `json:"additional_files"`
	Final             bool              `json:"final"`
	Provenance        *Provenance       `json:"provenance"`
	Visual            *VisualRecord     `json:"visual,omitempty"`
	VisualHandoff     *VisualHandoff    `json:"visual_handoff,omitempty"`
	Audio             *AudioRequirement `json:"audio,omitempty"`
	Font              *FontRequirement  `json:"font,omitempty"`
}

// MixPolicy specifies accessible mixing targets, not a measured final mix.
type MixPolicy struct {
	Controls          []string `json:"controls"`
	DefaultsPercent   []int    `json:"defaults_percent"`
	Priority          []string `json:"priority"`
	WarningMusicDB    int      `json:"warning_music_db"`
	WarningAmbienceDB int      `json:"warning_ambience_db"`
	DuckAttackMS      int      `json:"duck_attack_ms"`
	DuckReleaseMS     int      `json:"duck_release_ms"`
	DialogueMusicDB   int      `json:"dialogue_music_db"`
	ReferenceLUFS     int      `json:"reference_lufs"`
	PeakCeilingDBTP   int      `json:"peak_ceiling_dbtp"`
	Policy            string   `json:"policy"`
}

// CampaignIdentity maps an existing domain ID to its owning sources and references.
// It does not create an additional gameplay identity or reward.
type CampaignIdentity struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Bindings    []string        `json:"bindings"`
	SourceFiles []*FileMetadata `json:"source_files"`
}

// Manifest is a deterministic inventory, not evidence of final acceptance.
type Manifest struct {
	SchemaVersion  int                   `json:"schema_version"`
	ContentVersion string                `json:"content_version"`
	Mix            MixPolicy             `json:"mix"`
	MusicPolicy    string                `json:"music_policy"`
	Campaign       []CampaignIdentity    `json:"campaign"`
	Components     []ComponentProvenance `json:"components"`
	Records        []ManifestRecord      `json:"records"`
}

type cueSource struct {
	ID        string `json:"id"`
	Seconds   int    `json:"seconds"`
	Binding   string `json:"binding"`
	Direction string `json:"direction"`
}

type ambienceSource struct {
	ID        string `json:"id"`
	Binding   string `json:"binding"`
	Direction string `json:"direction"`
}

type effectSource struct {
	Family string   `json:"family"`
	Events []string `json:"events"`
}

type audioSource struct {
	SchemaVersion   int              `json:"schema_version"`
	MusicPolicy     string           `json:"music_policy"`
	Mix             MixPolicy        `json:"mix"`
	Music           []cueSource      `json:"music"`
	Stingers        []string         `json:"stingers"`
	Ambience        []ambienceSource `json:"ambience"`
	Effects         []effectSource   `json:"effects"`
	Characters      []string         `json:"characters"`
	VocalCategories []string         `json:"vocal_categories"`
	Trends          []string         `json:"trends"`
}

type fontSource struct {
	SchemaVersion     int      `json:"schema_version"`
	Family            string   `json:"family"`
	Weights           []string `json:"weights"`
	Glyphs            string   `json:"glyphs"`
	SizesPX           []int    `json:"sizes_px"`
	IntendedLicense   string   `json:"intended_license"`
	LicenseObligation string   `json:"license_obligation"`
}

// GenerateManifest loads the same validated visual and campaign inventories used
// by the game, then expands the audio, font, text and provenance registers.
// filesystem must confine symlinks to its root (os.Root.FS does so).
func GenerateManifest(ctx context.Context, filesystem fs.FS) (_ *Manifest, err error) {
	defer mon.Task()(&ctx)(&err)

	campaign, err := LoadCampaign(ctx, filesystem, "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		return nil, err
	}
	text, err := LoadRussian(ctx, filesystem, campaign)
	if err != nil {
		return nil, err
	}
	visual, err := LoadVisualAssets(ctx, filesystem, campaign, text)
	if err != nil {
		return nil, err
	}
	var audio audioSource
	var fonts fontSource
	var rights provenanceSource
	for _, document := range []struct {
		path string
		dst  any
	}{
		{"content/catalogue/audio.json", &audio},
		{"content/catalogue/fonts.json", &fonts},
		{"content/catalogue/provenance.json", &rights},
	} {
		if err = readCampaignDocument(ctx, filesystem, document.path, document.dst); err != nil {
			return nil, err
		}
	}
	var campaignDocument campaignSource
	if err = readCampaignDocument(ctx, filesystem, "content/data/campaign.json", &campaignDocument); err != nil {
		return nil, err
	}
	manifest := &Manifest{SchemaVersion: 1, ContentVersion: campaignDocument.ContentVersion}
	manifest.Campaign, err = expandCampaignIdentities(ctx, filesystem, campaign)
	if err != nil {
		return nil, err
	}
	for _, item := range visual.Records(ctx) {
		itemCopy := item
		manifest.Records = append(manifest.Records, ManifestRecord{ID: item.ID, Kind: "visual", Bindings: item.Bindings, LoadGroup: loadGroup(item.Bindings), Format: item.Technical.ExportFormat, Visual: &itemCopy})
	}
	if err = expandAudio(ctx, manifest, audio, text); err != nil {
		return nil, err
	}
	if err = expandFonts(manifest, fonts); err != nil {
		return nil, err
	}
	// Text entries retain their authored document path; derived ticket labels
	// come from the service document rather than an invented export.
	textPaths := make(map[string]string)
	for _, source := range textFiles {
		var document textDocument
		if err = readCampaignDocument(ctx, filesystem, source.path, &document); err != nil {
			return nil, err
		}
		for _, entry := range document.Entries {
			textPaths[entry.ID] = source.path
		}
	}
	for _, entry := range text.ordered {
		path := textPaths[entry.ID]
		if path == "" {
			path = "content/data/ru-services.json"
		}
		bindings := []string{}
		if entry.Reference != "" {
			bindings = append(bindings, entry.Reference)
		}
		manifest.Records = append(manifest.Records, ManifestRecord{ID: entry.ID, Kind: "text", Bindings: bindings, LoadGroup: loadGroup(bindings), Format: "UTF-8 JSON", SourcePath: path, ExportPath: path})
	}
	if err = applyProvenance(ctx, filesystem, manifest, rights); err != nil {
		return nil, err
	}
	sort.Slice(manifest.Records, func(i, j int) bool { return manifest.Records[i].ID < manifest.Records[j].ID })
	if err = validateManifest(ctx, filesystem, manifest, false, nil); err != nil {
		return nil, err
	}
	return manifest, nil
}

func expandCampaignIdentities(ctx context.Context, filesystem fs.FS, campaign *Campaign) (_ []CampaignIdentity, err error) {
	defer mon.Task()(&ctx)(&err)

	const campaignData = "content/data/campaign.json"
	const campaignCode = "internal/tbtd/content/campaign.go"
	const storyCode = "internal/tbtd/campaign/story.go"
	const storyValidation = "internal/tbtd/campaign/validation.go"
	const textData = "content/data/ru-services.json"
	var records []CampaignIdentity
	files := make(map[string]*FileMetadata)
	seen := make(map[string]bool)
	add := func(id, kind string, bindings []string, paths ...string) error {
		if id == "" || seen[id] {
			return ErrInvalid.New("duplicate campaign identity %s", id)
		}
		seen[id] = true
		record := CampaignIdentity{ID: id, Kind: kind, Bindings: bindings}
		for _, name := range paths {
			file, ok := files[name]
			if !ok {
				var readErr error
				file, readErr = fingerprint(ctx, filesystem, name)
				if readErr != nil {
					return readErr
				}
				if file == nil {
					return ErrInvalid.New("missing campaign source %s", name)
				}
				files[name] = file
			}
			record.SourceFiles = append(record.SourceFiles, file)
		}
		records = append(records, record)
		return nil
	}
	for _, item := range campaign.Deeds(ctx) {
		deed := item.Deed
		bindings := []string{string(deed.Encounter), string(deed.Anchor), string(deed.Consequence), string(deed.Title)}
		for _, id := range deed.Prerequisites {
			bindings = addUnique(bindings, string(id))
		}
		for _, id := range deed.Objects {
			bindings = addUnique(bindings, string(id))
		}
		for _, id := range deed.Narratives {
			bindings = addUnique(bindings, string(id))
		}
		for _, id := range deed.Defeated {
			bindings = addUnique(bindings, string(id))
		}
		if err = add(string(deed.ID), "deed", bindings, campaignData, campaignCode, textData); err != nil {
			return nil, err
		}
	}
	for _, item := range campaign.Objects(ctx) {
		bindings := []string{string(item.Encounter)}
		if item.Deed != "" {
			bindings = append(bindings, string(item.Deed))
		}
		if err = add(string(item.ID), "object", bindings, campaignData, campaignCode); err != nil {
			return nil, err
		}
	}
	for _, item := range campaign.Consequences(ctx) {
		if err = add(string(item.ID), "consequence", []string{string(item.Deed)}, campaignData, campaignCode); err != nil {
			return nil, err
		}
	}
	for _, batch := range campaign.Batches(ctx) {
		if err = add(string(batch.Emergency.ID), "emergency", []string{string(batch.Emergency.Encounter), string(batch.Emergency.Anchor)}, campaignData, campaignCode); err != nil {
			return nil, err
		}
	}
	for _, gate := range campaign.Gates(ctx) {
		var bindings []string
		for _, id := range gate.Deeds {
			bindings = addUnique(bindings, string(id))
		}
		for _, id := range gate.Narratives {
			bindings = addUnique(bindings, string(id))
		}
		for _, id := range gate.Defeated {
			bindings = addUnique(bindings, string(id))
		}
		if err = add(string(gate.ID), "gate", bindings, campaignData, campaignCode); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"hospital", "church", "ministry", "institute"} {
		path := "content/data/encounters/" + name + ".json"
		var layout InstitutionGeometry
		if err = readCampaignDocument(ctx, filesystem, path, &layout); err != nil {
			return nil, err
		}
		if err = validateInstitution(ctx, layout, campaign); err != nil {
			return nil, ErrInvalid.Wrap(err)
		}
		if err = add(string(layout.Location), "location", nil, path); err != nil {
			return nil, err
		}
		for _, bay := range layout.Bays {
			if err = add(string(bay.Encounter), "encounter", []string{string(layout.Location), string(bay.Anchor), string(bay.Emergency)}, path, "content/data/encounters/templates.json", "content/data/encounters/variants.json"); err != nil {
				return nil, err
			}
			if err = add(string(bay.Anchor), "anchor", []string{string(bay.Encounter)}, path); err != nil {
				return nil, err
			}
		}
		boss := layout.Boss
		if err = add(string(boss.Encounter), "encounter", []string{string(layout.Location), string(boss.Anchor), string(boss.OutAnchor), string(boss.Closure)}, path, "content/data/bosses/"+name+".json"); err != nil {
			return nil, err
		}
		for _, id := range []AnchorID{boss.Anchor, boss.OutAnchor} {
			if err = add(string(id), "anchor", []string{string(boss.Encounter)}, path); err != nil {
				return nil, err
			}
		}
		for _, prologue := range layout.Prologue {
			if err = add(string(prologue.Encounter), "encounter", []string{string(prologue.Anchor)}, path); err != nil {
				return nil, err
			}
			if err = add(string(prologue.Anchor), "anchor", []string{string(prologue.Encounter)}, path); err != nil {
				return nil, err
			}
		}
		for _, lobby := range layout.ReturnLobbies {
			if err = add(string(lobby.Anchor), "anchor", nil, path); err != nil {
				return nil, err
			}
		}
	}
	for i := 1; i <= 12; i++ {
		if err = add(fmt.Sprintf("NAR-%02d", i), "narrative", nil, storyCode, storyValidation); err != nil {
			return nil, err
		}
	}
	for _, stage := range []string{"REVIEW", "CONSENT", "DEATH", "HELL", "CEREMONY", "TABLEAU"} {
		if err = add("END-"+stage, "ending", nil, storyCode, storyValidation); err != nil {
			return nil, err
		}
		if stage != "REVIEW" && stage != "CONSENT" {
			if err = add("ANC-END-"+stage, "anchor", []string{"END-" + stage}, storyCode, storyValidation); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, nil
}

func loadGroup(bindings []string) string {
	for _, binding := range bindings {
		for _, code := range []string{"HOS", "CHU", "MIN", "INS"} {
			if binding == "LOC-"+code || strings.HasPrefix(binding, "ENC-"+code+"-") {
				return "location-" + strings.ToLower(code)
			}
		}
		if strings.HasPrefix(binding, "END-") {
			return "ending"
		}
	}
	return "shared"
}

func audioBase(owner, caption, direction string, variations, takes int, long bool) *AudioRequirement {
	item := &AudioRequirement{EventOwner: owner, Caption: caption, Direction: direction, Variations: variations, Takes: takes, SourceFormat: "48 kHz 24-bit PCM WAV", RuntimeFormat: "48 kHz 16-bit PCM WAV", Channels: "mono spot where appropriate", SampleRate: 48000, TargetLoudness: "reference mix -16 LUFS; true peak <= -1 dBTP", Transition: "semantic event; no change to gameplay timing", Tail: "clean tail; no unintended clicks"}
	if long {
		item.RuntimeFormat = "48 kHz Ogg Vorbis 160-192 kbps"
		item.Channels = "stereo"
		item.Transition = "clean start, documented sample loop boundaries and transition markers; combat crossfade 1000 ms"
		item.Tail = "short clean ending tail"
	}
	return item
}

func addAudio(manifest *Manifest, id, kind string, bindings []string, audio *AudioRequirement) {
	manifest.Records = append(manifest.Records, ManifestRecord{ID: id, Kind: kind, Bindings: bindings, LoadGroup: loadGroup(bindings), Format: audio.RuntimeFormat, Audio: audio})
}

func expandAudio(ctx context.Context, manifest *Manifest, source audioSource, text *RussianText) (err error) {
	defer mon.Task()(&ctx)(&err)
	if source.SchemaVersion != 1 || len(source.Music) != 18 || len(source.Stingers) != 8 || len(source.Ambience) != 6 || len(source.Effects) != 9 || len(source.Characters) != 8 || len(source.VocalCategories) != 4 || len(source.Trends) != 3 {
		return ErrInvalid.New("incomplete audio register")
	}
	mix := source.Mix
	if strings.Join(mix.Controls, " ") != "Master Music Effects Ambience Vocal" || len(mix.DefaultsPercent) != 5 || mix.DefaultsPercent[0] != 80 || mix.DefaultsPercent[1] != 65 || mix.DefaultsPercent[2] != 90 || mix.DefaultsPercent[3] != 55 || mix.DefaultsPercent[4] != 80 || strings.Join(mix.Priority, "|") != "danger warnings|essential interface results|vocal reactions|impacts|music|ambience" || mix.WarningMusicDB != -6 || mix.WarningAmbienceDB != -8 || mix.DuckAttackMS != 50 || mix.DuckReleaseMS != 300 || mix.DialogueMusicDB != -6 || mix.ReferenceLUFS != -16 || mix.PeakCeilingDBTP != -1 || !allText(mix.Policy) {
		return ErrInvalid.New("invalid audio mixing policy")
	}
	if !allText(source.MusicPolicy) {
		return ErrInvalid.New("missing original music policy")
	}
	manifest.Mix = mix
	manifest.MusicPolicy = source.MusicPolicy
	musicIDs := strings.Fields("MUS-HOS-EXP MUS-HOS-CBT MUS-HOS-BOS MUS-CHU-EXP MUS-CHU-CBT MUS-CHU-BOS MUS-MIN-EXP MUS-MIN-CBT MUS-MIN-BOS MUS-INS-EXP MUS-INS-CBT MUS-INS-BOS MUS-TITLE MUS-PRO MUS-FUNERAL MUS-HELL MUS-CEREMONY MUS-CREDITS")
	lengths := []int{96, 64, 96, 96, 64, 96, 96, 64, 96, 96, 64, 96, 64, 48, 48, 64, 48, 120}
	bindings := strings.Fields("LOC-HOS LOC-HOS ENC-HOS-05 LOC-CHU LOC-CHU ENC-CHU-05 LOC-MIN LOC-MIN ENC-MIN-05 LOC-INS LOC-INS ENC-INS-05 UI-MENU-TITLE NAR-01 NAR-02 END-HELL END-CEREMONY END-TABLEAU")
	for i, cue := range source.Music {
		if cue.ID != musicIDs[i] || cue.Seconds != lengths[i] || cue.Binding != bindings[i] || !allText(cue.Direction) {
			return ErrInvalid.New("invalid music cue %s", cue.ID)
		}
		audio := audioBase("music state for "+cue.Binding+": "+strings.TrimPrefix(cue.ID, "MUS-"), "", cue.Direction, 1, 0, true)
		audio.DurationSeconds = cue.Seconds
		audio.Markers = []TransitionMarker{{Name: "transition"}}
		switch cue.ID {
		case "MUS-PRO":
			audio.Markers = append(audio.Markers, TransitionMarker{Name: "heart_attack_break"})
		case "MUS-FUNERAL":
			audio.Markers = append(audio.Markers, TransitionMarker{Name: "resurrection_stop"})
		case "MUS-CEREMONY":
			audio.Markers = append(audio.Markers, TransitionMarker{Name: "held_chord"})
		}
		tailID := cue.ID + "-TAIL"
		addAudio(manifest, cue.ID, "music", []string{cue.Binding, tailID}, audio)
		addAudio(manifest, tailID, "audio-tail", []string{cue.ID, cue.Binding}, audioBase("ending tail for "+cue.ID, "", "Separate short, clean transition tail for this cue.", 1, 0, false))
	}
	manifest.Records = append(manifest.Records, ManifestRecord{ID: "MUS-SCORE-ADMIN", Kind: "audio-score", Bindings: []string{"STG-CREDIT", "MUS-CEREMONY"}, LoadGroup: "shared", Format: "composer score; format unresolved"})
	for _, code := range []string{"HOS", "CHU", "MIN", "INS"} {
		id := "MUS-MIX-" + code
		addAudio(manifest, id, "audio-mix", []string{"LOC-" + code, "MUS-" + code + "-CBT"}, audioBase("reference encounter mix for LOC-"+code, "", "Representative mix reference for loudness, peaks and warning priority review.", 1, 0, false))
	}
	if strings.Join(source.Stingers, " ") != "CREDIT CONSEQUENCE FAIL CHECKPOINT DEATH VICTORY PERMIT ENDING" {
		return ErrInvalid.New("invalid stinger inventory")
	}
	stingerBindings := [][]string{
		{"TXT-RECEIPT-HEADING"},
		{"TXT-CONSEQUENCE-HEADING"},
		{"TXT-EVD-FAILED"},
		{"TXT-CHECKPOINT"},
		{"UI-REC-DEAD"},
		{"UI-SAV-OK"},
		{"END-REVIEW"},
		{"END-TABLEAU"},
	}
	stingerOwners := []string{"durable receipt", "durable consequence", "failed evidence", "durable checkpoint", "durable death", "durable encounter victory", "durable permit", "ending completion"}
	for i, name := range source.Stingers {
		direction := "Distinct original stinger at its semantic event."
		if name == "CREDIT" {
			direction = "Receipt arrangement of administrative motif; duration strictly less than one second."
		} else if name == "CONSEQUENCE" {
			direction = "Broken mechanical descent, not the receipt melody."
		} else if name == "VICTORY" {
			direction = "Victory only after the durable encounter result succeeds."
		}
		id := "STG-" + name
		addAudio(manifest, id, "stinger", stingerBindings[i], audioBase(stingerOwners[i], "", direction, 1, 0, false))
	}
	if len(source.Ambience) != 6 {
		return ErrInvalid.New("invalid ambience inventory")
	}
	ambienceBindings := strings.Fields("LOC-HOS LOC-CHU LOC-MIN LOC-INS END-HELL END-CEREMONY")
	for i, bed := range source.Ambience {
		if bed.ID != strings.Fields("AMB-HOS AMB-CHU AMB-MIN AMB-INS AMB-HELL AMB-CEREMONY")[i] || bed.Binding != ambienceBindings[i] || !allText(bed.Direction) {
			return ErrInvalid.New("invalid ambience %s", bed.ID)
		}
		addAudio(manifest, bed.ID, "ambience", []string{bed.Binding}, audioBase("ambience state for "+bed.Binding, "", bed.Direction+" No essential instruction without matching text.", 1, 0, true))
	}
	families := strings.Fields("MOVE BODY LIMB FEC SERVICE WARN SOURCE UI END")
	expectedEvents := []string{
		"TILE:CAP-MOVE:4 WOOD:CAP-MOVE:4 CONCRETE:CAP-MOVE:4 METAL:CAP-MOVE:4 JUMP:CAP-JUMP:1 LAND:CAP-LAND:1",
		"SWING:CAP-SWING:3 IMPACT:CAP-HIT:3 HURT:CAP-HURT:3 TEAR:CAP-TEAR:3 REPLACE:CAP-REPLACE:3 REGEN:CAP-REGEN:3 HEADBUTT:CAP-HEADBUTT:3 COLLAPSE:CAP-COLLAPSE:3",
		"ARMFLIGHT:CAP-LIMB-FLIGHT:1 LEGFLIGHT:CAP-LIMB-FLIGHT:1 IMPACT:CAP-LIMB-IMPACT:1 BOUNCE:CAP-LIMB-BOUNCE:1 RETRIEVE:CAP-LIMB-RETRIEVE:1 PLATE:CAP-PLATE:1",
		"RELEASE:CAP-FEC-RELEASE:1 HIT:CAP-FEC-HIT:1 RECEPTOR:CAP-FEC-CAMERA:1 SMEAR:CAP-FEC-SMEAR:1 SLIP:CAP-FEC-SLIP:1 EMPTY:CAP-FEC-EMPTY:1 REFILL:CAP-FEC-REFILL:1 WASTE-PRESSURE:CAP-WARN-HAZARD:1 WASTE-HAZARD:CAP-WARN-HAZARD:1",
		"SEAL:CAP-SERVICE-SEAL:1 TROLLEY:CAP-SERVICE-TROLLEY:1 PLATE:CAP-PLATE:1 LATCH:CAP-SERVICE-LATCH:1 PADDLE:CAP-SERVICE-PADDLE:1 BYPASS:CAP-SERVICE-BYPASS:1 METER:CAP-SERVICE-METER:1 RECORDER:CAP-SERVICE-RECORDER:1 CAMERA:CAP-SERVICE-CAMERA:1",
		"ENEMY:CAP-WARN-ENEMY:1 RANGED:CAP-WARN-RANGED:1 HAZARD:CAP-WARN-HAZARD:1 CRUSHER:CAP-WARN-CRUSHER:1 EMERGENCY:CAP-WARN-EMERGENCY:1",
		"QUEUE:CAP-SOURCE-QUEUE:1 SPAWN:CAP-SOURCE-SPAWN:1 HIT:CAP-SOURCE-HIT:1 STOP:CAP-SOURCE-STOP:1 PROTECT:CAP-RELAY-PROTECT:1 BREAK:CAP-RELAY-BREAK:1",
		"FOCUS:CAP-UI-FOCUS:1 CONFIRM:CAP-UI-CONFIRM:1 CANCEL:CAP-UI-CANCEL:1 INVALID:CAP-UI-INVALID:1 LEDGER:CAP-UI-LEDGER:1 PENDING:CAP-UI-PENDING:1 SAVED:CAP-UI-SAVED:1 ERROR:CAP-UI-ERROR:1",
		"DEATH:CAP-END-DEATH:1 HELL:CAP-END-HELL:1 APPOINTMENT:CAP-END-APPOINTMENT:1 POWER:CAP-END-POWER:1 RIBBON:CAP-END-RIBBON:1 APPLAUSE:CAP-END-APPLAUSE:1 FIRE:CAP-END-FIRE:1",
	}
	for i, family := range source.Effects {
		if family.Family != families[i] || strings.Join(family.Events, " ") != expectedEvents[i] {
			return ErrInvalid.New("invalid effect family %s", family.Family)
		}
		for _, spec := range family.Events {
			parts := strings.Split(spec, ":")
			if len(parts) != 3 {
				return ErrInvalid.New("invalid effect %s", spec)
			}
			variations, parseErr := strconv.Atoi(parts[2])
			if parseErr != nil || variations < 1 || variations > 100 || !validID("SFX-"+family.Family+"-"+parts[0], "SFX") {
				return ErrInvalid.New("invalid effect %s", spec)
			}
			if _, ok := text.Entry(parts[1]); !ok || !strings.HasPrefix(parts[1], "CAP-") {
				return ErrInvalid.New("unknown effect caption %s", parts[1])
			}
			id := "SFX-" + family.Family + "-" + parts[0]
			direction := "One semantic event owner; variations change texture, not meaning or warning duration."
			if family.Family == "BODY" && parts[0] == "TEAR" {
				direction += " Fabric, wet pull and vocal effort; no real injury recording."
			}
			if family.Family == "FEC" {
				direction += " Player wet slap distinct from pipe-pressure ministry waste and sustained hazard."
			}
			owner := map[string]string{"MOVE": "SYS-MOV", "BODY": "SYS-CBT", "LIMB": "SYS-LIM", "FEC": "SYS-FEC", "SERVICE": "SYS-INT", "WARN": "SYS-CBT", "SOURCE": "EN-TRE", "UI": "SYS-STA"}[family.Family]
			var ending string
			if family.Family == "END" {
				switch parts[0] {
				case "DEATH":
					owner, ending = "NAR-10", "END-DEATH"
				case "HELL", "APPOINTMENT":
					owner, ending = "NAR-11", "END-HELL"
				default:
					owner, ending = "NAR-12", "END-CEREMONY"
				}
			}
			bindings := []string{parts[1], owner}
			if family.Family == "SOURCE" {
				bindings = append(bindings, "LOC-INS", "ENC-INS-05")
			}
			if ending != "" {
				bindings = append(bindings, ending)
				if parts[0] == "FIRE" || parts[0] == "APPLAUSE" {
					bindings = append(bindings, "END-TABLEAU")
				}
			}
			addAudio(manifest, id, "effect", bindings, audioBase(owner+":"+parts[0], parts[1], direction, variations, 0, false))
		}
	}
	for _, boss := range BossDefinitions(ctx) {
		for _, attack := range boss.Attacks {
			for _, phase := range []string{"PREP", "ACTIVE", "RECOVER"} {
				id := "SFX-BOSS-" + attack.ID + "-" + phase
				caption := "CAP-BOSS-" + attack.ID + "-" + phase
				if _, ok := text.Entry(caption); !ok {
					return ErrInvalid.New("missing caption %s", caption)
				}
				addAudio(manifest, id, "effect", []string{string(boss.Encounter), caption}, audioBase("boss attack "+attack.ID+" "+phase, caption, "Distinct attack phase; preparation starts at authoritative telegraph.", 1, 0, false))
			}
		}
	}
	if strings.Join(source.Characters, " ") != "GRI VER POT SAV BLA LEV NIN SAT" || strings.Join(source.VocalCategories, " ") != "EFFORT HURT LAUGH REACT" || strings.Join(source.Trends, " ") != "SIXSEVEN LEVEL BRAND" {
		return ErrInvalid.New("invalid vocal inventory")
	}
	for _, character := range source.Characters {
		for _, category := range source.VocalCategories {
			id := "VOC-" + character + "-" + category
			direction := "Nonverbal; use only where the action requires it."
			if character == "SAT" {
				direction += " Restrained reaction."
			} else if character == "NIN" {
				direction += " No comic pleasure at patient suffering."
			}
			addAudio(manifest, id, "vocal", []string{"CHR-" + character}, audioBase("CHR-"+character+":"+category, "", direction, 1, 3, false))
		}
	}
	for _, trend := range source.Trends {
		id := "VOC-TRE-" + trend
		caption := "CAP-TREND-" + trend
		if _, ok := text.Entry(caption); !ok {
			return ErrInvalid.New("missing trend caption %s", caption)
		}
		addAudio(manifest, id, "vocal", []string{"LOC-INS", "EN-TRE", caption}, audioBase("EN-TRE:replacement:"+trend, caption, "Newly recorded ensemble, exact caption words; no sampled meme or voice imitation. First replacement announces its changed label; later replacements alternate established labels. One intelligible foreground chant, global interval at least 4 seconds; indicator remains when muted.", 1, 3, false))
	}
	return nil
}

func expandFonts(manifest *Manifest, source fontSource) error {
	if source.SchemaVersion != 1 || source.Family != "Noto Sans" || strings.Join(source.Weights, " ") != "Regular SemiBold Bold" || len(source.SizesPX) != 3 || source.SizesPX[0] != 24 || source.SizesPX[1] != 26 || source.SizesPX[2] != 32 || !allText(source.Glyphs, source.IntendedLicense, source.LicenseObligation) {
		return ErrInvalid.New("invalid font register")
	}
	for _, required := range []string{"А–Я", "а–я", "Ё", "ё", "Latin", "digits", "punctuation", "«»", "№", "₽", "$", "every shipped"} {
		if !strings.Contains(source.Glyphs, required) {
			return ErrInvalid.New("font register missing glyph coverage %s", required)
		}
	}
	if !strings.Contains(source.IntendedLicense, "SIL Open Font License 1.1") || !strings.Contains(source.LicenseObligation, "notices") {
		return ErrInvalid.New("font register missing license obligations")
	}
	for _, weight := range source.Weights {
		font := &FontRequirement{Family: source.Family, Weight: weight, Glyphs: source.Glyphs, SizesPX: append([]int(nil), source.SizesPX...), IntendedLicense: source.IntendedLicense, LicenseObligation: source.LicenseObligation}
		manifest.Records = append(manifest.Records, ManifestRecord{ID: "FONT-NOTO-" + strings.ToUpper(weight), Kind: "font", Bindings: []string{}, LoadGroup: "shared", Format: "font file (format unresolved)", Font: font})
	}
	return nil
}

// ManifestBytes encodes a stable, human-readable manifest.
func ManifestBytes(ctx context.Context, manifest *Manifest) (_ []byte, err error) {
	defer mon.Task()(&ctx)(&err)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, Error.Wrap(err)
	}
	return append(data, '\n'), nil
}

func safeContentPath(path string) bool {
	return fs.ValidPath(path) && path != "." && !strings.ContainsAny(path, "\\:\x00") && (strings.HasPrefix(path, "content/") || strings.HasPrefix(path, "licenses/"))
}

func fingerprint(ctx context.Context, filesystem fs.FS, path string) (_ *FileMetadata, err error) {
	defer mon.Task()(&ctx)(&err)
	if !safeContentPath(path) && path != "THIRD-PARTY-NOTICES.txt" && path != "internal/tbtd/content/campaign.go" && path != "internal/tbtd/campaign/story.go" && path != "internal/tbtd/campaign/validation.go" {
		return nil, ErrInvalid.New("unsafe path %q", path)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	file, err := filesystem.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, Error.Wrap(fmt.Errorf("open %s: %w", path, err))
	}
	info, statErr := file.Stat()
	if statErr != nil {
		return nil, Error.Wrap(errors.Join(statErr, file.Close()))
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return nil, ErrInvalid.Wrap(errors.Join(fmt.Errorf("%s: not a nonempty regular file", path), file.Close()))
	}
	hash := sha256.New()
	count, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return nil, Error.Wrap(errors.Join(copyErr, closeErr))
	}
	if count != info.Size() {
		return nil, ErrInvalid.New("%s: file changed while hashing", path)
	}
	return &FileMetadata{Path: path, Size: count, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

// vorbisSamples verifies a single complete Ogg stream and returns its final
// decoded-sample granule. It checks framing, packet order and page checksums;
// perceptual and full codec decoding remain production review responsibilities.
func vorbisSamples(ctx context.Context, filesystem fs.FS, path string) (_ int64, err error) {
	defer mon.Task()(&ctx)(&err)
	file, err := filesystem.Open(path)
	if err != nil {
		return 0, Error.Wrap(err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	var serial, sequence uint32
	var packet []byte
	packetCount := 0
	continuation, ended := false, false
	var samples, maxBlock int64
	for {
		if err = ctx.Err(); err != nil {
			return 0, err
		}
		header := make([]byte, 27)
		_, readErr := io.ReadFull(file, header)
		if errors.Is(readErr, io.EOF) && ended {
			break
		}
		if readErr != nil || ended || string(header[:4]) != "OggS" || header[4] != 0 || header[5]&^byte(7) != 0 {
			return 0, ErrInvalid.New("%s: truncated or invalid Ogg page", path)
		}
		if (header[5]&1 != 0) != continuation || (sequence == 0 && header[5]&2 == 0) || (sequence != 0 && header[5]&2 != 0) {
			return 0, ErrInvalid.New("%s: invalid Ogg stream flags", path)
		}
		if sequence == 0 {
			serial = binary.LittleEndian.Uint32(header[14:18])
		}
		if binary.LittleEndian.Uint32(header[14:18]) != serial || binary.LittleEndian.Uint32(header[18:22]) != sequence {
			return 0, ErrInvalid.New("%s: invalid Ogg page sequence", path)
		}
		sequence++
		lacing := make([]byte, int(header[26]))
		if _, readErr = io.ReadFull(file, lacing); readErr != nil || len(lacing) == 0 {
			return 0, ErrInvalid.New("%s: truncated Ogg segment table", path)
		}
		size := 0
		for _, segment := range lacing {
			size += int(segment)
		}
		body := make([]byte, size)
		if _, readErr = io.ReadFull(file, body); readErr != nil {
			return 0, ErrInvalid.New("%s: truncated Ogg packet", path)
		}
		checksum := binary.LittleEndian.Uint32(header[22:26])
		clear(header[22:26])
		var crc uint32
		for _, part := range [][]byte{header, lacing, body} {
			for _, value := range part {
				crc ^= uint32(value) << 24
				for bit := 0; bit < 8; bit++ {
					if crc&0x80000000 != 0 {
						crc = crc<<1 ^ 0x04c11db7
					} else {
						crc <<= 1
					}
				}
			}
		}
		if crc != checksum {
			return 0, ErrInvalid.New("%s: invalid Ogg checksum", path)
		}
		offset := 0
		for _, segment := range lacing {
			end := offset + int(segment)
			if packetCount < 3 {
				if len(packet)+int(segment) > 1<<20 {
					return 0, ErrInvalid.New("%s: oversized Vorbis header", path)
				}
				packet = append(packet, body[offset:end]...)
			} else if len(packet) == 0 && end > offset {
				packet = append(packet, body[offset])
			}
			offset = end
			continuation = segment == 255
			if continuation {
				continue
			}
			if err = checkVorbisPacket(packet, packetCount); err != nil {
				return 0, ErrInvalid.Wrap(fmt.Errorf("%s: %w", path, err))
			}
			if packetCount == 0 {
				maxBlock = int64(1) << (packet[28] >> 4)
			}
			packetCount++
			packet = nil
		}
		granule := binary.LittleEndian.Uint64(header[6:14])
		if granule != math.MaxUint64 {
			if granule > math.MaxInt64 || int64(granule) < samples {
				return 0, ErrInvalid.New("%s: invalid Ogg granule", path)
			}
			samples = int64(granule)
		}
		ended = header[5]&4 != 0
		if ended && (continuation || packetCount < 5 || samples <= 0 || granule == math.MaxUint64) {
			return 0, ErrInvalid.New("%s: incomplete Vorbis audio", path)
		}
		// Each pair of audio packets contributes at most half the largest
		// declared block. Reject a forged granule even if its page CRC is valid.
		if ended && (samples-1)/(maxBlock/2) >= int64(packetCount-4) {
			return 0, ErrInvalid.New("%s: Vorbis granule exceeds packet capacity", path)
		}
	}
	return samples, nil
}

// vorbisBits reads the little-endian bit fields used by the Vorbis setup header.
// It never reads beyond the bounded packet assembled by vorbisSamples.
type vorbisSetupBits struct {
	data []byte
	pos  uint64
	bad  bool
}

func (bits *vorbisSetupBits) take(count uint) uint64 {
	if bits.bad || count > 32 || bits.pos+uint64(count) > uint64(len(bits.data))*8 {
		bits.bad = true
		return 0
	}
	var value uint64
	for i := uint(0); i < count; i++ {
		value |= uint64((bits.data[bits.pos/8]>>(bits.pos%8))&1) << i
		bits.pos++
	}
	return value
}

func (bits *vorbisSetupBits) skip(count uint64) {
	if bits.bad || count > uint64(len(bits.data))*8-bits.pos {
		bits.bad = true
		return
	}
	bits.pos += count
}

func vorbisILog(value uint64) uint {
	var result uint
	for value > 0 {
		result++
		value >>= 1
	}
	return result
}

func vorbisLookupValues(entries, dimensions uint64) uint64 {
	low, high := uint64(1), entries
	for low < high {
		candidate := low + (high-low+1)/2
		product := uint64(1)
		for i := uint64(0); i < dimensions && product <= entries; i++ {
			if product > entries/candidate {
				product = entries + 1
				break
			}
			product *= candidate
		}
		if product <= entries {
			low = candidate
		} else {
			high = candidate - 1
		}
	}
	return low
}

// validateVorbisSetup checks the complete setup structure and its references.
// Decoding audio packets and perceptual quality still require production review.
func validateVorbisSetup(data []byte) error {
	bits := &vorbisSetupBits{data: data}
	books := bits.take(8) + 1
	for book := uint64(0); book < books && !bits.bad; book++ {
		if bits.take(24) != 0x564342 {
			return fmt.Errorf("invalid Vorbis codebook sync")
		}
		dimensions, entries := bits.take(16), bits.take(24)
		if bits.bad || dimensions == 0 || entries == 0 {
			return fmt.Errorf("invalid Vorbis codebook dimensions")
		}
		if bits.take(1) != 0 {
			length := bits.take(5) + 1
			for assigned := uint64(0); assigned < entries && !bits.bad; length++ {
				if length > 32 {
					return fmt.Errorf("invalid ordered codebook")
				}
				count := bits.take(vorbisILog(entries - assigned))
				if count > entries-assigned {
					return fmt.Errorf("invalid ordered codebook count")
				}
				assigned += count
			}
		} else {
			sparse := bits.take(1) != 0
			if !sparse {
				bits.skip(entries * 5)
			} else {
				for entry := uint64(0); entry < entries && !bits.bad; entry++ {
					if bits.take(1) != 0 {
						bits.take(5)
					}
				}
			}
		}
		lookup := bits.take(4)
		if lookup > 2 {
			return fmt.Errorf("invalid Vorbis lookup type")
		}
		if lookup != 0 {
			bits.take(32)
			bits.take(32)
			valueBits := bits.take(4) + 1
			bits.take(1)
			values := entries * dimensions
			if lookup == 1 {
				values = vorbisLookupValues(entries, dimensions)
			}
			bits.skip(values * valueBits)
		}
	}
	times := bits.take(6) + 1
	for i := uint64(0); i < times && !bits.bad; i++ {
		if bits.take(16) != 0 {
			return fmt.Errorf("invalid Vorbis time transform")
		}
	}
	floors := bits.take(6) + 1
	for floor := uint64(0); floor < floors && !bits.bad; floor++ {
		switch bits.take(16) {
		case 0:
			bits.skip(8 + 16 + 16 + 6 + 8)
			count := bits.take(4) + 1
			for i := uint64(0); i < count && !bits.bad; i++ {
				if bits.take(8) >= books {
					return fmt.Errorf("invalid Vorbis floor book")
				}
			}
		case 1:
			count := bits.take(5)
			partitions := make([]uint64, count)
			var classes uint64
			for i := range partitions {
				partitions[i] = bits.take(4)
				classes = max(classes, partitions[i]+1)
			}
			dimensions := make([]uint64, classes)
			for i := range dimensions {
				dimensions[i] = bits.take(3) + 1
				subclasses := bits.take(2)
				if subclasses > 0 && bits.take(8) >= books {
					return fmt.Errorf("invalid Vorbis floor masterbook")
				}
				for j := uint64(0); j < uint64(1)<<subclasses && !bits.bad; j++ {
					book := bits.take(8)
					if book > books {
						return fmt.Errorf("invalid Vorbis floor subclass book")
					}
				}
			}
			bits.take(2)
			rangeBits := bits.take(4)
			for _, class := range partitions {
				bits.skip(dimensions[class] * rangeBits)
			}
		default:
			return fmt.Errorf("invalid Vorbis floor type")
		}
	}
	residues := bits.take(6) + 1
	for residue := uint64(0); residue < residues && !bits.bad; residue++ {
		if bits.take(16) > 2 {
			return fmt.Errorf("invalid Vorbis residue type")
		}
		bits.skip(24 + 24 + 24)
		count := bits.take(6) + 1
		if bits.take(8) >= books {
			return fmt.Errorf("invalid Vorbis residue classbook")
		}
		cascades := make([]uint64, count)
		for i := range cascades {
			cascades[i] = bits.take(3)
			if bits.take(1) != 0 {
				cascades[i] |= bits.take(5) << 3
			}
		}
		for _, cascade := range cascades {
			for i := uint(0); i < 8; i++ {
				if cascade&(1<<i) != 0 && bits.take(8) >= books {
					return fmt.Errorf("invalid Vorbis residue book")
				}
			}
		}
	}
	mappings := bits.take(6) + 1
	for mapping := uint64(0); mapping < mappings && !bits.bad; mapping++ {
		if bits.take(16) != 0 {
			return fmt.Errorf("invalid Vorbis mapping type")
		}
		submaps := uint64(1)
		if bits.take(1) != 0 {
			submaps = bits.take(4) + 1
		}
		if bits.take(1) != 0 {
			steps := bits.take(8) + 1
			for i := uint64(0); i < steps && !bits.bad; i++ {
				if bits.take(1) == bits.take(1) { // Stereo has one bit per channel index.
					return fmt.Errorf("invalid Vorbis coupling")
				}
			}
		}
		if bits.take(2) != 0 {
			return fmt.Errorf("invalid Vorbis mapping reserved bits")
		}
		if submaps > 1 {
			for channel := 0; channel < 2; channel++ {
				if bits.take(4) >= submaps {
					return fmt.Errorf("invalid Vorbis channel mapping")
				}
			}
		}
		for i := uint64(0); i < submaps && !bits.bad; i++ {
			if bits.take(8) != 0 || bits.take(8) >= floors || bits.take(8) >= residues {
				return fmt.Errorf("invalid Vorbis submap")
			}
		}
	}
	modes := bits.take(6) + 1
	for mode := uint64(0); mode < modes && !bits.bad; mode++ {
		bits.take(1)
		if bits.take(16) != 0 || bits.take(16) != 0 || bits.take(8) >= mappings {
			return fmt.Errorf("invalid Vorbis mode")
		}
	}
	if bits.take(1) != 1 || bits.bad {
		return fmt.Errorf("truncated Vorbis setup or missing framing bit")
	}
	return nil
}

func checkVorbisPacket(packet []byte, index int) error {
	if index >= 3 {
		if len(packet) == 0 || packet[0]&1 != 0 {
			return fmt.Errorf("invalid Vorbis audio packet")
		}
		return nil
	}
	if len(packet) < 7 || packet[0] != []byte{1, 3, 5}[index] || string(packet[1:7]) != "vorbis" {
		return fmt.Errorf("invalid Vorbis header packet")
	}
	if index == 0 {
		if len(packet) != 30 || binary.LittleEndian.Uint32(packet[7:11]) != 0 || packet[11] != 2 || binary.LittleEndian.Uint32(packet[12:16]) != 48000 || packet[28]&15 < 6 || packet[28]&15 > packet[28]>>4 || packet[28]>>4 > 13 || packet[29] != 1 {
			return fmt.Errorf("invalid Vorbis identification")
		}
	} else if index == 1 {
		if len(packet) < 16 {
			return fmt.Errorf("truncated Vorbis comments")
		}
		offset := uint64(11) + uint64(binary.LittleEndian.Uint32(packet[7:11]))
		if offset+4 > uint64(len(packet)) {
			return fmt.Errorf("truncated Vorbis vendor")
		}
		count := binary.LittleEndian.Uint32(packet[offset : offset+4])
		offset += 4
		for i := uint32(0); i < count; i++ {
			if offset+4 > uint64(len(packet)) {
				return fmt.Errorf("truncated Vorbis comments")
			}
			size := binary.LittleEndian.Uint32(packet[offset : offset+4])
			offset += 4 + uint64(size)
			if offset > uint64(len(packet)) {
				return fmt.Errorf("truncated Vorbis comment")
			}
		}
		if offset+1 != uint64(len(packet)) || packet[offset] != 1 {
			return fmt.Errorf("invalid Vorbis comment framing")
		}
	} else if err := validateVorbisSetup(packet[7:]); err != nil {
		return err
	}
	return nil
}

// validateFont checks the complete sfnt directory and essential table bounds.
// Glyph design and linguistic coverage still require production review.
func validateFont(data []byte) error {
	if len(data) < 12 || (string(data[:4]) != "\x00\x01\x00\x00" && string(data[:4]) != "OTTO" && string(data[:4]) != "true") {
		return fmt.Errorf("invalid font header")
	}
	count := int(binary.BigEndian.Uint16(data[4:6]))
	if count == 0 || count > 4096 || 12+16*count > len(data) {
		return fmt.Errorf("truncated font directory")
	}
	type table struct{ start, end int }
	tables := make(map[string]table, count)
	var ranges []table
	for i := 0; i < count; i++ {
		entry := data[12+i*16 : 28+i*16]
		tag := string(entry[:4])
		offset, length := uint64(binary.BigEndian.Uint32(entry[8:12])), uint64(binary.BigEndian.Uint32(entry[12:16]))
		if _, exists := tables[tag]; exists || length == 0 || offset%4 != 0 || offset < uint64(12+16*count) || offset+length > uint64(len(data)) {
			return fmt.Errorf("invalid font table %q", tag)
		}
		item := table{start: int(offset), end: int(offset + length)}
		tables[tag] = item
		ranges = append(ranges, item)
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	for i := 1; i < len(ranges); i++ {
		if ranges[i].start < ranges[i-1].end {
			return fmt.Errorf("overlapping font tables")
		}
	}
	for _, required := range []struct {
		tag     string
		minimum int
	}{{"cmap", 12}, {"head", 54}, {"hhea", 36}, {"hmtx", 4}, {"maxp", 6}, {"name", 6}} {
		item, ok := tables[required.tag]
		if !ok || item.end-item.start < required.minimum {
			return fmt.Errorf("missing or truncated font table %s", required.tag)
		}
	}
	head := tables["head"]
	if binary.BigEndian.Uint32(data[head.start+12:head.start+16]) != 0x5f0f3cf5 {
		return fmt.Errorf("invalid font head magic")
	}
	maxp := tables["maxp"]
	glyphs := int(binary.BigEndian.Uint16(data[maxp.start+4 : maxp.start+6]))
	hhea := tables["hhea"]
	metrics := int(binary.BigEndian.Uint16(data[hhea.start+34 : hhea.start+36]))
	hmtx := tables["hmtx"]
	if glyphs == 0 || metrics == 0 || metrics > glyphs || hmtx.end-hmtx.start < 4*metrics+2*(glyphs-metrics) {
		return fmt.Errorf("invalid font metrics")
	}
	cmap := tables["cmap"]
	cmapData := data[cmap.start:cmap.end]
	encodings := int(binary.BigEndian.Uint16(cmapData[2:4]))
	if binary.BigEndian.Uint16(cmapData[:2]) != 0 || encodings == 0 || 4+8*encodings > len(cmapData) {
		return fmt.Errorf("invalid font cmap")
	}
	for i := 0; i < encodings; i++ {
		offset := uint64(binary.BigEndian.Uint32(cmapData[8+i*8 : 12+i*8]))
		if offset < uint64(4+8*encodings) || offset+4 > uint64(len(cmapData)) {
			return fmt.Errorf("invalid font cmap subtable")
		}
		format := binary.BigEndian.Uint16(cmapData[offset : offset+2])
		var length uint64
		if format == 12 || format == 13 {
			if offset+8 > uint64(len(cmapData)) {
				return fmt.Errorf("truncated font cmap subtable")
			}
			length = uint64(binary.BigEndian.Uint32(cmapData[offset+4 : offset+8]))
		} else {
			length = uint64(binary.BigEndian.Uint16(cmapData[offset+2 : offset+4]))
		}
		if length < 4 || offset+length > uint64(len(cmapData)) {
			return fmt.Errorf("truncated font cmap subtable")
		}
	}
	if string(data[:4]) == "OTTO" {
		if _, cff := tables["CFF "]; !cff {
			if _, cff2 := tables["CFF2"]; !cff2 {
				return fmt.Errorf("missing font outlines")
			}
		}
	} else {
		loca, hasLoca := tables["loca"]
		glyf, hasGlyf := tables["glyf"]
		format := binary.BigEndian.Uint16(data[head.start+50 : head.start+52])
		if !hasLoca || !hasGlyf || format > 1 {
			return fmt.Errorf("missing font outlines")
		}
		width := 2
		if format == 1 {
			width = 4
		}
		if loca.end-loca.start < (glyphs+1)*width {
			return fmt.Errorf("truncated font loca")
		}
		var previous uint32
		for i := 0; i <= glyphs; i++ {
			pos := loca.start + i*width
			var offset uint32
			if width == 2 {
				offset = uint32(binary.BigEndian.Uint16(data[pos:pos+2])) * 2
			} else {
				offset = binary.BigEndian.Uint32(data[pos : pos+4])
			}
			if offset < previous || uint64(offset) > uint64(glyf.end-glyf.start) {
				return fmt.Errorf("invalid font glyph offset")
			}
			previous = offset
		}
	}
	return nil
}

// validateMediaHeader rejects obvious substitutions and technical mismatches.
// Listening, glyph coverage and full decoder validation remain manual handoff work.
func validateMediaHeader(ctx context.Context, filesystem fs.FS, path, kind, id string, source bool) (err error) {
	defer mon.Task()(&ctx)(&err)
	file, err := filesystem.Open(path)
	if err != nil {
		return Error.Wrap(err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return Error.Wrap(err)
	}
	if kind == "visual" && !source {
		config, decodeErr := png.DecodeConfig(io.LimitReader(file, 1<<20))
		if decodeErr != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 2048 || config.Height > 2048 {
			return ErrInvalid.New("%s: invalid PNG or atlas dimensions", path)
		}
		imageFile, openErr := filesystem.Open(path)
		if openErr != nil {
			return Error.Wrap(openErr)
		}
		_, decodeErr = png.Decode(imageFile)
		closeErr := imageFile.Close()
		if decodeErr != nil || closeErr != nil {
			return ErrInvalid.Wrap(fmt.Errorf("%s: incomplete PNG: %w", path, errors.Join(decodeErr, closeErr)))
		}
		return nil
	}
	if kind == "font" && !source {
		if info.Size() > 32<<20 {
			return ErrInvalid.New("%s: font exceeds size limit", path)
		}
		data, readErr := io.ReadAll(file)
		if readErr != nil {
			return Error.Wrap(readErr)
		}
		if err = validateFont(data); err != nil {
			return ErrInvalid.Wrap(fmt.Errorf("%s: %w", path, err))
		}
		return nil
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 65536))
	if readErr != nil {
		return Error.Wrap(readErr)
	}
	if kind == "text" || kind == "audio-score" || (source && kind != "music" && kind != "ambience" && kind != "effect" && kind != "stinger" && kind != "vocal" && kind != "audio-tail" && kind != "audio-mix") {
		return nil
	}
	if !source && (kind == "music" || kind == "ambience") {
		if len(data) < 27 || string(data[:4]) != "OggS" || data[4] != 0 {
			return ErrInvalid.New("%s: invalid Ogg header", path)
		}
		start := 27 + int(data[26])
		if len(data) < start+30 || string(data[start:start+7]) != "\x01vorbis" || data[start+11] != 2 || binary.LittleEndian.Uint32(data[start+12:start+16]) != 48000 {
			return ErrInvalid.New("%s: expected 48 kHz stereo Vorbis", path)
		}
		return nil
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || int64(binary.LittleEndian.Uint32(data[4:8]))+8 != info.Size() {
		return ErrInvalid.New("%s: invalid WAV header", path)
	}
	bits := uint16(16)
	if source {
		bits = 24
	}
	foundFormat, foundData := false, false
	var blockAlign uint16
	var byteRate uint32
	for offset := 12; offset+8 <= len(data); {
		size := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		name := string(data[offset : offset+4])
		start := offset + 8
		if name == "data" {
			foundData = foundFormat && size > 0 && int64(start)+int64(size) <= info.Size() && size%uint64(blockAlign) == 0
			if foundData && id == "STG-CREDIT" && !source && size >= uint64(byteRate) {
				return ErrInvalid.New("%s: credit stinger must be shorter than one second", path)
			}
			break
		}
		if size > uint64(len(data)-start) {
			break
		}
		if name == "fmt " {
			if size < 16 || binary.LittleEndian.Uint16(data[start:start+2]) != 1 || binary.LittleEndian.Uint32(data[start+4:start+8]) != 48000 || binary.LittleEndian.Uint16(data[start+14:start+16]) != bits {
				return ErrInvalid.New("%s: invalid PCM format", path)
			}
			channels := binary.LittleEndian.Uint16(data[start+2 : start+4])
			blockAlign = binary.LittleEndian.Uint16(data[start+12 : start+14])
			byteRate = binary.LittleEndian.Uint32(data[start+8 : start+12])
			if channels < 1 || channels > 2 || blockAlign != channels*(bits/8) || byteRate != 48000*uint32(blockAlign) {
				return ErrInvalid.New("%s: invalid PCM channels or rate", path)
			}
			foundFormat = true
		}
		offset = start + int(size) + int(size%2)
	}
	if !foundFormat || !foundData {
		return ErrInvalid.New("%s: missing PCM format or sample data", path)
	}
	return nil
}

func validateVisualHandoff(record ManifestRecord, strict bool) error {
	handoff := record.VisualHandoff
	if handoff == nil {
		if strict && record.Visual != nil {
			return fmt.Errorf("%s: unresolved visual geometry", record.ID)
		}
		return nil
	}
	if record.Visual == nil || handoff.LogicalWidth <= 0 || handoff.LogicalHeight <= 0 || handoff.LogicalWidth > 2048 || handoff.LogicalHeight > 2048 || !allText(handoff.Pivot) {
		return fmt.Errorf("%s: invalid visual geometry", record.ID)
	}
	var sockets []string
	if record.Visual.Animation != nil {
		sockets = record.Visual.Animation.Sockets
	}
	if len(handoff.Sockets) != len(sockets) {
		return fmt.Errorf("%s: incomplete socket geometry", record.ID)
	}
	for i, socket := range handoff.Sockets {
		if socket.Socket != sockets[i] || !socket.Resolved || math.IsNaN(socket.X) || math.IsNaN(socket.Y) || math.IsInf(socket.X, 0) || math.IsInf(socket.Y, 0) {
			return fmt.Errorf("%s: invalid socket geometry", record.ID)
		}
	}
	return nil
}

// ValidateManifest checks references, metadata and, in strict mode, final
// delivery and rights evidence. It does not certify artistic or legal quality.
// filesystem must confine symlinks to its root, as os.Root.FS does.
func ValidateManifest(ctx context.Context, filesystem fs.FS, manifest *Manifest, strict bool) (err error) {
	defer mon.Task()(&ctx)(&err)
	if filesystem == nil || manifest == nil {
		return ErrInvalid.New("missing manifest or filesystem")
	}
	canonical, err := GenerateManifest(ctx, filesystem)
	if err != nil {
		return err
	}
	return validateManifest(ctx, filesystem, manifest, strict, canonical)
}

func validateManifest(ctx context.Context, filesystem fs.FS, manifest *Manifest, strict bool, canonical *Manifest) (err error) {
	defer mon.Task()(&ctx)(&err)
	if filesystem == nil || manifest == nil || manifest.SchemaVersion != 1 || manifest.ContentVersion == "" || len(manifest.Records) == 0 {
		return ErrInvalid.New("invalid manifest header")
	}
	var required map[string]ManifestRecord
	if canonical != nil {
		if manifest.SchemaVersion != canonical.SchemaVersion || manifest.ContentVersion != canonical.ContentVersion || manifest.MusicPolicy != canonical.MusicPolicy || !reflect.DeepEqual(manifest.Mix, canonical.Mix) || !reflect.DeepEqual(manifest.Campaign, canonical.Campaign) || !reflect.DeepEqual(manifest.Components, canonical.Components) || len(manifest.Records) != len(canonical.Records) {
			return ErrInvalid.New("manifest header or inventory differs from authored registers")
		}
		required = make(map[string]ManifestRecord, len(canonical.Records))
		for _, record := range canonical.Records {
			required[record.ID] = record
		}
	}
	componentIDs := make(map[string]bool, len(manifest.Components))
	for _, component := range manifest.Components {
		if !validID(component.ID, "COMP") || componentIDs[component.ID] || !allText(component.Kind, component.Name) {
			return ErrInvalid.New("invalid or duplicate component %s", component.ID)
		}
		componentIDs[component.ID] = true
		if err = validateProvenance(ctx, filesystem, component.Provenance, strict); err != nil {
			return fmt.Errorf("%s: %w", component.ID, err)
		}
		if strict && !hasLicenseNotice(component.Provenance) {
			return ErrInvalid.New("%s: missing required license notice", component.ID)
		}
	}
	campaignIDs := make(map[string]bool, len(manifest.Campaign))
	campaignFiles := make(map[string]*FileMetadata)
	for _, item := range manifest.Campaign {
		if item.ID == "" || campaignIDs[item.ID] || len(item.SourceFiles) == 0 {
			return ErrInvalid.New("invalid or duplicate campaign identity %s", item.ID)
		}
		campaignIDs[item.ID] = true
		for _, file := range item.SourceFiles {
			if file == nil {
				return ErrInvalid.New("%s: missing campaign source metadata", item.ID)
			}
			actual, ok := campaignFiles[file.Path]
			if !ok {
				var readErr error
				actual, readErr = fingerprint(ctx, filesystem, file.Path)
				if readErr != nil {
					return readErr
				}
				campaignFiles[file.Path] = actual
			}
			if actual == nil || *actual != *file {
				return ErrInvalid.New("%s: inconsistent campaign source metadata", item.ID)
			}
		}
	}
	seen := make(map[string]bool, len(manifest.Records))
	for _, record := range manifest.Records {
		if !validPresentationID(record.ID) || seen[record.ID] || !allText(record.Kind, record.LoadGroup, record.Format) {
			return ErrInvalid.New("invalid or duplicate manifest identity %s", record.ID)
		}
		seen[record.ID] = true
		if canonical != nil {
			expected, ok := required[record.ID]
			if !ok || !reflect.DeepEqual(record, expected) {
				return ErrInvalid.New("%s: manifest differs from authored registers or delivered files", record.ID)
			}
		}
		if record.SourcePath == "" {
			if record.SourceFile != nil {
				return ErrInvalid.New("%s: source metadata without source", record.ID)
			}
		} else {
			actual, readErr := fingerprint(ctx, filesystem, record.SourcePath)
			if readErr != nil {
				return readErr
			}
			if (actual == nil) != (record.SourceFile == nil) || (actual != nil && *actual != *record.SourceFile) {
				return ErrInvalid.New("%s: inconsistent source metadata", record.ID)
			}
		}
		if record.ExportPath == "" {
			if record.File != nil {
				return ErrInvalid.New("%s: metadata without export", record.ID)
			}
		} else {
			actual, readErr := fingerprint(ctx, filesystem, record.ExportPath)
			if readErr != nil {
				return readErr
			}
			if (actual == nil) != (record.File == nil) || (actual != nil && *actual != *record.File) {
				return ErrInvalid.New("%s: inconsistent file metadata", record.ID)
			}
		}
		if len(record.AdditionalExports) != len(record.AdditionalFiles) {
			return ErrInvalid.New("%s: inconsistent additional exports", record.ID)
		}
		paths := map[string]bool{record.ExportPath: true}
		for i, path := range record.AdditionalExports {
			if paths[path] {
				return ErrInvalid.New("%s: duplicate additional export", record.ID)
			}
			paths[path] = true
			actual, readErr := fingerprint(ctx, filesystem, path)
			if readErr != nil {
				return readErr
			}
			if (actual == nil) != (record.AdditionalFiles[i] == nil) || (actual != nil && *actual != *record.AdditionalFiles[i]) || (strict && actual == nil) {
				return ErrInvalid.New("%s: inconsistent or missing additional file", record.ID)
			}
		}
		if err = validateVisualHandoff(record, strict); err != nil {
			return ErrInvalid.Wrap(err)
		}
		if record.Audio != nil {
			a := record.Audio
			if !allText(a.EventOwner, a.Direction, a.RuntimeFormat, a.SourceFormat, a.Channels, a.TargetLoudness, a.Transition, a.Tail) || a.SampleRate != 48000 || a.Variations < 1 || a.Takes < 0 || a.DurationSeconds < 0 || (a.LoopStartSample == nil) != (a.LoopEndSample == nil) || (a.LoopStartSample != nil && (*a.LoopStartSample < 0 || *a.LoopEndSample <= *a.LoopStartSample)) {
				return ErrInvalid.New("%s: invalid audio metadata", record.ID)
			}
			if strict && ((record.Kind == "ambience" || (record.Kind == "music" && record.ID != "MUS-PRO" && record.ID != "MUS-CEREMONY")) && a.LoopStartSample == nil) {
				return ErrInvalid.New("%s: unresolved loop boundaries", record.ID)
			}
			if strict && len(record.AdditionalExports)+1 < max(a.Variations, a.Takes) {
				return ErrInvalid.New("%s: missing required variations or takes", record.ID)
			}
			var samples int64
			if record.File != nil && (record.Kind == "music" || record.Kind == "ambience") {
				samples, err = vorbisSamples(ctx, filesystem, record.ExportPath)
				if err != nil {
					return fmt.Errorf("%s: %w", record.ID, err)
				}
				if a.LoopEndSample != nil && *a.LoopEndSample > samples {
					return ErrInvalid.New("%s: loop exceeds delivered samples", record.ID)
				}
				if strict && a.DurationSeconds > 0 {
					want := int64(a.DurationSeconds) * int64(a.SampleRate)
					if samples != want {
						return ErrInvalid.New("%s: delivered cue duration differs from %d samples", record.ID, want)
					}
					if a.LoopStartSample != nil && *a.LoopEndSample-*a.LoopStartSample != want {
						return ErrInvalid.New("%s: loop duration differs from %d samples", record.ID, want)
					}
				}
			}
			for _, marker := range a.Markers {
				if !allText(marker.Name) || marker.Sample < 0 || (!marker.Resolved && marker.Sample != 0) || (marker.Resolved && samples > 0 && marker.Sample >= samples) {
					return ErrInvalid.New("%s: invalid transition marker %s", record.ID, marker.Name)
				}
				if strict && !marker.Resolved {
					return ErrInvalid.New("%s: unresolved transition marker %s", record.ID, marker.Name)
				}
			}
		}
		if err = validateProvenance(ctx, filesystem, record.Provenance, strict); err != nil {
			return fmt.Errorf("%s: %w", record.ID, err)
		}
		if strict && record.Kind == "font" && !hasLicenseNotice(record.Provenance) {
			return ErrInvalid.New("%s: missing required font license notice", record.ID)
		}
		if strict && (!record.Final || record.File == nil || record.SourceFile == nil) {
			return fmt.Errorf("%s: %w", record.ID, ErrFinalMediaMissing)
		}
		if record.SourceFile != nil {
			if err = validateMediaHeader(ctx, filesystem, record.SourcePath, record.Kind, record.ID, true); err != nil {
				return fmt.Errorf("%s source: %w", record.ID, err)
			}
		}
		if record.File != nil {
			if err = validateMediaHeader(ctx, filesystem, record.ExportPath, record.Kind, record.ID, false); err != nil {
				return fmt.Errorf("%s export: %w", record.ID, err)
			}
		}
		for i, path := range record.AdditionalExports {
			if record.AdditionalFiles[i] != nil {
				if err = validateMediaHeader(ctx, filesystem, path, record.Kind, record.ID, false); err != nil {
					return fmt.Errorf("%s export: %w", record.ID, err)
				}
			}
		}
	}
	for _, item := range manifest.Campaign {
		for _, binding := range item.Bindings {
			if !seen[binding] && !campaignIDs[binding] {
				return ErrInvalid.New("%s: dangling campaign reference %s", item.ID, binding)
			}
		}
	}
	for _, record := range manifest.Records {
		for _, binding := range record.Bindings {
			if !allText(binding) {
				return ErrInvalid.New("%s: empty binding", record.ID)
			}
			if validPresentationID(binding) && !seen[binding] {
				return ErrInvalid.New("%s: dangling binding %s", record.ID, binding)
			}
			for _, prefix := range []string{"LOC-", "ENC-", "ANC-", "D-", "OBJ-", "CON-", "EM-", "NAR-", "END-", "G-"} {
				if strings.HasPrefix(binding, prefix) && !campaignIDs[binding] {
					return ErrInvalid.New("%s: dangling campaign binding %s", record.ID, binding)
				}
			}
		}
		if record.Audio != nil && record.Audio.Caption != "" && !seen[record.Audio.Caption] {
			return ErrInvalid.New("%s: dangling caption", record.ID)
		}
	}
	return nil
}

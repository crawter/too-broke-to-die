// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"slices"
	"sort"
	"strings"
)

// ErrFinalMediaMissing distinguishes an undelivered export from invalid content.
var ErrFinalMediaMissing = errors.New("required final visual media is missing")

// VisualTechnical describes production constraints, not measurements of an export.
// Zero dimensions and empty paths, pivot and socket coordinates mean unresolved.
type VisualTechnical struct {
	SourcePath               string
	ExportPath               string
	LogicalWidth             int
	LogicalHeight            int
	Pivot                    string
	SourceScale              int
	ExportFormat             string
	ColorSpace               string
	AtlasMax                 int
	Padding                  int
	Extrusion                int
	StructuralUnit           int
	CollisionOverlayPreview  bool
	EditableLayers           bool
	SourceFormat             string
	ReferenceCharacterWidth  int
	ReferenceCharacterHeight int
}

// TableauRequirement makes the final composition mandatory independently of
// artwork and avoids treating an optional fire vignette as the ending.
type TableauRequirement struct {
	AspectWidth                  int
	AspectHeight                 int
	WingMinimumQuarter           bool
	RibbonAndMinisterLowerCenter bool
	BossesFlankMinister          bool
	FlamesCrossWingWindows       bool
	NinaEvacuatesOpposite        bool
	ClosingTextClearUpperBand    bool
}

// AnimationMarker names a semantic simulation event. Timing is "milliseconds",
// "ticks", or "event"; a known zero offset is distinct from an event boundary.
type AnimationMarker struct {
	Event        string
	Milliseconds int
	Tick         int
	Timing       string
}

// SocketPosition is a required socket position. Resolved is false until an
// artist supplies coordinates; a resolved origin is not treated as missing.
type SocketPosition struct {
	Socket   string
	X        float64
	Y        float64
	Resolved bool
}

// AnimationRequirement records the authored reference timing and interruption
// contract. Zero duration means the action or scene determines the duration.
type AnimationRequirement struct {
	Poses             int
	PoseCountStatus   string
	PoseRequirements  []string
	DurationMS        int
	DurationMode      string
	MinimumTicks      int
	TickRate          int
	DrawingsPerSecond int
	Markers           []AnimationMarker
	Sockets           []string
	SocketPositions   []SocketPosition
	Layers            []string
	Interruptions     []string
}

// EffectRequirement captures limits and essential-feedback exclusions. Counts
// apply to decorative effects only, never to evidence or aftermath.
type EffectRequirement struct {
	HitSprayMaxMS           int
	RemovalSprayMaxMS       int
	DecorativeStainLimit    int
	RoomParticleLimit       int
	FadeOldestFirst         bool
	ExcludeEssentialMarkers bool
	ReducedPreservesHarm    bool
}

// VisualRecord is a production requirement, not an installed media file. No
// path, dimension, digest, authorship or delivery is inferred from its ID.
type VisualRecord struct {
	ID                 string
	Kind               string
	Direction          string
	Bindings           []string
	States             []string
	Palette            []string
	Technical          VisualTechnical
	Animation          *AnimationRequirement
	Effect             *EffectRequirement
	Tableau            *TableauRequirement
	FinalExportPresent bool
}

// VisualAssets owns a validated, sorted set of independent visual requirements.
type VisualAssets struct {
	records []VisualRecord
}

type visualCharacter struct {
	Code      string `json:"code"`
	Direction string `json:"direction"`
}

type visualEnemy struct {
	Institution string `json:"institution"`
	Melee       string `json:"melee"`
	Ranged      string `json:"ranged"`
}

type characterRegister struct {
	SchemaVersion   int               `json:"schema_version"`
	Principals      []visualCharacter `json:"principals"`
	EnemySets       []visualEnemy     `json:"enemy_sets"`
	Expressions     []string          `json:"expressions"`
	CrowdHeads      int               `json:"crowd_heads"`
	CrowdTorsos     int               `json:"crowd_torsos"`
	CrowdActivities int               `json:"crowd_activities"`
}

type visualInstitution struct {
	Code      string   `json:"code"`
	Palette   []string `json:"palette"`
	Direction string   `json:"direction"`
}

type visualFamily struct {
	ID        string   `json:"id"`
	Direction string   `json:"direction"`
	Roles     []string `json:"roles"`
	States    []string `json:"states"`
}

type environmentRegister struct {
	SchemaVersion   int                 `json:"schema_version"`
	Institutions    []visualInstitution `json:"institutions"`
	Families        []visualFamily      `json:"families"`
	EmergencyStates []string            `json:"emergency_states"`
	Prologue        []string            `json:"prologue"`
	Endings         []string            `json:"endings"`
	Tableau         string              `json:"tableau"`
}

type visualClip struct {
	State         string   `json:"state"`
	Poses         int      `json:"poses"`
	DurationMS    int      `json:"duration_ms"`
	Direction     string   `json:"direction"`
	Markers       []string `json:"markers"`
	Interruptions []string `json:"interruptions"`
}

type animationRegister struct {
	SchemaVersion int          `json:"schema_version"`
	Player        []visualClip `json:"player"`
	EnemyStates   []string     `json:"enemy_states"`
	RescueStates  []string     `json:"rescue_states"`
	BossPhases    []string     `json:"boss_phases"`
	Sockets       []string     `json:"sockets"`
}

type interfaceRegister struct {
	SchemaVersion int      `json:"schema_version"`
	HUD           []string `json:"hud"`
	Menus         []string `json:"menus"`
	Ledger        []string `json:"ledger"`
	Recovery      []string `json:"recovery"`
	Markers       []string `json:"markers"`
	Effects       []string `json:"effects"`
	Signs         []string `json:"signs"`
}

// LoadVisualAssets reads the authored registers and expands them against the
// validated campaign and Russian text. Missing external exports are valid
// production requirements; malformed input and dangling references are errors.
func LoadVisualAssets(ctx context.Context, filesystem fs.FS, campaign *Campaign, text *RussianText) (_ *VisualAssets, err error) {
	defer mon.Task()(&ctx)(&err)

	if campaign == nil || text == nil {
		return nil, ErrInvalid.New("visual register requires campaign and Russian text")
	}
	if len(campaign.Deeds(ctx)) != 100 || len(campaign.Batches(ctx)) != 16 {
		return nil, ErrInvalid.New("visual register requires the complete campaign")
	}
	var characters characterRegister
	var environments environmentRegister
	var animations animationRegister
	var ui interfaceRegister
	for _, document := range []struct {
		path   string
		target any
	}{
		{"content/catalogue/characters.json", &characters},
		{"content/catalogue/environments.json", &environments},
		{"content/catalogue/animations.json", &animations},
		{"content/catalogue/interface.json", &ui},
	} {
		if err = readCampaignDocument(ctx, filesystem, document.path, document.target); err != nil {
			return nil, err
		}
	}
	if err = validateVisualSources(ctx, characters, environments, animations, ui); err != nil {
		return nil, ErrInvalid.Wrap(err)
	}
	records, err := expandVisualSources(ctx, characters, environments, animations, ui, campaign, text)
	if err != nil {
		return nil, ErrInvalid.Wrap(err)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	for i, record := range records {
		if !validPresentationID(record.ID) || record.Kind == "" || strings.TrimSpace(record.Direction) == "" || (i > 0 && records[i-1].ID == record.ID) {
			return nil, ErrInvalid.New("invalid or duplicate visual record %q", record.ID)
		}
		if record.FinalExportPresent || record.Technical.SourcePath != "" || record.Technical.ExportPath != "" {
			return nil, ErrInvalid.New("%s: unverified final media", record.ID)
		}
		if record.Kind == "environment" && !validVisualPalette(record.Palette) {
			return nil, ErrInvalid.New("%s: invalid environment palette", record.ID)
		}
		if record.Animation != nil {
			if err = validateVisualAnimation(record); err != nil {
				return nil, ErrInvalid.Wrap(err)
			}
		}
		if record.Kind == "effect" && !validEffectRequirement(record.Effect) {
			return nil, ErrInvalid.New("%s: invalid effect limits", record.ID)
		}
	}
	if err = validateVisualBindings(ctx, records, campaign, text); err != nil {
		return nil, ErrInvalid.Wrap(err)
	}
	return &VisualAssets{records: records}, nil
}

func validEffectRequirement(effect *EffectRequirement) bool {
	return effect != nil && effect.HitSprayMaxMS > 0 && effect.HitSprayMaxMS <= 400 && effect.RemovalSprayMaxMS > 0 && effect.RemovalSprayMaxMS <= 600 && effect.DecorativeStainLimit > 0 && effect.DecorativeStainLimit <= 48 && effect.RoomParticleLimit > 0 && effect.RoomParticleLimit <= 512 && effect.FadeOldestFirst && effect.ExcludeEssentialMarkers && effect.ReducedPreservesHarm
}

func validateVisualAnimation(record VisualRecord) error {
	clip := record.Animation
	if clip == nil || clip.DrawingsPerSecond != 12 || len(clip.PoseRequirements) == 0 || !allText(clip.PoseRequirements...) || len(clip.Interruptions) == 0 || !allText(clip.Interruptions...) {
		return fmt.Errorf("%s: incomplete animation requirements", record.ID)
	}
	if (clip.PoseCountStatus == "specified" && clip.Poses <= 0) || (clip.PoseCountStatus == "unresolved" && clip.Poses != 0) || (clip.PoseCountStatus != "specified" && clip.PoseCountStatus != "unresolved") {
		return fmt.Errorf("%s: invalid pose count", record.ID)
	}
	switch clip.DurationMode {
	case "fixed", "minimum", "maximum", "reference":
		if clip.DurationMS <= 0 {
			return fmt.Errorf("%s: invalid duration", record.ID)
		}
	case "event":
		if clip.DurationMS != 0 {
			return fmt.Errorf("%s: event duration must be unresolved", record.ID)
		}
	case "simulation":
		if clip.DurationMS <= 0 || clip.MinimumTicks <= 0 || clip.TickRate != 120 {
			return fmt.Errorf("%s: invalid simulation duration", record.ID)
		}
	default:
		return fmt.Errorf("%s: unknown duration mode", record.ID)
	}
	if len(clip.Markers) == 0 {
		return fmt.Errorf("%s: missing markers", record.ID)
	}
	seen := make(map[string]bool)
	for _, marker := range clip.Markers {
		if marker.Event == "" || seen[marker.Event] || marker.Milliseconds < 0 || marker.Tick < 0 {
			return fmt.Errorf("%s: invalid marker %s", record.ID, marker.Event)
		}
		seen[marker.Event] = true
		switch marker.Timing {
		case "event":
			if marker.Tick != 0 || marker.Milliseconds != 0 {
				return fmt.Errorf("%s: event marker has a numeric offset", record.ID)
			}
		case "milliseconds":
			if marker.Tick != 0 {
				return fmt.Errorf("%s: mixed marker units", record.ID)
			}
		case "ticks":
			if clip.TickRate != 120 || marker.Milliseconds != 0 || marker.Tick >= clip.MinimumTicks {
				return fmt.Errorf("%s: invalid tick marker", record.ID)
			}
		default:
			return fmt.Errorf("%s: unknown marker timing", record.ID)
		}
	}
	if record.Kind == "player-animation" {
		if !slices.Equal(clip.Sockets, []string{"LIM-LA", "LIM-RA", "LIM-LL", "LIM-RL"}) || !slices.Equal(clip.Layers, []string{"torso", "head", "sockets", "limbs"}) || len(clip.SocketPositions) != len(clip.Sockets) {
			return fmt.Errorf("%s: incomplete modular body", record.ID)
		}
		for i, position := range clip.SocketPositions {
			if position.Socket != clip.Sockets[i] || math.IsNaN(position.X) || math.IsNaN(position.Y) || math.IsInf(position.X, 0) || math.IsInf(position.Y, 0) || (!position.Resolved && (position.X != 0 || position.Y != 0)) {
				return fmt.Errorf("%s: invalid socket position", record.ID)
			}
		}
	}
	return nil
}

func copyVisual(record VisualRecord) VisualRecord {
	record.Bindings = slices.Clone(record.Bindings)
	record.States = slices.Clone(record.States)
	record.Palette = slices.Clone(record.Palette)
	if record.Animation != nil {
		clip := *record.Animation
		clip.Markers = slices.Clone(clip.Markers)
		clip.Sockets = slices.Clone(clip.Sockets)
		clip.SocketPositions = slices.Clone(clip.SocketPositions)
		clip.PoseRequirements = slices.Clone(clip.PoseRequirements)
		clip.Layers = slices.Clone(clip.Layers)
		clip.Interruptions = slices.Clone(clip.Interruptions)
		record.Animation = &clip
	}
	if record.Effect != nil {
		effect := *record.Effect
		record.Effect = &effect
	}
	if record.Tableau != nil {
		tableau := *record.Tableau
		record.Tableau = &tableau
	}
	return record
}

// Records returns sorted, isolated copies, suitable for manifest generation.
func (assets *VisualAssets) Records(ctx context.Context) []VisualRecord {
	defer mon.Task()(&ctx)(nil)
	result := make([]VisualRecord, len(assets.records))
	for i, record := range assets.records {
		result[i] = copyVisual(record)
	}
	return result
}

// Record looks up one stable identity and returns an isolated copy.
func (assets *VisualAssets) Record(id string) (VisualRecord, bool) {
	i, found := slices.BinarySearchFunc(assets.records, id, func(record VisualRecord, id string) int {
		return strings.Compare(record.ID, id)
	})
	if !found {
		return VisualRecord{}, false
	}
	return copyVisual(assets.records[i]), true
}

// RequireFinalMedia reports the first absent export without treating the valid
// production requirement itself as malformed. It cannot establish rights or
// perceptual qualification, even after a future media handoff.
func (assets *VisualAssets) RequireFinalMedia(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	for _, record := range assets.records {
		if !record.FinalExportPresent {
			return fmt.Errorf("%s: %w", record.ID, ErrFinalMediaMissing)
		}
	}
	return nil
}

func visualBase(id, kind, direction string, bindings ...string) VisualRecord {
	return VisualRecord{
		ID: id, Kind: kind, Direction: direction, Bindings: bindings,
		Technical: VisualTechnical{SourceScale: 2, SourceFormat: "editable layered source", ExportFormat: "RGBA PNG", ColorSpace: "sRGB", AtlasMax: 2048, Padding: 4, Extrusion: 4, CollisionOverlayPreview: true, EditableLayers: true},
	}
}

func addUnique(values []string, value string) []string {
	if value != "" && !slices.Contains(values, value) {
		return append(values, value)
	}
	return values
}

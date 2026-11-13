// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// Provenance records evidence and dispositions, not legal clearance. Empty
// fields mean unresolved. A documented inapplicable field must explain why.
type Provenance struct {
	Authorship       string          `json:"authorship"`
	SourceMaterial   string          `json:"source_material"`
	License          string          `json:"license"`
	Permission       string          `json:"permission"`
	PerformerConsent string          `json:"performer_consent"`
	Attribution      string          `json:"attribution"`
	Evidence         []string        `json:"evidence"`
	Notices          []string        `json:"notices"`
	NoticeFiles      []*FileMetadata `json:"notice_files"`
	Review           string          `json:"review"`
	ReviewStatus     string          `json:"review_status"`
}

type provenanceEntry struct {
	AssetID          string   `json:"asset_id"`
	Notices          []string `json:"notices,omitempty"`
	Final            bool     `json:"final"`
	Authorship       string   `json:"authorship"`
	SourceMaterial   string   `json:"source_material"`
	License          string   `json:"license"`
	Permission       string   `json:"permission"`
	PerformerConsent string   `json:"performer_consent"`
	Attribution      string   `json:"attribution"`
	Evidence         []string `json:"evidence"`
	Review           string   `json:"review"`
	ReviewStatus     string   `json:"review_status"`
}

type deliveryEntry struct {
	AssetID           string             `json:"asset_id"`
	SourcePath        string             `json:"source_path"`
	ExportPath        string             `json:"export_path"`
	AdditionalExports []string           `json:"additional_exports"`
	LoopStartSample   int64              `json:"loop_start_sample"`
	LoopEndSample     int64              `json:"loop_end_sample"`
	VisualHandoff     []VisualHandoff    `json:"visual_handoff,omitempty"`
	Markers           []TransitionMarker `json:"markers,omitempty"`
}

// ComponentProvenance records rights for a dependency, decoder or production tool.
// Names identify review targets, not claims that a particular version is cleared.
type ComponentProvenance struct {
	ID         string      `json:"id"`
	Kind       string      `json:"kind"`
	Name       string      `json:"name"`
	Provenance *Provenance `json:"provenance"`
}

type componentEntry struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	Name             string   `json:"name"`
	Authorship       string   `json:"authorship"`
	SourceMaterial   string   `json:"source_material"`
	License          string   `json:"license"`
	Permission       string   `json:"permission"`
	PerformerConsent string   `json:"performer_consent"`
	Attribution      string   `json:"attribution"`
	Evidence         []string `json:"evidence"`
	Notices          []string `json:"notices"`
	Review           string   `json:"review"`
	ReviewStatus     string   `json:"review_status"`
}

type provenanceSource struct {
	SchemaVersion int               `json:"schema_version"`
	ReviewPolicy  string            `json:"review_policy"`
	Deliveries    []deliveryEntry   `json:"deliveries"`
	Sources       []provenanceEntry `json:"sources"`
	Components    []componentEntry  `json:"components,omitempty"`
}

func applyProvenance(ctx context.Context, filesystem fs.FS, manifest *Manifest, source provenanceSource) (err error) {
	defer mon.Task()(&ctx)(&err)
	if source.SchemaVersion != 1 || !allText(source.ReviewPolicy) {
		return ErrInvalid.New("invalid provenance register")
	}
	indices := make(map[string]int, len(manifest.Records))
	for i := range manifest.Records {
		record := &manifest.Records[i]
		if _, exists := indices[record.ID]; exists {
			return ErrInvalid.New("duplicate asset %s", record.ID)
		}
		indices[record.ID] = i
		record.Provenance = &Provenance{Evidence: []string{}}
	}
	delivered := make(map[string]bool)
	for _, delivery := range source.Deliveries {
		index, exists := indices[delivery.AssetID]
		if !exists || delivered[delivery.AssetID] || delivery.ExportPath == "" || !safeContentPath(delivery.ExportPath) || (delivery.SourcePath != "" && !safeContentPath(delivery.SourcePath)) {
			return ErrInvalid.New("invalid or duplicate delivery %s", delivery.AssetID)
		}
		delivered[delivery.AssetID] = true
		record := &manifest.Records[index]
		if record.Kind == "text" {
			return ErrInvalid.New("text delivery is owned by its source document: %s", record.ID)
		}
		if len(delivery.VisualHandoff) > 1 || (len(delivery.VisualHandoff) > 0 && record.Visual == nil) {
			return ErrInvalid.New("%s: invalid visual handoff", record.ID)
		}
		if len(delivery.VisualHandoff) == 1 {
			record.VisualHandoff = &delivery.VisualHandoff[0]
		}
		record.SourcePath, record.ExportPath = delivery.SourcePath, delivery.ExportPath
		record.AdditionalExports = append([]string(nil), delivery.AdditionalExports...)
		seen := map[string]bool{delivery.ExportPath: true}
		for _, path := range delivery.AdditionalExports {
			if !safeContentPath(path) || seen[path] {
				return ErrInvalid.New("%s: unsafe or duplicate additional export", record.ID)
			}
			seen[path] = true
		}
		if delivery.LoopStartSample < 0 || delivery.LoopEndSample < 0 || (delivery.LoopEndSample == 0 && delivery.LoopStartSample != 0) || (delivery.LoopEndSample > 0 && (record.Audio == nil || (record.Kind != "music" && record.Kind != "ambience") || record.ID == "MUS-PRO" || record.ID == "MUS-CEREMONY" || delivery.LoopStartSample >= delivery.LoopEndSample || (record.Audio.DurationSeconds > 0 && delivery.LoopEndSample > int64(record.Audio.DurationSeconds)*int64(record.Audio.SampleRate)))) {
			return ErrInvalid.New("%s: invalid delivered loop boundaries", record.ID)
		}
		if delivery.LoopEndSample > 0 {
			record.Audio.LoopStartSample = &delivery.LoopStartSample
			record.Audio.LoopEndSample = &delivery.LoopEndSample
		}
		markerNames := make(map[string]bool)
		for _, marker := range delivery.Markers {
			if record.Audio == nil || markerNames[marker.Name] || marker.Sample < 0 || (!marker.Resolved && marker.Sample != 0) {
				return ErrInvalid.New("%s: invalid transition marker %s", record.ID, marker.Name)
			}
			markerNames[marker.Name] = true
			found := false
			for i := range record.Audio.Markers {
				if record.Audio.Markers[i].Name == marker.Name {
					record.Audio.Markers[i] = marker
					found = true
					break
				}
			}
			if !found {
				return ErrInvalid.New("%s: unknown transition marker %s", record.ID, marker.Name)
			}
		}
	}
	entries := make(map[string]bool)
	for _, entry := range source.Sources {
		index, exists := indices[entry.AssetID]
		if !exists || entries[entry.AssetID] {
			return ErrInvalid.New("invalid or duplicate provenance %s", entry.AssetID)
		}
		entries[entry.AssetID] = true
		manifest.Records[index].Final = entry.Final
		manifest.Records[index].Provenance = &Provenance{Authorship: entry.Authorship, SourceMaterial: entry.SourceMaterial, License: entry.License, Permission: entry.Permission, PerformerConsent: entry.PerformerConsent, Attribution: entry.Attribution, Evidence: entry.Evidence, Notices: entry.Notices, Review: entry.Review, ReviewStatus: entry.ReviewStatus}
	}
	if err = applyComponents(ctx, filesystem, manifest, source.Components); err != nil {
		return err
	}
	for i := range manifest.Records {
		record := &manifest.Records[i]
		if err = fingerprintNotices(ctx, filesystem, record.Provenance); err != nil {
			return fmt.Errorf("%s: %w", record.ID, err)
		}
		if record.SourcePath != "" {
			record.SourceFile, err = fingerprint(ctx, filesystem, record.SourcePath)
			if err != nil {
				return err
			}
		}
		if record.ExportPath != "" {
			record.File, err = fingerprint(ctx, filesystem, record.ExportPath)
			if err != nil {
				return err
			}
		}
		for _, path := range record.AdditionalExports {
			file, fileErr := fingerprint(ctx, filesystem, path)
			if fileErr != nil {
				return fileErr
			}
			record.AdditionalFiles = append(record.AdditionalFiles, file)
		}
	}
	return nil
}

func applyComponents(ctx context.Context, filesystem fs.FS, manifest *Manifest, entries []componentEntry) (err error) {
	defer mon.Task()(&ctx)(&err)
	components := map[string]ComponentProvenance{}
	for _, component := range []ComponentProvenance{
		{ID: "COMP-RAYLIB", Kind: "dependency", Name: "raylib"},
		{ID: "COMP-RAYLIB-GO", Kind: "dependency", Name: "raylib Go binding"},
		{ID: "COMP-GLFW", Kind: "dependency", Name: "GLFW"},
		{ID: "COMP-DECODERS", Kind: "decoder", Name: "bundled decoders"},
		{ID: "COMP-MONKIT", Kind: "dependency", Name: "monkit"},
		{ID: "COMP-ERRS", Kind: "dependency", Name: "errs"},
		{ID: "COMP-XSYS", Kind: "dependency", Name: "golang.org/x/sys"},
		{ID: "COMP-NOTO", Kind: "font", Name: "Noto Sans"},
	} {
		component.Provenance = &Provenance{Evidence: []string{}}
		components[component.ID] = component
	}
	seen := make(map[string]bool)
	for _, entry := range entries {
		if !validID(entry.ID, "COMP") || seen[entry.ID] || !allText(entry.Name) {
			return ErrInvalid.New("invalid or duplicate component %s", entry.ID)
		}
		switch entry.Kind {
		case "dependency", "decoder", "font", "tool", "source":
		default:
			return ErrInvalid.New("invalid component kind %s", entry.Kind)
		}
		if expected, exists := components[entry.ID]; exists && (expected.Name != entry.Name || expected.Kind != entry.Kind) {
			return ErrInvalid.New("changed component identity %s", entry.ID)
		}
		seen[entry.ID] = true
		p := &Provenance{Authorship: entry.Authorship, SourceMaterial: entry.SourceMaterial, License: entry.License, Permission: entry.Permission, PerformerConsent: entry.PerformerConsent, Attribution: entry.Attribution, Evidence: entry.Evidence, Notices: entry.Notices, Review: entry.Review, ReviewStatus: entry.ReviewStatus}
		components[entry.ID] = ComponentProvenance{ID: entry.ID, Name: entry.Name, Kind: entry.Kind, Provenance: p}
	}
	ids := make([]string, 0, len(components))
	for id := range components {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		component := components[id]
		if err = fingerprintNotices(ctx, filesystem, component.Provenance); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		manifest.Components = append(manifest.Components, component)
	}
	return nil
}

func safeNoticePath(path string) bool {
	return path == "THIRD-PARTY-NOTICES.txt" || (strings.HasPrefix(path, "licenses/") && safeContentPath(path))
}

func hasLicenseNotice(provenance *Provenance) bool {
	for _, path := range provenance.Notices {
		if strings.HasPrefix(path, "licenses/") {
			return true
		}
	}
	return false
}

func fingerprintNotices(ctx context.Context, filesystem fs.FS, provenance *Provenance) (err error) {
	defer mon.Task()(&ctx)(&err)
	seen := make(map[string]bool)
	for _, path := range provenance.Notices {
		if !safeNoticePath(path) || seen[path] {
			return ErrInvalid.New("invalid or duplicate license notice %q", path)
		}
		seen[path] = true
		file, readErr := fingerprint(ctx, filesystem, path)
		if readErr != nil {
			return readErr
		}
		provenance.NoticeFiles = append(provenance.NoticeFiles, file)
	}
	return nil
}

func validateProvenance(ctx context.Context, filesystem fs.FS, provenance *Provenance, strict bool) (err error) {
	defer mon.Task()(&ctx)(&err)
	if provenance == nil {
		return ErrInvalid.New("missing provenance record")
	}
	switch provenance.ReviewStatus {
	case "", "pending", "concerns", "reviewed":
	default:
		return ErrInvalid.New("invalid rights review status")
	}
	if len(provenance.Notices) != len(provenance.NoticeFiles) {
		return ErrInvalid.New("inconsistent license notice metadata")
	}
	noticePaths := make(map[string]bool)
	for i, path := range provenance.Notices {
		if !safeNoticePath(path) || noticePaths[path] {
			return ErrInvalid.New("invalid license notice %q", path)
		}
		noticePaths[path] = true
		actual, readErr := fingerprint(ctx, filesystem, path)
		if readErr != nil {
			return readErr
		}
		expected := provenance.NoticeFiles[i]
		if (actual == nil) != (expected == nil) || (actual != nil && *actual != *expected) || (strict && actual == nil) {
			return ErrInvalid.New("missing or inconsistent license notice %s", path)
		}
	}
	seen := make(map[string]bool)
	for _, path := range provenance.Evidence {
		if !safeContentPath(path) || seen[path] {
			return ErrInvalid.New("invalid or duplicate evidence path %q", path)
		}
		seen[path] = true
		file, readErr := fingerprint(ctx, filesystem, path)
		if readErr != nil {
			return readErr
		}
		if strict && file == nil {
			return fmt.Errorf("missing rights evidence %s", path)
		}
	}
	if !strict {
		return nil
	}
	if provenance.ReviewStatus != "reviewed" {
		return ErrInvalid.New("rights evidence review is not complete")
	}
	for _, field := range []string{provenance.Authorship, provenance.SourceMaterial, provenance.License, provenance.Permission, provenance.PerformerConsent, provenance.Attribution, provenance.Review} {
		value := strings.ToLower(strings.TrimSpace(field))
		if value == "" || value == "unknown" || value == "unresolved" || value == "pending" || value == "missing" || value == "not reviewed" || (strings.HasPrefix(value, "not applicable") && !strings.HasPrefix(value, "not applicable: ")) {
			return ErrInvalid.New("incomplete provenance or rights review")
		}
	}
	if len(provenance.Evidence) == 0 {
		return ErrInvalid.New("missing rights evidence")
	}
	return nil
}

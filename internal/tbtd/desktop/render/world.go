// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"fmt"
	"strings"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
)

type layer uint8

const (
	layerBackground layer = iota
	layerTerrain
	layerActor
	layerEffects
	layerEssential
)

type drawItem struct {
	layer  layer
	asset  content.PresentationID
	entity string
	state  string
	shape  markerShape
	label  string
	bounds session.WorldRect
	point  session.Point
	path   []session.Point
}

// validateScenery requires explicit placement for every background export.
// An atlas may be referenced more than once for different authored regions.
func validateScenery(background content.PresentationID, exports int, layers []session.SceneryLayer) error {
	if background == "" || exports <= 0 || len(layers) == 0 {
		return content.ErrInvalid.New("%s: missing scenery layout", background)
	}
	covered := make([]bool, exports)
	for _, layer := range layers {
		if layer.Asset == "" || layer.Export < 0 || layer.Depth < 0 || layer.Depth > 2 || !validPoint(session.Point{X: layer.Bounds.X, Y: layer.Bounds.Y}) || !validPoint(session.Point{X: layer.Bounds.Width, Y: layer.Bounds.Height}) || layer.Bounds.Width <= 0 || layer.Bounds.Height <= 0 {
			return content.ErrInvalid.New("%s: invalid scenery placement", layer.Asset)
		}
		if layer.Asset == background {
			if layer.Export >= exports {
				return content.ErrInvalid.New("%s: invalid scenery export", background)
			}
			covered[layer.Export] = true
		}
	}
	for i, found := range covered {
		if !found {
			return content.ErrInvalid.New("%s: missing placement for export %d", background, i)
		}
	}
	return nil
}

// planWorld orders permitted presentation data without consulting or mutating
// the live world. The actual first impact comes from the simulation preview.
func planWorld(ctx context.Context, data session.FrameViewData) (_ []drawItem, err error) {
	defer mon.Task()(&ctx)(&err)
	if data.World.Background == "" {
		return nil, content.ErrInvalid.New("%s: missing encounter background binding", data.World.Encounter)
	}
	items := []drawItem{{layer: layerBackground, asset: data.World.Background}}
	for _, terrain := range data.World.Terrain {
		items = append(items, drawItem{layer: layerTerrain, bounds: terrain})
	}
	for _, entity := range data.Entities {
		var asset content.PresentationID
		switch entity.Kind {
		case "player":
			asset = playerClip(data.Player)
		case "enemy":
			state := strings.ToUpper(entity.State)
			if state == "DEFEATED" {
				state = "DEFEAT"
			}
			asset = content.PresentationID("AN-EN-" + strings.TrimPrefix(string(entity.Asset), "ART-EN-") + "-" + state)
		case "object", "rescue", "boss", "projectile":
			if entity.Asset == "" {
				return nil, content.ErrInvalid.New("%s: missing %s asset binding", entity.ID, entity.Kind)
			}
			asset = entity.Asset
		default:
			// Static objects are part of the authored room package; selection
			// prompts are independently published as essential markers.
			continue
		}
		items = append(items, drawItem{layer: layerActor, asset: asset, entity: entity.ID, state: entity.State, point: interpolate(entity, data.World.Fraction)})
	}
	for _, marker := range data.World.Markers {
		shape := markerFor(marker)
		if shape == "" {
			return nil, content.ErrInvalid.Wrap(fmt.Errorf("%s: unknown world marker %q", marker.ID, marker.Kind))
		}
		items = append(items, drawItem{layer: layerEssential, entity: marker.ID, shape: shape, label: marker.Label, bounds: marker.Bounds, point: session.Point{X: marker.Bounds.X, Y: marker.Bounds.Y}})
	}
	if data.Aim.Visible {
		points := append([]session.Point(nil), data.Aim.Trajectory...)
		items = append(items, drawItem{layer: layerEssential, path: points})
		if data.Aim.HasImpact {
			shape := impactShape(data.Aim.ImpactKind)
			if shape == "" {
				return nil, content.ErrInvalid.New("unknown impact classification %q", data.Aim.ImpactKind)
			}
			items = append(items, drawItem{layer: layerEssential, shape: shape, point: data.Aim.Impact})
		}
	}
	return items, nil
}

// Copyright (C) 2026 crawter
// All rights reserved.

// Package render contains the main-thread world presentation adapter.
package render

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	monkit "github.com/spacemonkeygo/monkit/v3"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/desktop/assets"
	"too-broke-to-die/internal/tbtd/desktop/input"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

var mon = monkit.Package()

// Renderer borrows cache resources; its owner must keep the OS thread locked
// and the cache alive until Close. Draw does not begin or end the window frame.
type Renderer struct {
	cache   *assets.Cache
	thread  assets.ThreadID
	owner   uint64
	camera  Camera
	effects effectPool
	room    content.EncounterID
	closed  bool
}

// New constructs a renderer without allocating native resources.
func New(ctx context.Context, cache *assets.Cache, thread assets.ThreadID) (_ *Renderer, err error) {
	defer mon.Task()(&ctx)(&err)
	if cache == nil || thread == nil || thread() == 0 {
		return nil, content.ErrInvalid.New("missing renderer cache or thread")
	}
	return &Renderer{cache: cache, thread: thread, owner: thread()}, nil
}

// Close drops borrowed references; it never unloads cache-owned handles.
func (renderer *Renderer) Close(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if renderer == nil {
		return nil
	}
	if renderer.thread == nil || renderer.thread() != renderer.owner {
		return assets.ErrThread
	}
	renderer.closed = true
	renderer.effects.reset(ctx)
	renderer.cache = nil
	return nil
}

type sprite struct {
	texture rl.Texture2D
	part    session.VisualPart
	point   session.Point
	facing  int
}

// Draw draws a prepared view into the current raylib frame on the owner thread.
// It resolves every required texture before drawing, and refuses incomplete
// frame/layer mappings rather than treating an atlas as a completed pose.
func (renderer *Renderer) Draw(ctx context.Context, view session.FrameView, width, height int, elapsed time.Duration, access settings.Accessibility) (_ input.Viewport, err error) {
	defer mon.Task()(&ctx)(&err)
	if renderer == nil || renderer.thread == nil || renderer.thread() != renderer.owner {
		return input.Viewport{}, assets.ErrThread
	}
	if renderer.closed {
		return input.Viewport{}, assets.ErrClosed
	}
	if width <= 0 || height <= 0 || width > 16384 || height > 16384 || elapsed < 0 {
		return input.Viewport{}, content.ErrInvalid.New("invalid viewport")
	}
	data := view.Data(ctx)
	items, err := planWorld(ctx, data)
	if err != nil {
		return input.Viewport{}, err
	}
	if data.World.Encounter != renderer.room {
		renderer.camera = Camera{}
		renderer.effects.reset(ctx)
		renderer.room = data.World.Encounter
	}
	if err = renderer.effects.update(ctx, data.World.Tick, data.World.Effects, access); err != nil {
		return input.Viewport{}, err
	}
	var player *session.EntityView
	for i := range data.Entities {
		if data.Entities[i].ID == "player" {
			player = &data.Entities[i]
			break
		}
	}
	if player == nil {
		return input.Viewport{}, content.ErrInvalid.New("world: missing player")
	}
	position := interpolate(*player, data.World.Fraction)
	if !validPoint(position) || !validPoint(data.Player.Velocity) {
		return input.Viewport{}, content.ErrInvalid.New("player: invalid position or velocity")
	}
	if err = renderer.camera.FollowBounds(ctx, position, data.Player.Velocity.X, elapsed, data.World.BossLocked, 0, data.World.Landings); err != nil {
		return input.Viewport{}, err
	}
	viewport := renderer.camera.Viewport(width, height)
	background, err := renderer.cache.Texture(ctx, string(data.World.Background))
	if err != nil {
		return viewport, err
	}
	if err = validateScenery(data.World.Background, len(background), data.World.Scenery); err != nil {
		return viewport, err
	}
	layers := slices.Clone(data.World.Scenery)
	slices.SortStableFunc(layers, func(a, b session.SceneryLayer) int { return a.Depth - b.Depth })
	var scenery []sprite
	for _, layer := range layers {
		textures := background
		if layer.Asset != data.World.Background {
			textures, err = renderer.cache.Texture(ctx, string(layer.Asset))
			if err != nil {
				return viewport, err
			}
		}
		if layer.Export < 0 || layer.Export >= len(textures) {
			return viewport, content.ErrInvalid.New("%s: invalid scenery export", layer.Asset)
		}
		part := session.VisualPart{Asset: layer.Asset, Source: layer.Source, Size: session.Point{X: layer.Bounds.Width, Y: layer.Bounds.Height}}
		texture := textures[layer.Export]
		if err = validatePart(part, texture); err != nil {
			return viewport, err
		}
		scenery = append(scenery, sprite{texture: texture, part: part, point: session.Point{X: layer.Bounds.X, Y: layer.Bounds.Y}})
	}
	var sprites []sprite
	for _, item := range items {
		if item.layer != layerActor {
			continue
		}
		if _, err = renderer.cache.Texture(ctx, string(item.asset)); err != nil {
			return viewport, err
		}
		var entity *session.EntityView
		for i := range data.Entities {
			if data.Entities[i].ID == item.entity {
				entity = &data.Entities[i]
				break
			}
		}
		if entity == nil || len(entity.Parts) == 0 {
			return viewport, content.ErrInvalid.New("%s: missing animation frame and layer handoff", item.asset)
		}
		for _, part := range entity.Parts {
			if part.Animation != item.asset || part.Asset == "" {
				return viewport, content.ErrInvalid.New("%s: invalid animation layer %s", item.asset, part.Slot)
			}
			textures, textureErr := renderer.cache.Texture(ctx, string(part.Asset))
			if textureErr != nil {
				return viewport, textureErr
			}
			if part.Export < 0 || part.Export >= len(textures) {
				return viewport, content.ErrInvalid.New("%s: invalid export index", part.Asset)
			}
			texture := textures[part.Export]
			if err = validatePart(part, texture); err != nil {
				return viewport, err
			}
			sprites = append(sprites, sprite{texture: texture, part: part, point: item.point, facing: entity.Facing})
		}
	}
	var effects []sprite
	for _, effect := range renderer.effects.active {
		part := effect.event.Part
		if part.Asset != effect.event.Asset {
			return viewport, content.ErrInvalid.New("%s: missing effect frame handoff", effect.event.Asset)
		}
		textures, textureErr := renderer.cache.Texture(ctx, string(part.Asset))
		if textureErr != nil {
			return viewport, textureErr
		}
		if part.Export < 0 || part.Export >= len(textures) {
			return viewport, content.ErrInvalid.New("%s: invalid effect export", part.Asset)
		}
		texture := textures[part.Export]
		if err = validatePart(part, texture); err != nil {
			return viewport, err
		}
		effects = append(effects, sprite{texture: texture, part: part, point: effect.event.Position, facing: effect.event.Direction})
	}
	font, err := renderer.cache.Font(ctx, "FONT-NOTO-REGULAR")
	if err != nil {
		return viewport, err
	}
	for _, item := range items {
		if !validItem(item) {
			return viewport, content.ErrInvalid.New("%s: invalid world geometry", item.entity)
		}
	}
	for _, sprite := range scenery {
		rl.SetTextureFilter(sprite.texture, rl.FilterBilinear)
	}
	for _, sprite := range sprites {
		rl.SetTextureFilter(sprite.texture, rl.FilterBilinear)
	}
	for _, sprite := range effects {
		rl.SetTextureFilter(sprite.texture, rl.FilterBilinear)
	}
	rl.BeginScissorMode(int32(viewport.X), int32(viewport.Y), int32(viewport.Width), int32(viewport.Height))
	defer rl.EndScissorMode()
	worldView := viewport
	worldView.Target.X -= renderer.effects.shakeOffset(data.World.Tick, access)
	for _, sprite := range scenery {
		drawSprite(worldView, sprite)
	}
	// Collision edges are guidance, not a replacement for authored terrain.
	// Keep them above scenery and below actors and essential markers.
	for _, item := range items {
		if item.layer == layerTerrain {
			drawTerrainEdge(worldView, item.bounds)
		}
	}
	for _, sprite := range sprites {
		drawSprite(worldView, sprite)
	}
	for _, sprite := range effects {
		drawSprite(worldView, sprite)
	}
	for _, item := range items {
		if item.layer == layerEssential {
			drawMarker(worldView, font, item, access.HighContrast)
		}
	}
	if point, ok := reticlePoint(viewport, data.Aim, position); ok {
		drawReticle(point, float32(viewport.Width/LogicalWidth))
	}
	return viewport, nil
}

func validPoint(point session.Point) bool {
	return !math.IsNaN(point.X) && !math.IsNaN(point.Y) && !math.IsInf(point.X, 0) && !math.IsInf(point.Y, 0) && math.Abs(point.X) <= 1e7 && math.Abs(point.Y) <= 1e7
}

func validatePart(part session.VisualPart, texture rl.Texture2D) error {
	source := part.Source
	if texture.ID == 0 || !validPoint(session.Point{X: source.X, Y: source.Y}) || !validPoint(session.Point{X: source.Width, Y: source.Height}) || !validPoint(part.Offset) || !validPoint(part.Size) || source.X < 0 || source.Y < 0 || source.Width <= 0 || source.Height <= 0 || source.X+source.Width > float64(texture.Width) || source.Y+source.Height > float64(texture.Height) || part.Size.X <= 0 || part.Size.Y <= 0 {
		return content.ErrInvalid.Wrap(fmt.Errorf("%s: invalid layer geometry", part.Asset))
	}
	return nil
}

func validItem(item drawItem) bool {
	if !validPoint(item.point) || !validPoint(session.Point{X: item.bounds.X, Y: item.bounds.Y}) || !validPoint(session.Point{X: item.bounds.Width, Y: item.bounds.Height}) || item.bounds.Width < 0 || item.bounds.Height < 0 {
		return false
	}
	for _, point := range item.path {
		if !validPoint(point) {
			return false
		}
	}
	return true
}

func screenPoint(view input.Viewport, point session.Point) rl.Vector2 {
	scale := view.Width / LogicalWidth
	return rl.Vector2{X: float32(view.X + (point.X-view.Target.X+640)*scale), Y: float32(view.Y + (360-(point.Y-view.Target.Y))*scale)}
}

// terrainEdge projects the authoritative collision top for a readable ledge
// highlight. The underlying terrain artwork remains required independently.
func terrainEdge(view input.Viewport, bounds session.WorldRect) (rl.Vector2, rl.Vector2) {
	left := screenPoint(view, session.Point{X: bounds.X, Y: bounds.Y + bounds.Height})
	right := screenPoint(view, session.Point{X: bounds.X + bounds.Width, Y: bounds.Y + bounds.Height})
	return left, right
}

func drawTerrainEdge(view input.Viewport, bounds session.WorldRect) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	left, right := terrainEdge(view, bounds)
	rl.DrawLineEx(left, right, float32(2*view.Width/LogicalWidth), rl.Color{R: 220, G: 220, B: 220, A: 180})
}

// spriteGeometry mirrors around the entity's bottom-centre anchor, not the
// individual layer. This keeps every socket aligned when facing changes.
func spriteGeometry(view input.Viewport, sprite sprite) (rl.Rectangle, rl.Rectangle) {
	part := sprite.part
	offsetX := part.Offset.X
	sourceWidth := part.Source.Width
	if sprite.facing < 0 {
		offsetX = -offsetX - part.Size.X
		sourceWidth = -sourceWidth
	}
	top := screenPoint(view, session.Point{X: sprite.point.X + offsetX, Y: sprite.point.Y + part.Offset.Y + part.Size.Y})
	scale := float32(view.Width / LogicalWidth)
	source := rl.Rectangle{X: float32(part.Source.X), Y: float32(part.Source.Y), Width: float32(sourceWidth), Height: float32(part.Source.Height)}
	destination := rl.Rectangle{X: top.X, Y: top.Y, Width: float32(part.Size.X) * scale, Height: float32(part.Size.Y) * scale}
	return source, destination
}

func drawSprite(view input.Viewport, sprite sprite) {
	source, destination := spriteGeometry(view, sprite)
	rl.DrawTexturePro(sprite.texture, source, destination, rl.Vector2{}, 0, rl.White)
}

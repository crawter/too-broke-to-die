// Copyright (C) 2026 crawter
// All rights reserved.

package assets

import (
	"context"
	"errors"
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// nativeResources is the external graphics-device boundary, not an interface
// between in-process components. Tests provide it without opening a window.
type nativeResources interface {
	texture([]byte) (rl.Texture2D, error)
	font(string, []byte, []rune) (rl.Font, error)
	target(int32, int32) (rl.RenderTexture2D, error)
	unloadTexture(rl.Texture2D)
	unloadFont(rl.Font)
	unloadTarget(rl.RenderTexture2D)
}

type raylibResources struct{}

func (raylibResources) texture(data []byte) (rl.Texture2D, error) {
	image := rl.LoadImageFromMemory(".png", data, int32(len(data)))
	if image == nil {
		return rl.Texture2D{}, errors.New("image decode failed")
	}
	defer rl.UnloadImage(image)
	if !rl.IsImageValid(image) {
		return rl.Texture2D{}, errors.New("image decode failed")
	}
	texture := rl.LoadTextureFromImage(image)
	if !rl.IsTextureValid(texture) {
		if texture.ID != 0 {
			rl.UnloadTexture(texture)
		}
		return rl.Texture2D{}, errors.New("texture allocation failed")
	}
	return texture, nil
}

// fontFunctions describes raylib's font boundary. Keeping these operations
// together allows acquisition and cleanup to be exercised without a device.
// The caller stays on the locked graphics thread throughout the operation.
type fontFunctions struct {
	defaultFont  func() rl.Font
	load         func(string, []byte, int32, []rune) rl.Font
	validFont    func(rl.Font) bool
	validTexture func(rl.Texture2D) bool
	glyphInfo    func(rl.Font, rune) rl.GlyphInfo
	unload       func(rl.Font)
}

func (raylibResources) font(extension string, data []byte, glyphs []rune) (rl.Font, error) {
	return loadNativeFont(extension, data, glyphs, fontFunctions{
		defaultFont:  rl.GetFontDefault,
		load:         rl.LoadFontFromMemory,
		validFont:    rl.IsFontValid,
		validTexture: rl.IsTextureValid,
		glyphInfo:    rl.GetGlyphInfo,
		unload:       rl.UnloadFont,
	})
}

func loadNativeFont(extension string, data []byte, glyphs []rune, native fontFunctions) (rl.Font, error) {
	// UnloadFont refuses to free any font whose texture ID matches the default
	// font's ID, including zero. Require the window's default font first so a
	// failed atlas upload (ID zero) remains distinguishable and releasable.
	defaultID := native.defaultFont().Texture.ID
	if defaultID == 0 {
		return rl.Font{}, errors.New("default font is not initialized")
	}
	if len(data) == 0 || len(glyphs) == 0 {
		return rl.Font{}, errors.New("font data or glyph inventory is empty")
	}
	font := native.load(extension, data, 32, glyphs)
	if font.Texture.ID == defaultID {
		return rl.Font{}, errors.New("font allocation returned the default font")
	}
	if !native.validFont(font) || !native.validTexture(font.Texture) {
		// A zero texture can still own CPU-side glyph images and rectangles.
		native.unload(font)
		return rl.Font{}, errors.New("font allocation failed")
	}
	for _, codepoint := range glyphs {
		// GetGlyphInfo falls back to '?' (or index zero). Comparing Value is
		// essential: the SFNT cmap check can span tables raylib does not use.
		if native.glyphInfo(font, codepoint).Value != codepoint {
			native.unload(font)
			return rl.Font{}, fmt.Errorf("loaded font missing glyph U+%04X", codepoint)
		}
	}
	return font, nil
}

func (raylibResources) target(width, height int32) (rl.RenderTexture2D, error) {
	target := rl.LoadRenderTexture(width, height)
	if !rl.IsRenderTextureValid(target) {
		if target.ID != 0 {
			rl.UnloadRenderTexture(target)
		}
		return rl.RenderTexture2D{}, errors.New("render target allocation failed")
	}
	return target, nil
}

func (raylibResources) unloadTexture(texture rl.Texture2D) {
	rl.UnloadTexture(texture)
}

func (raylibResources) unloadFont(font rl.Font) {
	rl.UnloadFont(font)
}
func (raylibResources) unloadTarget(target rl.RenderTexture2D) {
	rl.UnloadRenderTexture(target)
}

// ReplaceTarget acquires a render target before retiring its previous version.
// The returned handle is borrowed and must never be unloaded by the caller.
func (cache *Cache) ReplaceTarget(ctx context.Context, id string, width, height int32) (_ rl.RenderTexture2D, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = cache.check(); err != nil {
		return rl.RenderTexture2D{}, err
	}
	if id == "" || width <= 0 || height <= 0 || width > 16384 || height > 16384 {
		return rl.RenderTexture2D{}, errors.New("invalid render target")
	}
	if err = ctx.Err(); err != nil {
		return rl.RenderTexture2D{}, err
	}
	target, err := cache.native.target(width, height)
	if err != nil {
		return rl.RenderTexture2D{}, err
	}
	if old, exists := cache.targets[id]; exists {
		cache.native.unloadTarget(old)
	}
	cache.targets[id] = target
	return target, nil
}

// Copyright (C) 2026 crawter
// All rights reserved.

// Package assets owns presentation resources on the locked initial thread.
package assets

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	rl "github.com/gen2brain/raylib-go/raylib"
	monkit "github.com/spacemonkeygo/monkit/v3"

	"too-broke-to-die/internal/tbtd/content"
)

var mon = monkit.Package()

var (
	// ErrThread means an operation was attempted away from the owning thread.
	ErrThread = errors.New("asset cache called from a different thread")
	// ErrClosed means the cache has already been closed.
	ErrClosed = errors.New("asset cache is closed")
)

// TextureTargetBytes is the texture allocation target, not a measurement.
const TextureTargetBytes = 384 << 20

// ThreadID returns the current OS thread identity. The composition root must
// lock the initial OS thread before New and keep it locked until after Close.
type ThreadID func() uint64

// Cache owns native handles. Borrowed handles are valid only until the next
// successful group or target replacement, or Close; callers must not unload them.
type Cache struct {
	filesystem fs.FS
	threadID   ThreadID
	owner      uint64
	native     nativeResources
	records    []content.ManifestRecord
	glyphs     []rune
	textures   map[string]rl.Texture2D
	bindings   map[string][]string
	fonts      map[string]rl.Font
	targets    map[string]rl.RenderTexture2D
	closed     bool
}

// New validates the authored manifest and acquires the shared set. filesystem
// must confine symlinks to its root (for example os.Root.FS). AdditionalText
// contains any dynamically supplied player-facing strings, such as bindings.
// Thread identity must come from the OS, not from a goroutine identity.
func New(ctx context.Context, filesystem fs.FS, manifest *content.Manifest, threadID ThreadID, additionalText []string) (_ *Cache, err error) {
	defer mon.Task()(&ctx)(&err)

	if filesystem == nil || manifest == nil || threadID == nil || threadID() == 0 {
		return nil, content.ErrInvalid.New("missing asset filesystem, manifest or thread identity")
	}
	if err = content.ValidateManifest(ctx, filesystem, manifest, false); err != nil {
		return nil, err
	}
	glyphs, err := glyphInventory(ctx, filesystem, manifest, additionalText)
	if err != nil {
		return nil, err
	}
	return newCache(ctx, filesystem, manifest.Records, threadID, glyphs, raylibResources{})
}

func newCache(ctx context.Context, filesystem fs.FS, records []content.ManifestRecord, threadID ThreadID, glyphs []rune, native nativeResources) (_ *Cache, err error) {
	defer mon.Task()(&ctx)(&err)

	// Copy the metadata we consume: the validated caller-owned manifest may be
	// reused or discarded after construction, but cannot alter this cache.
	owned := make([]content.ManifestRecord, len(records))
	for i, record := range records {
		owned[i] = record
		if record.File != nil {
			file := *record.File
			owned[i].File = &file
		}
		if record.Font != nil {
			font := *record.Font
			owned[i].Font = &font
		}
		if record.Visual != nil {
			visual := *record.Visual
			owned[i].Visual = &visual
		}
		owned[i].AdditionalExports = append([]string(nil), record.AdditionalExports...)
		owned[i].AdditionalFiles = make([]*content.FileMetadata, len(record.AdditionalFiles))
		for j, file := range record.AdditionalFiles {
			if file != nil {
				fileCopy := *file
				owned[i].AdditionalFiles[j] = &fileCopy
			}
		}
	}
	cache := &Cache{filesystem: filesystem, threadID: threadID, owner: threadID(), native: native, records: owned, glyphs: append([]rune(nil), glyphs...), textures: make(map[string]rl.Texture2D), bindings: make(map[string][]string), fonts: make(map[string]rl.Font), targets: make(map[string]rl.RenderTexture2D)}
	defer func() {
		if err != nil {
			err = errors.Join(err, cache.Close(ctx))
		}
	}()
	if err = cache.loadFonts(ctx); err != nil {
		return nil, err
	}
	if err = cache.replaceGroup(ctx, "shared", false); err != nil {
		return nil, err
	}
	return cache, nil
}

func (cache *Cache) check() error {
	if cache == nil || cache.threadID == nil || cache.threadID() != cache.owner {
		return ErrThread
	}
	if cache.closed {
		return ErrClosed
	}
	return nil
}

// Texture returns all exported textures for an asset ID, in manifest order.
func (cache *Cache) Texture(ctx context.Context, id string) (_ []rl.Texture2D, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = cache.check(); err != nil {
		return nil, err
	}
	paths, ok := cache.bindings[id]
	if !ok {
		return nil, content.ErrInvalid.New("%s: texture not loaded", id)
	}
	result := make([]rl.Texture2D, 0, len(paths))
	for _, path := range paths {
		result = append(result, cache.textures[path])
	}
	return result, nil
}

// Font borrows one Noto Sans face. The regular face also serves recovery UI.
func (cache *Cache) Font(ctx context.Context, id string) (_ rl.Font, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = cache.check(); err != nil {
		return rl.Font{}, err
	}
	font, ok := cache.fonts[id]
	if !ok {
		return rl.Font{}, content.ErrInvalid.New("%s: font not loaded", id)
	}
	return font, nil
}

// Close releases every owned handle exactly once on the owning thread.
func (cache *Cache) Close(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if cache == nil {
		return nil
	}
	if cache.threadID == nil || cache.threadID() != cache.owner {
		return ErrThread
	}
	if cache.closed {
		return nil
	}
	cache.closed = true
	for _, key := range sortedKeys(cache.targets) {
		cache.native.unloadTarget(cache.targets[key])
	}
	for _, key := range sortedKeys(cache.textures) {
		cache.native.unloadTexture(cache.textures[key])
	}
	for _, key := range sortedKeys(cache.fonts) {
		cache.native.unloadFont(cache.fonts[key])
	}
	cache.targets = nil
	cache.textures = nil
	cache.bindings = nil
	cache.fonts = nil
	return nil
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assetFailure(id string, cause error) error {
	return content.ErrInvalid.Wrap(fmt.Errorf("%s: %w", id, cause))
}

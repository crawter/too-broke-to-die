// Copyright (C) 2026 crawter
// All rights reserved.

package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"strings"

	rl "github.com/gen2brain/raylib-go/raylib"

	"too-broke-to-die/internal/tbtd/content"
)

const maxExportBytes = 32 << 20

func readExport(ctx context.Context, filesystem fs.FS, id, path string, metadata *content.FileMetadata) (_ []byte, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if metadata == nil || path == "" || metadata.Path != path || !fs.ValidPath(path) || !strings.HasPrefix(path, "content/") || metadata.Size <= 0 || metadata.Size > maxExportBytes {
		return nil, assetFailure(id, errors.New("missing or invalid export metadata"))
	}
	expected, decodeErr := hex.DecodeString(metadata.SHA256)
	if decodeErr != nil || len(expected) != sha256.Size || metadata.SHA256 != strings.ToLower(metadata.SHA256) {
		return nil, assetFailure(id, errors.New("invalid SHA-256"))
	}
	file, err := filesystem.Open(path)
	if err != nil {
		return nil, assetFailure(id, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxExportBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, assetFailure(id, errors.Join(readErr, closeErr))
	}
	if int64(len(data)) != metadata.Size {
		return nil, assetFailure(id, errors.New("export size mismatch"))
	}
	sum := sha256.Sum256(data)
	if !bytes.Equal(sum[:], expected) {
		return nil, assetFailure(id, errors.New("export SHA-256 mismatch"))
	}
	return data, nil
}

func validatePNG(data []byte) error {
	// PNG IHDR color type 6 is an RGBA export; an RGB, palette or grayscale
	// substitution does not meet the authored atlas contract.
	if len(data) < 26 || data[25] != 6 {
		return errors.New("expected RGBA PNG")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 2048 || config.Height > 2048 {
		return errors.New("invalid PNG or atlas dimensions")
	}
	if _, err = png.Decode(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("invalid PNG: %w", err)
	}
	return nil
}

func (cache *Cache) replaceGroup(ctx context.Context, group string, transitOnly bool) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = cache.check(); err != nil {
		return err
	}
	next := make(map[string]rl.Texture2D)
	bindings := make(map[string][]string)
	acquired := make(map[string]rl.Texture2D)
	defer func() {
		if err != nil {
			for _, key := range sortedKeys(acquired) {
				cache.native.unloadTexture(acquired[key])
			}
		}
	}()
	transitFound := false
	for _, record := range cache.records {
		if record.Kind != "visual" || (record.LoadGroup != "shared" && record.LoadGroup != group) {
			continue
		}
		if transitOnly && record.LoadGroup != "shared" {
			if record.Visual == nil || record.Visual.Kind != "transit" {
				continue
			}
			transitFound = true
		}
		if !record.Final || record.Format != "RGBA PNG" || len(record.AdditionalExports) != len(record.AdditionalFiles) {
			return assetFailure(record.ID, errors.New("unresolved final PNG export"))
		}
		paths := append([]string{record.ExportPath}, record.AdditionalExports...)
		files := append([]*content.FileMetadata{record.File}, record.AdditionalFiles...)
		for i, path := range paths {
			data, readErr := readExport(ctx, cache.filesystem, record.ID, path, files[i])
			if readErr != nil {
				return readErr
			}
			if err = validatePNG(data); err != nil {
				return assetFailure(record.ID, err)
			}
			key := path + "\x00" + files[i].SHA256
			if _, exists := next[key]; !exists {
				texture, exists := cache.textures[key]
				if !exists {
					texture, err = cache.native.texture(data)
					if err != nil {
						return assetFailure(record.ID, err)
					}
					acquired[key] = texture
				}
				next[key] = texture
			}
			bindings[record.ID] = append(bindings[record.ID], key)
		}
	}
	if transitOnly && !transitFound {
		return content.ErrInvalid.New("%s: missing authored transit asset", group)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	for _, key := range sortedKeys(cache.textures) {
		if _, keep := next[key]; !keep {
			cache.native.unloadTexture(cache.textures[key])
		}
	}
	cache.textures, cache.bindings = next, bindings
	return nil
}

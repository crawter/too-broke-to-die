// Copyright (C) 2026 crawter
// All rights reserved.

// Command tbtdcontent generates the source inventory without opening devices.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	monkit "github.com/spacemonkeygo/monkit/v3"

	"too-broke-to-die/internal/tbtd/content"
)

var mon = monkit.Package()

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)

	root, err := os.OpenRoot(".")
	if err != nil {
		return fmt.Errorf("open repository root: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	manifest, err := content.GenerateManifest(ctx, root.FS())
	if err != nil {
		return err
	}
	data, err := content.ManifestBytes(ctx, manifest)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("content", ".manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}
	name := file.Name()
	defer func() {
		if removeErr := os.Remove(name); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	_, writeErr := file.Write(data)
	modeErr := file.Chmod(0644)
	closeErr := file.Close()
	if writeErr != nil || modeErr != nil || closeErr != nil {
		return fmt.Errorf("write manifest: %w", errors.Join(writeErr, modeErr, closeErr))
	}
	if err = os.Rename(name, filepath.Join("content", "manifest.json")); err != nil {
		return fmt.Errorf("install manifest: %w", err)
	}
	missing, notFinal, rightsOpen := 0, 0, 0
	for _, record := range manifest.Records {
		if record.File == nil {
			missing++
		}
		if !record.Final {
			notFinal++
		}
		if record.Provenance == nil || record.Provenance.ReviewStatus != "reviewed" || len(record.Provenance.Evidence) == 0 {
			rightsOpen++
		}
	}
	_, err = fmt.Fprintf(os.Stderr, "manifest: %d records; %d exports missing, %d final statuses unresolved, %d rights reviews open; manual qualification not run\n", len(manifest.Records), missing, notFinal, rightsOpen)
	return err
}

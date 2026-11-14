// Copyright (C) 2026 crawter
// All rights reserved.

package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/zeebo/errs"
)

const maxExportBytes = 1024 * 1024

var exportNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}\.json$`)

// ExportDescription describes exactly what a player-requested export contains.
// It can be shown before calling Export. No files are created by this method.
func (recorder *Recorder) ExportDescription() string {
	return "One local JSON file: product version and content digest, recent redacted operational events, bounded run aggregates, anonymized monkit statistics and recent task traces. No save files, dialogue, user-folder contents or automatic transmission. Earlier log files are not included."
}

type exportReport struct {
	Format                int            `json:"format"`
	ProductVersion        string         `json:"product_version"`
	ContentDigest         string         `json:"content_digest"`
	DurationBucketUpperNS []int64        `json:"duration_bucket_upper_ns"`
	Events                []record       `json:"events"`
	Aggregates            []aggregate    `json:"aggregates"`
	Registry              []registryStat `json:"monkit"`
	Traces                []traceRecord  `json:"traces"`
}

// Export creates a new, bounded report only on an explicit player request.
// name is a simple .json basename under the profile diagnostics directory.
// Existing files are never overwritten. No path or file contents are imported.
func (recorder *Recorder) Export(ctx context.Context, name string) (err error) {
	defer mon.Task()(&ctx)(&err)
	if !exportNamePattern.MatchString(name) || !filepath.IsLocal(name) {
		return ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	stats := collectRegistry(ctx, recorder.registry)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.closed {
		return ErrClosed
	}
	recorder.traceMu.Lock()
	traces := append([]traceRecord(nil), recorder.traces...)
	recorder.traceMu.Unlock()
	report := exportReport{Format: 1, ProductVersion: recorder.config.ProductVersion, ContentDigest: recorder.config.ContentDigest, Events: append([]record(nil), recorder.events...), Registry: stats, Traces: traces}
	for _, bound := range durationBounds {
		report.DurationBucketUpperNS = append(report.DurationBucketUpperNS, int64(bound))
	}
	keys := make([]string, 0, len(recorder.series))
	for key := range recorder.series {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		report.Aggregates = append(report.Aggregates, *recorder.series[key])
	}
	data, err := json.Marshal(report)
	if err != nil {
		return Error.Wrap(err)
	}
	if len(data)+1 > maxExportBytes {
		return ErrLimit
	}
	data = append(data, '\n')
	file, err := recorder.openExport(name)
	if err != nil {
		return Error.Wrap(err)
	}
	err = writeExport(ctx, file, data)
	if err != nil {
		return Error.Wrap(errs.Combine(err, recorder.exports.Remove(name)))
	}
	return nil
}

// writeExport closes even on write or sync failure, preserving all causes.
func writeExport(ctx context.Context, file logFile, data []byte) (err error) {
	defer mon.Task()(&ctx)(&err)
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errs.Combine(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write diagnostic export: %w", err)
	}
	return nil
}

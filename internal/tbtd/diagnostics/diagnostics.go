// Copyright (C) 2026 crawter
// All rights reserved.

// Package diagnostics owns bounded, offline operational records. It accepts
// identities, not arbitrary messages, error strings, paths, or save data.
package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	monkit "github.com/spacemonkeygo/monkit/v3"
	"github.com/zeebo/errs"
)

var mon = monkit.Package()

var (
	// Error identifies an operational diagnostics failure.
	Error = errs.Class("diagnostics")
	// ErrClosed indicates that the recorder is no longer accepting work.
	ErrClosed = errors.New("diagnostics closed")
	// ErrInvalid indicates an invalid configuration or record.
	ErrInvalid = errors.New("invalid diagnostic input")
	// ErrLimit indicates a bounded record or collection cannot be accepted.
	ErrLimit = errors.New("diagnostic limit reached")
)

const (
	defaultLogSize = 5 * 1024 * 1024
	maxSeries      = 256
	maxRegistry    = 512
	maxTraces      = 128
)

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+(?:-[A-Za-z0-9_.:-]+)*$`)
var digestPattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

// Config contains only local limits and build identity. Zero log size and file
// count select 5 MiB and three files, including the active file. Sensitive
// must include the local account name and may include other tokens to redact;
// never pass secrets as diagnostic fields. Diagnostic mode increases only fixed
// in-memory bounds.
// The caller creates the trusted profile root and retains ownership of it.
type Config struct {
	ProductVersion string
	ContentDigest  string
	LogSize        int64
	LogFiles       int
	Diagnostic     bool
	Sensitive      []string
}

// Dimensions contains only stable identities and numeric metadata, never
// arbitrary text. Unreadable revision/schema/total must be nil, not guessed.
// Path-like, non-ASCII, and explicitly sensitive tokens are redacted.
type Dimensions struct {
	Location          string  `json:"location,omitempty"`
	Encounter         string  `json:"encounter,omitempty"`
	Entity            string  `json:"entity,omitempty"`
	Content           string  `json:"content_id,omitempty"`
	Asset             string  `json:"asset_id,omitempty"`
	Campaign          string  `json:"campaign,omitempty"`
	Operation         string  `json:"operation,omitempty"`
	OperationSequence *uint64 `json:"operation_sequence,omitempty"`
	Owner             string  `json:"owner,omitempty"`
	Stage             string  `json:"stage,omitempty"`
	State             string  `json:"state,omitempty"`
	Class             string  `json:"error_class,omitempty"`
	Revision          *uint64 `json:"revision,omitempty"`
	Schema            *uint32 `json:"schema,omitempty"`
	SafeTotal         *int64  `json:"safe_total,omitempty"`
}

type record struct {
	Type           string     `json:"type"`
	Time           time.Time  `json:"time"`
	ProductVersion string     `json:"product_version"`
	ContentDigest  string     `json:"content_digest"`
	Name           string     `json:"name,omitempty"`
	Dimensions     Dimensions `json:"dimensions"`
	ValueNS        int64      `json:"value_ns,omitempty"`
}

// Recorder serializes file operations and collection. Its root handles and file
// are owned by it; the supplied profile root and registry are borrowed.
type Recorder struct {
	mu         sync.Mutex
	config     Config
	registry   *monkit.Registry
	logs       *os.Root
	exports    *os.Root
	openExport func(string) (logFile, error)
	file       logFile
	size       int64
	series     map[string]*aggregate
	events     []record
	cancel     func()
	closed     bool
	closeErr   error
	writeErr   error
	syncErr    error

	// Monkit callbacks can run synchronously inside file operations guarded by
	// mu. They use only traceMu, always acquired after mu when both are needed.
	traceMu     sync.Mutex
	traces      []traceRecord
	watching    map[*traceObserver]struct{}
	traceClosed bool
}

// New constructs a recorder with an explicitly supplied process registry and
// trusted, already-open profile root. No goroutine or network service is started.
func New(ctx context.Context, config Config, registry *monkit.Registry, root *os.Root) (_ *Recorder, err error) {
	defer mon.Task()(&ctx)(&err)
	if registry == nil || root == nil || !tokenPattern.MatchString(config.ProductVersion) || len(config.ProductVersion) > 64 || !digestPattern.MatchString(config.ContentDigest) {
		return nil, ErrInvalid
	}
	if config.LogSize == 0 {
		config.LogSize = defaultLogSize
	}
	if config.LogFiles == 0 {
		config.LogFiles = 3
	}
	if config.LogSize < 512 || config.LogSize > defaultLogSize || config.LogFiles < 1 || config.LogFiles > 10 || len(config.Sensitive) == 0 || len(config.Sensitive) > 32 {
		return nil, ErrInvalid
	}
	for _, term := range config.Sensitive {
		if len(term) == 0 || len(term) > 128 {
			return nil, ErrInvalid
		}
	}
	config.Sensitive = append([]string(nil), config.Sensitive...)
	config.ProductVersion = redact(config.ProductVersion, config.Sensitive)
	config.ContentDigest = strings.ToLower(config.ContentDigest)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	for _, name := range []string{"logs", "diagnostics"} {
		if err = root.MkdirAll(name, 0700); err != nil {
			return nil, Error.Wrap(err)
		}
		info, statErr := root.Lstat(name)
		if statErr != nil {
			return nil, Error.Wrap(statErr)
		}
		if !info.IsDir() {
			return nil, Error.New("diagnostic directory is not a directory")
		}
	}
	recorder := &Recorder{config: config, registry: registry, series: make(map[string]*aggregate), watching: make(map[*traceObserver]struct{})}
	defer func() {
		if err != nil {
			err = errs.Combine(err, recorder.release(ctx))
		}
	}()
	if recorder.logs, err = root.OpenRoot("logs"); err != nil {
		return nil, Error.Wrap(err)
	}
	if recorder.exports, err = root.OpenRoot("diagnostics"); err != nil {
		return nil, Error.Wrap(err)
	}
	if err = recorder.openLog(ctx); err != nil {
		return nil, Error.Wrap(err)
	}
	recorder.openExport = func(name string) (logFile, error) {
		return recorder.exports.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}
	recorder.cancel = registry.ObserveTraces(recorder.watchTrace)
	return recorder, nil
}

func redact(value string, sensitive []string) string {
	if value == "" {
		return ""
	}
	if len(value) > 128 || !tokenPattern.MatchString(value) {
		return "[redacted]"
	}
	lower := strings.ToLower(value)
	for _, term := range sensitive {
		if strings.Contains(lower, strings.ToLower(term)) {
			return "[redacted]"
		}
	}
	return value
}

func (recorder *Recorder) cleanDimensions(d Dimensions) Dimensions {
	fields := []*string{&d.Location, &d.Encounter, &d.Entity, &d.Content, &d.Asset, &d.Campaign, &d.Operation, &d.Owner, &d.Stage, &d.State, &d.Class}
	for _, field := range fields {
		*field = redact(*field, recorder.config.Sensitive)
	}
	// Do not retain caller-owned pointers.
	if d.OperationSequence != nil {
		value := *d.OperationSequence
		d.OperationSequence = &value
	}
	if d.Revision != nil {
		value := *d.Revision
		d.Revision = &value
	}
	if d.Schema != nil {
		value := *d.Schema
		d.Schema = &value
	}
	if d.SafeTotal != nil {
		value := *d.SafeTotal
		d.SafeTotal = &value
	}
	return d
}

var eventNames = map[string]bool{
	"save_committed": true, "save_failed": true, "save_recovered_previous": true,
	"save_corrupt": true, "save_too_new": true, "content_validation_failed": true,
	"asset_load_failed": true, "audio_unavailable": true, "graphics_unavailable": true,
	"worker_unexpected_exit": true, "shutdown_deadline": true,
}

// Event writes one operational event. Callers report an unexpected failure at
// its owning boundary once; expected gameplay rejections are not events.
func (recorder *Recorder) Event(ctx context.Context, name string, dimensions Dimensions) (err error) {
	defer mon.Task()(&ctx)(&err)
	if !eventNames[name] {
		return ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.closed {
		return ErrClosed
	}
	rec := record{Type: "event", Time: time.Now().UTC(), ProductVersion: recorder.config.ProductVersion, ContentDigest: recorder.config.ContentDigest, Name: name, Dimensions: recorder.cleanDimensions(dimensions)}
	data, err := json.Marshal(rec)
	if err != nil {
		return Error.Wrap(err)
	}
	// An oversized input is not a filesystem failure and must not poison
	// subsequent, smaller records.
	if int64(len(data))+1 > recorder.config.LogSize {
		return ErrLimit
	}
	if err = recorder.appendLog(ctx, data); err != nil {
		// The first write failure poisons subsequent writes. Do not combine
		// that same retained failure with itself on every later event.
		if recorder.writeErr == nil {
			recorder.writeErr = err
		}
		return Error.Wrap(err)
	}
	limit := 64
	if recorder.config.Diagnostic {
		limit = 256
	}
	if len(recorder.events) == limit {
		copy(recorder.events, recorder.events[1:])
		recorder.events = recorder.events[:limit-1]
	}
	recorder.events = append(recorder.events, rec)
	return nil
}

// Close flushes final aggregates and registry statistics, detaches trace
// observation and releases owned handles once. Concurrent calls get the same
// result, including earlier write errors. It is safe before a program runs.
func (recorder *Recorder) Close() error { return recorder.close(context.Background()) }

func (recorder *Recorder) close(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	// Snapshot registry without holding mu: registered gauges are external code.
	stats := collectRegistry(ctx, recorder.registry)
	recorder.mu.Lock()
	if recorder.closed {
		err = recorder.closeErr
		recorder.mu.Unlock()
		return err
	}
	recorder.closed = true
	cancel := recorder.cancel
	recorder.cancel = nil
	recorder.traceMu.Lock()
	recorder.traceClosed = true
	watchers := make([]*traceObserver, 0, len(recorder.watching))
	for watcher := range recorder.watching {
		watchers = append(watchers, watcher)
	}
	recorder.traceMu.Unlock()
	flushErr := recorder.flushLocked(ctx, stats)
	recorder.closeErr = errs.Combine(flushErr, recorder.release(ctx))
	err = recorder.closeErr
	recorder.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, watcher := range watchers {
		watcher.cancel()
	}
	return err
}

func (recorder *Recorder) release(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	var failures []error
	if recorder.file != nil {
		failures = append(failures, recorder.file.Close())
		recorder.file = nil
	}
	if recorder.logs != nil {
		failures = append(failures, recorder.logs.Close())
		recorder.logs = nil
	}
	if recorder.exports != nil {
		failures = append(failures, recorder.exports.Close())
		recorder.exports = nil
	}
	return errs.Combine(failures...)
}

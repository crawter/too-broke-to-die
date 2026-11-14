// Copyright (C) 2026 crawter
// All rights reserved.

package diagnostics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"time"

	monkit "github.com/spacemonkeygo/monkit/v3"
	"github.com/zeebo/errs"
)

// Measurements are fixed-name, fixed-bucket observations. They never produce
// a disk write per tick or actor. Duration values are nonnegative nanoseconds.
const (
	FrameDuration            = "frame_duration"
	SimulationDuration       = "simulation_duration"
	DrawDuration             = "draw_duration"
	SimulationBacklogDropped = "simulation_backlog_dropped"
	SaveDuration             = "save_duration"
	CollisionIterationLimit  = "collision_iteration_limit"
)

var durationBounds = [...]time.Duration{time.Millisecond, 4 * time.Millisecond, 8 * time.Millisecond, 16 * time.Millisecond, 17 * time.Millisecond, 33 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond, 250 * time.Millisecond, time.Second, 5 * time.Second}

type aggregate struct {
	Name       string     `json:"name"`
	Dimensions Dimensions `json:"dimensions"`
	Count      uint64     `json:"count"`
	SumNS      uint64     `json:"sum_ns"`
	// Buckets are disjoint, with the last bucket above the last bound.
	Buckets [len(durationBounds) + 1]uint64 `json:"buckets"`
}

type metricRecord struct {
	Type           string        `json:"type"`
	ProductVersion string        `json:"product_version"`
	ContentDigest  string        `json:"content_digest"`
	Aggregate      *aggregate    `json:"aggregate,omitempty"`
	Registry       *registryStat `json:"registry,omitempty"`
	Trace          *traceRecord  `json:"trace,omitempty"`
}

// Observe adds one duration or collision-limit occurrence. For a collision
// occurrence pass zero; dropped backlog uses the discarded wall duration.
// The number of distinct dimension combinations is capped for the whole run.
func (recorder *Recorder) Observe(ctx context.Context, name string, dimensions Dimensions, duration time.Duration) (err error) {
	// Per-frame and per-actor samples are measured by their enclosing tasks.
	switch name {
	case FrameDuration, SimulationDuration, DrawDuration, SimulationBacklogDropped, SaveDuration:
		if duration < 0 {
			return ErrInvalid
		}
	case CollisionIterationLimit:
		if duration != 0 {
			return ErrInvalid
		}
	default:
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
	dimensions = recorder.cleanDimensions(dimensions)
	keyBytes, err := json.Marshal(struct {
		Name       string
		Dimensions Dimensions
	}{name, dimensions})
	if err != nil {
		return Error.Wrap(err)
	}
	key := string(keyBytes)
	entry := recorder.series[key]
	if entry == nil {
		if len(recorder.series) == maxSeries {
			return ErrLimit
		}
		entry = &aggregate{Name: name, Dimensions: dimensions}
		recorder.series[key] = entry
	}
	if entry.Count == math.MaxUint64 || math.MaxUint64-entry.SumNS < uint64(duration) {
		return ErrLimit
	}
	bucket := len(durationBounds)
	for i, bound := range durationBounds {
		if duration <= bound {
			bucket = i
			break
		}
	}
	entry.Count++
	entry.SumNS += uint64(duration)
	entry.Buckets[bucket]++
	return nil
}

type registryStat struct {
	// Identifiers are one-way digests: monkit sources, tags, error names and
	// fields may contain arbitrary external text and must not be emitted.
	ID    string  `json:"id"`
	Value float64 `json:"value"`
}

func opaqueID(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// collectRegistry takes a bounded, deterministic sample of the entire injected
// registry. Error-name series are excluded, as they may contain private data;
// monkit's aggregate error counts remain. No span arguments or annotations are read.
func collectRegistry(ctx context.Context, registry *monkit.Registry) []registryStat {
	defer mon.Task()(&ctx)(nil)
	stats := make(map[string]float64)
	registry.Stats(func(key monkit.SeriesKey, field string, value float64) {
		if math.IsNaN(value) || math.IsInf(value, 0) || (key.Tags != nil && key.Tags.Get("error_name") != "") {
			return
		}
		id := opaqueID(key.String() + "\x00" + field)
		// Keep the lexically smallest IDs so source iteration order cannot affect
		// the bounded sample. Never retain the original arbitrary string.
		if len(stats) < maxRegistry {
			stats[id] = value
			return
		}
		if _, ok := stats[id]; ok {
			stats[id] = value
			return
		}
		largest := ""
		for existing := range stats {
			if existing > largest {
				largest = existing
			}
		}
		if id < largest {
			delete(stats, largest)
			stats[id] = value
		}
	})
	keys := make([]string, 0, len(stats))
	for key := range stats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]registryStat, 0, len(keys))
	for _, key := range keys {
		out = append(out, registryStat{ID: key, Value: stats[key]})
	}
	return out
}

type traceRecord struct {
	TraceID    int64  `json:"trace_id"`
	SpanID     int64  `json:"span_id"`
	ParentID   int64  `json:"parent_id,omitempty"`
	FunctionID string `json:"function_id"`
	DurationNS int64  `json:"duration_ns"`
	Failed     bool   `json:"failed,omitempty"`
	Panicked   bool   `json:"panicked,omitempty"`
}

type traceObserver struct {
	recorder *Recorder
	cancel   func()
	rootID   int64
}

// The registry calls this synchronously when a trace is created. At most 64
// live traces are observed; each root completion detaches its observer.
func (recorder *Recorder) watchTrace(trace *monkit.Trace) {
	recorder.traceMu.Lock()
	defer recorder.traceMu.Unlock()
	if recorder.traceClosed || len(recorder.watching) >= 64 {
		return
	}
	observer := &traceObserver{recorder: recorder}
	observer.cancel = trace.ObserveSpans(observer)
	recorder.watching[observer] = struct{}{}
}

func (observer *traceObserver) Start(span *monkit.Span) {
	observer.recorder.traceMu.Lock()
	if observer.rootID == 0 {
		observer.rootID = span.Id()
	}
	observer.recorder.traceMu.Unlock()
}

func (observer *traceObserver) Finish(span *monkit.Span, failure error, panicked bool, finish time.Time) {
	recorder := observer.recorder
	parent, _ := span.ParentId()
	rec := traceRecord{TraceID: span.Trace().Id(), SpanID: span.Id(), ParentID: parent, FunctionID: opaqueID(span.Func().FullName()), DurationNS: finish.Sub(span.Start()).Nanoseconds(), Failed: failure != nil, Panicked: panicked}
	if rec.DurationNS < 0 {
		rec.DurationNS = 0
	}
	recorder.traceMu.Lock()
	if !recorder.traceClosed {
		limit := 16
		if recorder.config.Diagnostic {
			limit = maxTraces
		}
		if len(recorder.traces) == limit {
			copy(recorder.traces, recorder.traces[1:])
			recorder.traces = recorder.traces[:limit-1]
		}
		recorder.traces = append(recorder.traces, rec)
	}
	rootFinished := span.Id() == observer.rootID
	if rootFinished {
		delete(recorder.watching, observer)
	}
	recorder.traceMu.Unlock()
	if rootFinished {
		observer.cancel()
	}
}

// Flush writes bounded cumulative aggregates and a registry snapshot, and
// synchronizes the active log. It can be called without closing the recorder.
func (recorder *Recorder) Flush(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return err
	}
	stats := collectRegistry(ctx, recorder.registry)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.closed {
		return ErrClosed
	}
	return recorder.flushLocked(ctx, stats)
}

// flushLocked retains only the first write and sync failures. Each call also
// reports its own sync failure, so later failures are visible without growing a
// recursively combined error tree. Close reports the first and final failures.
func (recorder *Recorder) flushLocked(ctx context.Context, stats []registryStat) (err error) {
	defer mon.Task()(&ctx)(&err)
	recorder.traceMu.Lock()
	traces := append([]traceRecord(nil), recorder.traces...)
	recorder.traceMu.Unlock()
	write := func(item metricRecord) bool {
		item.ProductVersion = recorder.config.ProductVersion
		item.ContentDigest = recorder.config.ContentDigest
		data, writeErr := json.Marshal(item)
		if writeErr == nil {
			writeErr = recorder.appendLog(ctx, data)
		}
		if writeErr != nil {
			recorder.writeErr = writeErr
			return false
		}
		return true
	}
	if recorder.writeErr == nil {
		keys := make([]string, 0, len(recorder.series))
		for key := range recorder.series {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		ok := true
		for _, key := range keys {
			if !write(metricRecord{Type: "aggregate", Aggregate: recorder.series[key]}) {
				ok = false
				break
			}
		}
		if ok {
			for i := range stats {
				if !write(metricRecord{Type: "monkit", Registry: &stats[i]}) {
					ok = false
					break
				}
			}
		}
		if ok {
			for i := range traces {
				if !write(metricRecord{Type: "trace", Trace: &traces[i]}) {
					break
				}
			}
		}
	}
	var syncErr error
	if recorder.file != nil {
		syncErr = recorder.file.Sync()
		if recorder.syncErr == nil {
			recorder.syncErr = syncErr
			syncErr = nil
		}
	}
	return Error.Wrap(errs.Combine(recorder.writeErr, recorder.syncErr, syncErr))
}

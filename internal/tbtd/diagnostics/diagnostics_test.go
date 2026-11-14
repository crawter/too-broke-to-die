// Copyright (C) 2026 crawter
// All rights reserved.

package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	monkit "github.com/spacemonkeygo/monkit/v3"
)

func newTestRecorder(t *testing.T, size int64, files int, diagnostic bool) (*Recorder, *monkit.Registry, string) {
	t.Helper()
	path := t.TempDir()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	registry := monkit.NewRegistry()
	recorder, err := New(t.Context(), Config{ProductVersion: "1.0.0", ContentDigest: strings.Repeat("a", 64), LogSize: size, LogFiles: files, Diagnostic: diagnostic, Sensitive: []string{"Alice"}}, registry, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := recorder.Close(); err != nil {
			t.Error(err)
		}
	})
	return recorder, registry, path
}

func TestCollectionExportAndPrivacy(t *testing.T) {
	recorder, registry, path := newTestRecorder(t, 0, 0, true)
	ctx := t.Context()
	if !strings.Contains(recorder.ExportDescription(), "No save") {
		t.Fatal("description did not explain exclusions")
	}
	files, err := os.ReadDir(filepath.Join(path, "diagnostics"))
	if err != nil || len(files) != 0 {
		t.Fatalf("created export without request: %v %v", files, err)
	}
	revision, sequence, schema, total := uint64(7), uint64(12), uint32(1), int64(42)
	dims := Dimensions{Location: "LOC-HOS", Encounter: "ENC-HOS-01", Entity: "player", Revision: &revision, Schema: &schema, SafeTotal: &total, Operation: "CERT-01", OperationSequence: &sequence, Class: "SaveCorrupt"}
	for i := 0; i < 3; i++ {
		if err := recorder.Observe(ctx, FrameDuration, dims, 16*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if err := recorder.Observe(ctx, SimulationBacklogDropped, dims, 8*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Observe(ctx, CollisionIterationLimit, dims, 0); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(ctx, "save_recovered_previous", dims); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(ctx, "asset_load_failed", Dimensions{Asset: "/home/Alice/secret.txt", Class: `C:\Users\Alice\secret`, Owner: "Alice", Operation: "пароль"}); err != nil {
		t.Fatal(err)
	}
	// Copies, not pointers to caller-owned mutable values.
	revision = 999
	sequence = 999
	total = 999
	registry.ScopeNamed("test").Counter("test_counter").Inc(4)
	operation := registry.ScopeNamed("test").FuncNamed("operation")
	taskCtx := context.Background()
	stop := operation.Task(&taskCtx, "secret dialogue /home/Alice")
	stop(nil)
	if err := recorder.Export(ctx, "report.json"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(path, "diagnostics", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Alice", "/home", `C:\\Users`, "пароль", "secret", "dialogue"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("private text %q in report: %s", forbidden, data)
		}
	}
	var report exportReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Registry) == 0 || len(report.Traces) == 0 || len(report.Events) != 2 || len(report.Aggregates) != 3 {
		t.Fatalf("incomplete report: %+v", report)
	}
	counterKey := monkit.NewSeriesKey("test_counter").WithTag("scope", "test")
	counterID := opaqueID(counterKey.String() + "\x00value")
	foundCounter := false
	for _, stat := range report.Registry {
		if stat.ID == counterID && stat.Value == 4 {
			foundCounter = true
		}
	}
	if !foundCounter || report.Traces[len(report.Traces)-1].FunctionID != opaqueID(operation.FullName()) {
		t.Fatal("missing actual registry counter or task trace")
	}
	if *report.Events[0].Dimensions.Revision != 7 || *report.Events[0].Dimensions.OperationSequence != 12 || *report.Events[0].Dimensions.SafeTotal != 42 || report.Events[1].Dimensions.Asset != "[redacted]" {
		t.Fatalf("dimensions or redaction: %+v", report.Events)
	}
	found := false
	for _, metric := range report.Aggregates {
		if metric.Name == FrameDuration {
			found = metric.Count == 3 && metric.SumNS == uint64(48*time.Millisecond) && metric.Buckets[3] == 3 && metric.Dimensions.Encounter == "ENC-HOS-01"
		}
	}
	if !found {
		t.Fatal("frame histogram or dimensions incorrect")
	}
	if err := recorder.Export(ctx, "report.json"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrote export: %v", err)
	}
	if err := recorder.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(path, "logs", "current.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "Alice") || strings.Contains(string(log), "/home") || !strings.Contains(string(log), "save_recovered_previous") || !strings.Contains(string(log), `"type":"monkit"`) {
		t.Fatalf("log: %s", log)
	}
}

func TestLimitsAndRotation(t *testing.T) {
	recorder, _, path := newTestRecorder(t, 512, 3, false)
	ctx := t.Context()
	if err := recorder.Observe(ctx, FrameDuration, Dimensions{}, -1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative: %v", err)
	}
	if err := recorder.Event(ctx, "dialogue", Dimensions{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown event: %v", err)
	}
	large := strings.Repeat("x", 128)
	if err := recorder.Event(ctx, "save_failed", Dimensions{Location: large, Encounter: large, Entity: large, Asset: large}); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversized event: %v", err)
	}
	for i := 0; i < 30; i++ {
		if err := recorder.Event(ctx, "save_failed", Dimensions{Operation: fmt.Sprintf("op-%d", i), Class: "StorageFull"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(path, "logs"))
	if err != nil || len(entries) != 3 {
		t.Fatalf("retention: %v %v", entries, err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.Size() > 512 {
			t.Fatalf("size: %v %v", info, err)
		}
	}
	recorder.mu.Lock()
	err = recorder.appendLog(ctx, make([]byte, 512))
	recorder.mu.Unlock()
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("oversized record: %v", err)
	}
	for i := 0; i < maxSeries; i++ {
		if err := recorder.Observe(ctx, DrawDuration, Dimensions{Encounter: fmt.Sprintf("ENC-%d", i)}, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := recorder.Observe(ctx, DrawDuration, Dimensions{Encounter: "overflow"}, 0); !errors.Is(err, ErrLimit) {
		t.Fatalf("series bound: %v", err)
	}
}

func TestExactRotationBoundaryAndBoundedDetail(t *testing.T) {
	recorder, registry, path := newTestRecorder(t, 512, 3, true)
	recorder.mu.Lock()
	err := recorder.appendLog(t.Context(), make([]byte, 511))
	if err == nil {
		err = recorder.appendLog(t.Context(), []byte("x"))
	}
	recorder.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int64{"previous-1.jsonl": 512, "current.jsonl": 2} {
		info, err := os.Stat(filepath.Join(path, "logs", name))
		if err != nil || info.Size() != size {
			t.Fatalf("%s: %v %v", name, info, err)
		}
	}
	for i := 0; i < 270; i++ {
		if err := recorder.Event(t.Context(), "save_failed", Dimensions{}); err != nil {
			t.Fatal(err)
		}
	}
	operation := registry.ScopeNamed("test").FuncNamed("trace")
	for i := 0; i < 150; i++ {
		ctx := context.Background()
		stop := operation.Task(&ctx)
		stop(nil)
	}
	recorder.mu.Lock()
	events := len(recorder.events)
	recorder.mu.Unlock()
	recorder.traceMu.Lock()
	traces, active := len(recorder.traces), len(recorder.watching)
	recorder.traceMu.Unlock()
	if events != 256 || traces != maxTraces || active != 0 {
		t.Fatalf("detail bounds: events=%d traces=%d active=%d", events, traces, active)
	}
}

func TestRetentionReductionOnReopen(t *testing.T) {
	path := t.TempDir()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	config := Config{ProductVersion: "1", ContentDigest: strings.Repeat("e", 64), LogSize: 512, LogFiles: 5, Sensitive: []string{"Alice"}}
	recorder, err := New(t.Context(), config, monkit.NewRegistry(), root)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := recorder.Event(t.Context(), "save_failed", Dimensions{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	config.LogFiles = 3
	recorder, err = New(t.Context(), config, monkit.NewRegistry(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(path, "logs"))
	if err != nil || len(entries) > 3 {
		t.Fatalf("retention after reopening: %v %v", entries, err)
	}
}

func TestPathContainmentAndInitialization(t *testing.T) {
	path, outside := t.TempDir(), t.TempDir()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	config := Config{ProductVersion: "1", ContentDigest: strings.Repeat("b", 64), Sensitive: []string{"Alice"}}
	if _, err := New(t.Context(), config, nil, root); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil registry: %v", err)
	}
	if _, err := New(t.Context(), Config{}, monkit.NewRegistry(), root); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad config: %v", err)
	}
	missingUsername := config
	missingUsername.Sensitive = nil
	if _, err := New(t.Context(), missingUsername, monkit.NewRegistry(), root); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing username: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(path, "logs")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(t.Context(), config, monkit.NewRegistry(), root); err == nil {
		t.Fatal("accepted external symlink")
	}
	if err := os.Remove(filepath.Join(path, "logs")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, "logs"), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(path, "logs", "current.jsonl")
	if err := os.Symlink(filepath.Join(outside, "victim"), link); err != nil {
		t.Fatal(err)
	}
	// This fails after both child directory handles have been acquired.
	if _, err := New(t.Context(), config, monkit.NewRegistry(), root); err == nil {
		t.Fatal("accepted a symlinked log")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	recorder, err := New(t.Context(), config, monkit.NewRegistry(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	for _, name := range []string{"../outside.json", "/tmp/outside.json", `..\outside.json`, "nested/x.json", "bad.txt"} {
		if err := recorder.Export(t.Context(), name); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted %q: %v", name, err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "victim"), filepath.Join(path, "diagnostics", "link.json")); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Export(t.Context(), "link.json"); err == nil {
		t.Fatal("followed export symlink")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("wrote outside: %v %v", entries, err)
	}
}

type failedFile struct {
	logFile
	writeErr error
	syncErr  error
	closeErr error
}

func (file *failedFile) Write(data []byte) (int, error) {
	if file.writeErr != nil {
		return 0, file.writeErr
	}
	return file.logFile.Write(data)
}
func (file *failedFile) Sync() error  { return errors.Join(file.syncErr, file.logFile.Sync()) }
func (file *failedFile) Close() error { return errors.Join(file.closeErr, file.logFile.Close()) }

func TestWriteFlushAndCleanupFailures(t *testing.T) {
	writeFailure := errors.New("write failed")
	syncFailure := errors.New("sync failed")
	closeFailure := errors.New("close failed")
	path := t.TempDir()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	recorder, err := New(t.Context(), Config{ProductVersion: "1", ContentDigest: strings.Repeat("c", 64), Sensitive: []string{"Alice"}}, monkit.NewRegistry(), root)
	if err != nil {
		t.Fatal(err)
	}
	recorder.file = &failedFile{logFile: recorder.file, writeErr: writeFailure, syncErr: syncFailure, closeErr: closeFailure}
	for i := 0; i < 128; i++ {
		eventErr := recorder.Event(t.Context(), "save_failed", Dimensions{})
		if !errors.Is(eventErr, writeFailure) {
			t.Fatalf("write %d: %v", i, eventErr)
		}
		if len(eventErr.Error()) > 8192 {
			t.Fatalf("write %d accumulated an unbounded error", i)
		}
		flushErr := recorder.Flush(t.Context())
		if !errors.Is(flushErr, writeFailure) || !errors.Is(flushErr, syncFailure) {
			t.Fatalf("flush %d lost a cause: %v", i, flushErr)
		}
		if len(flushErr.Error()) > 8192 {
			t.Fatalf("flush %d accumulated an unbounded error", i)
		}
	}
	err = recorder.Close()
	if len(err.Error()) > 8192 {
		t.Fatal("close accumulated an unbounded error")
	}
	for _, expected := range []error{writeFailure, syncFailure, closeFailure} {
		if !errors.Is(err, expected) {
			t.Errorf("missing %v in %v", expected, err)
		}
	}
	again := recorder.Close()
	for _, expected := range []error{writeFailure, syncFailure, closeFailure} {
		if !errors.Is(again, expected) {
			t.Errorf("repeated close missing %v in %v", expected, again)
		}
	}
	if err := recorder.Event(t.Context(), "save_failed", Dimensions{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("event after close: %v", err)
	}
	file, err := os.Create(filepath.Join(path, "export-test"))
	if err != nil {
		t.Fatal(err)
	}
	err = writeExport(t.Context(), &failedFile{logFile: file, writeErr: writeFailure, syncErr: syncFailure, closeErr: closeFailure}, []byte("data"))
	for _, expected := range []error{writeFailure, syncFailure, closeFailure} {
		if !errors.Is(err, expected) {
			t.Errorf("export: missing %v in %v", expected, err)
		}
	}
}

func TestDistinctFlushFailuresAreReported(t *testing.T) {
	path := t.TempDir()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	recorder, err := New(t.Context(), Config{ProductVersion: "1", ContentDigest: strings.Repeat("a", 64), Sensitive: []string{"Alice"}}, monkit.NewRegistry(), root)
	if err != nil {
		t.Fatal(err)
	}
	first := errors.New("first sync failure")
	second := errors.New("second sync failure")
	file := &failedFile{logFile: recorder.file, syncErr: first}
	recorder.file = file
	if err := recorder.Flush(t.Context()); !errors.Is(err, first) {
		t.Fatalf("first flush: %v", err)
	}
	file.syncErr = second
	if err := recorder.Flush(t.Context()); !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("second flush: %v", err)
	}
	if err := recorder.Close(); !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("close: %v", err)
	}
}

func TestExportFailureRemovesPartialFile(t *testing.T) {
	recorder, _, path := newTestRecorder(t, 0, 0, false)
	failure := errors.New("export write failure")
	open := recorder.openExport
	recorder.openExport = func(name string) (logFile, error) {
		file, err := open(name)
		if err != nil {
			return nil, err
		}
		return &failedFile{logFile: file, writeErr: failure}, nil
	}
	if err := recorder.Export(t.Context(), "failed.json"); !errors.Is(err, failure) {
		t.Fatalf("export failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "diagnostics", "failed.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial export retained: %v", err)
	}
}

func TestRotationFailureIsRetained(t *testing.T) {
	path := t.TempDir()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	recorder, err := New(t.Context(), Config{ProductVersion: "1", ContentDigest: strings.Repeat("d", 64), LogSize: 512, Sensitive: []string{"Alice"}}, monkit.NewRegistry(), root)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("rotation sync failure")
	recorder.file = &failedFile{logFile: recorder.file, syncErr: failure}
	var rotationErr error
	for i := 0; i < 10 && rotationErr == nil; i++ {
		rotationErr = recorder.Event(t.Context(), "save_failed", Dimensions{})
	}
	if !errors.Is(rotationErr, failure) {
		t.Fatalf("rotation: %v", rotationErr)
	}
	if err := recorder.Close(); !errors.Is(err, failure) {
		t.Fatalf("close lost rotation failure: %v", err)
	}
}

func TestConcurrentRecordingAndClose(t *testing.T) {
	recorder, _, _ := newTestRecorder(t, 0, 0, true)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				err := recorder.Observe(context.Background(), SimulationDuration, Dimensions{Encounter: "ENC-HOS-01"}, time.Millisecond)
				if err != nil && !errors.Is(err, ErrClosed) {
					t.Errorf("observe: %v", err)
					return
				}
			}
		}()
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
}

func taskCounts() map[string]int64 {
	counts := make(map[string]int64)
	monkit.Default.Funcs(func(function *monkit.Func) {
		for _, suffix := range []string{".openLog", ".rotate", ".flushLocked", ".release", ".writeExport", ".collectRegistry"} {
			if strings.HasSuffix(function.FullName(), suffix) {
				counts[suffix] += function.Success()
			}
		}
	})
	return counts
}

func exerciseProcessRegistry(path string) (err error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	ctx := context.Background()
	recorder, err := New(ctx, Config{ProductVersion: "1", ContentDigest: strings.Repeat("f", 64), LogSize: 512, Sensitive: []string{"Alice"}, Diagnostic: true}, monkit.Default, root)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, recorder.Close()) }()
	for i := 0; i < 5; i++ {
		if err = recorder.Event(ctx, "save_failed", Dimensions{}); err != nil {
			return err
		}
	}
	if err = recorder.Export(ctx, "report.json"); err != nil {
		return err
	}
	if err = recorder.Flush(ctx); err != nil {
		return err
	}
	// Exercise the same process registry while recorder operations and close
	// contend, not just while the recorder is used serially.
	var workers sync.WaitGroup
	ready := make(chan struct{}, 4)
	start := make(chan struct{})
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			eventErr := recorder.Event(ctx, "save_failed", Dimensions{})
			ready <- struct{}{}
			<-start
			if eventErr == nil {
				eventErr = recorder.Export(ctx, fmt.Sprintf("concurrent-%d.json", index))
			}
			if eventErr == nil {
				eventErr = recorder.Flush(ctx)
			}
			if eventErr != nil && !errors.Is(eventErr, ErrClosed) {
				failures <- eventErr
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		<-ready
	}
	close(start)
	closeErr := recorder.Close()
	workers.Wait()
	close(failures)
	for failure := range failures {
		closeErr = errors.Join(closeErr, failure)
	}
	return closeErr
}

func TestInstrumentedIOWithProcessRegistry(t *testing.T) {
	before := taskCounts()
	path := t.TempDir()
	done := make(chan error, 1)
	go func() { done <- exerciseProcessRegistry(path) }()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-timer.C:
		t.Fatal("process registry deadlocked during recorder I/O")
	}
	after := taskCounts()
	for _, suffix := range []string{".openLog", ".rotate", ".flushLocked", ".release", ".writeExport", ".collectRegistry"} {
		if after[suffix] <= before[suffix] {
			t.Errorf("no completed monkit task for %s", suffix)
		}
	}
	data, err := os.ReadFile(filepath.Join(path, "diagnostics", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report exportReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	var rotateID string
	monkit.Default.Funcs(func(function *monkit.Func) {
		if strings.HasSuffix(function.FullName(), ".rotate") {
			rotateID = opaqueID(function.FullName())
		}
	})
	for _, trace := range report.Traces {
		if trace.FunctionID == rotateID && rotateID != "" {
			return
		}
	}
	t.Fatal("export did not capture the instrumented rotation span")
}

func TestExportShortWrite(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeExport(t.Context(), &shortFile{logFile: file}, []byte("abc")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write: %v", err)
	}
}

type shortFile struct{ logFile }

func (file *shortFile) Write([]byte) (int, error) { return 0, nil }

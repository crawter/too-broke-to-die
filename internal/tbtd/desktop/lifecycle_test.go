// Copyright (C) 2026 crawter
// All rights reserved.

package desktop

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return Config{ContentDir: root, DataDir: t.TempDir(), ProductVersion: "test", Sensitive: []string{"private-user"}, DrainBudget: 5 * time.Second}
}

func TestInternalShutdownBeforeWriterJoin(t *testing.T) {
	program := &Program{}
	done := make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(done) }) })
	stopped := make(chan struct{})
	finished := make(chan error, 1)
	cause := errors.New("loop failure")
	go func() {
		finished <- program.finishRun(context.Background(), cause, func() { close(stopped) }, done)
	}()
	select {
	case <-program.ShutdownStarted():
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown notification was not sent")
	}
	<-stopped
	select {
	case err := <-finished:
		t.Fatalf("returned before writer joined: %v", err)
	default:
	}
	release.Do(func() { close(done) })
	if err := <-finished; !errors.Is(err, cause) {
		t.Fatalf("loop failure lost: %v", err)
	}
}

func TestInternalShutdownBlockedWriterSubprocess(t *testing.T) {
	if os.Getenv("TBTD_BLOCKED_WRITER") == "1" {
		program := &Program{}
		go func() {
			<-program.ShutdownStarted()
			timer := time.NewTimer(250 * time.Millisecond)
			defer timer.Stop()
			<-timer.C
			os.Exit(23)
		}()
		if err := program.finishRun(context.Background(), errors.New("internal loop failure"), func() {}, make(chan struct{})); err != nil {
			os.Exit(24)
		}
		os.Exit(25)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInternalShutdownBlockedWriterSubprocess$")
	cmd.Env = append(os.Environ(), "TBTD_BLOCKED_WRITER=1")
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
		t.Fatalf("shutdown watchdog was not started: %v %s", err, output)
	}
}

func TestConfigValidation(t *testing.T) {
	config := testConfig(t)
	if err := config.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Config){
		"content":     func(c *Config) { c.ContentDir = "relative" },
		"profile":     func(c *Config) { c.DataDir = "" },
		"version":     func(c *Config) { c.ProductVersion = " " },
		"sensitive":   func(c *Config) { c.Sensitive = nil },
		"empty term":  func(c *Config) { c.Sensitive = []string{""} },
		"budget":      func(c *Config) { c.DrainBudget = 0 },
		"long budget": func(c *Config) { c.DrainBudget = 11 * time.Second },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := config
			change(&invalid)
			if err := invalid.Validate(t.Context()); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := config.Validate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestCloseBeforeRunDrainsAcceptedSettings(t *testing.T) {
	config := testConfig(t)
	program, err := New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	request, err := session.NewSettingsRequest(t.Context(), session.SettingsInput{ID: "queued", Kind: session.SaveSettings, Values: settings.Defaults(t.Context())})
	if err != nil {
		t.Fatal(err)
	}
	if err = program.store.Settings().Submit(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err = program.Close(); err != nil {
		t.Fatal(err)
	}
	if err = program.Close(); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
	result, err := program.store.Settings().Poll(t.Context(), "queued")
	if err != nil || result.Data(t.Context()).Status != session.StorageSucceeded {
		t.Fatalf("accepted settings were not drained: %v, %v", result.Data(t.Context()).Status, err)
	}
	if _, err = os.Stat(filepath.Join(config.DataDir, "campaigns")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settings write touched campaigns: %v", err)
	}
	root, err := platform.OpenRoot(t.Context(), config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := root.LockProfile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(lock.Close(), root.Close()); err != nil {
		t.Fatal(err)
	}
}

func TestStartupSettingsResetAndWindowedOverride(t *testing.T) {
	config := testConfig(t)
	campaignFile := filepath.Join(config.DataDir, "campaigns", "existing", "progress.a.json")
	if err := os.MkdirAll(filepath.Dir(campaignFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(campaignFile, []byte("preserve campaign"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	values := settings.Defaults(t.Context())
	values.Audio.Master = 19
	request, err := session.NewSettingsRequest(t.Context(), session.SettingsInput{ID: "saved", Kind: session.SaveSettings, Values: values})
	if err != nil {
		t.Fatal(err)
	}
	if err = first.store.Settings().Submit(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	config.ResetSettings, config.Windowed = true, true
	second, err := New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	if err = second.Run(t.Context()); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("startup: %v", err)
	}
	if second.values.Audio.Master != settings.Defaults(t.Context()).Audio.Master || second.values.Display.Mode != settings.Windowed || second.values.Display.Width != 1280 || second.values.Display.Height != 720 {
		t.Fatalf("reset and override: %+v", second.values)
	}
	if err = second.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(campaignFile)
	if err != nil || string(content) != "preserve campaign" {
		t.Fatalf("campaign changed: %q %v", content, err)
	}
	config.ResetSettings, config.Windowed = false, false
	third, err := New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	if err = third.Run(t.Context()); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("startup after reset: %v", err)
	}
	if third.values.Display != settings.Defaults(t.Context()).Display {
		t.Fatalf("recovery override was persisted: %+v", third.values.Display)
	}
	if err = third.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestResetPreservesTooNewSettings(t *testing.T) {
	config := testConfig(t)
	config.ResetSettings = true
	path := filepath.Join(config.DataDir, "settings.json")
	original := []byte(`{"magic":"TBTD-SETTINGS","schema":2}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	program, err := New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	if err = program.Run(t.Context()); !errors.Is(err, session.SaveTooNew) {
		t.Fatalf("newer settings not rejected: %v", err)
	}
	if err = program.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(original) {
		t.Fatalf("newer settings changed: %q %v", data, err)
	}
}

func TestFilesystemAcquisitionRollbackAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fail    string
		closed  []string
		combine bool
	}{
		{name: "content open", fail: "content"},
		{name: "profile creation and close failure", fail: "create", closed: []string{"content"}, combine: true},
		{name: "diagnostic root open", fail: "profile", closed: []string{"content"}},
		{name: "recorder acquisition", fail: "recorder", closed: []string{"profile", "content"}},
		{name: "successful acquisition", closed: []string{"profile", "content"}, combine: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := testConfig(t)
			failure := errors.New("injected acquisition failure")
			cleanupFailure := errors.New("injected directory cleanup failure")
			var closed []string
			var opens int
			storeOpened := false
			if tc.fail == "recorder" {
				if err := os.WriteFile(filepath.Join(config.DataDir, "logs"), []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			files := filesystemBoundary{
				open: func(path string) (*os.Root, error) {
					opens++
					if opens == 1 && tc.fail == "content" {
						return nil, failure
					}
					if opens == 2 {
						storeOpened = true
						if tc.fail == "profile" {
							return nil, failure
						}
					}
					return os.OpenRoot(path)
				},
				create: func(ctx context.Context, path string) (*platform.Root, error) {
					if tc.fail == "create" {
						return nil, failure
					}
					return platform.CreateRoot(ctx, path)
				},
				close: func(root *os.Root) error {
					name := "content"
					if root.Name() == config.DataDir {
						name = "profile"
					}
					closed = append(closed, name)
					if storeOpened {
						check, err := platform.OpenRoot(t.Context(), config.DataDir)
						if err != nil {
							t.Fatal(err)
						}
						lock, err := check.LockProfile(t.Context())
						if err != nil {
							checkErr := check.Close()
							t.Fatalf("profile still locked at %s cleanup: %v; close: %v", name, err, checkErr)
						}
						if err = errors.Join(lock.Close(), check.Close()); err != nil {
							t.Fatal(err)
						}
					}
					err := root.Close()
					if tc.combine && name == "content" {
						return errors.Join(err, cleanupFailure)
					}
					return err
				},
			}
			program, err := newWithFilesystem(t.Context(), config, files)
			if tc.fail != "" {
				if err == nil {
					t.Fatal("acquisition failure was ignored")
				}
				if tc.fail != "recorder" && !errors.Is(err, failure) {
					t.Fatalf("acquisition identity lost: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				err = program.Close()
				if again := program.Close(); again != err {
					t.Fatalf("cleanup result not retained: %v vs %v", again, err)
				}
			}
			if tc.combine && !errors.Is(err, cleanupFailure) {
				t.Fatalf("cleanup identity lost: %v", err)
			}
			if !slices.Equal(closed, tc.closed) {
				t.Fatalf("cleanup order: %v, want %v", closed, tc.closed)
			}
		})
	}
}

func TestConstructionFilesystemFailures(t *testing.T) {
	t.Run("missing content root", func(t *testing.T) {
		config := testConfig(t)
		config.ContentDir = filepath.Join(t.TempDir(), "missing")
		if _, err := New(t.Context(), config); !errors.Is(err, session.ContentInvalid) {
			t.Fatalf("content error: %v", err)
		}
	})
	t.Run("missing authored definitions", func(t *testing.T) {
		config := testConfig(t)
		config.ContentDir = t.TempDir()
		if _, err := New(t.Context(), config); err == nil {
			t.Fatal("empty content root accepted")
		}
	})
	t.Run("profile is a file", func(t *testing.T) {
		config := testConfig(t)
		config.DataDir = filepath.Join(t.TempDir(), "profile")
		if err := os.WriteFile(config.DataDir, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := New(t.Context(), config); !errors.Is(err, session.StorageDenied) {
			t.Fatalf("storage error: %v", err)
		}
		data, err := os.ReadFile(config.DataDir)
		if err != nil || string(data) != "preserve" {
			t.Fatalf("profile changed: %q %v", data, err)
		}
	})
}

func TestUnexpectedPersistenceExitIsRetained(t *testing.T) {
	program, err := New(t.Context(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	// Closing the concrete store first makes the real worker fail on start.
	if err = program.store.Close(); err != nil {
		t.Fatal(err)
	}
	program.mu.Lock()
	program.startWriter(t.Context())
	done := program.writerDone
	program.mu.Unlock()
	<-done
	if err = program.Close(); !errors.Is(err, ErrWorkerExited) || !errors.Is(err, session.ErrStoreClosed) {
		t.Fatalf("worker failure missing: %v", err)
	}
	if again := program.Close(); again != err {
		t.Fatalf("failure not retained: %v", again)
	}
}

func TestUnexpectedNilWorkerExit(t *testing.T) {
	program, err := New(t.Context(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := program.Close(); closeErr != nil && !errors.Is(closeErr, ErrWorkerExited) {
			t.Error(closeErr)
		}
	})
	program.mu.Lock()
	program.startWriter(t.Context())
	done := program.writerDone
	program.mu.Unlock()
	request, err := session.NewSettingsRequest(t.Context(), session.SettingsInput{ID: "ready", Kind: session.LoadSettings})
	if err != nil {
		t.Fatal(err)
	}
	if err = program.store.Settings().Submit(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticks := time.NewTicker(time.Millisecond)
	defer ticks.Stop()
	for {
		result, pollErr := program.store.Settings().Poll(t.Context(), "ready")
		if pollErr != nil {
			t.Fatal(pollErr)
		}
		if result.Data(t.Context()).Status != session.StoragePending {
			break
		}
		select {
		case <-ticks.C:
		case <-deadline.C:
			t.Fatal("worker did not process accepted request")
		}
	}
	if err = program.store.Close(); err != nil {
		t.Fatal(err)
	}
	<-done
	if err = program.Close(); !errors.Is(err, ErrWorkerExited) {
		t.Fatalf("early nil exit not reported: %v", err)
	}
}

func TestCloseWithAlreadyClosedRoots(t *testing.T) {
	first, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(first.Close(), second.Close()); err != nil {
		t.Fatal(err)
	}
	program := &Program{contentRoot: first, profileRoot: second}
	if err = program.Close(); err != nil {
		t.Fatalf("close already-closed roots: %v", err)
	}
	if err = program.Close(); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
}

func TestConstructionRollbackAndRuntimeStatus(t *testing.T) {
	config := testConfig(t)
	// A file in place of the logs directory fails after the writer acquired
	// the profile lock. Rollback must release that lock for the next attempt.
	logPath := filepath.Join(config.DataDir, "logs")
	if err := os.WriteFile(logPath, []byte("obstruction"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(t.Context(), config); err == nil {
		t.Fatal("invalid diagnostics directory accepted")
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	program, err := New(t.Context(), config)
	if err != nil {
		t.Fatalf("profile remained locked after rollback: %v", err)
	}
	if _, err = New(t.Context(), config); !errors.Is(err, platform.ErrProfileInUse) {
		t.Fatalf("second profile owner: %v", err)
	}
	if err = program.Run(t.Context()); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("unexpected runtime status: %v", err)
	}
	if err = program.Close(); err != nil {
		t.Fatal(err)
	}
}

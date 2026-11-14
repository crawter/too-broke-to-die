// Copyright (C) 2026 crawter
// All rights reserved.

package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/desktop/assets"
	"too-broke-to-die/internal/tbtd/desktop/input"
	"too-broke-to-die/internal/tbtd/desktop/render"
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

type recordingWindow struct {
	opened   int
	closed   int
	applied  []settings.Display
	openErr  error
	applyErr error
	closeErr error
	onClose  func(context.Context) error
	events   []string
	thread   func() string
	threads  []string
}

func (window *recordingWindow) record(event string) {
	window.events = append(window.events, event)
	if window.thread != nil {
		window.threads = append(window.threads, window.thread())
	}
}

func (window *recordingWindow) Open(context.Context) error {
	window.record("open")
	if window.openErr != nil {
		return window.openErr
	}
	window.opened++
	return nil
}

func (window *recordingWindow) Apply(_ context.Context, display settings.Display) error {
	window.record("apply")
	window.applied = append(window.applied, display)
	return window.applyErr
}

func (window *recordingWindow) Close(ctx context.Context) error {
	window.record("close")
	window.closed++
	if window.onClose != nil {
		return errors.Join(window.closeErr, window.onClose(ctx))
	}
	return window.closeErr
}

func testNew(ctx context.Context, config Config, window *recordingWindow) (*Program, error) {
	return newWithWindow(ctx, config, filesystemBoundary{
		open: os.OpenRoot, create: platform.CreateRoot,
		close: func(root *os.Root) error { return root.Close() },
	}, window)
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

func TestCloseWaitsForRunBeforeStoppingWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runDone := make(chan struct{})
	writerDone := make(chan struct{})
	stopped := make(chan struct{})
	program := &Program{
		runCancel:  cancel,
		runDone:    runDone,
		writerDone: writerDone,
		writerStop: func() { close(stopped) },
	}
	finished := make(chan error, 1)
	go func() { finished <- program.Close() }()
	<-ctx.Done()
	select {
	case <-stopped:
		t.Fatal("writer stopped while the frame owner was still running")
	default:
	}
	close(writerDone) // The run owner drains and joins the writer before returning.
	close(runDone)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	<-stopped
	if err := program.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseBeforeRunDrainsAcceptedSettings(t *testing.T) {
	config := testConfig(t)
	program, err := testNew(t.Context(), config, &recordingWindow{})
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
	first, err := testNew(t.Context(), config, &recordingWindow{})
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
	window := &recordingWindow{}
	second, err := testNew(t.Context(), config, window)
	if err != nil {
		t.Fatal(err)
	}
	if err = second.Run(t.Context()); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("startup: %v", err)
	}
	if second.values.Audio.Master != settings.Defaults(t.Context()).Audio.Master || second.values.Display.Mode != settings.Windowed || second.values.Display.Width != 1280 || second.values.Display.Height != 720 {
		t.Fatalf("reset and override: %+v", second.values)
	}
	if len(window.applied) != 1 || window.applied[0] != second.values.Display {
		t.Fatalf("window did not receive recovery display: %+v", window.applied)
	}
	if err = second.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(campaignFile)
	if err != nil || string(content) != "preserve campaign" {
		t.Fatalf("campaign changed: %q %v", content, err)
	}
	config.ResetSettings, config.Windowed = false, false
	third, err := testNew(t.Context(), config, &recordingWindow{})
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
	program, err := testNew(t.Context(), config, &recordingWindow{})
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

func TestWindowAcquisitionFailureRollsBack(t *testing.T) {
	config := testConfig(t)
	acquireErr := errors.New("window acquisition")
	cleanupErr := errors.New("content root cleanup")
	window := &recordingWindow{openErr: acquireErr}
	files := filesystemBoundary{
		open: os.OpenRoot, create: platform.CreateRoot,
		close: func(root *os.Root) error {
			err := root.Close()
			if root.Name() == config.ContentDir {
				return errors.Join(err, cleanupErr)
			}
			return err
		},
	}
	_, err := newWithWindow(t.Context(), config, files, window)
	if !errors.Is(err, acquireErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("acquisition and rollback errors: %v", err)
	}
	if window.opened != 0 || window.closed != 0 {
		t.Fatalf("unacquired window was closed: %+v", window)
	}
	root, err := platform.OpenRoot(t.Context(), config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := root.LockProfile(t.Context())
	if err != nil {
		t.Fatalf("profile lock not released: %v", err)
	}
	if err = errors.Join(lock.Close(), root.Close()); err != nil {
		t.Fatal(err)
	}
}

func TestWindowApplyFailureAndCloseOnce(t *testing.T) {
	applyErr := errors.New("display configuration")
	closeErr := errors.New("window close")
	window := &recordingWindow{applyErr: applyErr, closeErr: closeErr}
	program, err := testNew(t.Context(), testConfig(t), window)
	if err != nil {
		t.Fatal(err)
	}
	if err = program.Run(t.Context()); !errors.Is(err, applyErr) {
		t.Fatalf("display failure lost: %v", err)
	}
	if err = program.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("cleanup failure lost: %v", err)
	}
	if again := program.Close(); again != err {
		t.Fatalf("cleanup result not retained: %v", again)
	}
	if !slices.Equal(window.events, []string{"open", "apply", "close"}) || window.opened != 1 || window.closed != 1 {
		t.Fatalf("window ownership: %+v", window)
	}
}

func TestWindowCallsStayOnLockedThread(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("thread identity uses Linux procfs")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	thread := func() string {
		id, err := os.Readlink("/proc/thread-self")
		if err != nil {
			return ""
		}
		return id
	}
	owner := thread()
	if owner == "" {
		t.Skip("procfs thread identity unavailable")
	}
	window := &recordingWindow{thread: thread}
	program, err := testNew(t.Context(), testConfig(t), window)
	if err != nil {
		t.Fatal(err)
	}
	if err = program.Run(t.Context()); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("run: %v", err)
	}
	if err = program.Close(); err != nil {
		t.Fatal(err)
	}
	if len(window.threads) != 3 {
		t.Fatalf("window calls: %v", window.events)
	}
	for _, got := range window.threads {
		if got != owner {
			t.Fatalf("window call on thread %q, want %q", got, owner)
		}
	}
}

type delayedFrameStore struct {
	request session.ProgressRequest
	count   int
	ready   bool
}

func (store *delayedFrameStore) Submit(_ context.Context, request session.ProgressRequest) error {
	store.request = request
	store.count++
	return nil
}

func (store *delayedFrameStore) Poll(ctx context.Context, id session.RequestID) (session.ProgressResult, error) {
	data := session.ProgressResultData{ID: id, Status: session.StoragePending}
	if store.ready {
		data.Status = session.StorageSucceeded
		data.Campaign = "00000000-0000-4000-8000-000000000001"
		data.Revision, data.RevisionKnown = 1, true
		data.PayloadDigest = strings.Repeat("a", 64)
		data.Snapshot = campaign.Snapshot{}
	}
	return session.NewProgressResult(ctx, data), nil
}

func TestSessionFramesRemainResponsiveDuringPendingRestore(t *testing.T) {
	ctx := t.Context()
	filesystem := os.DirFS(testConfig(t).ContentDir)
	definitions, err := content.LoadCampaign(ctx, filesystem, "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := campaign.New(ctx, definitions)
	if err != nil {
		t.Fatal(err)
	}
	bays, err := content.LoadServiceBays(ctx, filesystem, "content/data/encounters/templates.json", "content/data/encounters/variants.json", definitions)
	if err != nil {
		t.Fatal(err)
	}
	store := &delayedFrameStore{}
	service, err := session.New(ctx, owner, definitions, bays, store)
	if err != nil {
		t.Fatal(err)
	}
	frame := func(value session.FrameInput) session.FrameViewData {
		t.Helper()
		command, frameErr := session.NewFrameCommand(ctx, value)
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		view, frameErr := service.HandleFrame(ctx, command)
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		return view.Data(ctx)
	}
	view := frame(session.FrameInput{Selections: []session.MenuSelection{{Action: "continue"}}})
	if view.State != session.StateRestoring || store.count != 1 {
		t.Fatalf("restore not submitted: %+v", view)
	}
	view = frame(session.FrameInput{Elapsed: 6 * time.Second, Focus: session.FocusLost, CloseRequested: true, Edges: []session.ActionEdge{{Action: settings.Melee, Kind: session.Pressed}}})
	if view.State != session.StateRestoring || !view.Slow || view.Progress.Campaign != "" || store.count != 1 {
		t.Fatalf("pending frame: %+v", view)
	}
	store.ready = true
	view = frame(session.FrameInput{})
	if view.State != session.StatePause || view.Confirmation != "quit_to_title" || view.World.Tick != 0 {
		t.Fatalf("pending close or pause lost: %+v", view)
	}
	view = frame(session.FrameInput{Focus: session.FocusGained, Selections: []session.MenuSelection{{Action: "cancel"}}, Values: []session.ActionValue{{Action: settings.Melee}}})
	if view.State != session.StatePause || view.Confirmation != "" || view.World.Tick != 0 {
		t.Fatalf("cancel unexpectedly resumed: %+v", view)
	}
	view = frame(session.FrameInput{Selections: []session.MenuSelection{{Action: "resume"}}})
	if view.State != session.StateActive {
		t.Fatalf("explicit resume: %+v", view)
	}
	view = frame(session.FrameInput{Elapsed: 100 * time.Millisecond, Focus: session.FocusLost, Edges: []session.ActionEdge{{Action: settings.Melee, Kind: session.Pressed}}})
	if view.State != session.StatePause || view.World.Tick != 0 {
		t.Fatalf("focus did not precede gameplay: %+v", view)
	}
}

func TestSettingsFramesPreserveRecoveryDisplayAndWaitForSave(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := t.Context()
	config := testConfig(t)
	config.Windowed = true
	window := &recordingWindow{}
	program, err := testNew(ctx, config, window)
	if err != nil {
		t.Fatal(err)
	}
	program.values = settings.Defaults(ctx)
	program.savedDisplay = program.values.Display
	program.values.Display = settings.Display{Mode: settings.Windowed, Width: 1280, Height: 720, VSync: true}
	program.sampler, err = input.New(ctx, program.values, nativeThreadID)
	if err != nil {
		t.Fatal(err)
	}
	texts, err := content.LoadRussian(ctx, program.contentRoot.FS(), program.definitions)
	if err != nil {
		t.Fatal(err)
	}
	program.ui, err = render.NewInterface(ctx, texts, program.definitions, program.sampler, program.values)
	if err != nil {
		t.Fatal(err)
	}
	view, err := session.NewFrameView(ctx, session.FrameViewData{State: session.StateTitle}, campaign.Snapshot{}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	activate := func(action session.MenuAction, target string) {
		t.Helper()
		screen, planErr := program.ui.Plan(ctx, view)
		if planErr != nil {
			t.Fatal(planErr)
		}
		for index, item := range screen.Items {
			if item.Action == action && item.Target == target {
				program.ui.SetFocus(screen, index)
				program.ui.Activate(screen)
				return
			}
		}
		t.Fatalf("missing UI action %s %s", action, target)
	}
	activate("ui_settings", "1")
	activate("ui_setting", "TXT-SET-MASTER")
	activate("ui_settings_apply", "")
	// A draft requested during a campaign save waits for its acknowledgment.
	if err = program.updateSettings(ctx, session.FrameViewData{Save: session.SavePending}); err != nil {
		t.Fatal(err)
	}
	if program.settingsPending != "" || program.settingsDraft == nil {
		t.Fatal("settings overtook the campaign save")
	}
	program.mu.Lock()
	program.startWriter(ctx)
	done, stop := program.writerDone, program.writerStop
	program.mu.Unlock()
	if err = program.updateSettings(ctx, session.FrameViewData{}); err != nil {
		t.Fatal(err)
	}
	if program.settingsPending == "" {
		t.Fatal("settings were not submitted")
	}
	stop()
	<-done
	if err = program.updateSettings(ctx, session.FrameViewData{}); err != nil {
		t.Fatal(err)
	}
	if program.values.Audio.Master != 90 || program.values.Display.Mode != settings.Windowed || program.settingsPending != "" {
		t.Fatalf("acknowledged settings: %+v", program.values)
	}
	if err = program.Close(); err != nil {
		t.Fatal(err)
	}
	config.Windowed = false
	reopened, err := testNew(ctx, config, &recordingWindow{})
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.Run(ctx); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("reload: %v", err)
	}
	if reopened.values.Display != settings.Defaults(ctx).Display || reopened.values.Audio.Master != 90 {
		t.Fatalf("persisted settings: %+v", reopened.values)
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCompositionRejectsMissingFinalFonts(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	config := testConfig(t)
	window := &recordingWindow{}
	files := filesystemBoundary{
		open: os.OpenRoot, create: platform.CreateRoot,
		close: func(root *os.Root) error { return root.Close() },
	}
	program, err := newWithNative(t.Context(), config, files, window)
	if err == nil || program != nil || PresentError(err).Code != session.ContentInvalid {
		t.Fatalf("missing final fonts must reject startup: %v", err)
	}
	if !slices.Equal(window.events, []string{"open", "close"}) {
		t.Fatalf("window not rolled back: %v", window.events)
	}
	root, err := platform.OpenRoot(t.Context(), config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := root.LockProfile(t.Context())
	if err != nil {
		t.Fatalf("profile lock not released: %v", err)
	}
	if err = errors.Join(lock.Close(), root.Close()); err != nil {
		t.Fatal(err)
	}
}

func TestRendererClosedBeforeWindowAndCleanupErrorsCombined(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := t.Context()
	config := testConfig(t)
	filesystem := os.DirFS(config.ContentDir)
	definitions, err := content.LoadCampaign(ctx, filesystem, "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	texts, err := content.LoadRussian(ctx, filesystem, definitions)
	if err != nil {
		t.Fatal(err)
	}
	sampler, err := input.New(ctx, settings.Defaults(ctx), nativeThreadID)
	if err != nil {
		t.Fatal(err)
	}
	ui, err := render.NewInterface(ctx, texts, definitions, sampler, settings.Defaults(ctx))
	if err != nil {
		t.Fatal(err)
	}
	// The renderer borrows the cache and Close does not use its native handles.
	renderer, err := render.New(ctx, &assets.Cache{}, nativeThreadID, ui)
	if err != nil {
		t.Fatal(err)
	}
	windowErr := errors.New("window cleanup")
	rootErr := errors.New("root cleanup")
	orderErr := errors.New("renderer remained open when window closed")
	window := &recordingWindow{closeErr: windowErr}
	window.onClose = func(ctx context.Context) error {
		_, drawErr := renderer.Draw(ctx, session.FrameView{}, 1280, 720, 0, settings.Defaults(ctx).Accessibility)
		if !errors.Is(drawErr, assets.ErrClosed) {
			return orderErr
		}
		return nil
	}
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	program := &Program{ownerThread: nativeThreadID(), renderer: renderer, window: window, contentRoot: root, closeRoot: func(root *os.Root) error {
		return errors.Join(root.Close(), rootErr)
	}}
	err = program.Close()
	if !errors.Is(err, windowErr) || !errors.Is(err, rootErr) || errors.Is(err, orderErr) {
		t.Fatalf("cleanup order or combined errors: %v", err)
	}
	if again := program.Close(); !errors.Is(again, windowErr) || !errors.Is(again, rootErr) || window.closed != 1 {
		t.Fatalf("repeated cleanup: %v, window closed %d times", again, window.closed)
	}
}

func TestNativeCloseRejectsWrongThreadWithoutConsumingOwnership(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	window := &recordingWindow{}
	program := &Program{ownerThread: nativeThreadID(), window: window}
	result := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		result <- program.Close()
	}()
	if err := <-result; !errors.Is(err, assets.ErrThread) {
		t.Fatalf("wrong thread cleanup: %v", err)
	}
	if window.closed != 0 {
		t.Fatal("window closed on wrong thread")
	}
	if err := program.Close(); err != nil {
		t.Fatal(err)
	}
	if err := program.Close(); err != nil || window.closed != 1 {
		t.Fatalf("ownership not retained exactly once: %d, %v", window.closed, err)
	}
	go func() { result <- program.Close() }()
	if err := <-result; err != nil || window.closed != 1 {
		t.Fatalf("completed cleanup not retained across threads: %v", err)
	}
}

func TestConstructionFilesystemFailures(t *testing.T) {
	t.Run("missing content root", func(t *testing.T) {
		config := testConfig(t)
		config.ContentDir = filepath.Join(t.TempDir(), "missing")
		if _, err := testNew(t.Context(), config, &recordingWindow{}); !errors.Is(err, session.ContentInvalid) {
			t.Fatalf("content error: %v", err)
		}
	})
	t.Run("missing authored definitions", func(t *testing.T) {
		config := testConfig(t)
		config.ContentDir = t.TempDir()
		if _, err := testNew(t.Context(), config, &recordingWindow{}); err == nil {
			t.Fatal("empty content root accepted")
		}
	})
	t.Run("profile is a file", func(t *testing.T) {
		config := testConfig(t)
		config.DataDir = filepath.Join(t.TempDir(), "profile")
		if err := os.WriteFile(config.DataDir, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := testNew(t.Context(), config, &recordingWindow{}); !errors.Is(err, session.StorageDenied) {
			t.Fatalf("storage error: %v", err)
		}
		data, err := os.ReadFile(config.DataDir)
		if err != nil || string(data) != "preserve" {
			t.Fatalf("profile changed: %q %v", data, err)
		}
	})
}

func TestUnexpectedPersistenceExitIsRetained(t *testing.T) {
	program, err := testNew(t.Context(), testConfig(t), &recordingWindow{})
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
	program, err := testNew(t.Context(), testConfig(t), &recordingWindow{})
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
	if _, err := testNew(t.Context(), config, &recordingWindow{}); err == nil {
		t.Fatal("invalid diagnostics directory accepted")
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	program, err := testNew(t.Context(), config, &recordingWindow{})
	if err != nil {
		t.Fatalf("profile remained locked after rollback: %v", err)
	}
	if _, err = testNew(t.Context(), config, &recordingWindow{}); !errors.Is(err, platform.ErrProfileInUse) {
		t.Fatalf("second profile owner: %v", err)
	}
	if err = program.Run(t.Context()); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("unexpected runtime status: %v", err)
	}
	if err = program.Close(); err != nil {
		t.Fatal(err)
	}
}

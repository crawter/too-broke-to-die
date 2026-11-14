// Copyright (C) 2026 crawter
// All rights reserved.

package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	monkit "github.com/spacemonkeygo/monkit/v3"
	"github.com/zeebo/errs"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/desktop/assets"
	"too-broke-to-die/internal/tbtd/desktop/audio"
	"too-broke-to-die/internal/tbtd/desktop/input"
	"too-broke-to-die/internal/tbtd/desktop/render"
	"too-broke-to-die/internal/tbtd/diagnostics"
	"too-broke-to-die/internal/tbtd/persistence"
	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

// ErrRuntimeUnavailable means the program has no initialized native renderer.
var ErrRuntimeUnavailable = errors.New("native desktop runtime not yet wired")

// ErrWorkerExited marks a persistence worker that stopped before shutdown.
var ErrWorkerExited = errors.New("persistence worker exited unexpectedly")

// Program owns the window, profile writer, local recorder, and filesystem roots.
// The caller must keep the initial OS thread locked through Run and Close.
type Program struct {
	window           windowBoundary
	ownerThread      uint64
	manifest         *content.Manifest
	definitions      *content.Campaign
	cache            *assets.Cache
	sampler          *input.Sampler
	ui               *render.Interface
	renderer         *render.Renderer
	mixer            *audio.Mixer
	contentRoot      *os.Root
	profileRoot      *os.Root
	store            *persistence.Store
	recorder         *diagnostics.Recorder
	session          *session.Service
	windowed         bool
	reset            bool
	values           settings.Values
	savedDisplay     settings.Display
	settingsDraft    *settings.Values
	settingsPending  session.RequestID
	settingsSequence uint64
	mu               sync.Mutex
	writerDone       chan struct{}
	writerStop       context.CancelFunc
	writerErr        error
	runCancel        context.CancelFunc
	runDone          chan struct{}
	runStarted       bool
	closing          bool
	closed           bool
	shutdown         chan struct{}
	shutdownOnce     sync.Once
	closeOnce        sync.Once
	closeErr         error
	closeRoot        func(*os.Root) error
}

// New validates authored definitions and acquires the exclusive profile writer.
// It starts no application worker. Any partial acquisition is rolled back.
func New(ctx context.Context, config Config) (_ *Program, err error) {
	defer mon.Task()(&ctx)(&err)
	return newWithNative(ctx, config, filesystemBoundary{
		open:   os.OpenRoot,
		create: platform.CreateRoot,
		close:  func(root *os.Root) error { return root.Close() },
	}, raylibWindow{})
}

// newWithNative keeps all native acquisition on the caller's locked thread.
// The cache and mixer own their handles; the renderer only borrows the cache.
func newWithNative(ctx context.Context, config Config, files filesystemBoundary, window windowBoundary) (_ *Program, err error) {
	defer mon.Task()(&ctx)(&err)
	program, err := newWithWindow(ctx, config, files, window)
	if err != nil {
		return nil, err
	}
	program.ownerThread = nativeThreadID()
	defer func() {
		if err != nil {
			err = errs.Combine(err, program.Close())
		}
	}()
	filesystem := program.contentRoot.FS()
	texts, err := content.LoadRussian(ctx, filesystem, program.definitions)
	if err != nil {
		return nil, err
	}
	program.cache, err = assets.New(ctx, filesystem, program.manifest, nativeThreadID, nil)
	if err != nil {
		return nil, err
	}
	values := settings.Defaults(ctx)
	program.sampler, err = input.New(ctx, values, nativeThreadID)
	if err != nil {
		return nil, err
	}
	program.ui, err = render.NewInterface(ctx, texts, program.definitions, program.sampler, values)
	if err != nil {
		return nil, err
	}
	program.renderer, err = render.New(ctx, program.cache, nativeThreadID, program.ui)
	if err != nil {
		return nil, err
	}
	// AudioUnavailable may return an owned mixer as well as an error.
	program.mixer, err = audio.New(ctx, filesystem, program.manifest, nativeThreadID, "shared")
	if err != nil {
		return nil, err
	}
	return program, nil
}

// windowBoundary is the operating-system window boundary. Its owner calls all
// methods on the locked initial thread. Open must clean up on failure.
type windowBoundary interface {
	Open(context.Context) error
	Apply(context.Context, settings.Display) error
	Close(context.Context) error
}

type raylibWindow struct{}

func (raylibWindow) Open(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return err
	}
	rl.SetConfigFlags(rl.FlagWindowResizable | rl.FlagWindowHighdpi | rl.FlagVsyncHint)
	rl.InitWindow(1280, 720, "TOO BROKE TO DIE")
	if !rl.IsWindowReady() {
		return session.GraphicsUnavailable
	}
	rl.SetExitKey(rl.KeyNull)
	rl.SetTargetFPS(60)
	return nil
}

func (raylibWindow) Apply(ctx context.Context, display settings.Display) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return err
	}
	if !rl.IsWindowReady() {
		return session.GraphicsUnavailable
	}
	if display.Width < 1280 || display.Height < 720 || display.Width > 16384 || display.Height > 16384 {
		return settings.ErrInvalid
	}
	if display.Mode != settings.Windowed && display.Mode != settings.Borderless && display.Mode != settings.Fullscreen {
		return settings.ErrInvalid
	}
	if rl.IsWindowFullscreen() {
		rl.ToggleFullscreen()
	}
	if rl.IsWindowState(rl.FlagBorderlessWindowedMode) {
		rl.ToggleBorderlessWindowed()
	}
	rl.SetWindowSize(display.Width, display.Height)
	switch display.Mode {
	case settings.Borderless:
		rl.ToggleBorderlessWindowed()
	case settings.Fullscreen:
		rl.ToggleFullscreen()
	}
	if display.VSync {
		rl.SetWindowState(rl.FlagVsyncHint)
	} else {
		rl.ClearWindowState(rl.FlagVsyncHint)
	}
	return nil
}

func (raylibWindow) Close(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	rl.CloseWindow()
	return nil
}

func newWithWindow(ctx context.Context, config Config, files filesystemBoundary, window windowBoundary) (_ *Program, err error) {
	defer mon.Task()(&ctx)(&err)
	if window == nil {
		return nil, errors.New("missing window boundary")
	}
	program, err := newWithFilesystem(ctx, config, files)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errs.Combine(err, program.Close())
		}
	}()
	if err = window.Open(ctx); err != nil {
		return nil, fmt.Errorf("open window: %w", err)
	}
	program.window = window
	return program, nil
}

// filesystemBoundary isolates the owned directory operations from the native
// filesystem. It does not substitute campaign, session, or persistence logic.
type filesystemBoundary struct {
	open   func(string) (*os.Root, error)
	create func(context.Context, string) (*platform.Root, error)
	close  func(*os.Root) error
}

func newWithFilesystem(ctx context.Context, config Config, files filesystemBoundary) (_ *Program, err error) {
	defer mon.Task()(&ctx)(&err)
	if files.open == nil || files.create == nil || files.close == nil {
		return nil, errors.New("missing filesystem boundary")
	}
	if err = config.Validate(ctx); err != nil {
		return nil, err
	}
	program := &Program{windowed: config.Windowed, reset: config.ResetSettings, closeRoot: files.close}
	defer func() {
		if err != nil {
			err = errs.Combine(err, program.Close())
		}
	}()
	program.contentRoot, err = files.open(config.ContentDir)
	if err != nil {
		return nil, fmt.Errorf("open content root: %w", errors.Join(session.ContentInvalid, err))
	}
	if program.contentRoot == nil {
		return nil, errors.New("filesystem returned a nil content root")
	}
	filesystem := program.contentRoot.FS()
	manifest, err := content.GenerateManifest(ctx, filesystem)
	if err != nil {
		return nil, fmt.Errorf("load content inventory: %w", err)
	}
	manifestBytes, err := content.ManifestBytes(ctx, manifest)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(manifestBytes)
	manifestDigest := hex.EncodeToString(digest[:])
	definitions, err := content.LoadCampaign(ctx, filesystem, "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		return nil, err
	}
	program.manifest, program.definitions = manifest, definitions
	bays, err := content.LoadServiceBays(ctx, filesystem, "content/data/encounters/templates.json", "content/data/encounters/variants.json", definitions)
	if err != nil {
		return nil, err
	}
	owner, err := campaign.New(ctx, definitions)
	if err != nil {
		return nil, err
	}
	codec, err := persistence.NewCodec(ctx, owner, definitions, persistence.Compatibility{
		ProductVersion: config.ProductVersion,
		ContentVersion: manifest.ContentVersion,
		ManifestDigest: manifestDigest,
	}, nil)
	if err != nil {
		return nil, err
	}
	root, err := files.create(ctx, config.DataDir)
	if err != nil {
		return nil, fmt.Errorf("create profile root: %w", errors.Join(session.StorageDenied, err))
	}
	if root == nil {
		return nil, errors.New("filesystem returned a nil profile root")
	}
	if err = root.Close(); err != nil {
		return nil, fmt.Errorf("close profile creation root: %w", err)
	}
	program.store, err = persistence.Open(ctx, config.DataDir, codec, 8, config.DrainBudget)
	if err != nil {
		return nil, fmt.Errorf("open profile: %w", errors.Join(session.StorageDenied, err))
	}
	program.profileRoot, err = files.open(config.DataDir)
	if err != nil {
		return nil, fmt.Errorf("open diagnostics root: %w", err)
	}
	if program.profileRoot == nil {
		return nil, errors.New("filesystem returned a nil diagnostics root")
	}
	program.recorder, err = diagnostics.New(ctx, diagnostics.Config{
		ProductVersion: config.ProductVersion,
		ContentDigest:  manifestDigest,
		Diagnostic:     config.Diagnostics,
		Sensitive:      config.Sensitive,
	}, monkit.Default, program.profileRoot)
	if err != nil {
		return nil, fmt.Errorf("open diagnostics: %w", err)
	}
	program.session, err = session.New(ctx, owner, definitions, bays, program.store)
	if err != nil {
		return nil, err
	}
	return program, nil
}

// Run supervises the persistence worker while frames run on the owning thread.
func (program *Program) Run(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return err
	}
	if program.ownerThread != 0 && nativeThreadID() != program.ownerThread {
		return assets.ErrThread
	}
	program.mu.Lock()
	if program.runStarted || program.closing || program.store == nil {
		program.mu.Unlock()
		return session.ErrStoreClosed
	}
	program.runStarted = true
	loopCtx, cancelLoop := context.WithCancel(ctx)
	program.runCancel = cancelLoop
	program.runDone = make(chan struct{})
	runDone := program.runDone
	program.startWriter(ctx)
	stop, done := program.writerStop, program.writerDone
	program.mu.Unlock()
	defer close(runDone)
	defer cancelLoop()

	loopErr := program.loadSettings(loopCtx, done)
	if loopErr == nil && program.window != nil {
		loopErr = program.window.Apply(loopCtx, program.values.Display)
	}
	if loopErr == nil && program.renderer != nil {
		loopErr = program.sampler.SetBindings(loopCtx, program.values)
		if loopErr == nil {
			loopErr = program.ui.SetSettings(loopCtx, program.values)
		}
		if loopErr == nil {
			loopErr = program.runFrames(loopCtx)
		}
	} else if loopErr == nil {
		loopErr = ErrRuntimeUnavailable
	}
	return program.finishRun(ctx, loopErr, stop, done)
}

// runFrames stays on the thread that constructed the native resources. The
// session owns pause, saving and close confirmation; no proposed progress is
// published by this adapter.
func (program *Program) runFrames(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	var view session.FrameView
	var haveView, closeSeen bool
	var viewport input.Viewport
	group := "shared"
	last := time.Now()
	targetFPS := int32(60)
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		started := time.Now()
		elapsed := started.Sub(last)
		last = started
		rl.PollInputEvents()
		closeNow := rl.WindowShouldClose()
		closeRequest := closeNow && !closeSeen
		closeSeen = closeNow
		width, height := rl.GetScreenWidth(), rl.GetScreenHeight()
		if width <= 0 || height <= 0 {
			return session.GraphicsUnavailable
		}
		geometry := (render.Camera{}).Viewport(width, height)
		geometry.Target = viewport.Target
		if viewport.Zoom > 0 {
			geometry.Offset, geometry.Zoom, geometry.Rotation = viewport.Offset, viewport.Zoom, viewport.Rotation
		}
		var origin input.Point
		facing := 1
		if haveView {
			for _, entity := range view.Data(ctx).Entities {
				if entity.ID == "player" {
					origin, facing = input.Point{X: entity.X, Y: entity.Y}, entity.Facing
					break
				}
			}
		}
		command, sampleErr := program.sampler.Sample(ctx, input.Frame{Elapsed: elapsed, View: geometry, ThrowOrigin: origin, Facing: facing, CloseRequested: closeRequest})
		if sampleErr != nil {
			return sampleErr
		}
		if haveView {
			font, fontErr := program.cache.Font(ctx, "FONT-NOTO-REGULAR")
			if fontErr != nil {
				return fontErr
			}
			pointer, valid := program.sampler.Pointer()
			if !valid {
				pointer = input.Point{X: -1, Y: -1}
			}
			command, err = program.ui.Navigate(ctx, view, command, pointer, func(text string, size float64) float64 {
				return float64(rl.MeasureTextEx(font, text, float32(size), 1).X)
			})
			if err != nil {
				return err
			}
		}
		if closeRequest && (!haveView || view.Data(ctx).State == session.StateTitle) {
			frame := command.Data(ctx)
			frame.Selections = []session.MenuSelection{{Action: "exit_game"}}
			command, err = session.NewFrameCommand(ctx, frame)
			if err != nil {
				return err
			}
		}
		view, err = program.session.HandleFrame(ctx, command)
		if err != nil {
			return err
		}
		haveView = true
		data := view.Data(ctx)
		if err = program.updateSettings(ctx, data); err != nil {
			return err
		}
		if data.ExitRequested {
			return nil
		}
		nextGroup := string(data.World.Location)
		if data.Scene.Ending != "" || data.State == session.StateEnd {
			nextGroup = "ending"
		}
		if nextGroup == "" {
			nextGroup = "shared"
		}
		if nextGroup != group {
			if err = program.cache.LoadLocation(ctx, nextGroup); err != nil {
				return err
			}
			audioGroup := map[string]string{"shared": "shared", "ending": "ending", "LOC-HOS": "location-hos", "LOC-CHU": "location-chu", "LOC-MIN": "location-min", "LOC-INS": "location-ins"}[nextGroup]
			if err = program.mixer.LoadGroup(ctx, audioGroup); err != nil {
				return err
			}
			group = nextGroup
		}
		focused := rl.IsWindowFocused() && !rl.IsWindowMinimized() && !rl.IsWindowHidden()
		if err = program.mixer.Update(ctx, view, audio.Frame{Elapsed: elapsed, Focused: focused, Dialogue: data.State == session.StateScene, Settings: program.values.Audio}); err != nil {
			return err
		}
		fps := int32(60)
		if rl.IsWindowMinimized() || rl.IsWindowHidden() {
			fps = 15
		}
		if fps != targetFPS {
			rl.SetTargetFPS(fps)
			targetFPS = fps
		}
		rl.BeginDrawing()
		rl.ClearBackground(rl.Black)
		viewport, err = program.renderer.Draw(ctx, view, width, height, elapsed, program.values.Accessibility)
		rl.EndDrawing()
		if err != nil {
			return err
		}
		if program.recorder != nil {
			if err = program.recorder.Observe(ctx, diagnostics.FrameDuration, diagnostics.Dimensions{State: string(data.State)}, time.Since(started)); err != nil {
				return err
			}
		}
	}
}

// updateSettings keeps player preferences separate from campaign commits. A
// recovery display override is applied in memory, never written to the store.
func (program *Program) updateSettings(ctx context.Context, view session.FrameViewData) (err error) {
	defer mon.Task()(&ctx)(&err)
	if draft, ok := program.ui.TakeSettingsDraft(ctx); ok {
		if program.windowed {
			draft.Display = program.savedDisplay
		}
		program.settingsDraft = &draft
	}
	store := program.store.Settings()
	if program.settingsPending != "" {
		result, pollErr := store.Poll(ctx, program.settingsPending)
		if pollErr != nil {
			return pollErr
		}
		data := result.Data(ctx)
		if data.Status != session.StoragePending {
			program.settingsPending = ""
			if data.Status != session.StorageSucceeded {
				if data.Code == "" {
					return session.Internal
				}
				return data.Code
			}
			program.savedDisplay = data.Values.Display
			values := data.Values
			if program.windowed {
				values.Display = settings.Display{Mode: settings.Windowed, Width: 1280, Height: 720, VSync: values.Display.VSync}
			}
			if values.Display != program.values.Display {
				if err = program.window.Apply(ctx, values.Display); err != nil {
					return err
				}
			}
			if err = program.sampler.SetBindings(ctx, values); err != nil {
				return err
			}
			if err = program.ui.SetSettings(ctx, values); err != nil {
				return err
			}
			if err = program.ui.NotifySettingsResult(ctx, result); err != nil {
				return err
			}
			program.values = values
		}
	}
	if program.settingsPending != "" || program.settingsDraft == nil || view.Save == session.SavePending {
		return nil
	}
	if program.settingsSequence == ^uint64(0) {
		return session.Internal
	}
	program.settingsSequence++
	id := session.RequestID(fmt.Sprintf("desktop-settings-%d", program.settingsSequence))
	request, err := session.NewSettingsRequest(ctx, session.SettingsInput{ID: id, Kind: session.SaveSettings, Values: *program.settingsDraft})
	if err != nil {
		return err
	}
	if err = store.Submit(ctx, request); err != nil {
		return err
	}
	program.settingsDraft = nil
	program.settingsPending = id
	return nil
}

// ShutdownStarted is closed before the program waits for owned work to drain.
// The process owner uses it to start its independent total shutdown budget.
func (program *Program) ShutdownStarted() <-chan struct{} {
	program.mu.Lock()
	defer program.mu.Unlock()
	if program.shutdown == nil {
		program.shutdown = make(chan struct{})
	}
	return program.shutdown
}

func (program *Program) beginShutdown(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	program.mu.Lock()
	if program.shutdown == nil {
		program.shutdown = make(chan struct{})
	}
	started := program.shutdown
	program.mu.Unlock()
	program.shutdownOnce.Do(func() { close(started) })
}

func (program *Program) finishRun(ctx context.Context, loopErr error, stop context.CancelFunc, done <-chan struct{}) (err error) {
	defer mon.Task()(&ctx)(&err)
	program.beginShutdown(ctx)
	stop()
	<-done
	program.mu.Lock()
	writerErr := program.writerErr
	program.mu.Unlock()
	return errs.Combine(loopErr, writerErr)
}

// loadSettings uses the same serialized worker as campaign writes. A recovery
// window override is session-only and is never written into player settings.
func (program *Program) loadSettings(ctx context.Context, done <-chan struct{}) (err error) {
	defer mon.Task()(&ctx)(&err)
	input := session.SettingsInput{ID: "startup-settings", Kind: session.LoadSettings}
	if program.reset {
		input.Kind = session.SaveSettings
		input.Values = settings.Defaults(ctx)
	}
	request, err := session.NewSettingsRequest(ctx, input)
	if err != nil {
		return err
	}
	store := program.store.Settings()
	if err = store.Submit(ctx, request); err != nil {
		return err
	}
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, pollErr := store.Poll(ctx, input.ID)
		if pollErr != nil {
			return pollErr
		}
		data := result.Data(ctx)
		if data.Status != session.StoragePending {
			if data.Status != session.StorageSucceeded {
				if data.Code == "" {
					return session.Internal
				}
				return data.Code
			}
			program.values = data.Values
			program.savedDisplay = data.Values.Display
			if program.windowed {
				program.values.Display = settings.Display{Mode: settings.Windowed, Width: 1280, Height: 720, VSync: program.values.Display.VSync}
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			program.mu.Lock()
			writerErr := program.writerErr
			program.mu.Unlock()
			if writerErr == nil {
				return ErrWorkerExited
			}
			return writerErr
		case <-ticker.C:
		}
	}
}

// startWriter requires mu. The owned context retains tracing values while
// ordinary cancellation is handled by the lifecycle owner after gameplay stops.
func (program *Program) startWriter(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	writerCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	program.writerStop = stop
	program.writerDone = make(chan struct{})
	go program.runWriter(writerCtx, program.writerDone)
}

func (program *Program) runWriter(ctx context.Context, done chan struct{}) {
	err := program.store.Run(ctx)
	if ctx.Err() == nil {
		err = fmt.Errorf("persistence worker: %w", errors.Join(ErrWorkerExited, err))
	}
	program.mu.Lock()
	program.writerErr = err
	cancelLoop := program.runCancel
	program.mu.Unlock()
	if errors.Is(err, ErrWorkerExited) {
		program.beginShutdown(ctx)
		if cancelLoop != nil {
			cancelLoop()
		}
	}
	close(done)
}

// Close drains any accepted store work before closing the remaining resources.
// It returns the same result on repeated calls and is safe before Run.
func (program *Program) Close() error {
	return program.close(context.Background())
}

func (program *Program) close(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	program.mu.Lock()
	closed, closeErr := program.closed, program.closeErr
	program.mu.Unlock()
	if closed {
		return closeErr
	}
	if program.ownerThread != 0 && nativeThreadID() != program.ownerThread {
		program.beginShutdown(ctx)
		program.mu.Lock()
		cancel := program.runCancel
		program.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return assets.ErrThread
	}
	program.beginShutdown(ctx)
	program.closeOnce.Do(func() {
		var failures []error
		program.mu.Lock()
		program.closing = true
		if program.store != nil && program.writerDone == nil {
			program.startWriter(ctx)
		}
		stop, done := program.writerStop, program.writerDone
		cancelLoop, runDone := program.runCancel, program.runDone
		program.mu.Unlock()
		if cancelLoop != nil {
			cancelLoop()
		}
		// Run owns the session and stops the writer after its last frame.
		// Waiting for it first prevents a concurrent Close from stopping the
		// queue while that frame is still submitting accepted work.
		if runDone != nil {
			<-runDone
		}
		if done != nil {
			stop()
			// Store.Close alone may return at its drain deadline while the
			// worker still holds dependencies. Join before releasing them;
			// the process watchdog bounds a non-cooperative worker.
			<-done
			program.mu.Lock()
			failures = append(failures, program.writerErr)
			program.mu.Unlock()
		}
		if program.store != nil {
			failures = append(failures, program.store.Close())
		}
		if program.mixer != nil {
			failures = append(failures, program.mixer.Close(ctx))
		}
		if program.renderer != nil {
			failures = append(failures, program.renderer.Close(ctx))
		}
		if program.cache != nil {
			failures = append(failures, program.cache.Close(ctx))
		}
		if program.window != nil {
			failures = append(failures, program.window.Close(ctx))
		}
		if program.recorder != nil {
			failures = append(failures, program.recorder.Close())
		}
		closeRoot := program.closeRoot
		if closeRoot == nil {
			closeRoot = func(root *os.Root) error { return root.Close() }
		}
		if program.profileRoot != nil {
			failures = append(failures, closeRoot(program.profileRoot))
		}
		if program.contentRoot != nil {
			failures = append(failures, closeRoot(program.contentRoot))
		}
		program.mu.Lock()
		program.closeErr = errs.Combine(failures...)
		program.closed = true
		program.mu.Unlock()
	})
	program.mu.Lock()
	defer program.mu.Unlock()
	return program.closeErr
}

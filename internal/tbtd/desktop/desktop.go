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

	monkit "github.com/spacemonkeygo/monkit/v3"
	"github.com/zeebo/errs"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/diagnostics"
	"too-broke-to-die/internal/tbtd/persistence"
	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

// ErrRuntimeUnavailable means the native desktop loop is not yet wired. It is
// a startup failure, not an indication that the game or its media are ready.
var ErrRuntimeUnavailable = errors.New("native desktop runtime not yet wired")

// ErrWorkerExited marks a persistence worker that stopped before shutdown.
var ErrWorkerExited = errors.New("persistence worker exited unexpectedly")

// Program owns the profile writer, local recorder, and their filesystem roots.
// Native resources are not acquired by this preliminary composition root.
type Program struct {
	contentRoot  *os.Root
	profileRoot  *os.Root
	store        *persistence.Store
	recorder     *diagnostics.Recorder
	session      *session.Service
	windowed     bool
	reset        bool
	values       settings.Values
	mu           sync.Mutex
	writerDone   chan struct{}
	writerStop   context.CancelFunc
	writerErr    error
	runCancel    context.CancelFunc
	runDone      chan struct{}
	runStarted   bool
	closing      bool
	shutdown     chan struct{}
	shutdownOnce sync.Once
	closeOnce    sync.Once
	closeErr     error
	closeRoot    func(*os.Root) error
}

// New validates authored definitions and acquires the exclusive profile writer.
// It starts no application worker. Any partial acquisition is rolled back.
func New(ctx context.Context, config Config) (_ *Program, err error) {
	defer mon.Task()(&ctx)(&err)
	return newWithFilesystem(ctx, config, filesystemBoundary{
		open:   os.OpenRoot,
		create: platform.CreateRoot,
		close:  func(root *os.Root) error { return root.Close() },
	})
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

// Run supervises the persistence worker. Until the native loop is wired it
// returns ErrRuntimeUnavailable without submitting a campaign transition.
func (program *Program) Run(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return err
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
	if loopErr == nil {
		// Native frame processing must run here on the calling, locked OS thread.
		loopErr = ErrRuntimeUnavailable
	}
	return program.finishRun(ctx, loopErr, stop, done)
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
		if runDone != nil {
			<-runDone
		}
		if program.store != nil {
			failures = append(failures, program.store.Close())
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
		program.closeErr = errs.Combine(failures...)
	})
	return program.closeErr
}

// Copyright (C) 2026 crawter
// All rights reserved.

// Command tbtd is the offline desktop process boundary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	monkit "github.com/spacemonkeygo/monkit/v3"
	"github.com/zeebo/errs"

	"too-broke-to-die/internal/tbtd/desktop"
	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
)

var mon = monkit.Package()

const shutdownBudget = 10 * time.Second

// version can be set by the build command.
var version = "dev"

type flags struct {
	windowed      bool
	resetSettings bool
	diagnostics   bool
}

func parseFlags(args []string, output io.Writer) (flags, error) {
	var result flags
	parser := flag.NewFlagSet("tbtd", flag.ContinueOnError)
	parser.SetOutput(output)
	parser.BoolVar(&result.windowed, "windowed", false, "start in recovery window")
	parser.BoolVar(&result.resetSettings, "reset-settings", false, "reset settings without deleting campaign progress")
	parser.BoolVar(&result.diagnostics, "diagnostics", false, "record additional local diagnostics")
	if err := parser.Parse(args); err != nil {
		return flags{}, err
	}
	if parser.NArg() != 0 {
		return flags{}, fmt.Errorf("unexpected positional arguments")
	}
	return result, nil
}

func init() {
	// Lock in init so main runs on the process's initial OS thread as well.
	runtime.LockOSThread()
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		reportErr := reportStartup(context.Background(), err)
		if reportErr != nil {
			fmt.Fprintln(os.Stderr, "Не удалось полностью записать сообщение диагностики.")
		}
		if errs.Combine(err, reportErr) != nil {
			os.Exit(1)
		}
	}
}

func run(ctx context.Context, args []string) (err error) {
	defer mon.Task()(&ctx)(&err)
	options, err := parseFlags(args, os.Stderr)
	if err != nil {
		return err
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	watch := startWatchdog(ctx, signals, shutdownBudget, func() { os.Exit(1) })
	var observer <-chan struct{}
	defer func() {
		watch.stop()
		if observer != nil {
			<-observer
		}
	}()
	ctx = watch.ctx

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	dataDir, err := platform.DataDirectory(ctx, platform.DirectoryInputs{})
	if err != nil {
		return fmt.Errorf("locate profile: %w", err)
	}
	account, err := user.Current()
	if err != nil {
		return fmt.Errorf("identify diagnostic redaction term: %w", err)
	}
	program, err := desktop.New(ctx, desktop.Config{
		ContentDir:     filepath.Dir(executable),
		DataDir:        dataDir,
		ProductVersion: version,
		Sensitive:      []string{account.Username},
		Windowed:       options.windowed,
		ResetSettings:  options.resetSettings,
		Diagnostics:    options.diagnostics,
		DrainBudget:    shutdownBudget,
	})
	if err != nil {
		return err
	}
	observer = watch.observeShutdown(program.ShutdownStarted())
	runErr := program.Run(ctx)
	watch.begin()
	closeErr := program.Close()
	if runErr == context.Canceled && errors.Is(ctx.Err(), context.Canceled) {
		runErr = nil
	}
	return errs.Combine(runErr, closeErr)
}

// reportStartup sends only a predefined class to the fatal boundary. The local
// startup log is separate from campaign and settings files and is bounded to
// three files. A log failure never suppresses stderr or the system message box.
func reportStartup(ctx context.Context, cause error) (err error) {
	defer mon.Task()(&ctx)(&err)
	root, file, openErr := openStartupLog(ctx)
	var log io.Writer = failedLog{err: openErr}
	if file != nil {
		log = file
	}
	reportErr := presentStartup(ctx, cause, platform.FatalSinks{Log: log})
	var cleanupErr error
	if file != nil {
		cleanupErr = errs.Combine(file.Sync(), file.Close())
	}
	if root != nil {
		cleanupErr = errs.Combine(cleanupErr, root.Close())
	}
	return errs.Combine(openErr, reportErr, cleanupErr)
}

type failedLog struct{ err error }

func (log failedLog) Write([]byte) (int, error) { return 0, log.err }

// presentStartup uses identities, never raw paths or error text, for all sinks.
func presentStartup(ctx context.Context, cause error, sinks platform.FatalSinks) (err error) {
	defer mon.Task()(&ctx)(&err)
	class := platform.FatalInternal
	switch desktop.PresentError(cause).Code {
	case session.ContentInvalid:
		class = platform.FatalContent
	case session.StorageDenied, session.StorageFull, session.StorageConflict, session.SaveCorrupt, session.SaveTooNew, session.ContentIncompatible:
		class = platform.FatalStorage
	case session.GraphicsUnavailable:
		class = platform.FatalGraphics
	}
	if errors.Is(cause, platform.ErrProfileInUse) || errors.Is(cause, platform.ErrUnsupportedStorage) {
		class = platform.FatalStorage
	}
	return platform.ShowFatal(ctx, class, sinks)
}

func openStartupLog(ctx context.Context) (_ *os.Root, _ *os.File, err error) {
	defer mon.Task()(&ctx)(&err)
	path, err := platform.DataDirectory(ctx, platform.DirectoryInputs{})
	if err != nil {
		return nil, nil, err
	}
	created, err := platform.CreateRoot(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	if err = created.Close(); err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			err = errs.Combine(err, root.Close())
		}
	}()
	if err = root.MkdirAll("logs", 0700); err != nil {
		return nil, nil, err
	}
	info, err := root.Lstat("logs")
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, errors.New("startup log directory is not a directory")
	}
	if err = root.Remove("logs/startup.2.log"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	for slot := 1; slot >= 0; slot-- {
		source := fmt.Sprintf("logs/startup.%d.log", slot)
		target := fmt.Sprintf("logs/startup.%d.log", slot+1)
		if err = root.Rename(source, target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
	}
	file, err := root.OpenFile("logs/startup.0.log", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, nil, err
	}
	return root, file, nil
}

type watchdog struct {
	ctx     context.Context
	cancel  context.CancelFunc
	beginCh chan struct{}
	done    chan struct{}
	joined  chan struct{}
	once    sync.Once
}

// startWatchdog owns one joined goroutine. Shutdown starts with the first
// signal, parent cancellation, or begin; another signal or the deadline forces
// process exit even if the initial thread is blocked in Run or Close.
func startWatchdog(parent context.Context, signals <-chan os.Signal, budget time.Duration, force func()) *watchdog {
	defer mon.Task()(&parent)(nil)
	ctx, cancel := context.WithCancel(parent)
	watch := &watchdog{ctx: ctx, cancel: cancel, beginCh: make(chan struct{}), done: make(chan struct{}), joined: make(chan struct{})}
	go func() {
		defer close(watch.joined)
		select {
		case <-parent.Done():
		case <-signals:
		case <-watch.beginCh:
		case <-watch.done:
			return
		}
		cancel()
		timer := time.NewTimer(budget)
		defer timer.Stop()
		select {
		case <-signals:
			force()
		case <-timer.C:
			force()
		case <-watch.done:
		}
	}()
	return watch
}

// observeShutdown forwards an internally requested stop even when Run is
// blocked joining the writer. The caller joins the observer during cleanup.
func (watch *watchdog) observeShutdown(shutdown <-chan struct{}) <-chan struct{} {
	ctx := watch.ctx
	defer mon.Task()(&ctx)(nil)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case <-shutdown:
			watch.begin()
		case <-watch.done:
		}
	}()
	return finished
}

func (watch *watchdog) begin() {
	watch.once.Do(func() { close(watch.beginCh) })
}

func (watch *watchdog) stop() {
	close(watch.done)
	watch.cancel()
	<-watch.joined
}

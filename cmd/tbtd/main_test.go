// Copyright (C) 2026 crawter
// All rights reserved.

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
)

func TestParseFlags(t *testing.T) {
	var output bytes.Buffer
	got, err := parseFlags([]string{"--windowed", "--reset-settings", "--diagnostics"}, &output)
	if err != nil || !got.windowed || !got.resetSettings || !got.diagnostics {
		t.Fatalf("flags: %+v %v", got, err)
	}
	got, err = parseFlags(nil, &output)
	if err != nil || got != (flags{}) {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	for _, args := range [][]string{{"--unknown"}, {"extra"}, {"--windowed=invalid"}} {
		if _, err = parseFlags(args, &output); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if _, err = parseFlags([]string{"--help"}, &output); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help: %v", err)
	}
}

func TestStartupFatalPresentation(t *testing.T) {
	for _, tc := range []struct {
		cause error
		class platform.FatalClass
	}{
		{fmt.Errorf("private content path: %w", session.ContentInvalid), platform.FatalContent},
		{fmt.Errorf("private profile path: %w", platform.ErrProfileInUse), platform.FatalStorage},
		{session.SaveTooNew, platform.FatalStorage},
		{session.SaveCorrupt, platform.FatalStorage},
		{session.GraphicsUnavailable, platform.FatalGraphics},
		{errors.New("private failure"), platform.FatalInternal},
	} {
		var stderr, log bytes.Buffer
		var box string
		sinks := platform.FatalSinks{Stderr: &stderr, Log: &log, MessageBox: func(message string) error { box = message; return nil }}
		if err := presentStartup(t.Context(), tc.cause, sinks); err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{stderr.String(), log.String(), box} {
			if !strings.Contains(text, string(tc.class)) || strings.Contains(text, "private") || !strings.Contains(text, "Не удалось") {
				t.Fatalf("unsafe or missing fatal message: %q", text)
			}
		}
	}
	logErr := errors.New("log failed")
	boxErr := errors.New("box failed")
	var stderr bytes.Buffer
	err := presentStartup(t.Context(), session.Internal, platform.FatalSinks{
		Stderr: &stderr, Log: failedLog{err: logErr}, MessageBox: func(string) error { return boxErr },
	})
	if !errors.Is(err, logErr) || !errors.Is(err, boxErr) || stderr.Len() == 0 {
		t.Fatalf("fatal sink failures lost: %v; stderr %q", err, stderr.String())
	}
}

func TestStartupLogRotation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses the Linux data directory override")
	}
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)
	for i := 0; i < 4; i++ {
		root, file, err := openStartupLog(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = fmt.Fprintf(file, "%d", i); err != nil {
			t.Fatal(err)
		}
		if err = errors.Join(file.Sync(), file.Close(), root.Close()); err != nil {
			t.Fatal(err)
		}
	}
	logs := filepath.Join(dataDir, "hexforge", "too-broke-to-die", "logs")
	for slot, want := range []string{"3", "2", "1"} {
		data, err := os.ReadFile(filepath.Join(logs, fmt.Sprintf("startup.%d.log", slot)))
		if err != nil || string(data) != want {
			t.Fatalf("slot %d: %q %v", slot, data, err)
		}
	}
}

func TestWatchdogNormalStop(t *testing.T) {
	signals := make(chan os.Signal, 2)
	forced := make(chan struct{}, 1)
	watch := startWatchdog(context.Background(), signals, time.Second, func() { forced <- struct{}{} })
	signals <- os.Interrupt
	<-watch.ctx.Done()
	watch.begin()
	watch.stop()
	select {
	case <-forced:
		t.Fatal("normal stop forced exit")
	default:
	}
}

func TestWatchdogBeginsOnParentOrExplicitShutdown(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		parent, cancel := context.WithCancel(context.Background())
		forced := make(chan struct{}, 1)
		watch := startWatchdog(parent, make(chan os.Signal), time.Second, func() { forced <- struct{}{} })
		if explicit {
			watch.begin()
			watch.begin()
		} else {
			cancel()
		}
		<-watch.ctx.Done()
		watch.stop()
		cancel()
		select {
		case <-forced:
			t.Fatal("forced exit during normal shutdown")
		default:
		}
	}
}

func TestWatchdogSubprocess(t *testing.T) {
	mode := os.Getenv("TBTD_WATCHDOG_TEST")
	if mode != "" {
		signals := make(chan os.Signal, 2)
		budget := 250 * time.Millisecond
		if mode == "second-run" || mode == "second-close" {
			budget = 30 * time.Second
		}
		watch := startWatchdog(context.Background(), signals, budget, func() { os.Exit(23) })
		// Model a blocked Run or Close on the initial goroutine, while the
		// independent watchdog receives termination from another goroutine.
		blockedRun := strings.HasSuffix(mode, "-run")
		go func() {
			if blockedRun {
				signals <- os.Interrupt
			}
			<-watch.ctx.Done()
			if mode == "second-run" || mode == "second-close" {
				signals <- os.Interrupt
			}
		}()
		if blockedRun {
			select {} // Run cannot return.
		}
		watch.begin() // Run returned; Close cannot return.
		select {}
	}
	for _, mode := range []string{"deadline-run", "deadline-close", "second-run", "second-close"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWatchdogSubprocess$")
			cmd.Env = append(os.Environ(), "TBTD_WATCHDOG_TEST="+mode)
			start := time.Now()
			output, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
				t.Fatalf("child did not force exit: %v %s", err, output)
			}
			elapsed := time.Since(start)
			if (mode == "deadline-run" || mode == "deadline-close") && elapsed < 200*time.Millisecond {
				t.Fatalf("deadline fired early: %v", elapsed)
			}
			if elapsed > 3*time.Second {
				t.Fatalf("watchdog too slow: %v", elapsed)
			}
		})
	}
}

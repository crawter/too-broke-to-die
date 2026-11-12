// Copyright (C) 2026 crawter
// All rights reserved.

package platform

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

func testRoot(t *testing.T) (*Root, string) {
	t.Helper()
	path := t.TempDir()
	root, err := OpenRoot(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root, path
}

func TestDataDirectoryWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows known-folder adapter")
	}
	failure := errors.New("known folder failure")
	for _, tc := range []struct {
		base    string
		err     error
		want    string
		wantErr error
	}{
		{base: `C:\Users\Player\AppData\Local`, want: `C:\Users\Player\AppData\Local\Hexforge\TooBrokeToDie`},
		{base: "relative", wantErr: ErrInvalidPath},
		{err: failure, wantErr: failure},
	} {
		path, err := DataDirectory(t.Context(), DirectoryInputs{LocalAppData: func() (string, error) { return tc.base, tc.err }})
		if path != tc.want || !errors.Is(err, tc.wantErr) {
			t.Errorf("got %q, %v; want %q, %v", path, err, tc.want, tc.wantErr)
		}
	}
}

func TestWindowsNetworkPath(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{path: `\\?\UNC\server\share\profile`, want: true},
		{path: `\\?\unc\server\share`, want: true},
		{path: `\\server\share`, want: true},
		{path: `\\?\C:\local`},
		{path: `\\.\C:\local`},
		{path: `C:\local`},
	} {
		if got := windowsNetworkPath(tc.path); got != tc.want {
			t.Errorf("network path %q: %v", tc.path, got)
		}
	}
}

func TestCreateRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "profile")
	root, err := CreateRoot(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	root, err = CreateRoot(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateRoot(t.Context(), "relative"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("relative root: %v", err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateRoot(t.Context(), filepath.Join(file, "child")); err == nil {
		t.Fatal("created root under a file")
	}
}

func TestCreateRootRetriesFlushAncestors(t *testing.T) {
	for _, stage := range []string{"partial mkdir", "directory sync"} {
		t.Run(stage, func(t *testing.T) {
			ctx := t.Context()
			base := t.TempDir()
			parent := filepath.Join(base, "nested")
			path := filepath.Join(parent, "profile")
			failure := errors.New("injected creation failure")
			mkdir := os.MkdirAll
			syncDir := syncDirectory
			if stage == "partial mkdir" {
				mkdir = func(_ string, mode os.FileMode) error {
					if err := os.MkdirAll(parent, mode); err != nil {
						return err
					}
					return failure
				}
			} else {
				syncDir = func(ctx context.Context, file *os.File) error {
					if file.Name() == parent {
						return failure
					}
					return syncDirectory(ctx, file)
				}
			}
			root, err := createRoot(ctx, path, mkdir, syncDir)
			if root != nil || !errors.Is(err, failure) {
				t.Fatalf("first attempt: root=%v, err=%v", root, err)
			}
			if _, err := os.Stat(parent); err != nil {
				t.Fatalf("partial tree not created: %v", err)
			}
			// A retry must not treat the already-present target or parent as
			// evidence that an earlier failed flush has become durable.
			root, err = createRoot(ctx, path, os.MkdirAll, func(ctx context.Context, file *os.File) error {
				if file.Name() == parent {
					return failure
				}
				return syncDirectory(ctx, file)
			})
			if root != nil || !errors.Is(err, failure) {
				t.Fatalf("persistent failure on retry: root=%v, err=%v", root, err)
			}
			var visited []string
			root, err = createRoot(ctx, path, os.MkdirAll, func(ctx context.Context, file *os.File) error {
				visited = append(visited, file.Name())
				return syncDirectory(ctx, file)
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := root.Close(); err != nil {
				t.Fatal(err)
			}
			var want []string
			for current := path; ; current = filepath.Dir(current) {
				want = append(want, current)
				if filepath.Dir(current) == current {
					break
				}
			}
			if !slices.Equal(visited, want) {
				t.Fatalf("flushed %v, want %v", visited, want)
			}
		})
	}
}

func TestRootAndBoundedFiles(t *testing.T) {
	ctx := t.Context()
	root, path := testRoot(t)
	if _, err := OpenRoot(ctx, "relative"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("relative root: %v", err)
	}
	if err := root.MkdirAll(ctx, filepath.Join("campaigns", "one")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", ".", "..", "../outside", "a" + string(filepath.Separator) + ".." + string(filepath.Separator) + "b", filepath.Join(path, "absolute")} {
		if _, err := root.ReadFile(ctx, name, 20); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("read %q: %v", name, err)
		}
		if err := root.MkdirAll(ctx, name); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("mkdir %q: %v", name, err)
		}
	}
	if _, err := root.WriteTemp(ctx, ".", []byte("abc"), 2); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized write: %v", err)
	}
	if _, err := root.WriteTemp(ctx, ".", nil, -1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("negative bound: %v", err)
	}
	name, err := root.WriteTemp(ctx, filepath.Join("campaigns", "one"), []byte("abc"), 3)
	if err != nil {
		t.Fatal(err)
	}
	data, err := root.ReadFile(ctx, name, 3)
	if err != nil || string(data) != "abc" {
		t.Fatalf("read %q: %v", data, err)
	}
	for _, max := range []int64{-1, 0, 2} {
		if _, err := root.ReadFile(ctx, name, max); !errors.Is(err, ErrTooLarge) {
			t.Errorf("read bound %d: %v", max, err)
		}
	}
	if err := root.SyncFile(ctx, name); err != nil {
		t.Fatal(err)
	}
	if _, err := root.ReadFile(ctx, "absent", 1); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
	if err := root.SyncFile(ctx, "absent"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing sync: %v", err)
	}
	if _, err := root.ReadFile(ctx, "campaigns", 5); err == nil {
		t.Fatal("read directory succeeded")
	}
	empty, err := root.WriteTemp(ctx, ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err = root.ReadFile(ctx, empty, 0)
	if err != nil || len(data) != 0 {
		t.Fatalf("empty file: %q %v", data, err)
	}
}

func TestSymlinkBoundary(t *testing.T) {
	ctx := t.Context()
	root, path := testRoot(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(path, "escape")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := root.ReadFile(ctx, filepath.Join("escape", "secret"), 100); err == nil {
		t.Fatal("read through escaping symlink")
	}
	if err := root.MkdirAll(ctx, filepath.Join("escape", "dir")); err == nil {
		t.Fatal("mkdir through escaping symlink")
	}
	if _, err := root.WriteTemp(ctx, "escape", []byte("x"), 1); err == nil {
		t.Fatal("write through escaping symlink")
	}
	if _, err := root.ReplaceFile(ctx, filepath.Join("escape", "a"), filepath.Join("escape", "b")); err == nil {
		t.Fatal("rename through escaping symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "dir")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside changed: %v", err)
	}
	if err := root.MkdirAll(ctx, "inside"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inside", filepath.Join(path, "internal-link")); err != nil {
		t.Fatal(err)
	}
	name, err := root.WriteTemp(ctx, "internal-link", []byte("safe"), 4)
	if err != nil {
		t.Fatal(err)
	}
	data, err := root.ReadFile(ctx, name, 4)
	if err != nil || string(data) != "safe" {
		t.Fatalf("internal symlink: %q %v", data, err)
	}
}

type testWriter struct {
	n        int
	writeErr error
	syncErr  error
	closeErr error
	synced   bool
	closed   bool
}

func (writer *testWriter) Write([]byte) (int, error) { return writer.n, writer.writeErr }

func (writer *testWriter) Sync() error {
	writer.synced = true
	return writer.syncErr
}

func (writer *testWriter) Close() error {
	writer.closed = true
	return writer.closeErr
}

// faultFile injects external file failures while exercising real temporary files.
type faultFile struct {
	file *os.File
	testWriter
}

func (file *faultFile) Write(data []byte) (int, error) {
	count := file.n
	if count > len(data) {
		count = len(data)
	}
	n, err := file.file.Write(data[:count])
	return n, errors.Join(err, file.writeErr)
}

func (file *faultFile) Sync() error {
	file.synced = true
	return errors.Join(file.file.Sync(), file.syncErr)
}

func (file *faultFile) Close() error {
	file.closed = true
	return errors.Join(file.file.Close(), file.closeErr)
}

func TestWriteSyncedFailures(t *testing.T) {
	writeFailure := errors.New("write failure")
	syncFailure := errors.New("sync failure")
	closeFailure := errors.New("close failure")
	for _, tc := range []struct {
		name       string
		writer     testWriter
		want       []error
		wantSynced bool
	}{
		{name: "success", writer: testWriter{n: 3}, wantSynced: true},
		{name: "short", writer: testWriter{n: 2}, want: []error{io.ErrShortWrite}},
		{name: "write and close", writer: testWriter{n: 1, writeErr: writeFailure, closeErr: closeFailure}, want: []error{writeFailure, closeFailure}},
		{name: "sync and close", writer: testWriter{n: 3, syncErr: syncFailure, closeErr: closeFailure}, want: []error{syncFailure, closeFailure}, wantSynced: true},
		{name: "close", writer: testWriter{n: 3, closeErr: closeFailure}, want: []error{closeFailure}, wantSynced: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "fault-")
			if err != nil {
				t.Fatal(err)
			}
			writer := &faultFile{file: file, testWriter: tc.writer}
			err = writeSynced(t.Context(), writer, []byte("abc"))
			if len(tc.want) == 0 && err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !errors.Is(err, want) {
					t.Errorf("error %v does not contain %v", err, want)
				}
			}
			if !writer.closed || writer.synced != tc.wantSynced {
				t.Errorf("closed=%v synced=%v", writer.closed, writer.synced)
			}
		})
	}
}

func TestReplaceFile(t *testing.T) {
	ctx := t.Context()
	root, path := testRoot(t)
	for name, value := range map[string]string{"a": "old", "b": "valid"} {
		if err := os.WriteFile(filepath.Join(path, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	temp, err := root.WriteTemp(ctx, ".", []byte("new"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := root.ReplaceFile(ctx, temp, filepath.Join("different", "a")); !errors.Is(err, ErrInvalidPath) || result.Installed {
		t.Fatalf("different directory: %+v %v", result, err)
	}
	if result, err := root.ReplaceFile(ctx, "missing", "a"); err == nil || result.Installed {
		t.Fatalf("missing source: %+v %v", result, err)
	}
	if data, err := root.ReadFile(ctx, "a", 10); err != nil || string(data) != "old" {
		t.Fatalf("old slot lost: %q %v", data, err)
	}
	result, err := root.ReplaceFile(ctx, temp, "a")
	if err != nil || !result.Installed {
		t.Fatalf("replace: %+v %v", result, err)
	}
	if err := root.SyncFile(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"a": "new", "b": "valid"} {
		data, err := root.ReadFile(ctx, name, 10)
		if err != nil || string(data) != want {
			t.Errorf("%s=%q, %v", name, data, err)
		}
	}
	if _, err := root.ReadFile(ctx, temp, 3); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp still present: %v", err)
	}
	temp, err = root.WriteTemp(ctx, ".", []byte("next"), 4)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("directory sync failed")
	result, err = root.replaceFile(ctx, temp, "a", func(context.Context, *os.File) error { return failure })
	if !result.Installed || !errors.Is(err, failure) {
		t.Fatalf("ambiguous replacement: %+v %v", result, err)
	}
	data, err := root.ReadFile(ctx, "a", 10)
	if err != nil || string(data) != "next" {
		t.Fatalf("installed replacement: %q %v", data, err)
	}
	data, err = root.ReadFile(ctx, "b", 10)
	if err != nil || string(data) != "valid" {
		t.Fatalf("other revision: %q %v", data, err)
	}
}

func TestLockProfile(t *testing.T) {
	ctx := t.Context()
	root, path := testRoot(t)
	if err := os.WriteFile(filepath.Join(path, "profile.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	lock, err := root.LockProfile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	second, err := root.LockProfile(ctx)
	if !errors.Is(err, ErrProfileInUse) || second != nil {
		t.Fatalf("contention: %v, %v", second, err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err = root.LockProfile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentLockClose(t *testing.T) {
	root, _ := testRoot(t)
	lock, err := root.LockProfile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	failures := make(chan error, 16)
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := lock.Close(); err != nil {
				failures <- err
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	lock, err = root.LockProfile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestShowFatal(t *testing.T) {
	var stderr, log bytes.Buffer
	var message string
	sinks := FatalSinks{Stderr: &stderr, Log: &log, MessageBox: func(s string) error { message = s; return nil }}
	if err := ShowFatal(t.Context(), FatalGraphics, sinks); err != nil {
		t.Fatal(err)
	}
	if stderr.String() != log.String() || strings.TrimSpace(stderr.String()) != message || !strings.Contains(message, "GraphicsUnavailable") || !strings.Contains(message, "logs") {
		t.Fatalf("fatal routing: %q %q %q", stderr.String(), log.String(), message)
	}
	stderr.Reset()
	log.Reset()
	failure := errors.New("sink failure")
	sinks.Stderr = &testWriter{n: 0, writeErr: failure}
	sinks.MessageBox = func(s string) error { message = s; return failure }
	if err := ShowFatal(t.Context(), FatalClass("/secret/password"), sinks); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if strings.Contains(message, "secret") || !strings.Contains(message, "Internal") || !strings.Contains(log.String(), "Internal") {
		t.Fatalf("unsafe or missing fallback message: %q %q", message, log.String())
	}
	if err := ShowFatal(t.Context(), FatalStorage, FatalSinks{Stderr: &stderr, MessageBox: func(string) error { return nil }}); err == nil {
		t.Fatal("missing sinks accepted")
	}
	if err := writeMessage(t.Context(), &testWriter{n: 0}, "x"); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short message: %v", err)
	}
}

// Exercise concurrent ownership and interrupted-process recovery under the
// race detector without recursively running this gate.
func TestLinuxRaceGate(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux race gate")
	}
	cmd := exec.CommandContext(t.Context(), "go", "test", "-race", "-count=1", "-run", "^(TestConcurrentLockClose|TestLockProfile|TestInterruptedOwnerReleasesLock)$", ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("race tests: %v\n%s", err, output)
	}
}

// Run the Windows vet/build gate from the display-free Linux test lane. The
// Windows test binary is compiled but not executed and opens no native device.
func TestWindowsCrossBuild(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux cross-build gate")
	}
	root := filepath.Join("..", "..", "..")
	for _, args := range [][]string{{"vet", "./..."}, {"build", "./..."}, {"test", "-c", "-o", filepath.Join(t.TempDir(), "platform.test.exe"), "."}} {
		cmd := exec.CommandContext(t.Context(), "go", args...)
		if args[0] != "test" {
			cmd.Dir = root
		}
		cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, output)
		}
	}
}

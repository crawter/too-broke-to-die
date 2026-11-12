// Copyright (C) 2026 crawter
// All rights reserved.

//go:build linux

package platform

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDataDirectoryLinux(t *testing.T) {
	failure := errors.New("home failure")
	for _, tc := range []struct {
		name    string
		xdg     string
		home    string
		homeErr error
		want    string
		wantErr error
	}{
		{name: "xdg", xdg: "/data", home: "/unused", want: "/data/hexforge/too-broke-to-die"},
		{name: "home", home: "/home/player", want: "/home/player/.local/share/hexforge/too-broke-to-die"},
		{name: "relative xdg", xdg: "relative", wantErr: ErrInvalidPath},
		{name: "relative home", home: "relative", wantErr: ErrInvalidPath},
		{name: "empty home", wantErr: ErrInvalidPath},
		{name: "home failure", homeErr: failure, wantErr: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, err := DataDirectory(t.Context(), DirectoryInputs{
				Getenv: func(key string) string {
					if key != "XDG_DATA_HOME" {
						t.Errorf("unexpected environment key %q", key)
					}
					return tc.xdg
				},
				HomeDir: func() (string, error) { return tc.home, tc.homeErr },
			})
			if path != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %q, %v; want %q, %v", path, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestStorageClassificationLinux(t *testing.T) {
	for _, kind := range []uint64{0x6969, 0xff534d42, 0x65735546, 0x01021994, 0x01021997} {
		if status := linuxStorageType(kind); status != StorageUnsupported {
			t.Errorf("%x: %s", kind, status)
		}
	}
	for _, kind := range []uint64{0xef53, 0x794c7630, 0x12345678} {
		if status := linuxStorageType(kind); status != StorageUnknown {
			t.Errorf("%x falsely supported: %s", kind, status)
		}
	}
	root, _ := testRoot(t)
	status, err := root.CheckStorage(t.Context())
	if err != nil && !errors.Is(err, ErrUnsupportedStorage) {
		t.Fatal(err)
	}
	if (status == StorageUnsupported) != errors.Is(err, ErrUnsupportedStorage) {
		t.Fatalf("inconsistent storage result %q: %v", status, err)
	}
}

func TestLinuxRemovable(t *testing.T) {
	sysRoot := t.TempDir()
	dev := unix.Mkdev(8, 1)
	if removable, err := linuxRemovable(t.Context(), sysRoot, dev); err != nil || removable {
		t.Fatalf("missing sysfs: %v %v", removable, err)
	}
	partition := filepath.Join(sysRoot, "devices", "pci", "block", "disk", "partition")
	links := filepath.Join(sysRoot, "dev", "block")
	for _, dir := range []string{partition, links} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(links, "8:1")
	if err := os.Symlink(partition, link); err != nil {
		t.Fatal(err)
	}
	flag := filepath.Join(filepath.Dir(partition), "removable")
	for _, tc := range []struct {
		value     string
		removable bool
		bad       bool
	}{
		{value: "1\n", removable: true},
		{value: "0\n"},
		{value: "bad", bad: true},
	} {
		if err := os.WriteFile(flag, []byte(tc.value), 0600); err != nil {
			t.Fatal(err)
		}
		removable, err := linuxRemovable(t.Context(), sysRoot, dev)
		if removable != tc.removable || (err != nil) != tc.bad {
			t.Errorf("flag %q: %v %v", tc.value, removable, err)
		}
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if _, err := linuxRemovable(t.Context(), sysRoot, dev); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("escaped sysfs: %v", err)
	}
}

func TestLockRejectsEscapingSymlink(t *testing.T) {
	root, path := testRoot(t)
	outside := filepath.Join(t.TempDir(), "other-lock")
	if err := os.Symlink(outside, filepath.Join(path, "profile.lock")); err != nil {
		t.Fatal(err)
	}
	lock, err := root.LockProfile(t.Context())
	if err == nil || lock != nil {
		t.Fatalf("escaped lock: %v %v", lock, err)
	}
	if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside lock created: %v", err)
	}
}

// This test becomes a child process only when explicitly invoked by its parent.
func TestLockOwnerProcess(t *testing.T) {
	path := os.Getenv("TBTD_LOCK_CHILD_ROOT")
	if path == "" {
		return
	}
	root, err := OpenRoot(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := root.LockProfile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(os.Stdout, "READY"); err != nil {
		t.Fatal(err)
	}
	var input [1]byte
	_, _ = os.Stdin.Read(input[:])
	if err := lock.Close(); err != nil {
		t.Error(err)
	}
	if err := root.Close(); err != nil {
		t.Error(err)
	}
}

func TestInterruptedOwnerReleasesLock(t *testing.T) {
	root, path := testRoot(t)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestLockOwnerProcess$")
	cmd.Env = append(os.Environ(), "TBTD_LOCK_CHILD_ROOT="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "READY\n" {
		t.Fatalf("child readiness %q: %v", line, err)
	}
	if lock, err := root.LockProfile(t.Context()); !errors.Is(err, ErrProfileInUse) || lock != nil {
		t.Fatalf("live child: %v, %v", lock, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed process exited successfully")
	}
	lock, err := root.LockProfile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

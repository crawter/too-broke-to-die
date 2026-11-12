// Copyright (C) 2026 crawter
// All rights reserved.

//go:build linux

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func dataDirectory(ctx context.Context, inputs DirectoryInputs) (_ string, err error) {
	defer mon.Task()(&ctx)(&err)
	getenv := inputs.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	base := getenv("XDG_DATA_HOME")
	if base == "" {
		home := inputs.HomeDir
		if home == nil {
			home = os.UserHomeDir
		}
		base, err = home()
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(base) {
			return "", ErrInvalidPath
		}
		base = filepath.Join(base, ".local", "share")
	}
	if !filepath.IsAbs(base) {
		return "", ErrInvalidPath
	}
	return filepath.Join(base, "hexforge", "too-broke-to-die"), nil
}

func lockFile(ctx context.Context, file *os.File) (err error) {
	defer mon.Task()(&ctx)(&err)
	err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return errors.Join(ErrProfileInUse, err)
	}
	return err
}

func unlockFile(ctx context.Context, file *os.File) (err error) {
	defer mon.Task()(&ctx)(&err)
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func syncDirectory(ctx context.Context, file *os.File) (err error) {
	defer mon.Task()(&ctx)(&err)
	return file.Sync()
}

// Filesystem type alone cannot prove a local disk, including for overlay and
// FUSE. Known remote and nonpersistent filesystems are reported as unsupported.
func storageStatus(ctx context.Context, file *os.File, _ string) (_ StorageStatus, err error) {
	defer mon.Task()(&ctx)(&err)
	var stat unix.Statfs_t
	if err := unix.Fstatfs(int(file.Fd()), &stat); err != nil {
		return StorageUnknown, err
	}
	if linuxStorageType(uint64(stat.Type)) == StorageUnsupported {
		return StorageUnsupported, nil
	}
	var device unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &device); err != nil {
		return StorageUnknown, err
	}
	removable, err := linuxRemovable(ctx, "/sys", uint64(device.Dev))
	if err != nil {
		return StorageUnknown, err
	}
	if removable {
		return StorageUnsupported, nil
	}
	return StorageUnknown, nil
}

// Sysfs may be unavailable, and device mapper, overlays and USB bridges can
// hide the underlying medium. Absence of a removable flag is not proof of a
// fixed local disk. A partition's flag is usually on its parent device.
func linuxRemovable(ctx context.Context, sysRoot string, dev uint64) (_ bool, err error) {
	defer mon.Task()(&ctx)(&err)
	devices := filepath.Join(sysRoot, "devices")
	link := filepath.Join(sysRoot, "dev", "block", fmt.Sprintf("%d:%d", unix.Major(dev), unix.Minor(dev)))
	current, err := filepath.EvalSymlinks(link)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	relative, err := filepath.Rel(devices, current)
	if err != nil || !filepath.IsLocal(relative) {
		return false, ErrInvalidPath
	}
	for {
		value, readErr := os.ReadFile(filepath.Join(current, "removable"))
		if readErr == nil {
			switch strings.TrimSpace(string(value)) {
			case "1":
				return true, nil
			case "0":
			default:
				return false, errors.New("invalid sysfs removable flag")
			}
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return false, readErr
		}
		if current == devices {
			return false, nil
		}
		current = filepath.Dir(current)
	}
}

func linuxStorageType(kind uint64) StorageStatus {
	switch kind {
	case 0x6969, 0xff534d42, 0x517b, 0x65735546, 0x01021994,
		0x858458f6, 0x01021997, 0x564c, 0x00c36400, 0x5346414f,
		0x9fa0, 0x62656572:
		return StorageUnsupported // NFS, CIFS, SMB, FUSE, tmpfs, ramfs, 9p, NCP, Ceph, AFS, proc, sysfs
	default:
		return StorageUnknown
	}
}

func showSystemMessage(context.Context, string) error { return nil }

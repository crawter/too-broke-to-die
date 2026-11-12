// Copyright (C) 2026 crawter
// All rights reserved.

//go:build windows

package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func dataDirectory(ctx context.Context, inputs DirectoryInputs) (_ string, err error) {
	defer mon.Task()(&ctx)(&err)
	knownFolder := inputs.LocalAppData
	if knownFolder == nil {
		knownFolder = func() (string, error) {
			return windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
		}
	}
	base, err := knownFolder()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(base) {
		return "", ErrInvalidPath
	}
	return filepath.Join(base, "Hexforge", "TooBrokeToDie"), nil
}

func lockFile(ctx context.Context, file *os.File) (err error) {
	defer mon.Task()(&ctx)(&err)
	var offset windows.Overlapped
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &offset)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errors.Join(ErrProfileInUse, err)
	}
	return err
}

func unlockFile(ctx context.Context, file *os.File) (err error) {
	defer mon.Task()(&ctx)(&err)
	var offset windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &offset)
}

// The PDD only requires a directory flush on Linux.
func syncDirectory(context.Context, *os.File) error { return nil }

func storageStatus(ctx context.Context, file *os.File, _ string) (_ StorageStatus, err error) {
	defer mon.Task()(&ctx)(&err)
	// Resolve from the open handle so junctions do not hide the actual volume.
	buffer := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(file.Fd()), &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return StorageUnknown, err
	}
	if n == 0 || n >= uint32(len(buffer)) {
		return StorageUnknown, errors.New("cannot resolve storage volume")
	}
	path := windows.UTF16ToString(buffer[:n])
	// Network paths can lack a volume mount point, so recognize them before
	// asking GetVolumePathName for a drive type.
	if windowsNetworkPath(path) {
		return StorageUnsupported, nil
	}
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return StorageUnknown, err
	}
	volume := make([]uint16, 32768)
	if err = windows.GetVolumePathName(path16, &volume[0], uint32(len(volume))); err != nil {
		return StorageUnknown, err
	}
	switch windows.GetDriveType(&volume[0]) {
	case windows.DRIVE_REMOTE, windows.DRIVE_REMOVABLE, windows.DRIVE_CDROM, windows.DRIVE_RAMDISK:
		return StorageUnsupported, nil
	default:
		return StorageUnknown, nil
	}
}

func showSystemMessage(ctx context.Context, message string) (err error) {
	defer mon.Task()(&ctx)(&err)
	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return err
	}
	title, err := windows.UTF16PtrFromString("TOO BROKE TO DIE")
	if err != nil {
		return err
	}
	_, err = windows.MessageBox(0, text, title, 0x10) // MB_OK | MB_ICONERROR
	return err
}

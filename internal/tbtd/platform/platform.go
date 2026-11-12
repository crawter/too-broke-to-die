// Copyright (C) 2026 crawter
// All rights reserved.

// Package platform owns the local filesystem and operating-system boundaries.
package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	monkit "github.com/spacemonkeygo/monkit/v3"
)

var mon = monkit.Package()

var (
	// ErrInvalidPath indicates an invalid relative name or data-directory input.
	ErrInvalidPath = errors.New("invalid platform path")
	// ErrTooLarge means a complete file exceeds the caller's bound.
	ErrTooLarge = errors.New("file exceeds bound")
	// ErrProfileInUse means another process owns the profile.
	ErrProfileInUse = errors.New("profile is already in use")
	// ErrUnsupportedStorage identifies a detected unsupported filesystem.
	ErrUnsupportedStorage = errors.New("unsupported storage")
)

// DirectoryInputs supplies OS directory inputs; nil functions use the real OS.
// Windows uses LocalAppData, while Linux uses Getenv and HomeDir.
type DirectoryInputs struct {
	Getenv       func(string) string
	HomeDir      func() (string, error)
	LocalAppData func() (string, error)
}

// DataDirectory returns the per-user data directory without creating it.
func DataDirectory(ctx context.Context, inputs DirectoryInputs) (_ string, err error) {
	defer mon.Task()(&ctx)(&err)
	return dataDirectory(ctx, inputs)
}

// Root owns an open directory handle. All names accepted by its operations
// are relative to it; os.Root also confines symlink traversal. The configured
// root itself is trusted and may be a symlink. Close only after all users stop.
type Root struct {
	dir *os.Root
}

// OpenRoot opens an existing, trusted data directory. The caller owns Close.
func OpenRoot(ctx context.Context, path string) (_ *Root, err error) {
	defer mon.Task()(&ctx)(&err)
	if !filepath.IsAbs(path) {
		return nil, ErrInvalidPath
	}
	dir, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	return &Root{dir: dir}, nil
}

// CreateRoot creates the trusted data directory if necessary and flushes its
// ancestor chain on Linux. A failure may leave a partial tree; no profile
// writer should start until this succeeds.
func CreateRoot(ctx context.Context, path string) (_ *Root, err error) {
	defer mon.Task()(&ctx)(&err)
	return createRoot(ctx, path, os.MkdirAll, syncDirectory)
}

func createRoot(ctx context.Context, path string, mkdirAll func(string, os.FileMode) error, syncDir func(context.Context, *os.File) error) (_ *Root, err error) {
	defer mon.Task()(&ctx)(&err)
	if !filepath.IsAbs(path) {
		return nil, ErrInvalidPath
	}
	path = filepath.Clean(path)
	if err = mkdirAll(path, 0700); err != nil {
		return nil, err
	}
	// A prior attempt may have created any ancestor before failing to flush
	// it. Existence cannot establish durability, so repeat the entire walk.
	for current := path; ; current = filepath.Dir(current) {
		file, openErr := os.Open(current)
		if openErr != nil {
			return nil, openErr
		}
		if err = errors.Join(syncDir(ctx, file), file.Close()); err != nil {
			return nil, err
		}
		if parent := filepath.Dir(current); parent == current {
			break
		}
	}
	return OpenRoot(ctx, path)
}

// Close releases the directory handle. It must not race with its users.
func (root *Root) Close() error { return root.close(context.Background()) }

func (root *Root) close(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	return root.dir.Close()
}

func validName(name string) bool {
	return name != "." && filepath.IsLocal(name) && filepath.Clean(name) == name
}

// Windows returns UNC paths with or without the extended-length prefix.
func windowsNetworkPath(path string) bool {
	return strings.HasPrefix(strings.ToUpper(path), `\\?\UNC\`) || (strings.HasPrefix(path, `\\`) && !strings.HasPrefix(path, `\\?\`) && !strings.HasPrefix(path, `\\.\`))
}

// MkdirAll creates private directories beneath the root. It flushes each
// directory and its parent on Linux; the root's own creation is the caller's
// responsibility. A failure may leave a partially created tree.
func (root *Root) MkdirAll(ctx context.Context, name string) (err error) {
	defer mon.Task()(&ctx)(&err)
	if !validName(name) {
		return ErrInvalidPath
	}
	if err = root.dir.MkdirAll(name, 0700); err != nil {
		return err
	}
	for current := name; ; current = filepath.Dir(current) {
		file, openErr := root.dir.Open(current)
		if openErr != nil {
			return openErr
		}
		syncErr := syncDirectory(ctx, file)
		closeErr := file.Close()
		if err = errors.Join(syncErr, closeErr); err != nil {
			return err
		}
		if current == "." {
			return nil
		}
	}
}

// StorageStatus reports only what can be detected, not a durability promise.
type StorageStatus string

const (
	// StorageUnknown includes locations whose actual backing or sync behavior cannot be established.
	StorageUnknown StorageStatus = "unknown"
	// StorageUnsupported is a detected network, temporary or removable location.
	StorageUnsupported StorageStatus = "unsupported"
)

// CheckStorage returns an unsupported identity for known unsuitable storage.
// Even an unknown result cannot rule out removable devices or sync folders.
func (root *Root) CheckStorage(ctx context.Context) (_ StorageStatus, err error) {
	defer mon.Task()(&ctx)(&err)
	file, err := root.dir.Open(".")
	if err != nil {
		return StorageUnknown, err
	}
	status, checkErr := storageStatus(ctx, file, root.dir.Name())
	err = errors.Join(checkErr, file.Close())
	if err != nil {
		return StorageUnknown, err
	}
	if status == StorageUnsupported {
		return status, ErrUnsupportedStorage
	}
	return status, nil
}

// ProfileLock holds the OS lock until Close. Never remove or replace profile.lock
// while the profile is in use: the file's existence alone does not imply a lock.
type ProfileLock struct {
	mu   sync.Mutex
	file *os.File
}

// LockProfile acquires an exclusive nonblocking OS lock before a writer starts.
func (root *Root) LockProfile(ctx context.Context) (_ *ProfileLock, err error) {
	defer mon.Task()(&ctx)(&err)
	file, err := root.dir.OpenFile("profile.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errors.New("profile lock is not a regular file")
	}
	if err == nil {
		err = lockFile(ctx, file)
	}
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return &ProfileLock{file: file}, nil
}

// Close releases the lock and handle once, even after an unlock failure.
func (lock *ProfileLock) Close() error { return lock.close(context.Background()) }

func (lock *ProfileLock) close(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if lock.file == nil {
		return nil
	}
	file := lock.file
	lock.file = nil
	return errors.Join(unlockFile(ctx, file), file.Close())
}

// WriteTemp writes a complete bounded payload to a new private file in dir,
// synchronizes and closes it. It returns its relative name for reopening and
// validation before replacement. Failed writes leave no intentional temp file.
func (root *Root) WriteTemp(ctx context.Context, dir string, data []byte, max int64) (_ string, err error) {
	defer mon.Task()(&ctx)(&err)
	if dir != "." && !validName(dir) {
		return "", ErrInvalidPath
	}
	if max < 0 || int64(len(data)) > max {
		return "", ErrTooLarge
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return "", err
	}
	name := filepath.Join(dir, ".tmp-"+hex.EncodeToString(random[:]))
	file, err := root.dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	if err = writeSynced(ctx, file, data); err != nil {
		return "", errors.Join(err, root.dir.Remove(name))
	}
	return name, nil
}

type syncWriter interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

// writeSynced owns file, checks the exact count and always closes it.
func writeSynced(ctx context.Context, file syncWriter, data []byte) (err error) {
	defer mon.Task()(&ctx)(&err)
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	var syncErr error
	if writeErr == nil {
		syncErr = file.Sync()
	}
	return errors.Join(writeErr, syncErr, file.Close())
}

// ReadFile reopens a regular file, returning an error rather than truncated
// data when it exceeds max. Close failures are reported.
func (root *Root) ReadFile(ctx context.Context, name string, max int64) (_ []byte, err error) {
	defer mon.Task()(&ctx)(&err)
	if !validName(name) {
		return nil, ErrInvalidPath
	}
	if max < 0 {
		return nil, ErrTooLarge
	}
	file, err := root.dir.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, max))
	if err != nil {
		return nil, err
	}
	var extra [1]byte
	n, readErr := file.Read(extra[:])
	if n > 0 {
		return nil, ErrTooLarge
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, readErr
	}
	return data, nil
}

// SyncFile reopens and flushes an installed regular file before acknowledgment.
func (root *Root) SyncFile(ctx context.Context, name string) (err error) {
	defer mon.Task()(&ctx)(&err)
	if !validName(name) {
		return ErrInvalidPath
	}
	file, err := root.dir.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	return file.Sync()
}

// ReplaceResult distinguishes an installed file from a failure before rename.
// An installed result with an error requires the consumer to resolve the slot.
type ReplaceResult struct {
	Installed bool
}

// ReplaceFile replaces one slot with a temporary file in the same directory.
// It never deletes the destination first, and never touches the other slot.
// os.Root.Rename has os.Rename semantics while holding the parent directory
// handle, preventing a symlink swap from escaping the data root. Go does not
// guarantee atomic rename on non-Unix systems. The caller must reopen, flush,
// and validate the installed file before acknowledging it.
func (root *Root) ReplaceFile(ctx context.Context, source, destination string) (result ReplaceResult, err error) {
	defer mon.Task()(&ctx)(&err)
	return root.replaceFile(ctx, source, destination, syncDirectory)
}

func (root *Root) replaceFile(ctx context.Context, source, destination string, syncDir func(context.Context, *os.File) error) (result ReplaceResult, err error) {
	defer mon.Task()(&ctx)(&err)
	if !validName(source) || !validName(destination) || source == destination || filepath.Dir(source) != filepath.Dir(destination) {
		return result, ErrInvalidPath
	}
	dir, err := root.dir.OpenRoot(filepath.Dir(source))
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	folder, err := dir.Open(".")
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, folder.Close()) }()
	if err = dir.Rename(filepath.Base(source), filepath.Base(destination)); err != nil {
		return result, err
	}
	result.Installed = true
	err = syncDir(ctx, folder)
	return result, err
}

// FatalClass identifies a safe, predefined startup diagnostic.
type FatalClass string

const (
	// FatalGraphics reports that the graphics system cannot start.
	FatalGraphics FatalClass = "GraphicsUnavailable"
	// FatalStorage reports inaccessible or locked storage.
	FatalStorage FatalClass = "StorageDenied"
	// FatalContent reports invalid required content.
	FatalContent FatalClass = "ContentInvalid"
	// FatalInternal reports an unexpected startup failure.
	FatalInternal FatalClass = "Internal"
)

// FatalSinks are external output boundaries. Log is required. A nil Stderr
// uses os.Stderr, and a nil MessageBox uses the system adapter (a no-op on Linux).
type FatalSinks struct {
	Stderr     io.Writer
	Log        io.Writer
	MessageBox func(string) error
}

// ShowFatal sends a safe Russian message to all destinations even if one
// fails. Only predefined classes and a root-relative log location are printed.
func ShowFatal(ctx context.Context, class FatalClass, sinks FatalSinks) (err error) {
	defer mon.Task()(&ctx)(&err)
	switch class {
	case FatalGraphics, FatalStorage, FatalContent, FatalInternal:
	default:
		class = FatalInternal
	}
	message := fmt.Sprintf("Не удалось запустить игру. Класс ошибки: %s. Журнал диагностики: папка logs в каталоге данных игры.", class)
	stderr := sinks.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	stderrErr := writeMessage(ctx, stderr, message+"\n")
	var logErr error
	if sinks.Log == nil {
		logErr = errors.New("missing local log sink")
	} else {
		logErr = writeMessage(ctx, sinks.Log, message+"\n")
	}
	box := sinks.MessageBox
	if box == nil {
		box = func(message string) error { return showSystemMessage(ctx, message) }
	}
	return errors.Join(stderrErr, logErr, box(message))
}

func writeMessage(ctx context.Context, writer io.Writer, message string) (err error) {
	defer mon.Task()(&ctx)(&err)
	n, err := io.WriteString(writer, message)
	if err == nil && n != len(message) {
		return io.ErrShortWrite
	}
	return err
}

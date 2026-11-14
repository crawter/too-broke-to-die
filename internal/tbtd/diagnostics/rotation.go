// Copyright (C) 2026 crawter
// All rights reserved.

package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/zeebo/errs"
)

type logFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

func logName(index int) string {
	if index == 0 {
		return "current.jsonl"
	}
	return fmt.Sprintf("previous-%d.jsonl", index)
}

// checkLog rejects symlinks and non-regular files, including pre-existing
// oversized logs; it does not silently truncate someone else's data.
// Per-file checks in openLog and rotate are measured by the enclosing task.
func (recorder *Recorder) checkLog(_ context.Context, name string) (int64, bool, error) {
	info, err := recorder.logs.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > recorder.config.LogSize {
		return 0, false, ErrLimit
	}
	return info.Size(), true, nil
}

func (recorder *Recorder) openLog(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	for i := 0; i < recorder.config.LogFiles; i++ {
		if _, _, err := recorder.checkLog(ctx, logName(i)); err != nil {
			return err
		}
	}
	// A previous run may have retained more files than this configuration.
	for i := recorder.config.LogFiles; i < 10; i++ {
		name := logName(i)
		info, err := recorder.logs.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return ErrInvalid
		}
		if err = recorder.logs.Remove(name); err != nil {
			return err
		}
	}
	size, exists, err := recorder.checkLog(ctx, logName(0))
	if err != nil {
		return err
	}
	flags := os.O_WRONLY | os.O_APPEND
	if !exists {
		flags |= os.O_CREATE | os.O_EXCL
	}
	file, err := recorder.logs.OpenFile(logName(0), flags, 0600)
	if err != nil {
		return err
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() != size {
		if statErr == nil {
			statErr = ErrInvalid
		}
		return errs.Combine(statErr, file.Close())
	}
	recorder.file = file
	recorder.size = size
	return nil
}

func (recorder *Recorder) rotate(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if recorder.file == nil {
		return ErrClosed
	}
	err = errs.Combine(recorder.file.Sync(), recorder.file.Close())
	recorder.file = nil
	if err != nil {
		return err
	}
	last := recorder.config.LogFiles - 1
	if last == 0 {
		if err = recorder.logs.Remove(logName(0)); err != nil {
			return err
		}
	} else {
		_, exists, checkErr := recorder.checkLog(ctx, logName(last))
		if checkErr != nil {
			return checkErr
		}
		if exists {
			if err = recorder.logs.Remove(logName(last)); err != nil {
				return err
			}
		}
		for i := last - 1; i >= 0; i-- {
			_, exists, checkErr = recorder.checkLog(ctx, logName(i))
			if checkErr != nil {
				return checkErr
			}
			if exists {
				if err = recorder.logs.Rename(logName(i), logName(i+1)); err != nil {
					return err
				}
			}
		}
	}
	return recorder.openLog(ctx)
}

// Individual writes in Event and flushLocked are measured by those tasks.
func (recorder *Recorder) appendLog(ctx context.Context, data []byte) error {
	if int64(len(data))+1 > recorder.config.LogSize {
		return ErrLimit
	}
	if recorder.writeErr != nil {
		return recorder.writeErr
	}
	if recorder.file == nil {
		return ErrClosed
	}
	if recorder.size+int64(len(data))+1 > recorder.config.LogSize {
		if err := recorder.rotate(ctx); err != nil {
			return err
		}
	}
	line := make([]byte, len(data)+1)
	copy(line, data)
	line[len(data)] = '\n'
	n, err := recorder.file.Write(line)
	recorder.size += int64(n)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	return err
}

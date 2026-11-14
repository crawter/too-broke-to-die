// Copyright (C) 2026 crawter
// All rights reserved.

//go:build linux

package desktop

import (
	"golang.org/x/sys/unix"
)

// nativeThreadID is the operating-system thread identity, not a goroutine ID.
func nativeThreadID() uint64 {
	return uint64(unix.Gettid())
}

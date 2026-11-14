// Copyright (C) 2026 crawter
// All rights reserved.

//go:build windows

package desktop

import (
	"golang.org/x/sys/windows"
)

// nativeThreadID is the operating-system thread identity, not a goroutine ID.
func nativeThreadID() uint64 {
	return uint64(windows.GetCurrentThreadId())
}

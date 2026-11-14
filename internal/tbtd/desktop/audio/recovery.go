// Copyright (C) 2026 crawter
// All rights reserved.

//go:build !linux || !cgo

package audio

import (
	"too-broke-to-die/internal/tbtd/session"
)

// A build without the native decoder cannot play compressed streams. It must
// report the unavailable backend rather than silently treating media as valid.
func openVorbis(_ []byte) (streamDecoder, error) {
	return nil, session.AudioUnavailable
}

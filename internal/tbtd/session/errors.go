// Copyright (C) 2026 crawter
// All rights reserved.

// Package session defines adapter-independent frame and storage contracts.
package session

import (
	monkit "github.com/spacemonkeygo/monkit/v3"
)

var mon = monkit.Package()

// ErrorCode is a stable, comparable error identity. Presentation translates it
// to safe Russian text; it must not expose an underlying operating-system error.
type ErrorCode string

const (
	// InvalidAction means the input cannot be accepted in the current state.
	InvalidAction ErrorCode = "invalid_action"

	// ContentInvalid means a required definition or asset failed validation.
	ContentInvalid ErrorCode = "content_invalid"

	// StorageDenied means access to storage or profile ownership was denied.
	StorageDenied ErrorCode = "storage_denied"

	// StorageFull means the required write could not fit on the storage volume.
	StorageFull ErrorCode = "storage_full"

	// StorageConflict means the campaign, revision, or operation identity conflicts.
	StorageConflict ErrorCode = "storage_conflict"

	// SaveCorrupt means a revision failed structural, checksum, or domain validation.
	SaveCorrupt ErrorCode = "save_corrupt"

	// SaveTooNew means the saved data requires a newer compatible version.
	SaveTooNew ErrorCode = "save_too_new"

	// ContentIncompatible requires an explicit content migration or mapping.
	ContentIncompatible ErrorCode = "content_incompatible"

	// GraphicsUnavailable means the window or a required graphics capability failed.
	GraphicsUnavailable ErrorCode = "graphics_unavailable"

	// AudioUnavailable means audio output failed and silent operation may continue.
	AudioUnavailable ErrorCode = "audio_unavailable"

	// Internal means an unexpected invariant or operational failure occurred.
	Internal ErrorCode = "internal"
)

// Error makes ErrorCode usable with errors.Is, including through wrapping.
func (code ErrorCode) Error() string { return string(code) }

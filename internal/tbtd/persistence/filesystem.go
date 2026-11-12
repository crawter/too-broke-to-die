// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"context"

	"too-broke-to-die/internal/tbtd/platform"
)

// revisionFiles is the external filesystem boundary. The production adapter is
// platform.Root; tests can wrap it to inject failures without replacing the codec.
type revisionFiles interface {
	MkdirAll(context.Context, string) error
	WriteTemp(context.Context, string, []byte, int64) (string, error)
	ReadFile(context.Context, string, int64) ([]byte, error)
	SyncFile(context.Context, string) error
	ReplaceFile(context.Context, string, string) (platform.ReplaceResult, error)
}

var _ revisionFiles = (*platform.Root)(nil)

// revisionOption is a construction-time option for filesystem tests.
type revisionOption func(*Revisions)

// afterInstall is an injection point for an acknowledgment lost after all
// filesystem work. It is deliberately unavailable through player configuration.
func afterInstall(hook func() error) revisionOption {
	return func(revisions *Revisions) { revisions.ack = hook }
}

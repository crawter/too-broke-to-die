// Copyright (C) 2026 crawter
// All rights reserved.

package input

import (
	"context"
	"slices"

	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

// SetBindings atomically installs validated settings. The caller owns storage;
// this operation never changes campaign progress or other settings.
func (sampler *Sampler) SetBindings(ctx context.Context, values settings.Values) (err error) {
	defer mon.Task()(&ctx)(&err)
	if sampler == nil {
		return session.InvalidAction
	}
	if err = values.Validate(ctx); err != nil {
		return err
	}
	sampler.bindings = slices.Clone(values.Bindings)
	return nil
}

// RestoreDefaults restores only the binding records, leaving all other options
// and campaign state with their respective owners.
func (sampler *Sampler) RestoreDefaults(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	values := settings.Defaults(ctx)
	return sampler.SetBindings(ctx, values)
}

// Bindings returns an independent copy suitable for a settings draft.
func (sampler *Sampler) Bindings(ctx context.Context) []settings.Binding {
	defer mon.Task()(&ctx)(nil)
	return slices.Clone(sampler.bindings)
}

// Copyright (C) 2026 crawter
// All rights reserved.

package audio

import (
	"context"
	"fmt"

	"too-broke-to-die/internal/tbtd/session"
)

// ContinueSilent accepts an explicitly presented AudioUnavailable choice. It
// cannot be used to bypass missing or invalid required content.
func (mixer *Mixer) ContinueSilent(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = mixer.check(); err != nil {
		return err
	}
	if !mixer.failed {
		return session.InvalidAction
	}
	if _, err = mixer.readGroup(ctx, mixer.group); err != nil {
		return err
	}
	mixer.closeDevice(ctx)
	mixer.silent = true
	return nil
}

// Retry releases old handles before reopening and rebuilds only the current
// load group. It does not replay previously consumed events.
func (mixer *Mixer) Retry(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = mixer.check(); err != nil {
		return err
	}
	if !mixer.failed && !mixer.silent {
		return session.InvalidAction
	}
	data, err := mixer.readGroup(ctx, mixer.group)
	if err != nil {
		return err
	}
	mixer.closeDevice(ctx)
	mixer.silent = false
	mixer.failed = true
	if !mixer.native.open() {
		return session.AudioUnavailable
	}
	mixer.opened = true
	if err = mixer.acquire(ctx, mixer.group, data); err != nil {
		mixer.closeDevice(ctx)
		return err
	}
	// The previous device may have left delayed samples in its limiter. A new
	// processor starts empty only after the old device has been closed.
	mixer.processor = newMixProcessor(ctx)
	mixer.native.attachMixed(mixer.processor)
	mixer.attached = true
	mixer.needsResume = true
	mixer.failed = false
	return nil
}

// LoadGroup validates and replaces the shared/current audio set. The caller
// chooses the same explicit group as the presentation cache, never a filename.
func (mixer *Mixer) LoadGroup(ctx context.Context, group string) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = mixer.check(); err != nil {
		return err
	}
	if !validGroup(group) {
		return fmt.Errorf("%w: unknown audio group", session.ContentInvalid)
	}
	if mixer.failed && !mixer.silent {
		return session.AudioUnavailable
	}
	if group == mixer.group && (mixer.silent || mixer.assets != nil) {
		return nil
	}
	data, err := mixer.readGroup(ctx, group)
	if err != nil {
		return err
	}
	if mixer.silent {
		mixer.group = group
		mixer.pruneDesired(ctx, group)
		return nil
	}
	mixer.release(ctx)
	if err = mixer.acquire(ctx, group, data); err != nil {
		mixer.release(ctx)
		if !mixer.native.ready() {
			mixer.failed = true
			return fmt.Errorf("%w: group acquisition", session.AudioUnavailable)
		}
		return err
	}
	mixer.pruneDesired(ctx, group)
	mixer.needsResume = true
	return nil
}

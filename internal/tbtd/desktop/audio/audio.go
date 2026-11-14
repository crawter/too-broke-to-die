// Copyright (C) 2026 crawter
// All rights reserved.

// Package audio owns audio-device resources on the locked initial thread.
package audio

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	monkit "github.com/spacemonkeygo/monkit/v3"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/desktop/assets"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

var mon = monkit.Package()

// Mixer is the sole owner of its device, streams, effects and voice aliases.
// The caller locks the initial OS thread before New and keeps it locked through
// Close. No mixer method may be invoked on another thread.
type Mixer struct {
	filesystem  fs.FS
	records     []content.ManifestRecord
	thread      assets.ThreadID
	owner       uint64
	native      device
	group       string
	assets      map[string]*asset
	voices      [32]voice
	streams     map[string]*streamPlayback
	opened      bool
	closed      bool
	failed      bool
	silent      bool
	transient   map[transientID]bool
	durable     map[durableID]bool
	generation  uint64
	campaign    session.CampaignID
	clock       time.Duration
	lastChant   time.Duration
	hadChant    bool
	musicDuck   float64
	ambientDuck float64
	processor   *mixProcessor
	attached    bool
	desired     map[string]string
	streamState map[string]*streamState
	needsResume bool
}

// Frame supplies presentation-only inputs not carried by a session view.
// Focused must reflect the current window focus. Elapsed is monotonic frame
// time, never simulation time. Dialogue signals a currently visible story line.
type Frame struct {
	Elapsed  time.Duration
	Focused  bool
	Dialogue bool
	Settings settings.Audio
}

// New validates the authored manifest and loads shared audio plus group.
// The filesystem must confine symlinks (for example, os.Root.FS). On device
// failure a non-nil mixer and AudioUnavailable are returned so the caller can
// offer explicit silent play or retry. Content failure never permits a fallback.
func New(ctx context.Context, filesystem fs.FS, manifest *content.Manifest, thread assets.ThreadID, group string) (_ *Mixer, err error) {
	defer mon.Task()(&ctx)(&err)
	if filesystem == nil || manifest == nil || thread == nil || thread() == 0 || !validGroup(group) {
		return nil, fmt.Errorf("%w: invalid audio dependencies", session.ContentInvalid)
	}
	if err = content.ValidateManifest(ctx, filesystem, manifest, false); err != nil {
		return nil, fmt.Errorf("%w: %w", session.ContentInvalid, err)
	}
	return newMixer(ctx, filesystem, manifest.Records, thread, group, raylibDevice{})
}

func newMixer(ctx context.Context, filesystem fs.FS, records []content.ManifestRecord, thread assets.ThreadID, group string, native device) (_ *Mixer, err error) {
	defer mon.Task()(&ctx)(&err)
	if filesystem == nil || thread == nil || thread() == 0 || native == nil || !validGroup(group) || len(records) == 0 {
		return nil, fmt.Errorf("%w: invalid audio dependencies", session.ContentInvalid)
	}
	owned := make([]content.ManifestRecord, len(records))
	for i, record := range records {
		owned[i] = record
		if record.Audio != nil {
			value := *record.Audio
			value.Markers = append([]content.TransitionMarker(nil), value.Markers...)
			if value.LoopStartSample != nil {
				start := *value.LoopStartSample
				value.LoopStartSample = &start
			}
			if value.LoopEndSample != nil {
				end := *value.LoopEndSample
				value.LoopEndSample = &end
			}
			owned[i].Audio = &value
		}
		if record.File != nil {
			file := *record.File
			owned[i].File = &file
		}
		owned[i].AdditionalExports = append([]string(nil), record.AdditionalExports...)
		owned[i].AdditionalFiles = make([]*content.FileMetadata, len(record.AdditionalFiles))
		for j, file := range record.AdditionalFiles {
			if file != nil {
				copyFile := *file
				owned[i].AdditionalFiles[j] = &copyFile
			}
		}
	}
	mixer := &Mixer{filesystem: filesystem, records: owned, thread: thread, owner: thread(), native: native, group: group, transient: make(map[transientID]bool), durable: make(map[durableID]bool), musicDuck: 1, ambientDuck: 1, processor: newMixProcessor(ctx), desired: make(map[string]string), streamState: make(map[string]*streamState)}
	data, err := mixer.readGroup(ctx, group)
	if err != nil {
		return nil, err
	}
	if !native.open() {
		mixer.failed = true
		return mixer, session.AudioUnavailable
	}
	mixer.opened = true
	if err = mixer.acquire(ctx, group, data); err != nil {
		mixer.closeDevice(ctx)
		if errors.Is(err, session.AudioUnavailable) {
			mixer.failed = true
			return mixer, err
		}
		return nil, err
	}
	mixer.native.attachMixed(mixer.processor)
	mixer.attached = true
	return mixer, nil
}

func validGroup(group string) bool {
	switch group {
	case "shared", "location-hos", "location-chu", "location-min", "location-ins", "ending":
		return true
	default:
		return false
	}
}

func (mixer *Mixer) check() error {
	if mixer == nil || mixer.thread == nil || mixer.thread() != mixer.owner {
		return assets.ErrThread
	}
	if mixer.closed {
		return assets.ErrClosed
	}
	return nil
}

// Update consumes a copied immutable view. It never changes captions, gameplay,
// certification or persistence state. AudioUnavailable requires a caller-owned
// pause and an explicit recovery choice. A stream_transition semantic event
// supplies asset, marker and action parameters; action is hold, stop or tail.
// The named marker and tail must belong to the validated current load group.
func (mixer *Mixer) Update(ctx context.Context, view session.FrameView, frame Frame) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = mixer.check(); err != nil {
		return err
	}
	if frame.Elapsed < 0 || !validLevels(frame.Settings) {
		return fmt.Errorf("%w: invalid audio frame", session.InvalidAction)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	mixer.processor.configure(frame.Settings)
	data := view.Data(ctx)
	if mixer.campaign != data.Progress.Campaign {
		mixer.campaign = data.Progress.Campaign
		mixer.durable = make(map[durableID]bool)
	}
	if mixer.generation != data.World.Generation {
		mixer.generation = data.World.Generation
		mixer.transient = make(map[transientID]bool)
	}
	if mixer.failed && !mixer.silent {
		return session.AudioUnavailable
	}
	if !mixer.silent && (!mixer.opened || !mixer.native.ready()) {
		mixer.failed = true
		return session.AudioUnavailable
	}
	paused := data.State == session.StatePause || data.State == session.StateLedger || data.State == session.StateSaving || data.State == session.StateRestoring || data.State == session.StateError
	if frame.Focused && !paused {
		mixer.clock += frame.Elapsed
	}
	if !mixer.silent {
		mixer.advanceVoices(paused, !frame.Focused)
	}
	events := append([]session.SemanticEvent(nil), data.Events...)
	for _, effect := range data.World.Effects {
		if event, ok := effectCue(effect); ok {
			events = append(events, event)
		}
	}
	for _, event := range events {
		if !mixer.acceptEvent(event, data) {
			continue
		}
		if event.Kind == "stream_transition" {
			if err = mixer.scheduleTransition(ctx, event); err != nil {
				return err
			}
			continue
		}
		if mixer.silent || paused || !frame.Focused {
			if mixer.rememberSilentStream(eventAsset(event)) {
				mixer.needsResume = true
				continue
			}
			if mixer.silent {
				continue
			}
		}
		if err = mixer.playEvent(ctx, event, data, frame, paused); err != nil {
			return err
		}
	}
	if mixer.silent {
		mixer.advanceSilentStreams(frame, paused)
	} else {
		if mixer.needsResume && frame.Focused && !paused {
			if err = mixer.resumeStreams(ctx); err != nil {
				return err
			}
		}
		if err = mixer.advanceStreams(frame, paused); err != nil {
			mixer.failed = true
			return err
		}
		mixer.updateMix(frame)
	}
	return nil
}

// Close releases owned handles once and must run on the owner thread.
func (mixer *Mixer) Close(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if mixer == nil {
		return nil
	}
	if mixer.thread == nil || mixer.thread() != mixer.owner {
		return assets.ErrThread
	}
	if !mixer.closed {
		mixer.closeDevice(ctx)
		mixer.closed = true
	}
	return nil
}

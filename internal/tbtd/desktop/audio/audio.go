// Copyright (C) 2026 crawter
// All rights reserved.

// Package audio owns audio-device resources on the locked initial thread.
package audio

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
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
	return newMixer(ctx, filesystem, manifest.Records, thread, group, &raylibDevice{})
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
		mixer.captureStreamResults()
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
			if errors.Is(err, session.AudioUnavailable) {
				mixer.captureStreamResults()
				mixer.failed = true
			}
			return err
		}
	}
	if mixer.silent {
		mixer.advanceSilentStreams(frame, paused)
	} else {
		if mixer.needsResume && frame.Focused && !paused {
			if err = mixer.resumeStreams(ctx); err != nil {
				if errors.Is(err, session.AudioUnavailable) {
					mixer.captureStreamResults()
					mixer.failed = true
				}
				return err
			}
		}
		if err = mixer.advanceStreams(frame, paused); err != nil {
			if errors.Is(err, session.AudioUnavailable) {
				mixer.captureStreamResults()
				mixer.failed = true
			}
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

type transientID struct {
	tick    uint64
	entity  string
	ordinal uint32
}

type durableID struct {
	revision  uint64
	operation session.OperationID
	kind      string
}

func (mixer *Mixer) acceptEvent(event session.SemanticEvent, data session.FrameViewData) bool {
	if event.Kind == "" || data.State == session.StateRestoring || data.RestorationReady {
		return false
	}
	if event.Durable {
		if event.Revision == 0 || event.Revision > data.Progress.Revision || event.Operation.Event == "" || event.Operation.Sequence == 0 {
			return false
		}
		id := durableID{revision: event.Revision, operation: event.Operation, kind: event.Kind}
		if mixer.durable[id] {
			return false
		}
		mixer.durable[id] = true
		return true
	}
	if event.Entity == "" {
		return false
	}
	id := transientID{tick: event.Tick, entity: event.Entity, ordinal: event.Ordinal}
	if mixer.transient[id] {
		return false
	}
	mixer.transient[id] = true
	return true
}

// effectCue maps the session's tick-stamped simulation effects, never a
// decorative particle or visual bounce, to their corresponding sound family.
func effectCue(effect session.EffectEvent) (session.SemanticEvent, bool) {
	var id string
	switch effect.Kind {
	case "hit":
		id = "SFX-BODY-IMPACT"
		if effect.Entity == "player" {
			id = "SFX-BODY-HURT"
		}
	case "removal":
		id = "SFX-BODY-TEAR"
	case "feces":
		id = "SFX-FEC-HIT"
	default:
		return session.SemanticEvent{}, false
	}
	return session.SemanticEvent{Kind: "effect", Tick: effect.Tick, Entity: effect.Entity, Ordinal: effect.Ordinal, Parameters: []session.EventParameter{{Name: "asset", Value: id}}}, true
}

func eventAsset(event session.SemanticEvent) string {
	for _, parameter := range event.Parameters {
		if parameter.Name == "asset" {
			return parameter.Value
		}
	}
	switch event.Kind {
	case "saved":
		return "SFX-UI-SAVED"
	case "receipt":
		return "STG-CREDIT"
	case "victory":
		return "STG-VICTORY"
	case "warning":
		return "SFX-WARN-ENEMY"
	default:
		return ""
	}
}

func (mixer *Mixer) playEvent(ctx context.Context, event session.SemanticEvent, data session.FrameViewData, frame Frame, paused bool) (err error) {
	defer mon.Task()(&ctx)(&err)
	id := eventAsset(event)
	if id == "" {
		return nil
	}
	// Reward cues must be explicitly acknowledged, never inferred from a save
	// proposal, screen transition, animation, or a changed credit count.
	if (id == "STG-CREDIT" || id == "STG-VICTORY" || event.Kind == "receipt" || event.Kind == "victory") && (!event.Durable || data.Save == session.SavePending) {
		return nil
	}
	item := mixer.assets[id]
	if item == nil {
		return fmt.Errorf("%w: %s: audio not in current group", session.ContentInvalid, id)
	}
	menu := strings.HasPrefix(id, "SFX-UI-")
	if (!frame.Focused || paused) && !menu {
		return nil
	}
	if item.stream != nil {
		return mixer.startStream(ctx, id)
	}
	if len(item.sounds) == 0 {
		return fmt.Errorf("%w: %s: no audio export", session.ContentInvalid, id)
	}
	priority := priorityFor(id)
	chant := strings.HasPrefix(id, "VOC-TRE-")
	if chant {
		if (mixer.hadChant && mixer.clock-mixer.lastChant < 4*time.Second) || mixer.foregroundChant() {
			return nil
		}
	}
	index := mixer.voiceSlot(priority)
	if index < 0 {
		return nil
	}
	mixer.releaseVoice(index)
	variation := int(event.Ordinal % uint32(len(item.sounds)))
	alias, err := mixer.native.alias(item.sounds[variation])
	if err != nil {
		mixer.failed = true
		return fmt.Errorf("%w: %w", session.AudioUnavailable, err)
	}
	vocal := item.record.Kind == "vocal"
	mixer.voices[index] = voice{sound: alias, priority: priority, menu: menu, vocal: vocal, chant: chant}
	mixer.native.playSound(alias, voiceVolume(frame.Settings, vocal), 0.5)
	if chant {
		mixer.hadChant = true
		mixer.lastChant = mixer.clock
	}
	return nil
}

type voice struct {
	sound    *sound
	priority int
	menu     bool
	vocal    bool
	music    bool
	chant    bool
	paused   bool
}

func priorityFor(id string) int {
	switch {
	case strings.HasPrefix(id, "SFX-WARN-"), strings.Contains(id, "-PREP"), id == "STG-CONSEQUENCE":
		return 6
	case strings.HasPrefix(id, "SFX-UI-"), strings.HasPrefix(id, "STG-"):
		return 5
	case strings.HasPrefix(id, "VOC-"):
		return 4
	default:
		return 3
	}
}

func (mixer *Mixer) voiceSlot(priority int) int {
	lowest := -1
	for i, voice := range mixer.voices {
		if voice.sound == nil {
			return i
		}
		if voice.priority < priority && (lowest < 0 || voice.priority < mixer.voices[lowest].priority) {
			lowest = i
		}
	}
	return lowest
}

func (mixer *Mixer) foregroundChant() bool {
	for _, voice := range mixer.voices {
		if voice.sound != nil && voice.chant {
			return true
		}
	}
	return false
}

func (mixer *Mixer) releaseVoice(index int) {
	current := &mixer.voices[index]
	if current.sound != nil {
		mixer.native.stopSound(current.sound)
		mixer.native.unloadAlias(current.sound)
	}
	*current = voice{}
}

func (mixer *Mixer) advanceVoices(paused, unfocused bool) {
	for i := range mixer.voices {
		voice := &mixer.voices[i]
		if voice.sound == nil {
			continue
		}
		freeze := (paused && !voice.menu) || (unfocused && (voice.vocal || voice.music))
		if freeze && !voice.paused {
			mixer.native.pauseSound(voice.sound)
			voice.paused = true
		} else if !freeze && voice.paused {
			mixer.native.resumeSound(voice.sound)
			voice.paused = false
		}
		if !voice.paused && !mixer.native.playingSound(voice.sound) {
			mixer.releaseVoice(i)
		}
	}
}

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

// Copyright (C) 2026 crawter
// All rights reserved.

package audio

import (
	"context"
	"fmt"
	"math"
	"time"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
)

// streamState survives native handle replacement and contains presentation
// state only. A silent device advances it using the permitted scene clock.
type streamState struct {
	position   float64
	held       bool
	transition *streamTransition
}

type streamPlayback struct {
	*streamState
	gain   float64
	target float64
	paused bool
}

type streamTransition struct {
	marker int64
	action string
	tail   string
}

func (mixer *Mixer) groupRecord(id string) *content.ManifestRecord {
	for i := range mixer.records {
		record := &mixer.records[i]
		if record.ID == id && (record.LoadGroup == "shared" || record.LoadGroup == mixer.group) {
			return record
		}
	}
	return nil
}

// selectStream records an explicit scene selection independently of the device.
// Re-selecting a held stream resumes it; replacing it cancels the old command.
func (mixer *Mixer) selectStream(id string) bool {
	record := mixer.groupRecord(id)
	if record == nil || (record.Kind != "music" && record.Kind != "ambience") || record.Audio == nil {
		return false
	}
	previous := mixer.desired[record.Kind]
	if previous != id {
		if old := mixer.streamState[previous]; old != nil {
			old.transition = nil
		}
		if mixer.streams[id] == nil {
			mixer.streamState[id] = &streamState{}
		}
	}
	state := mixer.streamState[id]
	if state == nil {
		state = &streamState{}
		mixer.streamState[id] = state
	}
	state.held = false
	state.transition = nil
	mixer.desired[record.Kind] = id
	return true
}

func (mixer *Mixer) startStream(ctx context.Context, id string) (err error) {
	defer mon.Task()(&ctx)(&err)
	item := mixer.assets[id]
	if item == nil || item.stream == nil || !mixer.selectStream(id) {
		return fmt.Errorf("%w: %s: missing stream", session.ContentInvalid, id)
	}
	return mixer.openSelectedStream(ctx, id)
}

// openSelectedStream reconstructs handles without changing held or pending
// presentation decisions; explicit selection is handled separately.
func (mixer *Mixer) openSelectedStream(ctx context.Context, id string) (err error) {
	defer mon.Task()(&ctx)(&err)
	item, state := mixer.assets[id], mixer.streamState[id]
	if item == nil || item.stream == nil || state == nil || mixer.desired[item.record.Kind] != id {
		return fmt.Errorf("%w: %s: missing selected stream", session.ContentInvalid, id)
	}
	if state.held {
		return nil
	}
	if math.IsNaN(state.position) || math.IsInf(state.position, 0) || state.position < 0 || state.position > math.MaxFloat32 {
		return fmt.Errorf("%w: %s: invalid saved stream position", session.ContentInvalid, id)
	}
	if mixer.streams == nil {
		mixer.streams = make(map[string]*streamPlayback)
	}
	for existing, playback := range mixer.streams {
		if mixer.assets[existing].record.Kind == item.record.Kind {
			if existing == id {
				playback.target = 1
			} else {
				playback.target = 0
				playback.transition = nil
			}
		}
	}
	if mixer.streams[id] != nil {
		return nil
	}
	mixer.streams[id] = &streamPlayback{streamState: state, target: 1}
	mixer.native.streamVolume(item.stream, 0)
	mixer.native.playStream(item.stream)
	if state.position > 0 {
		mixer.native.seekStream(item.stream, float32(state.position))
	}
	return nil
}

// scheduleTransition accepts presentation commands expressed as a semantic
// event: asset, marker, and action (hold, stop, or tail). A later command or
// selection replaces the pending transition; no gameplay decision is made here.
func (mixer *Mixer) scheduleTransition(ctx context.Context, event session.SemanticEvent) (err error) {
	defer mon.Task()(&ctx)(&err)
	id := eventAsset(event)
	record, state := mixer.groupRecord(id), mixer.streamState[id]
	if record == nil || record.Audio == nil || (record.Kind != "music" && record.Kind != "ambience") || state == nil || mixer.desired[record.Kind] != id {
		return fmt.Errorf("%w: %s: no selected stream", session.InvalidAction, id)
	}
	var markerName, action string
	for _, parameter := range event.Parameters {
		switch parameter.Name {
		case "marker":
			markerName = parameter.Value
		case "action":
			action = parameter.Value
		}
	}
	if action != "hold" && action != "stop" && action != "tail" {
		return fmt.Errorf("%w: invalid transition action", session.InvalidAction)
	}
	var marker *int64
	for _, candidate := range record.Audio.Markers {
		if candidate.Name == markerName && candidate.Resolved && candidate.Sample >= 0 {
			sample := candidate.Sample
			marker = &sample
			break
		}
	}
	if marker == nil || record.Audio.SampleRate <= 0 {
		return fmt.Errorf("%w: %s: unknown transition marker", session.ContentInvalid, id)
	}
	var tail string
	if action == "tail" {
		tail = id + "-TAIL"
		if tailRecord := mixer.groupRecord(tail); tailRecord == nil || tailRecord.Kind != "audio-tail" || !tailRecord.Final || tailRecord.Audio == nil || tailRecord.File == nil {
			return fmt.Errorf("%w: %s: missing transition tail", session.ContentInvalid, tail)
		}
		if !mixer.silent {
			if asset := mixer.assets[tail]; asset == nil || len(asset.sounds) == 0 {
				return fmt.Errorf("%w: %s: transition tail not loaded", session.ContentInvalid, tail)
			}
		}
	}
	state.transition = &streamTransition{marker: *marker, action: action, tail: tail}
	return nil
}

// rememberSilentStream updates a deferred selection without native playback.
func (mixer *Mixer) rememberSilentStream(id string) bool {
	return mixer.selectStream(id)
}

func (mixer *Mixer) pruneDesired(ctx context.Context, group string) {
	defer mon.Task()(&ctx)(nil)
	for kind, id := range mixer.desired {
		keep := false
		for _, record := range mixer.records {
			if record.ID == id && (record.LoadGroup == "shared" || record.LoadGroup == group) {
				keep = true
				break
			}
		}
		if !keep {
			delete(mixer.desired, kind)
		}
	}
	for id := range mixer.streamState {
		if record := mixer.groupRecord(id); record == nil || mixer.desired[record.Kind] != id {
			delete(mixer.streamState, id)
		}
	}
}

func (mixer *Mixer) resumeStreams(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	for _, kind := range []string{"music", "ambience"} {
		id := mixer.desired[kind]
		if id == "" {
			continue
		}
		state := mixer.streamState[id]
		if state == nil {
			return fmt.Errorf("%w: %s: missing stream state", session.ContentInvalid, id)
		}
		if state.held {
			continue
		}
		if playback := mixer.streams[id]; playback != nil && playback.target == 1 {
			continue
		}
		if err = mixer.openSelectedStream(ctx, id); err != nil {
			return err
		}
	}
	mixer.needsResume = false
	return nil
}

// advanceSilentStreams follows the permitted presentation clock without
// recreating native handles. Completed tails are intentionally not queued for
// replay; pending transitions and held positions remain part of the selection.
func (mixer *Mixer) advanceSilentStreams(frame Frame, paused bool) {
	if paused || !frame.Focused {
		return
	}
	for _, kind := range []string{"music", "ambience"} {
		id := mixer.desired[kind]
		state, record := mixer.streamState[id], mixer.groupRecord(id)
		if state == nil || record == nil || record.Audio == nil || state.held {
			continue
		}
		audio := record.Audio
		next := state.position + frame.Elapsed.Seconds()
		if transition := state.transition; transition != nil && next*float64(audio.SampleRate) >= float64(transition.marker) {
			state.position = float64(transition.marker) / float64(audio.SampleRate)
			state.transition = nil
			if transition.action == "hold" {
				state.held = true
			} else {
				delete(mixer.desired, kind)
				delete(mixer.streamState, id)
			}
			continue
		}
		if audio.LoopStartSample != nil && audio.LoopEndSample != nil && audio.SampleRate > 0 {
			start := float64(*audio.LoopStartSample) / float64(audio.SampleRate)
			end := float64(*audio.LoopEndSample) / float64(audio.SampleRate)
			if end > start && next >= end {
				next = start + math.Mod(next-end, end-start)
			}
		}
		state.position = next
	}
}

func (mixer *Mixer) playTail(id string, frame Frame) error {
	index := mixer.voiceSlot(3)
	if index < 0 {
		return nil
	}
	mixer.releaseVoice(index)
	alias, err := mixer.native.alias(mixer.assets[id].sounds[0])
	if err != nil {
		return fmt.Errorf("%w: %w", session.AudioUnavailable, err)
	}
	mixer.voices[index] = voice{sound: alias, priority: 3, music: true}
	volume := float32(peakCeiling * float64(frame.Settings.Master) * float64(frame.Settings.Music) / 10000 * mixer.musicDuck)
	mixer.native.playSound(alias, volume, 0.5)
	return nil
}

func (mixer *Mixer) advanceStreams(frame Frame, paused bool) error {
	for id, playback := range mixer.streams {
		item := mixer.assets[id]
		freeze := paused || !frame.Focused || playback.held
		if freeze && !playback.paused {
			mixer.native.pauseStream(item.stream)
			playback.paused = true
		} else if !freeze && playback.paused {
			mixer.native.resumeStream(item.stream)
			playback.paused = false
		}
		if freeze {
			continue
		}
		if !mixer.native.updateStream(item.stream) {
			return fmt.Errorf("%w: %s: stream update", session.AudioUnavailable, id)
		}
		audio := item.record.Audio
		position := float64(mixer.native.streamTime(item.stream))
		if math.IsNaN(position) || math.IsInf(position, 0) || position < 0 {
			return fmt.Errorf("%w: %s: invalid stream position", session.AudioUnavailable, id)
		}
		playback.position = position
		if transition := playback.transition; transition != nil && position*float64(audio.SampleRate) >= float64(transition.marker) {
			playback.transition = nil
			if transition.action == "hold" {
				playback.position = float64(transition.marker) / float64(audio.SampleRate)
				playback.held = true
				mixer.native.pauseStream(item.stream)
				playback.paused = true
				continue
			}
			if transition.action == "tail" {
				if err := mixer.playTail(transition.tail, frame); err != nil {
					return err
				}
			}
			mixer.native.stopStream(item.stream)
			delete(mixer.streams, id)
			if mixer.desired[item.record.Kind] == id {
				delete(mixer.desired, item.record.Kind)
				delete(mixer.streamState, id)
			}
			continue
		}
		if audio.LoopStartSample != nil && audio.LoopEndSample != nil && audio.SampleRate > 0 {
			start := float64(*audio.LoopStartSample) / float64(audio.SampleRate)
			end := float64(*audio.LoopEndSample) / float64(audio.SampleRate)
			if end <= start {
				return fmt.Errorf("%w: %s: invalid loop bounds", session.ContentInvalid, id)
			}
			if position >= end {
				// Preserve overshoot (including multiple loop lengths) rather than
				// losing it each time the displayed frame crosses the boundary.
				playback.position = start + math.Mod(position-end, end-start)
				mixer.native.seekStream(item.stream, float32(playback.position))
			}
		}
		playback.gain = ramp(playback.gain, playback.target, frame.Elapsed, time.Second)
		if playback.target == 0 && math.Abs(playback.gain) < 0.000001 {
			mixer.native.stopStream(item.stream)
			delete(mixer.streams, id)
		}
	}
	return nil
}

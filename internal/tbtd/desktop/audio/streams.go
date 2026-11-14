// Copyright (C) 2026 crawter
// All rights reserved.

package audio

import (
	"context"
	"errors"
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
	gain    float64
	target  float64
	paused  bool
	tailing bool
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
	if playback := mixer.streams[id]; playback != nil {
		result := mixer.native.streamResult(item.stream)
		if playback.tailing || result == "tail" || result == "tail-done" || result == "stop" || result == "ended" {
			mixer.native.stopStream(item.stream)
			delete(mixer.streams, id)
			state.position = 0
		}
	}
	for existing, playback := range mixer.streams {
		if mixer.assets[existing].record.Kind == item.record.Kind {
			if existing == id {
				playback.target = 1
			} else {
				playback.target = 0
				playback.transition = nil
				result := mixer.native.streamResult(mixer.assets[existing].stream)
				if result == "tail" || result == "tail-done" {
					playback.tailing = true
				} else if err = mixer.native.streamTransition(mixer.assets[existing].stream, nil, nil); err != nil {
					return fmt.Errorf("%w: %w", session.AudioUnavailable, err)
				}
			}
		}
	}
	if playback := mixer.streams[id]; playback != nil {
		if err = mixer.native.streamTransition(item.stream, state.transition, mixer.transitionTail(state.transition)); err != nil {
			return fmt.Errorf("%w: %w", session.ContentInvalid, err)
		}
		if playback.paused {
			mixer.native.resumeStream(item.stream)
			playback.paused = false
		}
		return nil
	}
	if err = mixer.native.streamTransition(item.stream, state.transition, mixer.transitionTail(state.transition)); err != nil {
		return fmt.Errorf("%w: %w", session.ContentInvalid, err)
	}
	mixer.native.streamVolume(item.stream, 0)
	mixer.native.seekStream(item.stream, float32(state.position))
	if err = mixer.native.playStream(item.stream); err != nil {
		if !mixer.native.ready() || errors.Is(err, session.AudioUnavailable) {
			return fmt.Errorf("%w: %w", session.AudioUnavailable, err)
		}
		return contentFailure(id, err)
	}
	mixer.streams[id] = &streamPlayback{streamState: state, target: 1}
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
	transition := &streamTransition{marker: *marker, action: action, tail: tail}
	if playback := mixer.streams[id]; playback != nil {
		if err = mixer.native.streamTransition(mixer.assets[id].stream, transition, mixer.transitionTail(transition)); err != nil {
			return fmt.Errorf("%w: %s: %w", session.ContentInvalid, id, err)
		}
	}
	state.transition = transition
	return nil
}

func (mixer *Mixer) transitionTail(transition *streamTransition) *sound {
	if transition == nil || transition.tail == "" {
		return nil
	}
	item := mixer.assets[transition.tail]
	if item == nil || len(item.sounds) == 0 {
		return nil
	}
	return item.sounds[0]
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
		elapsed := frame.Elapsed.Seconds()
		next := state.position + elapsed
		var loopStart, loopEnd float64
		looping := audio.LoopStartSample != nil && audio.LoopEndSample != nil && audio.SampleRate > 0
		if looping {
			loopStart = float64(*audio.LoopStartSample) / float64(audio.SampleRate)
			loopEnd = float64(*audio.LoopEndSample) / float64(audio.SampleRate)
			looping = loopEnd > loopStart
		}
		if transition := state.transition; transition != nil && audio.SampleRate > 0 {
			marker := float64(transition.marker) / float64(audio.SampleRate)
			distance := marker - state.position
			reachable := true
			if looping && marker > loopEnd {
				// The loop end is reached before a marker in the outro.
				reachable = false
			} else if distance < 0 && looping && marker >= loopStart {
				// A marker already passed in this iteration occurs on the next.
				distance = loopEnd - state.position + marker - loopStart
			} else if distance < 0 {
				// One-shot markers cannot be revisited after they have passed.
				distance = 0
			}
			if reachable && elapsed >= distance {
				state.position = marker
				state.transition = nil
				if transition.action == "hold" {
					state.held = true
				} else {
					delete(mixer.desired, kind)
					delete(mixer.streamState, id)
				}
				continue
			}
		}
		if looping && next >= loopEnd {
			next = loopStart + math.Mod(next-loopEnd, loopEnd-loopStart)
		}
		state.position = next
	}
}

// captureStreamResults records callback completions before device teardown so
// recovery cannot replay a tail or revive a stopped selection.
func (mixer *Mixer) captureStreamResults() {
	for id, playback := range mixer.streams {
		item := mixer.assets[id]
		position := float64(mixer.native.streamTime(item.stream))
		if !math.IsNaN(position) && !math.IsInf(position, 0) && position >= 0 {
			playback.position = position
		}
		switch mixer.native.streamResult(item.stream) {
		case "hold":
			playback.transition = nil
			playback.held = true
		case "tail", "tail-done", "stop", "ended":
			playback.transition = nil
			if mixer.desired[item.record.Kind] == id {
				delete(mixer.desired, item.record.Kind)
				delete(mixer.streamState, id)
			}
		}
	}
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
		if err := mixer.native.updateStream(item.stream); err != nil {
			return fmt.Errorf("%s: stream update: %w", id, err)
		}
		position := float64(mixer.native.streamTime(item.stream))
		if math.IsNaN(position) || math.IsInf(position, 0) || position < 0 {
			return fmt.Errorf("%w: %s: invalid stream position", session.AudioUnavailable, id)
		}
		playback.position = position
		result := mixer.native.streamResult(item.stream)
		if result == "hold" {
			playback.transition = nil
			playback.held = true
			mixer.native.pauseStream(item.stream)
			playback.paused = true
			continue
		}
		if result == "tail" || result == "tail-done" || result == "stop" || result == "ended" {
			playback.transition = nil
			if mixer.desired[item.record.Kind] == id {
				delete(mixer.desired, item.record.Kind)
				delete(mixer.streamState, id)
			}
			if result != "tail" {
				mixer.native.stopStream(item.stream)
				delete(mixer.streams, id)
				continue
			}
			playback.tailing = true
		}
		playback.gain = ramp(playback.gain, playback.target, frame.Elapsed, time.Second)
		if playback.target == 0 && math.Abs(playback.gain) < 0.000001 {
			mixer.native.stopStream(item.stream)
			delete(mixer.streams, id)
		}
	}
	return nil
}

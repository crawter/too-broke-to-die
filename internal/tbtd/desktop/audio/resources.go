// Copyright (C) 2026 crawter
// All rights reserved.

package audio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"runtime"
	"strings"
	"sync"

	rl "github.com/gen2brain/raylib-go/raylib"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
)

const maxAudioBytes = 64 << 20

// The native device is the external boundary. All calls, including cleanup,
// are made synchronously on the mixer owner's locked thread.
type device interface {
	open() bool
	ready() bool
	close()
	attachMixed(*mixProcessor)
	detachMixed(*mixProcessor)
	loadSound([]byte, bool) (*sound, error)
	unloadSound(*sound)
	alias(*sound) (*sound, error)
	unloadAlias(*sound)
	playSound(*sound, float32, float32)
	soundVolume(*sound, float32)
	stopSound(*sound)
	pauseSound(*sound)
	resumeSound(*sound)
	playingSound(*sound) bool
	loadStream([]byte, *content.AudioRequirement) (*stream, error)
	unloadStream(*stream)
	playStream(*stream) error
	stopStream(*stream)
	pauseStream(*stream)
	resumeStream(*stream)
	updateStream(*stream) bool
	streamVolume(*stream, float32)
	streamTime(*stream) float32
	seekStream(*stream, float32)
	streamTransition(*stream, *streamTransition, *sound) error
	streamResult(*stream) string
}

type sound struct {
	value rl.Sound
	tail  []float32
}

// stream data and transport are protected by the device's stream mutex.
// PCM is decoded only while selected and is released on stop.
type stream struct {
	data       []byte
	frames     int64
	pcm        []float32
	position   int64
	loopStart  int64
	loopEnd    int64
	playing    bool
	paused     bool
	held       bool
	volume     float32
	transition *streamTransition
	tail       []float32
	tailFrame  int64
	result     string
}

type raylibDevice struct {
	mu      sync.Mutex
	output  rl.AudioStream
	streams []*stream
}

func (native *raylibDevice) open() bool {
	rl.InitAudioDevice()
	if !rl.IsAudioDeviceReady() {
		return false
	}
	native.output = rl.LoadAudioStream(48000, 32, 2)
	if !rl.IsAudioStreamValid(native.output) {
		if native.output.Buffer != nil {
			rl.UnloadAudioStream(native.output)
		}
		rl.CloseAudioDevice()
		return false
	}
	// One callback supplies all long streams. The binding's cgo implementation
	// supports only one stream callback at a time; mixing here also preserves
	// sample-accurate boundaries before raylib's device-rate conversion.
	rl.SetAudioStreamCallback(native.output, native.renderStreams)
	rl.PlayAudioStream(native.output)
	return true
}
func (native *raylibDevice) ready() bool { return rl.IsAudioDeviceReady() }
func (native *raylibDevice) close() {
	rl.StopAudioStream(native.output)
	rl.UnloadAudioStream(native.output)
	native.output = rl.AudioStream{}
	rl.CloseAudioDevice()
}
func (*raylibDevice) attachMixed(processor *mixProcessor) {
	rl.AttachAudioMixedProcessor(processor.callback)
}
func (*raylibDevice) detachMixed(processor *mixProcessor) {
	rl.DetachAudioMixedProcessor(processor.callback)
}
func (*raylibDevice) loadSound(data []byte, tail bool) (*sound, error) {
	wave := rl.LoadWaveFromMemory(".wav", data, int32(len(data)))
	if !rl.IsWaveValid(wave) {
		if wave.Data != nil {
			rl.UnloadWave(wave)
		}
		return nil, errors.New("invalid wave")
	}
	defer rl.UnloadWave(wave)
	var samples []float32
	if tail {
		var err error
		samples, err = waveSamples(wave)
		if err != nil {
			return nil, err
		}
	}
	value := rl.LoadSoundFromWave(wave)
	if !rl.IsSoundValid(value) {
		if value.Stream.Buffer != nil {
			rl.UnloadSound(value)
		}
		return nil, errors.New("sound allocation failed")
	}
	return &sound{value: value, tail: samples}, nil
}

// waveSamples copies a bounded 48 kHz mono or stereo wave into stereo PCM.
func waveSamples(wave rl.Wave) ([]float32, error) {
	if wave.SampleRate != 48000 || (wave.Channels != 1 && wave.Channels != 2) || wave.FrameCount == 0 || wave.FrameCount > 48000*600 {
		return nil, errors.New("invalid or oversized stream wave")
	}
	source := rl.LoadWaveSamples(wave)
	if len(source) == 0 {
		return nil, errors.New("could not allocate wave samples")
	}
	defer rl.UnloadWaveSamples(source)
	result := make([]float32, int(wave.FrameCount)*2)
	if wave.Channels == 2 {
		copy(result, source)
	} else {
		for i, sample := range source {
			result[2*i], result[2*i+1] = sample, sample
		}
	}
	return result, nil
}

func (*raylibDevice) unloadSound(item *sound) { rl.UnloadSound(item.value) }
func (*raylibDevice) alias(item *sound) (*sound, error) {
	value := rl.LoadSoundAlias(item.value)
	if !rl.IsSoundValid(value) {
		if value.Stream.Buffer != nil {
			rl.UnloadSoundAlias(value)
		}
		return nil, errors.New("sound alias allocation failed")
	}
	return &sound{value: value}, nil
}
func (*raylibDevice) unloadAlias(item *sound) { rl.UnloadSoundAlias(item.value) }
func (*raylibDevice) playSound(item *sound, volume, pan float32) {
	rl.SetSoundVolume(item.value, volume)
	rl.SetSoundPan(item.value, pan)
	rl.PlaySound(item.value)
}
func (*raylibDevice) soundVolume(item *sound, volume float32) {
	rl.SetSoundVolume(item.value, volume)
}
func (*raylibDevice) stopSound(item *sound)   { rl.StopSound(item.value) }
func (*raylibDevice) pauseSound(item *sound)  { rl.PauseSound(item.value) }
func (*raylibDevice) resumeSound(item *sound) { rl.ResumeSound(item.value) }
func (*raylibDevice) playingSound(item *sound) bool {
	return rl.IsSoundPlaying(item.value)
}

// validateStreamFormat checks sample-index bounds against the decoded stream.
func validateStreamFormat(frames int64, sampleRate, channels uint32, audio *content.AudioRequirement) error {
	if audio == nil || frames <= 0 || frames > 48000*600 || sampleRate != 48000 || (channels != 1 && channels != 2) {
		return errors.New("invalid or oversized music stream")
	}
	if (audio.LoopStartSample == nil) != (audio.LoopEndSample == nil) {
		return errors.New("incomplete stream loop")
	}
	if audio.LoopStartSample != nil && (*audio.LoopStartSample < 0 || *audio.LoopEndSample <= *audio.LoopStartSample || *audio.LoopEndSample > frames) {
		return errors.New("invalid stream loop")
	}
	for _, marker := range audio.Markers {
		if marker.Sample < 0 || marker.Sample > frames {
			return errors.New("invalid stream marker")
		}
	}
	return nil
}

func (native *raylibDevice) loadStream(data []byte, audio *content.AudioRequirement) (*stream, error) {
	if len(data) == 0 || len(data) > maxAudioBytes || audio == nil {
		return nil, errors.New("invalid music stream input")
	}
	// Inspect the compressed stream before decoding, bounding the later PCM
	// allocation. The decoder retains the source until it is unloaded.
	pin := new(runtime.Pinner)
	pin.Pin(&data[0])
	value := rl.LoadMusicStreamFromMemory(".ogg", data, int32(len(data)))
	if !rl.IsMusicValid(value) || !rl.IsAudioStreamValid(value.Stream) {
		if value.Stream.Buffer != nil || value.CtxData != nil {
			rl.UnloadMusicStream(value)
		}
		pin.Unpin()
		return nil, errors.New("invalid music stream")
	}
	frames := int64(value.FrameCount)
	validationErr := validateStreamFormat(frames, value.Stream.SampleRate, value.Stream.Channels, audio)
	rl.UnloadMusicStream(value)
	pin.Unpin()
	if validationErr != nil {
		return nil, validationErr
	}
	item := &stream{data: data, frames: frames}
	if audio.LoopStartSample != nil {
		item.loopStart, item.loopEnd = *audio.LoopStartSample, *audio.LoopEndSample
	}
	native.mu.Lock()
	native.streams = append(native.streams, item)
	native.mu.Unlock()
	return item, nil
}

func (native *raylibDevice) unloadStream(item *stream) {
	native.mu.Lock()
	for i, current := range native.streams {
		if current == item {
			native.streams = append(native.streams[:i], native.streams[i+1:]...)
			break
		}
	}
	item.pcm, item.data, item.tail = nil, nil, nil
	native.mu.Unlock()
}

func (native *raylibDevice) playStream(item *stream) error {
	wave := rl.LoadWaveFromMemory(".ogg", item.data, int32(len(item.data)))
	if !rl.IsWaveValid(wave) {
		if wave.Data != nil {
			rl.UnloadWave(wave)
		}
		return errors.New("invalid music wave")
	}
	samples, err := waveSamples(wave)
	rl.UnloadWave(wave)
	if err != nil {
		return err
	}
	if int64(len(samples)/2) != item.frames {
		return errors.New("decoded music length changed")
	}
	native.mu.Lock()
	item.pcm = samples
	item.playing = true
	item.paused = false
	item.held = false
	item.result = ""
	native.mu.Unlock()
	return nil
}
func (native *raylibDevice) stopStream(item *stream) {
	native.mu.Lock()
	item.playing = false
	item.pcm, item.tail = nil, nil
	native.mu.Unlock()
}
func (native *raylibDevice) pauseStream(item *stream) {
	native.mu.Lock()
	item.paused = true
	native.mu.Unlock()
}
func (native *raylibDevice) resumeStream(item *stream) {
	native.mu.Lock()
	item.paused = false
	item.held = false
	if item.result == "hold" {
		item.result = ""
	}
	native.mu.Unlock()
}
func (native *raylibDevice) updateStream(_ *stream) bool {
	return rl.IsAudioDeviceReady() && rl.IsAudioStreamValid(native.output)
}
func (native *raylibDevice) streamVolume(item *stream, volume float32) {
	native.mu.Lock()
	item.volume = volume
	native.mu.Unlock()
}
func (native *raylibDevice) streamTime(item *stream) float32 {
	native.mu.Lock()
	defer native.mu.Unlock()
	return float32(item.position) / 48000
}
func (native *raylibDevice) seekStream(item *stream, seconds float32) {
	native.mu.Lock()
	item.position = int64(float64(seconds) * 48000)
	native.mu.Unlock()
}

func (native *raylibDevice) streamTransition(item *stream, transition *streamTransition, tail *sound) error {
	native.mu.Lock()
	defer native.mu.Unlock()
	if transition != nil && (transition.marker < 0 || transition.marker > item.frames || (transition.action == "tail" && (tail == nil || len(tail.tail) == 0))) {
		return errors.New("invalid stream transition")
	}
	// A command cancellation racing the callback must not resurrect samples
	// after a hold, stop, or tail has already started.
	if item.result != "" {
		if transition == nil {
			return nil
		}
		return errors.New("stream transition already completed")
	}
	item.transition = transition
	item.tail = nil
	if tail != nil {
		item.tail = tail.tail
	}
	item.tailFrame = 0
	item.result = ""
	return nil
}

func (native *raylibDevice) streamResult(item *stream) string {
	native.mu.Lock()
	defer native.mu.Unlock()
	return item.result
}

// renderStreams is the library-owned audio callback. It makes no raylib calls
// and touches only transport state under the device mutex, never session state.
func (native *raylibDevice) renderStreams(samples []float32, frames int) {
	clear(samples)
	native.mu.Lock()
	defer native.mu.Unlock()
	for _, item := range native.streams {
		renderStream(item, samples, frames)
	}
}

// streamBoundary runs before outputting a frame, including immediately after
// the previous frame. A marker at the loop end wins over the wrap.
func streamBoundary(item *stream) {
	if !item.playing || item.paused || item.held || item.result == "tail" {
		return
	}
	if transition := item.transition; transition != nil {
		passedOneShot := item.position > transition.marker && (item.loopEnd == 0 || transition.marker < item.loopStart)
		if item.position == transition.marker || passedOneShot {
			item.transition = nil
			item.result = transition.action
			switch transition.action {
			case "hold":
				item.held = true
			case "stop":
				item.playing = false
			case "tail":
				item.tailFrame = 0
			}
			return
		}
	}
	if item.loopEnd > 0 && item.position >= item.loopEnd {
		item.position = item.loopStart
		// A transition at loopStart is reached at the wrap itself.
		if item.transition != nil && item.transition.marker == item.position {
			streamBoundary(item)
		}
	}
	if item.position >= item.frames {
		item.playing = false
		item.result = "ended"
	}
}

func renderStream(item *stream, output []float32, frames int) {
	for frame := 0; frame < min(frames, len(output)/2); frame++ {
		streamBoundary(item)
		if !item.playing || item.paused || item.held {
			break
		}
		if item.result == "tail" {
			if item.tailFrame >= int64(len(item.tail)/2) {
				item.playing = false
				item.result = "tail-done"
				break
			}
			index := int(item.tailFrame) * 2
			output[2*frame] += item.tail[index] * item.volume
			output[2*frame+1] += item.tail[index+1] * item.volume
			item.tailFrame++
			continue
		}
		index := int(item.position) * 2
		if index < 0 || index+1 >= len(item.pcm) {
			item.playing = false
			item.result = "ended"
			break
		}
		output[2*frame] += item.pcm[index] * item.volume
		output[2*frame+1] += item.pcm[index+1] * item.volume
		item.position++
	}
	streamBoundary(item)
	if item.result == "tail" && item.tailFrame >= int64(len(item.tail)/2) {
		item.playing = false
		item.result = "tail-done"
	}
}

type asset struct {
	record content.ManifestRecord
	sounds []*sound
	stream *stream
}

func audioKind(kind string) bool {
	switch kind {
	case "music", "ambience", "audio-tail", "stinger", "effect", "vocal":
		return true
	default:
		return false
	}
}

func contentFailure(id string, cause error) error {
	return fmt.Errorf("%w: %s: %w", session.ContentInvalid, id, cause)
}

func readAudio(ctx context.Context, filesystem fs.FS, id, path string, metadata *content.FileMetadata) (_ []byte, err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if metadata == nil || path == "" || !fs.ValidPath(path) || !strings.HasPrefix(path, "content/") || metadata.Path != path || metadata.Size <= 0 || metadata.Size > maxAudioBytes {
		return nil, contentFailure(id, errors.New("invalid or absent export metadata"))
	}
	digest, err := hex.DecodeString(metadata.SHA256)
	if err != nil || len(digest) != sha256.Size || metadata.SHA256 != strings.ToLower(metadata.SHA256) {
		return nil, contentFailure(id, errors.New("invalid digest"))
	}
	file, err := filesystem.Open(path)
	if err != nil {
		return nil, contentFailure(id, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxAudioBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, contentFailure(id, errors.Join(readErr, closeErr))
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != metadata.Size || !bytes.Equal(sum[:], digest) {
		return nil, contentFailure(id, errors.New("export size or digest mismatch"))
	}
	return data, nil
}

// readGroup validates all required exports before touching the audio device.
func (mixer *Mixer) readGroup(ctx context.Context, group string) (_ map[string][][]byte, err error) {
	defer mon.Task()(&ctx)(&err)
	data := make(map[string][][]byte)
	for _, record := range mixer.records {
		if !audioKind(record.Kind) || (record.LoadGroup != "shared" && record.LoadGroup != group) {
			continue
		}
		if !record.Final || record.Audio == nil || len(record.AdditionalExports) != len(record.AdditionalFiles) || len(record.AdditionalExports)+1 < max(record.Audio.Variations, record.Audio.Takes) {
			return nil, contentFailure(record.ID, errors.New("unresolved final audio"))
		}
		long := record.Kind == "music" || record.Kind == "ambience"
		if long && (len(record.AdditionalExports) != 0 || record.Audio.SampleRate != 48000) {
			return nil, contentFailure(record.ID, errors.New("invalid stream metadata"))
		}
		if long && record.ID != "MUS-PRO" && record.ID != "MUS-CEREMONY" && (record.Audio.LoopStartSample == nil || record.Audio.LoopEndSample == nil || *record.Audio.LoopStartSample < 0 || *record.Audio.LoopEndSample <= *record.Audio.LoopStartSample) {
			return nil, contentFailure(record.ID, errors.New("unresolved loop points"))
		}
		for _, marker := range record.Audio.Markers {
			if !marker.Resolved || marker.Sample < 0 {
				return nil, contentFailure(record.ID, errors.New("unresolved transition marker"))
			}
		}
		paths := append([]string{record.ExportPath}, record.AdditionalExports...)
		files := append([]*content.FileMetadata{record.File}, record.AdditionalFiles...)
		for i, path := range paths {
			part, readErr := readAudio(ctx, mixer.filesystem, record.ID, path, files[i])
			if readErr != nil {
				return nil, readErr
			}
			data[record.ID] = append(data[record.ID], part)
		}
	}
	return data, nil
}

func (mixer *Mixer) acquire(ctx context.Context, group string, data map[string][][]byte) (err error) {
	defer mon.Task()(&ctx)(&err)
	mixer.assets = make(map[string]*asset)
	for _, record := range mixer.records {
		parts, ok := data[record.ID]
		if !ok {
			continue
		}
		item := &asset{record: record}
		mixer.assets[record.ID] = item
		for _, part := range parts {
			if err = ctx.Err(); err != nil {
				return err
			}
			if record.Kind == "music" || record.Kind == "ambience" {
				item.stream, err = mixer.native.loadStream(part, record.Audio)
			} else {
				var value *sound
				value, err = mixer.native.loadSound(part, record.Kind == "audio-tail")
				if err == nil {
					item.sounds = append(item.sounds, value)
				}
			}
			if err != nil {
				if !mixer.native.ready() {
					return fmt.Errorf("%w: %w", session.AudioUnavailable, err)
				}
				return contentFailure(record.ID, err)
			}
		}
	}
	mixer.group = group
	return nil
}

func (mixer *Mixer) release(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	for i := range mixer.voices {
		mixer.releaseVoice(i)
	}
	for _, record := range mixer.records {
		item := mixer.assets[record.ID]
		if item == nil {
			continue
		}
		if item.stream != nil {
			mixer.native.stopStream(item.stream)
			mixer.native.unloadStream(item.stream)
		}
		for _, value := range item.sounds {
			mixer.native.unloadSound(value)
		}
	}
	mixer.assets = nil
	mixer.streams = nil
}

func (mixer *Mixer) closeDevice(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	if mixer.attached {
		mixer.native.detachMixed(mixer.processor)
		mixer.attached = false
	}
	mixer.release(ctx)
	if mixer.opened {
		mixer.native.close()
		mixer.opened = false
	}
}

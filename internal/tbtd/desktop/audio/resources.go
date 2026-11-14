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
	loadSound([]byte) (*sound, error)
	unloadSound(*sound)
	alias(*sound) (*sound, error)
	unloadAlias(*sound)
	playSound(*sound, float32, float32)
	soundVolume(*sound, float32)
	stopSound(*sound)
	pauseSound(*sound)
	resumeSound(*sound)
	playingSound(*sound) bool
	loadStream([]byte) (*stream, error)
	unloadStream(*stream)
	playStream(*stream)
	stopStream(*stream)
	pauseStream(*stream)
	resumeStream(*stream)
	updateStream(*stream) bool
	streamVolume(*stream, float32)
	streamTime(*stream) float32
	seekStream(*stream, float32)
}

type sound struct{ value rl.Sound }

type stream struct {
	value rl.Music
	data  []byte
	pin   *runtime.Pinner
}

type raylibDevice struct{}

func (raylibDevice) open() bool {
	rl.InitAudioDevice()
	return rl.IsAudioDeviceReady()
}
func (raylibDevice) ready() bool { return rl.IsAudioDeviceReady() }
func (raylibDevice) close()      { rl.CloseAudioDevice() }
func (raylibDevice) attachMixed(processor *mixProcessor) {
	rl.AttachAudioMixedProcessor(processor.callback)
}
func (raylibDevice) detachMixed(processor *mixProcessor) {
	rl.DetachAudioMixedProcessor(processor.callback)
}
func (raylibDevice) loadSound(data []byte) (*sound, error) {
	wave := rl.LoadWaveFromMemory(".wav", data, int32(len(data)))
	if !rl.IsWaveValid(wave) {
		if wave.Data != nil {
			rl.UnloadWave(wave)
		}
		return nil, errors.New("invalid wave")
	}
	defer rl.UnloadWave(wave)
	value := rl.LoadSoundFromWave(wave)
	if !rl.IsSoundValid(value) {
		if value.Stream.Buffer != nil {
			rl.UnloadSound(value)
		}
		return nil, errors.New("sound allocation failed")
	}
	return &sound{value: value}, nil
}
func (raylibDevice) unloadSound(item *sound) { rl.UnloadSound(item.value) }
func (raylibDevice) alias(item *sound) (*sound, error) {
	value := rl.LoadSoundAlias(item.value)
	if !rl.IsSoundValid(value) {
		if value.Stream.Buffer != nil {
			rl.UnloadSoundAlias(value)
		}
		return nil, errors.New("sound alias allocation failed")
	}
	return &sound{value: value}, nil
}
func (raylibDevice) unloadAlias(item *sound) { rl.UnloadSoundAlias(item.value) }
func (raylibDevice) playSound(item *sound, volume, pan float32) {
	rl.SetSoundVolume(item.value, volume)
	rl.SetSoundPan(item.value, pan)
	rl.PlaySound(item.value)
}
func (raylibDevice) soundVolume(item *sound, volume float32) {
	rl.SetSoundVolume(item.value, volume)
}
func (raylibDevice) stopSound(item *sound)   { rl.StopSound(item.value) }
func (raylibDevice) pauseSound(item *sound)  { rl.PauseSound(item.value) }
func (raylibDevice) resumeSound(item *sound) { rl.ResumeSound(item.value) }
func (raylibDevice) playingSound(item *sound) bool {
	return rl.IsSoundPlaying(item.value)
}
func (raylibDevice) loadStream(data []byte) (*stream, error) {
	// raylib's Ogg decoder retains the source buffer for the stream lifetime.
	// Pin it until after native unload, including on Windows' pure-Go binding.
	pin := new(runtime.Pinner)
	pin.Pin(&data[0])
	value := rl.LoadMusicStreamFromMemory(".ogg", data, int32(len(data)))
	if !rl.IsMusicValid(value) {
		if value.Stream.Buffer != nil || value.CtxData != nil {
			rl.UnloadMusicStream(value)
		}
		pin.Unpin()
		return nil, errors.New("invalid music stream")
	}
	value.Looping = false
	return &stream{value: value, data: data, pin: pin}, nil
}
func (raylibDevice) unloadStream(item *stream) {
	rl.UnloadMusicStream(item.value)
	item.pin.Unpin()
	item.data = nil
}
func (raylibDevice) playStream(item *stream)   { rl.PlayMusicStream(item.value) }
func (raylibDevice) stopStream(item *stream)   { rl.StopMusicStream(item.value) }
func (raylibDevice) pauseStream(item *stream)  { rl.PauseMusicStream(item.value) }
func (raylibDevice) resumeStream(item *stream) { rl.ResumeMusicStream(item.value) }
func (raylibDevice) updateStream(item *stream) bool {
	if !rl.IsAudioDeviceReady() || !rl.IsMusicValid(item.value) {
		return false
	}
	rl.UpdateMusicStream(item.value)
	return rl.IsAudioDeviceReady() && rl.IsMusicValid(item.value)
}
func (raylibDevice) streamVolume(item *stream, volume float32) {
	rl.SetMusicVolume(item.value, volume)
}
func (raylibDevice) streamTime(item *stream) float32 { return rl.GetMusicTimePlayed(item.value) }
func (raylibDevice) seekStream(item *stream, seconds float32) {
	rl.SeekMusicStream(item.value, seconds)
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
				item.stream, err = mixer.native.loadStream(part)
			} else {
				var value *sound
				value, err = mixer.native.loadSound(part)
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

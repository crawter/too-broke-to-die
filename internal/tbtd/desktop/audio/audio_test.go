// Copyright (C) 2026 crawter
// All rights reserved.

package audio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"slices"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

type fakeDevice struct {
	available  bool
	opened     bool
	failLoad   int
	loads      int
	failStream bool
	calls      []string
	playing    map[*sound]bool
	positions  map[*stream]float32
	volumes    map[*stream]float32
	plays      int
}

func (fake *fakeDevice) open() bool {
	fake.calls = append(fake.calls, "open")
	fake.opened = fake.available
	return fake.opened
}
func (fake *fakeDevice) ready() bool {
	return fake.opened && fake.available
}
func (fake *fakeDevice) close() {
	fake.calls = append(fake.calls, "close")
	fake.opened = false
}
func (fake *fakeDevice) attachMixed(_ *mixProcessor) {
	fake.calls = append(fake.calls, "attach")
}
func (fake *fakeDevice) detachMixed(_ *mixProcessor) {
	fake.calls = append(fake.calls, "detach")
}
func (fake *fakeDevice) loadSound(_ []byte, tail bool) (*sound, error) {
	fake.calls = append(fake.calls, "load-sound")
	fake.loads++
	if fake.loads == fake.failLoad {
		return nil, errors.New("injected decode failure")
	}
	item := &sound{}
	if tail {
		item.tail = []float32{1, 1}
	}
	return item, nil
}
func (fake *fakeDevice) unloadSound(_ *sound) {
	fake.calls = append(fake.calls, "unload-sound")
}
func (fake *fakeDevice) alias(_ *sound) (*sound, error) {
	return &sound{}, nil
}
func (fake *fakeDevice) unloadAlias(_ *sound) {
	fake.calls = append(fake.calls, "unload-alias")
}
func (fake *fakeDevice) playSound(item *sound, _, _ float32) {
	fake.playing[item] = true
	fake.plays++
}
func (fake *fakeDevice) soundVolume(_ *sound, _ float32) {}
func (fake *fakeDevice) stopSound(item *sound) {
	delete(fake.playing, item)
}
func (fake *fakeDevice) pauseSound(item *sound) {
	fake.playing[item] = false
}
func (fake *fakeDevice) resumeSound(item *sound) {
	fake.playing[item] = true
}
func (fake *fakeDevice) playingSound(item *sound) bool {
	return fake.playing[item]
}
func (fake *fakeDevice) loadStream(_ []byte, audio *content.AudioRequirement) (*stream, error) {
	fake.calls = append(fake.calls, "load-stream")
	item := &stream{frames: 48000 * 100}
	if audio.LoopStartSample != nil && audio.LoopEndSample != nil {
		item.loopStart, item.loopEnd = *audio.LoopStartSample, *audio.LoopEndSample
	}
	return item, nil
}
func (fake *fakeDevice) unloadStream(_ *stream) {
	fake.calls = append(fake.calls, "unload-stream")
}
func (fake *fakeDevice) playStream(item *stream) error {
	fake.calls = append(fake.calls, "play-stream")
	item.playing, item.paused, item.held = true, false, false
	item.result = ""
	return nil
}
func (fake *fakeDevice) stopStream(item *stream) {
	fake.calls = append(fake.calls, "stop-stream")
	item.playing = false
}
func (fake *fakeDevice) pauseStream(item *stream) {
	fake.calls = append(fake.calls, "pause-stream")
	item.paused = true
}
func (fake *fakeDevice) resumeStream(item *stream) {
	fake.calls = append(fake.calls, "resume-stream")
	item.paused, item.held = false, false
	if item.result == "hold" {
		item.result = ""
	}
}
func (fake *fakeDevice) updateStream(item *stream) bool {
	if fake.failStream || !fake.ready() {
		return false
	}
	item.position = int64(float64(fake.positions[item]) * 48000)
	before := item.result
	streamBoundary(item)
	if item.result == "tail" && before != "tail" {
		fake.plays++
	}
	fake.positions[item] = float32(item.position) / 48000
	return true
}
func (fake *fakeDevice) streamVolume(item *stream, volume float32) {
	fake.volumes[item] = volume
}
func (fake *fakeDevice) streamTime(item *stream) float32 {
	return fake.positions[item]
}
func (fake *fakeDevice) seekStream(item *stream, position float32) {
	fake.positions[item] = position
}
func (fake *fakeDevice) streamTransition(item *stream, transition *streamTransition, tail *sound) error {
	if item.result != "" {
		if transition == nil {
			return nil
		}
		return errors.New("stream transition already completed")
	}
	item.transition = transition
	item.result = ""
	if tail != nil {
		item.tail = tail.tail
	}
	return nil
}
func (fake *fakeDevice) streamResult(item *stream) string {
	return item.result
}

func fixture(t *testing.T) (fstest.MapFS, []content.ManifestRecord, *fakeDevice) {
	t.Helper()
	files := fstest.MapFS{}
	var records []content.ManifestRecord
	for _, spec := range []struct{ id, kind, group string }{
		{"SFX-BODY-IMPACT", "effect", "shared"},
		{"SFX-WARN-ENEMY", "effect", "shared"},
		{"SFX-UI-SAVED", "effect", "shared"},
		{"STG-CREDIT", "stinger", "shared"},
		{"STG-VICTORY", "stinger", "shared"},
		{"VOC-TRE-LEVEL", "vocal", "location-ins"},
		{"MUS-INS-EXP", "music", "location-ins"},
		{"MUS-INS-CBT", "music", "location-ins"},
		{"MUS-INS-EXP-TAIL", "audio-tail", "location-ins"},
		{"MUS-PRO", "music", "shared"},
		{"MUS-FUNERAL", "music", "shared"},
		{"MUS-CEREMONY", "music", "ending"},
		{"MUS-CEREMONY-TAIL", "audio-tail", "ending"},
	} {
		path := "content/" + spec.id
		body := []byte(spec.id)
		sum := sha256.Sum256(body)
		files[path] = &fstest.MapFile{Data: body}
		requirement := &content.AudioRequirement{Variations: 1, SampleRate: 48000}
		if spec.kind == "music" {
			if spec.id != "MUS-PRO" && spec.id != "MUS-CEREMONY" {
				start, end := int64(48000), int64(96000)
				requirement.LoopStartSample, requirement.LoopEndSample = &start, &end
			}
			name := "transition"
			switch spec.id {
			case "MUS-PRO":
				name = "heart_attack_break"
			case "MUS-FUNERAL":
				name = "resurrection_stop"
			case "MUS-CEREMONY":
				name = "held_chord"
			}
			requirement.Markers = []content.TransitionMarker{{Name: name, Sample: 72000, Resolved: true}}
		}
		records = append(records, content.ManifestRecord{ID: spec.id, Kind: spec.kind, LoadGroup: spec.group, Final: true, Audio: requirement, ExportPath: path, File: &content.FileMetadata{Path: path, Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}})
	}
	fake := &fakeDevice{available: true, playing: make(map[*sound]bool), positions: make(map[*stream]float32), volumes: make(map[*stream]float32)}
	return files, records, fake
}

func makeMixer(t *testing.T) (*Mixer, *fakeDevice) {
	t.Helper()
	files, records, fake := fixture(t)
	mixer, err := newMixer(context.Background(), files, records, func() uint64 { return 1 }, "location-ins", fake)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mixer.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return mixer, fake
}

func frameView(t *testing.T, state session.AppState, events ...session.SemanticEvent) session.FrameView {
	t.Helper()
	view, err := session.NewFrameView(context.Background(), session.FrameViewData{State: state, Events: events}, campaign.Snapshot{}, "campaign", 0)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func event(id string, tick uint64) session.SemanticEvent {
	return session.SemanticEvent{Kind: "effect", Entity: "entity", Tick: tick, Parameters: []session.EventParameter{{Name: "asset", Value: id}}}
}

func frame() Frame {
	return Frame{Focused: true, Elapsed: time.Second / 60, Settings: settings.Audio{Master: 80, Music: 65, Effects: 90, Ambience: 55, Vocal: 80}}
}

func TestCueIdentitiesAndAcknowledgment(t *testing.T) {
	mixer, fake := makeMixer(t)
	ctx := context.Background()
	cue := event("SFX-BODY-IMPACT", 10)
	view := frameView(t, session.StateActive, cue)
	for i := 0; i < 2; i++ {
		if err := mixer.Update(ctx, view, frame()); err != nil {
			t.Fatal(err)
		}
	}
	if fake.plays != 1 {
		t.Fatalf("duplicate played %d times", fake.plays)
	}
	cue.Ordinal++
	if err := mixer.Update(ctx, frameView(t, session.StateActive, cue), frame()); err != nil || fake.plays != 2 {
		t.Fatalf("different ordinal: %d %v", fake.plays, err)
	}
	data := session.FrameViewData{State: session.StateActive, Progress: session.ProgressView{Revision: 7}}
	durable := session.SemanticEvent{Kind: "receipt", Durable: true, Revision: 7, Operation: session.OperationID{Event: "D-HOS-SUP-01", Sequence: 1}}
	if !mixer.acceptEvent(durable, data) || mixer.acceptEvent(durable, data) {
		t.Fatal("durable identity not deduplicated")
	}
	durable.Operation.Event = "D-HOS-SUP-02"
	if !mixer.acceptEvent(durable, data) {
		t.Fatal("distinct event identity suppressed")
	}
	durable.Revision = 8
	if mixer.acceptEvent(durable, data) {
		t.Fatal("future revision accepted")
	}
	pending := event("STG-VICTORY", 12)
	if err := mixer.playEvent(ctx, pending, data, frame(), false); err != nil || fake.plays != 2 {
		t.Fatal("transient victory played")
	}
	durable.Kind = "victory"
	durable.Revision = 7
	data.Save = session.SavePending
	if err := mixer.playEvent(ctx, durable, data, frame(), false); err != nil || fake.plays != 2 {
		t.Fatal("pending victory played")
	}
	data.Save = session.SaveConfirmed
	if err := mixer.playEvent(ctx, durable, data, frame(), false); err != nil || fake.plays != 3 {
		t.Fatalf("confirmed victory not played: %v", err)
	}
	data.State = session.StateRestoring
	if mixer.acceptEvent(durable, data) {
		t.Fatal("historical cue replayed on restoration")
	}
}

func TestAuthoritativeEffectCue(t *testing.T) {
	mixer, fake := makeMixer(t)
	ctx := context.Background()
	data := session.FrameViewData{State: session.StateActive, World: session.WorldView{Generation: 1, Effects: []session.EffectEvent{{Tick: 20, Entity: "enemy", Ordinal: 2, Kind: "hit"}}}}
	view, err := session.NewFrameView(ctx, data, campaign.Snapshot{}, "campaign", 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = mixer.Update(ctx, view, frame()); err != nil {
			t.Fatal(err)
		}
	}
	if fake.plays != 1 {
		t.Fatalf("effect replayed: %d", fake.plays)
	}
	if cue, ok := effectCue(session.EffectEvent{Kind: "hit", Entity: "player"}); !ok || eventAsset(cue) != "SFX-BODY-HURT" {
		t.Fatal("player damage did not select hurt")
	}
	if _, ok := effectCue(session.EffectEvent{Kind: "bounce"}); ok {
		t.Fatal("decorative bounce invented an impact")
	}
}

func TestMissingInvalidAndPartialAcquisition(t *testing.T) {
	ctx := context.Background()
	files, records, fake := fixture(t)
	delete(files, records[0].ExportPath)
	if mixer, err := newMixer(ctx, files, records, func() uint64 { return 1 }, "shared", fake); mixer != nil || !errors.Is(err, session.ContentInvalid) || len(fake.calls) != 0 {
		t.Fatalf("missing file: %v %v", fake.calls, err)
	}
	files, records, fake = fixture(t)
	files[records[0].ExportPath].Data[0] ^= 1
	if _, err := newMixer(ctx, files, records, func() uint64 { return 1 }, "shared", fake); !errors.Is(err, session.ContentInvalid) || len(fake.calls) != 0 {
		t.Fatalf("digest mismatch: %v %v", fake.calls, err)
	}
	files, records, fake = fixture(t)
	fake.failLoad = 2
	if mixer, err := newMixer(ctx, files, records, func() uint64 { return 1 }, "shared", fake); mixer != nil || !errors.Is(err, session.ContentInvalid) {
		t.Fatalf("partial failure: %v", err)
	}
	if !slices.Contains(fake.calls, "unload-sound") || fake.calls[len(fake.calls)-1] != "close" {
		t.Fatalf("partial cleanup: %v", fake.calls)
	}
}

func TestGroupReplacementAndSilentValidation(t *testing.T) {
	ctx := context.Background()
	files, records, fake := fixture(t)
	mixer, err := newMixer(ctx, files, records, func() uint64 { return 1 }, "shared", fake)
	if err != nil {
		t.Fatal(err)
	}
	if err = mixer.LoadGroup(ctx, "location-ins"); err != nil {
		t.Fatal(err)
	}
	if mixer.assets["MUS-INS-EXP"] == nil || mixer.group != "location-ins" {
		t.Fatal("location resources not loaded")
	}
	fake.available = false
	if err = mixer.Update(ctx, frameView(t, session.StateActive), frame()); !errors.Is(err, session.AudioUnavailable) {
		t.Fatal("expected unavailable device")
	}
	removed := files[records[0].ExportPath]
	delete(files, records[0].ExportPath)
	if err = mixer.ContinueSilent(ctx); !errors.Is(err, session.ContentInvalid) {
		t.Fatalf("silent fallback accepted missing current content: %v", err)
	}
	files[records[0].ExportPath] = removed
	if err = mixer.ContinueSilent(ctx); err != nil {
		t.Fatal(err)
	}
	delete(files, records[0].ExportPath)
	if err = mixer.LoadGroup(ctx, "shared"); !errors.Is(err, session.ContentInvalid) || mixer.group != "location-ins" {
		t.Fatalf("silent fallback accepted missing content: %v", err)
	}
	if err = mixer.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestFailedGroupCanReloadPreviousGroup(t *testing.T) {
	ctx := context.Background()
	files, records, fake := fixture(t)
	mixer, err := newMixer(ctx, files, records, func() uint64 { return 1 }, "shared", fake)
	if err != nil {
		t.Fatal(err)
	}
	fake.failLoad = fake.loads + 2
	if err = mixer.LoadGroup(ctx, "location-ins"); !errors.Is(err, session.ContentInvalid) || mixer.assets != nil {
		t.Fatalf("group failure: %v", err)
	}
	fake.failLoad = 0
	if err = mixer.LoadGroup(ctx, "shared"); err != nil || mixer.assets["SFX-BODY-IMPACT"] == nil {
		t.Fatalf("previous group did not rebuild: %v", err)
	}
	if err = mixer.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestVoicePriorityAndChant(t *testing.T) {
	mixer, fake := makeMixer(t)
	ctx := context.Background()
	data := session.FrameViewData{State: session.StateActive}
	for i := 0; i < 32; i++ {
		if err := mixer.playEvent(ctx, event("SFX-BODY-IMPACT", uint64(i)), data, frame(), false); err != nil {
			t.Fatal(err)
		}
	}
	if err := mixer.playEvent(ctx, event("SFX-BODY-IMPACT", 33), data, frame(), false); err != nil || fake.plays != 32 {
		t.Fatal("voice cap not enforced")
	}
	if err := mixer.playEvent(ctx, event("SFX-WARN-ENEMY", 34), data, frame(), false); err != nil || fake.plays != 33 || !slices.Contains(fake.calls, "unload-alias") {
		t.Fatal("warning did not replace lower priority")
	}
	mixer.releaseVoice(0)
	if err := mixer.playEvent(ctx, event("VOC-TRE-LEVEL", 35), data, frame(), false); err != nil {
		t.Fatal(err)
	}
	before := fake.plays
	mixer.clock = 10 * time.Second
	if err := mixer.playEvent(ctx, event("VOC-TRE-LEVEL", 36), data, frame(), false); err != nil || fake.plays != before {
		t.Fatal("overlapping chant")
	}
	for i, voice := range mixer.voices {
		if voice.chant {
			mixer.releaseVoice(i)
		}
	}
	mixer.clock = 4*time.Second - time.Nanosecond
	if err := mixer.playEvent(ctx, event("VOC-TRE-LEVEL", 37), data, frame(), false); err != nil || fake.plays != before {
		t.Fatal("chant interval too short")
	}
	mixer.clock = 4 * time.Second
	if err := mixer.playEvent(ctx, event("VOC-TRE-LEVEL", 38), data, frame(), false); err != nil || fake.plays != before+1 {
		t.Fatal("chant at boundary suppressed")
	}
}

func TestStreamsMixPauseAndFocus(t *testing.T) {
	mixer, fake := makeMixer(t)
	ctx := context.Background()
	if err := mixer.startStream(ctx, "MUS-INS-EXP"); err != nil {
		t.Fatal(err)
	}
	f := frame()
	f.Elapsed = 500 * time.Millisecond
	if err := mixer.advanceStreams(f, false); err != nil || mixer.streams["MUS-INS-EXP"].gain != 0.5 {
		t.Fatal("initial fade")
	}
	if err := mixer.startStream(ctx, "MUS-INS-CBT"); err != nil {
		t.Fatal(err)
	}
	f.Elapsed = time.Second
	if err := mixer.advanceStreams(f, false); err != nil || mixer.streams["MUS-INS-EXP"] != nil || mixer.streams["MUS-INS-CBT"].gain != 1 {
		t.Fatal("combat crossfade")
	}
	item := mixer.assets["MUS-INS-CBT"].stream
	fake.positions[item] = 2
	if err := mixer.advanceStreams(f, false); err != nil || fake.positions[item] != 1 {
		t.Fatal("loop did not wrap at the authored boundary")
	}
	f.Focused = false
	if err := mixer.advanceStreams(f, false); err != nil || !mixer.streams["MUS-INS-CBT"].paused {
		t.Fatal("focus did not pause stream")
	}
	f.Focused = true
	if err := mixer.advanceStreams(f, true); err != nil || !mixer.streams["MUS-INS-CBT"].paused {
		t.Fatal("ledger did not pause stream")
	}
	if err := mixer.advanceStreams(f, false); err != nil || mixer.streams["MUS-INS-CBT"].paused {
		t.Fatal("stream did not resume")
	}
	if validLevels(settings.Audio{Master: -1}) || validLevels(settings.Audio{Music: 101}) || !validLevels(f.Settings) {
		t.Fatal("invalid level bounds")
	}
	if voiceVolume(settings.Audio{}, false) != 0 || math.Abs(float64(voiceVolume(settings.Audio{Master: 100, Effects: 100}, false))-peakCeiling) > 1e-6 {
		t.Fatal("volume endpoints")
	}
	mixer.voices[0] = voice{sound: &sound{}, priority: 6}
	f.Elapsed = 50 * time.Millisecond
	mixer.updateMix(f)
	if math.Abs(mixer.musicDuck-math.Pow(10, -6.0/20)) > 1e-9 || math.Abs(mixer.ambientDuck-math.Pow(10, -8.0/20)) > 1e-9 {
		t.Fatal("warning attack")
	}
	mixer.voices[0] = voice{}
	f.Elapsed = 300 * time.Millisecond
	mixer.updateMix(f)
	if mixer.musicDuck != 1 || mixer.ambientDuck != 1 {
		t.Fatal("warning release")
	}
}

func TestStreamFormatBounds(t *testing.T) {
	point := func(value int64) *int64 { return &value }
	for _, tc := range []struct {
		name     string
		frames   int64
		rate     uint32
		channels uint32
		audio    *content.AudioRequirement
		valid    bool
	}{
		{"nonlooping", 100, 48000, 2, &content.AudioRequirement{}, true},
		{"mono and endpoint marker", 100, 48000, 1, &content.AudioRequirement{Markers: []content.TransitionMarker{{Sample: 100}}}, true},
		{"loop to end", 100, 48000, 2, &content.AudioRequirement{LoopStartSample: point(0), LoopEndSample: point(100)}, true},
		{"maximum length", 48000 * 600, 48000, 2, &content.AudioRequirement{}, true},
		{"missing metadata", 100, 48000, 2, nil, false},
		{"empty", 0, 48000, 2, &content.AudioRequirement{}, false},
		{"oversized", 48000*600 + 1, 48000, 2, &content.AudioRequirement{}, false},
		{"sample rate", 100, 44100, 2, &content.AudioRequirement{}, false},
		{"channels", 100, 48000, 3, &content.AudioRequirement{}, false},
		{"missing loop end", 100, 48000, 2, &content.AudioRequirement{LoopStartSample: point(0)}, false},
		{"missing loop start", 100, 48000, 2, &content.AudioRequirement{LoopEndSample: point(100)}, false},
		{"negative loop", 100, 48000, 2, &content.AudioRequirement{LoopStartSample: point(-1), LoopEndSample: point(100)}, false},
		{"empty loop", 100, 48000, 2, &content.AudioRequirement{LoopStartSample: point(10), LoopEndSample: point(10)}, false},
		{"loop beyond audio", 100, 48000, 2, &content.AudioRequirement{LoopStartSample: point(0), LoopEndSample: point(101)}, false},
		{"negative marker", 100, 48000, 2, &content.AudioRequirement{Markers: []content.TransitionMarker{{Sample: -1}}}, false},
		{"marker beyond audio", 100, 48000, 2, &content.AudioRequirement{Markers: []content.TransitionMarker{{Sample: 101}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStreamFormat(tc.frames, tc.rate, tc.channels, tc.audio)
			if (err == nil) != tc.valid {
				t.Fatalf("validation: %v", err)
			}
		})
	}
	device := &raylibDevice{}
	if _, err := device.loadStream(nil, &content.AudioRequirement{}); err == nil {
		t.Fatal("empty compressed input accepted")
	}
	if _, err := device.loadStream([]byte{1}, nil); err == nil {
		t.Fatal("missing metadata accepted")
	}
}

func TestLoopOvershootIsPreserved(t *testing.T) {
	newTrack := func() *stream {
		return &stream{pcm: []float32{1, 1, 2, 2, 3, 3, 4, 4}, frames: 4, loopStart: 1, loopEnd: 3, playing: true, volume: 1}
	}
	whole := make([]float32, 24)
	renderStream(newTrack(), whole, 12)
	track := newTrack()
	chunked := make([]float32, len(whole))
	for offset := 0; offset < len(chunked); {
		end := min(offset+6, len(chunked))
		renderStream(track, chunked[offset:end], (end-offset)/2)
		offset = end
	}
	if !slices.Equal(whole, chunked) || !slices.Equal(whole, []float32{1, 1, 2, 2, 3, 3, 2, 2, 3, 3, 2, 2, 3, 3, 2, 2, 3, 3, 2, 2, 3, 3, 2, 2}) {
		t.Fatalf("loop output changed with buffer partition: %v / %v", whole, chunked)
	}
}

func TestStreamOutputBoundaries(t *testing.T) {
	pcm := func(values ...float32) []float32 {
		var result []float32
		for _, value := range values {
			result = append(result, value, -value)
		}
		return result
	}
	newTrack := func() *stream {
		return &stream{pcm: pcm(10, 11, 12, 13, 14, 15, 16, 17), frames: 8, loopStart: 2, loopEnd: 6, playing: true, volume: 1}
	}
	render := func(native *raylibDevice, chunks ...int) []float32 {
		var result []float32
		for _, frames := range chunks {
			buffer := make([]float32, frames*2)
			native.renderStreams(buffer, frames)
			result = append(result, buffer...)
		}
		return result
	}
	t.Run("unaligned repeated loop", func(t *testing.T) {
		item := newTrack()
		native := &raylibDevice{streams: []*stream{item}}
		got := render(native, 3, 4, 1, 5)
		want := pcm(10, 11, 12, 13, 14, 15, 12, 13, 14, 15, 12, 13, 14)
		if !slices.Equal(got, want) {
			t.Fatalf("loop output %v, want %v", got, want)
		}
	})
	for _, action := range []string{"hold", "stop", "tail"} {
		t.Run(action, func(t *testing.T) {
			item := newTrack()
			native := &raylibDevice{streams: []*stream{item}}
			tail := &sound{tail: pcm(50, 51, 52)}
			if err := native.streamTransition(item, &streamTransition{marker: 4, action: action}, tail); err != nil {
				t.Fatal(err)
			}
			got := render(native, 3, 2, 4)
			want := pcm(10, 11, 12, 13, 0, 0, 0, 0, 0)
			if action == "tail" {
				want = pcm(10, 11, 12, 13, 50, 51, 52, 0, 0)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("%s output %v, want %v", action, got, want)
			}
		})
	}
	t.Run("loop end marker precedes wrap", func(t *testing.T) {
		item := newTrack()
		native := &raylibDevice{streams: []*stream{item}}
		if err := native.streamTransition(item, &streamTransition{marker: 6, action: "stop"}, nil); err != nil {
			t.Fatal(err)
		}
		if got, want := render(native, 4, 5), pcm(10, 11, 12, 13, 14, 15, 0, 0, 0); !slices.Equal(got, want) {
			t.Fatalf("loop end output %v, want %v", got, want)
		}
	})
	t.Run("cancellation cannot resurrect a started tail", func(t *testing.T) {
		item := newTrack()
		native := &raylibDevice{streams: []*stream{item}}
		if err := native.streamTransition(item, &streamTransition{marker: 4, action: "tail"}, &sound{tail: pcm(50, 51, 52)}); err != nil {
			t.Fatal(err)
		}
		render(native, 5)
		if err := native.streamTransition(item, nil, nil); err != nil {
			t.Fatal(err)
		}
		if got, want := render(native, 4), pcm(51, 52, 0, 0); !slices.Equal(got, want) {
			t.Fatalf("tail output after cancellation %v, want %v", got, want)
		}
	})
	t.Run("marker next iteration and paused", func(t *testing.T) {
		item := newTrack()
		native := &raylibDevice{streams: []*stream{item}}
		render(native, 5)
		if err := native.streamTransition(item, &streamTransition{marker: 4, action: "stop"}, nil); err != nil {
			t.Fatal(err)
		}
		native.pauseStream(item)
		if got := render(native, 3); !slices.Equal(got, pcm(0, 0, 0)) {
			t.Fatalf("paused output %v", got)
		}
		native.resumeStream(item)
		if got, want := render(native, 2, 4), pcm(15, 12, 13, 0, 0, 0); !slices.Equal(got, want) {
			t.Fatalf("next iteration output %v, want %v", got, want)
		}
	})
}

func TestStreamCallbackAndControlsAreSynchronized(t *testing.T) {
	item := &stream{pcm: []float32{1, 1, 2, 2, 3, 3}, frames: 3, loopStart: 1, loopEnd: 3, playing: true, volume: 1}
	native := &raylibDevice{streams: []*stream{item}}
	var done sync.WaitGroup
	done.Add(1)
	go func() {
		defer done.Done()
		for i := 0; i < 500; i++ {
			buffer := make([]float32, 14)
			native.renderStreams(buffer, 7)
		}
	}()
	for i := 0; i < 500; i++ {
		native.streamVolume(item, float32(i%2))
		native.pauseStream(item)
		native.resumeStream(item)
		_ = native.streamTime(item)
	}
	done.Wait()
}

func TestMixerSchedulesSampleBoundaryBeforeOutput(t *testing.T) {
	mixer, fake := makeMixer(t)
	ctx := context.Background()
	id := "MUS-INS-EXP"
	item := mixer.assets[id]
	start, end := int64(2), int64(6)
	item.record.Audio.LoopStartSample, item.record.Audio.LoopEndSample = &start, &end
	item.record.Audio.Markers[0].Sample = 4
	item.stream.frames, item.stream.loopStart, item.stream.loopEnd = 8, start, end
	f := frame()
	f.Elapsed = 0
	if err := mixer.Update(ctx, frameView(t, session.StateActive, event(id, 1), transitionEvent(id, "transition", "tail", 2)), f); err != nil {
		t.Fatal(err)
	}
	item.stream.pcm = []float32{1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8}
	item.stream.volume = 1
	output := make([]float32, 16)
	(&raylibDevice{streams: []*stream{item.stream}}).renderStreams(output[:6], 3)
	(&raylibDevice{streams: []*stream{item.stream}}).renderStreams(output[6:], 5)
	want := []float32{1, 1, 2, 2, 3, 3, 4, 4, 1, 1, 0, 0, 0, 0, 0, 0}
	if !slices.Equal(output, want) {
		t.Fatalf("emitted %v, want %v", output, want)
	}
	fake.positions[item.stream] = float32(item.stream.position) / 48000
	if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || mixer.streams[id] != nil || mixer.desired["music"] != "" {
		t.Fatalf("completed tail was not retired: %v", err)
	}
}

func TestStreamCrossfadeReversal(t *testing.T) {
	ctx := context.Background()
	mixer, fake := makeMixer(t)
	f := frame()
	f.Elapsed = time.Second
	if err := mixer.startStream(ctx, "MUS-INS-EXP"); err != nil {
		t.Fatal(err)
	}
	if err := mixer.advanceStreams(f, false); err != nil {
		t.Fatal(err)
	}
	if err := mixer.startStream(ctx, "MUS-INS-CBT"); err != nil {
		t.Fatal(err)
	}
	f.Elapsed = 250 * time.Millisecond
	if err := mixer.advanceStreams(f, false); err != nil {
		t.Fatal(err)
	}
	before := len(fake.calls)
	if err := mixer.startStream(ctx, "MUS-INS-EXP"); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != before || mixer.streams["MUS-INS-EXP"].target != 1 || mixer.streams["MUS-INS-CBT"].target != 0 {
		t.Fatal("reversal must reuse exploration and fade combat out")
	}
	f.Elapsed = time.Second
	if err := mixer.advanceStreams(f, false); err != nil || mixer.streams["MUS-INS-CBT"] != nil || mixer.streams["MUS-INS-EXP"].gain != 1 {
		t.Fatal("reversed fade did not complete")
	}
}

func transitionEvent(id, marker, action string, tick uint64) session.SemanticEvent {
	return session.SemanticEvent{Kind: "stream_transition", Entity: "scene", Tick: tick, Parameters: []session.EventParameter{{Name: "asset", Value: id}, {Name: "marker", Value: marker}, {Name: "action", Value: action}}}
}

func TestTransitionCommands(t *testing.T) {
	ctx := context.Background()
	mixer, fake := makeMixer(t)
	f := frame()
	id := "MUS-INS-EXP"
	if err := mixer.Update(ctx, frameView(t, session.StateActive, event(id, 1)), f); err != nil {
		t.Fatal(err)
	}
	stream := mixer.assets[id].stream
	fake.positions[stream] = 1
	if err := mixer.Update(ctx, frameView(t, session.StateActive, transitionEvent(id, "transition", "hold", 2)), f); err != nil {
		t.Fatal(err)
	}
	if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || mixer.streams[id].held {
		t.Fatal("transition executed before marker")
	}
	fake.positions[stream] = 1.5
	if err := mixer.Update(ctx, frameView(t, session.StatePause), f); err != nil || mixer.streams[id].held {
		t.Fatal("transition ran during pause")
	}
	if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || !mixer.streams[id].held {
		t.Fatal("marker did not hold the stream")
	}
	if err := mixer.startStream(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := mixer.Update(ctx, frameView(t, session.StateActive, transitionEvent(id, "transition", "tail", 3)), f); err != nil {
		t.Fatal(err)
	}
	if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || mixer.streams[id] == nil || !mixer.streams[id].tailing || fake.plays != 1 {
		t.Fatal("marker did not stop stream and play its tail")
	}
	if err := mixer.Update(ctx, frameView(t, session.StatePause), f); err != nil || !stream.paused {
		t.Fatal("tail did not pause with gameplay")
	}
}

func TestMusicTailPausesOnFocusLoss(t *testing.T) {
	ctx := context.Background()
	mixer, fake := makeMixer(t)
	f := frame()
	id := "MUS-INS-EXP"
	if err := mixer.Update(ctx, frameView(t, session.StateActive, event(id, 1)), f); err != nil {
		t.Fatal(err)
	}
	fake.positions[mixer.assets[id].stream] = 1.5
	if err := mixer.Update(ctx, frameView(t, session.StateActive, transitionEvent(id, "transition", "tail", 2)), f); err != nil {
		t.Fatal(err)
	}
	stream := mixer.assets[id].stream
	if mixer.streams[id] == nil || !mixer.streams[id].tailing || stream.paused || fake.plays != 1 {
		t.Fatal("tail did not start")
	}
	f.Focused = false
	if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || !stream.paused {
		t.Fatal("focus loss did not pause tail")
	}
	f.Focused = true
	if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || stream.paused || fake.plays != 1 {
		t.Fatal("focus return did not resume the same tail")
	}
}

func TestNarrativeTransitionMarkers(t *testing.T) {
	for _, tc := range []struct {
		id     string
		marker string
		action string
		group  string
	}{
		{"MUS-PRO", "heart_attack_break", "hold", "shared"},
		{"MUS-FUNERAL", "resurrection_stop", "stop", "shared"},
		{"MUS-CEREMONY", "held_chord", "tail", "ending"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			ctx := context.Background()
			files, records, fake := fixture(t)
			mixer, err := newMixer(ctx, files, records, func() uint64 { return 1 }, tc.group, fake)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := mixer.Close(ctx); err != nil {
					t.Error(err)
				}
			})
			if err := mixer.startStream(ctx, tc.id); err != nil {
				t.Fatal(err)
			}
			if err := mixer.scheduleTransition(ctx, transitionEvent(tc.id, tc.marker, tc.action, 1)); err != nil {
				t.Fatal(err)
			}
			stream := mixer.assets[tc.id].stream
			fake.positions[stream] = 1.499
			if err := mixer.advanceStreams(frame(), false); err != nil || mixer.streams[tc.id] == nil {
				t.Fatal("narrative transition ran early")
			}
			fake.positions[stream] = 1.5
			if err := mixer.advanceStreams(frame(), true); err != nil || mixer.streams[tc.id] == nil {
				t.Fatal("narrative transition ran in pause")
			}
			if err := mixer.advanceStreams(frame(), false); err != nil {
				t.Fatal(err)
			}
			if tc.action == "hold" {
				if mixer.streams[tc.id] == nil || !mixer.streams[tc.id].held {
					t.Fatal("heart attack did not hold music")
				}
			} else if tc.action == "tail" {
				if mixer.streams[tc.id] == nil || !mixer.streams[tc.id].tailing || fake.plays != 1 {
					t.Fatal("narrative tail not applied")
				}
			} else if mixer.streams[tc.id] != nil || fake.plays != 0 {
				t.Fatal("narrative stop not applied")
			}
		})
	}
}

func TestTransitionInterruptionAndValidation(t *testing.T) {
	ctx := context.Background()
	mixer, fake := makeMixer(t)
	f := frame()
	if err := mixer.startStream(ctx, "MUS-INS-EXP"); err != nil {
		t.Fatal(err)
	}
	if err := mixer.scheduleTransition(ctx, transitionEvent("MUS-INS-EXP", "missing", "stop", 1)); !errors.Is(err, session.ContentInvalid) {
		t.Fatalf("missing marker: %v", err)
	}
	if err := mixer.scheduleTransition(ctx, transitionEvent("MUS-INS-EXP", "transition", "invalid", 1)); !errors.Is(err, session.InvalidAction) {
		t.Fatalf("invalid action: %v", err)
	}
	if err := mixer.scheduleTransition(ctx, transitionEvent("MUS-INS-EXP", "transition", "tail", 1)); err != nil {
		t.Fatal(err)
	}
	if err := mixer.startStream(ctx, "MUS-INS-CBT"); err != nil {
		t.Fatal(err)
	}
	if mixer.streams["MUS-INS-EXP"].transition != nil {
		t.Fatal("interrupted transition remained scheduled")
	}
	fake.positions[mixer.assets["MUS-INS-EXP"].stream] = 1.5
	if err := mixer.advanceStreams(f, false); err != nil || fake.plays != 0 {
		t.Fatal("interrupted transition played its tail")
	}
}

func TestSuspendedStreamSelections(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		state   session.AppState
		focused bool
	}{
		{"pause", session.StatePause, true},
		{"ledger", session.StateLedger, true},
		{"focus", session.StateActive, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
			mixer, fake := makeMixer(t)
			f := frame()
			f.Focused = scenario.focused
			view := frameView(t, scenario.state, event("MUS-INS-EXP", 1), event("MUS-INS-CBT", 2))
			if err := mixer.Update(ctx, view, f); err != nil {
				t.Fatal(err)
			}
			if len(mixer.streams) != 0 || mixer.desired["music"] != "MUS-INS-CBT" || slices.Contains(fake.calls, "play-stream") {
				t.Fatal("suspended selection played early or lost latest choice")
			}
			f.Focused = true
			if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil {
				t.Fatal(err)
			}
			if mixer.streams["MUS-INS-CBT"] == nil || mixer.streams["MUS-INS-EXP"] != nil {
				t.Fatal("resume did not select only the latest track")
			}
		})
	}
}

func TestSuspendedSelectionAndTransition(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   session.AppState
		focused bool
	}{
		{"pause", session.StatePause, true},
		{"focus loss", session.StateActive, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			mixer, fake := makeMixer(t)
			f := frame()
			f.Focused = tc.focused
			id := "MUS-INS-EXP"
			view := frameView(t, tc.state, event(id, 1), transitionEvent(id, "transition", "hold", 2))
			if err := mixer.Update(ctx, view, f); err != nil {
				t.Fatal(err)
			}
			if mixer.streamState[id] == nil || mixer.streamState[id].transition == nil || len(mixer.streams) != 0 || slices.Contains(fake.calls, "play-stream") {
				t.Fatal("selection or validated transition was not deferred")
			}
			f.Focused = true
			if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil {
				t.Fatal(err)
			}
			playback := mixer.streams[id]
			if playback == nil || playback.transition == nil {
				t.Fatal("resumption discarded the transition")
			}
			fake.positions[mixer.assets[id].stream] = 1.5
			if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || !playback.held {
				t.Fatal("resumed transition did not execute")
			}
		})
	}
}

func TestDeferredSelectionCancelsSupersededTransition(t *testing.T) {
	ctx := context.Background()
	mixer, _ := makeMixer(t)
	view := frameView(t, session.StatePause,
		event("MUS-INS-EXP", 1),
		transitionEvent("MUS-INS-EXP", "transition", "hold", 2),
		event("MUS-INS-CBT", 3))
	if err := mixer.Update(ctx, view, frame()); err != nil {
		t.Fatal(err)
	}
	if mixer.desired["music"] != "MUS-INS-CBT" || mixer.streamState["MUS-INS-EXP"].transition != nil {
		t.Fatal("superseded command survived a new selection")
	}
	if err := mixer.Update(ctx, frameView(t, session.StateActive), frame()); err != nil || mixer.streams["MUS-INS-CBT"] == nil || mixer.streams["MUS-INS-EXP"] != nil {
		t.Fatal("resumption opened the wrong stream")
	}
}

func TestResumeFrameUsesLatestSelection(t *testing.T) {
	ctx := context.Background()
	mixer, fake := makeMixer(t)
	f := frame()
	if err := mixer.Update(ctx, frameView(t, session.StatePause, event("MUS-INS-CBT", 1)), f); err != nil {
		t.Fatal(err)
	}
	if err := mixer.Update(ctx, frameView(t, session.StateActive, event("MUS-INS-EXP", 2)), f); err != nil {
		t.Fatal(err)
	}
	if mixer.streams["MUS-INS-EXP"] == nil || mixer.streams["MUS-INS-CBT"] != nil || mixer.desired["music"] != "MUS-INS-EXP" {
		t.Fatal("resumption started a superseded selection")
	}
	plays := 0
	for _, call := range fake.calls {
		if call == "play-stream" {
			plays++
		}
	}
	if plays != 1 {
		t.Fatalf("started %d streams", plays)
	}
}

func limitedSamples(processor *mixProcessor, input []float32) []float32 {
	samples := make([]float32, len(input)+126)
	copy(samples, input)
	processor.process(samples, len(samples)/2)
	return samples[126:]
}

func TestMixedOutputAccessibility(t *testing.T) {
	processor := newMixProcessor(context.Background())
	processor.configure(settings.Audio{Mono: true, ReducedDynamicRange: true})
	samples := limitedSamples(processor, []float32{0.1, 0.3})
	if samples[0] != samples[1] || samples[0] <= 0.2 {
		t.Fatalf("mono or quiet-detail compression: %v", samples)
	}
	processor = newMixProcessor(context.Background())
	samples = limitedSamples(processor, []float32{0.1, 0.2})
	if samples[0] != 0.1 || samples[1] != 0.2 {
		t.Fatalf("quiet stereo changed: %v", samples)
	}
	processor = newMixProcessor(context.Background())
	samples = limitedSamples(processor, []float32{-5, 5})
	if math.Abs(float64(samples[0])) > peakCeiling || math.Abs(float64(samples[1])) > peakCeiling {
		t.Fatalf("limited samples: %v", samples)
	}
}

func reconstructedPeak(samples []float32, channel int) float64 {
	frames := len(samples) / 2
	peak := 0.0
	for center := 0; center < frames; center++ {
		for phase := 0; phase < 4; phase++ {
			var value, sum float64
			for tap := -31; tap <= 32; tap++ {
				x := float64(tap) - float64(phase)/4
				if math.Abs(x) >= 32 {
					continue
				}
				weight := 1.0
				if x != 0 {
					weight = math.Sin(math.Pi*x) / (math.Pi * x)
				}
				weight *= 0.5 + 0.5*math.Cos(math.Pi*x/32)
				sum += weight
				index := center + tap
				if index >= 0 && index < frames {
					value += weight * float64(samples[2*index+channel])
				}
			}
			peak = math.Max(peak, math.Abs(value/sum))
		}
	}
	return peak
}

func TestTruePeakLimitingAcrossCallbacks(t *testing.T) {
	input := make([]float32, 512)
	input[200], input[202] = 0.89, 0.89
	for i := 150; i < 170; i++ {
		if i%2 == 0 {
			input[2*i+1] = 0.89
		} else {
			input[2*i+1] = -0.89
		}
	}
	if reconstructedPeak(input, 0) <= peakCeiling {
		t.Fatal("fixture does not contain an intersample overshoot")
	}
	for _, reduced := range []bool{false, true} {
		processor := newMixProcessor(context.Background())
		processor.configure(settings.Audio{ReducedDynamicRange: reduced})
		whole := limitedSamples(processor, input)
		if peak := reconstructedPeak(whole, 0); peak > peakCeiling || reconstructedPeak(whole, 1) > peakCeiling {
			t.Fatalf("reduced=%v: reconstructed peak %v", reduced, peak)
		}
		processor = newMixProcessor(context.Background())
		processor.configure(settings.Audio{ReducedDynamicRange: reduced})
		chunked := make([]float32, len(input)+126)
		copy(chunked, input)
		for offset := 0; offset < len(chunked); {
			end := min(offset+34, len(chunked))
			processor.process(chunked[offset:end], (end-offset)/2)
			offset = end
		}
		if !slices.Equal(whole, chunked[126:]) {
			t.Fatal("limiting changed at callback boundaries")
		}
	}
}

func enterSilent(t *testing.T, mixer *Mixer, fake *fakeDevice) {
	t.Helper()
	ctx := context.Background()
	fake.available = false
	if err := mixer.Update(ctx, frameView(t, session.StateActive), frame()); !errors.Is(err, session.AudioUnavailable) {
		t.Fatalf("device loss: %v", err)
	}
	if err := mixer.ContinueSilent(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSilentCompletedTailDoesNotReplay(t *testing.T) {
	ctx := context.Background()
	mixer, fake := makeMixer(t)
	f := frame()
	f.Elapsed = 0
	id := "MUS-INS-EXP"
	if err := mixer.Update(ctx, frameView(t, session.StateActive, event(id, 1)), f); err != nil {
		t.Fatal(err)
	}
	enterSilent(t, mixer, fake)
	command := transitionEvent(id, "transition", "tail", 2)
	if err := mixer.Update(ctx, frameView(t, session.StateActive, command), f); err != nil {
		t.Fatal(err)
	}
	f.Elapsed = 2 * time.Second
	if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || mixer.desired["music"] != "" || fake.plays != 0 {
		t.Fatal("silent tail did not complete without playback")
	}
	fake.available = true
	fake.calls = nil
	if err := mixer.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	f.Elapsed = 0
	if err := mixer.Update(ctx, frameView(t, session.StateActive, command), f); err != nil || len(mixer.streams) != 0 || fake.plays != 0 || slices.Contains(fake.calls, "play-stream") {
		t.Fatal("retry replayed a completed silent tail")
	}
}

func TestSilentHoldSurvivesRetry(t *testing.T) {
	ctx := context.Background()
	mixer, fake := makeMixer(t)
	f := frame()
	f.Elapsed = 0
	id := "MUS-INS-EXP"
	if err := mixer.Update(ctx, frameView(t, session.StateActive, event(id, 1)), f); err != nil {
		t.Fatal(err)
	}
	enterSilent(t, mixer, fake)
	f.Elapsed = 2 * time.Second
	if err := mixer.Update(ctx, frameView(t, session.StatePause, transitionEvent(id, "transition", "hold", 2)), f); err != nil || mixer.streamState[id].held || mixer.streamState[id].transition == nil {
		t.Fatal("silent transition advanced in pause")
	}
	if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || !mixer.streamState[id].held {
		t.Fatal("silent hold was not applied")
	}
	fake.available = true
	fake.calls = nil
	if err := mixer.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	f.Elapsed = 0
	if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || len(mixer.streams) != 0 || slices.Contains(fake.calls, "play-stream") {
		t.Fatal("retry restarted held music")
	}
	if err := mixer.Update(ctx, frameView(t, session.StateActive, event(id, 3)), f); err != nil {
		t.Fatal(err)
	}
	if mixer.streams[id] == nil || mixer.streams[id].held || fake.positions[mixer.assets[id].stream] != 1.5 {
		t.Fatal("explicit reselection did not resume held position")
	}
}

func TestPendingTransitionSurvivesSilentRetry(t *testing.T) {
	ctx := context.Background()
	mixer, fake := makeMixer(t)
	f := frame()
	f.Elapsed = 0
	id := "MUS-INS-EXP"
	if err := mixer.Update(ctx, frameView(t, session.StateActive, event(id, 1)), f); err != nil {
		t.Fatal(err)
	}
	fake.positions[mixer.assets[id].stream] = 1
	command := transitionEvent(id, "transition", "tail", 2)
	if err := mixer.Update(ctx, frameView(t, session.StateActive, command), f); err != nil {
		t.Fatal(err)
	}
	enterSilent(t, mixer, fake)
	f.Elapsed = 250 * time.Millisecond
	if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil {
		t.Fatal(err)
	}
	fake.available = true
	if err := mixer.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	f.Elapsed = 0
	if err := mixer.Update(ctx, frameView(t, session.StateActive, command), f); err != nil {
		t.Fatal(err)
	}
	playback := mixer.streams[id]
	if playback == nil || playback.transition == nil || fake.positions[mixer.assets[id].stream] != 1.25 || fake.plays != 0 {
		t.Fatal("retry lost pending marker or position")
	}
	fake.positions[mixer.assets[id].stream] = 1.5
	if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || mixer.streams[id] == nil || !mixer.streams[id].tailing || fake.plays != 1 {
		t.Fatal("pending tail did not execute exactly once")
	}
	if err := mixer.Update(ctx, frameView(t, session.StateActive, command), f); err != nil || fake.plays != 1 {
		t.Fatal("tail replayed after acknowledgment")
	}
}

func TestTransitionQueuedDuringSilentPlaySurvivesRetry(t *testing.T) {
	ctx := context.Background()
	mixer, fake := makeMixer(t)
	f := frame()
	f.Elapsed = 0
	id := "MUS-INS-EXP"
	if err := mixer.Update(ctx, frameView(t, session.StateActive, event(id, 1)), f); err != nil {
		t.Fatal(err)
	}
	enterSilent(t, mixer, fake)
	command := transitionEvent(id, "transition", "tail", 2)
	if err := mixer.Update(ctx, frameView(t, session.StateActive, command), f); err != nil || mixer.streamState[id].transition == nil {
		t.Fatal("silent command was not retained")
	}
	fake.available = true
	if err := mixer.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mixer.Update(ctx, frameView(t, session.StateActive, command), f); err != nil || mixer.streams[id] == nil || mixer.streams[id].transition == nil {
		t.Fatal("retry discarded the silent command")
	}
	fake.positions[mixer.assets[id].stream] = 1.5
	if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || fake.plays != 1 {
		t.Fatal("retained silent command did not execute once")
	}
}

func TestAlreadyHeldOrStoppedBeforeRetry(t *testing.T) {
	for _, action := range []string{"hold", "stop"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			mixer, fake := makeMixer(t)
			f := frame()
			f.Elapsed = 0
			id := "MUS-INS-EXP"
			if err := mixer.Update(ctx, frameView(t, session.StateActive, event(id, 1)), f); err != nil {
				t.Fatal(err)
			}
			fake.positions[mixer.assets[id].stream] = 1.5
			if err := mixer.Update(ctx, frameView(t, session.StateActive, transitionEvent(id, "transition", action, 2)), f); err != nil {
				t.Fatal(err)
			}
			fake.available = false
			if err := mixer.Update(ctx, frameView(t, session.StateActive), f); !errors.Is(err, session.AudioUnavailable) {
				t.Fatalf("device failure: %v", err)
			}
			fake.available = true
			fake.calls = nil
			if err := mixer.Retry(ctx); err != nil {
				t.Fatal(err)
			}
			if err := mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || len(mixer.streams) != 0 || slices.Contains(fake.calls, "play-stream") {
				t.Fatal("retry restarted held or stopped music")
			}
			if action == "hold" && (mixer.streamState[id] == nil || !mixer.streamState[id].held) {
				t.Fatal("held state was lost")
			}
			if action == "stop" && mixer.desired["music"] != "" {
				t.Fatal("stopped stream remained selected")
			}
		})
	}
}

func TestSilentSelectionAndStop(t *testing.T) {
	ctx := context.Background()
	files, records, fake := fixture(t)
	fake.available = false
	mixer, err := newMixer(ctx, files, records, func() uint64 { return 1 }, "location-ins", fake)
	if mixer == nil || !errors.Is(err, session.AudioUnavailable) {
		t.Fatalf("initial device loss: %v", err)
	}
	if err = mixer.ContinueSilent(ctx); err != nil {
		t.Fatal(err)
	}
	f := frame()
	f.Elapsed = 2 * time.Second
	id := "MUS-INS-EXP"
	if err = mixer.Update(ctx, frameView(t, session.StateActive, event(id, 1), transitionEvent(id, "transition", "stop", 2)), f); err != nil || mixer.desired["music"] != "" {
		t.Fatalf("silent selection/stop: %v", err)
	}
	fake.available = true
	fake.calls = nil
	if err = mixer.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if err = mixer.Update(ctx, frameView(t, session.StateActive), f); err != nil || len(mixer.streams) != 0 || slices.Contains(fake.calls, "play-stream") {
		t.Fatal("stopped music restarted")
	}
	if err = mixer.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSilentTransitionAcrossLoopBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		id         string
		position   float64
		marker     int64
		elapsed    time.Duration
		want       float64
		held       bool
		wantQueued bool
	}{
		{"before marker", "MUS-INS-EXP", 1.1, 72000, 200 * time.Millisecond, 1.3, false, true},
		{"exact marker", "MUS-INS-EXP", 1.25, 72000, 250 * time.Millisecond, 1.5, true, false},
		{"passed marker waits for next loop", "MUS-INS-EXP", 1.75, 72000, 500 * time.Millisecond, 1.25, false, true},
		{"passed marker on next loop", "MUS-INS-EXP", 1.75, 72000, 750 * time.Millisecond, 1.5, true, false},
		{"several loops", "MUS-INS-EXP", 1.75, 72000, 4 * time.Second, 1.5, true, false},
		{"marker at loop end", "MUS-INS-EXP", 1.75, 96000, 250 * time.Millisecond, 2, true, false},
		{"marker at loop start", "MUS-INS-EXP", 1.75, 48000, 250 * time.Millisecond, 1, true, false},
		{"outro is not reachable", "MUS-INS-EXP", 1.75, 120000, 4 * time.Second, 1.75, false, true},
		{"one shot already passed", "MUS-PRO", 1.75, 72000, 0, 1.5, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mixer, _ := makeMixer(t)
			mixer.desired["music"] = tc.id
			state := &streamState{position: tc.position, transition: &streamTransition{marker: tc.marker, action: "hold"}}
			mixer.streamState[tc.id] = state
			f := frame()
			f.Elapsed = tc.elapsed
			mixer.advanceSilentStreams(f, false)
			if math.Abs(state.position-tc.want) > 1e-9 || state.held != tc.held || (state.transition != nil) != tc.wantQueued {
				t.Fatalf("position=%v held=%v queued=%v; want %v %v %v", state.position, state.held, state.transition != nil, tc.want, tc.held, tc.wantQueued)
			}
		})
	}
}

func TestCallbackCompletionSurvivesDeviceLossBeforePoll(t *testing.T) {
	for _, result := range []string{"hold", "stop", "tail", "tail-done"} {
		t.Run(result, func(t *testing.T) {
			mixer, fake := makeMixer(t)
			ctx := context.Background()
			id := "MUS-INS-EXP"
			if err := mixer.Update(ctx, frameView(t, session.StateActive, event(id, 1)), frame()); err != nil {
				t.Fatal(err)
			}
			item := mixer.assets[id].stream
			item.result = result
			fake.positions[item] = 1.5
			fake.available = false
			if err := mixer.Update(ctx, frameView(t, session.StateActive), frame()); !errors.Is(err, session.AudioUnavailable) {
				t.Fatalf("device loss: %v", err)
			}
			fake.available = true
			if err := mixer.Retry(ctx); err != nil {
				t.Fatal(err)
			}
			if err := mixer.Update(ctx, frameView(t, session.StateActive), frame()); err != nil || len(mixer.streams) != 0 {
				t.Fatalf("completed stream restarted: %v", err)
			}
			if result == "hold" && (mixer.streamState[id] == nil || !mixer.streamState[id].held) {
				t.Fatal("held selection was lost")
			}
		})
	}
}

func TestStreamFailureAndOwnerThread(t *testing.T) {
	ctx := context.Background()
	files, records, fake := fixture(t)
	thread := uint64(1)
	mixer, err := newMixer(ctx, files, records, func() uint64 { return thread }, "location-ins", fake)
	if err != nil {
		t.Fatal(err)
	}
	if err = mixer.startStream(ctx, "MUS-INS-EXP"); err != nil {
		t.Fatal(err)
	}
	fake.failStream = true
	if err = mixer.Update(ctx, frameView(t, session.StateActive), frame()); !errors.Is(err, session.AudioUnavailable) {
		t.Fatalf("stream failure: %v", err)
	}
	thread = 2
	before := len(fake.calls)
	if err = mixer.Close(ctx); err == nil || len(fake.calls) != before {
		t.Fatalf("wrong-thread cleanup: %v", err)
	}
	thread = 1
	fake.failStream = false
	if err = mixer.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if err = mixer.Update(ctx, frameView(t, session.StatePause), frame()); err != nil || len(mixer.streams) != 0 {
		t.Fatal("retry resumed music before gameplay resumed")
	}
	if err = mixer.Update(ctx, frameView(t, session.StateActive), frame()); err != nil || mixer.streams["MUS-INS-EXP"] == nil {
		t.Fatal("retry did not restore the selected stream")
	}
	if err = mixer.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceRecoveryAndCleanup(t *testing.T) {
	ctx := context.Background()
	files, records, fake := fixture(t)
	fake.available = false
	mixer, err := newMixer(ctx, files, records, func() uint64 { return 1 }, "shared", fake)
	if mixer == nil || !errors.Is(err, session.AudioUnavailable) {
		t.Fatalf("device initialization: %v", err)
	}
	if err = mixer.Update(ctx, frameView(t, session.StateActive), frame()); !errors.Is(err, session.AudioUnavailable) {
		t.Fatalf("missing failure: %v", err)
	}
	if err = mixer.ContinueSilent(ctx); err != nil {
		t.Fatal(err)
	}
	cue := event("SFX-BODY-IMPACT", 1)
	if err = mixer.Update(ctx, frameView(t, session.StateActive, cue), frame()); err != nil {
		t.Fatal(err)
	}
	fake.available = true
	if err = mixer.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if err = mixer.Update(ctx, frameView(t, session.StateActive, cue), frame()); err != nil || fake.plays != 0 {
		t.Fatal("retry replayed silent event")
	}
	fake.available = false
	if err = mixer.Update(ctx, frameView(t, session.StateActive), frame()); !errors.Is(err, session.AudioUnavailable) {
		t.Fatal("device loss not detected")
	}
	fake.available = true
	fake.calls = nil
	if err = mixer.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	closeAt, openAt := slices.Index(fake.calls, "close"), slices.Index(fake.calls, "open")
	if closeAt < 0 || openAt <= closeAt || slices.Index(fake.calls, "unload-sound") >= closeAt {
		t.Fatalf("retry order: %v", fake.calls)
	}
	if err = mixer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	before := len(fake.calls)
	if err = mixer.Close(ctx); err != nil || len(fake.calls) != before {
		t.Fatal("repeated cleanup")
	}
}

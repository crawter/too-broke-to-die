// Copyright (C) 2026 crawter
// All rights reserved.

package audio

import (
	"context"
	"math"
	"sync/atomic"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"too-broke-to-die/internal/tbtd/settings"
)

const peakCeiling = 0.8912509381337456 // -1 dB in linear amplitude

const (
	monoFlag uint32 = 1 << iota
	reducedRangeFlag
)

// mixProcessor is owned by the mixer. Only atomic flags cross into raylib's
// audio callback; the callback neither calls raylib nor mutates game state.
type mixProcessor struct {
	flags    atomic.Uint32
	callback rl.AudioCallback
	bound    float64
	history  [127][2]float64
	next     int
	gain     float64
}

func newMixProcessor(ctx context.Context) *mixProcessor {
	defer mon.Task()(&ctx)(nil)
	processor := &mixProcessor{bound: peakKernelBound(ctx), gain: 1}
	processor.callback = processor.process
	return processor
}

// peakKernelBound is the largest L1 norm of a four-phase, 64-tap Hann-windowed
// sinc reconstruction filter. Normalizing each phase preserves DC. The L1
// bound lets the limiter constrain intersample peaks even as gain changes.
func peakKernelBound(ctx context.Context) float64 {
	defer mon.Task()(&ctx)(nil)
	bound := 1.0
	for phase := 0; phase < 4; phase++ {
		fraction := float64(phase) / 4
		var sum, absolute float64
		for tap := -31; tap <= 32; tap++ {
			x := float64(tap) - fraction
			if math.Abs(x) >= 32 {
				continue
			}
			value := 1.0
			if x != 0 {
				value = math.Sin(math.Pi*x) / (math.Pi * x)
			}
			value *= 0.5 + 0.5*math.Cos(math.Pi*x/32)
			sum += value
			absolute += math.Abs(value)
		}
		bound = math.Max(bound, absolute/math.Abs(sum))
	}
	return bound
}

func (processor *mixProcessor) configure(levels settings.Audio) {
	var flags uint32
	if levels.Mono {
		flags |= monoFlag
	}
	if levels.ReducedDynamicRange {
		flags |= reducedRangeFlag
	}
	processor.flags.Store(flags)
}

// process is raylib's fixed callback signature; it handles interleaved stereo.
// Sixty-three frames of lookahead and history cover every 64-tap interpolator
// window containing an output sample. The linked gain bounds the absolute
// weighted sum, not just the sample peaks. Release is gradual; actual mixes
// and device reconstruction still require manual measurement and listening.
func (processor *mixProcessor) process(samples []float32, frames int) {
	flags := processor.flags.Load()
	count := min(frames, len(samples)/2)
	for i := 0; i < count; i++ {
		left, right := float64(samples[2*i]), float64(samples[2*i+1])
		if math.IsNaN(left) || math.IsInf(left, 0) {
			left = 0
		}
		if math.IsNaN(right) || math.IsInf(right, 0) {
			right = 0
		}
		if flags&monoFlag != 0 {
			left = (left + right) / 2
			right = left
		}
		if flags&reducedRangeFlag != 0 {
			left = 1.5 * left / (1 + 0.5*math.Abs(left))
			right = 1.5 * right / (1 + 0.5*math.Abs(right))
		}
		processor.history[processor.next] = [2]float64{left, right}
		maximum := 0.0
		for index := range processor.history {
			maximum = math.Max(maximum, math.Abs(processor.history[index][0]))
			maximum = math.Max(maximum, math.Abs(processor.history[index][1]))
		}
		limit := peakCeiling * (1 - 1e-6) / processor.bound
		target := 1.0
		if maximum > limit {
			target = limit / maximum
		}
		if target < processor.gain {
			processor.gain = target
		} else {
			processor.gain = math.Min(target, processor.gain+0.00005)
		}
		out := processor.history[(processor.next+len(processor.history)-63)%len(processor.history)]
		samples[2*i] = float32(out[0] * processor.gain)
		samples[2*i+1] = float32(out[1] * processor.gain)
		processor.next = (processor.next + 1) % len(processor.history)
	}
}

func validLevels(levels settings.Audio) bool {
	for _, value := range []int{levels.Master, levels.Music, levels.Effects, levels.Ambience, levels.Vocal} {
		if value < 0 || value > 100 {
			return false
		}
	}
	return true
}

func voiceVolume(levels settings.Audio, vocal bool) float32 {
	value := levels.Effects
	if vocal {
		value = levels.Vocal
	}
	return float32(peakCeiling * float64(levels.Master) * float64(value) / 10000)
}

func ramp(current, target float64, elapsed, duration time.Duration) float64 {
	if elapsed <= 0 {
		return current
	}
	step := float64(elapsed) / float64(duration)
	if target < current {
		return math.Max(target, current-step)
	}
	return math.Min(target, current+step)
}

// updateMix applies the independent controls and duck envelope. The ceiling
// here limits individual sources; it is not a measured true-peak master limiter.
func (mixer *Mixer) updateMix(frame Frame) {
	warning := false
	for _, voice := range mixer.voices {
		if voice.sound != nil && !voice.paused && voice.priority == 6 {
			warning = true
		}
	}
	musicTarget, ambienceTarget := 1.0, 1.0
	if warning || frame.Dialogue {
		musicTarget = math.Pow(10, -6.0/20)
	}
	if warning {
		ambienceTarget = math.Pow(10, -8.0/20)
	}
	musicDuration, ambienceDuration := 300*time.Millisecond, 300*time.Millisecond
	if musicTarget < mixer.musicDuck {
		musicDuration = 50 * time.Millisecond
	}
	if ambienceTarget < mixer.ambientDuck {
		ambienceDuration = 50 * time.Millisecond
	}
	// Normalize the step to the full authored attenuation so attack and release
	// both take their specified duration, including reversals partway through.
	mixer.musicDuck = duckRamp(mixer.musicDuck, musicTarget, frame.Elapsed, musicDuration, 1-math.Pow(10, -6.0/20))
	mixer.ambientDuck = duckRamp(mixer.ambientDuck, ambienceTarget, frame.Elapsed, ambienceDuration, 1-math.Pow(10, -8.0/20))
	for _, voice := range mixer.voices {
		if voice.sound != nil {
			volume := voiceVolume(frame.Settings, voice.vocal)
			if voice.music {
				volume = float32(peakCeiling * float64(frame.Settings.Master) * float64(frame.Settings.Music) / 10000 * mixer.musicDuck)
			}
			mixer.native.soundVolume(voice.sound, volume)
		}
	}
	for id, playback := range mixer.streams {
		item := mixer.assets[id]
		level, duck := frame.Settings.Music, mixer.musicDuck
		if item.record.Kind == "ambience" {
			level, duck = frame.Settings.Ambience, mixer.ambientDuck
		}
		volume := peakCeiling * float64(frame.Settings.Master) * float64(level) / 10000 * duck * playback.gain
		mixer.native.streamVolume(item.stream, float32(volume))
	}
}

func duckRamp(current, target float64, elapsed, duration time.Duration, distance float64) float64 {
	if elapsed <= 0 {
		return current
	}
	step := distance * float64(elapsed) / float64(duration)
	if target < current {
		return math.Max(target, current-step)
	}
	return math.Min(target, current+step)
}

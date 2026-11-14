// Copyright (C) 2026 crawter
// All rights reserved.

package audio

import (
	"context"
	"fmt"
	"strings"
	"time"

	"too-broke-to-die/internal/tbtd/session"
)

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

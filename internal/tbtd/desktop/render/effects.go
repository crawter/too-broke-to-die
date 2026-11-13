// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"math"
	"slices"
	"time"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

const (
	maxParticles = 512
	maxStains    = 48
)

// effectPool holds only decorative effects, never hazards or evidence.
type effectPool struct {
	particles          int
	stains             []session.EffectEvent
	active             []activeEffect
	seen               map[effectKey]uint64
	lastTick           uint64
	skipNextMonolithic bool
	shake              *activeEffect
}

type effectKey struct {
	tick    uint64
	ordinal uint32
	entity  string
	kind    string
}

type activeEffect struct {
	event          session.EffectEvent
	expires        uint64
	particles      int
	reducedVisible bool
}

func (pool *effectPool) addParticles(count int) int {
	if count <= 0 {
		return 0
	}
	added := min(count, maxParticles-pool.particles)
	pool.particles += added
	return added
}

func (pool *effectPool) addStain(event session.EffectEvent) {
	if len(pool.stains) >= maxStains {
		pool.stains = pool.stains[len(pool.stains)-maxStains+1:]
	}
	// The stain does not need the independently owned particle slice.
	event.Particles = nil
	pool.stains = append(pool.stains, event)
}

// shakeLimit returns the largest permitted world-only displacement and life.
func shakeLimit(heavy bool, access settings.Accessibility) (float64, time.Duration) {
	if access.Shake == settings.ShakeOff || access.ReducedMotion {
		return 0, 0
	}
	distance, duration := 2.0, 80*time.Millisecond
	if heavy {
		distance, duration = 4, 120*time.Millisecond
	}
	if access.Shake == settings.ShakeLow {
		distance /= 2
	}
	return distance, duration
}

func sprayLifetime(removal bool) time.Duration {
	if removal {
		return 600 * time.Millisecond
	}
	return 400 * time.Millisecond
}

// update consumes authoritative events once, using simulation ticks so pause
// cannot advance an effect or replay a removal. Only decorative particles are
// capped; gameplay markers and actual limbs are never placed in this pool.
func (pool *effectPool) update(ctx context.Context, tick uint64, events []session.EffectEvent, _ settings.Accessibility) (err error) {
	defer mon.Task()(&ctx)(&err)
	if tick > math.MaxInt64-72 {
		return content.ErrInvalid.New("effect clock overflow")
	}
	for _, event := range events {
		expected := content.PresentationID("FX-BODY")
		if event.Kind == "feces" {
			expected = "FX-FEC"
		}
		if len(event.Particles) >= maxParticles || event.Tick > tick || event.Entity == "" || event.Asset != expected || !validPoint(event.Position) || event.Direction < -1 || event.Direction > 1 || (event.Kind != "hit" && event.Kind != "removal" && event.Kind != "feces") {
			return content.ErrInvalid.New("%s: invalid visual effect event for %s", event.Asset, event.Entity)
		}
	}
	if tick < pool.lastTick {
		pool.reset(ctx)
	}
	pool.lastTick = tick
	pool.active = slices.DeleteFunc(pool.active, func(effect activeEffect) bool {
		if tick < effect.expires {
			return false
		}
		pool.particles -= effect.particles
		return true
	})
	for key, expires := range pool.seen {
		if tick >= expires {
			delete(pool.seen, key)
		}
	}
	if pool.shake != nil && tick >= pool.shake.expires {
		pool.shake = nil
	}
	if pool.seen == nil {
		pool.seen = make(map[effectKey]uint64)
	}
	for _, event := range events {
		key := effectKey{tick: event.Tick, ordinal: event.Ordinal, entity: event.Entity, kind: event.Kind}
		if _, exists := pool.seen[key]; exists {
			continue
		}
		if len(pool.seen) >= maxParticles*4 {
			continue
		}
		lifetime := uint64(48)
		if event.Kind == "removal" {
			lifetime = 72
		}
		if tick >= event.Tick+lifetime {
			continue
		}
		pool.seen[key] = event.Tick + lifetime
		// Reserve the actual number of supplied authored regions, not a
		// notional count unrelated to the sprites that will be drawn.
		count := pool.addParticles(1 + len(event.Particles))
		event.Particles = slices.Clone(event.Particles)
		effect := activeEffect{event: event, expires: event.Tick + lifetime, particles: count}
		if count != 0 {
			if event.Kind != "feces" && len(event.Particles) == 0 {
				// Choose once per admitted emission, not once per frame or
				// from tick parity. Duplicates and expired events do not
				// advance the cadence.
				effect.reducedVisible = !pool.skipNextMonolithic
				pool.skipNextMonolithic = !pool.skipNextMonolithic
			}
			pool.active = append(pool.active, effect)
		}
		if event.Kind == "hit" || event.Kind == "removal" {
			pool.addStain(event)
		}
		if event.Entity == "player" && (event.Kind == "hit" || event.Kind == "removal") {
			_, duration := shakeLimit(event.Heavy, settings.Accessibility{Shake: settings.ShakeFull})
			shakeTicks := uint64((duration.Nanoseconds()*120 + int64(time.Second) - 1) / int64(time.Second))
			if tick < event.Tick+shakeTicks {
				effect.expires = event.Tick + shakeTicks
				pool.shake = &effect
			}
		}
	}
	return nil
}

// effectItem is a region delivered by the producer. Visibility is decided
// separately from validation so reduced effects cannot hide a broken handoff.
type effectItem struct {
	part    session.VisualPart
	point   session.Point
	facing  int
	stain   bool
	visible bool
	alpha   uint8
}

func (pool *effectPool) plan(ctx context.Context, access settings.Accessibility) (_ []effectItem, err error) {
	defer mon.Task()(&ctx)(&err)
	var items []effectItem
	for i, event := range pool.stains {
		if event.Stain.Asset == "" {
			return nil, content.ErrInvalid.New("%s: missing authored stain frame", event.Asset)
		}
		visible := access.Blood != settings.ParticlesReduced || i >= len(pool.stains)-maxStains/2
		// The oldest retained marks fade first as the room fills.
		alpha := uint8(64 + 191*(i+1)/len(pool.stains))
		items = append(items, effectItem{part: event.Stain, point: event.Position, facing: event.Direction, stain: true, visible: visible, alpha: alpha})
	}
	for _, effect := range pool.active {
		if effect.event.Part.Asset != effect.event.Asset {
			return nil, content.ErrInvalid.New("%s: missing effect frame handoff", effect.event.Asset)
		}
		bloodReduced := access.Blood == settings.ParticlesReduced && effect.event.Kind != "feces"
		baseVisible := effect.particles > 0
		if bloodReduced && len(effect.event.Particles) == 0 {
			// A monolithic authored spray cannot be subdivided without an
			// invented atlas mapping. Reduce its emission rate instead.
			baseVisible = baseVisible && effect.reducedVisible
		}
		items = append(items, effectItem{part: effect.event.Part, point: effect.event.Position, facing: effect.event.Direction, visible: baseVisible, alpha: 255})
		for i, part := range effect.event.Particles {
			if part.Asset == "" {
				return nil, content.ErrInvalid.New("%s: missing particle frame %d", effect.event.Asset, i)
			}
			visible := i+1 < effect.particles && (!bloodReduced || i%2 == 0)
			items = append(items, effectItem{part: part, point: effect.event.Position, facing: effect.event.Direction, visible: visible, alpha: 255})
		}
	}
	return items, nil
}

// shakeOffset is applied to world drawing only, never to the reticle viewport.
func (pool *effectPool) shakeOffset(tick uint64, access settings.Accessibility) float64 {
	if pool.shake == nil || tick >= pool.shake.expires || tick < pool.shake.event.Tick {
		return 0
	}
	magnitude, _ := shakeLimit(pool.shake.event.Heavy, access)
	if (tick-pool.shake.event.Tick)%2 != 0 {
		return -magnitude
	}
	return magnitude
}

// reset clears decorations on a room transition. Essential state is separate.
func (pool *effectPool) reset(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	*pool = effectPool{}
}

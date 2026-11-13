// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"fmt"
	"time"

	"too-broke-to-die/internal/tbtd/simulation"
)

const (
	// TickRate is the authoritative gameplay and scene frequency.
	TickRate = 120

	// MaxFrameTicks bounds work performed in a single rendered frame.
	MaxFrameTicks = 8

	// MaxFrameInterval is the largest interval accepted without pausing.
	MaxFrameInterval = 250 * time.Millisecond

	// BacklogDropped is the PDD's named overload diagnostic.
	BacklogDropped = "simulation_backlog_dropped"

	clockUnit = int64(time.Second)
)

// DurationTicks rounds authored durations up to whole simulation ticks.
func DurationTicks(duration time.Duration) (int64, error) {
	return simulation.DurationTicks(duration)
}

// ClockDiagnostic identifies discarded wall time and the state where it was
// dropped. ScaledNanoseconds preserves time multiplied by TickRate exactly;
// Dropped rounds that duration down to whole nanoseconds.
type ClockDiagnostic struct {
	Name string

	State AppState

	Dropped time.Duration

	ScaledNanoseconds int64
}

// FixedClock accumulates monotonic time in 120 Hz units without rounding a
// tick duration. Its single owner must consume or interrupt each frame budget.
type FixedClock struct {
	accumulated int64

	remaining int

	open bool

	diagnostic ClockDiagnostic
}

// Begin adds elapsed monotonic time and rejects oversized intervals.
func (clock *FixedClock) Begin(ctx context.Context, elapsed time.Duration, state AppState) (stalled bool, err error) {
	defer mon.Task()(&ctx)(&err)
	if clock.open || elapsed < 0 {
		return false, fmt.Errorf("%w: invalid clock frame", InvalidAction)
	}
	clock.diagnostic = ClockDiagnostic{}
	if elapsed > MaxFrameInterval {
		clock.Clear()
		return true, nil
	}
	clock.accumulated += int64(elapsed) * TickRate
	clock.remaining = min(int(clock.accumulated/clockUnit), MaxFrameTicks)
	clock.open = true
	if clock.remaining == 0 {
		clock.open = false
	}
	return false, nil
}

// Next consumes at most one tick and drops excess whole ticks after the frame
// budget, retaining the sub-tick remainder for interpolation.
func (clock *FixedClock) Next(state AppState) bool {
	if !clock.open || clock.remaining == 0 {
		return false
	}
	clock.accumulated -= clockUnit
	clock.remaining--
	if clock.remaining == 0 {
		clock.open = false
		dropped := clock.accumulated - clock.accumulated%clockUnit
		if dropped > 0 {
			clock.accumulated -= dropped
			clock.diagnostic = ClockDiagnostic{Name: BacklogDropped, State: state, Dropped: time.Duration(dropped / TickRate), ScaledNanoseconds: dropped}
		}
	}
	return true
}

// Interrupt discards the rest of a frame and its accumulated time.
func (clock *FixedClock) Interrupt() { clock.Clear() }

// Clear resets the frame budget and interpolation remainder.
func (clock *FixedClock) Clear() {
	clock.accumulated = 0
	clock.remaining = 0
	clock.open = false
}

// Diagnostic returns the most recent frame's dropped-time diagnostic.
func (clock *FixedClock) Diagnostic() ClockDiagnostic { return clock.diagnostic }

// Fraction returns a fractional tick for visual interpolation only.
func (clock *FixedClock) Fraction() float64 {
	remainder := clock.accumulated % clockUnit
	return float64(remainder) / float64(clockUnit)
}

// Freeze discards pending ticks but preserves the fractional scene tick.
func (clock *FixedClock) Freeze() {
	clock.accumulated %= clockUnit
	clock.remaining = 0
	clock.open = false
}

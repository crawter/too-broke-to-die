// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"fmt"
	"time"
)

// DurationTicks rounds a nonnegative authored duration up to whole 120 Hz ticks.
func DurationTicks(duration time.Duration) (int64, error) {
	if duration < 0 {
		return 0, fmt.Errorf("negative duration")
	}
	seconds := int64(duration / time.Second)
	nanos := int64(duration % time.Second)
	return seconds*120 + (nanos*120+int64(time.Second)-1)/int64(time.Second), nil
}

const (
	bufferTicks        = 15
	forgivenessTicks   = 12
	dropTicks          = 22
	evadeTicks         = 22
	evadeImmunityTicks = 15
	evadeCooldownTicks = 78
)

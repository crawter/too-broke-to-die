// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"errors"
	"testing"
)

func TestRestoreRejectsMultipleDeathsInOneOperation(t *testing.T) {
	ctx := context.Background()
	campaign, _ := fixture(t)
	started, err := campaign.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	first := next(started)
	first.Death = DeathGameplay
	first.DeathDetail = "first fall"
	snapshot := prepare(t, campaign, started, first)
	second := next(snapshot)
	second.Death = DeathGameplay
	second.DeathDetail = "second fall"
	snapshot = prepare(t, campaign, snapshot, second)
	state := snapshot.Data(ctx)
	if _, err := campaign.Restore(ctx, state); err != nil || debt(t, snapshot) != 9998 {
		t.Fatalf("distinct operations should restore: %v", err)
	}
	state.Incidents[1].ID.Sequence = state.Incidents[0].ID.Sequence
	state.Incidents[1].ID.Ordinal++
	if _, err := campaign.Restore(ctx, state); !errors.Is(err, ErrInvalid) {
		t.Fatalf("accepted two death incidents in one operation: %v", err)
	}
}

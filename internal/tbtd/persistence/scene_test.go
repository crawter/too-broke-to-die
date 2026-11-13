// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"context"
	"reflect"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
)

func TestSceneEntryRevisionRoundTrip(t *testing.T) {
	ctx := context.Background()
	codec, owner, _ := fixture(t)
	entry := campaign.Transition{Story: &campaign.StoryChange{Action: campaign.StorySceneEnter, Narrative: "NAR-01", Unit: "UNIT-ONE-1"}}
	snapshot := prepare(t, owner, campaign.Snapshot{}, entry)
	for _, unit := range []string{"UNIT-ONE-1", "UNIT-ONE-2"} {
		if unit != "UNIT-ONE-1" {
			snapshot = prepare(t, owner, snapshot, campaign.Transition{Story: &campaign.StoryChange{Action: campaign.StorySceneUnit, Narrative: "NAR-01", Unit: unit}})
		}
		encoded, written := encode(t, codec, revision(codec, snapshot))
		decoded, err := codec.Decode(ctx, encoded)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := owner.RestorePlan(ctx, decoded.Snapshot)
		if err != nil || plan.SceneEntry != "NAR-01" || decoded.Snapshot.Data(ctx).SceneUnit != unit || !reflect.DeepEqual(snapshot.Data(ctx), written.Snapshot.Data(ctx)) {
			t.Fatalf("unit %s failed round trip: %+v, %v", unit, plan, err)
		}
	}
}

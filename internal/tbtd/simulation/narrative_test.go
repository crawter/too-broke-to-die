// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
)

func TestPermitLobbyRestoresProtectedReviewCounter(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	snapshot, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for institution := 0; institution < 4; institution++ {
		for family := 0; family < 4; family++ {
			for _, id := range manifest.Batches(ctx)[institution*4+family].Deeds {
				snapshot = certifyFixture(t, owner, manifest, snapshot, id)
			}
		}
		closure := manifest.Deeds(ctx)[institution*25+24]
		snapshot, err = owner.DefeatBoss(ctx, snapshot, closure.Deed.Encounter)
		if err != nil {
			t.Fatal(err)
		}
		snapshot = certifyFixture(t, owner, manifest, snapshot, closure.Deed.ID)
	}
	snapshot, err = owner.Checkpoint(ctx, snapshot, "ANC-MIN-PERMIT")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, snapshot, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, state := world.Snapshot(ctx).Data(ctx)
	found := false
	for _, entity := range state.Entities {
		found = found || (string(entity.ID) == PermitReviewTarget && entity.Role == content.RoleTerminal && entity.Protected)
	}
	interaction := world.InteractionView(ctx)
	if !found || !interaction.Found || interaction.Target.ID != PermitReviewTarget || snapshot.Data(ctx).Permit {
		t.Fatalf("counter missing or permit prematurely granted: %+v, %+v", state, interaction)
	}
}

// Copyright (C) 2026 crawter
// All rights reserved.

package campaign

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"too-broke-to-die/internal/tbtd/content"
)

func TestSceneEntryAndUnitAreDurableAndValidated(t *testing.T) {
	ctx := context.Background()
	owner, _ := fixture(t)
	var initial Snapshot
	early := next(initial)
	early.Story = &StoryChange{Action: StorySceneEnter, Narrative: "NAR-04", Unit: "UNIT-FOUR-1"}
	if _, err := owner.Prepare(ctx, initial, early); !errors.Is(err, ErrBlocked) {
		t.Fatalf("arrival before prerequisites: %v", err)
	}
	entry := next(initial)
	entry.Story = &StoryChange{Action: StorySceneEnter, Narrative: "NAR-01", Unit: "UNIT-ONE-1"}
	entered := prepare(t, owner, initial, entry)
	if _, err := owner.Checkpoint(ctx, initial, "ANC-MIN-PERMIT"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("permit lobby allowed before 100 receipts: %v", err)
	}
	if initial.Data(ctx).SceneEntry != "" || entered.Data(ctx).SceneUnit != "UNIT-ONE-1" || entered.Credits() != 0 {
		t.Fatal("entry changed input or granted credit")
	}
	plan, err := owner.RestorePlan(ctx, entered)
	if err != nil || plan.State != "ST-SCN" || plan.SceneEntry != "NAR-01" || plan.Anchor != "ANC-PRO-02" {
		t.Fatalf("scene recovery: %+v, %v", plan, err)
	}
	restored, err := owner.Restore(ctx, entered.Data(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if retry := prepare(t, owner, restored, entry); !reflect.DeepEqual(retry.Data(ctx), restored.Data(ctx)) {
		t.Fatal("entry retry changed state")
	}
	unit := next(restored)
	unit.Story = &StoryChange{Action: StorySceneUnit, Narrative: "NAR-01", Unit: "UNIT-ONE-2"}
	restored = prepare(t, owner, restored, unit)
	if restored.Data(ctx).SceneUnit != "UNIT-ONE-2" || restored.Data(ctx).OperationSequence != 2 {
		t.Fatal("unit entry not recorded")
	}
	invalid := restored.Data(ctx)
	invalid.SceneUnit = "../unit"
	if _, err = owner.Restore(ctx, invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("malformed unit accepted: %v", err)
	}
	invalid = restored.Data(ctx)
	invalid.SceneEntry = "NAR-04"
	if _, err = owner.Restore(ctx, invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("inconsistent entry accepted: %v", err)
	}
	complete := next(restored)
	complete.Story = &StoryChange{Action: StoryNarrative, Narrative: "NAR-01"}
	completed := prepare(t, owner, restored, complete)
	plan, err = owner.RestorePlan(ctx, completed)
	if err != nil || completed.Data(ctx).SceneEntry != "" || completed.Data(ctx).SceneUnit != "" || plan.SceneEntry != "NAR-02" || completed.Credits() != 0 {
		t.Fatalf("completion: %+v %v", plan, err)
	}
}

func TestArrivalEntriesRestoreBeforeCompletion(t *testing.T) {
	ctx := context.Background()
	owner, manifest := fixture(t)
	started, err := owner.Start(ctx, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	enter := func(snapshot Snapshot, narrative content.NarrativeID, anchor content.AnchorID) Snapshot {
		t.Helper()
		event := next(snapshot)
		event.Story = &StoryChange{Action: StorySceneEnter, Narrative: narrative, Unit: "UNIT-ARRIVAL-1"}
		entry := prepare(t, owner, snapshot, event)
		plan, planErr := owner.RestorePlan(ctx, entry)
		if planErr != nil || plan.State != "ST-SCN" || plan.SceneEntry != string(narrative) || plan.Anchor != anchor {
			t.Fatalf("entry %s: %+v %v", narrative, plan, planErr)
		}
		if len(entry.Data(ctx).Narratives) != len(snapshot.Data(ctx).Narratives) {
			t.Fatal("entry fabricated milestone")
		}
		finish := next(entry)
		finish.Story = &StoryChange{Action: StoryNarrative, Narrative: narrative}
		return prepare(t, owner, entry, finish)
	}
	started = enter(started, "NAR-04", "ANC-HOS-01")
	for institution := 0; institution < 3; institution++ {
		for family := 0; family < 4; family++ {
			for _, id := range manifest.Batches(ctx)[institution*4+family].Deeds {
				started, _ = certify(t, owner, manifest, started, id)
			}
		}
		closure := manifest.Deeds(ctx)[institution*25+24]
		started, err = owner.DefeatBoss(ctx, started, closure.Deed.Encounter)
		if err != nil {
			t.Fatal(err)
		}
		started, _ = certify(t, owner, manifest, started, closure.Deed.ID)
	}
	completed := enter(started, "NAR-08", "ANC-INS-01")
	if completed.Credits() != 75 || completed.Data(ctx).DebtCents != 0 {
		t.Fatal("arrival scene changed credits or debt")
	}
}

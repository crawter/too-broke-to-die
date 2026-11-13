// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"fmt"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/simulation"
)

// permitCandidate consumes only a completed interaction from the protected
// review counter; the campaign decides whether the ledger qualifies.
func (service *Service) permitCandidate(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if service.world == nil || service.replay || service.committed.Data(ctx).Permit {
		return nil
	}
	for _, event := range service.world.InteractionView(ctx).Events {
		if event.Kind == "completed" && event.Target == simulation.PermitReviewTarget {
			return service.commitStory(ctx, campaign.StoryChange{Action: campaign.StoryPermit}, StateActive, "permit")
		}
	}
	return nil
}

// advanceReplay changes only transient presentation. In particular it never
// calls the campaign transition or storage boundary, including at the tableau.
func (service *Service) advanceReplay(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if !service.replay {
		return fmt.Errorf("%w: replay is not active", InvalidAction)
	}
	switch campaign.EndingStage(service.scene.definition.Ending) {
	case campaign.EndingDeath:
		return service.loadScene(ctx, string(campaign.EndingHell), "")
	case campaign.EndingHell:
		return service.loadScene(ctx, string(campaign.EndingCeremony), "")
	case campaign.EndingCeremony:
		return service.loadScene(ctx, string(campaign.EndingTableau), "")
	case campaign.EndingTableau:
		service.replay = false
		service.scene = scenePlayback{}
		return service.policy.Transition(ctx, EndReplay, "")
	default:
		return fmt.Errorf("%w: invalid replay stage", ContentInvalid)
	}
}

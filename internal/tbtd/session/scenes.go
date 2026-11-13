// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"fmt"
	"math"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
	"too-broke-to-die/internal/tbtd/simulation"
)

func closureNarrative(deed content.DeedID) content.NarrativeID {
	switch deed {
	case "D-HOS-END":
		return "NAR-05"
	case "D-CHU-END":
		return "NAR-06"
	case "D-MIN-END":
		return "NAR-07"
	case "D-INS-END":
		return "NAR-09"
	default:
		return ""
	}
}

func isClosureNarrative(id content.NarrativeID) bool {
	switch id {
	case "NAR-05", "NAR-06", "NAR-07", "NAR-09":
		return true
	default:
		return false
	}
}

type scenePlayback struct {
	definition content.Scene
	unit       int
	ticks      uint64
	hold       int
	pressed    bool
}

// NewWithScenes constructs a session with a separately validated catalogue of
// authored scenes. New remains usable by callers that have not yet supplied
// scene content; it does not invent presentation resources.
func NewWithScenes(ctx context.Context, owner *campaign.Campaign, manifest *content.Campaign, bays []content.ServiceBay, store ProgressStore, catalogue *content.Catalogue) (_ *Service, err error) {
	defer mon.Task()(&ctx)(&err)
	if catalogue == nil {
		return nil, fmt.Errorf("%w: missing scene catalogue", ContentInvalid)
	}
	service, err := New(ctx, owner, manifest, bays, store)
	if err != nil {
		return nil, err
	}
	service.scenes = make(map[string]content.Scene)
	for _, scene := range catalogue.Definitions(ctx).Scenes {
		key := string(scene.Narrative)
		if key == "" {
			key = string(scene.Ending)
		}
		service.scenes[key] = scene
	}
	for _, key := range []string{"NAR-01", "NAR-02", "NAR-03", "NAR-04", "NAR-05", "NAR-06", "NAR-07", "NAR-08", "NAR-09", "END-DEATH", "END-HELL", "END-CEREMONY", "END-TABLEAU"} {
		if scene, ok := service.scenes[key]; !ok || len(scene.Units) == 0 {
			return nil, fmt.Errorf("%w: missing scene %s", ContentInvalid, key)
		}
	}
	return service, nil
}

func (service *Service) loadScene(ctx context.Context, key, unit string) (err error) {
	defer mon.Task()(&ctx)(&err)
	definition, ok := service.scenes[key]
	if !ok || len(definition.Units) == 0 {
		return fmt.Errorf("%w: missing scene %s", ContentInvalid, key)
	}
	index := 0
	if unit != "" {
		index = -1
		for i, candidate := range definition.Units {
			if candidate.ID == unit {
				index = i
				break
			}
		}
		if index < 0 {
			return fmt.Errorf("%w: missing unit %s in %s", ContentInvalid, unit, key)
		}
	}
	service.scene = scenePlayback{definition: definition, unit: index}
	return nil
}

// narrativeCandidate accepts only a completed authored interaction or the
// pursuit's physical destination, never a menu-supplied milestone identity.
func (service *Service) narrativeCandidate(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if len(service.scenes) == 0 || service.world == nil || service.replay {
		return nil
	}
	committed := service.committed.Data(ctx)
	_, world := service.world.Snapshot(ctx).Data(ctx)
	if world.Encounter == "ENC-PRO-02" && committed.DonationTheft && len(committed.Narratives) == 0 && committed.SceneEntry == "" {
		for _, bay := range service.bays {
			for _, room := range bay.Institution.Prologue {
				if room.Encounter == world.Encounter && world.Player.Bounds.X+world.Player.Bounds.Width/2 >= room.Objective.X {
					return service.startNarrative(ctx, "NAR-01")
				}
			}
		}
	}
	for _, event := range service.world.InteractionView(ctx).Events {
		if event.Kind != "completed" {
			continue
		}
		switch event.Target {
		case simulation.PrologueDonationTarget:
			if world.Encounter != "ENC-PRO-01" || committed.DonationTheft || len(committed.Narratives) != 0 {
				continue
			}
			if err = service.policy.Transition(ctx, OpenPause, ""); err != nil {
				return err
			}
			service.confirmation = "steal_donations"
			return nil
		case simulation.HospitalPassageTarget:
			return service.startNarrative(ctx, "NAR-04")
		case simulation.InstitutePassageTarget:
			return service.startNarrative(ctx, "NAR-08")
		}
	}
	return nil
}

func (service *Service) startNarrative(ctx context.Context, id content.NarrativeID) (err error) {
	defer mon.Task()(&ctx)(&err)
	if service.policy.State() != StateActive || service.replay {
		return fmt.Errorf("%w: narrative unavailable", InvalidAction)
	}
	definition, ok := service.scenes[string(id)]
	if !ok || len(definition.Units) == 0 {
		return fmt.Errorf("%w: missing narrative scene %s", ContentInvalid, id)
	}
	return service.commitStory(ctx, campaign.StoryChange{Action: campaign.StorySceneEnter, Narrative: id, Unit: definition.Units[0].ID}, StateScene, string(id)+" entry")
}

func (service *Service) commitStory(ctx context.Context, change campaign.StoryChange, destination AppState, label string) (err error) {
	defer mon.Task()(&ctx)(&err)
	if service.replay || service.pending != nil {
		return fmt.Errorf("%w: story write unavailable", InvalidAction)
	}
	sequence := service.committed.Data(ctx).OperationSequence
	if sequence == math.MaxUint64 {
		return fmt.Errorf("%w: operation sequence exhausted", Internal)
	}
	transition := campaign.Transition{CampaignID: string(service.id), Sequence: sequence + 1, Story: &change}
	proposal, err := service.campaign.Prepare(ctx, service.committed, transition)
	if err != nil {
		return err
	}
	if proposal.Data(ctx).OperationSequence != transition.Sequence {
		return nil // An already completed named event is not another write.
	}
	plan, err := service.campaign.RestorePlan(ctx, proposal)
	if err != nil {
		return err
	}
	if destination == StateScene {
		definition, ok := service.scenes[plan.SceneEntry]
		if !ok || len(definition.Units) == 0 {
			return fmt.Errorf("%w: missing scene %s", ContentInvalid, plan.SceneEntry)
		}
		unit := proposal.Data(ctx).SceneUnit
		if unit != "" {
			found := false
			for _, candidate := range definition.Units {
				found = found || candidate.ID == unit
			}
			if !found {
				return fmt.Errorf("%w: missing unit %s", ContentInvalid, unit)
			}
		}
	}
	if _, err = simulation.NewWorld(ctx, service.campaign, service.manifest, proposal, service.bays, nil); err != nil {
		return fmt.Errorf("%w: story restoration: %w", ContentInvalid, err)
	}
	return service.beginCommit(ctx, proposal, OperationID{Event: label, Sequence: transition.Sequence}, "story", label, destination)
}

// restoreStoryView reconstructs a world and the saved scene only after the
// store has acknowledged the complete proposal.
func (service *Service) restoreStoryView(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	plan, err := service.campaign.RestorePlan(ctx, service.committed)
	if err != nil {
		return err
	}
	world, err := simulation.NewWorld(ctx, service.campaign, service.manifest, service.committed, service.bays, nil)
	if err != nil {
		return err
	}
	if plan.State == string(StateScene) {
		if err = service.loadScene(ctx, plan.SceneEntry, service.committed.Data(ctx).SceneUnit); err != nil {
			return err
		}
	} else {
		service.scene = scenePlayback{}
	}
	service.world = world
	return service.policy.BindWorld(ctx, world)
}

func (service *Service) advanceScene(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	playback := &service.scene
	if service.policy.State() != StateScene || len(playback.definition.Units) == 0 {
		return fmt.Errorf("%w: no current scene", InvalidAction)
	}
	if playback.unit+1 == len(playback.definition.Units) {
		return service.finishCurrentScene(ctx)
	}
	if service.replay {
		playback.unit++
		playback.ticks, playback.hold, playback.pressed = 0, 0, false
		return nil
	}
	next := playback.definition.Units[playback.unit+1]
	change := campaign.StoryChange{Action: campaign.StorySceneUnit, Narrative: playback.definition.Narrative, Stage: campaign.EndingStage(playback.definition.Ending), Unit: next.ID}
	return service.commitStory(ctx, change, StateScene, playback.definition.ID+"/"+next.ID)
}

func (service *Service) finishCurrentScene(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	definition := service.scene.definition
	if service.policy.State() != StateScene || len(definition.Units) == 0 {
		return fmt.Errorf("%w: no current scene", InvalidAction)
	}
	if service.replay {
		return service.advanceReplay(ctx)
	}
	if definition.Ending != "" {
		stage := campaign.EndingStage(definition.Ending)
		destination := StateScene
		if stage == campaign.EndingTableau {
			destination = StateEnd
		}
		return service.commitStory(ctx, campaign.StoryChange{Action: campaign.StoryEnding, Stage: stage}, destination, string(stage))
	}
	if isClosureNarrative(definition.Narrative) {
		return service.commitStory(ctx, campaign.StoryChange{Action: campaign.StorySceneFinish, Narrative: definition.Narrative}, StateActive, string(definition.Narrative)+" viewed")
	}
	destination := StateActive
	if definition.Narrative == "NAR-01" || definition.Narrative == "NAR-02" {
		destination = StateScene
	}
	return service.commitStory(ctx, campaign.StoryChange{Action: campaign.StoryNarrative, Narrative: definition.Narrative}, destination, string(definition.Narrative))
}

func (service *Service) sceneInput(ctx context.Context, input FrameInput, wasHeld bool) (err error) {
	defer mon.Task()(&ctx)(&err)
	if service.policy.State() != StateScene || service.confirmation != "" || len(service.scene.definition.Units) == 0 {
		service.scene.pressed, service.scene.hold = false, 0
		return nil
	}
	for _, edge := range input.Edges {
		if edge.Action != settings.MenuConfirm {
			continue
		}
		if edge.Kind == Pressed && !wasHeld {
			service.scene.pressed, service.scene.hold = true, 0
			wasHeld = true
		} else if edge.Kind == Released && wasHeld {
			wasHeld = false
			if service.scene.pressed {
				service.scene.pressed, service.scene.hold = false, 0
				return service.advanceScene(ctx)
			}
		}
	}
	return nil
}

func (service *Service) tickScene() {
	if len(service.scene.definition.Units) == 0 {
		return
	}
	if service.scene.ticks < math.MaxUint64 {
		service.scene.ticks++
	}
	if service.scene.pressed {
		if service.scene.hold < TickRate {
			service.scene.hold++
		}
		if service.scene.hold == TickRate && service.scene.definition.Ending != "END-TABLEAU" {
			service.scene.pressed, service.scene.hold = false, 0
			service.confirmation = "scene_skip"
			service.policy.scene.Freeze()
		}
	}
}

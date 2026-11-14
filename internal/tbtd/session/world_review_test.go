// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
	"too-broke-to-die/internal/tbtd/simulation"
)

func hasCaption(data FrameViewData, id content.PresentationID) bool {
	for _, text := range data.Texts {
		if text.ID == id {
			return true
		}
	}
	return false
}

func TestImpactCaptionSurvivesFramesAndPauseUntilExpiry(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	progress, _ := preparedCertification(t)
	loadFixture(t, service, store, progress)
	simulationServiceTick(t, service, simulation.PlayerCommand{}, []simulation.DamageHit{{Attacker: "hazard", Attack: "caption-hit", Kind: simulation.OrdinaryDamage, Amount: 18, Direction: 1}})
	service.captureEffects(ctx)
	view, err := service.view(ctx)
	if err != nil || !hasCaption(view.Data(ctx), "CAP-HURT") {
		t.Fatalf("missing impact: %v", err)
	}
	service.effects = nil
	simulationServiceTick(t, service, simulation.PlayerCommand{}, nil)
	view, err = service.view(ctx)
	if err != nil || !hasCaption(view.Data(ctx), "CAP-HURT") {
		t.Fatal("caption vanished with transient effects")
	}
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Pause, Kind: Pressed}}})
	paused := frame(t, service, FrameInput{Elapsed: time.Second})
	if !hasCaption(paused, "CAP-HURT") {
		t.Fatal("pause expired caption")
	}
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Pause, Kind: Released}}})
	choose(t, service, "resume", "")
	for i := 0; i < 360; i++ {
		simulationServiceTick(t, service, simulation.PlayerCommand{}, nil)
	}
	view, err = service.view(ctx)
	if err != nil || hasCaption(view.Data(ctx), "CAP-HURT") {
		t.Fatal("expired caption remains")
	}
}

func TestRealLimbFeedbackClearsOnRestoration(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	loadFixture(t, service, store, campaign.Snapshot{})
	const tick = 8333334 * time.Nanosecond
	view := frame(t, service, FrameInput{Elapsed: tick, Edges: []ActionEdge{{Action: settings.RemoveLimb, Kind: Pressed}}})
	for i := 0; i < 45 && !hasCaption(view, "CAP-TEAR"); i++ {
		view = frame(t, service, FrameInput{Elapsed: tick})
	}
	if !hasCaption(view, "CAP-TEAR") {
		t.Fatal("completed removal has no caption")
	}
	view = frame(t, service, FrameInput{Elapsed: tick, Values: []ActionValue{{Action: settings.Aim, X: 1}}, Edges: []ActionEdge{{Action: settings.Throw, Kind: Pressed}}})
	for i := 0; i < 400 && !hasCaption(view, "CAP-LIMB-FLIGHT"); i++ {
		view = frame(t, service, FrameInput{Elapsed: tick})
	}
	if !hasCaption(view, "CAP-LIMB-FLIGHT") {
		t.Fatal("limb release has no caption")
	}
	if err := service.policy.Transition(ctx, OpenPause, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.restore(ctx, ReturnCheckpoint); err != nil {
		t.Fatal(err)
	}
	restored, err := service.view(ctx)
	if err != nil || hasCaption(restored.Data(ctx), "CAP-TEAR") || hasCaption(restored.Data(ctx), "CAP-LIMB-FLIGHT") {
		t.Fatal("restoration replayed transient captions")
	}
}

func TestEmergencyWarningCaptionFollowsLiveState(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	progress, err := service.campaign.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range service.manifest.Batches(ctx)[0].Deeds {
		progress = certifyAuthored(t, service, progress, id)
	}
	loadFixture(t, service, store, progress)
	view, err := service.view(ctx)
	if err != nil || !hasCaption(view.Data(ctx), "CAP-WARN-EMERGENCY") {
		t.Fatalf("missing restored warning: %v", err)
	}
}

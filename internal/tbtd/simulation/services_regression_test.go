// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

// commitServiceChange starts from a completed console hold and exercises the
// real campaign preparation and explicit storage acknowledgment boundary.
func commitServiceChange(t *testing.T, world *World, owner *campaign.Campaign, manifest *content.Campaign, bays []content.ServiceBay, committed campaign.Snapshot, action campaign.AttemptAction, id content.DeedID, checkPending bool) campaign.Snapshot {
	t.Helper()
	ctx := context.Background()
	if _, err := world.RequestService(ctx, owner, manifest, committed, action, id); err != nil {
		t.Fatal(err)
	}
	transition := campaign.Transition{CampaignID: serviceCampaignID, Sequence: committed.Data(ctx).OperationSequence + 1}
	proposal, err := world.PrepareService(ctx, owner, committed, transition)
	if err != nil {
		t.Fatal(err)
	}
	if checkPending {
		failed := serviceAck(proposal)
		failed.Succeeded = false
		if err = world.AcknowledgeService(ctx, owner, manifest, bays, failed); err == nil {
			t.Fatal("failed storage acknowledgment was accepted")
		}
		if len(world.combat.projectiles) != 1 || len(world.limbs.objects) == 0 || !world.limbs.objects[0].Airborne {
			t.Fatal("an unsuccessful save changed the existing flight")
		}
	}
	player, health, ammunition, attached := world.state.Player, world.state.Health, world.state.Feces, world.motion.limbs
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	if world.state.Player != player || world.state.Health != health || world.state.Feces != ammunition || world.motion.limbs != attached {
		t.Fatal("attempt transition restored or consumed player resources")
	}
	return proposal
}

func TestAttemptTransitionsRetireOldFlights(t *testing.T) {
	ctx := context.Background()
	for _, family := range []int{2, 3} {
		for _, mode := range []string{"reset", "abandon-reaccept", "switch"} {
			t.Run(fmt.Sprintf("family-%d/%s", family, mode), func(t *testing.T) {
				owner, manifest, bays, committed, world := activeService(t, family)
				oldID := world.service.attempt.Deed
				serviceHold(t, world, 160, nil)
				world.initLimbs()
				world.motion.limbs &^= LeftLeg
				world.state.Limbs = 3
				world.state.Health, world.state.Feces = 41, 2
				world.limbs.objects = []LimbObject{{ID: "old-limb", Socket: SocketLeftLeg, Generation: world.limbs.generation, Airborne: true, Damaging: true, Position: Vector{X: 787, Y: 120}, Velocity: Vector{X: 600}, FlightTick: world.state.Tick}}
				world.combat.projectiles = []Projectile{{ID: "old-feces", Attacker: "player", Attack: "old", Position: Vector{X: 767, Y: 120}, Velocity: Vector{X: 600}, PlayerOwned: true}}
				if mode == "reset" {
					committed = commitServiceChange(t, world, owner, manifest, bays, committed, campaign.AttemptReset, oldID, true)
				} else {
					committed = commitServiceChange(t, world, owner, manifest, bays, committed, campaign.AttemptAbandon, oldID, true)
					serviceHold(t, world, 160, nil)
					id := oldID
					if mode == "switch" {
						id = manifest.Batches(ctx)[family].Deeds[1]
					}
					committed = commitServiceChange(t, world, owner, manifest, bays, committed, campaign.AttemptAccept, id, false)
				}
				if len(world.combat.projectiles) != 0 {
					t.Fatal("old feces projectile survived the attempt transition")
				}
				limbs := world.LimbView(ctx)
				if len(limbs.Objects) != 1 || limbs.Objects[0].ID != "old-limb" || limbs.Objects[0].Socket != SocketLeftLeg || limbs.Objects[0].Airborne || limbs.Objects[0].Damaging || limbs.Objects[0].Position.X != 96 || limbs.Attached&LeftLeg != 0 {
					t.Fatalf("old limb was lost or remained dangerous: %+v", limbs)
				}
				serviceTick(t, world, PlayerCommand{})
				view := world.ServiceView(ctx)
				if view.Status != ServiceActive || view.PaddleLatched || view.CameraInterrupted || view.CameraFouled || view.FuseTripped {
					t.Fatalf("old flight affected the new attempt: %+v", view)
				}
				// A fresh action in the new attempt must still work; the retired
				// leg remains a recoverable object rather than being refunded.
				completeServiceActions(t, world)
				proposal, err := world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: committed.Data(ctx).OperationSequence + 1})
				if err != nil || proposal.Credits() != committed.Credits()+1 {
					t.Fatalf("fresh action did not complete the new request: %v", err)
				}
			})
		}
	}
}

func TestResetDoesNotTransferSettledPlateEvidence(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world := activeService(t, 1)
	serviceHold(t, world, 160, nil)
	world.initLimbs()
	world.motion.limbs &^= LeftLeg
	world.state.Limbs = 3
	world.limbs.objects = []LimbObject{{ID: "old-weight", Socket: SocketLeftLeg, Generation: world.limbs.generation, Position: Vector{X: 432, Y: 1}}}
	if !world.rescueWeighted() {
		t.Fatal("fixture is not on the authored plate")
	}
	committed = commitServiceChange(t, world, owner, manifest, bays, committed, campaign.AttemptReset, world.service.attempt.Deed, false)
	if world.rescueWeighted() || len(world.limbs.objects) != 1 || world.limbs.objects[0].Position.X != 96 || world.limbs.objects[0].ID != "old-weight" {
		t.Fatalf("old weight transferred or disappeared: %+v", world.LimbView(ctx))
	}
	serviceTick(t, world, PlayerCommand{})
	if world.rescueGateLatched() || world.ServiceView(ctx).Status != ServiceActive || committed.Credits() != 6 {
		t.Fatal("reset fabricated rescue progress")
	}
}

func TestCatwalkTrolleyRoutingUsesServiceBoundary(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, active, _ := activeService(t, 0)
	committed := certifyFixture(t, owner, manifest, active, "D-HOS-SUP-01")
	active, err := owner.Accept(ctx, committed, "D-HOS-SUP-02")
	if err != nil {
		t.Fatal(err)
	}
	newWorld := func() (*World, *PuzzleObject) {
		t.Helper()
		world, buildErr := NewWorld(ctx, owner, manifest, active, bays, nil)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		for i := range world.combat.enemies {
			world.combat.enemies[i].Body.Bounds.X = 242
		}
		for i := range world.objects.items {
			object := &world.objects.items[i]
			if object.Kind != ObjectTrolley {
				continue
			}
			object.Bounds.X = 816
			world.syncObjectPlatforms()
			world.state.Player.Bounds = Rect{X: 788, Y: 80, Width: PlayerWidth, Height: PlayerHeight}
			world.state.Player.Support = "terrain-CATWALK"
			return world, object
		}
		t.Fatal("missing active trolley")
		return nil, nil
	}
	world, trolley := newWorld()
	serviceTick(t, world, PlayerCommand{Move: 1, Edges: []PlayerEdge{press(settings.Jump)}})
	if trolley.Bounds.X != 816 {
		t.Fatal("jumping moved the trolley")
	}
	world, trolley = newWorld()
	serviceTick(t, world, PlayerCommand{Move: 1}, DamageHit{Attacker: "hazard", Attack: "one", Kind: OrdinaryDamage, Amount: 15, Direction: 1})
	if trolley.Bounds.X != 816 {
		t.Fatal("damage-interrupted push moved the trolley")
	}
	world, trolley = newWorld()
	for tick := 0; tick < 120; tick++ {
		before := trolley.Bounds.X
		serviceTick(t, world, PlayerCommand{Move: 1})
		if trolley.Bounds.X-before > 100*TickSeconds+Tolerance {
			t.Fatal("trolley exceeded the push-speed limit")
		}
	}
	if trolley.Bounds.X <= 880 {
		t.Fatalf("trolley did not cross authored cover: %v", trolley.Bounds.X)
	}
	for _, projectile := range []bool{false, true} {
		hit, found, sweepErr := world.grid.Sweep(ctx, Rect{X: 840, Y: 10, Width: 1, Height: 1}, Vector{X: 100}, nil, SweepOptions{Projectile: projectile})
		if sweepErr != nil || !found || hit.Collider.ID != "variant-cover" {
			t.Fatalf("cover stopped blocking a player or projectile: %+v %v", hit, sweepErr)
		}
	}
}

func TestResetPreservesCarriedLimbAndSpentAmmunition(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world := activeService(t, 2)
	serviceHold(t, world, 160, nil)
	world.initLimbs()
	world.motion.limbs &^= LeftArm | LeftLeg
	world.state.Limbs = 2
	world.state.Health, world.state.Feces = 47, 1
	world.limbs.objects = []LimbObject{
		{ID: "old-flight", Socket: SocketLeftLeg, Generation: world.limbs.generation, Position: Vector{X: 787, Y: 120}, Airborne: true, Damaging: true},
		{ID: "carried", Socket: SocketLeftArm, Generation: world.limbs.generation, Carried: true},
	}
	world.limbs.carried, world.limbs.payload = "carried", true
	world.combat.projectiles = []Projectile{{ID: "spent", Attacker: "player", Attack: "old", PlayerOwned: true}}
	commitServiceChange(t, world, owner, manifest, bays, committed, campaign.AttemptReset, world.service.attempt.Deed, true)
	view := world.LimbView(ctx)
	if view.Carried != "carried" || !view.LimbPayload || len(view.Objects) != 2 || !view.Objects[1].Carried || view.Objects[0].Airborne || world.state.Feces != 1 || world.state.Health != 47 {
		t.Fatalf("reset lost a carried resource or refunded ammunition: %+v", view)
	}
}

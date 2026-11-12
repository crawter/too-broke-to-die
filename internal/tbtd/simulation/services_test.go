// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

const serviceCampaignID = "00000000-0000-4000-8000-000000000001"

func serviceAck(snapshot campaign.Snapshot) ServiceAcknowledgment {
	data := snapshot.Data(context.Background())
	return ServiceAcknowledgment{Succeeded: true, CampaignID: data.CampaignID, Sequence: data.OperationSequence, Snapshot: snapshot}
}

func serviceTick(t *testing.T, world *World, player PlayerCommand, hits ...DamageHit) {
	t.Helper()
	command := ServiceCommand{Objects: ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: player, Hits: hits, Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}}}}}}
	if err := world.StepServices(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func serviceHazardTick(t *testing.T, world *World, player PlayerCommand, hazards []bool) {
	t.Helper()
	command := ServiceCommand{Hazards: hazards, Objects: ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: player, Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}}}}}}
	if err := world.StepServices(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func serviceHold(t *testing.T, world *World, x float64, lastHit *DamageHit) {
	t.Helper()
	world.state.Player.Bounds.X = x
	world.state.Player.Bounds.Y = 0
	world.state.Player.Velocity = Vector{}
	world.state.Player.Support = "boundary-floor"
	for tick := 1; tick <= 36; tick++ {
		player := PlayerCommand{}
		if tick == 1 {
			player.Edges = []PlayerEdge{release(settings.Interact), press(settings.Interact)}
		}
		if tick == 36 && lastHit != nil {
			serviceTick(t, world, player, *lastHit)
		} else {
			serviceTick(t, world, player)
		}
	}
}

func activeService(t *testing.T, family int) (*campaign.Campaign, *content.Campaign, []content.ServiceBay, campaign.Snapshot, *World) {
	t.Helper()
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	snapshot, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	batches := manifest.Batches(ctx)
	for i := 0; i < family; i++ {
		for _, id := range batches[i].Deeds {
			snapshot = certifyFixture(t, owner, manifest, snapshot, id)
		}
	}
	snapshot, err = owner.Accept(ctx, snapshot, batches[family].Deeds[0])
	if err != nil {
		t.Fatal(err)
	}
	anchor := content.AnchorID(fmt.Sprintf("ANC-HOS-%02d", family+1))
	snapshot, err = owner.Checkpoint(ctx, snapshot, anchor)
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, snapshot, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	return owner, manifest, bays, snapshot, world
}

func completeServiceActions(t *testing.T, world *World) {
	t.Helper()
	ctx := context.Background()
	// Keep the real local enemies behind the service route; they remain live
	// but do not begin offscreen attacks during this deterministic audit.
	for i := range world.combat.enemies {
		world.combat.enemies[i].Body.Bounds.X = 242
		world.combat.enemies[i].Body.Velocity = Vector{}
	}
	switch world.service.deed.Family {
	case "SUP":
		var trolley *PuzzleObject
		for i := range world.objects.items {
			if world.objects.items[i].Kind == ObjectTrolley {
				trolley = &world.objects.items[i]
				break
			}
		}
		if trolley == nil {
			t.Fatal("missing trolley")
		}
		world.state.Player.Bounds.X = trolley.Bounds.X - PlayerWidth
		world.state.Player.Support = "boundary-floor"
		catwalk := false
		returned := false
		for tick := 0; tick < 1800 && !world.supplyReady(); tick++ {
			// The authored catwalk lets the player push over variant 02's cover.
			if world.service.deed.Variant == 2 && !catwalk && trolley.Bounds.X+trolley.Bounds.Width >= 879 {
				world.state.Player.Bounds.X = trolley.Bounds.X - PlayerWidth
				world.state.Player.Bounds.Y = 80
				world.state.Player.Velocity = Vector{}
				world.state.Player.Support = "terrain-CATWALK"
				catwalk = true
			}
			if catwalk && !returned && trolley.Bounds.X >= 941 {
				world.state.Player.Bounds.X = trolley.Bounds.X - PlayerWidth
				world.state.Player.Bounds.Y = 0
				world.state.Player.Velocity = Vector{}
				world.state.Player.Support = "boundary-floor"
				returned = true
			}
			serviceTick(t, world, PlayerCommand{Move: 1})
		}
		if !world.supplyReady() {
			t.Fatalf("trolley did not reach pad: %+v, player %+v", *trolley, world.state.Player)
		}
	case "RES":
		var crate *PuzzleObject
		for i := range world.objects.items {
			if world.objects.items[i].Kind == ObjectCrate {
				crate = &world.objects.items[i]
				break
			}
		}
		if crate == nil {
			t.Fatal("missing crate")
		}
		world.state.Player.Bounds.X = crate.Bounds.X - PlayerWidth
		world.state.Player.Support = "boundary-floor"
		for tick := 0; tick < 120 && !world.rescueWeighted(); tick++ {
			serviceTick(t, world, PlayerCommand{Move: 1})
		}
		serviceHold(t, world, 700, nil)
		if !world.rescueGateLatched() {
			t.Fatal("gate did not latch")
		}
		serviceHold(t, world, 752, nil)
		for tick := 0; tick < 1000 && !world.service.refuge; tick++ {
			if world.service.waiting {
				for _, adult := range world.combat.civilians {
					if adult.ID == string(world.service.roles[content.RoleActor]) {
						serviceHold(t, world, adult.Bounds.X-30, nil)
						break
					}
				}
			} else {
				serviceTick(t, world, PlayerCommand{})
			}
		}
	case "UTL":
		world.state.Player.Bounds = Rect{X: 610, Y: 80, Width: PlayerWidth, Height: PlayerHeight}
		world.state.Player.Support = "terrain-STEP-2"
		serviceTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.RemoveLimb)}})
		for i := 1; i < 42; i++ {
			serviceTick(t, world, PlayerCommand{})
		}
		serviceTick(t, world, PlayerCommand{Aim: Vector{X: 1, Y: 0.5}, Edges: []PlayerEdge{press(settings.Throw)}})
		for tick := 0; tick < 100 && !world.service.paddle; tick++ {
			serviceTick(t, world, PlayerCommand{})
		}
		for world.state.Tick < world.motion.actionUntil {
			serviceTick(t, world, PlayerCommand{})
		}
		serviceHold(t, world, 970, nil)
	case "PRV":
		serviceHold(t, world, 394, nil)
		world.state.Player.Bounds = Rect{X: 610, Y: 80, Width: PlayerWidth, Height: PlayerHeight}
		world.state.Player.Velocity = Vector{}
		world.state.Player.Support = "terrain-LEDGE"
		serviceTick(t, world, PlayerCommand{Aim: Vector{X: 1, Y: 0.5}, Edges: []PlayerEdge{press(settings.Throw)}})
		for tick := 0; tick < 100 && !world.service.camera; tick++ {
			serviceTick(t, world, PlayerCommand{})
		}
		for world.state.Tick < world.motion.actionUntil {
			serviceTick(t, world, PlayerCommand{})
		}
	}
	if world.ServiceView(ctx).Status != ServiceEvidenceReady {
		t.Fatalf("%s evidence not ready: %+v", world.service.attempt.Deed, world.ServiceView(ctx))
	}
	serviceHold(t, world, 1364, nil)
	if world.ServiceView(ctx).Status != ServicePending {
		t.Fatalf("%s terminal hold failed: %+v, prompt %+v", world.service.attempt.Deed, world.ServiceView(ctx), world.InteractionView(ctx))
	}
}

func TestAllServiceBindings(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	snapshot, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for index, batch := range manifest.Batches(ctx) {
		for number, id := range batch.Deeds {
			active, acceptErr := owner.Accept(ctx, snapshot, id)
			if acceptErr != nil {
				t.Fatal(acceptErr)
			}
			var bay content.ServiceBay
			for _, item := range bays {
				if item.Deed == id {
					bay = item
					break
				}
			}
			active, err = owner.Checkpoint(ctx, active, bay.Anchor)
			if err != nil {
				t.Fatal(err)
			}
			world, buildErr := NewWorld(ctx, owner, manifest, active, bays, nil)
			if buildErr != nil {
				t.Fatalf("%s: %v", id, buildErr)
			}
			view := world.ServiceView(ctx)
			if view.Deed != id || view.Family != batch.Family || view.Variant != number+1 || view.Attempt != 1 || view.Status != ServiceActive || view.Objective == "" {
				t.Fatalf("bad binding: %+v", view)
			}
			if world.service.roles[content.RoleTerminal] == "" || world.serviceReady() || len(world.service.ticketObjects) == 0 {
				t.Fatalf("%s: missing bound objects or premature evidence", id)
			}
			completeServiceActions(t, world)
			transition := campaign.Transition{CampaignID: serviceCampaignID, Sequence: active.Data(ctx).OperationSequence + 1}
			proposed, prepareErr := world.PrepareService(ctx, owner, active, transition)
			if prepareErr != nil || proposed.Credits() != active.Credits()+1 {
				t.Fatalf("%s certification: %v", id, prepareErr)
			}
			if ackErr := world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(proposed)); ackErr != nil {
				t.Fatalf("%s acknowledgment: %v", id, ackErr)
			}
			count++
			snapshot = proposed
		}
		if index%4 == 3 && index < 15 {
			boss := content.EncounterID("ENC-" + string(batch.Institution)[4:] + "-05")
			snapshot, err = owner.DefeatBoss(ctx, snapshot, boss)
			if err != nil {
				t.Fatal(err)
			}
			snapshot = certifyFixture(t, owner, manifest, snapshot, content.DeedID("D-"+string(batch.Institution)[4:]+"-END"))
		}
	}
	if count != 96 {
		t.Fatalf("got %d tickets", count)
	}
}

func TestSupplyCertificationAndSameTickDeath(t *testing.T) {
	ctx := context.Background()
	owner, _, _, committed, world := activeService(t, 0)
	serviceHold(t, world, 1364, nil)
	if world.service.pending != nil || world.ServiceView(ctx).Status != ServiceActive {
		t.Fatal("unloaded terminal supplied evidence")
	}
	for i := range world.objects.items {
		if world.objects.items[i].Kind == ObjectTrolley {
			world.objects.items[i].Bounds.X = 1320 - world.objects.items[i].Bounds.Width/2
		}
	}
	world.syncObjectPlatforms()
	lethal := DamageHit{Attacker: "crusher", Attack: "one", Kind: LethalDamage, Amount: 100, Direction: 1}
	serviceHold(t, world, 1364, &lethal)
	if world.ServiceView(ctx).Status != ServicePending || committed.Credits() != 0 {
		t.Fatalf("completion was not pending: %+v", world.ServiceView(ctx))
	}
	transition := campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1, Death: campaign.DeathGameplay, DeathDetail: "crusher"}
	proposed, err := world.PrepareService(ctx, owner, committed, transition)
	if err != nil {
		t.Fatal(err)
	}
	if proposed.Credits() != 1 || committed.Credits() != 0 || proposed.Data(ctx).DebtCents != 4999 {
		t.Fatal("completion and death were not prepared atomically")
	}
	if err = world.StepServices(ctx, ServiceCommand{}); err == nil {
		t.Fatal("continued gameplay while saving")
	}
	retry, err := world.PrepareService(ctx, owner, proposed, transition)
	if err != nil || retry.Credits() != 1 {
		t.Fatalf("retry: %v", err)
	}
	receipt, err := world.ServiceReceipt(ctx, owner, proposed, "D-HOS-SUP-01")
	if err != nil || receipt == nil || receipt.Ordinal != 1 {
		t.Fatalf("receipt: %+v %v", receipt, err)
	}
	if pending, err := world.ServiceReceipt(ctx, owner, committed, "D-HOS-SUP-01"); err != nil || pending != nil {
		t.Fatal("pending completion published as receipt")
	}
}

func TestServiceAcknowledgmentAndConsoleRequests(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world := activeService(t, 0)
	for i := range world.objects.items {
		if world.objects.items[i].Kind == ObjectTrolley {
			world.objects.items[i].Bounds.X = 1288
		}
	}
	world.syncObjectPlatforms()
	world.state.Health, world.state.Feces = 37, 1
	serviceHold(t, world, 1364, nil)
	position := world.state.Player.Bounds
	proposal, err := world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(committed)); err == nil {
		t.Fatal("accepted old snapshot as a successful save")
	}
	failedSave := serviceAck(proposal)
	failedSave.Succeeded = false
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, failedSave); err == nil {
		t.Fatal("accepted a failed save carrying the proposed snapshot")
	}
	wrongOperation := serviceAck(proposal)
	wrongOperation.Sequence++
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, wrongOperation); err == nil {
		t.Fatal("accepted a different operation acknowledgment")
	}
	if err = world.StepServices(ctx, ServiceCommand{}); err == nil || world.state.Health != 37 || world.state.Feces != 1 {
		t.Fatal("failed save did not keep the world frozen")
	}
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	if world.state.Player.Bounds != position || world.state.Health != 37 || world.state.Feces != 1 || world.ServiceView(ctx).Status != ServiceCertified || len(world.state.Receipts) != 1 {
		t.Fatalf("acknowledgment restored or lost the living state: %+v", world.ServiceView(ctx))
	}
	serviceHold(t, world, 1364, nil)
	view := world.ServiceView(ctx)
	if view.Receipt == nil || view.Receipt.Certification.Deed != "D-HOS-SUP-01" || world.service.pending != nil {
		t.Fatalf("repeat terminal: %+v", view)
	}
	view.Receipt.Certification.EvidenceObjects[0] = "modified"
	if world.ServiceView(ctx).Receipt.Certification.EvidenceObjects[0] == "modified" {
		t.Fatal("receipt interaction exposed mutable world state")
	}
	serviceHold(t, world, 160, nil)
	receipt, err := world.RequestService(ctx, owner, manifest, proposal, campaign.AttemptAccept, "D-HOS-SUP-01")
	if err != nil || receipt == nil || receipt.Ordinal != 1 {
		t.Fatalf("repeat console: %+v %v", receipt, err)
	}
	if _, err = world.RequestService(ctx, owner, manifest, proposal, campaign.AttemptAccept, "D-HOS-RES-01"); !errors.Is(err, campaign.ErrBlocked) || !strings.Contains(err.Error(), "D-HOS-SUP-02") {
		t.Fatalf("missing prerequisite was not explained: %v", err)
	}
	if _, err = world.RequestService(ctx, owner, manifest, proposal, campaign.AttemptAccept, "D-HOS-SUP-02"); err != nil {
		t.Fatal(err)
	}
	if err = world.StepServices(ctx, ServiceCommand{}); err == nil {
		t.Fatal("accepted request did not freeze before save")
	}
	next, err := world.PrepareService(ctx, owner, proposal, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 2})
	if err != nil {
		t.Fatal(err)
	}
	position = world.state.Player.Bounds
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(next)); err != nil {
		t.Fatal(err)
	}
	if world.ServiceView(ctx).Deed != "D-HOS-SUP-02" || world.ServiceView(ctx).Attempt != 1 || world.state.Player.Bounds != position || world.state.Health != 37 || world.state.Feces != 1 || len(world.state.Receipts) != 1 {
		t.Fatalf("request acknowledgment: %+v", world.ServiceView(ctx))
	}
	serviceTick(t, world, PlayerCommand{})
}

func TestProtectedConsoleWhenIssuerUnavailable(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world := activeService(t, 0)
	if err := world.AddProtectedCivilian(ctx, Civilian{ID: "issuer", Bounds: Rect{X: 160, Width: 28, Height: 58}}); err != nil {
		t.Fatal(err)
	}
	for i := range world.combat.civilians {
		if world.combat.civilians[i].ID == "issuer" {
			world.combat.civilians[i].Incapacitated = true
		}
	}
	serviceHold(t, world, 160, nil)
	if _, err := world.RequestService(ctx, owner, manifest, committed, campaign.AttemptAccept, "D-HOS-SUP-02"); err == nil {
		t.Fatal("accepted a second request while the first remained active")
	}
	if world.service.attemptChange != nil || world.state.Feces != 5 {
		t.Fatal("blocked request consumed resources or changed attempt")
	}
	if _, err := world.RequestService(ctx, owner, manifest, committed, campaign.AttemptReset, "D-HOS-SUP-01"); err != nil {
		t.Fatal(err)
	}
	reset, err := world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(reset)); err != nil || world.ServiceView(ctx).Attempt != 2 {
		t.Fatalf("protected console did not reset request: %v", err)
	}
}

func TestServiceCommitIncludesPendingRefillCharge(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world := activeService(t, 0)
	if err := world.AddRefillStation(ctx, RefillStation{ID: "reporter", Position: Vector{X: 240}, Reporting: true}); err != nil {
		t.Fatal(err)
	}
	world.state.Feces = 0
	world.state.Player.Bounds.X = 240
	world.state.Player.Velocity = Vector{}
	world.state.Player.Support = "boundary-floor"
	serviceTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}})
	for tick := 1; tick < 180; tick++ {
		serviceTick(t, world, PlayerCommand{})
	}
	if world.feces.pending == nil || world.state.Feces != 5 {
		t.Fatal("reporting refill did not produce a pending charge")
	}
	if err := world.StepServices(ctx, ServiceCommand{}); err == nil {
		t.Fatal("uncommitted refill allowed another tick")
	}
	transition := campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1}
	charged, err := world.PrepareServiceWaste(ctx, owner, committed, transition)
	if err != nil || charged.Data(ctx).DebtCents != 999 || len(charged.Data(ctx).Incidents) != 1 {
		t.Fatalf("refill incident: %v", err)
	}
	if _, err = world.PrepareServiceWaste(ctx, owner, charged, transition); err != nil {
		t.Fatalf("refill retry changed its payload: %v", err)
	}
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(charged)); err != nil || world.feces.pending != nil {
		t.Fatalf("committed refill was not acknowledged: %v", err)
	}
	for i := range world.objects.items {
		if world.objects.items[i].Kind == ObjectTrolley {
			world.objects.items[i].Bounds.X = 1288
		}
	}
	world.syncObjectPlatforms()
	serviceHold(t, world, 1364, nil)
	proposal, err := world.PrepareService(ctx, owner, charged, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 2})
	if err != nil || proposal.Data(ctx).DebtCents != 999 || len(proposal.Data(ctx).Incidents) != 1 {
		t.Fatalf("certification lost refill incident: %v", err)
	}
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	serviceHold(t, world, 160, nil)
	if _, err = world.RequestService(ctx, owner, manifest, proposal, campaign.AttemptAccept, "D-HOS-SUP-02"); err != nil {
		t.Fatalf("committed incident blocked next request: %v", err)
	}
}

func TestServiceTicketViewsUseCommittedProgress(t *testing.T) {
	ctx := context.Background()
	owner, manifest, _, committed, world := activeService(t, 0)
	active, err := world.ServiceTicket(ctx, owner, manifest, committed, "D-HOS-SUP-01")
	if err != nil || active.Service.Status != ServiceActive || active.Receipt != nil {
		t.Fatalf("active: %+v %v", active, err)
	}
	blocked, err := world.ServiceTicket(ctx, owner, manifest, committed, "D-HOS-RES-01")
	if err != nil || blocked.Service.Status != ServiceBlocked || len(blocked.Missing) == 0 {
		t.Fatalf("blocked: %+v %v", blocked, err)
	}
	if _, err = world.ServiceTicket(ctx, owner, manifest, committed, "D-HOS-END"); err == nil {
		t.Fatal("closure treated as a service ticket")
	}
	certified := certifyFixture(t, owner, manifest, committed, "D-HOS-SUP-01")
	view, err := world.ServiceTicket(ctx, owner, manifest, certified, "D-HOS-SUP-01")
	if err != nil || view.Service.Status != ServiceCertified || view.Receipt == nil {
		t.Fatalf("certified: %+v %v", view, err)
	}
	view.Receipt.Certification.EvidenceObjects[0] = "changed"
	again, err := world.ServiceTicket(ctx, owner, manifest, certified, "D-HOS-SUP-01")
	if err != nil || again.Receipt.Certification.EvidenceObjects[0] == "changed" {
		t.Fatal("receipt view aliases committed campaign")
	}
	available, err := world.ServiceTicket(ctx, owner, manifest, certified, "D-HOS-SUP-02")
	if err != nil || available.Service.Status != ServiceAvailable {
		t.Fatalf("available: %+v %v", available, err)
	}
}

func TestSiblingTerminalsDoNotStealActiveHold(t *testing.T) {
	ctx := context.Background()
	for family := 0; family < 4; family++ {
		t.Run(fmt.Sprint(family), func(t *testing.T) {
			owner, manifest, bays, active, _ := activeService(t, family)
			batch := manifest.Batches(ctx)[family]
			committed := certifyFixture(t, owner, manifest, active, batch.Deeds[0])
			active, err := owner.Accept(ctx, committed, batch.Deeds[1])
			if err != nil {
				t.Fatal(err)
			}
			world, err := NewWorld(ctx, owner, manifest, active, bays, nil)
			if err != nil {
				t.Fatal(err)
			}
			world.state.Entities = append(world.state.Entities, Entity{ID: "AAA-SIBLING-TERMINAL", Role: content.RoleTerminal, Position: Vector{X: 1344}, Protected: true})
			if err = world.AddInteractionTarget(ctx, InteractionTarget{ID: "AAA-SIBLING-TERMINAL", Kind: InteractionService, Bounds: Rect{X: 1336, Width: 16, Height: 32}}); err != nil {
				t.Fatal(err)
			}
			if err = world.installService(ctx, manifest, bays, active); err != nil {
				t.Fatal(err)
			}
			for _, target := range world.objects.interactions {
				if target.ID == "AAA-SIBLING-TERMINAL" {
					t.Fatal("sibling terminal is still selectable")
				}
			}
			completeServiceActions(t, world)
			proposal, err := world.PrepareService(ctx, owner, active, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(proposal)); err != nil {
				t.Fatal(err)
			}
			serviceHold(t, world, 160, nil)
			receipt, err := world.RequestService(ctx, owner, manifest, proposal, campaign.AttemptAccept, batch.Deeds[0])
			if err != nil || receipt == nil || receipt.Certification.Deed != batch.Deeds[0] {
				t.Fatalf("sibling receipt unavailable: %+v %v", receipt, err)
			}
		})
	}
}

func TestSupplyVariantTwoCoverAndRealPush(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, active, _ := activeService(t, 0)
	committed := certifyFixture(t, owner, manifest, active, "D-HOS-SUP-01")
	active, err := owner.Accept(ctx, committed, "D-HOS-SUP-02")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, active, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, projectile := range []bool{false, true} {
		box := Rect{X: 840, Y: 10, Width: 1, Height: 1}
		hit, found, sweepErr := world.Sweep(ctx, box, Vector{X: 80}, SweepOptions{Projectile: projectile})
		if sweepErr != nil || !found || hit.Collider.ID != "variant-cover" {
			t.Fatalf("cover did not block player/projectile: %+v %v", hit, sweepErr)
		}
	}
	completeServiceActions(t, world)
	proposed, err := world.PrepareService(ctx, owner, active, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
	if err != nil || proposed.Credits() != 2 {
		t.Fatalf("variant 02 did not certify: %v", err)
	}
}

func TestSupplyCrusherFailureAndReset(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world := activeService(t, 0)
	for i := range world.objects.items {
		if world.objects.items[i].Kind == ObjectTrolley {
			world.objects.items[i].Bounds.X = 735
		}
	}
	world.syncObjectPlatforms()
	command := ServiceCommand{CrusherActive: true, Objects: ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}}}}}}
	if err := world.StepServices(ctx, command); err != nil {
		t.Fatal(err)
	}
	if world.ServiceView(ctx).Status != ServiceFailed || !world.ServiceView(ctx).SealBroken {
		t.Fatal("crusher did not fail trolley")
	}
	failed, err := world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
	if err != nil || failed.Credits() != 0 {
		t.Fatalf("failure: %v", err)
	}
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(failed)); err != nil {
		t.Fatal(err)
	}
	serviceHold(t, world, 160, nil)
	if _, err = world.RequestService(ctx, owner, manifest, failed, campaign.AttemptReset, "D-HOS-SUP-01"); err != nil {
		t.Fatal(err)
	}
	reset, err := world.PrepareService(ctx, owner, failed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(reset)); err != nil {
		t.Fatal(err)
	}
	restored := world
	if restored.ServiceView(ctx).Attempt != 2 || restored.ServiceView(ctx).Status != ServiceActive {
		t.Fatal("reset did not restore attempt")
	}
	for _, object := range restored.Objects(ctx) {
		if object.Kind == ObjectTrolley && object.Bounds != object.Origin {
			t.Fatal("reset failed to restore trolley origin")
		}
	}
	completeServiceActions(t, restored)
	certified, err := restored.PrepareService(ctx, owner, reset, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 3})
	if err != nil || certified.Credits() != 1 || len(certified.Data(ctx).Incidents) != 1 {
		t.Fatalf("completion after reset: %v", err)
	}
}

func TestUtilityEarlyBypassAndPrivacyOrder(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world := activeService(t, 2)
	serviceHold(t, world, 970, nil)
	if world.ServiceView(ctx).Status != ServiceFailed || !world.ServiceView(ctx).FuseTripped {
		t.Fatalf("early bypass: %+v", world.ServiceView(ctx))
	}
	_, _, _, _, world = activeService(t, 3)
	camera := world.service.roles[content.RoleCamera]
	world.combat.projectiles = []Projectile{{ID: "test-shot", Attacker: "player", Attack: "1", Position: Vector{X: 767, Y: 120}, Velocity: Vector{X: 600}, PlayerOwned: true}}
	serviceTick(t, world, PlayerCommand{})
	if world.ServiceView(ctx).Status != ServiceFailed || !world.ServiceView(ctx).CameraFouled || camera == "" {
		t.Fatalf("unrecorded camera: %+v, impacts %+v", world.ServiceView(ctx), world.feces.impacts)
	}
}

func TestUtilityPaddleAndPrivacyCompletion(t *testing.T) {
	ctx := context.Background()
	owner, _, _, committed, world := activeService(t, 2)
	world.initLimbs()
	world.motion.limbs &^= LeftLeg
	world.limbs.objects = []LimbObject{{ID: "test-limb", Socket: SocketLeftLeg, Generation: world.limbs.generation, Airborne: true, Damaging: true, Position: Vector{X: 787, Y: 120}, Velocity: Vector{X: 600}}}
	serviceTick(t, world, PlayerCommand{})
	if !world.ServiceView(ctx).PaddleLatched {
		t.Fatalf("paddle not latched: %+v, events %+v", world.ServiceView(ctx), world.objects.events)
	}
	serviceTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Regenerate)}})
	for i := 1; i < 180; i++ {
		serviceTick(t, world, PlayerCommand{})
	}
	if len(world.LimbView(ctx).Objects) != 0 || !world.ServiceView(ctx).PaddleLatched {
		t.Fatal("regeneration did not retain the paddle evidence")
	}
	serviceHold(t, world, 970, nil)
	if world.ServiceView(ctx).Status != ServiceEvidenceReady {
		t.Fatalf("bypass not ready: %+v", world.ServiceView(ctx))
	}
	serviceHold(t, world, 1364, nil)
	if world.ServiceView(ctx).Status != ServicePending {
		t.Fatalf("utility hold: %+v", world.ServiceView(ctx))
	}
	proposal, err := world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
	if err != nil || proposal.Credits() != committed.Credits()+1 {
		t.Fatalf("utility candidate: %v", err)
	}

	owner, _, _, committed, world = activeService(t, 3)
	serviceHold(t, world, 394, nil)
	if !world.ServiceView(ctx).RecorderStarted {
		t.Fatalf("recorder: %+v", world.ServiceView(ctx))
	}
	world.combat.projectiles = []Projectile{{ID: "test-shot", Attacker: "player", Attack: "1", Position: Vector{X: 767, Y: 120}, Velocity: Vector{X: 600}, PlayerOwned: true}}
	serviceTick(t, world, PlayerCommand{})
	if world.ServiceView(ctx).Status != ServiceEvidenceReady {
		t.Fatalf("recorded impact: %+v, impacts %+v", world.ServiceView(ctx), world.feces.impacts)
	}
	serviceHold(t, world, 1364, nil)
	proposal, err = world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
	if err != nil || proposal.Credits() != committed.Credits()+1 {
		t.Fatalf("privacy candidate: %v", err)
	}
}

func TestRescueCrateLatchAndNamedAdult(t *testing.T) {
	ctx := context.Background()
	owner, _, _, committed, world := activeService(t, 1)
	for i := range world.objects.items {
		if world.objects.items[i].Kind == ObjectCrate {
			world.objects.items[i].Bounds.X = 408
		}
	}
	world.syncObjectPlatforms()
	serviceTick(t, world, PlayerCommand{})
	serviceHold(t, world, 700, nil)
	if !world.rescueGateLatched() {
		t.Fatalf("gate did not latch: %+v, events %+v", world.Objects(ctx), world.objects.events)
	}
	serviceHold(t, world, 752, nil)
	if !world.ServiceView(ctx).AdultFollowing {
		t.Fatalf("adult not following: %+v", world.ServiceView(ctx))
	}
	for tick := 0; tick < 900 && !world.ServiceView(ctx).AdultInRefuge; tick++ {
		if world.ServiceView(ctx).AdultWaiting {
			for _, civilian := range world.combat.civilians {
				if civilian.ID == string(world.service.roles[content.RoleActor]) {
					serviceHold(t, world, civilian.Bounds.X-30, nil)
					break
				}
			}
		} else {
			serviceTick(t, world, PlayerCommand{})
		}
	}
	if world.ServiceView(ctx).Status != ServiceEvidenceReady {
		t.Fatalf("adult did not reach refuge: %+v", world.ServiceView(ctx))
	}
	serviceHold(t, world, 1364, nil)
	proposal, err := world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
	if err != nil || proposal.Credits() != committed.Credits()+1 {
		t.Fatalf("rescue candidate: %v, view %+v", err, world.ServiceView(ctx))
	}
}

func TestHostilityDoesNotBlockValidTerminal(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world := activeService(t, 3)
	serviceHold(t, world, 394, nil)
	world.combat.projectiles = []Projectile{{ID: "test-shot", Attacker: "player", Attack: "1", Position: Vector{X: 767, Y: 120}, Velocity: Vector{X: 600}, PlayerOwned: true}}
	serviceTick(t, world, PlayerCommand{})
	if world.ServiceView(ctx).Status != ServiceEvidenceReady {
		t.Fatal("camera evidence was not collected")
	}
	world.combat.enemies[0].Body.Bounds.X = 1300
	world.combat.enemies[0].Phase = EnemyTelegraph
	world.combat.enemies[0].Until = world.state.Tick + 100
	serviceHold(t, world, 1364, nil)
	if world.ServiceView(ctx).Status != ServicePending {
		t.Fatalf("hostile enemy blocked valid evidence: %+v", world.ServiceView(ctx))
	}
}

func TestPrivacyExhaustionAndRenewal(t *testing.T) {
	ctx := context.Background()
	owner, _, _, committed, world := activeService(t, 3)
	for i := range world.combat.enemies {
		world.combat.enemies[i].Body.Bounds.X = 242
	}
	world.state.Player.Bounds.X = 300
	for i := 0; i < 5; i++ {
		serviceTick(t, world, PlayerCommand{Aim: Vector{X: -1}, Edges: []PlayerEdge{release(settings.Throw), press(settings.Throw)}})
		for world.state.Tick < world.motion.actionUntil {
			serviceTick(t, world, PlayerCommand{})
		}
	}
	if world.state.Feces != 0 {
		t.Fatalf("did not exhaust five portions: %d", world.state.Feces)
	}
	serviceTick(t, world, PlayerCommand{Edges: []PlayerEdge{release(settings.Throw), press(settings.Throw)}})
	if world.FecesView(ctx).Invalid != "refill_at_marked_station" || world.service.camera {
		t.Fatal("empty throw did not report its prerequisite")
	}
	world.state.Player.Bounds.X = 210
	world.state.Player.Velocity = Vector{}
	world.state.Player.Support = "boundary-floor"
	serviceTick(t, world, PlayerCommand{Edges: []PlayerEdge{press(settings.Interact)}})
	for tick := 1; tick < 180; tick++ {
		serviceTick(t, world, PlayerCommand{})
	}
	if world.state.Feces != 5 {
		t.Fatalf("refill did not renew the supply: %+v", world.FecesView(ctx))
	}
	// Release the failed throw before the subsequent genuine camera shot.
	serviceTick(t, world, PlayerCommand{Edges: []PlayerEdge{release(settings.Throw)}})
	completeServiceActions(t, world)
	proposal, err := world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
	if err != nil || proposal.Credits() != committed.Credits()+1 {
		t.Fatalf("renewed resource did not complete the service: %v", err)
	}
}

func TestRescueWaitsForHazardClearance(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, active, _ := activeService(t, 1)
	active, err := owner.Abandon(ctx, active)
	if err != nil {
		t.Fatal(err)
	}
	active, err = owner.Accept(ctx, active, "D-HOS-RES-04")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, active, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range world.combat.enemies {
		world.combat.enemies[i].Body.Bounds.X = 242
	}
	for i := range world.objects.items {
		if world.objects.items[i].Kind == ObjectCrate {
			world.objects.items[i].Bounds.X = 408
		}
	}
	world.syncObjectPlatforms()
	serviceTick(t, world, PlayerCommand{})
	serviceHold(t, world, 700, nil)
	serviceHold(t, world, 752, nil)
	hazards := []bool{false, true}
	for tick := 0; tick < 500 && !world.service.waiting; tick++ {
		serviceHazardTick(t, world, PlayerCommand{}, hazards)
	}
	if !world.service.waiting || world.service.status != ServiceActive || world.service.waitIndex != 1 {
		t.Fatalf("adult did not wait safely: %+v", world.ServiceView(ctx))
	}
	adult := world.combat.civilians[0].Bounds
	world.state.Player.Bounds.X = adult.X - 30
	world.state.Player.Velocity = Vector{}
	world.state.Player.Support = "boundary-floor"
	for tick := 0; tick < 36; tick++ {
		player := PlayerCommand{}
		if tick == 0 {
			player.Edges = []PlayerEdge{release(settings.Interact), press(settings.Interact)}
		}
		serviceHazardTick(t, world, player, hazards)
	}
	if !world.service.waiting || world.ServiceView(ctx).Invalid != "wait_for_hazard" || world.combat.civilians[0].Bounds != adult {
		t.Fatalf("active hazard did not hold the adult: %+v", world.ServiceView(ctx))
	}
	hazards[1] = false
	for tick := 0; tick < 36; tick++ {
		player := PlayerCommand{}
		if tick == 0 {
			player.Edges = []PlayerEdge{release(settings.Interact), press(settings.Interact)}
		}
		serviceHazardTick(t, world, player, hazards)
	}
	if world.service.waiting || world.combat.civilians[0].Bounds.X <= adult.X {
		t.Fatalf("safe release did not resume following: %+v", world.ServiceView(ctx))
	}
}

func TestWrongCameraDoesNotFailOrSupplyEvidence(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world := activeService(t, 3)
	serviceHold(t, world, 394, nil)
	world.combat.projectiles = []Projectile{{ID: "wrong-shot", Attacker: "player", Attack: "2", Position: Vector{X: 500, Y: 120}, Velocity: Vector{X: 600}, PlayerOwned: true}}
	wrong := FecesTarget{Collider: Collider{ID: "other-camera", Kind: ProjectileTarget, Bounds: Rect{X: 502, Y: 88, Width: 10, Height: 64}}, Kind: FecesReceptor, Ticket: "D-HOS-PRV-02"}
	command := ServiceCommand{Objects: ObjectCommand{Feces: FecesCommand{Targets: []FecesTarget{wrong}, Limb: LimbCommand{Combat: CombatCommand{Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}}}}}}
	if err := world.StepServices(ctx, command); err != nil {
		t.Fatal(err)
	}
	view := world.ServiceView(ctx)
	if view.Invalid != "invalid_camera_target" || view.Status != ServiceActive || view.CameraInterrupted {
		t.Fatalf("wrong camera: %+v", view)
	}
}

func TestServiceLethalInterruptionBeforeHold(t *testing.T) {
	ctx := context.Background()
	owner, _, _, committed, world := activeService(t, 0)
	for i := range world.objects.items {
		if world.objects.items[i].Kind == ObjectTrolley {
			world.objects.items[i].Bounds.X = 1288
		}
	}
	world.syncObjectPlatforms()
	world.state.Player.Bounds.X = 1364
	for tick := 1; tick <= 35; tick++ {
		player := PlayerCommand{}
		if tick == 1 {
			player.Edges = []PlayerEdge{press(settings.Interact)}
		}
		if tick == 35 {
			serviceTick(t, world, player, DamageHit{Attacker: "hazard", Attack: "one", Kind: LethalDamage, Amount: 100, Direction: 1})
		} else {
			serviceTick(t, world, player)
		}
	}
	if world.service.pending != nil || committed.Credits() != 0 {
		t.Fatal("unfinished hold granted evidence")
	}
	if _, err := world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1}); err == nil {
		t.Fatal("unfinished hold prepared a certification")
	}
}

func TestSupplySealAndRestoredAttemptIdentity(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world := activeService(t, 0)
	for i := range world.objects.items {
		if world.objects.items[i].Kind == ObjectTrolley {
			world.objects.items[i].Bounds.X = 735
		}
	}
	world.syncObjectPlatforms()
	serviceTick(t, world, PlayerCommand{})
	if world.ServiceView(ctx).Status != ServiceActive {
		t.Fatal("ordinary scenery contact broke the seal")
	}
	charged, err := owner.Prepare(ctx, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1, Contacts: []campaign.Contact{{Kind: campaign.IncidentWaste, Detail: "reporting station"}}})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := NewWorld(ctx, owner, manifest, charged, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ServiceView(ctx).Attempt != 1 || !resumed.feces.reported || len(charged.Data(ctx).Incidents) != 1 {
		t.Fatal("checkpoint restoration changed attempt or charge identity")
	}
	for _, object := range resumed.Objects(ctx) {
		if object.Kind == ObjectTrolley && object.Bounds != object.Origin {
			t.Fatal("checkpoint did not restore trolley")
		}
	}
	reset, err := owner.Prepare(ctx, charged, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 2, Attempt: &campaign.AttemptChange{Action: campaign.AttemptReset, Deed: "D-HOS-SUP-01"}})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err = NewWorld(ctx, owner, manifest, reset, bays, nil)
	if err != nil || resumed.ServiceView(ctx).Attempt != 2 || resumed.feces.reported || len(reset.Data(ctx).Incidents) != 1 {
		t.Fatalf("explicit reset: %v", err)
	}
}

func TestRescueSweepStopsAtSolidObject(t *testing.T) {
	world := combatWorld(t)
	addObject(t, world, "wall", ObjectBreakable, Rect{X: 40, Width: 10, Height: 58})
	if got := world.rescueStep(Rect{X: 0, Width: 28, Height: 58}, 30); got != 12 {
		t.Fatalf("rescue crossed a solid object: %v", got)
	}
}

func TestFailureAndResetForRemainingFamilies(t *testing.T) {
	ctx := context.Background()
	for _, family := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(family), func(t *testing.T) {
			owner, manifest, bays, committed, world := activeService(t, family)
			switch family {
			case 1:
				command := ServiceCommand{Hazards: []bool{true}, Objects: ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}}}}}}
				if err := world.StepServices(ctx, command); err != nil {
					t.Fatal(err)
				}
			case 2:
				serviceHold(t, world, 970, nil)
			case 3:
				world.combat.projectiles = []Projectile{{ID: "test-shot", Attacker: "player", Attack: "1", Position: Vector{X: 767, Y: 120}, Velocity: Vector{X: 600}, PlayerOwned: true}}
				serviceTick(t, world, PlayerCommand{})
			}
			view := world.ServiceView(ctx)
			if view.Status != ServiceFailed || view.Failure == "" {
				t.Fatalf("failure: %+v", view)
			}
			failed, err := world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1})
			if err != nil || failed.Credits() != committed.Credits() {
				t.Fatalf("prepare failure: %v", err)
			}
			failureID := failed.Data(ctx).Incidents[0].ID
			if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(failed)); err != nil {
				t.Fatal(err)
			}
			serviceHold(t, world, 160, nil)
			if _, err = world.RequestService(ctx, owner, manifest, failed, campaign.AttemptReset, view.Deed); err != nil {
				t.Fatal(err)
			}
			reset, err := world.PrepareService(ctx, owner, failed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 2})
			if err != nil {
				t.Fatal(err)
			}
			if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(reset)); err != nil {
				t.Fatal(err)
			}
			restored := world
			again := restored.ServiceView(ctx)
			if again.Deed != view.Deed || again.Attempt != 2 || again.Status != ServiceActive || again.RecorderStarted || again.CameraInterrupted || again.PaddleLatched {
				t.Fatalf("reset: %+v", again)
			}
			if family == 1 && (len(restored.combat.civilians) != 1 || restored.combat.civilians[0].Incapacitated) {
				t.Fatal("named adult was not restored")
			}
			completeServiceActions(t, restored)
			certified, err := restored.PrepareService(ctx, owner, reset, campaign.Transition{CampaignID: serviceCampaignID, Sequence: 3})
			if err != nil || certified.Credits() != committed.Credits()+1 || len(certified.Data(ctx).Incidents) != 1 || certified.Data(ctx).Incidents[0].ID != failureID {
				t.Fatalf("completion after reset: %v", err)
			}
			for i, receipt := range committed.Data(ctx).Receipts {
				if certified.Data(ctx).Receipts[i].Certification.Deed != receipt.Certification.Deed {
					t.Fatal("reset lost a sibling receipt")
				}
			}
		})
	}
}

func TestRescueRequiresCrateOrLimb(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world := activeService(t, 1)
	serviceHold(t, world, 700, nil)
	if world.rescueGateLatched() {
		t.Fatal("unweighted gate latched")
	}
	world.initLimbs()
	world.limbs.objects = []LimbObject{{ID: "weight", Socket: SocketLeftLeg, Generation: world.limbs.generation, Position: Vector{X: 432, Y: 1}}}
	serviceTick(t, world, PlayerCommand{})
	serviceHold(t, world, 700, nil)
	if !world.rescueGateLatched() {
		t.Fatalf("settled limb did not permit gate latch: %+v", world.InteractionView(ctx))
	}
	world.limbs.objects = nil
	world.limbs.generation++
	serviceTick(t, world, PlayerCommand{})
	if !world.rescueGateLatched() {
		t.Fatal("losing the limb closed a latched gate")
	}
}

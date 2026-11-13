// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"slices"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

func encounterTick(t *testing.T, world *World, player PlayerCommand) {
	t.Helper()
	command := ServiceCommand{Objects: ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: player, Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}}}}}}
	if err := world.StepEncounter(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func TestAuthoredHazardPhases(t *testing.T) {
	ctx := context.Background()
	for family := 0; family < 4; family++ {
		_, manifest, bays, committed, _ := activeService(t, family)
		for variant := 1; variant <= 6; variant++ {
			var bayIndex int
			for i, bay := range bays {
				if bay.Deed == manifest.Batches(ctx)[family].Deeds[variant-1] {
					bayIndex = i
					break
				}
			}
			bay := bays[bayIndex]
			world := &World{state: State{Location: bay.Institution.Location}, service: serviceState{status: ServiceActive}}
			world.encounter.hazards = bay.Hazards
			world.encounter.room = bay.Institution.Bays[family]
			if len(world.Hazards(ctx)) != len(bay.Variant.Stations) || world.Hazards(ctx)[0].Phase != HazardDormant {
				t.Fatalf("%s: bad dormant stations", bay.Deed)
			}
			world.encounter.started = true
			for i, hazard := range bay.Hazards {
				for _, check := range []struct {
					at    int
					phase HazardPhase
				}{
					{msTicks(hazard.OffsetMS), HazardWarning},
					{msTicks(hazard.OffsetMS) + msTicks(hazard.WarningMS) - 1, HazardWarning},
					{msTicks(hazard.OffsetMS) + msTicks(hazard.WarningMS), HazardActive},
					{msTicks(hazard.OffsetMS) + msTicks(hazard.WarningMS) + msTicks(hazard.ActiveMS), HazardSafe},
					{msTicks(hazard.OffsetMS) + msTicks(hazard.WarningMS) + msTicks(hazard.ActiveMS) + msTicks(hazard.InactiveMS), HazardWarning},
				} {
					world.encounter.elapsed = check.at
					view := world.Hazards(ctx)[i]
					if view.Phase != check.phase || view.Waiting.Width < 160 || (family == 0 && i == 0) != view.Lethal {
						t.Fatalf("%s station %d at %d: %+v", bay.Deed, i, check.at, view)
					}
				}
			}
			if committed.Credits() != family*6 {
				t.Fatal("fixture credit changed")
			}
		}
	}
}

func TestRejectInvalidHazardTiming(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	bays[0].Hazards = slices.Clone(bays[0].Hazards)
	bays[0].Hazards[0].WarningMS = 0
	if _, err := NewWorld(ctx, owner, manifest, campaign.Snapshot{}, bays, nil); err == nil {
		t.Fatal("accepted a zero-duration hazard")
	}
}

func TestAllInstitutionHazardKinds(t *testing.T) {
	ctx := context.Background()
	_, _, bays := campaignFixture(t)
	for _, bay := range bays {
		world := &World{state: State{Location: bay.Institution.Location}, service: serviceState{status: ServiceActive}}
		world.encounter.hazards = bay.Hazards
		world.encounter.room.Family = bay.Template.ID[4:]
		kind := map[content.LocationID]string{"LOC-HOS": "steam", "LOC-CHU": "censer", "LOC-MIN": "waste", "LOC-INS": "cable"}[bay.Institution.Location]
		if world.encounter.room.Family == "UTL" {
			kind = "electrical_arc"
		}
		for i, station := range world.Hazards(ctx) {
			want := kind
			if world.encounter.room.Family == "SUP" && i == 0 {
				want = "crusher"
			}
			if station.Kind != want {
				t.Fatalf("%s: got %q want %q", bay.Deed, station.Kind, want)
			}
		}
	}
}

func TestCrusherDamageAndControl(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world := activeService(t, 0)
	world.state.Player.Bounds.X = 260
	encounterTick(t, world, PlayerCommand{})
	for i := 0; i < 144; i++ {
		encounterTick(t, world, PlayerCommand{})
	}
	if world.Hazards(ctx)[0].Phase != HazardActive {
		t.Fatalf("crusher failed to activate: %+v", world.Hazards(ctx))
	}
	world.state.Player.Bounds.X = 730
	world.motion.immune = true
	encounterTick(t, world, PlayerCommand{})
	if !world.motion.dead || world.state.Health != 0 {
		t.Fatal("crusher did not bypass ordinary immunity")
	}
	_, _, _, _, world = activeService(t, 0)
	world.encounter.started, world.encounter.elapsed = true, 144
	world.state.Player.Bounds = Rect{X: 730, Y: 80, Width: PlayerWidth, Height: PlayerHeight}
	world.state.Player.Support = "terrain-CATWALK"
	encounterTick(t, world, PlayerCommand{})
	if world.motion.dead {
		t.Fatal("crusher harmed player on the safe catwalk")
	}
	world.state.Player.Bounds = Rect{X: 656, Y: 0, Width: PlayerWidth, Height: PlayerHeight}
	world.state.Player.Support = "boundary-floor"
	if !world.Hazards(ctx)[0].HeldOpen || world.Hazards(ctx)[0].Phase != HazardSafe {
		t.Fatal("control plate did not hold crusher open")
	}
	encounterTick(t, world, PlayerCommand{})
	world.state.Player.Bounds.X = 260
	if world.Hazards(ctx)[0].Phase != HazardWarning {
		t.Fatal("releasing the control did not restart the warning")
	}
	_, _, _, _, world = activeService(t, 0)
	world.encounter.started, world.encounter.elapsed = true, 144
	world.state.Player.Bounds.X = 260
	for i := range world.objects.items {
		if world.objects.items[i].Kind == ObjectTrolley {
			world.objects.items[i].Bounds.X = 730
		}
	}
	encounterTick(t, world, PlayerCommand{})
	if !world.service.sealBroken || world.service.status != ServiceFailed {
		t.Fatalf("crusher did not break trolley seal: %+v", world.ServiceView(ctx))
	}
}

func TestRescueAdultWaitsClearAndHazardFailsAttempt(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, world := activeService(t, 1)
	world.encounter.started, world.encounter.elapsed = true, 108
	world.state.Player.Bounds.X = 260
	encounterTick(t, world, PlayerCommand{})
	if len(world.combat.civilians) != 1 || overlaps(world.combat.civilians[0].Bounds, world.Hazards(ctx)[0].Bounds) {
		t.Fatal("authored waiting adult overlaps the marked station")
	}
	if world.ServiceView(ctx).Status == ServiceFailed {
		t.Fatal("safe waiting adult was struck")
	}
	world.combat.civilians[0].Bounds.X = world.Hazards(ctx)[0].Bounds.X + 1
	encounterTick(t, world, PlayerCommand{})
	if world.ServiceView(ctx).Status != ServiceFailed || !world.combat.civilians[0].Incapacitated {
		t.Fatal("hazard contact did not fail the rescue attempt")
	}
}

func TestEncounterSameTickEvidenceAndDeath(t *testing.T) {
	ctx := context.Background()
	for _, lethalTick := range []int{35, 36} {
		owner, _, _, committed, world := activeService(t, 0)
		for i := range world.objects.items {
			if world.objects.items[i].Kind == ObjectTrolley {
				world.objects.items[i].Bounds.X = 1288
			}
		}
		world.syncObjectPlatforms()
		world.state.Player.Bounds.X = 1364
		world.state.Player.Support = "boundary-floor"
		for tick := 1; tick <= lethalTick; tick++ {
			player := PlayerCommand{}
			if tick == 1 {
				player.Edges = []PlayerEdge{press(settings.Interact)}
			}
			command := ServiceCommand{Objects: ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Player: player, Visible: Rect{X: -500, Y: -20, Width: 100, Height: 100}}}}}}
			if tick == lethalTick {
				command.Objects.Feces.Limb.Combat.Hits = []DamageHit{{Attacker: "fixture", Attack: "lethal", Kind: LethalDamage, Amount: 100, Direction: 1}}
			}
			if err := world.StepEncounter(ctx, command); err != nil {
				t.Fatal(err)
			}
		}
		if !world.motion.dead || (world.service.pending != nil) != (lethalTick == 36) {
			t.Fatalf("completion/death ordering at %d: %+v", lethalTick, world.ServiceView(ctx))
		}
		if lethalTick == 36 {
			transition := campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1, Death: campaign.DeathGameplay, DeathDetail: "fixture"}
			proposal, err := world.PrepareService(ctx, owner, committed, transition)
			if err != nil || proposal.Credits() != 1 || proposal.Data(ctx).DebtCents != 4999 || committed.Credits() != 0 {
				t.Fatalf("atomic completion and death: %v", err)
			}
		}
	}
}

func TestSixthReceiptStartsOnlyAfterAcknowledgment(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	committed, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	batch := manifest.Batches(ctx)[0]
	for i := 1; i < 6; i++ {
		committed = certifyFixture(t, owner, manifest, committed, batch.Deeds[i])
	}
	committed, err = owner.Accept(ctx, committed, batch.Deeds[0])
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	completeServiceActions(t, world)
	proposal, err := world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: committed.Data(ctx).OperationSequence + 1})
	if err != nil {
		t.Fatal(err)
	}
	if world.EmergencyView(ctx).ID != "" || committed.Credits() != 5 || proposal.Credits() != 6 {
		t.Fatal("unacknowledged sixth receipt started an emergency")
	}
	failed := serviceAck(proposal)
	failed.Succeeded = false
	if world.AcknowledgeService(ctx, owner, manifest, bays, failed) == nil || world.EmergencyView(ctx).ID != "" {
		t.Fatal("failed save started emergency")
	}
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	if view := world.EmergencyView(ctx); view.ID != batch.Emergency.ID || view.WarningTicks != 180 {
		t.Fatalf("missing acknowledged emergency: %+v", view)
	}
	encounterTick(t, world, PlayerCommand{})
	if world.EmergencyView(ctx).WarningTicks != 179 {
		t.Fatal("warning did not advance on the resumed tick")
	}
	restored, err := NewWorld(ctx, owner, manifest, proposal, bays, nil)
	if err != nil || restored.EmergencyView(ctx).WarningTicks != 180 || restored.state.Player.Bounds.X != 82 {
		t.Fatalf("unsafe restoration: %v", err)
	}
}

func TestEmergencyDeathRestoresWarningAndReceipts(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	committed, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range manifest.Batches(ctx)[0].Deeds {
		committed = certifyFixture(t, owner, manifest, committed, id)
	}
	world, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 180; i++ {
		encounterTick(t, world, PlayerCommand{})
	}
	world.state.Player.Bounds.X = world.encounter.room.Space.Envelope.X + 1
	world.state.Health = 15
	encounterTick(t, world, PlayerCommand{})
	if !world.motion.dead {
		t.Fatal("emergency failed to cause temporary death")
	}
	transition := campaign.Transition{CampaignID: serviceCampaignID, Sequence: 1, Death: campaign.DeathGameplay, DeathDetail: "steam"}
	proposal, err := owner.Prepare(ctx, committed, transition)
	if err != nil || proposal.Credits() != 6 || proposal.Data(ctx).DebtCents != 4999 {
		t.Fatalf("death lost emergency or receipt: %v", err)
	}
	for i := 0; i < 2; i++ {
		restored, restoreErr := NewWorld(ctx, owner, manifest, proposal, bays, nil)
		if restoreErr != nil || restored.EmergencyView(ctx).WarningTicks != 180 || restored.state.Player.Bounds.X != 82 || !restored.EmergencyView(ctx).BypassUsable || proposal.Data(ctx).DebtCents != 4999 {
			t.Fatalf("unsafe retry: %v", restoreErr)
		}
	}
}

func TestEmergencyBypassAndCleanup(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	committed, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range manifest.Batches(ctx)[0].Deeds {
		committed = certifyFixture(t, owner, manifest, committed, id)
	}
	world, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	world.state.Player.Bounds.X = 1452
	encounterTick(t, world, PlayerCommand{})
	view := world.EmergencyView(ctx)
	if view.Candidate != view.ID || view.Stage != campaign.EmergencyActive || committed.Credits() != 6 {
		t.Fatalf("bypass did not propose aftermath: %+v", view)
	}
	if err = world.StepEncounter(ctx, ServiceCommand{}); err == nil {
		t.Fatal("pending aftermath permitted another tick")
	}
	world, err = NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	world.state.Player.Bounds.X = world.encounter.room.Space.Refuge.X + 1
	for tick := 0; tick < 36; tick++ {
		player := PlayerCommand{}
		if tick == 0 {
			player.Edges = []PlayerEdge{press(settings.Interact)}
		}
		encounterTick(t, world, player)
	}
	if view = world.EmergencyView(ctx); view.Candidate != view.ID || committed.Credits() != 6 {
		t.Fatalf("cleanup did not propose the same aftermath: %+v", view)
	}
}

func TestSixteenthEmergencyRestorationAndTiming(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	committed, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for index, batch := range manifest.Batches(ctx) {
		for n := 5; n >= 0; n-- {
			committed = certifyFixture(t, owner, manifest, committed, batch.Deeds[n])
		}
		world, buildErr := NewWorld(ctx, owner, manifest, committed, bays, nil)
		if buildErr != nil {
			t.Fatalf("%s: %v", batch.Emergency.ID, buildErr)
		}
		view := world.EmergencyView(ctx)
		if view.ID != batch.Emergency.ID || view.WarningTicks != 180 || view.Stage != campaign.EmergencyActive || !view.BypassUsable || view.Warning != batch.Hazard || view.Aftermath != batch.Recovery || view.RefugeName == "" {
			t.Fatalf("%s restoration: %+v", batch.Emergency.ID, view)
		}
		for i := 0; i < 180; i++ {
			encounterTick(t, world, PlayerCommand{})
		}
		if view = world.EmergencyView(ctx); view.WarningTicks != 0 || view.ActiveTicks != 0 || view.Candidate != "" {
			t.Fatalf("warning boundary: %+v", view)
		}
		area := world.encounter.room.Space.Envelope
		if world.encounter.room.Space.Kind == "wave" {
			area = world.encounter.room.Space.Start
		}
		world.state.Player.Bounds.X = area.X + 1
		world.state.Player.Bounds.Y = 0
		world.state.Player.Support = "boundary-floor"
		encounterTick(t, world, PlayerCommand{})
		if view = world.EmergencyView(ctx); view.ActiveTicks != 1 || view.Candidate != "" {
			t.Fatalf("active boundary: %+v", view)
		}
		wantHealth := 85
		if world.encounter.room.Space.Kind == "fall" {
			wantHealth = 65
		}
		if world.state.Health != wantHealth {
			t.Fatalf("%s damage: %d, want %d", batch.Emergency.ID, world.state.Health, wantHealth)
		}
		deathWorld, buildErr := NewWorld(ctx, owner, manifest, committed, bays, nil)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		for i := 0; i < 180; i++ {
			encounterTick(t, deathWorld, PlayerCommand{})
		}
		deathWorld.state.Player.Bounds.X = area.X + 1
		deathWorld.state.Health = 15
		encounterTick(t, deathWorld, PlayerCommand{})
		if !deathWorld.motion.dead {
			t.Fatalf("%s could not cause death", batch.Emergency.ID)
		}
		death, deathErr := owner.Prepare(ctx, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: committed.Data(ctx).OperationSequence + 1, Death: campaign.DeathGameplay, DeathDetail: string(batch.Emergency.ID)})
		if deathErr != nil || death.Credits() != committed.Credits() || death.Data(ctx).DebtCents != 4999 {
			t.Fatalf("%s death proposal: %v", batch.Emergency.ID, deathErr)
		}
		retry, retryErr := NewWorld(ctx, owner, manifest, death, bays, nil)
		if retryErr != nil || retry.EmergencyView(ctx).ID != batch.Emergency.ID || retry.EmergencyView(ctx).WarningTicks != 180 || retry.state.Player.Bounds.X != 82 || !retry.EmergencyView(ctx).BypassUsable {
			t.Fatalf("%s unsafe death retry: %v", batch.Emergency.ID, retryErr)
		}
		world.state.Player.Bounds.X = 82
		world.state.Player.Bounds.Y = 0
		world.state.Player.Support = "boundary-floor"
		world.state.Player.Velocity = Vector{}
		for i := 1; i < 960; i++ {
			encounterTick(t, world, PlayerCommand{})
		}
		view = world.EmergencyView(ctx)
		if view.Candidate != view.ID || view.EventID != campaign.EmergencyTransitionID(view.ID) {
			t.Fatalf("missing aftermath candidate: %+v", view)
		}
		proposal, prepareErr := world.PrepareEmergency(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: committed.Data(ctx).OperationSequence + 1})
		if prepareErr != nil || proposal.Credits() != committed.Credits() || world.EmergencyView(ctx).Stage != campaign.EmergencyActive {
			t.Fatalf("aftermath preparation: %v", prepareErr)
		}
		failed := serviceAck(proposal)
		failed.Succeeded = false
		if world.AcknowledgeEmergency(ctx, owner, failed) == nil {
			t.Fatal("accepted failed save")
		}
		if err = world.AcknowledgeEmergency(ctx, owner, serviceAck(proposal)); err != nil {
			t.Fatal(err)
		}
		committed = proposal
		if world.EmergencyView(ctx).Stage != campaign.EmergencyAftermath {
			t.Fatal("acknowledgment did not publish aftermath")
		}
		found := false
		for _, collider := range world.grid.colliders {
			found = found || collider.ID == "aftermath-"+string(view.ID)
		}
		if found != world.encounter.room.AftermathBlocking {
			t.Fatal("acknowledged aftermath has incorrect blocking geometry")
		}
		if index%4 == 3 && index < 12 {
			deeds := manifest.Deeds(ctx)
			closure := deeds[(index/4)*25+24]
			committed, err = owner.DefeatBoss(ctx, committed, closure.Deed.Encounter)
			if err != nil {
				t.Fatal(err)
			}
			committed = certifyFixture(t, owner, manifest, committed, closure.Deed.ID)
		}
	}
	if committed.Credits() != 99 || len(committed.Data(ctx).Emergencies) != 16 {
		t.Fatalf("incorrect totals: %d", committed.Credits())
	}
}

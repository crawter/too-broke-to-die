// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
)

func instituteFixture(t *testing.T) (*campaign.Campaign, *content.Campaign, []content.ServiceBay, campaign.Snapshot, *World) {
	t.Helper()
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	snapshot, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for institution := 0; institution < 3; institution++ {
		for _, batch := range manifest.Batches(ctx)[institution*4 : institution*4+4] {
			for _, id := range batch.Deeds {
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
	snapshot, err = owner.Accept(ctx, snapshot, "D-INS-SUP-01")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = owner.Checkpoint(ctx, snapshot, "ANC-INS-01")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, snapshot, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	return owner, manifest, bays, snapshot, world
}

func TestFeedDefeatWarningAndReplacement(t *testing.T) {
	ctx := context.Background()
	_, _, _, committed, world := instituteFixture(t)
	if view := world.Recruitment(ctx); view.Health != 44 || view.Live != 1 || view.Ticket != "D-INS-SUP-01" {
		t.Fatalf("initial feed: %+v", view)
	}
	world.state.Player.Bounds.X = 500
	world.combat.enemies[0].Body.Bounds.X = 550
	world.combat.enemies[0].Health = 1
	encounterTick(t, world, PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Melee)}})
	for i := 0; i < 16; i++ {
		encounterTick(t, world, PlayerCommand{})
	}
	view := world.Recruitment(ctx)
	if view.Live != 0 || len(view.Queue) != 1 || view.Queue[0].Remaining > 540 || view.Queue[0].Warning {
		t.Fatalf("kill did not queue replacement: %+v", view)
	}
	world.state.Player.Bounds.X = 82
	world.state.Player.Velocity = Vector{}
	world.combat.projectiles = append(world.combat.projectiles, Projectile{ID: "stop-feed", Attacker: "player", Attack: "stop-feed", Position: Vector{X: 1164, Y: 30}, Velocity: Vector{X: 600}, PlayerOwned: true})
	encounterTick(t, world, PlayerCommand{})
	remaining := world.Recruitment(ctx).Queue[0].Remaining
	for i := 0; i < 240; i++ {
		encounterTick(t, world, PlayerCommand{})
	}
	if world.Recruitment(ctx).Queue[0].Remaining != remaining {
		t.Fatal("feces did not pause the replacement timer for two seconds")
	}
	for world.Recruitment(ctx).Queue[0].Remaining > 181 {
		encounterTick(t, world, PlayerCommand{})
	}
	if world.Recruitment(ctx).Queue[0].Warning {
		t.Fatal("replacement warning started before delay")
	}
	encounterTick(t, world, PlayerCommand{})
	if !world.Recruitment(ctx).Queue[0].Warning {
		t.Fatal("replacement warning did not begin")
	}
	for i := 0; i < 180; i++ {
		encounterTick(t, world, PlayerCommand{})
	}
	view = world.Recruitment(ctx)
	if view.Live != 1 || len(view.Queue) != 0 || committed.Credits() != 75 {
		t.Fatalf("replacement or credit: %+v credits=%d", view, committed.Credits())
	}
}

func TestReplacementRespectsLiveFollowerCap(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, _ := instituteFixture(t)
	committed, err := owner.Abandon(ctx, committed)
	if err != nil {
		t.Fatal(err)
	}
	committed, err = owner.Accept(ctx, committed, "D-INS-SUP-03")
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	if world.Recruitment(ctx).Live != 2 {
		t.Fatal("variant 03 did not start with two followers")
	}
	world.state.Player.Bounds.X = 500
	world.combat.enemies[0].Body.Bounds.X = 550
	world.combat.enemies[0].Health = 1
	encounterTick(t, world, PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Melee)}})
	for i := 0; i < 16; i++ {
		encounterTick(t, world, PlayerCommand{})
	}
	if err = world.AddEnemy(ctx, "fixture-follower", FollowerEnemy, Rect{X: 1190, Width: PlayerWidth, Height: PlayerHeight}); err != nil {
		t.Fatal(err)
	}
	world.state.Player.Bounds.X = 82
	world.state.Player.Velocity = Vector{}
	for i := 0; i < 600; i++ {
		encounterTick(t, world, PlayerCommand{})
	}
	view := world.Recruitment(ctx)
	if view.Live != 2 || len(view.Queue) != 1 || !view.Queue[0].Warning || view.Queue[0].Remaining != 0 || committed.Credits() != 75 {
		t.Fatalf("cap or queued warning violated: %+v", view)
	}
}

func TestSourceDestructionCancelsDueReplacement(t *testing.T) {
	ctx := context.Background()
	_, _, _, committed, world := instituteFixture(t)
	world.encounter.feed.health = 35
	world.encounter.feed.queue = []RecruitmentReplacement{{ID: "queued-follower", Remaining: 1, Warning: true}}
	world.initLimbs()
	world.limbs.objects = append(world.limbs.objects, LimbObject{ID: "limb", Socket: SocketLeftLeg, Generation: world.limbs.generation, Attack: 1, Airborne: true, Damaging: true, Position: Vector{X: 1164, Y: 30}, Velocity: Vector{X: 600}, FlightTick: world.state.Tick})
	encounterTick(t, world, PlayerCommand{})
	view := world.Recruitment(ctx)
	if view.Health != 0 || view.Live != 1 || len(view.Queue) != 0 || committed.Credits() != 75 {
		t.Fatalf("due replacement outlived destroyed source: %+v", view)
	}
}

func TestFeedMeleeAndCertifiedRevisit(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world := instituteFixture(t)
	world.state.Player.Bounds.X = 1125
	world.state.Player.Support = "boundary-floor"
	encounterTick(t, world, PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{press(settings.Melee)}})
	for i := 0; i < 15; i++ {
		encounterTick(t, world, PlayerCommand{})
	}
	if got := world.Recruitment(ctx).Health; got != 22 {
		t.Fatalf("melee source damage: %d", got)
	}
	// Start again from the committed attempt, then complete the genuine SUP
	// evidence and acknowledge it before checking the disconnected revisit.
	world, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	completeServiceActions(t, world)
	proposal, err := world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: committed.Data(ctx).OperationSequence + 1})
	if err != nil {
		t.Fatal(err)
	}
	if world.Recruitment(ctx).Disconnected || proposal.Credits() != 76 {
		t.Fatal("unacknowledged certification disconnected the feed")
	}
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	if !world.Recruitment(ctx).Disconnected {
		t.Fatal("acknowledged receipt did not disconnect the feed")
	}
	revisit, err := NewWorld(ctx, owner, manifest, proposal, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	if view := revisit.Recruitment(ctx); view.Ticket != "D-INS-SUP-01" || !view.Disconnected || view.Live != 0 || len(view.Queue) != 0 {
		t.Fatalf("certified revisit: %+v", view)
	}
}

func TestEncounterDeterministicReplay(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, first := instituteFixture(t)
	second, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	for tick := 0; tick < 300; tick++ {
		player := PlayerCommand{}
		if tick < 120 {
			player.Move = 1
		} else if tick < 240 {
			player.Move = -1
		}
		encounterTick(t, first, player)
		encounterTick(t, second, player)
		_, a := first.Snapshot(ctx).Data(ctx)
		_, b := second.Snapshot(ctx).Data(ctx)
		if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(first.Hazards(ctx), second.Hazards(ctx)) || !reflect.DeepEqual(first.Recruitment(ctx), second.Recruitment(ctx)) || !reflect.DeepEqual(first.CombatView(ctx), second.CombatView(ctx)) {
			t.Fatalf("replay diverged at tick %d", tick)
		}
	}
}

func TestAllCertifiedInstituteTicketFeeds(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, _ := instituteFixture(t)
	committed, err := owner.Abandon(ctx, committed)
	if err != nil {
		t.Fatal(err)
	}
	for index, batch := range manifest.Batches(ctx)[12:] {
		for _, id := range batch.Deeds {
			committed = certifyFixture(t, owner, manifest, committed, id)
		}
		world, buildErr := NewWorld(ctx, owner, manifest, committed, bays, nil)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		for _, id := range batch.Deeds {
			view, viewErr := world.RecruitmentForTicket(ctx, id)
			if viewErr != nil || view.Ticket != id || view.ID != "feed-"+string(id) || !view.Disconnected || view.Live != 0 || len(view.Queue) != 0 {
				t.Fatalf("%s revisit: %+v %v", id, view, viewErr)
			}
		}
		if _, viewErr := world.RecruitmentForTicket(ctx, "D-HOS-SUP-01"); viewErr == nil {
			t.Fatal("other institution's feed was accepted")
		}
		if committed.Credits() != 75+(index+1)*6 {
			t.Fatal("revisiting feeds changed certified credit")
		}
	}
}

func TestFeedFecesCooldownDestructionAndReset(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world := instituteFixture(t)
	shot := func(id string) {
		world.combat.projectiles = append(world.combat.projectiles, Projectile{ID: id, Attacker: "player", Attack: id, Position: Vector{X: 1164, Y: 30}, Velocity: Vector{X: 600}, PlayerOwned: true})
		encounterTick(t, world, PlayerCommand{})
	}
	shot("one")
	first := world.Recruitment(ctx)
	if first.StoppedTicks != 240 || first.CooldownTicks != 480 {
		t.Fatalf("source stop: %+v", first)
	}
	shot("two")
	second := world.Recruitment(ctx)
	if second.StoppedTicks != 239 || second.CooldownTicks != 479 {
		t.Fatalf("feces bypassed cooldown: %+v", second)
	}
	world.initLimbs()
	world.limbs.objects = append(world.limbs.objects, LimbObject{ID: "arm", Socket: SocketLeftArm, Generation: world.limbs.generation, Attack: 21, Airborne: true, Damaging: true, Position: Vector{X: 1164, Y: 30}, Velocity: Vector{X: 650}, FlightTick: world.state.Tick})
	encounterTick(t, world, PlayerCommand{})
	if got := world.Recruitment(ctx).Health; got != 19 {
		t.Fatalf("arm damage: %d", got)
	}
	world.limbs.objects = append(world.limbs.objects, LimbObject{ID: "leg", Socket: SocketLeftLeg, Generation: world.limbs.generation, Attack: 22, Airborne: true, Damaging: true, Position: Vector{X: 1164, Y: 30}, Velocity: Vector{X: 600}, FlightTick: world.state.Tick})
	encounterTick(t, world, PlayerCommand{})
	if got := world.Recruitment(ctx); got.Health != 0 || len(got.Queue) != 0 {
		t.Fatalf("source survived: %+v", got)
	}
	restored, err := NewWorld(ctx, owner, manifest, committed, bays, nil)
	if err != nil || restored.Recruitment(ctx).Health != 44 || committed.Credits() != 75 {
		t.Fatalf("restored feed: %v", err)
	}
	serviceHold(t, world, 160, nil)
	reset := commitServiceChange(t, world, owner, manifest, bays, committed, campaign.AttemptReset, "D-INS-SUP-01", false)
	if view := world.Recruitment(ctx); view.Health != 44 || view.Disconnected || world.ServiceView(ctx).Attempt != 2 || reset.Credits() != 75 {
		t.Fatalf("explicit reset did not restore the attempt feed: %+v", view)
	}
}

func completedEmergencyWorld(t *testing.T, batchIndex int) (*campaign.Campaign, *content.Campaign, []content.ServiceBay, campaign.Snapshot, *World) {
	t.Helper()
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	committed, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for index, batch := range manifest.Batches(ctx) {
		for _, id := range batch.Deeds {
			committed = certifyFixture(t, owner, manifest, committed, id)
		}
		if index == batchIndex {
			world, buildErr := NewWorld(ctx, owner, manifest, committed, bays, nil)
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			return owner, manifest, bays, committed, world
		}
		if index%4 == 3 {
			closure := manifest.Deeds(ctx)[index/4*25+24]
			committed, err = owner.DefeatBoss(ctx, committed, closure.Deed.Encounter)
			if err != nil {
				t.Fatal(err)
			}
			committed = certifyFixture(t, owner, manifest, committed, closure.Deed.ID)
		}
	}
	t.Fatal("unknown batch index")
	return nil, nil, nil, campaign.Snapshot{}, nil
}

func acknowledgeEscape(t *testing.T, owner *campaign.Campaign, committed campaign.Snapshot, world *World) campaign.Snapshot {
	t.Helper()
	ctx := context.Background()
	world.state.Player.Bounds.X = 1452
	encounterTick(t, world, PlayerCommand{})
	if world.EmergencyView(ctx).Candidate == "" {
		t.Fatal("escape did not produce an aftermath candidate")
	}
	proposal, err := world.PrepareEmergency(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: committed.Data(ctx).OperationSequence + 1})
	if err != nil {
		t.Fatal(err)
	}
	failed := serviceAck(proposal)
	failed.Succeeded = false
	if world.AcknowledgeEmergency(ctx, owner, failed) == nil || world.EmergencyView(ctx).Stage != campaign.EmergencyActive {
		t.Fatal("failed acknowledgment published aftermath")
	}
	if err = world.AcknowledgeEmergency(ctx, owner, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	return proposal
}

func TestEncounterResolvesSimultaneousHitsTogether(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		enemyID    string
		immune     bool
		supplied   bool
		wantHealth int
		accepted   string
	}{
		{name: "emergency before enemy", enemyID: "enemy-z", wantHealth: 85, accepted: "EM-HOS-SUP"},
		{name: "enemy before emergency", enemyID: "A-enemy", wantHealth: 82, accepted: "A-enemy"},
		{name: "supplied hit first", enemyID: "enemy-z", supplied: true, wantHealth: 93, accepted: "AAA"},
		{name: "existing immunity", enemyID: "enemy-z", immune: true, wantHealth: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, world := completedEmergencyWorld(t, 0)
			for i := 0; i < 180; i++ {
				encounterTick(t, world, PlayerCommand{})
			}
			world.state.Player.Bounds.X = 1101
			if err := world.AddEnemy(ctx, tc.enemyID, MeleeEnemy, Rect{X: 1060, Y: 0, Width: PlayerWidth, Height: PlayerHeight}); err != nil {
				t.Fatal(err)
			}
			enemy := &world.combat.enemies[0]
			enemy.Phase, enemy.Until, enemy.Facing, enemy.AttackID = EnemyTelegraph, world.state.Tick+1, 1, 1
			if tc.immune {
				world.combat.immuneUntil = world.state.Tick + 20
			}
			command := ServiceCommand{Objects: ObjectCommand{Feces: FecesCommand{Limb: LimbCommand{Combat: CombatCommand{Visible: Rect{X: 0, Y: 0, Width: 1536, Height: 560}}}}}}
			if tc.supplied {
				command.Objects.Feces.Limb.Combat.Hits = []DamageHit{{Attacker: "AAA", Attack: "1", Kind: OrdinaryDamage, Amount: 7, Direction: 1}}
			}
			if err := world.StepEncounter(ctx, command); err != nil {
				t.Fatal(err)
			}
			if world.state.Health != tc.wantHealth {
				t.Fatalf("health = %d, want %d: %+v", world.state.Health, tc.wantHealth, world.CombatView(ctx).Outcomes)
			}
			seen := make(map[string]string)
			for _, outcome := range world.CombatView(ctx).Outcomes {
				if outcome.Target == "player" {
					seen[outcome.Hit.Attacker] = outcome.Result
				}
			}
			for _, attacker := range []string{tc.enemyID, "EM-HOS-SUP"} {
				want := HitImmune
				if tc.accepted == attacker {
					want = HitAccepted
				}
				if seen[attacker] != want {
					t.Fatalf("incorrect hit ordering: %+v", seen)
				}
			}
			if tc.supplied && seen["AAA"] != HitAccepted {
				t.Fatalf("supplied hit was not accepted: %+v", seen)
			}
		})
	}
	_, _, _, _, crusher := activeService(t, 0)
	crusher.encounter.started, crusher.encounter.elapsed = true, 144
	crusher.state.Player.Bounds.X = 730
	crusher.combat.immuneUntil = crusher.state.Tick + 100
	crusher.combat.enemies[0].Body.Bounds.X = 680
	crusher.combat.enemies[0].Phase = EnemyTelegraph
	crusher.combat.enemies[0].Until = crusher.state.Tick + 1
	crusher.combat.enemies[0].Facing = 1
	crusher.combat.enemies[0].AttackID = 1
	encounterTick(t, crusher, PlayerCommand{})
	if !crusher.motion.dead || crusher.state.Health != 0 {
		t.Fatal("crushing did not bypass immunity")
	}
	var lethal, rejected bool
	for _, outcome := range crusher.CombatView(ctx).Outcomes {
		lethal = lethal || (outcome.Hit.Kind == LethalDamage && outcome.Result == HitAccepted)
		rejected = rejected || (outcome.Hit.Attacker == crusher.combat.enemies[0].ID && outcome.Result == HitImmune)
	}
	if !lethal || !rejected {
		t.Fatalf("missing simultaneous crusher and enemy outcomes: %+v", crusher.CombatView(ctx).Outcomes)
	}
}

func TestEmergencyBarrierAcknowledgmentAndRestoration(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	committed, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	batch := manifest.Batches(ctx)[0]
	for _, id := range batch.Deeds[:5] {
		committed = certifyFixture(t, owner, manifest, committed, id)
	}
	committed, err = owner.Accept(ctx, committed, batch.Deeds[5])
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
	id := emergencyBarrierID(batch.Emergency.ID)
	assertBarrier := func(world *World, want bool) {
		t.Helper()
		found := false
		for _, collider := range world.grid.colliders {
			found = found || collider.ID == id
		}
		if found != want {
			t.Fatalf("barrier presence = %t, want %t", found, want)
		}
	}
	assertBarrier(world, false)
	failed := serviceAck(proposal)
	failed.Succeeded = false
	if world.AcknowledgeEncounterService(ctx, owner, manifest, bays, failed) == nil {
		t.Fatal("accepted failed certification")
	}
	assertBarrier(world, false)
	if err = world.AcknowledgeEncounterService(ctx, owner, manifest, bays, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	assertBarrier(world, true)
	restored, err := NewWorld(ctx, owner, manifest, proposal, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBarrier(restored, true)
	barrier := rectFromContent(restored.encounter.room.Space.Barrier)
	restored.state.Player.Bounds.X = barrier.X - PlayerWidth - 2
	restored.state.Player.Support = "boundary-floor"
	for i := 0; i < 20; i++ {
		encounterTick(t, restored, PlayerCommand{Move: 1})
	}
	if restored.state.Player.Bounds.X+PlayerWidth > barrier.X+Tolerance {
		t.Fatal("player crossed the active emergency barrier")
	}
	// The high bypass remains traversable over the 128-pixel barrier.
	restored.state.Player.Bounds = Rect{X: barrier.X - 20, Y: 240, Width: PlayerWidth, Height: PlayerHeight}
	restored.state.Player.Velocity = Vector{X: 320}
	restored.state.Player.Support = "bypass-002"
	for i := 0; i < 15; i++ {
		encounterTick(t, restored, PlayerCommand{Move: 1})
	}
	if restored.state.Player.Bounds.X <= barrier.X+barrier.Width {
		t.Fatal("barrier blocked the overhead bypass")
	}
	after := acknowledgeEscape(t, owner, proposal, world)
	assertBarrier(world, true)
	rebuilt, err := NewWorld(ctx, owner, manifest, after, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBarrier(rebuilt, true)
}

func TestEmergencyBarrierWaitsForOccupiedSpace(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	committed, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	batch := manifest.Batches(ctx)[0]
	for _, id := range batch.Deeds[:5] {
		committed = certifyFixture(t, owner, manifest, committed, id)
	}
	committed, err = owner.Accept(ctx, committed, batch.Deeds[5])
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
	area := rectFromContent(world.encounter.room.Space.Barrier)
	world.state.Player.Bounds = Rect{X: area.X, Y: 0, Width: PlayerWidth, Height: PlayerHeight}
	world.state.Player.Velocity = Vector{}
	world.state.Player.Support = "boundary-floor"
	if err = world.AcknowledgeEncounterService(ctx, owner, manifest, bays, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	if !world.encounter.barrierPending {
		t.Fatal("occupied barrier was installed prematurely")
	}
	for _, collider := range world.grid.colliders {
		if collider.ID == emergencyBarrierID(batch.Emergency.ID) {
			t.Fatal("barrier materialized around the player")
		}
	}
	for i := 0; i < 30 && world.encounter.barrierPending; i++ {
		encounterTick(t, world, PlayerCommand{Move: 1})
	}
	if world.encounter.barrierPending || overlaps(world.state.Player.Bounds, area) {
		t.Fatal("barrier did not install once the player cleared it")
	}
	found := false
	for _, collider := range world.grid.colliders {
		found = found || collider.ID == emergencyBarrierID(batch.Emergency.ID)
	}
	if !found || world.motion.dead || world.EmergencyView(ctx).WarningTicks <= 0 {
		t.Fatal("barrier installation harmed the player or lost the warning")
	}
}

func defeatReturningFollower(t *testing.T, world *World, id string) {
	t.Helper()
	ctx := context.Background()
	for tick := 0; tick < 2500; tick++ {
		var target *Enemy
		for i := range world.combat.enemies {
			if world.combat.enemies[i].ID == id {
				target = &world.combat.enemies[i]
				break
			}
		}
		if target == nil {
			t.Fatalf("missing follower %s", id)
		}
		if target.Health <= 0 {
			return
		}
		player := world.state.Player.Bounds
		facing := 1
		area := Rect{X: player.X + player.Width, Y: player.Y + (player.Height-36)/2, Width: 48, Height: 36}
		if centerX(target.Body.Bounds) < centerX(player) {
			facing = -1
			area.X = player.X - 48
		}
		command := PlayerCommand{}
		if overlaps(area, target.Body.Bounds) && world.state.Tick >= target.ImmuneUntil && (world.motion.action == FreeAction || world.state.Tick >= world.motion.actionUntil) {
			command.Aim = Vector{X: float64(facing)}
			command.Edges = []PlayerEdge{release(settings.Melee), press(settings.Melee)}
		}
		encounterTick(t, world, command)
		if world.Recruitment(ctx).Live > 2 || world.service.pending != nil || world.motion.dead {
			t.Fatalf("combat altered progress or exceeded the cap: %+v", world.Recruitment(ctx))
		}
	}
	t.Fatalf("follower %s was not defeated", id)
}

func TestReturningFollowersToCertifiedTicketLifecycle(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays, committed, world := instituteFixture(t)
	// Construction fixture: the trolley is already at its authored pad. All
	// attacks, replacement timers and terminal holds below use StepEncounter.
	for i := range world.objects.items {
		if world.objects.items[i].Kind == ObjectTrolley {
			world.objects.items[i].Bounds.X = 1288
		}
	}
	world.syncObjectPlatforms()
	world.state.Player.Bounds.X = 900
	id := world.combat.enemies[0].ID
	for generation := 0; generation < 3; generation++ {
		defeatReturningFollower(t, world, id)
		view := world.Recruitment(ctx)
		if view.Live != 0 || len(view.Queue) != 1 || view.Queue[0].Warning || committed.Credits() != 75 || world.ServiceView(ctx).Status == ServicePending {
			t.Fatalf("generation %d defeat created progress: %+v", generation, view)
		}
		next := view.Queue[0].ID
		if next != fmt.Sprintf("feed-D-INS-SUP-01-follower-%06d", generation+1) {
			t.Fatalf("unstable replacement identity: %s", next)
		}
		for i := 0; i < 360; i++ {
			encounterTick(t, world, PlayerCommand{})
		}
		view = world.Recruitment(ctx)
		if len(view.Queue) != 1 || !view.Queue[0].Warning || view.Queue[0].Remaining != 180 || view.Live != 0 {
			t.Fatalf("generation %d warning: %+v", generation, view)
		}
		for i := 0; i < 180; i++ {
			encounterTick(t, world, PlayerCommand{})
		}
		view = world.Recruitment(ctx)
		if view.Live != 1 || len(view.Queue) != 0 || committed.Credits() != 75 {
			t.Fatalf("generation %d replacement: %+v", generation, view)
		}
		id = next
	}
	defeatReturningFollower(t, world, id)
	for i := 0; i < 200 && world.state.Player.Bounds.X < 1090; i++ {
		encounterTick(t, world, PlayerCommand{Move: 1})
	}
	for i := 0; i < 60 && world.state.Player.Velocity.X != 0; i++ {
		encounterTick(t, world, PlayerCommand{})
	}
	for swing := 0; swing < 2; swing++ {
		for world.motion.action != FreeAction && world.state.Tick < world.motion.actionUntil {
			encounterTick(t, world, PlayerCommand{})
		}
		encounterTick(t, world, PlayerCommand{Aim: Vector{X: 1}, Edges: []PlayerEdge{release(settings.Melee), press(settings.Melee)}})
		for i := 0; i < 16; i++ {
			encounterTick(t, world, PlayerCommand{})
		}
		if world.Recruitment(ctx).Health != 44-(swing+1)*22 {
			t.Fatalf("source strike %d: %+v", swing, world.Recruitment(ctx))
		}
	}
	if view := world.Recruitment(ctx); view.Health != 0 || len(view.Queue) != 0 || committed.Credits() != 75 {
		t.Fatalf("destroyed source granted progress or retained queue: %+v", view)
	}
	jumped, dropped := false, false
	for i := 0; i < 360; i++ {
		player := PlayerCommand{Move: 1}
		if !jumped && world.state.Player.Bounds.X >= 1180 && world.state.Player.Support != "" {
			player.Edges = []PlayerEdge{press(settings.Jump)}
			jumped = true
		}
		if world.state.Player.Bounds.X >= 1364 {
			player.Move = 0
			player.Edges = []PlayerEdge{release(settings.Jump)}
			if !dropped && world.state.Player.Support == "bypass-004" {
				player.Down = true
				player.Edges = []PlayerEdge{release(settings.Jump), press(settings.Jump)}
				dropped = true
			}
		}
		encounterTick(t, world, player)
		if jumped && player.Move == 0 && world.state.Player.Support == "boundary-floor" && world.state.Player.Velocity.X == 0 {
			break
		}
	}
	if !jumped || world.ServiceView(ctx).Status != ServiceEvidenceReady || world.state.Player.Support != "boundary-floor" {
		t.Fatalf("could not reach receiving terminal: %+v %+v", world.state.Player, world.ServiceView(ctx))
	}
	for tick := 0; tick < 36; tick++ {
		player := PlayerCommand{}
		if tick == 0 {
			player.Edges = []PlayerEdge{press(settings.Interact)}
		}
		encounterTick(t, world, player)
	}
	if world.ServiceView(ctx).Status != ServicePending || committed.Credits() != 75 || world.Recruitment(ctx).Disconnected {
		t.Fatalf("terminal hold did not produce only a candidate: %+v", world.ServiceView(ctx))
	}
	proposal, err := world.PrepareService(ctx, owner, committed, campaign.Transition{CampaignID: serviceCampaignID, Sequence: committed.Data(ctx).OperationSequence + 1})
	if err != nil || proposal.Credits() != 76 || world.Recruitment(ctx).Disconnected {
		t.Fatalf("certification preparation: %v", err)
	}
	if err = world.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(proposal)); err != nil {
		t.Fatal(err)
	}
	if !world.Recruitment(ctx).Disconnected {
		t.Fatal("acknowledged ticket did not disconnect its feed")
	}
	revisit, err := NewWorld(ctx, owner, manifest, proposal, bays, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := revisit.RecruitmentForTicket(ctx, "D-INS-SUP-01")
	if err != nil || !view.Disconnected || view.Live != 0 || len(view.Queue) != 0 {
		t.Fatalf("revisited feed: %+v %v", view, err)
	}
	if _, err = revisit.RecruitmentForTicket(ctx, "D-INS-SUP-02"); err == nil {
		t.Fatal("uncertified sibling appeared disconnected")
	}
	for i := 0; i < 120 && revisit.state.Player.Bounds.X < 140; i++ {
		encounterTick(t, revisit, PlayerCommand{Move: 1})
	}
	for i := 0; i < 60 && revisit.state.Player.Velocity.X != 0; i++ {
		encounterTick(t, revisit, PlayerCommand{})
	}
	for i := 0; i < 36; i++ {
		player := PlayerCommand{}
		if i == 0 {
			player.Edges = []PlayerEdge{press(settings.Interact)}
		}
		encounterTick(t, revisit, player)
	}
	if _, err = revisit.RequestService(ctx, owner, manifest, proposal, campaign.AttemptAccept, "D-INS-SUP-02"); err != nil {
		t.Fatal(err)
	}
	next, err := revisit.PrepareService(ctx, owner, proposal, campaign.Transition{CampaignID: serviceCampaignID, Sequence: proposal.Data(ctx).OperationSequence + 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = revisit.AcknowledgeService(ctx, owner, manifest, bays, serviceAck(next)); err != nil {
		t.Fatal(err)
	}
	if view = revisit.Recruitment(ctx); view.Ticket != "D-INS-SUP-02" || view.ID != "feed-D-INS-SUP-02" || view.Health != 44 || view.Disconnected || next.Credits() != 76 {
		t.Fatalf("next ticket did not get its own feed: %+v", view)
	}
}

func TestEveryEmergencyAftermathIsCommittedAndRestored(t *testing.T) {
	ctx := context.Background()
	owner, manifest, bays := campaignFixture(t)
	committed, err := owner.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{
		"steaming_pipe", "wrecked_beds", "damaged_manifold", "burnt_screens",
		"wax_and_tins", "broken_turnstile", "bell_exhibit", "paid_scandal_advertisement",
		"processing_report", "desks", "contaminated_floor", "audit_success_file",
		"sales_graphic", "fallen_arch", "replacement_advertisements", "attention_report",
	}
	for index, batch := range manifest.Batches(ctx) {
		for _, id := range batch.Deeds {
			committed = certifyFixture(t, owner, manifest, committed, id)
		}
		world, buildErr := NewWorld(ctx, owner, manifest, committed, bays, nil)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		if view := world.EmergencyView(ctx); view.ID != batch.Emergency.ID || len(view.Effects) != 0 {
			t.Fatalf("uncommitted aftermath: %+v", view)
		}
		credits := committed.Credits()
		committed = acknowledgeEscape(t, owner, committed, world)
		view := world.EmergencyView(ctx)
		if len(view.Effects) != 1 || view.Effects[0].Kind != kinds[index] || view.Effects[0].Bounds != rectFromContent(world.encounter.room.Aftermath) || view.Effects[0].Decorative == world.encounter.room.AftermathBlocking || committed.Credits() != credits {
			t.Fatalf("%s aftermath: %+v", batch.Emergency.ID, view)
		}
		restored, restoreErr := NewWorld(ctx, owner, manifest, committed, bays, nil)
		if restoreErr != nil {
			t.Fatal(restoreErr)
		}
		if after := restored.EmergencyView(ctx); after.Stage != campaign.EmergencyAftermath || !after.BypassUsable || !reflect.DeepEqual(after.Effects, view.Effects) {
			t.Fatalf("%s reconstructed aftermath: %+v", batch.Emergency.ID, after)
		}
		if index%4 == 3 && index < 12 {
			closure := manifest.Deeds(ctx)[index/4*25+24]
			committed, err = owner.DefeatBoss(ctx, committed, closure.Deed.Encounter)
			if err != nil {
				t.Fatal(err)
			}
			committed = certifyFixture(t, owner, manifest, committed, closure.Deed.ID)
		}
	}
}

func TestEmergencyAftermathActorsAndBoundedTurnstile(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		batch int
		id    string
		kind  string
	}{
		{batch: 2, id: "EM-HOS-UTL", kind: "damaged_manifold"},
		{batch: 5, id: "EM-CHU-RES", kind: "broken_turnstile"},
		{batch: 9, id: "EM-MIN-RES", kind: "desks"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			owner, manifest, bays, committed, world := completedEmergencyWorld(t, tc.batch)
			if len(world.EmergencyView(ctx).Effects) != 0 {
				t.Fatal("uncommitted aftermath exposed effects")
			}
			after := acknowledgeEscape(t, owner, committed, world)
			check := func(world *World) {
				t.Helper()
				view := world.EmergencyView(ctx)
				if view.Stage != campaign.EmergencyAftermath || len(view.Effects) != 1 || view.Effects[0].Kind != tc.kind || !view.BypassUsable {
					t.Fatalf("incorrect aftermath: %+v", view)
				}
				actors := 0
				positions := make(map[float64]bool)
				for _, entity := range world.state.Entities {
					if (tc.id == "EM-HOS-UTL" && entity.ID != "CHR-NIN") || (tc.id != "EM-HOS-UTL" && (entity.Role != content.RoleActor || entity.Ticket == "")) {
						continue
					}
					actors++
					positions[entity.Position.X] = true
					if !entity.Protected {
						t.Fatalf("unprotected aftermath actor: %+v", entity)
					}
					found := false
					for _, civilian := range world.combat.civilians {
						if civilian.ID == string(entity.ID) {
							found = world.protected(civilian.Bounds) && !civilian.Incapacitated
						}
					}
					if !found {
						t.Fatalf("missing protected actor %s", entity.ID)
					}
				}
				want := 6
				if tc.id == "EM-HOS-UTL" {
					want = 1
				}
				if actors != want || len(positions) != want {
					t.Fatalf("aftermath actor positions: %d / %v", actors, positions)
				}
			}
			check(world)
			restored, err := NewWorld(ctx, owner, manifest, after, bays, nil)
			if err != nil {
				t.Fatal(err)
			}
			check(restored)
			attackWorld, err := NewWorld(ctx, owner, manifest, after, bays, nil)
			if err != nil {
				t.Fatal(err)
			}
			// Attack toward the affected adults from a reachable side of their
			// protection volume; neither their identity nor health is disposable.
			attackX, facing := 1221.0, -1.0
			if tc.id == "EM-CHU-RES" {
				attackX = 1320
			} else if tc.id == "EM-MIN-RES" {
				attackX, facing = 940, 1
			}
			attackWorld.state.Player.Bounds.X = attackX
			attackWorld.state.Player.Support = "boundary-floor"
			encounterTick(t, attackWorld, PlayerCommand{Aim: Vector{X: facing}, Edges: []PlayerEdge{press(settings.Melee)}})
			for i := 0; i < 16; i++ {
				encounterTick(t, attackWorld, PlayerCommand{})
			}
			check(attackWorld)
			for i := 0; i < 60 && attackWorld.motion.action == AttackAction; i++ {
				encounterTick(t, attackWorld, PlayerCommand{})
			}
			if attackWorld.motion.action != FreeAction {
				t.Fatal("melee recovery did not finish")
			}
			encounterTick(t, attackWorld, PlayerCommand{Aim: Vector{X: facing}, Edges: []PlayerEdge{press(settings.Throw)}})
			for i := 0; i < 30; i++ {
				encounterTick(t, attackWorld, PlayerCommand{})
			}
			if len(attackWorld.combat.projectiles) != 0 {
				t.Fatal("projectile crossed the actors' protected boundary")
			}
			check(attackWorld)
			if tc.id != "EM-CHU-RES" {
				return
			}
			area := rectFromContent(restored.encounter.room.Aftermath)
			restored.state.Player.Bounds = Rect{X: area.X - PlayerWidth - 2, Y: 0, Width: PlayerWidth, Height: PlayerHeight}
			restored.state.Player.Support = "boundary-floor"
			for i := 0; i < 30 && len(restored.Reactions(ctx)) == 0; i++ {
				encounterTick(t, restored, PlayerCommand{Move: 1})
			}
			reactions := restored.Reactions(ctx)
			if len(reactions) != 1 || reactions[0].Kind != "turnstile_collision" || reactions[0].Until-restored.state.Tick != 84 || restored.state.Health != 100 {
				t.Fatalf("turnstile reaction: %+v", reactions)
			}
			for i := 0; i < 85; i++ {
				encounterTick(t, restored, PlayerCommand{})
			}
			if len(restored.Reactions(ctx)) != 0 || restored.state.Health != 100 || after.Credits() != committed.Credits() {
				t.Fatal("turnstile reaction did not end harmlessly")
			}
		})
	}
}

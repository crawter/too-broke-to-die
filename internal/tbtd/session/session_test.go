// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
	"too-broke-to-die/internal/tbtd/simulation"
)

const (
	oldDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type recordingStore struct {
	requests []ProgressRequest

	results map[RequestID]ProgressResult

	discards int

	submitErr error
}

func (store *recordingStore) Submit(_ context.Context, request ProgressRequest) error {
	if store.submitErr != nil {
		return store.submitErr
	}
	store.requests = append(store.requests, request)
	return nil
}

func (store *recordingStore) DiscardProgress(ctx context.Context, request ProgressRequest) error {
	store.discards++
	return store.Submit(ctx, request)
}

func (store *recordingStore) Poll(ctx context.Context, id RequestID) (ProgressResult, error) {
	result, ok := store.results[id]
	if !ok {
		return NewProgressResult(ctx, ProgressResultData{ID: id, Status: StoragePending}), nil
	}
	delete(store.results, id)
	return result, nil
}

func (store *recordingStore) last() ProgressInput {
	return store.requests[len(store.requests)-1].Data()
}

func (store *recordingStore) reply(data ProgressResultData) {
	if store.results == nil {
		store.results = make(map[RequestID]ProgressResult)
	}
	store.results[data.ID] = NewProgressResult(context.Background(), data)
}

func sessionFixture(t *testing.T) (*Service, *recordingStore) {
	t.Helper()
	ctx := context.Background()
	fsys := os.DirFS("../../..")
	manifest, err := content.LoadCampaign(ctx, fsys, "content/data/campaign.json", "content/data/ru-services.json")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := campaign.New(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	bays, err := content.LoadServiceBays(ctx, fsys, "content/data/encounters/templates.json", "content/data/encounters/variants.json", manifest)
	if err != nil {
		t.Fatal(err)
	}
	store := &recordingStore{}
	service, err := New(ctx, owner, manifest, bays, store)
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func frame(t *testing.T, service *Service, input FrameInput) FrameViewData {
	t.Helper()
	ctx := context.Background()
	command, err := NewFrameCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.HandleFrame(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	return view.Data(ctx)
}

func choose(t *testing.T, service *Service, action MenuAction, target string) FrameViewData {
	t.Helper()
	return frame(t, service, FrameInput{Selections: []MenuSelection{{Action: action, Target: target}}})
}

func loadFixture(t *testing.T, service *Service, store *recordingStore, snapshot campaign.Snapshot) {
	t.Helper()
	if got := choose(t, service, "continue", ""); got.State != StateRestoring {
		t.Fatalf("continue: %+v", got)
	}
	request := store.last()
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Status: StorageSucceeded, Revision: 7, RevisionKnown: true, PayloadDigest: oldDigest, Snapshot: snapshot})
	if got := frame(t, service, FrameInput{}); got.State != StatePause || got.Progress.Revision != 7 || got.Player.Health != 100 || got.Player.Feces != 5 {
		t.Fatalf("restoration: %+v", got)
	}
	if got := choose(t, service, "resume", ""); got.State != StateActive {
		t.Fatalf("resume: %+v", got)
	}
}

// certifyAuthored builds a valid fixture through the real campaign catalogue.
func certifyAuthored(t *testing.T, service *Service, snapshot campaign.Snapshot, id content.DeedID) campaign.Snapshot {
	t.Helper()
	ctx := context.Background()
	active, err := service.campaign.Accept(ctx, snapshot, id)
	if err != nil {
		t.Fatalf("accept %s: %v", id, err)
	}
	attempt, ok := active.ActiveAttempt()
	if !ok {
		t.Fatalf("no attempt for %s", id)
	}
	objects := make(map[content.ObjectID]content.Object)
	for _, object := range service.manifest.Objects(ctx) {
		objects[object.ID] = object
	}
	for _, deed := range service.manifest.Deeds(ctx) {
		if deed.Deed.ID != id {
			continue
		}
		event := campaign.Certification{Deed: id, Attempt: attempt.Number, Evidence: deed.Evidence, Trigger: deed.Trigger, HoldMilliseconds: deed.Deed.HoldMilliseconds}
		for _, objectID := range deed.Deed.Objects {
			object := objects[objectID]
			if object.Deed == id {
				event.EvidenceObjects = append(event.EvidenceObjects, objectID)
			}
			if object.Role == content.RoleTerminal {
				event.Terminal = objectID
			}
		}
		result, _, applyErr := service.campaign.Apply(ctx, active, event)
		if applyErr != nil {
			t.Fatalf("certify %s: %v", id, applyErr)
		}
		return result
	}
	t.Fatalf("unknown deed %s", id)
	return campaign.Snapshot{}
}

func authoredProgress(t *testing.T, service *Service, institutions int, closeLast bool) campaign.Snapshot {
	t.Helper()
	ctx := context.Background()
	snapshot, err := service.campaign.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for institution := 0; institution < institutions; institution++ {
		for family := 0; family < 4; family++ {
			for _, id := range service.manifest.Batches(ctx)[institution*4+family].Deeds {
				snapshot = certifyAuthored(t, service, snapshot, id)
			}
		}
		if institution == institutions-1 && !closeLast {
			break
		}
		closure := service.manifest.Deeds(ctx)[institution*25+24]
		snapshot, err = service.campaign.DefeatBoss(ctx, snapshot, closure.Deed.Encounter)
		if err != nil {
			t.Fatal(err)
		}
		snapshot = certifyAuthored(t, service, snapshot, closure.Deed.ID)
	}
	return snapshot
}

func TestRestoredEmergencyAndDefeatedBossPreserveHistory(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	started, err := service.campaign.Start(ctx, campaign.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range service.manifest.Batches(ctx)[0].Deeds {
		started = certifyAuthored(t, service, started, id)
	}
	loadFixture(t, service, store, started)
	before := service.committed.Data(ctx)
	_, world := service.world.Snapshot(ctx).Data(ctx)
	if len(world.Emergencies) != 1 || world.Emergencies[0].WarningTicks != 180 || !world.Emergencies[0].SafeBoundary || !world.Emergencies[0].BypassUsable {
		t.Fatalf("unsafe emergency restoration: %+v", world.Emergencies)
	}
	frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Pause, Kind: Pressed}}})
	choose(t, service, "return_checkpoint", "")
	view := choose(t, service, "confirm", "")
	_, world = service.world.Snapshot(ctx).Data(ctx)
	if view.State != StatePause || world.Emergencies[0].WarningTicks != 180 || !reflect.DeepEqual(before, service.committed.Data(ctx)) || len(store.requests) != 1 {
		t.Fatalf("emergency restore changed history: %+v", view)
	}

	bossService, bossStore := sessionFixture(t)
	progress := authoredProgress(t, bossService, 1, false)
	progress, err = bossService.campaign.DefeatBoss(ctx, progress, "ENC-HOS-05")
	if err != nil {
		t.Fatal(err)
	}
	loadFixture(t, bossService, bossStore, progress)
	_, world = bossService.world.Snapshot(ctx).Data(ctx)
	if !bossService.world.Boss(ctx).Confirmed || world.BossActive || !slices.Contains(world.Defeated, content.EncounterID("ENC-HOS-05")) || bossService.committed.Credits() != 24 || len(bossStore.requests) != 1 {
		t.Fatalf("defeated boss restoration: %+v", world)
	}
}

func TestSimulationEvidenceBeforeSameTickDeath(t *testing.T) {
	ctx := context.Background()
	for _, lethalTick := range []int{35, 36} {
		t.Run(map[int]string{35: "unfinished", 36: "completed"}[lethalTick], func(t *testing.T) {
			service, store := sessionFixture(t)
			committed, _ := preparedCertification(t)
			loadFixture(t, service, store, committed)
			// Play around the trolley and defeat its authored enemy before
			// pushing; otherwise the solid trolley overlaps the stationary actor.
			jumped := false
			for tick := 0; tick < 1000; tick++ {
				_, world := service.world.Snapshot(ctx).Data(ctx)
				enemy := service.world.CombatView(ctx).Enemies[0]
				if enemy.Body.Bounds.X-world.Player.Bounds.X < 70 && world.Player.Bounds.Y == 0 {
					break
				}
				player := simulation.PlayerCommand{Move: 1}
				if !jumped && world.Player.Bounds.X >= 240 {
					player.Edges = []simulation.PlayerEdge{{Action: settings.Jump, Pressed: true}}
					jumped = true
				}
				simulationServiceTick(t, service, player, nil)
			}
			for tick := 0; tick < 500; tick++ {
				enemies := service.world.CombatView(ctx).Enemies
				if len(enemies) != 1 || enemies[0].Health == 0 {
					break
				}
				_, world := service.world.Snapshot(ctx).Data(ctx)
				distance := enemies[0].Body.Bounds.X - world.Player.Bounds.X
				player := simulation.PlayerCommand{Aim: simulation.Vector{X: 1}}
				if distance < 0 {
					player.Aim.X = -1
				}
				if distance > 65 {
					player.Move = 1
				} else if distance < -65 {
					player.Move = -1
				} else if service.world.PlayerStatus().Action == simulation.FreeAction {
					player.Edges = []simulation.PlayerEdge{{Action: settings.Melee}, {Action: settings.Melee, Pressed: true}}
				}
				simulationServiceTick(t, service, player, nil)
			}
			enemies := service.world.CombatView(ctx).Enemies
			if len(enemies) != 1 || enemies[0].Health != 0 {
				t.Fatalf("enemy still blocks the trolley: %+v", enemies)
			}
			jumped = false
			for tick := 0; tick < 1000; tick++ {
				_, world := service.world.Snapshot(ctx).Data(ctx)
				if world.Player.Bounds.X < 240 {
					break
				}
				player := simulation.PlayerCommand{Move: -1}
				if !jumped && world.Player.Bounds.X < 450 {
					player.Edges = []simulation.PlayerEdge{{Action: settings.Jump}, {Action: settings.Jump, Pressed: true}}
					jumped = true
				}
				simulationServiceTick(t, service, player, nil)
			}
			for tick := 0; tick < 2200 && service.world.ServiceView(ctx).Status != simulation.ServiceEvidenceReady; tick++ {
				simulationServiceTick(t, service, simulation.PlayerCommand{Move: 1}, nil)
				if service.world.PlayerStatus().Dead {
					t.Fatal("player died before completing evidence")
				}
			}
			if service.world.ServiceView(ctx).Status != simulation.ServiceEvidenceReady {
				_, world := service.world.Snapshot(ctx).Data(ctx)
				t.Fatalf("trolley evidence not ready: %+v %+v", world.Player, service.world.Objects(ctx))
			}
			// Park the trolley just beyond the terminal so its push prompt
			// does not mask the completed-evidence interaction.
			for tick := 0; tick < 180; tick++ {
				objects := service.world.Objects(ctx)
				if len(objects) == 1 && objects[0].Bounds.X >= 1354 {
					break
				}
				simulationServiceTick(t, service, simulation.PlayerCommand{Move: 1}, nil)
			}
			for tick := 0; tick < 12; tick++ {
				simulationServiceTick(t, service, simulation.PlayerCommand{Move: -1}, nil)
			}
			for tick := 0; tick < 180; tick++ {
				_, world := service.world.Snapshot(ctx).Data(ctx)
				if world.Player.Velocity.X == 0 && world.Player.Support != "" {
					break
				}
				simulationServiceTick(t, service, simulation.PlayerCommand{}, nil)
			}
			prompt := service.world.InteractionView(ctx)
			if !prompt.Found || prompt.Target.Kind != simulation.InteractionService || prompt.Target.ID != "OBJ-HOS-SUP-TERMINAL" {
				_, world := service.world.Snapshot(ctx).Data(ctx)
				t.Fatalf("terminal not in reach: %+v, player %+v objects %+v", prompt, world.Player, service.world.Objects(ctx))
			}
			for tick := 1; tick <= lethalTick; tick++ {
				player := simulation.PlayerCommand{}
				if tick == 1 {
					player.Edges = []simulation.PlayerEdge{{Action: settings.Interact, Pressed: true}}
				}
				var hits []simulation.DamageHit
				if tick == lethalTick {
					hits = []simulation.DamageHit{{Attacker: "hazard", Attack: "lethal", Kind: simulation.LethalDamage, Amount: 100, Direction: 1}}
				}
				simulationServiceTick(t, service, player, hits)
			}
			if err := service.collect(ctx, true); err != nil {
				t.Fatal(err)
			}
			request := store.last()
			want := 0
			if lethalTick == 36 {
				want = 1
			}
			if request.Proposal.Credits() != want || request.Proposal.Data(ctx).DebtCents != 4999 {
				t.Fatalf("wrong atomic proposal: %+v", request.Proposal.Data(ctx))
			}
			_, before := service.world.Snapshot(ctx).Data(ctx)
			view := frame(t, service, FrameInput{Elapsed: time.Second})
			_, after := service.world.Snapshot(ctx).Data(ctx)
			if view.State != StateSaving || view.Progress.Credits != 0 || view.Progress.DebtCents != 0 || after.Tick != before.Tick {
				t.Fatalf("pending death leaked state: %+v", view)
			}
			store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: request.Proposal})
			view = frame(t, service, FrameInput{})
			if view.State != StateDead || view.Progress.Credits != want || view.Progress.DebtCents != 4999 || len(view.Progress.Incidents) != 1 || len(view.Progress.Consequences) != want {
				t.Fatalf("death result: %+v", view)
			}
		})
	}
}

func simulationServiceTick(t *testing.T, service *Service, player simulation.PlayerCommand, hits []simulation.DamageHit) {
	t.Helper()
	command := simulation.ServiceCommand{Objects: simulation.ObjectCommand{Feces: simulation.FecesCommand{Limb: simulation.LimbCommand{Combat: simulation.CombatCommand{Player: player, Visible: simulation.Rect{X: -500, Y: -20, Width: 100, Height: 100}, Hits: hits}}}}}
	if err := service.world.StepServices(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func TestSimulatedBossVictoryWaitsForAcknowledgment(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	progress := authoredProgress(t, service, 1, false)
	progress, err := service.campaign.Checkpoint(ctx, progress, "ANC-HOS-05")
	if err != nil {
		t.Fatal(err)
	}
	loadFixture(t, service, store, progress)
	// The live arena resolves attacks as well as boss health. Follow its
	// warnings instead of standing in every hazard while testing save gating.
	var jumpedFor string
	for tick := 0; tick < 16000 && service.policy.State() == StateActive; tick++ {
		_, world := service.world.Snapshot(ctx).Data(ctx)
		boss := service.world.Boss(ctx)
		body := world.Player.Bounds
		input := FrameInput{Elapsed: 8333334 * time.Nanosecond, Values: []ActionValue{{Action: settings.MoveLeft}, {Action: settings.MoveRight}, {Action: settings.Aim, X: 1}}}
		key := boss.Attack.ID + ":" + string(boss.Stage)
		if boss.Stage == simulation.BossTelegraph && boss.Elapsed == 0 {
			jumpedFor = ""
		}
		switch {
		case boss.Stage == simulation.BossActive && boss.Attack.ID == "HOS-C":
			input.Values[1].X = 1
		case boss.Stage == simulation.BossTelegraph && boss.Attack.ID == "HOS-A" && boss.Remaining <= 20 && jumpedFor != key && body.Y < 1:
			input.Edges = []ActionEdge{{Action: settings.Jump, Kind: Released}, {Action: settings.Jump, Kind: Pressed}}
			jumpedFor = key
		case boss.Stage == simulation.BossActive && boss.Attack.ID == "HOS-B" && len(boss.Danger) == 1 && jumpedFor != key:
			cart := boss.Danger[0]
			if body.Y < 1 && ((cart.X < body.X && body.X-(cart.X+cart.Width) < 100) || (cart.X > body.X && cart.X-(body.X+body.Width) < 100)) {
				input.Edges = []ActionEdge{{Action: settings.Jump, Kind: Released}, {Action: settings.Jump, Kind: Pressed}}
				jumpedFor = key
			}
		case body.X > boss.Bounds.X-45:
			input.Values[0].X = 1
		case body.X < boss.Bounds.X-60:
			input.Values[1].X = 1
		case boss.Stage == simulation.BossRecovery && service.world.PlayerStatus().Action == simulation.FreeAction:
			input.Edges = []ActionEdge{{Action: settings.Melee, Kind: Released}, {Action: settings.Melee, Kind: Pressed}}
		}
		frame(t, service, input)
	}
	boss := service.world.Boss(ctx)
	if service.policy.State() != StateSaving || boss.Pending == nil || boss.Confirmed {
		t.Fatalf("boss did not reach pending victory: %+v", boss)
	}
	request := store.last()
	_, before := service.world.Snapshot(ctx).Data(ctx)
	if request.Operation.Sequence != 1 || !slices.Contains(request.Proposal.Data(ctx).Defeated, content.EncounterID("ENC-HOS-05")) {
		t.Fatalf("victory proposal: %+v", request)
	}
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageFailed, Code: StorageFull})
	view := frame(t, service, FrameInput{Elapsed: time.Second})
	_, frozen := service.world.Snapshot(ctx).Data(ctx)
	if view.State != StateError || view.Progress.Credits != 24 || service.world.Boss(ctx).Confirmed || slices.Contains(service.committed.Data(ctx).Defeated, content.EncounterID("ENC-HOS-05")) || frozen.Tick != before.Tick {
		t.Fatalf("failed boss save leaked victory: %+v", view)
	}
	choose(t, service, "retry_save", "")
	resolution := store.last()
	store.reply(ProgressResultData{ID: resolution.ID, Campaign: testID, Operation: request.Operation, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: request.Proposal})
	view = frame(t, service, FrameInput{})
	if view.State != StateActive || !service.world.Boss(ctx).Confirmed || !slices.Contains(service.committed.Data(ctx).Defeated, content.EncounterID("ENC-HOS-05")) || view.Progress.Credits != 24 || view.Progress.DebtCents != 0 {
		t.Fatalf("acknowledged boss victory: %+v", view)
	}
}

func TestConsentStageRestorationDoesNotWrite(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	progress := authoredProgress(t, service, 4, true)
	var err error
	progress, err = service.campaign.IssuePermit(ctx, progress)
	if err != nil {
		t.Fatal(err)
	}
	progress, err = service.campaign.Prepare(ctx, progress, campaign.Transition{CampaignID: string(testID), Sequence: 1, Story: &campaign.StoryChange{Action: campaign.StoryConsent}})
	if err != nil {
		t.Fatal(err)
	}
	choose(t, service, "continue", "")
	request := store.last()
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Status: StorageSucceeded, Revision: 7, RevisionKnown: true, PayloadDigest: oldDigest, Snapshot: progress})
	view := frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	_, world := service.world.Snapshot(ctx).Data(ctx)
	if view.State != StateScene || !view.Progress.Consent || view.Progress.Ending != campaign.EndingDeath || view.Progress.Credits != 100 || world.SceneEntry != string(campaign.EndingDeath) || world.HasPlayer || len(store.requests) != 1 || len(view.Events) != 0 {
		t.Fatalf("consent restoration: %+v, %+v", view, world)
	}
	frame(t, service, FrameInput{Elapsed: 25 * time.Millisecond})
	if len(store.requests) != 1 || !reflect.DeepEqual(service.committed.Data(ctx), progress.Data(ctx)) {
		t.Fatal("restored scene created a campaign event")
	}
}

func TestNewGameLooksUpSavedProgressBeforeConfirmation(t *testing.T) {
	service, store := sessionFixture(t)
	_, saved := preparedCertification(t)
	view := choose(t, service, "new_game", "")
	if view.Confirmation != "" || view.State != StateTitle || len(store.requests) != 1 || store.last().Kind != LoadActive {
		t.Fatalf("confirmation preceded lookup: %+v", view)
	}
	choose(t, service, "new_game", "")
	if len(store.requests) != 1 {
		t.Fatal("started a second lookup")
	}
	request := store.last()
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: saved})
	view = frame(t, service, FrameInput{})
	if view.State != StateTitle || view.Confirmation != "new_game" || view.Progress.Campaign != testID || view.Progress.Credits != 1 || !strings.Contains(view.Warning, "1/100") {
		t.Fatalf("wrong saved progress: %+v", view)
	}
	choose(t, service, "cancel", "")
	if len(store.requests) != 1 || service.pending != nil {
		t.Fatal("cancel initiated another operation")
	}
}

func TestNewGameLookupRejectsUnacceptedOrMismatchedResults(t *testing.T) {
	for _, submitFailure := range []bool{true, false} {
		t.Run(map[bool]string{true: "submit", false: "mismatch"}[submitFailure], func(t *testing.T) {
			service, store := sessionFixture(t)
			if submitFailure {
				store.submitErr = ErrStoreBusy
			}
			view := choose(t, service, "new_game", "")
			if !submitFailure {
				request := store.last()
				store.reply(ProgressResultData{ID: "wrong", Campaign: testID, Status: StorageSucceeded, Revision: 7, RevisionKnown: true, PayloadDigest: oldDigest})
				store.results[request.ID] = store.results["wrong"]
				view = frame(t, service, FrameInput{})
			}
			if view.State != StateError || view.Confirmation != "" || service.id != "" {
				t.Fatalf("lookup claimed an unverified profile: %+v", view)
			}
		})
	}
}

func TestNewGameLookupDoesNotCallUnreadableProgressZero(t *testing.T) {
	for _, status := range []StorageStatus{StorageCorrupt, StorageTooNew, StorageIncompatible} {
		t.Run(string(status), func(t *testing.T) {
			service, store := sessionFixture(t)
			choose(t, service, "new_game", "")
			request := store.last()
			store.reply(ProgressResultData{ID: request.ID, Status: status})
			view := frame(t, service, FrameInput{})
			if view.State != StateError || view.Confirmation != "" {
				t.Fatalf("lookup failure: %+v", view)
			}
			if status == StorageCorrupt {
				view = choose(t, service, "new_game", "")
				if view.Confirmation != "new_game" || !strings.Contains(view.Warning, "неизвест") || strings.Contains(view.Warning, "0/100") {
					t.Fatalf("unknown progress misrepresented: %+v", view)
				}
			}
		})
	}
}

func TestCorruptCommitCannotBeReplacedByNewGame(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	committed, proposal := preparedCertification(t)
	loadFixture(t, service, store, committed)
	identity := OperationID{Event: "D-HOS-SUP-01", Sequence: 1}
	if err := service.beginCommit(ctx, proposal, identity, "test", "service", StateActive); err != nil {
		t.Fatal(err)
	}
	original := store.last()
	store.reply(ProgressResultData{ID: original.ID, Campaign: testID, Operation: identity, Status: StorageCorrupt})
	view := frame(t, service, FrameInput{})
	if view.State != StateError || view.Error != SaveCorrupt || len(view.Choices) != 3 {
		t.Fatalf("unexpected recovery: %+v", view)
	}
	for _, action := range []MenuAction{"new_game", "choose_older"} {
		command, err := NewFrameCommand(ctx, FrameInput{Selections: []MenuSelection{{Action: action, Target: string(testID) + ":0"}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = service.HandleFrame(ctx, command); !errors.Is(err, InvalidAction) {
			t.Fatalf("accepted %s: %v", action, err)
		}
		if service.pending == nil || !reflect.DeepEqual(service.pending.request.Data(), original) || len(store.requests) != 2 {
			t.Fatal("retained commit was replaced")
		}
	}
}

func TestFailureWorkClearedBeforeRestoration(t *testing.T) {
	ctx := context.Background()
	for _, death := range []bool{false, true} {
		t.Run(map[bool]string{false: "checkpoint", true: "death"}[death], func(t *testing.T) {
			service, store := sessionFixture(t)
			committed, certification := preparedCertification(t)
			loadFixture(t, service, store, committed)
			transition := campaign.Transition{CampaignID: string(testID), Sequence: 1, Failure: true, Contacts: []campaign.Contact{{Kind: campaign.IncidentWaste, Detail: "reporting station"}}}
			destination := StateActive
			if death {
				transition.Death, transition.DeathDetail = campaign.DeathGameplay, "hazard"
				destination = StateDead
			}
			failed, err := service.campaign.Prepare(ctx, service.committed, transition)
			if err != nil {
				t.Fatal(err)
			}
			if err = service.beginCommit(ctx, failed, OperationID{Event: "failure", Sequence: 1}, "test", "failure", destination); err != nil {
				t.Fatal(err)
			}
			request := store.last()
			normalized, err := service.campaign.ClearWork(ctx, failed)
			if err != nil {
				t.Fatal(err)
			}
			store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: normalized})
			frame(t, service, FrameInput{})
			before := service.committed.Data(ctx)
			if before.Attempts[0].Failed == "" {
				t.Fatal("fixture did not retain failed excursion work")
			}
			if death {
				choose(t, service, "retry_death", "")
			} else {
				frame(t, service, FrameInput{Edges: []ActionEdge{{Action: settings.Pause, Kind: Pressed}}})
				choose(t, service, "return_checkpoint", "")
				choose(t, service, "confirm", "")
			}
			after := service.committed.Data(ctx)
			attempt, ok := service.committed.ActiveAttempt()
			if !ok || attempt.Number != 1 || attempt.Failed != "" || after.OperationSequence != before.OperationSequence || !reflect.DeepEqual(after.LatestOperation, before.LatestOperation) || !reflect.DeepEqual(after.Incidents, before.Incidents) || after.DebtCents != before.DebtCents || service.revision != 8 || service.digest != newDigest {
				t.Fatalf("restore lost durable state: %+v", after)
			}
			evidence := certification.Data(ctx).Receipts[0].Certification
			next := campaign.Transition{CampaignID: string(testID), Sequence: 2, Certification: &campaign.CertificationCandidate{Completed: true, Event: evidence}}
			result, err := service.campaign.Prepare(ctx, service.committed, next)
			if err != nil || result.Credits() != 1 || len(result.Data(ctx).Incidents) != len(before.Incidents) {
				t.Fatalf("restored attempt could not certify: %v", err)
			}
			if err = service.beginCommit(ctx, result, OperationID{Event: string(evidence.Deed), Sequence: 2}, "test", string(evidence.Deed), StatePause); err != nil {
				t.Fatal(err)
			}
			request = store.last()
			store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: request.Operation, Status: StorageSucceeded, Revision: 9, RevisionKnown: true, PayloadDigest: oldDigest, Snapshot: result})
			view := frame(t, service, FrameInput{})
			if view.Progress.Credits != 1 || view.Progress.DebtCents != before.DebtCents || !reflect.DeepEqual(service.committed.Data(ctx).Incidents, before.Incidents) {
				t.Fatalf("certification after restoration lost history: %+v", view)
			}
		})
	}
}

func TestCandidateIdentitySeparatesFailuresFromCertification(t *testing.T) {
	deed := "D-HOS-SUP-01"
	certification := candidateIdentity(testID, 2, "service", deed, simulation.ServicePending, false)
	failure := candidateIdentity(testID, 1, "service", "failed: "+deed, simulation.ServiceFailed, false)
	if certification != deed || failure == deed || failure != candidateIdentity(testID, 1, "service", "failed: "+deed, simulation.ServiceFailed, false) {
		t.Fatalf("incorrect event identities: %q, %q", certification, failure)
	}
}

func TestSessionCommitAcknowledgmentAndTickFreeze(t *testing.T) {
	ctx := context.Background()
	service, store := sessionFixture(t)
	committed, proposal := preparedCertification(t)
	loadFixture(t, service, store, committed)
	operation := OperationID{Event: "D-HOS-SUP-01", Sequence: 1}
	if err := service.beginCommit(ctx, proposal, operation, "test", "D-HOS-SUP-01", StateActive); err != nil {
		t.Fatal(err)
	}
	request := store.last()
	_, before := service.world.Snapshot(ctx).Data(ctx)
	view := frame(t, service, FrameInput{Elapsed: 5 * time.Second})
	_, after := service.world.Snapshot(ctx).Data(ctx)
	if view.State != StateSaving || !view.Slow || view.Progress.Credits != 0 || after.Tick != before.Tick {
		t.Fatalf("pending leaked progress or ticks: %+v", view)
	}
	store.reply(ProgressResultData{ID: request.ID, Campaign: testID, Operation: OperationID{Event: "wrong", Sequence: 1}, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: proposal})
	view = frame(t, service, FrameInput{Elapsed: time.Second})
	if view.State != StateError || view.Progress.Credits != 0 || view.Error != StorageConflict || len(view.Choices) != 3 {
		t.Fatalf("mismatched result published: %+v", view)
	}
	_, after = service.world.Snapshot(ctx).Data(ctx)
	if after.Tick != before.Tick {
		t.Fatal("tick advanced after failure")
	}
	choose(t, service, "retry_save", "")
	resolved := store.last()
	if resolved.Kind != ResolveProgress || resolved.Operation != operation || resolved.ExpectedRevision != 7 || resolved.ExpectedDigest != oldDigest || !reflect.DeepEqual(resolved.Proposal.Data(ctx), proposal.Data(ctx)) {
		t.Fatalf("retry changed request: %+v", resolved)
	}
	store.reply(ProgressResultData{ID: resolved.ID, Campaign: testID, Operation: operation, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: proposal})
	view = frame(t, service, FrameInput{})
	if view.State != StateActive || view.Progress.Credits != 1 || view.Progress.Revision != 8 || len(view.Events) != 1 || !view.Events[0].Durable {
		t.Fatalf("acknowledgment: %+v", view)
	}
	view.Progress.Receipts[0].Title = "changed"
	view.Events[0].Kind = "changed"
	if got := frame(t, service, FrameInput{}); got.Progress.Receipts[0].Title == "changed" || len(got.Events) != 0 {
		t.Fatal("published view aliases state or replayed a cue")
	}
}

func TestSessionRejectsChangedPayloadAndRevision(t *testing.T) {
	ctx := context.Background()
	for _, invalid := range []string{"snapshot", "revision", "digest"} {
		t.Run(invalid, func(t *testing.T) {
			service, store := sessionFixture(t)
			committed, proposal := preparedCertification(t)
			loadFixture(t, service, store, committed)
			identity := OperationID{Event: "D-HOS-SUP-01", Sequence: 1}
			if err := service.beginCommit(ctx, proposal, identity, "test", "service", StateActive); err != nil {
				t.Fatal(err)
			}
			request := store.last()
			result := ProgressResultData{ID: request.ID, Campaign: testID, Operation: identity, Status: StorageSucceeded, Revision: 8, RevisionKnown: true, PayloadDigest: newDigest, Snapshot: proposal}
			switch invalid {
			case "snapshot":
				result.Snapshot = committed
			case "revision":
				result.Revision = 9
			case "digest":
				result.PayloadDigest = strings.Repeat("z", 64)
			}
			store.reply(result)
			if view := frame(t, service, FrameInput{Elapsed: time.Second}); view.State != StateError || view.Progress.Credits != 0 || view.Error != StorageConflict {
				t.Fatalf("accepted %s: %+v", invalid, view)
			}
		})
	}
}

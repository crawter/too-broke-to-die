// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
)

// BossStage is the authoritative protection and attack interval.
type BossStage string

const (
	// BossDormant is the protected pre-fight state before the player enters combat.
	BossDormant BossStage = "dormant"
	// BossTelegraph protects the boss while the next attack is warned and aimed.
	BossTelegraph BossStage = "telegraph"
	// BossActive protects the boss while the committed attack executes.
	BossActive BossStage = "active"
	// BossRecovery exposes the boss unless a required institute relay protects it.
	BossRecovery BossStage = "recovery"
	// BossTransition protects the boss between health phases and interrupts its attack.
	BossTransition BossStage = "transition"
	// BossPending stops combat after zero health while durable victory awaits acknowledgment.
	BossPending BossStage = "pending"
	// BossDefeated is the inactive encounter restored or acknowledged as durably defeated.
	BossDefeated BossStage = "defeated"
)

// BossHit reports presentation feedback, not a campaign event.
type BossHit struct {
	Source string
	Result string
	Damage int
}

// BossRelay exposes an institute relay's current-attempt state.
type BossRelay struct {
	ID            string
	Bounds        Rect
	Health        int
	Required      bool
	StoppedUntil  uint64
	CooldownUntil uint64
	meleeHit      uint64
}

// BossView describes the current attack hook and durable victory status.
// Locked targets are sampled exactly once, at the end of the telegraph.
type BossView struct {
	Encounter     content.EncounterID
	Health        int
	Phase         int
	PhaseData     content.BossPhase
	AttackIndex   int
	Attack        content.BossAttack
	Stage         BossStage
	Elapsed       int
	Remaining     int
	Protected     bool
	LockedTargets []Vector
	Markers       []Vector
	Bounds        Rect
	Relays        []BossRelay
	StaggerTicks  int
	Hits          []BossHit
	Pending       *campaign.EncounterCandidate
	Confirmed     bool
}

type bossState struct {
	definition   content.BossDefinition
	bounds       Rect
	stage        BossStage
	health       int
	phase        int
	index        int
	elapsed      int
	activeMin    int
	locked       []Vector
	markers      []Vector
	relays       []BossRelay
	staggerUntil uint64
	staggered    bool
	recoveryMin  int
	recovery     uint64
	meleeHit     uint64
	hits         []BossHit
	pending      *campaign.EncounterCandidate
	committed    campaign.Snapshot
	prepared     *campaign.Snapshot
}

func cloneBoss(boss bossState) bossState {
	boss.locked = slices.Clone(boss.locked)
	boss.markers = slices.Clone(boss.markers)
	boss.relays = slices.Clone(boss.relays)
	boss.hits = slices.Clone(boss.hits)
	if boss.pending != nil {
		candidate := *boss.pending
		boss.pending = &candidate
	}
	return boss
}

func (world *World) installBoss(ctx context.Context, geometry content.BossGeometry, plan campaign.RestorationPlan, committed campaign.Snapshot) (err error) {
	defer mon.Task()(&ctx)(&err)
	definition, err := content.FindBossDefinition(ctx, geometry.Encounter)
	if err != nil {
		return err
	}
	if !plan.ResetPartialBosses || geometry.Anchor != definition.Anchor || geometry.Closure != definition.Closure {
		return fmt.Errorf("invalid boss restoration binding")
	}
	featureID := map[content.EncounterID]string{"ENC-HOS-05": "EXTRACTION-RIG", "ENC-CHU-05": "PULPIT", "ENC-MIN-05": "LIFT", "ENC-INS-05": "BROADCAST-CAGE"}[geometry.Encounter]
	var bounds Rect
	for _, feature := range geometry.Features {
		if feature.ID == featureID {
			bounds = Rect{X: feature.X, Y: feature.Y, Width: feature.Width, Height: feature.Height}
		}
	}
	if !validRect(bounds, false) {
		return fmt.Errorf("missing boss feature %s", featureID)
	}
	world.boss = bossState{definition: definition, bounds: bounds, health: definition.Health, stage: BossDormant, committed: committed}
	if geometry.Encounter == "ENC-INS-05" {
		for _, id := range []string{"A", "B"} {
			var relayBounds Rect
			for _, feature := range geometry.Features {
				if feature.ID == "RELAY-"+id {
					relayBounds = Rect{X: feature.X, Y: feature.Y, Width: feature.Width, Height: feature.Height}
				}
			}
			if !validRect(relayBounds, false) {
				return fmt.Errorf("missing relay %s", id)
			}
			world.boss.relays = append(world.boss.relays, BossRelay{ID: id, Bounds: relayBounds})
		}
		world.resetBossRelays()
	}
	if slices.Contains(plan.Defeated, geometry.Encounter) {
		world.boss.health, world.boss.stage = 0, BossDefeated
	}
	return nil
}

func (boss *bossState) attack() content.BossAttack {
	if boss.definition.Encounter == "" || boss.phase < 0 || boss.phase >= len(boss.definition.Phases) {
		return content.BossAttack{}
	}
	order := boss.definition.Phases[boss.phase].Attacks
	if boss.index < 0 || boss.index >= len(order) {
		return content.BossAttack{}
	}
	for _, attack := range boss.definition.Attacks {
		if attack.ID == order[boss.index] {
			attack.ProjectileHeights = slices.Clone(attack.ProjectileHeights)
			attack.TargetLevels = slices.Clone(attack.TargetLevels)
			return attack
		}
	}
	return content.BossAttack{}
}

func (boss *bossState) duration() int {
	attack := boss.attack()
	switch boss.stage {
	case BossTelegraph:
		return msTicks(attack.TelegraphMS)
	case BossActive:
		return max(msTicks(attack.ActiveMS), boss.activeMin)
	case BossRecovery:
		return max(msTicks(attack.RecoveryMS), boss.recoveryMin)
	case BossTransition:
		return msTicks(boss.definition.TransitionMS)
	}
	return 0
}

// Boss returns a detached view; pending is not a confirmed victory.
func (world *World) Boss(ctx context.Context) BossView {
	defer mon.Task()(&ctx)(nil)
	if world == nil {
		return BossView{}
	}
	boss := cloneBoss(world.boss)
	stagger := 0
	if boss.staggerUntil > world.state.Tick {
		stagger = int(boss.staggerUntil - world.state.Tick)
	}
	var phase content.BossPhase
	if boss.phase >= 0 && boss.phase < len(boss.definition.Phases) {
		phase = boss.definition.Phases[boss.phase]
		phase.Attacks = slices.Clone(phase.Attacks)
		phase.Relays = slices.Clone(phase.Relays)
	}
	return BossView{Encounter: boss.definition.Encounter, Health: boss.health, Phase: boss.phase + 1, PhaseData: phase, AttackIndex: boss.index, Attack: boss.attack(), Stage: boss.stage, Elapsed: boss.elapsed, Remaining: max(0, boss.duration()-boss.elapsed), Protected: !boss.exposed(), LockedTargets: boss.locked, Markers: boss.markers, Bounds: boss.bounds, Relays: boss.relays, StaggerTicks: stagger, Hits: boss.hits, Pending: boss.pending, Confirmed: boss.stage == BossDefeated}
}

// StepBoss executes one encounter tick with the normal player, interaction,
// projectile and damage systems. It stops on a pending durable victory. Attack
// geometry and institute relay behavior belong to encounter-specific executors.
func (world *World) StepBoss(ctx context.Context, command ObjectCommand) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil || world.boss.definition.Encounter == "" || world.boss.stage == BossPending {
		return fmt.Errorf("boss tick unavailable")
	}
	if world.boss.stage == BossDefeated {
		return world.StepObjects(ctx, command)
	}
	before, previous, motion, grid := cloneState(world.state), cloneState(world.previous), world.motion, world.grid
	combat, limbs, feces, objects, boss := cloneCombat(world.combat), cloneLimbs(world.limbs), cloneFeces(world.feces), cloneObjects(world.objects), cloneBoss(world.boss)
	defer func() {
		if err != nil {
			world.state, world.previous, world.motion, world.grid = before, previous, motion, grid
			world.combat, world.limbs, world.feces, world.objects, world.boss = combat, limbs, feces, objects, boss
		}
	}()
	world.boss.hits = nil
	priorAttack, priorAction := world.combat.attack, world.motion.action
	if world.boss.stage != BossDormant {
		collider := Collider{ID: string(world.boss.definition.Encounter), Kind: ProjectileTarget, Bounds: world.boss.bounds}
		command.Feces.Limb.Targets = append(slices.Clone(command.Feces.Limb.Targets), collider)
		command.Feces.Targets = append(slices.Clone(command.Feces.Targets), FecesTarget{Collider: collider, Kind: FecesBoss, Exposed: world.boss.exposed(), Recovery: fmt.Sprint(world.boss.recovery)})
		for _, relay := range world.boss.relays {
			if !relay.Required || relay.Health <= 0 {
				continue
			}
			collider := Collider{ID: "boss-relay-" + relay.ID, Kind: ProjectileTarget, Bounds: relay.Bounds}
			command.Feces.Limb.Targets = append(command.Feces.Limb.Targets, collider)
			command.Feces.Targets = append(command.Feces.Targets, FecesTarget{Collider: collider, Kind: FecesSource})
		}
	}
	if err = world.StepObjects(ctx, command); err != nil {
		return err
	}
	if world.boss.stage == BossDormant {
		body := world.state.Player.Bounds
		if centerX(body) >= 112 && centerX(body) < 1168 && (world.boss.definition.Encounter != "ENC-MIN-05" || body.Y >= 160) {
			world.boss.stage, world.boss.elapsed = BossTelegraph, 0
		}
		return nil
	}
	if err = world.resolveBossImpacts(ctx, priorAttack, priorAction); err != nil {
		return err
	}
	if err = world.resolveBossRelays(ctx, priorAttack, priorAction); err != nil {
		return err
	}
	if world.boss.health == 0 {
		world.boss.stage = BossPending
		world.boss.pending = &campaign.EncounterCandidate{Completed: true, Encounter: world.boss.definition.Encounter}
		world.clearBossProjectiles()
		return nil
	}
	if world.boss.phase < 2 && world.boss.health <= world.boss.definition.Thresholds[world.boss.phase] && world.boss.stage != BossTransition {
		world.beginBossTransition()
		return nil
	}
	world.advanceBoss()
	return nil
}

// ClosureInteraction returns a candidate only for the authored terminal's
// completed hold after the corresponding boss victory was acknowledged.
func (world *World) ClosureInteraction(ctx context.Context, manifest *content.Campaign) (_ *campaign.ClosureCandidate, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || manifest == nil || world.boss.stage != BossDefeated {
		return nil, nil
	}
	for _, deed := range manifest.Deeds(ctx) {
		if deed.Family != "END" || deed.Deed.Encounter != world.boss.definition.Encounter {
			continue
		}
		if len(deed.Deed.Objects) != 1 {
			return nil, fmt.Errorf("invalid closure terminal %s", deed.Deed.ID)
		}
		if slices.Contains(world.state.Receipts, deed.Deed.ID) {
			return nil, nil
		}
		for _, event := range world.objects.events {
			if event.Kind == "completed" && event.Target == string(deed.Deed.Objects[0]) {
				return &campaign.ClosureCandidate{Deed: deed.Deed.ID, Terminal: deed.Deed.Objects[0], HoldMilliseconds: deed.Deed.HoldMilliseconds}, nil
			}
		}
	}
	return nil, nil
}

// PrepareBoss combines the stable victory candidate with other same-tick
// candidates. The campaign assigns the occurrence once per operation sequence.
func (world *World) PrepareBoss(ctx context.Context, owner *campaign.Campaign, committed campaign.Snapshot, transition campaign.Transition) (_ campaign.Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || owner == nil || world.boss.pending == nil || transition.Encounter != nil {
		return campaign.Snapshot{}, fmt.Errorf("no pending boss victory")
	}
	if !reflect.DeepEqual(committed.Data(ctx), world.boss.committed.Data(ctx)) && (world.boss.prepared == nil || !reflect.DeepEqual(committed.Data(ctx), world.boss.prepared.Data(ctx))) {
		return campaign.Snapshot{}, fmt.Errorf("boss campaign revision mismatch")
	}
	candidate := *world.boss.pending
	transition.Encounter = &candidate
	if world.motion.dead {
		if transition.Death != campaign.DeathNone && transition.Death != campaign.DeathGameplay {
			return campaign.Snapshot{}, fmt.Errorf("conflicting death candidate")
		}
		transition.Death = campaign.DeathGameplay
		if transition.DeathDetail == "" {
			transition.DeathDetail = "boss encounter"
		}
	}
	proposal, err := owner.Prepare(ctx, committed, transition)
	if err != nil {
		return campaign.Snapshot{}, err
	}
	if world.boss.prepared != nil && !reflect.DeepEqual(world.boss.prepared.Data(ctx), proposal.Data(ctx)) {
		return campaign.Snapshot{}, fmt.Errorf("boss proposal differs from pending save")
	}
	world.boss.prepared = &proposal
	return proposal, nil
}

// AcknowledgeBoss publishes only the exact proposal acknowledged by storage.
func (world *World) AcknowledgeBoss(ctx context.Context, owner *campaign.Campaign, result ServiceAcknowledgment) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || owner == nil || world.boss.prepared == nil || world.boss.pending == nil || !result.Succeeded {
		return fmt.Errorf("boss save not acknowledged")
	}
	data := result.Snapshot.Data(ctx)
	if result.CampaignID == "" || result.CampaignID != data.CampaignID || result.Sequence == 0 || result.Sequence != data.OperationSequence || !reflect.DeepEqual(data, world.boss.prepared.Data(ctx)) {
		return fmt.Errorf("boss acknowledgment mismatch")
	}
	plan, err := owner.RestorePlan(ctx, result.Snapshot)
	if err != nil {
		return err
	}
	if !slices.Contains(plan.Defeated, world.boss.definition.Encounter) {
		return fmt.Errorf("boss absent from restoration plan")
	}
	for _, emergency := range plan.Emergencies {
		if emergency.WarningMilliseconds <= 0 || emergency.WarningMilliseconds > 3600000 || !emergency.BehindSafeBoundary || !emergency.BypassUsable {
			return fmt.Errorf("invalid emergency restoration %s", emergency.Emergency.ID)
		}
	}
	world.state.Defeated = slices.Clone(plan.Defeated)
	world.state.Anchor = plan.Anchor
	world.state.Routes = slices.Clone(plan.Routes)
	world.state.ItemRoutes = slices.Clone(plan.ItemRoutes)
	world.state.OwnedItems = slices.Clone(plan.OwnedItems)
	world.state.Installed = slices.Clone(plan.Installed)
	world.state.Receipts = nil
	for _, receipt := range plan.Receipts {
		world.state.Receipts = append(world.state.Receipts, receipt.Certification.Deed)
	}
	slices.Sort(world.state.Receipts)
	for _, emergency := range plan.Emergencies {
		found := false
		for i := range world.state.Emergencies {
			existing := &world.state.Emergencies[i]
			if existing.ID == emergency.Emergency.ID {
				if existing.Stage != emergency.Emergency.Stage {
					existing.Stage = emergency.Emergency.Stage
					existing.WarningTicks = 0
				}
				found = true
				break
			}
		}
		if !found {
			warning := 0
			if emergency.Emergency.Stage == campaign.EmergencyActive {
				warning = msTicks(int(emergency.WarningMilliseconds))
			}
			world.state.Emergencies = append(world.state.Emergencies, EmergencyState{ID: emergency.Emergency.ID, Encounter: emergency.Encounter, Stage: emergency.Emergency.Stage, WarningTicks: warning, SafeBoundary: emergency.BehindSafeBoundary, BypassUsable: emergency.BypassUsable})
		}
	}
	slices.SortFunc(world.state.Emergencies, func(a, b EmergencyState) int { return compareID(string(a.ID), string(b.ID)) })
	world.state.BossActive = false
	world.boss.stage, world.boss.pending, world.boss.prepared = BossDefeated, nil, nil
	world.boss.committed = result.Snapshot
	return nil
}

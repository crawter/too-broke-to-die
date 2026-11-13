// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"too-broke-to-die/internal/tbtd/content"
)

// clearBossProjectiles removes only this encounter's attack projectiles;
// player throws and other entities' projectiles keep their identities.
func (world *World) clearBossProjectiles() {
	id := string(world.boss.definition.Encounter)
	world.combat.projectiles = slices.DeleteFunc(world.combat.projectiles, func(projectile Projectile) bool { return !projectile.PlayerOwned && projectile.Attacker == id })
}

func (world *World) beginBossTransition() {
	world.clearBossProjectiles()
	world.boss.phase++
	world.boss.index = 0
	world.boss.stage = BossTransition
	world.boss.elapsed = 0
	world.boss.activeMin = 0
	world.boss.locked = nil
	world.boss.markers = nil
	world.boss.staggerUntil = 0
	world.boss.staggered = false
	world.boss.recoveryMin = 0
	world.resetBossRelays()
}

// advanceBoss is measured by StepBoss. If a single impact crosses both
// thresholds, each transition receives its full protection interval in order.
func (world *World) advanceBoss() {
	boss := &world.boss
	boss.elapsed++
	if boss.elapsed < boss.duration() {
		return
	}
	boss.elapsed = 0
	switch boss.stage {
	case BossTelegraph:
		if len(boss.markers) > 0 {
			boss.locked = slices.Clone(boss.markers)
		} else {
			body := world.state.Player.Bounds
			boss.locked = []Vector{{X: centerX(body), Y: centerY(body)}}
		}
		boss.markers = nil
		boss.activeMin = 0
		boss.stage = BossActive
	case BossActive:
		boss.activeMin = 0
		boss.stage = BossRecovery
		boss.recovery++
		boss.staggered = false
		boss.staggerUntil = 0
		boss.recoveryMin = 0
	case BossRecovery:
		boss.index = (boss.index + 1) % len(boss.definition.Phases[boss.phase].Attacks)
		boss.stage = BossTelegraph
		boss.locked = nil
		boss.staggerUntil = 0
	case BossTransition:
		if boss.phase < 2 && boss.health <= boss.definition.Thresholds[boss.phase] {
			world.beginBossTransition()
			return
		}
		boss.stage = BossTelegraph
	}
}

func (world *World) bossImpact(source string, damage int, stagger bool) {
	boss := &world.boss
	if boss.health <= 0 {
		return
	}
	if !boss.exposed() {
		boss.hits = append(boss.hits, BossHit{Source: source, Result: "protected"})
		return
	}
	boss.health = max(0, boss.health-damage)
	boss.hits = append(boss.hits, BossHit{Source: source, Result: "exposed", Damage: damage})
	if stagger && !boss.staggered {
		boss.staggered = true
		// One stagger can extend the recovery only enough to show its full
		// 250 milliseconds; subsequent impacts cannot extend it again.
		boss.recoveryMin = max(boss.recoveryMin, boss.elapsed+30)
		boss.staggerUntil = world.state.Tick + 30
	}
}

// resolveBossImpacts uses the inherited swept throws and actual melee shape.
// The previous attack preserves an already-active strike if player damage in
// this same tick cleared the inherited combat state.
func (world *World) resolveBossImpacts(ctx context.Context, previous PlayerAttack, previousAction ActionKind) (err error) {
	defer mon.Task()(&ctx)(&err)
	boss := &world.boss
	attack := world.combat.attack
	if attack.ID == 0 && previousAction == AttackAction && previous.ID != 0 && !previous.Throw && world.state.Tick >= previous.Start+15 && world.state.Tick < previous.Start+25 {
		attack = previous
		attack.Phase = PhaseActive
	}
	if attack.ID != 0 && !attack.Throw && attack.Phase == PhaseActive && boss.meleeHit != attack.ID {
		body := world.state.Player.Bounds
		area := Rect{X: body.X + body.Width, Y: body.Y + (body.Height-36)/2, Width: attack.Reach, Height: 36}
		if attack.Facing < 0 {
			area.X = body.X - attack.Reach
		}
		if !world.protected(body) && !world.protected(area) && overlaps(area, boss.bounds) {
			clear, clearErr := world.strikeClear(ctx, body, area, boss.bounds, attack.Facing)
			if clearErr != nil {
				return clearErr
			}
			if clear {
				boss.meleeHit = attack.ID
				world.bossImpact("melee", attack.Damage, false)
			}
		}
	}
	id := string(boss.definition.Encounter)
	for _, event := range world.limbs.events {
		if event.Kind != "impact" || event.Target != id {
			continue
		}
		damage := 35
		if socketMask(event.Socket)&(LeftArm|RightArm) != 0 {
			damage = 25
		}
		world.bossImpact("limb", damage, false)
	}
	for _, impact := range world.feces.impacts {
		if impact.Target != id || (impact.Kind != "boss_exposed" && impact.Kind != "boss_protected") {
			continue
		}
		world.bossImpact("feces", 8, true)
	}
	return nil
}

// prepareBossMarkers is the in-package encounter hook for marked circles,
// lanes, levels or trajectories. The last telegraph sample is copied at the
// boundary and cannot be changed during the committed active attack.
func (world *World) prepareBossMarkers(ctx context.Context, markers []Vector) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.boss.stage != BossTelegraph || len(markers) != world.boss.attack().TargetCount {
		return fmt.Errorf("invalid boss target markers")
	}
	for _, marker := range markers {
		if !validVector(marker) {
			return fmt.Errorf("invalid boss target position")
		}
	}
	world.boss.markers = slices.Clone(markers)
	return nil
}

// extendBossActive lets an encounter executor retain protection until its
// committed physical sweep or projectile release sequence has completed.
// The authored minimum remains in force and every interval is bounded.
func (world *World) extendBossActive(ctx context.Context, ticks int) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.boss.stage != BossActive || ticks < world.boss.duration() || ticks > msTicks(3600000) {
		return fmt.Errorf("invalid boss active duration")
	}
	world.boss.activeMin = ticks
	return nil
}

// exposed includes institute relay protection, not merely the timed recovery.
func (boss *bossState) exposed() bool {
	if boss.stage != BossRecovery {
		return false
	}
	for _, relay := range boss.relays {
		if relay.Required && relay.Health > 0 {
			return false
		}
	}
	return true
}

func (world *World) resetBossRelays() {
	boss := &world.boss
	for i := range boss.relays {
		relay := &boss.relays[i]
		relay.Required = slices.Contains(boss.definition.Phases[boss.phase].Relays, relay.ID)
		if relay.Required {
			relay.Health = boss.definition.RelayHealth
			relay.StoppedUntil, relay.CooldownUntil, relay.meleeHit = 0, 0, 0
		}
	}
}

func (world *World) resolveBossRelays(ctx context.Context, previous PlayerAttack, previousAction ActionKind) (err error) {
	defer mon.Task()(&ctx)(&err)
	attack := world.combat.attack
	if attack.ID == 0 && previousAction == AttackAction && previous.ID != 0 && !previous.Throw && world.state.Tick >= previous.Start+15 && world.state.Tick < previous.Start+25 {
		attack = previous
		attack.Phase = PhaseActive
	}
	for i := range world.boss.relays {
		relay := &world.boss.relays[i]
		if !relay.Required || relay.Health <= 0 {
			continue
		}
		id := "boss-relay-" + relay.ID
		if attack.ID != 0 && !attack.Throw && attack.Phase == PhaseActive && relay.meleeHit != attack.ID {
			body := world.state.Player.Bounds
			area := Rect{X: body.X + body.Width, Y: body.Y + (body.Height-36)/2, Width: attack.Reach, Height: 36}
			if attack.Facing < 0 {
				area.X = body.X - attack.Reach
			}
			if !world.protected(body) && !world.protected(area) && overlaps(area, relay.Bounds) {
				clear, clearErr := world.strikeClear(ctx, body, area, relay.Bounds, attack.Facing)
				if clearErr != nil {
					return clearErr
				}
				if clear {
					relay.Health = max(0, relay.Health-attack.Damage)
					relay.meleeHit = attack.ID
				}
			}
		}
		for _, event := range world.limbs.events {
			if event.Kind == "impact" && event.Target == id {
				damage := 35
				if socketMask(event.Socket)&(LeftArm|RightArm) != 0 {
					damage = 25
				}
				relay.Health = max(0, relay.Health-damage)
			}
		}
		for _, impact := range world.feces.impacts {
			if impact.Kind == "source" && impact.Target == id && world.state.Tick >= relay.CooldownUntil && relay.Health > 0 {
				relay.StoppedUntil = world.state.Tick + 240
				relay.CooldownUntil = world.state.Tick + 480
			}
		}
	}
	return nil
}

// BossRecoveryID identifies an exposed interval for attack-specific hooks.
func (world *World) BossRecoveryID(ctx context.Context) string {
	defer mon.Task()(&ctx)(nil)
	if world == nil || world.boss.stage != BossRecovery {
		return ""
	}
	return fmt.Sprintf("%s-%d", world.boss.definition.Encounter, world.boss.recovery)
}

// BossPresentation is supplementary Russian-facing encounter data. Attack
// mechanics remain owned by content.BossDefinition, not these labels. Missing
// final media is not a claim of presentation or rights acceptance.
type BossPresentation struct {
	SchemaVersion int                 `json:"schema_version"`
	Encounter     content.EncounterID `json:"encounter"`
	Actor         string              `json:"actor"`
	Name          string              `json:"name"`
	Adult         bool                `json:"adult"`
	Appearance    string              `json:"appearance"`
	Aftermath     string              `json:"aftermath"`
	MediaStatus   string              `json:"media_status"`
}

func validateBossPresentation(ctx context.Context, presentation BossPresentation) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return err
	}
	definition, err := content.FindBossDefinition(ctx, presentation.Encounter)
	if err != nil {
		return err
	}
	var actor, name string
	switch definition.Encounter {
	case "ENC-HOS-05":
		actor, name = "CHR-POT", "Ренат Потрошин"
	case "ENC-CHU-05":
		actor, name = "CHR-SAV", "Отец Савва"
	case "ENC-MIN-05":
		actor, name = "CHR-BLA", "Аркадий Благоев"
	case "ENC-INS-05":
		actor, name = "CHR-LEV", "Лев Виральский"
	default:
		return fmt.Errorf("unsupported boss presentation %s", definition.Encounter)
	}
	if presentation.SchemaVersion != 1 || presentation.Actor != actor || presentation.Name != name || !presentation.Adult || presentation.MediaStatus != "missing" || strings.TrimSpace(presentation.Appearance) == "" || strings.TrimSpace(presentation.Aftermath) == "" || strings.ContainsRune(presentation.Appearance+presentation.Aftermath, utf8.RuneError) {
		return fmt.Errorf("invalid presentation for %s", definition.Encounter)
	}
	return nil
}

// LoadBossPresentation reads strictly bounded, supplemental authored text from
// an injected filesystem. It never treats absent art or rights as delivered.
func LoadBossPresentation(ctx context.Context, filesystem fs.FS, name string) (_ BossPresentation, err error) {
	defer mon.Task()(&ctx)(&err)
	if filesystem == nil || !fs.ValidPath(name) {
		return BossPresentation{}, fmt.Errorf("invalid presentation path")
	}
	if err = ctx.Err(); err != nil {
		return BossPresentation{}, err
	}
	file, err := filesystem.Open(name)
	if err != nil {
		return BossPresentation{}, fmt.Errorf("open boss presentation: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 65537))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return BossPresentation{}, fmt.Errorf("read boss presentation: %w", errors.Join(readErr, closeErr))
	}
	if len(data) > 65536 || !utf8.Valid(data) {
		return BossPresentation{}, fmt.Errorf("invalid boss presentation encoding or size")
	}
	// The document is a flat object; check spelling and duplicate keys before
	// typed decoding, since encoding/json otherwise accepts both silently.
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return BossPresentation{}, fmt.Errorf("invalid boss presentation object")
	}
	allowed := []string{"schema_version", "encounter", "actor", "name", "adult", "appearance", "aftermath", "media_status"}
	seen := make(map[string]bool, len(allowed))
	for decoder.More() {
		keyToken, keyErr := decoder.Token()
		key, ok := keyToken.(string)
		if keyErr != nil || !ok || !slices.Contains(allowed, key) || seen[key] {
			return BossPresentation{}, fmt.Errorf("invalid boss presentation field")
		}
		seen[key] = true
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil || bytes.Equal(value, []byte("null")) {
			return BossPresentation{}, fmt.Errorf("invalid boss presentation value")
		}
	}
	if _, err = decoder.Token(); err != nil || len(seen) != len(allowed) {
		return BossPresentation{}, fmt.Errorf("incomplete boss presentation")
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return BossPresentation{}, fmt.Errorf("trailing boss presentation data")
	}
	var presentation BossPresentation
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&presentation); err != nil {
		return BossPresentation{}, fmt.Errorf("decode boss presentation: %w", err)
	}
	if err = validateBossPresentation(ctx, presentation); err != nil {
		return BossPresentation{}, err
	}
	return presentation, nil
}

// SetPresentation binds validated labels to the matching live encounter. This
// is optional for simulation and does not certify any missing media.
func (arena *BossArena) SetPresentation(ctx context.Context, presentation BossPresentation) (err error) {
	defer mon.Task()(&ctx)(&err)
	if arena == nil || arena.world == nil || arena.world.boss.definition.Encounter != presentation.Encounter {
		return fmt.Errorf("boss presentation encounter mismatch")
	}
	if err = validateBossPresentation(ctx, presentation); err != nil {
		return err
	}
	arena.presentation = &presentation
	return nil
}

// ArenaProjectile is a committed confession projectile. Coordinates and
// velocity use the same units as the world collision solver.
type ArenaProjectile struct {
	ID       string
	Position Vector
	Velocity Vector
	// Remaining bounds a committed projectile's flight, including a missed target.
	Remaining int
}

// BossArenaView exposes encounter-specific markers without making them a
// durable campaign result. Aftermath flags require the shared save acknowledgment.
type BossArenaView struct {
	Boss                 BossView
	Presentation         *BossPresentation
	Danger               []Rect
	Projectiles          []ArenaProjectile
	Conveyors            []Rect
	Screen               string
	OrganShortage        bool
	SpecimenBurst        bool
	Survived             bool
	DriveStopped         bool
	WardBypass           bool
	ArchiveOpen          bool
	KioskAccessible      bool
	WasteHeight          int
	WasteDamage          int
	LiftJammed           bool
	SanitationExercise   bool
	BroadcastStopped     bool
	SummonsHarmless      bool
	FinalPanelAccessible bool
	ProtectedRelays      []string
	Followers            int
	FollowerQueue        []RecruitmentReplacement
	FollowerSpawn        Rect
	Warning              string
}

// BossArena executes the local physical attacks around the shared boss state
// machine. A newly restored World needs a newly constructed BossArena.
type BossArena struct {
	world            *World
	projectiles      []ArenaProjectile
	presentation     *BossPresentation
	followerQueue    []RecruitmentReplacement
	followerSeen     map[string]bool
	followerNext     uint64
	followersStarted bool
}

// NewBossArena binds an encounter executor to a restored boss world.
func NewBossArena(ctx context.Context, world *World) (_ *BossArena, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil || !slices.Contains([]content.EncounterID{"ENC-HOS-05", "ENC-CHU-05", "ENC-MIN-05", "ENC-INS-05"}, world.boss.definition.Encounter) {
		return nil, fmt.Errorf("boss world required")
	}
	if world.boss.stage != BossDormant && world.boss.stage != BossDefeated {
		return nil, fmt.Errorf("boss arena must be bound at restoration")
	}
	arena := &BossArena{world: world}
	arena.moveInstituteCage()
	return arena, nil
}

// View returns detached warning shapes, projectiles and acknowledged aftermath.
func (arena *BossArena) View(ctx context.Context) BossArenaView {
	defer mon.Task()(&ctx)(nil)
	if arena == nil || arena.world == nil {
		return BossArenaView{}
	}
	world := arena.world
	view := BossArenaView{Boss: world.Boss(ctx), Projectiles: slices.Clone(arena.projectiles)}
	if arena.presentation != nil {
		presentation := *arena.presentation
		view.Presentation = &presentation
	}
	if world.boss.stage == BossTelegraph {
		view.Danger = bossWarningAreas(world.boss.attack(), world.boss.markers)
	} else if world.boss.stage == BossActive {
		view.Danger = bossWarningAreas(world.boss.attack(), world.boss.locked)
		if world.boss.attack().ID == "HOS-B" && len(world.boss.locked) == 1 {
			view.Danger = []Rect{hospitalTrolley(world.boss.locked[0], world.boss.elapsed)}
		}
	}
	if world.boss.definition.Encounter == "ENC-HOS-05" && world.boss.stage != BossDefeated && world.boss.stage != BossPending {
		view.Conveyors = hospitalConveyors(world.boss.phase)
		view.OrganShortage = world.boss.stage == BossTransition && world.boss.phase == 2
		view.SpecimenBurst = world.boss.stage == BossRecovery
	}
	if world.boss.definition.Encounter == "ENC-CHU-05" {
		switch world.boss.phase {
		case 1:
			view.Screen = "private_confessions"
		case 2:
			view.Screen = "paid_scandal_advertisements"
		}
	}
	if world.boss.definition.Encounter == "ENC-MIN-05" {
		if world.boss.stage == BossTelegraph || world.boss.stage == BossActive {
			view.Warning = map[string]string{
				"MIN-A": "Благополучие растёт — выброс отходов",
				"MIN-B": "Санитарный прорыв — опасная струя",
				"MIN-C": "Отчёт заверен — удар штампа",
			}[world.boss.attack().ID]
		}
		view.WasteHeight = world.boss.definition.Phases[world.boss.phase].WasteHeight
		view.WasteDamage = 15
		if world.boss.stage == BossPending || world.boss.stage == BossDefeated {
			view.WasteHeight, view.WasteDamage = 0, 0
		}
	}
	if world.boss.definition.Encounter == "ENC-INS-05" {
		for _, relay := range world.boss.relays {
			if relay.Required && relay.Health > 0 {
				view.ProtectedRelays = append(view.ProtectedRelays, "Реле "+map[string]string{"A": "А", "B": "Б"}[relay.ID])
			}
		}
		view.Followers = arena.liveFollowers()
		view.FollowerQueue = slices.Clone(arena.followerQueue)
		view.FollowerSpawn = Rect{X: 1040 - PlayerWidth/2, Y: 0, Width: PlayerWidth, Height: PlayerHeight}
		view.SummonsHarmless = world.boss.stage == BossPending || world.boss.stage == BossDefeated
	}
	if world.boss.stage == BossDefeated {
		view.Survived = true
		switch world.boss.definition.Encounter {
		case "ENC-HOS-05":
			view.DriveStopped, view.WardBypass = true, true
		case "ENC-CHU-05":
			view.ArchiveOpen, view.KioskAccessible = true, true
		case "ENC-MIN-05":
			view.LiftJammed, view.SanitationExercise = true, true
		case "ENC-INS-05":
			view.BroadcastStopped, view.FinalPanelAccessible = true, true
		}
	}
	return view
}

// Step runs the real shared boss tick and resolves local attacks with the
// inherited damage and collision rules. Failure leaves the world unchanged.
func (arena *BossArena) Step(ctx context.Context, command ObjectCommand) (err error) {
	defer mon.Task()(&ctx)(&err)
	if arena == nil || arena.world == nil {
		return fmt.Errorf("missing boss arena")
	}
	world := arena.world
	before := *world
	before.state, before.previous = cloneState(world.state), cloneState(world.previous)
	before.combat, before.limbs, before.feces = cloneCombat(world.combat), cloneLimbs(world.limbs), cloneFeces(world.feces)
	before.objects, before.boss = cloneObjects(world.objects), cloneBoss(world.boss)
	shots := slices.Clone(arena.projectiles)
	queue := slices.Clone(arena.followerQueue)
	seen := make(map[string]bool, len(arena.followerSeen))
	for id, value := range arena.followerSeen {
		seen[id] = value
	}
	next, started := arena.followerNext, arena.followersStarted
	defer func() {
		if err != nil {
			*world = before
			arena.projectiles, arena.followerQueue, arena.followerSeen = shots, queue, seen
			arena.followerNext, arena.followersStarted = next, started
		}
	}()
	if world.boss.stage == BossPending {
		return fmt.Errorf("boss victory awaits acknowledgment")
	}
	if err = arena.carryConveyor(ctx); err != nil {
		return err
	}
	arena.moveHospitalRig()
	arena.moveInstituteCage()
	if world.boss.stage == BossActive {
		switch world.boss.attack().ID {
		case "HOS-B":
			// 1,013 pixels of travel at three pixels per tick; neither end
			// of the trolley enters a protected alcove.
			err = world.extendBossActive(ctx, 338)
		case "HOS-C", "CHU-B":
			// The impact follows the locked warning rather than striking on
			// the very first tick. Players can actually leave the marker.
			err = world.extendBossActive(ctx, 48)
		case "CHU-C":
			err = world.extendBossActive(ctx, 436)
		case "MIN-A":
			err = world.extendBossActive(ctx, 270)
		case "MIN-B", "INS-B":
			err = world.extendBossActive(ctx, 72)
		case "MIN-C":
			// The marked half may be chosen at either arena edge. Allow an
			// intact runner to reach the opposite half after target lock.
			err = world.extendBossActive(ctx, 270)
		case "INS-A":
			err = world.extendBossActive(ctx, 48)
		case "INS-C":
			err = world.extendBossActive(ctx, 120)
		}
		if err != nil {
			return err
		}
	}
	// A trial on an isolated copy samples the player's post-movement location
	// at the exact telegraph boundary and predicts local contacts for the
	// inherited, identity-ordered damage resolver.
	trial := world.copyEncounterWorld(ctx)
	trial.boss = cloneBoss(world.boss)
	if err = trial.StepBoss(ctx, command); err != nil {
		return err
	}
	if world.boss.stage == BossTelegraph {
		markers := localBossMarkers(world.boss.attack(), trial.state.Player.Bounds)
		if err = world.prepareBossMarkers(ctx, markers); err != nil {
			return err
		}
	}
	if world.boss.stage == BossActive && trial.boss.stage != BossPending && trial.boss.stage != BossTransition {
		var hits []DamageHit
		hits, err = arena.localHits(ctx, before.state.Player.Bounds, trial.state.Player.Bounds)
		if err != nil {
			return err
		}
		command.Feces.Limb.Combat.Hits = append(slices.Clone(command.Feces.Limb.Combat.Hits), hits...)
	}
	if world.boss.definition.Encounter == "ENC-MIN-05" && trial.boss.stage != BossPending && trial.boss.stage != BossDefeated {
		if hit, ok := arena.sumpHit(before.state.Player.Bounds, trial.state.Player.Bounds); ok {
			command.Feces.Limb.Combat.Hits = append(slices.Clone(command.Feces.Limb.Combat.Hits), hit)
		}
	}
	if world.boss.definition.Encounter == "ENC-INS-05" && trial.boss.stage == BossPending {
		arena.stopFollowers()
	}
	queued := len(arena.followerQueue)
	if err = world.StepBoss(ctx, command); err != nil {
		return err
	}
	arena.moveInstituteCage()
	// A warning must already be visible on the first telegraph frame, not
	// only after its first timer tick. Later samples still lock at its end.
	if world.boss.stage == BossTelegraph && world.boss.elapsed == 0 {
		if err = world.prepareBossMarkers(ctx, localBossMarkers(world.boss.attack(), world.state.Player.Bounds)); err != nil {
			return err
		}
	}
	if world.boss.definition.Encounter == "ENC-INS-05" {
		if err = arena.advanceFollowers(ctx, queued, before.boss.phase); err != nil {
			return err
		}
	}
	// Preserve the pre-conveyor state for interpolation, not a partial tick.
	world.previous = before.state
	if world.boss.stage == BossTransition || world.boss.stage == BossPending || world.boss.stage == BossDefeated {
		arena.projectiles = nil
	}
	return nil
}

func localBossMarkers(attack content.BossAttack, player Rect) []Vector {
	x := centerX(player)
	switch attack.ID {
	case "HOS-A":
		return []Vector{{X: math.Max(232, math.Min(1048, x))}}
	case "HOS-B":
		return []Vector{{X: x}}
	case "HOS-C":
		x = math.Max(320, math.Min(960, x))
		return []Vector{{X: x - 160}, {X: x}, {X: x + 160}}
	case "CHU-A":
		return []Vector{{X: math.Max(252, math.Min(1028, x))}}
	case "CHU-B":
		return []Vector{{X: math.Max(162, math.Min(1118, x))}}
	case "CHU-C":
		return []Vector{{X: x, Y: 20}, {X: x, Y: 100}, {X: x, Y: 180}}
	case "MIN-A", "MIN-B", "MIN-C":
		return ministryMarkers(attack, player)
	case "INS-A", "INS-B", "INS-C":
		return instituteMarkers(attack, player)
	}
	return nil
}

func bossWarningAreas(attack content.BossAttack, markers []Vector) []Rect {
	if strings.HasPrefix(attack.ID, "MIN-") {
		return ministryWarnings(attack, markers)
	}
	if strings.HasPrefix(attack.ID, "INS-") {
		return instituteWarnings(attack, markers)
	}
	var areas []Rect
	for _, marker := range markers {
		switch attack.ID {
		case "HOS-A", "CHU-A":
			areas = append(areas, Rect{X: marker.X - float64(attack.MarkerWidth)/2, Y: 0, Width: float64(attack.MarkerWidth), Height: 48})
		case "HOS-B":
			areas = append(areas, Rect{X: 112, Y: 0, Width: 1056, Height: 48})
		case "HOS-C":
			areas = append(areas, Rect{X: marker.X - 32, Y: 0, Width: 64, Height: 72})
		case "CHU-B":
			areas = append(areas, Rect{X: marker.X - 50, Y: 0, Width: 100, Height: 400})
		case "CHU-C":
			areas = append(areas, Rect{X: 112, Y: marker.Y - 4, Width: 1056, Height: 8})
		}
	}
	return areas
}

func (arena *BossArena) localHits(ctx context.Context, from, to Rect) (_ []DamageHit, err error) {
	defer mon.Task()(&ctx)(&err)
	world, boss := arena.world, &arena.world.boss
	attack := boss.attack()
	if len(boss.locked) != attack.TargetCount {
		return nil, fmt.Errorf("missing locked boss targets")
	}
	if attack.ID == "CHU-C" {
		return arena.confessionHits(ctx, from, to)
	}
	if attack.ID == "MIN-A" || attack.ID == "INS-C" {
		return arena.trajectoryHits(ctx, from, to)
	}
	var areas []Rect
	relativeTo := to
	if attack.ID == "HOS-B" {
		area := hospitalTrolley(boss.locked[0], boss.elapsed)
		next := hospitalTrolley(boss.locked[0], boss.elapsed+1)
		// Sweep in relative coordinates, not just against the trolley's
		// position at one end of the tick.
		relativeTo = moved(to, Vector{X: area.X - next.X})
		areas = []Rect{area}
	} else if ((attack.ID == "HOS-C" || attack.ID == "CHU-B" || strings.HasPrefix(attack.ID, "MIN-") || strings.HasPrefix(attack.ID, "INS-")) && boss.elapsed == boss.duration()-1) || ((attack.ID == "HOS-A" || attack.ID == "CHU-A") && boss.elapsed == 0) {
		areas = bossWarningAreas(attack, boss.locked)
	}
	if world.protected(from) || world.protected(to) {
		return nil, nil
	}
	var hits []DamageHit
	for index, area := range areas {
		if sweptOverlap(from, relativeTo, area) {
			direction := 1
			if centerX(to) < centerX(area) {
				direction = -1
			}
			hits = append(hits, DamageHit{Attacker: string(boss.definition.Encounter), Attack: fmt.Sprintf("%s-%d-%d", attack.ID, boss.recovery, index), Kind: OrdinaryDamage, Amount: attack.Damage, Direction: direction})
		}
	}
	return hits, nil
}

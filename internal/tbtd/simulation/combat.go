// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"slices"

	"too-broke-to-die/internal/tbtd/settings"
)

// CombatPhase identifies the current committed player attack interval.
type CombatPhase string

const (
	// PhaseWindup is the committed preparation before an attack releases.
	PhaseWindup CombatPhase = "windup"
	// PhaseActive is the interval in which the strike or throw is released.
	PhaseActive CombatPhase = "active"
	// PhaseRecovery is the follow-through before another attack can start.
	PhaseRecovery CombatPhase = "recovery"
)

// PlayerAttack is a presentation-facing description of a committed attack.
type PlayerAttack struct {
	ID       uint64
	Throw    bool
	Phase    CombatPhase
	Start    uint64
	Facing   int
	Damage   int
	Reach    float64
	Released bool
}

// AttackRelease records a payload leaving inventory at the tick boundary.
type AttackRelease struct {
	AttackID     uint64
	ProjectileID string
	Payload      string
}

type combatState struct {
	attack           PlayerAttack
	aim              Vector
	hitTargets       map[string]bool
	nextAttack       uint64
	immuneUntil      uint64
	enemies          []Enemy
	projectiles      []Projectile
	pickups          []MedicalPickup
	civilians        []Civilian
	protection       []Rect
	outcomes         []HitOutcome
	releases         []AttackRelease
	pendingEnemyHits []enemyHit
	// receivedHits retains (attacker, attack, target) for this transient world.
	receivedHits map[[3]string]bool
	invalid      string
}

func cloneCombat(state combatState) combatState {
	state.enemies = slices.Clone(state.enemies)
	state.projectiles = slices.Clone(state.projectiles)
	state.pickups = slices.Clone(state.pickups)
	state.civilians = slices.Clone(state.civilians)
	state.protection = slices.Clone(state.protection)
	state.outcomes = slices.Clone(state.outcomes)
	state.releases = slices.Clone(state.releases)
	state.pendingEnemyHits = slices.Clone(state.pendingEnemyHits)
	if state.receivedHits != nil {
		copyHits := make(map[[3]string]bool, len(state.receivedHits))
		for key, hit := range state.receivedHits {
			copyHits[key] = hit
		}
		state.receivedHits = copyHits
	}
	if state.hitTargets != nil {
		copyTargets := make(map[string]bool, len(state.hitTargets))
		for id, hit := range state.hitTargets {
			copyTargets[id] = hit
		}
		state.hitTargets = copyTargets
	}
	return state
}

// CombatView is a detached transient view; it never contains campaign credits.
type CombatView struct {
	Attack        PlayerAttack
	Immune        bool
	ImmuneUntil   uint64
	Enemies       []Enemy
	Projectiles   []Projectile
	Pickups       []MedicalPickup
	Civilians     []Civilian
	Outcomes      []HitOutcome
	Releases      []AttackRelease
	InvalidAction string
}

// CombatView returns independent collections for rendering and diagnostics.
func (world *World) CombatView(ctx context.Context) CombatView {
	defer mon.Task()(&ctx)(nil)
	if world == nil {
		return CombatView{}
	}
	state := cloneCombat(world.combat)
	return CombatView{Attack: state.attack, Immune: world.motion.immune, ImmuneUntil: state.immuneUntil, Enemies: state.enemies, Projectiles: state.projectiles, Pickups: state.pickups, Civilians: state.civilians, Outcomes: state.outcomes, Releases: state.releases, InvalidAction: state.invalid}
}

// CombatCommand supplies normalized movement, the visible world rectangle and
// ordinary or lethal hazard contacts gathered for this tick. Visibility is
// supplied by the camera policy, not inferred from a sprite's animation.
type CombatCommand struct {
	Player  PlayerCommand
	Visible Rect
	Hits    []DamageHit
}

func (world *World) beginCombat(action settings.Action, tick uint64) {
	arms := world.motion.limbs & (LeftArm | RightArm)
	if action == settings.Throw && arms == 0 {
		world.combat.invalid = "throw_requires_arm"
		return
	}
	if action == settings.Throw && world.state.Feces <= 0 {
		world.combat.invalid = "throw_empty"
		return
	}
	world.combat.nextAttack++
	attack := PlayerAttack{ID: world.combat.nextAttack, Throw: action == settings.Throw, Start: tick, Facing: world.motion.facing, Phase: PhaseWindup}
	total := uint64(15 + 10 + 30)
	if attack.Throw {
		total = 12 + 6 + 30
	} else {
		switch arms {
		case LeftArm | RightArm:
			attack.Damage, attack.Reach = 22, 48
		case LeftArm, RightArm:
			attack.Damage, attack.Reach = 16, 48
		default:
			attack.Damage, attack.Reach = 10, 32
			total = 15 + 10 + 36
		}
	}
	world.combat.attack = attack
	world.combat.hitTargets = make(map[string]bool)
	world.motion.action = AttackAction
	world.motion.actionUntil = tick + total
}

func (world *World) advancePlayerAttack(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	attack := &world.combat.attack
	if attack.ID == 0 {
		return nil
	}
	elapsed := world.state.Tick - attack.Start
	windup, active, recovery := uint64(15), uint64(10), uint64(30)
	if attack.Throw {
		windup, active = 12, 6
	} else if attack.Damage == 10 {
		recovery = 36
	}
	if elapsed >= windup && attack.Throw && !attack.Released {
		attack.Released = true
		world.state.Feces--
		world.combat.releases = append(world.combat.releases, AttackRelease{AttackID: attack.ID, ProjectileID: fmt.Sprintf("player-%d", attack.ID), Payload: "feces"})
		world.launchPlayerProjectile(*attack)
	}
	switch {
	case elapsed < windup:
		attack.Phase = PhaseWindup
	case elapsed < windup+active:
		attack.Phase = PhaseActive
	case elapsed < windup+active+recovery:
		attack.Phase = PhaseRecovery
	default:
		world.combat.attack = PlayerAttack{}
		world.combat.hitTargets = nil
	}
	return nil
}

func (world *World) strikeEnemies(ctx context.Context, attack PlayerAttack) (err error) {
	defer mon.Task()(&ctx)(&err)
	body := world.state.Player.Bounds
	area := Rect{X: body.X + body.Width, Y: body.Y + (body.Height-36)/2, Width: attack.Reach, Height: 36}
	if attack.Facing < 0 {
		area.X = body.X - attack.Reach
	}
	if world.protected(body) || world.protected(area) {
		return nil
	}
	for i := range world.combat.enemies {
		enemy := &world.combat.enemies[i]
		if enemy.Health <= 0 || world.combat.hitTargets[enemy.ID] || !overlaps(area, enemy.Body.Bounds) {
			continue
		}
		clear, err := world.strikeClear(ctx, body, area, enemy.Body.Bounds, attack.Facing)
		if err != nil {
			return err
		}
		if clear {
			world.combat.hitTargets[enemy.ID] = true
			world.applyEnemyHit(enemy, DamageHit{Attacker: "player", Attack: fmt.Sprint(attack.ID), Kind: OrdinaryDamage, Amount: attack.Damage, Direction: attack.Facing})
		}
	}
	for i := range world.combat.civilians {
		civilian := &world.combat.civilians[i]
		if civilian.Incapacitated || world.combat.hitTargets[civilian.ID] || !overlaps(area, civilian.Bounds) {
			continue
		}
		clear, err := world.strikeClear(ctx, body, area, civilian.Bounds, attack.Facing)
		if err != nil {
			return err
		}
		if clear {
			world.combat.hitTargets[civilian.ID] = true
			civilian.Incapacitated = true
		}
	}
	return nil
}

// strikeClear uses the existing movement collision authority, including
// player-only walls, to avoid hitting through solid cover.
func (world *World) strikeClear(ctx context.Context, body, area, target Rect, facing int) (_ bool, err error) {
	defer mon.Task()(&ctx)(&err)
	from, to := body.X+body.Width, math.Max(body.X+body.Width, target.X)
	if facing < 0 {
		from, to = body.X, math.Min(body.X, target.X+target.Width)
	}
	if from == to {
		return true, nil
	}
	y := (math.Max(area.Y, target.Y) + math.Min(area.Y+area.Height, target.Y+target.Height)) / 2
	_, blocked, err := world.grid.Sweep(ctx, Rect{X: from, Y: y, Width: Tolerance, Height: Tolerance}, Vector{X: to - from}, platformColliders(world.state.Platforms), SweepOptions{})
	if err != nil {
		return false, err
	}
	return !blocked, nil
}

// StepCombat executes one complete combat tick using StepPlayer's clock and
// collision solver. A release at this tick's boundary precedes damage.
func (world *World) StepCombat(ctx context.Context, command CombatCommand) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil || !validRect(command.Visible, false) || !validHits(command.Hits) {
		return fmt.Errorf("invalid combat tick")
	}
	for _, edge := range command.Player.Edges {
		if !edge.Action.Valid() {
			return fmt.Errorf("invalid input edge")
		}
	}
	before, previous, motion, combat := cloneState(world.state), cloneState(world.previous), world.motion, cloneCombat(world.combat)
	defer func() {
		if err != nil {
			world.state, world.previous, world.motion, world.combat = before, previous, motion, combat
		}
	}()
	tick := world.state.Tick
	world.combat.invalid = ""
	// Apply aim before accepting a fresh attack, as movement normally does.
	if world.motion.action == FreeAction || tick >= world.motion.actionUntil {
		if command.Player.Move != 0 && finite(command.Player.Move) {
			world.motion.facing = int(math.Copysign(1, command.Player.Move))
		}
		if command.Player.Aim.X != 0 && finite(command.Player.Aim.X) {
			world.motion.facing = int(math.Copysign(1, command.Player.Aim.X))
		}
	}
	if world.motion.action != FreeAction && tick >= world.motion.actionUntil {
		world.motion.action = FreeAction
	}
	if world.motion.action != AttackAction && world.combat.attack.ID != 0 {
		world.combat.attack = PlayerAttack{}
		world.combat.hitTargets = nil
	}
	if validVector(command.Player.Aim) && command.Player.Aim != (Vector{}) {
		length := math.Hypot(command.Player.Aim.X, command.Player.Aim.Y)
		world.combat.aim = Vector{X: command.Player.Aim.X / length, Y: command.Player.Aim.Y / length}
	}
	world.motion.collectEdges(command.Player.Edges, tick)
	if world.motion.action == FreeAction && tick < world.motion.bufferUntil && (world.motion.buffer == settings.Melee || world.motion.buffer == settings.Throw) {
		action := world.motion.buffer
		world.motion.buffer = ""
		world.beginCombat(action, tick)
	}
	command.Player.Edges = nil // Edges were consumed once above.
	if err = world.StepPlayer(ctx, command.Player); err != nil {
		return err
	}
	world.combat.outcomes = nil
	world.combat.releases = nil
	world.combat.pendingEnemyHits = nil
	if err = world.advancePlayerAttack(ctx); err != nil {
		return err
	}
	if err = world.advanceEnemies(ctx, command.Visible); err != nil {
		return err
	}
	if world.combat.attack.ID != 0 && !world.combat.attack.Throw && world.combat.attack.Phase == PhaseActive {
		if err = world.strikeEnemies(ctx, world.combat.attack); err != nil {
			return err
		}
	}
	_, err = world.advanceProjectiles(ctx, true)
	if err != nil {
		return err
	}
	world.resolveEnemyHits(ctx)
	for _, enemy := range world.combat.enemies {
		if enemy.Health > 0 && enemy.Kind == RangedEnemy && enemy.StrikeTick == world.state.Tick && enemy.Phase == EnemyRecovery {
			world.launchEnemyProjectile(enemy)
		}
	}
	hits, err := world.advanceProjectiles(ctx, false)
	if err != nil {
		return err
	}
	meleeHits, err := world.enemyHits(ctx)
	if err != nil {
		return err
	}
	hits = append(hits, meleeHits...)
	hits = append(hits, command.Hits...)
	world.resolvePlayerHits(ctx, hits)
	world.collectMedicalPickups()
	if world.motion.dead {
		world.state.Health = 0
	}
	if world.motion.action != AttackAction {
		world.combat.attack = PlayerAttack{}
		world.combat.hitTargets = nil
	}
	world.motion.immune = !world.motion.dead && (world.state.Tick < world.combat.immuneUntil || (world.motion.action == EvadeAction && tick < world.motion.immuneUntil))
	return nil
}

// MedicalPickup is a marked, single-use health pickup.
type MedicalPickup struct {
	ID        string
	Bounds    Rect
	Collected bool
}

// AddMedicalPickup registers a marked pickup; full-health contact leaves it available.
func (world *World) AddMedicalPickup(ctx context.Context, pickup MedicalPickup) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || pickup.ID == "" || pickup.Collected || !validRect(pickup.Bounds, false) {
		return fmt.Errorf("invalid medical pickup")
	}
	for _, existing := range world.combat.pickups {
		if existing.ID == pickup.ID {
			return fmt.Errorf("duplicate medical pickup %s", pickup.ID)
		}
	}
	world.combat.pickups = append(world.combat.pickups, pickup)
	slices.SortFunc(world.combat.pickups, func(a, b MedicalPickup) int { return compareID(a.ID, b.ID) })
	return nil
}

func (world *World) collectMedicalPickups() {
	for i := range world.combat.pickups {
		pickup := &world.combat.pickups[i]
		if !pickup.Collected && !world.motion.dead && world.state.Health > 0 && world.state.Health < 100 && overlaps(pickup.Bounds, world.state.Player.Bounds) {
			world.state.Health = min(100, world.state.Health+25)
			pickup.Collected = true
		}
	}
}

// Civilian is a protected adult; children have no combat registration.
type Civilian struct {
	ID            string
	Bounds        Rect
	Incapacitated bool
}

// AddProtectedCivilian registers a named critical adult, not a child.
func (world *World) AddProtectedCivilian(ctx context.Context, civilian Civilian) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || civilian.ID == "" || civilian.Incapacitated || !validRect(civilian.Bounds, false) {
		return fmt.Errorf("invalid protected civilian")
	}
	for _, existing := range world.combat.civilians {
		if existing.ID == civilian.ID {
			return fmt.Errorf("duplicate civilian %s", civilian.ID)
		}
	}
	for _, enemy := range world.combat.enemies {
		if enemy.ID == civilian.ID {
			return fmt.Errorf("duplicate combat actor %s", civilian.ID)
		}
	}
	world.combat.civilians = append(world.combat.civilians, civilian)
	slices.SortFunc(world.combat.civilians, func(a, b Civilian) int { return compareID(a.ID, b.ID) })
	return nil
}

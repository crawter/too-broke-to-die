// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"fmt"
	"slices"
)

// BossAttackBehavior selects an explicitly implemented encounter attack hook.
type BossAttackBehavior string

// BossTargetKind describes the marker locked at the end of a telegraph.
type BossTargetKind string

const (
	// BossTargetArea locks markers identifying the impact or sweep areas.
	BossTargetArea BossTargetKind = "area"
	// BossTargetLane locks a marker identifying the lane of travel or impact.
	BossTargetLane BossTargetKind = "lane"
	// BossTargetPosition locks the sampled player position as an aim point.
	BossTargetPosition BossTargetKind = "position"
	// BossTargetHalf locks a marker identifying the arena half to be struck.
	BossTargetHalf BossTargetKind = "half"
	// BossTargetLevel locks a marker identifying the selected horizontal level.
	BossTargetLevel BossTargetKind = "level"
	// BossTargetTrajectories locks markers from which the executor resolves projectile paths.
	BossTargetTrajectories BossTargetKind = "trajectories"
)

// BossAttack describes a committed attack. ActiveMS is the minimum interval
// reserved for its releases; individual executors may require more time for
// travel or a physical sweep. Times are authored in milliseconds.
type BossAttack struct {
	ID                string
	Behavior          BossAttackBehavior
	Target            BossTargetKind
	Heavy             bool
	TelegraphMS       int
	ActiveMS          int
	RecoveryMS        int
	Damage            int
	TargetCount       int
	ReleaseCount      int
	ReleaseIntervalMS int
	MarkerWidth       int
	ProjectileSpeed   int
	ProjectileHeights []int
	TargetLevels      []int
	MinimumGap        int
}

// BossPhase gives the repeating attack order and phase-specific arena state.
type BossPhase struct {
	Attacks       []string
	Conveyors     int
	ConveyorSpeed int
	WasteHeight   int
	WasteDamage   int
	Relays        []string
}

// BossDefinition is data, not an executable script or a source of credit.
type BossDefinition struct {
	Encounter    EncounterID
	Anchor       AnchorID
	Closure      DeedID
	Health       int
	ArenaWidth   int
	RelayHealth  int
	Thresholds   [2]int
	TransitionMS int
	Attacks      []BossAttack
	Phases       []BossPhase
}

// BossDefinitions returns fresh authored records for the four encounters.
func BossDefinitions(ctx context.Context) []BossDefinition {
	defer mon.Task()(&ctx)(nil)
	codes := []string{"HOS", "CHU", "MIN", "INS"}
	behaviors := [4][3]BossAttackBehavior{{"boom", "trolley", "jars"}, {"pendulum", "lamp", "confessions"}, {"globs", "valve", "press"}, {"beam", "crane", "binders"}}
	targets := [4][3]BossTargetKind{{BossTargetArea, BossTargetLane, BossTargetArea}, {BossTargetArea, BossTargetLane, BossTargetTrajectories}, {BossTargetPosition, BossTargetLane, BossTargetHalf}, {BossTargetLevel, BossTargetArea, BossTargetTrajectories}}
	orders := [4][3][]int{{{0, 1}, {0, 2, 1}, {2, 1, 0}}, {{0, 1}, {2, 0, 1}, {1, 2, 0}}, {{0, 2}, {1, 0, 2}, {0, 1, 2}}, {{0, 1}, {2, 0, 1}, {1, 2, 0}}}
	result := make([]BossDefinition, 0, len(codes))
	for i, code := range codes {
		boss := BossDefinition{Encounter: EncounterID("ENC-" + code + "-05"), Anchor: AnchorID("ANC-" + code + "-05"), Closure: DeedID("D-" + code + "-END"), Health: 420, ArenaWidth: 1280, Thresholds: [2]int{294, 147}, TransitionMS: 1000}
		if code == "INS" {
			boss.RelayHealth = 44
		}
		recoveries := [3]int{1500, 1800, 1600}
		if code == "HOS" {
			recoveries[0] = 1400
		}
		for j, letter := range []string{"A", "B", "C"} {
			attack := BossAttack{ID: code + "-" + letter, Behavior: behaviors[i][j], Target: targets[i][j], TelegraphMS: 700, ActiveMS: 1, RecoveryMS: recoveries[j], Damage: 24, TargetCount: 1, ReleaseCount: 1}
			if j == 1 {
				attack.Heavy, attack.TelegraphMS, attack.Damage = true, 1100, 35
			}
			if code == "HOS" && j == 2 {
				attack.TelegraphMS, attack.TargetCount, attack.ReleaseCount = 1000, 3, 3
			}
			if code == "CHU" && j == 2 {
				attack.TargetCount, attack.ReleaseCount, attack.ReleaseIntervalMS, attack.ActiveMS = 3, 3, 350, 701
			}
			if code == "MIN" && j == 0 {
				attack.ReleaseCount, attack.ReleaseIntervalMS, attack.ActiveMS = 3, 250, 501
			}
			if code == "INS" && j == 2 {
				attack.TargetCount, attack.ReleaseCount, attack.MinimumGap = 2, 2, 160
			}
			switch attack.ID {
			case "HOS-A":
				attack.MarkerWidth = 240
			case "HOS-B":
				attack.ProjectileSpeed = 360
			case "CHU-A":
				attack.MarkerWidth = 280
			case "CHU-C":
				attack.ProjectileHeights = []int{20, 100, 180}
			case "MIN-B":
				attack.MarkerWidth = 160
			case "MIN-C":
				attack.MarkerWidth = 640
			case "INS-A":
				attack.TargetLevels = []int{0, 80}
			}
			boss.Attacks = append(boss.Attacks, attack)
		}
		for phase, order := range orders[i] {
			p := BossPhase{}
			for _, index := range order {
				p.Attacks = append(p.Attacks, boss.Attacks[index].ID)
			}
			switch code {
			case "HOS":
				p.Conveyors = phase
				p.ConveyorSpeed = 60
			case "MIN":
				p.WasteHeight = 40 * (phase + 1)
				p.WasteDamage = 15
			case "INS":
				switch phase {
				case 0:
					p.Relays = []string{"A"}
				case 1:
					p.Relays = []string{"B"}
				case 2:
					p.Relays = []string{"A", "B"}
				}
			}
			boss.Phases = append(boss.Phases, p)
		}
		result = append(result, boss)
	}
	return result
}

// ValidateBossDefinitions rejects malformed identities, timings and references.
func ValidateBossDefinitions(ctx context.Context, bosses []BossDefinition) (err error) {
	defer mon.Task()(&ctx)(&err)
	if len(bosses) != 4 {
		return ErrInvalid.New("expected four bosses")
	}
	seen := make(map[EncounterID]bool)
	canonical := BossDefinitions(ctx)
	for _, boss := range bosses {
		if seen[boss.Encounter] || boss.Health != 420 || boss.ArenaWidth != 1280 || boss.Thresholds != [2]int{294, 147} || boss.TransitionMS != 1000 || len(boss.Phases) != 3 || len(boss.Attacks) != 3 {
			return ErrInvalid.New("invalid boss %s", boss.Encounter)
		}
		seen[boss.Encounter] = true
		code := ""
		switch boss.Encounter {
		case "ENC-HOS-05":
			code = "HOS"
		case "ENC-CHU-05":
			code = "CHU"
		case "ENC-MIN-05":
			code = "MIN"
		case "ENC-INS-05":
			code = "INS"
		default:
			return ErrInvalid.New("unknown boss")
		}
		if boss.Anchor != AnchorID("ANC-"+code+"-05") || boss.Closure != DeedID("D-"+code+"-END") {
			return ErrInvalid.New("boss binding")
		}
		var authored BossDefinition
		for _, candidate := range canonical {
			if candidate.Encounter == boss.Encounter {
				authored = candidate
				break
			}
		}
		if boss.RelayHealth != authored.RelayHealth {
			return ErrInvalid.New("invalid relay health")
		}
		attacks := make(map[string]bool)
		for index, attack := range boss.Attacks {
			minTelegraph, damage := 650, 24
			if attack.Heavy {
				minTelegraph, damage = 1000, 35
			}
			if attack.ID == "" || attack.Behavior == "" || attacks[attack.ID] || attack.TelegraphMS < minTelegraph || attack.TelegraphMS > 3600000 || attack.ActiveMS <= 0 || attack.ActiveMS > 3600000 || attack.RecoveryMS < 800 || attack.RecoveryMS > 3600000 || attack.Damage != damage || attack.TargetCount < 1 || attack.TargetCount > 3 || attack.ReleaseCount < 1 || attack.ReleaseCount > 3 || attack.ReleaseIntervalMS < 0 || attack.ReleaseIntervalMS > 3600000 || (attack.ReleaseCount > 1 && attack.ReleaseIntervalMS > 0 && (attack.ReleaseCount-1)*attack.ReleaseIntervalMS >= attack.ActiveMS) {
				return ErrInvalid.New("invalid attack %s", attack.ID)
			}
			expected := authored.Attacks[index]
			if attack.ID != expected.ID || attack.Behavior != expected.Behavior || attack.Target != expected.Target || attack.Heavy != expected.Heavy || attack.TelegraphMS < expected.TelegraphMS || attack.ActiveMS < expected.ActiveMS || attack.RecoveryMS < expected.RecoveryMS || attack.TargetCount != expected.TargetCount || attack.ReleaseCount != expected.ReleaseCount || attack.ReleaseIntervalMS != expected.ReleaseIntervalMS || attack.MarkerWidth != expected.MarkerWidth || attack.ProjectileSpeed != expected.ProjectileSpeed || attack.MinimumGap != expected.MinimumGap || !slices.Equal(attack.ProjectileHeights, expected.ProjectileHeights) || !slices.Equal(attack.TargetLevels, expected.TargetLevels) {
				return ErrInvalid.New("attack differs from authored contract %s", attack.ID)
			}
			attacks[attack.ID] = true
		}
		for index, phase := range boss.Phases {
			expected := authored.Phases[index]
			if !slices.Equal(phase.Attacks, expected.Attacks) || phase.Conveyors != expected.Conveyors || phase.ConveyorSpeed != expected.ConveyorSpeed || phase.WasteHeight != expected.WasteHeight || phase.WasteDamage != expected.WasteDamage || !slices.Equal(phase.Relays, expected.Relays) {
				return ErrInvalid.New("phase differs from authored contract")
			}
			if len(phase.Attacks) == 0 || phase.Conveyors < 0 || phase.Conveyors > 2 || phase.WasteHeight < 0 || phase.WasteHeight > 120 || len(phase.Relays) > 2 {
				return ErrInvalid.New("invalid phase")
			}
			used := make(map[string]bool)
			for _, id := range phase.Attacks {
				if !attacks[id] || used[id] {
					return ErrInvalid.New("invalid phase attack %s", id)
				}
				used[id] = true
			}
			if len(phase.Relays) == 2 && phase.Relays[0] == phase.Relays[1] {
				return ErrInvalid.New("duplicate relay")
			}
			for _, relay := range phase.Relays {
				if !slices.Contains([]string{"A", "B"}, relay) {
					return ErrInvalid.New("unknown relay")
				}
			}
		}
	}
	return nil
}

// FindBossDefinition returns one isolated authored encounter record.
func FindBossDefinition(ctx context.Context, id EncounterID) (_ BossDefinition, err error) {
	defer mon.Task()(&ctx)(&err)
	bosses := BossDefinitions(ctx)
	if err = ValidateBossDefinitions(ctx, bosses); err != nil {
		return BossDefinition{}, err
	}
	for _, boss := range bosses {
		if boss.Encounter == id {
			return boss, nil
		}
	}
	return BossDefinition{}, fmt.Errorf("%w: boss %s", ErrMissingReference, id)
}

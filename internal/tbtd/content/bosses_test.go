// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestBossDefinitions(t *testing.T) {
	ctx := context.Background()
	bosses := BossDefinitions(ctx)
	if err := ValidateBossDefinitions(ctx, bosses); err != nil {
		t.Fatal(err)
	}
	orders := [4][3][]string{
		{{"HOS-A", "HOS-B"}, {"HOS-A", "HOS-C", "HOS-B"}, {"HOS-C", "HOS-B", "HOS-A"}},
		{{"CHU-A", "CHU-B"}, {"CHU-C", "CHU-A", "CHU-B"}, {"CHU-B", "CHU-C", "CHU-A"}},
		{{"MIN-A", "MIN-C"}, {"MIN-B", "MIN-A", "MIN-C"}, {"MIN-A", "MIN-B", "MIN-C"}},
		{{"INS-A", "INS-B"}, {"INS-C", "INS-A", "INS-B"}, {"INS-B", "INS-C", "INS-A"}},
	}
	for i, boss := range bosses {
		for phase := range boss.Phases {
			if !slices.Equal(boss.Phases[phase].Attacks, orders[i][phase]) {
				t.Fatalf("%s phase %d: %v", boss.Encounter, phase, boss.Phases[phase].Attacks)
			}
		}
	}
	bosses[0].Phases[0].Attacks[0] = "changed"
	if BossDefinitions(ctx)[0].Phases[0].Attacks[0] != "HOS-A" {
		t.Fatal("definitions share mutable storage")
	}
	if _, err := FindBossDefinition(ctx, "missing"); !errors.Is(err, ErrMissingReference) {
		t.Fatalf("missing definition: %v", err)
	}
}

func TestInvalidBossDefinitions(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func([]BossDefinition){
		"duplicate":   func(b []BossDefinition) { b[1].Encounter = b[0].Encounter },
		"health":      func(b []BossDefinition) { b[0].Health = 419 },
		"threshold":   func(b []BossDefinition) { b[0].Thresholds[1] = 148 },
		"transition":  func(b []BossDefinition) { b[0].TransitionMS = 999 },
		"telegraph":   func(b []BossDefinition) { b[0].Attacks[0].TelegraphMS = 649 },
		"heavy":       func(b []BossDefinition) { b[0].Attacks[1].TelegraphMS = 999 },
		"recovery":    func(b []BossDefinition) { b[0].Attacks[0].RecoveryMS = 799 },
		"active":      func(b []BossDefinition) { b[0].Attacks[0].ActiveMS = 0 },
		"bad release": func(b []BossDefinition) { b[1].Attacks[2].ActiveMS = 700 },
		"reference":   func(b []BossDefinition) { b[0].Phases[0].Attacks[0] = "missing" },
		"relay":       func(b []BossDefinition) { b[3].Phases[2].Relays[1] = "A" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			bosses := BossDefinitions(ctx)
			change(bosses)
			if err := ValidateBossDefinitions(ctx, bosses); err == nil {
				t.Fatal("malformed definition accepted")
			}
		})
	}
}

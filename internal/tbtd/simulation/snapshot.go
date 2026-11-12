// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"slices"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
)

// EmergencyState is a committed stage, not a transient certification event.
type EmergencyState struct {
	ID           content.EmergencyID
	Encounter    content.EncounterID
	Stage        string
	WarningTicks int
	SafeBoundary bool
	BypassUsable bool
}

// Diagnostic associates a solver problem with the affected encounter and body.
type Diagnostic struct {
	Name      string
	Encounter content.EncounterID
	Entity    string
}

// State is an independent copy of one authoritative tick, not a live world.
type State struct {
	Tick         uint64
	Location     content.LocationID
	Encounter    content.EncounterID
	Anchor       content.AnchorID
	Mode         string
	SceneEntry   string
	Ending       campaign.EndingStage
	ActiveTicket content.DeedID
	Objective    string
	BossActive   bool
	Player       Body
	HasPlayer    bool
	Health       int
	Limbs        int
	Feces        int
	Routes       []content.GateID
	ItemRoutes   []content.ObjectID
	OwnedItems   []campaign.CriticalItemID
	Installed    []content.DeedID
	Receipts     []content.DeedID
	Defeated     []content.EncounterID
	Entities     []Entity
	Platforms    []Platform
	Emergencies  []EmergencyState
	Diagnostics  []Diagnostic
}

func cloneState(state State) State {
	state.Routes = slices.Clone(state.Routes)
	state.ItemRoutes = slices.Clone(state.ItemRoutes)
	state.OwnedItems = slices.Clone(state.OwnedItems)
	state.Installed = slices.Clone(state.Installed)
	state.Receipts = slices.Clone(state.Receipts)
	state.Defeated = slices.Clone(state.Defeated)
	state.Entities = slices.Clone(state.Entities)
	state.Platforms = slices.Clone(state.Platforms)
	state.Emergencies = slices.Clone(state.Emergencies)
	state.Diagnostics = slices.Clone(state.Diagnostics)
	return state
}

// Snapshot owns the previous and current authoritative states. Data always
// copies; visual interpolation must never be fed back into the world.
type Snapshot struct {
	previous State
	current  State
}

// Data returns isolated copies of both states.
func (snapshot Snapshot) Data(ctx context.Context) (State, State) {
	defer mon.Task()(&ctx)(nil)
	return cloneState(snapshot.previous), cloneState(snapshot.current)
}

// Snapshot captures both authoritative states for a presentation consumer.
func (world *World) Snapshot(ctx context.Context) Snapshot {
	defer mon.Task()(&ctx)(nil)
	return Snapshot{previous: cloneState(world.previous), current: cloneState(world.state)}
}

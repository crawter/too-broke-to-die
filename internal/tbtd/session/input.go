// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"math"

	"too-broke-to-die/internal/tbtd/settings"
	"too-broke-to-die/internal/tbtd/simulation"
)

// inputQueue distinguishes physical held controls (needed for safe Resume)
// from gameplay edges and values, which menus and interruptions discard.
type inputQueue struct {
	held map[settings.Action]bool

	values map[settings.Action]ActionValue

	edges []simulation.PlayerEdge
}

func (input *inputQueue) sample(ctx context.Context, frame FrameInput, disconnected bool) []ActionEdge {
	defer mon.Task()(&ctx)(nil)
	if input.held == nil {
		input.held = make(map[settings.Action]bool)
	}
	if disconnected {
		clear(input.held)
	}
	var accepted []ActionEdge
	for _, edge := range frame.Edges {
		if disconnected && edge.Kind == Pressed {
			continue
		}
		wasHeld := input.held[edge.Action]
		pressed := edge.Kind == Pressed
		input.held[edge.Action] = pressed
		if pressed != wasHeld {
			accepted = append(accepted, edge)
		}
	}
	for _, value := range frame.Values {
		if value.Action != settings.Aim && value.Action != settings.MoveLeft && value.Action != settings.MoveRight && value.Action != settings.Down {
			input.held[value.Action] = value.X != 0 || value.Y != 0
		}
	}
	return accepted
}

func gameplayAction(action settings.Action) bool {
	switch action {
	case settings.MoveLeft, settings.MoveRight, settings.Down, settings.Jump, settings.Aim,
		settings.Melee, settings.Throw, settings.Evade, settings.Interact, settings.SelectPayload,
		settings.SelectLeftArm, settings.SelectRightArm, settings.SelectLeftLeg, settings.SelectRightLeg,
		settings.RemoveLimb, settings.Regenerate:
		return true
	default:
		return false
	}
}

func (input *inputQueue) enqueue(ctx context.Context, frame FrameInput, edges []ActionEdge) {
	defer mon.Task()(&ctx)(nil)
	if input.values == nil {
		input.values = make(map[settings.Action]ActionValue)
	}
	for _, value := range frame.Values {
		if gameplayAction(value.Action) {
			input.values[value.Action] = value
		}
	}
	for _, edge := range edges {
		if gameplayAction(edge.Action) {
			input.edges = append(input.edges, simulation.PlayerEdge{Action: edge.Action, Pressed: edge.Kind == Pressed})
		}
	}
}

func (input *inputQueue) clearGameplay() {
	clear(input.values)
	input.edges = nil
}

func (input *inputQueue) canResume() bool {
	for _, action := range []settings.Action{settings.Melee, settings.Throw, settings.RemoveLimb, settings.MenuConfirm} {
		if input.held[action] {
			return false
		}
	}
	return true
}

// playerTick applies the current aim together with the first tick's edges.
// World.StepCombat processes aim before starting the throw and implements the
// 15-tick jump/combat buffer; subsequent ticks receive held values only.
func (input *inputQueue) playerTick(ctx context.Context) simulation.PlayerCommand {
	defer mon.Task()(&ctx)(nil)
	left, right := input.values[settings.MoveLeft], input.values[settings.MoveRight]
	aim, down := input.values[settings.Aim], input.values[settings.Down]
	command := simulation.PlayerCommand{
		Move:  math.Max(-1, math.Min(1, math.Abs(right.X)-math.Abs(left.X))),
		Aim:   simulation.Vector{X: aim.X, Y: aim.Y},
		Down:  down.X != 0 || down.Y != 0,
		Edges: input.edges,
	}
	input.edges = nil
	return command
}

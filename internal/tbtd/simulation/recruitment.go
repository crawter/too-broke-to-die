// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"slices"

	"too-broke-to-die/internal/tbtd/content"
)

// RecruitmentView is the active ticket's transient feed and replacement queue.
// A receipt disconnects the matching feed; none of these events grants credit.
type RecruitmentView struct {
	ID            string
	Ticket        content.DeedID
	Bounds        Rect
	Health        int
	Disconnected  bool
	Live          int
	Queue         []RecruitmentReplacement
	StoppedTicks  int
	CooldownTicks int
}

// RecruitmentReplacement exposes the delay and visible warning independently.
type RecruitmentReplacement struct {
	ID        string
	Remaining int
	Warning   bool
}

type recruitmentState struct {
	id            string
	ticket        content.DeedID
	bounds        Rect
	health        int
	disconnected  bool
	queue         []RecruitmentReplacement
	seen          map[string]bool
	next          uint64
	stopUntil     uint64
	cooldownUntil uint64
	meleeHit      uint64
}

func cloneRecruitment(state recruitmentState) recruitmentState {
	state.queue = slices.Clone(state.queue)
	if state.seen != nil {
		seen := make(map[string]bool, len(state.seen))
		for id, defeated := range state.seen {
			seen[id] = defeated
		}
		state.seen = seen
	}
	return state
}

func (world *World) installRecruitment() {
	if world.state.Location != "LOC-INS" || world.encounter.ticket == "" {
		return
	}
	id := world.encounter.ticket
	certified := slices.Contains(world.state.Receipts, id)
	if !certified && (world.service.attempt.Deed != id || world.service.status != ServiceActive) {
		return
	}
	world.encounter.feed = recruitmentState{id: "feed-" + string(id), ticket: id, bounds: Rect{X: 1168, Y: 0, Width: 32, Height: 80}, health: 44, disconnected: certified, seen: make(map[string]bool)}
}

func (world *World) syncRecruitment() {
	feed := &world.encounter.feed
	if feed.ticket != "" && slices.Contains(world.state.Receipts, feed.ticket) {
		feed.disconnected = true
		feed.queue = nil
	}
}

// Recruitment returns a detached, deterministic queue and current live count.
func (world *World) Recruitment(ctx context.Context) RecruitmentView {
	defer mon.Task()(&ctx)(nil)
	if world == nil {
		return RecruitmentView{}
	}
	feed := world.encounter.feed
	if feed.id == "" {
		return RecruitmentView{}
	}
	live := 0
	for _, enemy := range world.combat.enemies {
		if enemy.Kind == FollowerEnemy && enemy.Health > 0 {
			live++
		}
	}
	stopped, cooldown := 0, 0
	if feed.stopUntil > world.state.Tick {
		stopped = int(feed.stopUntil - world.state.Tick)
	}
	if feed.cooldownUntil > world.state.Tick {
		cooldown = int(feed.cooldownUntil - world.state.Tick)
	}
	disconnected := feed.disconnected || slices.Contains(world.state.Receipts, feed.ticket)
	queue := slices.Clone(feed.queue)
	if disconnected {
		queue = nil
	}
	return RecruitmentView{ID: feed.id, Ticket: feed.ticket, Bounds: feed.bounds, Health: feed.health, Disconnected: disconnected, Live: live, Queue: queue, StoppedTicks: stopped, CooldownTicks: cooldown}
}

// RecruitmentForTicket reports a disconnected certified sibling as well as the
// active feed. It cannot activate an uncertified request or create a reward.
func (world *World) RecruitmentForTicket(ctx context.Context, id content.DeedID) (_ RecruitmentView, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.state.Location != "LOC-INS" || !slices.Contains(world.encounter.tickets, id) {
		return RecruitmentView{}, fmt.Errorf("ticket is not in this institute bay")
	}
	if world.encounter.feed.ticket == id {
		return world.Recruitment(ctx), nil
	}
	if !slices.Contains(world.state.Receipts, id) {
		return RecruitmentView{}, fmt.Errorf("ticket feed is not active or certified")
	}
	return RecruitmentView{ID: "feed-" + string(id), Ticket: id, Bounds: Rect{X: 1168, Y: 0, Width: 32, Height: 80}, Health: 44, Disconnected: true}, nil
}

func (world *World) advanceRecruitment(ctx context.Context, queuedAtStart int, tick uint64) (err error) {
	defer mon.Task()(&ctx)(&err)
	feed := &world.encounter.feed
	if feed.id == "" || feed.disconnected || feed.health <= 0 || tick < feed.stopUntil {
		return nil
	}
	live := world.Recruitment(ctx).Live
	for i := 0; i < len(feed.queue) && queuedAtStart > 0; {
		queuedAtStart--
		entry := &feed.queue[i]
		if entry.Remaining > 0 {
			entry.Remaining--
		}
		entry.Warning = entry.Remaining <= 180
		if entry.Remaining != 0 || live >= 2 {
			i++
			continue
		}
		box := Rect{X: 1184 - PlayerWidth/2, Y: 0, Width: PlayerWidth, Height: PlayerHeight}
		if err = world.AddEnemy(ctx, entry.ID, FollowerEnemy, box); err != nil {
			return fmt.Errorf("spawn replacement: %w", err)
		}
		live++
		feed.queue = append(feed.queue[:i], feed.queue[i+1:]...)
	}
	return nil
}

func (world *World) recruitmentTargets(command *ServiceCommand) {
	feed := &world.encounter.feed
	if feed.id == "" || feed.disconnected || feed.health <= 0 {
		return
	}
	collider := Collider{ID: feed.id, Kind: ProjectileTarget, Bounds: feed.bounds}
	command.Objects.Feces.Targets = append(slices.Clone(command.Objects.Feces.Targets), FecesTarget{Collider: collider, Kind: FecesSource, Ticket: feed.ticket})
	command.Objects.Feces.Limb.Targets = append(slices.Clone(command.Objects.Feces.Limb.Targets), collider)
}

func (world *World) recruitmentImpacts(ctx context.Context, previous PlayerAttack, previousAction ActionKind) (err error) {
	defer mon.Task()(&ctx)(&err)
	feed := &world.encounter.feed
	if feed.id == "" {
		return nil
	}
	for _, enemy := range world.combat.enemies {
		if enemy.Kind != FollowerEnemy || enemy.Health > 0 || feed.seen[enemy.ID] {
			continue
		}
		feed.seen[enemy.ID] = true
		if feed.health > 0 && !feed.disconnected {
			feed.next++
			feed.queue = append(feed.queue, RecruitmentReplacement{ID: fmt.Sprintf("%s-follower-%06d", feed.id, feed.next), Remaining: 540})
		}
	}
	if feed.health <= 0 || feed.disconnected {
		return nil
	}
	attack := world.combat.attack
	if attack.ID == 0 && previousAction == AttackAction && previous.ID != 0 && !previous.Throw && world.state.Tick >= previous.Start+15 && world.state.Tick < previous.Start+25 {
		// The inherited combat tick clears an attack after an enemy hit. Its
		// already-completed strike still applies before that same-tick damage.
		attack = previous
		attack.Phase = PhaseActive
	}
	if attack.ID != 0 && !attack.Throw && attack.Phase == PhaseActive && feed.meleeHit != attack.ID && !world.protected(world.state.Player.Bounds) {
		body := world.state.Player.Bounds
		area := Rect{X: body.X + body.Width, Y: body.Y + (body.Height-36)/2, Width: attack.Reach, Height: 36}
		if attack.Facing < 0 {
			area.X = body.X - attack.Reach
		}
		if overlaps(area, feed.bounds) && !world.protected(area) {
			clear, clearErr := world.strikeClear(ctx, body, area, feed.bounds, attack.Facing)
			if clearErr != nil {
				return clearErr
			}
			if clear {
				feed.health = max(0, feed.health-attack.Damage)
				feed.meleeHit = attack.ID
			}
		}
	}
	for _, event := range world.limbs.events {
		if event.Kind == "impact" && event.Target == feed.id {
			damage := 35
			if socketMask(event.Socket)&(LeftArm|RightArm) != 0 {
				damage = 25
			}
			feed.health = max(0, feed.health-damage)
		}
	}
	for _, impact := range world.feces.impacts {
		if impact.Kind == "source" && impact.Target == feed.id && impact.Ticket == feed.ticket && world.state.Tick >= feed.cooldownUntil {
			feed.stopUntil = world.state.Tick + 240
			feed.cooldownUntil = world.state.Tick + 480
		}
	}
	if feed.health == 0 {
		feed.queue = nil
	}
	return nil
}

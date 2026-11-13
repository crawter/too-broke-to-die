// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"strings"

	"too-broke-to-die/internal/tbtd/content"
)

func instituteMarkers(attack content.BossAttack, player Rect) []Vector {
	x := centerX(player)
	switch attack.ID {
	case "INS-A":
		level := 0.0
		if player.Y >= 40 {
			level = 80
		}
		return []Vector{{X: x, Y: level}}
	case "INS-B":
		return []Vector{{X: math.Max(192, math.Min(1088, x))}}
	case "INS-C":
		x = math.Max(272, math.Min(1008, x))
		return []Vector{{X: x - 120}, {X: x + 120}}
	}
	return nil
}

func instituteWarnings(attack content.BossAttack, markers []Vector) []Rect {
	var areas []Rect
	for _, marker := range markers {
		switch attack.ID {
		case "INS-A":
			areas = append(areas, Rect{X: 112, Y: marker.Y, Width: 1056, Height: 48})
		case "INS-B":
			areas = append(areas, Rect{X: marker.X - 80, Y: 0, Width: 160, Height: 400})
		case "INS-C":
			areas = append(areas, Rect{X: marker.X - 40, Y: 0, Width: 80, Height: 320})
		}
	}
	return areas
}

// The vulnerable cage descends to floor-melee height during every recovery.
// Its authoritative target bounds move with the visible cage, even when a
// surviving relay continues to protect it.
func (arena *BossArena) moveInstituteCage() {
	boss := &arena.world.boss
	if boss.definition.Encounter != "ENC-INS-05" {
		return
	}
	switch boss.stage {
	case BossRecovery, BossPending, BossDefeated:
		boss.bounds.Y = 0
	default:
		boss.bounds.Y = 80
	}
}

func (arena *BossArena) liveFollowers() int {
	live := 0
	for _, enemy := range arena.world.combat.enemies {
		if enemy.Kind == FollowerEnemy && enemy.Health > 0 {
			live++
		}
	}
	return live
}

func (arena *BossArena) activeRelay() (active, running bool) {
	for _, relay := range arena.world.boss.relays {
		if relay.Required && relay.Health > 0 {
			active = true
			running = running || arena.world.state.Tick >= relay.StoppedUntil
		}
	}
	return active, running
}

func (arena *BossArena) stopFollowers() {
	arena.followerQueue = nil
	for i := range arena.world.combat.enemies {
		enemy := &arena.world.combat.enemies[i]
		if enemy.Kind == FollowerEnemy && strings.HasPrefix(enemy.ID, "boss-follower-") {
			enemy.Health, enemy.Phase, enemy.Body.Velocity = 0, EnemyDefeated, Vector{}
		}
	}
}

func (arena *BossArena) queueFollower() {
	arena.followerNext++
	arena.followerQueue = append(arena.followerQueue, RecruitmentReplacement{ID: fmt.Sprintf("boss-follower-%06d", arena.followerNext), Remaining: 540})
}

func (arena *BossArena) spawnFollower(ctx context.Context, id string, x float64) (err error) {
	defer mon.Task()(&ctx)(&err)
	return arena.world.AddEnemy(ctx, id, FollowerEnemy, Rect{X: x - PlayerWidth/2, Y: 0, Width: PlayerWidth, Height: PlayerHeight})
}

// advanceFollowers owns a single arena-wide queue. Only actual defeated
// followers enqueue replacements; phase restoration may re-establish missing
// slots after the previous phase's disabled relays canceled their queue.
func (arena *BossArena) advanceFollowers(ctx context.Context, queuedAtStart, previousPhase int) (err error) {
	defer mon.Task()(&ctx)(&err)
	world := arena.world
	if world.boss.stage == BossPending || world.boss.stage == BossDefeated {
		arena.stopFollowers()
		return nil
	}
	if world.boss.stage == BossDormant {
		return nil
	}
	if !arena.followersStarted {
		arena.followerSeen = make(map[string]bool)
		for _, x := range []float64{400, 880} {
			if arena.liveFollowers() >= 2 {
				break
			}
			arena.followerNext++
			if err = arena.spawnFollower(ctx, fmt.Sprintf("boss-follower-%06d", arena.followerNext), x); err != nil {
				return err
			}
		}
		arena.followersStarted = true
	}
	active, running := arena.activeRelay()
	if !active {
		arena.followerQueue = nil
	}
	for _, enemy := range world.combat.enemies {
		if enemy.Kind != FollowerEnemy || !strings.HasPrefix(enemy.ID, "boss-follower-") || enemy.Health > 0 || arena.followerSeen[enemy.ID] {
			continue
		}
		arena.followerSeen[enemy.ID] = true
		if active && arena.liveFollowers()+len(arena.followerQueue) < 2 {
			arena.queueFollower()
		}
	}
	if active && previousPhase != world.boss.phase {
		for arena.liveFollowers()+len(arena.followerQueue) < 2 {
			arena.queueFollower()
		}
	}
	if !running {
		return nil
	}
	live := arena.liveFollowers()
	for i := 0; i < len(arena.followerQueue) && queuedAtStart > 0; {
		queuedAtStart--
		entry := &arena.followerQueue[i]
		if entry.Remaining > 0 {
			entry.Remaining--
		}
		entry.Warning = entry.Remaining <= 180
		if entry.Remaining > 0 || live >= 2 {
			i++
			continue
		}
		if err = arena.spawnFollower(ctx, entry.ID, 1040); err != nil {
			return err
		}
		live++
		arena.followerQueue = append(arena.followerQueue[:i], arena.followerQueue[i+1:]...)
	}
	return nil
}

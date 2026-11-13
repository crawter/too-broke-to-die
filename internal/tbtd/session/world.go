// Copyright (C) 2026 crawter
// All rights reserved.

package session

import (
	"context"
	"fmt"
	"math"
	"strings"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/settings"
	"too-broke-to-die/internal/tbtd/simulation"
)

func worldRect(rect simulation.Rect) WorldRect {
	return WorldRect{X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height}
}

// enemyWarning describes only the current telegraph or strike boundary.
func enemyWarning(enemy simulation.Enemy, tick uint64) (WorldMarker, bool) {
	striking := enemy.AttackID != 0 && enemy.StrikeTick == tick
	if enemy.Health <= 0 || (enemy.Phase != simulation.EnemyTelegraph && !striking) {
		return WorldMarker{}, false
	}
	state, label := "warning", "Удар"
	if striking {
		state = "active"
	}
	box := enemy.Body.Bounds
	if enemy.Kind == simulation.RangedEnemy {
		return WorldMarker{ID: enemy.ID + ":attack", Kind: "danger", State: state, Label: "Готовит выстрел", Bounds: worldRect(box)}, true
	}
	area := simulation.Rect{X: box.X + box.Width, Y: box.Y + (box.Height-36)/2, Width: 48, Height: 36}
	if enemy.Facing < 0 {
		area.X = box.X - 48
	}
	return WorldMarker{ID: enemy.ID + ":attack", Kind: "danger", State: state, Label: label, Bounds: worldRect(area)}, true
}

// enemyVisualState distinguishes the release tick from subsequent recovery;
// the simulation does not maintain a separate multi-tick active phase.
func enemyVisualState(enemy simulation.Enemy, tick uint64) string {
	if enemy.Health > 0 && enemy.Phase == simulation.EnemyRecovery && enemy.AttackID != 0 && enemy.StrikeTick == tick {
		return "active"
	}
	return string(enemy.Phase)
}

func bossAsset(boss simulation.BossView, code string) content.PresentationID {
	switch boss.Stage {
	case simulation.BossTelegraph:
		return content.PresentationID("AN-BOSS-" + boss.Attack.ID + "-PREP")
	case simulation.BossActive:
		return content.PresentationID("AN-BOSS-" + boss.Attack.ID + "-ACTIVE")
	case simulation.BossRecovery:
		return content.PresentationID("AN-BOSS-" + boss.Attack.ID + "-RECOVER")
	case simulation.BossTransition:
		return content.PresentationID("AN-BOSS-" + code + "-TRANSITION")
	case simulation.BossPending, simulation.BossDefeated:
		return content.PresentationID("AN-BOSS-" + code + "-DEFEAT")
	default:
		return content.PresentationID(map[string]string{"HOS": "ART-CHR-POT", "CHU": "ART-CHR-SAV", "MIN": "ART-CHR-BLA", "INS": "ART-CHR-LEV"}[code])
	}
}

// nearbyLandings publishes authored landing bounds near the current takeoff.
func nearbyLandings(x float64, platforms []content.Rect, jumps []content.BossJump) []WorldRect {
	var result []WorldRect
	for _, jump := range jumps {
		if math.Abs(x-jump.Jump.TakeoffX) > 120 || jump.Target < 0 || jump.Target >= len(platforms) {
			continue
		}
		platform := platforms[jump.Target]
		width := math.Min(platform.Width, 160)
		left := platform.X
		if jump.Jump.Direction == -1 {
			left += platform.Width - width
		}
		result = append(result, WorldRect{X: left, Y: platform.Y + platform.Height, Width: width, Height: platform.Height})
	}
	return result
}

// captureEffects copies every accepted tick event before the next tick clears
// the simulation's transient event buffers. No effect is gameplay authority.
func (service *Service) captureEffects(ctx context.Context) {
	defer mon.Task()(&ctx)(nil)
	_, state := service.world.Snapshot(ctx).Data(ctx)
	combat := service.world.CombatView(ctx)
	var ordinal uint32
	add := func(kind, entity string, position Point, direction int, heavy bool) {
		asset := content.PresentationID("FX-BODY")
		if kind == "feces" {
			asset = "FX-FEC"
		}
		service.effects = append(service.effects, EffectEvent{Tick: state.Tick, Ordinal: ordinal, Entity: entity, Kind: kind, Asset: asset, Position: position, Direction: direction, Heavy: heavy})
		ordinal++
	}
	for _, outcome := range combat.Outcomes {
		if outcome.Result != simulation.HitAccepted {
			continue
		}
		var box simulation.Rect
		found := false
		if outcome.Target == "player" {
			box, found = state.Player.Bounds, state.HasPlayer
		} else {
			for _, enemy := range combat.Enemies {
				if enemy.ID == outcome.Target {
					box, found = enemy.Body.Bounds, true
					break
				}
			}
		}
		if found {
			add("hit", outcome.Target, Point{X: box.X + box.Width/2, Y: box.Y + box.Height/2}, outcome.Hit.Direction, outcome.Hit.Amount >= 35)
		}
	}
	for _, event := range service.world.LimbView(ctx).Events {
		if event.Kind == "blood" && state.HasPlayer {
			box := state.Player.Bounds
			add("removal", "player", Point{X: box.X + box.Width/2, Y: box.Y + box.Height/2}, service.world.PlayerStatus().Facing, true)
		}
	}
	for _, impact := range service.world.FecesView(ctx).Impacts {
		add("feces", impact.Projectile, Point{X: impact.Position.X, Y: impact.Position.Y}, 0, false)
	}
}

// worldView publishes only completed simulation state. Positions may be
// interpolated by the consumer; action and projectile state may not.
func (service *Service) worldView(ctx context.Context, data *FrameViewData) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = service.bindBossArena(ctx); err != nil {
		return err
	}
	previous, current := service.world.Snapshot(ctx).Data(ctx)
	status := service.world.PlayerStatus()
	combat := service.world.CombatView(ctx)
	limbs := service.world.LimbView(ctx)
	boss := service.world.Boss(ctx)
	data.World = WorldView{Location: current.Location, Encounter: current.Encounter, Tick: current.Tick, Fraction: service.policy.game.Fraction(), BossLocked: boss.Encounter != "" && boss.Stage != simulation.BossDormant && boss.Stage != simulation.BossDefeated, Effects: append([]EffectEvent(nil), service.effects...)}
	encounter := string(current.Encounter)
	switch {
	case strings.HasPrefix(encounter, "ENC-PRO-"):
		data.World.Background = content.PresentationID("ART-" + strings.TrimPrefix(encounter, "ENC-"))
		for _, bay := range service.bays {
			if bay.Institution.Location == current.Location {
				for _, room := range bay.Institution.Prologue {
					if room.Encounter == current.Encounter {
						data.World.Landings = nearbyLandings(current.Player.Bounds.X+current.Player.Bounds.Width/2, room.Platforms, room.Jumps)
					}
				}
				break
			}
		}
	case boss.Encounter != "":
		data.World.Background = content.PresentationID("ART-BOSS-" + strings.TrimSuffix(strings.TrimPrefix(encounter, "ENC-"), "-05"))
		for _, bay := range service.bays {
			room := bay.Institution.Boss
			if room.Encounter == current.Encounter {
				data.World.Landings = nearbyLandings(current.Player.Bounds.X+current.Player.Bounds.Width/2, room.Platforms, room.Jumps)
				break
			}
		}
	default:
		for _, bay := range service.bays {
			if bay.Encounter == current.Encounter {
				data.World.Background = content.PresentationID("ART-BAY-" + strings.TrimPrefix(string(current.Location), "LOC-") + "-" + strings.TrimPrefix(bay.Template.ID, "BAY-"))
				if current.HasPlayer {
					x := current.Player.Bounds.X + current.Player.Bounds.Width/2
					for _, jump := range bay.Template.Jumps {
						if math.Abs(x-jump.TakeoffX) > 120 {
							continue
						}
						for _, feature := range bay.Template.Features {
							if feature.ID == jump.Landing {
								data.World.Landings = append(data.World.Landings, WorldRect{X: feature.X, Y: feature.Y, Width: feature.Width, Height: feature.Height})
							}
						}
					}
					for _, jump := range bay.Safety.Jumps {
						if math.Abs(x-jump.Jump.TakeoffX) <= 120 && jump.Target >= 0 && jump.Target < len(bay.Safety.Platforms) {
							platform := bay.Safety.Platforms[jump.Target]
							width := math.Min(platform.Width, 128)
							left := platform.X
							if jump.Jump.Direction == -1 {
								left += platform.Width - width
							}
							data.World.Landings = append(data.World.Landings, WorldRect{X: left, Y: platform.Y + platform.Height, Width: width, Height: platform.Height})
						}
					}
				}
				break
			}
		}
	}
	data.Boss = BossView{ID: boss.Encounter, Health: boss.Health, Phase: boss.Phase, Protected: boss.Protected}
	if boss.Encounter != "" {
		data.Boss.MaximumHealth = 420
	}
	data.Player = PlayerView{Health: current.Health, Feces: current.Feces, Immune: status.OrdinaryImmune, Facing: status.Facing, Grounded: current.Player.Support != "", Velocity: Point{X: current.Player.Velocity.X, Y: current.Player.Velocity.Y}, Limbs: [4]bool{status.Limbs&simulation.LeftArm != 0, status.Limbs&simulation.RightArm != 0, status.Limbs&simulation.LeftLeg != 0, status.Limbs&simulation.RightLeg != 0}, SelectedSocket: string(limbs.Selected), Channel: limbs.Channel, AttackPhase: string(combat.Attack.Phase), AttackStart: combat.Attack.Start, AttackReleased: combat.Attack.Released}
	if limbs.LimbPayload {
		for _, limb := range limbs.Objects {
			if limb.ID == limbs.Carried {
				data.Player.SelectedPayload = string(limb.Socket)
				break
			}
		}
	} else {
		data.Player.SelectedPayload = "feces"
	}
	switch status.Action {
	case simulation.HurtAction:
		data.Player.Action = "hurt"
	case simulation.DeathAction:
		data.Player.Action = "dead"
	case simulation.EvadeAction:
		data.Player.Action = "evade"
	case simulation.RemovalAction:
		data.Player.Action = "remove"
	case simulation.InteractionAction:
		data.Player.Action = "interact"
	case simulation.AttackAction:
		if combat.Attack.Throw {
			data.Player.Action = "throw"
		} else {
			data.Player.Action = "melee"
		}
	}
	if limbs.Channel != "" {
		duration := map[string]float64{"remove": 42, "replace": 48, "regenerate": 180}[limbs.Channel]
		if duration > 0 {
			data.Player.ChannelProgress = max(0, min(1, 1-float64(limbs.Remaining)/duration))
		}
	}
	if !current.HasPlayer {
		return nil
	}
	body := current.Player.Bounds
	old := previous.Player.Bounds
	if !previous.HasPlayer {
		old = body
	}
	data.Entities = append(data.Entities, EntityView{ID: "player", Kind: "player", PreviousX: old.X + old.Width/2, PreviousY: old.Y, X: body.X + body.Width/2, Y: body.Y, Width: body.Width, Height: body.Height, Facing: status.Facing})
	if status.OrdinaryImmune {
		data.World.Markers = append(data.World.Markers, WorldMarker{ID: "player:immune", Kind: "immune", Label: "ЗАЩИТА", Bounds: worldRect(body)})
	}
	code := strings.TrimPrefix(string(current.Location), "LOC-")
	puzzleObjects := make(map[string]simulation.PuzzleObject)
	for _, object := range service.world.Objects(ctx) {
		puzzleObjects[object.ID] = object
	}
	civilians := make(map[string]simulation.Civilian, len(combat.Civilians))
	for _, civilian := range combat.Civilians {
		civilians[civilian.ID] = civilian
	}
	serviceView := service.world.ServiceView(ctx)
	previousEntities := make(map[content.ObjectID]simulation.Entity, len(previous.Entities))
	for _, entity := range previous.Entities {
		previousEntities[entity.ID] = entity
	}
	for _, entity := range current.Entities {
		if entity.Kind == "MELEE" || entity.Kind == "RANGED" {
			continue // the combat view supplies the live actor, not its spawn
		}
		old := entity.Position
		if prior, ok := previousEntities[entity.ID]; ok {
			old = prior.Position
		}
		if entity.Role == content.RoleActor {
			for _, deed := range service.manifest.Deeds(ctx) {
				if deed.Deed.ID != entity.Ticket || deed.Family != "RES" {
					continue
				}
				state := "WAIT"
				position := entity.Position
				if civilian, ok := civilians[string(entity.ID)]; ok {
					position = simulation.Vector{X: civilian.Bounds.X + civilian.Bounds.Width/2, Y: civilian.Bounds.Y}
					old = position // Combat actors do not yet carry a previous-tick body.
					if civilian.Incapacitated {
						state = "INCAPACITATED"
					}
				}
				if state != "INCAPACITATED" && serviceView.Deed == entity.Ticket {
					switch {
					case serviceView.AdultInRefuge:
						state = "REFUGE"
					case serviceView.AdultWaiting:
						state = "SAFESTOP"
					case serviceView.AdultFollowing:
						state = "FOLLOW"
					}
				}
				asset := content.PresentationID(fmt.Sprintf("AN-RES-%s-%02d-%s", code, deed.Variant, state))
				data.Entities = append(data.Entities, EntityView{ID: string(entity.ID), Kind: "rescue", Asset: asset, State: state, PreviousX: old.X, PreviousY: old.Y, X: position.X, Y: position.Y})
				break
			}
			continue
		}
		if object, ok := puzzleObjects[string(entity.ID)]; ok {
			family := ""
			for _, bay := range service.bays {
				if bay.Deed == entity.Ticket {
					family = strings.TrimPrefix(bay.Template.ID, "BAY-")
					break
				}
			}
			asset := content.PresentationID("")
			if family != "" {
				asset = content.PresentationID("ART-OBJ-" + code + "-" + family + "-" + string(entity.Role))
			}
			state := "default"
			switch {
			case object.Broken:
				state = "broken"
			case object.Latched:
				state = "latched"
			case object.Active:
				state = "active"
			case object.Restoring:
				state = "restoring"
			}
			if object.Kind == simulation.ObjectGate {
				old.X += object.Bounds.Width / 2
			}
			if object.Kind == simulation.ObjectPaddle {
				old.Y -= object.Bounds.Height / 2
			}
			data.Entities = append(data.Entities, EntityView{ID: object.ID, Kind: "object", Asset: asset, State: state, PreviousX: old.X, PreviousY: old.Y, X: object.Bounds.X + object.Bounds.Width/2, Y: object.Bounds.Y, Width: object.Bounds.Width, Height: object.Bounds.Height})
			continue
		}
		data.Entities = append(data.Entities, EntityView{ID: string(entity.ID), Kind: string(entity.Role), PreviousX: old.X, PreviousY: old.Y, X: entity.Position.X, Y: entity.Position.Y})
	}
	for _, enemy := range combat.Enemies {
		kind := "MEL"
		if enemy.Kind == simulation.RangedEnemy {
			kind = "RNG"
		}
		box := enemy.Body.Bounds
		data.Entities = append(data.Entities, EntityView{ID: enemy.ID, Kind: "enemy", State: enemyVisualState(enemy, current.Tick), Asset: content.PresentationID("ART-EN-" + code + "-" + kind), X: box.X + box.Width/2, Y: box.Y, PreviousX: box.X + box.Width/2, PreviousY: box.Y, Width: box.Width, Height: box.Height, Facing: enemy.Facing})
		if marker, ok := enemyWarning(enemy, current.Tick); ok {
			data.World.Markers = append(data.World.Markers, marker)
		}
		if enemy.Health > 0 && current.Tick < enemy.ImmuneUntil {
			data.World.Markers = append(data.World.Markers, WorldMarker{ID: enemy.ID + ":immune", Kind: "immune", Label: "ЗАЩИТА", Bounds: worldRect(box)})
		}
	}
	for _, projectile := range combat.Projectiles {
		asset := content.PresentationID("")
		if projectile.PlayerOwned {
			asset = "FX-FEC"
		}
		data.Entities = append(data.Entities, EntityView{ID: projectile.ID, Kind: "projectile", Asset: asset, State: "active", X: projectile.Position.X, Y: projectile.Position.Y, PreviousX: projectile.Position.X, PreviousY: projectile.Position.Y})
		data.World.Markers = append(data.World.Markers, WorldMarker{ID: projectile.ID, Kind: "projectile", Bounds: WorldRect{X: projectile.Position.X, Y: projectile.Position.Y}})
	}
	for _, limb := range limbs.Objects {
		if limb.Carried {
			continue
		}
		kind := "limb"
		if limb.Airborne {
			kind = "projectile"
		}
		data.World.Markers = append(data.World.Markers, WorldMarker{ID: limb.ID, Kind: kind, Label: string(limb.Socket), Bounds: WorldRect{X: limb.Position.X, Y: limb.Position.Y}})
	}
	for _, smear := range service.world.FecesView(ctx).Smears {
		data.World.Markers = append(data.World.Markers, WorldMarker{ID: smear.ID, Kind: "smear", Label: "Скользко", Bounds: WorldRect{X: smear.Position.X, Y: smear.Position.Y}})
	}
	for _, hazard := range service.world.Hazards(ctx) {
		label := map[simulation.HazardPhase]string{simulation.HazardDormant: "Опасность: не запущена", simulation.HazardWarning: "Опасность: предупреждение", simulation.HazardActive: "Опасность: действует", simulation.HazardSafe: "Опасность: перерыв"}[hazard.Phase]
		data.World.Markers = append(data.World.Markers, WorldMarker{ID: hazard.ID, Kind: "danger", State: string(hazard.Phase), Label: label, Bounds: worldRect(hazard.Bounds)})
		data.World.Markers = append(data.World.Markers, WorldMarker{ID: hazard.ID + ":waiting", Kind: "refuge", Label: "Безопасно", Bounds: worldRect(hazard.Waiting)})
	}
	emergency := service.world.EmergencyView(ctx)
	if emergency.ID != "" && emergency.Stage == campaign.EmergencyActive {
		area := emergency.Danger
		state := "active"
		if emergency.WarningTicks > 0 {
			area = emergency.Envelope
			state = "warning"
		}
		if area.Width > 0 && area.Height > 0 {
			data.World.Markers = append(data.World.Markers, WorldMarker{ID: string(emergency.ID), Kind: "danger", State: state, Label: emergency.Warning, Bounds: worldRect(area)})
		}
		data.World.Markers = append(data.World.Markers, WorldMarker{ID: string(emergency.ID) + ":refuge", Kind: "refuge", Label: emergency.RefugeName, Bounds: worldRect(emergency.Refuge)})
	}
	interaction := service.world.InteractionView(ctx)
	if interaction.Found {
		data.World.Markers = append(data.World.Markers, WorldMarker{ID: interaction.Target.ID, Kind: "interaction", Label: "Взаимодействие", Bounds: worldRect(interaction.Target.Bounds)})
	}
	if boss.Encounter != "" {
		bossPosition := Point{X: boss.Bounds.X + boss.Bounds.Width/2, Y: boss.Bounds.Y}
		data.Entities = append(data.Entities, EntityView{ID: string(boss.Encounter), Kind: "boss", Asset: bossAsset(boss, code), State: string(boss.Stage), X: bossPosition.X, Y: bossPosition.Y, PreviousX: bossPosition.X, PreviousY: bossPosition.Y, Width: boss.Bounds.Width, Height: boss.Bounds.Height})
		arena := service.bossArena.View(ctx)
		for _, shot := range arena.Projectiles {
			data.Entities = append(data.Entities, EntityView{ID: shot.ID, Kind: "projectile", State: "active", X: shot.Position.X, Y: shot.Position.Y, PreviousX: shot.Position.X, PreviousY: shot.Position.Y})
			data.World.Markers = append(data.World.Markers, WorldMarker{ID: shot.ID, Kind: "projectile", Bounds: WorldRect{X: shot.Position.X, Y: shot.Position.Y}})
		}
		if arena.WasteHeight > 0 && arena.WasteDamage > 0 {
			data.World.Markers = append(data.World.Markers, WorldMarker{ID: "boss-waste", Kind: "danger", State: "active", Label: "Опасные отходы", Bounds: WorldRect{Width: 1280, Height: float64(arena.WasteHeight)}})
		}
		label := "УЯЗВИМ"
		if boss.Protected {
			label = "ЗАЩИТА"
		}
		data.World.Markers = append(data.World.Markers, WorldMarker{ID: string(boss.Encounter), Kind: "boss", State: string(boss.Stage), Label: label, Bounds: worldRect(boss.Bounds)})
		for i, area := range boss.Danger {
			state := "warning"
			if boss.Stage == simulation.BossActive {
				state = "active"
			}
			data.World.Markers = append(data.World.Markers, WorldMarker{ID: fmt.Sprintf("%s:danger:%d", boss.Encounter, i), Kind: "danger", State: state, Label: "Атака босса", Bounds: worldRect(area)})
		}
		for _, relay := range boss.Relays {
			if relay.Required && relay.Health > 0 {
				label := map[string]string{"A": "А", "B": "Б"}[relay.ID]
				data.World.Markers = append(data.World.Markers, WorldMarker{ID: relay.ID, Kind: "relay", Label: "Реле " + label, Bounds: worldRect(relay.Bounds)})
			}
		}
	}
	terrain, err := service.world.Query(ctx, simulation.Rect{X: body.X - 2048, Y: body.Y - 1024, Width: 4096, Height: 2048})
	if err != nil {
		return fmt.Errorf("world terrain: %w", err)
	}
	for _, collider := range terrain {
		if collider.Kind != simulation.ProjectileTarget {
			data.World.Terrain = append(data.World.Terrain, worldRect(collider.Bounds))
		}
	}
	if status.CanCarry {
		data.Aim.Reticle = service.policy.input.reticle
		aim := service.policy.input.values[settings.Aim]
		data.Aim.Direction = Point{X: aim.X, Y: aim.Y}
		if aim.X == 0 && aim.Y == 0 {
			data.Aim.Direction.X = float64(status.Facing)
		}
		// The boss and recruitment executor supplies these targets only for a
		// live attempt. Preview the same transient colliders as the next tick.
		var limbTargets []simulation.Collider
		var fecesTargets []simulation.FecesTarget
		if boss.Encounter != "" && boss.Stage != simulation.BossDormant && boss.Stage != simulation.BossPending && boss.Stage != simulation.BossDefeated {
			collider := simulation.Collider{ID: string(boss.Encounter), Kind: simulation.ProjectileTarget, Bounds: boss.Bounds}
			limbTargets = append(limbTargets, collider)
			fecesTargets = append(fecesTargets, simulation.FecesTarget{Collider: collider, Kind: simulation.FecesBoss, Exposed: !boss.Protected})
			for _, relay := range boss.Relays {
				if relay.Required && relay.Health > 0 {
					collider := simulation.Collider{ID: "boss-relay-" + relay.ID, Kind: simulation.ProjectileTarget, Bounds: relay.Bounds}
					limbTargets = append(limbTargets, collider)
					fecesTargets = append(fecesTargets, simulation.FecesTarget{Collider: collider, Kind: simulation.FecesSource})
				}
			}
		}
		feed := service.world.Recruitment(ctx)
		if feed.ID != "" && !feed.Disconnected && feed.Health > 0 {
			collider := simulation.Collider{ID: feed.ID, Kind: simulation.ProjectileTarget, Bounds: feed.Bounds}
			limbTargets = append(limbTargets, collider)
			fecesTargets = append(fecesTargets, simulation.FecesTarget{Collider: collider, Kind: simulation.FecesSource, Ticket: feed.Ticket})
		}
		var points []simulation.Vector
		var impact *simulation.Impact
		kind := "terrain"
		if limbs.LimbPayload {
			preview, previewErr := service.world.PreviewLimbThrow(ctx, simulation.Vector{}, limbTargets)
			if previewErr != nil {
				return fmt.Errorf("limb preview: %w", previewErr)
			}
			points, impact = preview.Points, preview.Impact
			if impact != nil && impact.Collider.Kind == simulation.ProjectileTarget {
				kind = "invalid"
				for _, object := range service.world.Objects(ctx) {
					if object.Kind == simulation.ObjectPaddle && "object:"+object.ID == impact.Collider.ID && !object.Latched {
						kind = "receptor"
					}
				}
			}
		} else if current.Feces > 0 {
			preview, previewErr := service.world.PreviewThrow(ctx, simulation.Vector{}, fecesTargets)
			if previewErr != nil {
				return fmt.Errorf("feces preview: %w", previewErr)
			}
			points, impact = preview.Points, preview.Impact
			if impact != nil && impact.Collider.Kind == simulation.ProjectileTarget {
				kind = "invalid"
				if preview.Kind == string(simulation.FecesReceptor) {
					kind = "receptor"
				}
			}
		}
		for _, point := range points {
			data.Aim.Trajectory = append(data.Aim.Trajectory, Point{X: point.X, Y: point.Y})
		}
		data.Aim.Visible = len(points) != 0
		if impact != nil && len(points) > 0 {
			last := points[len(points)-1]
			data.Aim.HasImpact, data.Aim.Impact, data.Aim.ImpactKind = true, Point{X: last.X, Y: last.Y}, kind
		}
	}
	return nil
}

// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

func validateVisualSources(ctx context.Context, characters characterRegister, env environmentRegister, animation animationRegister, ui interfaceRegister) (err error) {
	defer mon.Task()(&ctx)(&err)

	if characters.SchemaVersion != 1 || env.SchemaVersion != 1 || animation.SchemaVersion != 1 || ui.SchemaVersion != 1 {
		return fmt.Errorf("unsupported visual register schema")
	}
	codes := []string{"HOS", "CHU", "MIN", "INS"}
	principalCodes := []string{"GRI", "VER", "POT", "SAV", "BLA", "LEV", "NIN", "SAT"}
	if len(characters.Principals) != len(principalCodes) || len(characters.EnemySets) != 4 || len(env.Institutions) != 4 || len(env.Families) != 4 {
		return fmt.Errorf("incomplete character or environment inventory")
	}
	for i, code := range principalCodes {
		if characters.Principals[i].Code != code || !allText(characters.Principals[i].Direction) {
			return fmt.Errorf("invalid principal %s", code)
		}
	}
	if !slices.Equal(characters.Expressions, []string{"NEUTRAL", "ANGER", "PAIN", "FEAR", "DISGUST", "RELIEF"}) || characters.CrowdHeads < 6 || characters.CrowdTorsos < 4 || characters.CrowdActivities < 3 {
		return fmt.Errorf("invalid expressions or crowd kit")
	}
	for i, code := range codes {
		enemy, institution := characters.EnemySets[i], env.Institutions[i]
		if enemy.Institution != code || !allText(enemy.Melee, enemy.Ranged) || institution.Code != code || !allText(institution.Direction) || len(institution.Palette) != 4 {
			return fmt.Errorf("invalid institution %s", code)
		}
		if !validVisualPalette(institution.Palette) {
			return fmt.Errorf("invalid palette for %s", code)
		}
	}
	families := []string{"SUP", "RES", "UTL", "PRV"}
	roles := [][]string{{"TROLLEY", "PAD", "TERMINAL", "CRUSHER"}, {"ACTOR", "PLATE", "CRATE", "GATE", "LATCH", "REFUGE", "TERMINAL"}, {"PADDLE", "SWITCH", "FUSE", "TERMINAL"}, {"RECORDER", "CAMERA", "TERMINAL"}}
	states := [][]string{{"INTACT", "BROKEN"}, {"CLOSED", "WEIGHTEDOPEN", "LATCHEDOPEN"}, {"UNLATCHED", "LATCHED"}, {"CLEAR", "FOULED", "RECORDEDINTERRUPTION"}}
	for i, family := range env.Families {
		if family.ID != families[i] || !allText(family.Direction) || !slices.Equal(family.Roles, roles[i]) || !slices.Equal(family.States, states[i]) {
			return fmt.Errorf("invalid family %s", family.ID)
		}
	}
	if !slices.Equal(env.EmergencyStates, []string{"WARN", "ACTIVE", "AFTER"}) || len(env.Prologue) != 3 || !allText(env.Prologue...) || !slices.Equal(env.Endings, []string{"DEATH", "HELL", "CEREMONY", "TABLEAU"}) || !allText(env.Tableau) {
		return fmt.Errorf("invalid emergency, prologue or ending inventory")
	}
	playerStates := []string{"IDLE", "RUN", "HOP", "CRAWL", "JUMP", "EVADE", "MELEE", "THROW", "REMOVE", "TEETH", "REPLACE", "REGEN", "HURT", "SLIP", "PROP", "INTERACT", "DEAD", "RESTORE"}
	poses := []int{6, 8, 6, 6, 5, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 1, 2}
	durations := []int{1000, 500, 600, 700, 0, 180, 450, 400, 350, 350, 400, 1500, 180, 350, 700, 0, 0, 0}
	markers := []string{"loop", "footfall", "footfall", "loop", "launch apex landing", "start immunity_end end", "active_start active_end recovery_end", "release recovery_end", "separation", "separation", "completion", "completion", "impact stun_end immunity_end", "start end", "launch landing", "completion", "death", "resume"}
	interruptions := []string{"any", "any", "any", "any", "authoritative_action", "death lethal", "higher_priority", "higher_priority", "higher_priority release_input", "higher_priority release_input", "higher_priority release_input", "higher_priority release_input", "death", "higher_priority", "higher_priority", "higher_priority release_input", "state_change", "state_change"}
	if len(animation.Player) != len(playerStates) {
		return fmt.Errorf("invalid player clip inventory")
	}
	for i, clip := range animation.Player {
		if clip.State != playerStates[i] || clip.Poses != poses[i] || clip.DurationMS != durations[i] || !allText(clip.Direction) || !slices.Equal(clip.Markers, strings.Fields(markers[i])) || !slices.Equal(clip.Interruptions, strings.Fields(interruptions[i])) {
			return fmt.Errorf("invalid clip %s", clip.State)
		}
	}
	if !slices.Equal(animation.Sockets, []string{"LIM-LA", "LIM-RA", "LIM-LL", "LIM-RL"}) || !slices.Equal(animation.EnemyStates, []string{"IDLE", "APPROACH", "TELEGRAPH", "ACTIVE", "RECOVERY", "HURT", "DISTRACTION", "DEFEAT"}) || !slices.Equal(animation.RescueStates, []string{"WAIT", "FOLLOW", "SAFESTOP", "REFUGE", "INCAPACITATED"}) || !slices.Equal(animation.BossPhases, []string{"PREP", "ACTIVE", "RECOVER"}) {
		return fmt.Errorf("invalid animation states or sockets")
	}
	if !slices.Equal(ui.HUD, []string{"BODY", "PAYLOAD", "CREDIT", "OBJECTIVE", "BOSS", "CHANNEL", "NOTICE", "CAPTION", "SAVE"}) || !slices.Equal(ui.Menus, []string{"TITLE", "ADULT", "PAUSE", "SETTINGS", "DISPLAY", "AUDIO", "INTERFACE", "ACCESSIBILITY", "CONTROLS", "CONFIRM", "FOCUS", "DISABLED", "TUTORIAL", "PROMPT", "END"}) || !slices.Equal(ui.Ledger, []string{"SERVICES", "CONSEQUENCES", "TUTORIALS", "AVAILABLE", "ACTIVE", "READY", "CERTIFIED", "FAILED", "RECEIPT", "WARNING", "EMERGENCY", "AFTERMATH"}) || !slices.Equal(ui.Recovery, []string{"SAV-PENDING", "SAV-OK", "SAV-ERR", "REC-DEAD", "REC-RETURN", "REC-LOAD", "REC-READY", "REC-OLDER", "REC-BROKEN", "REC-SETTINGS"}) || !slices.Equal(ui.Effects, []string{"BODY", "FEC", "IMPACT", "HAZARD"}) {
		return fmt.Errorf("incomplete interface inventory")
	}
	if !slices.Equal(ui.Markers, []string{"DANGER", "REFUGE", "INTERACTION", "RECEPTOR", "PROTECTED", "EXPOSED", "TRAJECTORY", "IMPACT-TERRAIN", "IMPACT-VALID", "IMPACT-INVALID", "LOW-HEALTH", "HURT-IMMUNITY", "REACTION-IMMUNITY"}) || !slices.Equal(ui.Signs, []string{"TXT-SIGN-HOS", "TXT-SIGN-CHU", "TXT-SIGN-MIN", "TXT-SIGN-INS", "TXT-SIGN-HOS-DONATION", "TXT-SIGN-HOS-LIFE", "TXT-SIGN-HOS-WING", "TXT-SIGN-HOS-DISPATCH", "TXT-SIGN-HOS-VOLUNTEER", "TXT-SIGN-CHU-CONFESSION", "TXT-SIGN-CHU-ARCHIVE", "TXT-SIGN-MIN-CONTROL", "TXT-SIGN-MIN-PERMIT", "TXT-SIGN-INS-SOURCE", "TXT-SIGN-INS-EXIT", "TXT-SIGN-APPOINTMENT", "TXT-SIGN-CASINO"}) {
		return fmt.Errorf("incomplete marker or sign inventory")
	}
	return nil
}

func validVisualPalette(colors []string) bool {
	if len(colors) != 4 {
		return false
	}
	for _, color := range colors {
		if len(color) != 7 || color[0] != '#' || strings.Trim(color[1:], "0123456789ABCDEFabcdef") != "" {
			return false
		}
	}
	return true
}

func expandVisualSources(ctx context.Context, characters characterRegister, env environmentRegister, animation animationRegister, ui interfaceRegister, campaign *Campaign, text *RussianText) (_ []VisualRecord, err error) {
	defer mon.Task()(&ctx)(&err)

	var records []VisualRecord
	for _, principal := range characters.Principals {
		id := "ART-CHR-" + principal.Code
		sheet := visualBase(id, "character-sheet", principal.Direction, "CHR-"+principal.Code)
		sheet.States = []string{"FRONT", "BACK", "LEFT", "RIGHT", "SILHOUETTE", "SWATCHES", "SEPARATE-TEXT"}
		sheet.Technical.ReferenceCharacterWidth = 48
		sheet.Technical.ReferenceCharacterHeight = 80
		if slices.Contains([]string{"POT", "SAV", "BLA", "LEV"}, principal.Code) {
			sheet.States = append(sheet.States, "TELEGRAPH", "EXPOSED", "DAMAGED", "CEREMONY")
		}
		if principal.Code == "GRI" {
			sheet.Technical.Pivot = "standing collision box bottom center"
			sheet.States = append(sheet.States, "LIM-LA", "LIM-RA", "LIM-LL", "LIM-RL")
		}
		records = append(records, sheet)
		for i, expression := range characters.Expressions {
			records = append(records, visualBase(fmt.Sprintf("%s-EXP-%02d", id, i+1), "expression", expression, id))
		}
	}
	records = append(records, visualBase("ART-CHR-GRI-MINISTER", "costume", "Cream ceremonial jacket; retain Grisha's face, posture and uneven shoes.", "CHR-GRI", "NAR-11", "NAR-12", "END-TABLEAU"))
	for _, enemy := range characters.EnemySets {
		for _, item := range []struct{ code, direction string }{{"MEL", enemy.Melee}, {"RNG", enemy.Ranged}} {
			record := visualBase("ART-EN-"+enemy.Institution+"-"+item.code, "enemy-set", item.direction, "LOC-"+enemy.Institution, "EN-"+item.code)
			if enemy.Institution == "INS" && item.code == "MEL" {
				record.Bindings = append(record.Bindings, "EN-TRE")
			}
			records = append(records, record)
		}
	}
	for _, institution := range env.Institutions {
		code := institution.Code
		set := visualBase("ART-SET-"+code, "environment", institution.Direction, "LOC-"+code)
		set.Palette = slices.Clone(institution.Palette)
		set.Technical.StructuralUnit = 32
		records = append(records, set)
		crowd := visualBase("ART-CROWD-"+code, "crowd-kit", fmt.Sprintf("Adult modular crowd: at least %d heads, %d torsos and %d idle activities; no combat targets.", characters.CrowdHeads, characters.CrowdTorsos, characters.CrowdActivities), "LOC-"+code)
		records = append(records, crowd)
		boss := visualBase("ART-BOSS-"+code, "boss-arena", "Arena, machinery, protected alcoves, closure terminal and surviving-defeat arrangement.", "LOC-"+code, "ENC-"+code+"-05", "ANC-"+code+"-05", "ANC-"+code+"-OUT", "D-"+code+"-END", "CON-D-"+code+"-END")
		for _, deed := range campaign.Deeds(ctx) {
			if deed.Family == "END" && deed.Location == LocationID("LOC-"+code) {
				for _, object := range deed.Deed.Objects {
					boss.Bindings = addUnique(boss.Bindings, string(object))
				}
			}
		}
		closureNarrative := map[string]string{"HOS": "NAR-05", "CHU": "NAR-06", "MIN": "NAR-07", "INS": "NAR-09"}
		boss.Bindings = append(boss.Bindings, closureNarrative[code])
		boss.Technical.StructuralUnit = 32
		records = append(records, boss)
		records = append(records, visualBase("ART-TRANSIT-"+code, "transit", "Safe transit stop and separate destination sign.", "LOC-"+code, "ANC-"+code+"-OUT", "TXT-SIGN-"+code))
		for i, family := range env.Families {
			encounter := fmt.Sprintf("ENC-%s-%02d", code, i+1)
			anchor := fmt.Sprintf("ANC-%s-%02d", code, i+1)
			bay := visualBase("ART-BAY-"+code+"-"+family.ID, "service-bay", family.Direction+" Vestibule, clear terminal, checkpoint and marked bypass.", "LOC-"+code, encounter, anchor)
			bay.Technical.StructuralUnit = 32
			for _, deed := range campaign.Deeds(ctx) {
				if string(deed.Location) == "LOC-"+code && deed.Family == family.ID {
					bay.Bindings = append(bay.Bindings, string(deed.Deed.ID), string(deed.Deed.Consequence))
				}
			}
			records = append(records, bay)
			for _, stage := range env.EmergencyStates {
				id := "EM-" + code + "-" + family.ID
				var direction string
				for _, batch := range campaign.Batches(ctx) {
					if string(batch.Emergency.ID) == id {
						switch stage {
						case "WARN":
							direction = "1500 ms warning: " + batch.Hazard
						case "ACTIVE":
							direction = "8000 ms active: " + batch.Hazard
						case "AFTER":
							direction = batch.Recovery
						}
					}
				}
				if direction == "" {
					return nil, fmt.Errorf("missing campaign emergency %s", id)
				}
				records = append(records, visualBase("ART-"+id+"-"+stage, "emergency", direction, id, encounter, anchor, "LOC-"+code))
			}
		}
	}
	prologueNarratives := [][]string{{"NAR-01"}, {"NAR-01"}, {"NAR-02", "NAR-03"}}
	for i, direction := range env.Prologue {
		n := i + 1
		bindings := []string{fmt.Sprintf("ENC-PRO-%02d", n), fmt.Sprintf("ANC-PRO-%02d", n)}
		bindings = append(bindings, prologueNarratives[i]...)
		records = append(records, visualBase(fmt.Sprintf("ART-PRO-%02d", n), "prologue", direction, bindings...))
	}
	for _, stage := range env.Endings {
		direction := "Editable foreground, character, effect and separately addressable text layers."
		bindings := []string{"END-" + stage, "ANC-END-" + stage}
		switch stage {
		case "DEATH":
			bindings = append(bindings, "NAR-10")
		case "HELL":
			bindings = append(bindings, "NAR-11", "TXT-SIGN-APPOINTMENT")
		case "CEREMONY":
			bindings = append(bindings, "NAR-12", "TXT-SIGN-CASINO")
		case "TABLEAU":
			direction = env.Tableau
			bindings = append(bindings, "NAR-12", "TXT-SIGN-HOS-WING", "TXT-ENDING-TABLEAU", "ART-CHR-GRI-MINISTER", "ART-CHR-POT", "ART-CHR-SAV", "ART-CHR-BLA", "ART-CHR-LEV", "ART-CHR-NIN")
		}
		record := visualBase("ART-END-"+stage, "ending", direction, bindings...)
		if stage == "TABLEAU" {
			record.Tableau = &TableauRequirement{AspectWidth: 16, AspectHeight: 9, WingMinimumQuarter: true, RibbonAndMinisterLowerCenter: true, BossesFlankMinister: true, FlamesCrossWingWindows: true, NinaEvacuatesOpposite: true, ClosingTextClearUpperBand: true}
		}
		records = append(records, record)
	}
	hell := visualBase("ART-SET-HELL", "environment", "Basalt records room, ordinary desk and biography shelf; ash, coal, ember and brass.", "NAR-11", "END-HELL")
	hell.Palette = []string{"#C1B4A2", "#25211F", "#A85636", "#8D7755"}
	ceremony := visualBase("ART-SET-CER", "environment", "Casino bulbs and converted charity entrance; clearly labelled burning children's wing.", "NAR-12", "END-CEREMONY", "END-TABLEAU")
	ceremony.Palette = []string{"#E7D4A5", "#9B3948", "#45434A", "#E58A3E"}
	records = append(records, hell, ceremony)
	objectRecords, err := expandVisualObjects(ctx, env, campaign, text)
	if err != nil {
		return nil, err
	}
	records = append(records, objectRecords...)
	records = append(records, expandVisualAnimations(ctx, animation, campaign)...)
	uiRecords, err := expandVisualInterface(ctx, ui, text)
	if err != nil {
		return nil, err
	}
	records = append(records, uiRecords...)
	for i := range records {
		completeAnimationRequirements(&records[i])
	}
	return records, nil
}

func completeAnimationRequirements(record *VisualRecord) {
	clip := record.Animation
	if clip == nil {
		return
	}
	if clip.Poses > 0 {
		clip.PoseCountStatus = "specified"
	} else {
		clip.PoseCountStatus = "unresolved"
	}
	if len(clip.PoseRequirements) == 0 {
		clip.PoseRequirements = []string{record.Direction}
	}
	if clip.DurationMode == "" {
		clip.DurationMode = "event"
		if clip.DurationMS > 0 {
			clip.DurationMode = "fixed"
		}
	}
	for i := range clip.Markers {
		if clip.Markers[i].Timing == "" {
			clip.Markers[i].Timing = "event"
		}
	}
}

func validateVisualBindings(ctx context.Context, records []VisualRecord, campaign *Campaign, text *RussianText) (err error) {
	defer mon.Task()(&ctx)(&err)

	assets := make(map[string]bool, len(records))
	deeds := make(map[string]bool)
	objects := make(map[string]bool)
	consequences := make(map[string]bool)
	emergencies := make(map[string]bool)
	for _, record := range records {
		assets[record.ID] = true
	}
	for _, deed := range campaign.Deeds(ctx) {
		deeds[string(deed.Deed.ID)] = true
		consequences[string(deed.Deed.Consequence)] = true
	}
	for _, object := range campaign.Objects(ctx) {
		objects[string(object.ID)] = true
	}
	for _, batch := range campaign.Batches(ctx) {
		emergencies[string(batch.Emergency.ID)] = true
	}
	for _, record := range records {
		seen := make(map[string]bool)
		for _, id := range record.Bindings {
			if seen[id] {
				return fmt.Errorf("%s: duplicate binding %s", record.ID, id)
			}
			seen[id] = true
			valid := true
			switch {
			case strings.HasPrefix(id, "ART-"):
				valid = assets[id]
			case strings.HasPrefix(id, "TXT-") || strings.HasPrefix(id, "CAP-") || strings.HasPrefix(id, "TUT-"):
				_, valid = text.Entry(id)
			case strings.HasPrefix(id, "LOC-"):
				valid = locationPattern.MatchString(id)
			case strings.HasPrefix(id, "ENC-"):
				valid = encounterPattern.MatchString(id)
			case strings.HasPrefix(id, "ANC-"):
				valid = anchorPattern.MatchString(id)
			case strings.HasPrefix(id, "NAR-"):
				valid = narrativePattern.MatchString(id)
			case strings.HasPrefix(id, "END-"):
				valid = endingPattern.MatchString(id)
			case strings.HasPrefix(id, "D-"):
				valid = deeds[id]
			case strings.HasPrefix(id, "OBJ-"):
				valid = objects[id]
			case strings.HasPrefix(id, "CON-"):
				valid = consequences[id]
			case strings.HasPrefix(id, "EM-"):
				valid = emergencies[id]
			}
			if !valid {
				return fmt.Errorf("%s: unresolved binding %s: %w", record.ID, id, ErrMissingReference)
			}
		}
	}
	return nil
}

func expandVisualObjects(ctx context.Context, env environmentRegister, campaign *Campaign, text *RussianText) (_ []VisualRecord, err error) {
	defer mon.Task()(&ctx)(&err)

	var records []VisualRecord
	deeds := campaign.Deeds(ctx)
	objects := campaign.Objects(ctx)
	for _, family := range env.Families {
		for _, role := range family.Roles {
			base := visualBase("ART-OBJ-"+family.ID+"-"+role, "family-object", family.Direction, "FAM-"+family.ID)
			base.States = slices.Clone(family.States)
			records = append(records, base)
			for _, institution := range env.Institutions {
				code := institution.Code
				finish := visualBase("ART-OBJ-"+code+"-"+family.ID+"-"+role, "object-finish", "Institutional finish and ticket label; "+family.Direction, "LOC-"+code, base.ID)
				finish.States = slices.Clone(family.States)
				for _, deed := range deeds {
					if string(deed.Location) != "LOC-"+code || deed.Family != family.ID {
						continue
					}
					found := false
					for _, object := range objects {
						if string(object.Role) == role && slices.Contains(deed.Deed.Objects, object.ID) {
							finish.Bindings = addUnique(finish.Bindings, string(object.ID))
							found = true
						}
					}
					if !found {
						return nil, fmt.Errorf("%s: missing object role %s", deed.Deed.ID, role)
					}
					label := "TXT-" + string(deed.Deed.ID)
					if _, ok := text.Entry(label); !ok {
						return nil, fmt.Errorf("missing ticket text %s", label)
					}
					finish.Bindings = addUnique(finish.Bindings, string(deed.Deed.ID))
					finish.Bindings = addUnique(finish.Bindings, label)
				}
				records = append(records, finish)
			}
		}
	}
	for _, deed := range deeds {
		if deed.Family != "RES" {
			continue
		}
		code := strings.TrimPrefix(string(deed.Location), "LOC-")
		actor := "OBJ-" + string(deed.Deed.ID) + "-ACTOR"
		nameID := "TXT-" + actor
		name, ok := text.Entry(nameID)
		if !ok || name.Reference != actor || name.Text != deed.Label {
			return nil, fmt.Errorf("missing rescue adult name %s", nameID)
		}
		found := false
		for _, object := range objects {
			if string(object.ID) == actor && object.Deed == deed.Deed.ID && object.Role == RoleActor {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("missing rescue actor %s", actor)
		}
		records = append(records, visualBase(fmt.Sprintf("ART-RES-%s-%02d", code, deed.Variant), "rescue-adult", "Distinct head, hair, clothing, posture and portrait: "+name.Text, string(deed.Location), string(deed.Deed.Encounter), string(deed.Deed.ID), actor, nameID))
	}
	return records, nil
}

func expandVisualAnimations(ctx context.Context, source animationRegister, campaign *Campaign) []VisualRecord {
	defer mon.Task()(&ctx)(nil)

	var records []VisualRecord
	for _, clip := range source.Player {
		record := visualBase("AN-GRI-"+clip.State, "player-animation", clip.Direction, "ART-CHR-GRI")
		record.Technical.Pivot = "standing collision box bottom center"
		animation := &AnimationRequirement{Poses: clip.Poses, DurationMS: clip.DurationMS, DrawingsPerSecond: 12, Sockets: slices.Clone(source.Sockets), Layers: []string{"torso", "head", "sockets", "limbs"}, Interruptions: slices.Clone(clip.Interruptions)}
		for _, socket := range source.Sockets {
			animation.SocketPositions = append(animation.SocketPositions, SocketPosition{Socket: socket})
		}
		if slices.Contains([]string{"IDLE", "RUN", "HOP", "CRAWL"}, clip.State) {
			animation.DurationMode = "reference"
		} else if clip.State == "PROP" {
			animation.DurationMode = "maximum"
		}
		for _, marker := range clip.Markers {
			entry := AnimationMarker{Event: marker}
			switch clip.State + ":" + marker {
			case "EVADE:immunity_end":
				entry.Milliseconds = 120
			case "MELEE:active_start":
				entry.Milliseconds = 120
			case "MELEE:active_end":
				entry.Milliseconds = 200
			case "MELEE:recovery_end", "THROW:recovery_end":
				entry.Milliseconds = clip.DurationMS
			case "THROW:release":
				entry.Milliseconds = 100
			case "REMOVE:separation", "TEETH:separation", "REPLACE:completion", "REGEN:completion", "HURT:stun_end", "SLIP:end":
				entry.Milliseconds = clip.DurationMS
			case "HURT:immunity_end":
				entry.Milliseconds = 800
			}
			if entry.Milliseconds > 0 || marker == "start" || marker == "impact" {
				entry.Timing = "milliseconds"
			} else {
				entry.Timing = "event"
			}
			animation.Markers = append(animation.Markers, entry)
		}
		record.Animation = animation
		records = append(records, record)
		if clip.State == "MELEE" {
			headbutt := copyVisual(record)
			headbutt.ID = "AN-GRI-MELEE-HEADBUTT"
			headbutt.Direction = "No-arm headbutt: 120 ms preparation, 80 ms active and 300 ms recovery."
			headbutt.Animation.DurationMS = 500
			for i := range headbutt.Animation.Markers {
				if headbutt.Animation.Markers[i].Event == "recovery_end" {
					headbutt.Animation.Markers[i].Milliseconds = 500
				}
			}
			records = append(records, headbutt)
		}
	}
	for _, transition := range []struct {
		id        string
		direction string
	}{
		{"AN-GRI-BOTH-ARM-LOSS", "Authored transition to armless movement and headbutt; show both missing arm sockets."},
		{"AN-GRI-BOTH-LEG-LOSS", "Authored transition to legless crawl; show both missing leg sockets without stretching the run cycle."},
	} {
		record := visualBase(transition.id, "player-animation", transition.direction, "ART-CHR-GRI")
		record.Technical.Pivot = "standing collision box bottom center"
		clip := &AnimationRequirement{DrawingsPerSecond: 12, Sockets: slices.Clone(source.Sockets), Layers: []string{"torso", "head", "sockets", "limbs"}, Markers: []AnimationMarker{{Event: "socket_change", Timing: "event"}}, Interruptions: []string{"simulation_state_change"}}
		for _, socket := range source.Sockets {
			clip.SocketPositions = append(clip.SocketPositions, SocketPosition{Socket: socket})
		}
		record.Animation = clip
		records = append(records, record)
	}
	for _, code := range []string{"HOS", "CHU", "MIN", "INS"} {
		for _, kind := range []string{"MEL", "RNG"} {
			for _, state := range source.EnemyStates {
				pose := map[string]string{"IDLE": "Idle stance", "APPROACH": "Approach with visible weapon", "TELEGRAPH": "Preparation exposes dangerous weapon side and ranged release origin", "ACTIVE": "Strike or projectile release pose", "RECOVERY": "Uncontrolled exposed recovery", "HURT": "Damage recoil", "DISTRACTION": "Cover face without removing airborne shots", "DEFEAT": "Nonblocking defeated body without threat marker"}[state]
				record := visualBase("AN-EN-"+code+"-"+kind+"-"+state, "enemy-animation", pose, "ART-EN-"+code+"-"+kind, "LOC-"+code)
				clip := &AnimationRequirement{DrawingsPerSecond: 12, Interruptions: []string{"simulation_state_change"}, Markers: []AnimationMarker{{Event: "simulation_state_change", Timing: "event"}}}
				if state == "TELEGRAPH" {
					clip.DurationMS = 500
					if kind == "RNG" {
						clip.DurationMS = 650
					}
				}
				if state == "RECOVERY" {
					clip.DurationMS = 650
					if kind == "RNG" {
						clip.DurationMS = 900
					}
					clip.Markers = []AnimationMarker{{Event: "recovery_start", Timing: "milliseconds"}, {Event: "recovery_end", Milliseconds: clip.DurationMS, Timing: "milliseconds"}}
				}
				if state == "TELEGRAPH" {
					clip.Markers = []AnimationMarker{{Event: "telegraph_start", Timing: "milliseconds"}, {Event: "strike_or_release", Milliseconds: clip.DurationMS, Timing: "milliseconds"}}
				}
				if state == "ACTIVE" {
					clip.Markers = []AnimationMarker{{Event: "strike_or_release", Timing: "event"}, {Event: "recovery_start", Timing: "event"}}
				}
				record.Animation = clip
				if code == "INS" && kind == "MEL" {
					record.Bindings = append(record.Bindings, "EN-TRE")
				}
				records = append(records, record)
			}
		}
	}
	for _, deed := range campaign.Deeds(ctx) {
		if deed.Family != "RES" {
			continue
		}
		code := strings.TrimPrefix(string(deed.Location), "LOC-")
		for _, state := range source.RescueStates {
			art := fmt.Sprintf("ART-RES-%s-%02d", code, deed.Variant)
			pose := map[string]string{"WAIT": "Protected adult waits at the gate", "FOLLOW": "Adult follows at walking pace", "SAFESTOP": "Adult stops at the marked waiting point", "REFUGE": "Adult shows immediate relief, not institutional celebration", "INCAPACITATED": "Protected adult visibly incapacitated, not a lethal combat target"}[state]
			record := visualBase(fmt.Sprintf("AN-RES-%s-%02d-%s", code, deed.Variant, state), "rescue-animation", pose, art, string(deed.Deed.ID), "OBJ-"+string(deed.Deed.ID)+"-ACTOR")
			record.Animation = &AnimationRequirement{DrawingsPerSecond: 12, Markers: []AnimationMarker{{Event: "simulation_state_change", Timing: "event"}}, Interruptions: []string{"simulation_state_change"}}
			records = append(records, record)
		}
	}
	for _, boss := range BossDefinitions(ctx) {
		code := strings.TrimSuffix(strings.TrimPrefix(string(boss.Encounter), "ENC-"), "-05")
		for _, attack := range boss.Attacks {
			for _, phase := range source.BossPhases {
				record := visualBase("AN-BOSS-"+attack.ID+"-"+phase, "boss-animation", "Pose and danger marker follow authoritative telegraph, locked target, attack and exposed recovery; no animation changes timing.", string(boss.Encounter), "ART-BOSS-"+code, attack.ID)
				clip := &AnimationRequirement{DrawingsPerSecond: 12, Interruptions: []string{"threshold_transition", "defeat"}}
				switch phase {
				case "PREP":
					clip.DurationMS = attack.TelegraphMS
					clip.PoseRequirements = []string{"Protected preparation pose with readable warning and locked target."}
					clip.Markers = []AnimationMarker{{Event: "telegraph_start", Timing: "milliseconds"}, {Event: "target_lock", Milliseconds: attack.TelegraphMS, Timing: "milliseconds"}}
				case "ACTIVE":
					clip.DurationMode = "simulation"
					clip.DurationMS = attack.ActiveMS
					clip.TickRate = 120
					clip.MinimumTicks = bossActiveTicks(attack.ID)
					clip.PoseRequirements = []string{"Protected attack pose and visible machinery or projectile trajectory."}
					clip.Markers = bossActiveMarkers(attack.ID, clip.MinimumTicks)
				case "RECOVER":
					clip.DurationMS = attack.RecoveryMS
					clip.DurationMode = "minimum"
					clip.PoseRequirements = []string{"Exposed recovery pose; relay protection can keep the institute boss protected."}
					clip.Markers = []AnimationMarker{{Event: "recovery_start", Timing: "milliseconds"}, {Event: "minimum_recovery", Milliseconds: attack.RecoveryMS, Timing: "milliseconds"}, {Event: "recovery_end", Timing: "event"}}
				}
				record.Animation = clip
				records = append(records, record)
			}
		}
		for _, state := range []string{"TRANSITION", "DEFEAT"} {
			record := visualBase("AN-BOSS-"+code+"-"+state, "boss-animation", "Protected transition or surviving defeat; confirmation waits for durable storage.", string(boss.Encounter), "ART-BOSS-"+code)
			record.Animation = &AnimationRequirement{DrawingsPerSecond: 12, Markers: []AnimationMarker{{Event: "simulation_state_change"}}, Interruptions: []string{"simulation_state_change"}}
			if state == "TRANSITION" {
				record.Animation.DurationMS = boss.TransitionMS
			}
			records = append(records, record)
		}
	}
	return records
}

// Local encounter executors extend the shared authored minimum to these 120 Hz
// tick counts. Completion is still a simulation boundary, not an art timer.
func bossActiveTicks(id string) int {
	switch id {
	case "HOS-B":
		return 338
	case "HOS-C", "CHU-B", "INS-A":
		return 48
	case "CHU-C":
		return 436
	case "MIN-A", "MIN-C":
		return 270
	case "MIN-B", "INS-B":
		return 72
	case "INS-C":
		return 120
	default:
		return 1
	}
}

func bossActiveMarkers(id string, durationTicks int) []AnimationMarker {
	markers := []AnimationMarker{{Event: "active_start", Timing: "ticks"}}
	switch id {
	case "CHU-C":
		markers = append(markers, AnimationMarker{Event: "release-1", Timing: "ticks"}, AnimationMarker{Event: "release-2", Tick: 42, Timing: "ticks"}, AnimationMarker{Event: "release-3", Tick: 84, Timing: "ticks"})
	case "MIN-A":
		markers = append(markers, AnimationMarker{Event: "release-1", Tick: 30, Timing: "ticks"}, AnimationMarker{Event: "release-2", Tick: 60, Timing: "ticks"}, AnimationMarker{Event: "release-3", Tick: 90, Timing: "ticks"})
	case "INS-C":
		markers = append(markers, AnimationMarker{Event: "release-1", Timing: "ticks"}, AnimationMarker{Event: "release-2", Timing: "ticks"})
	case "HOS-B":
		markers = append(markers, AnimationMarker{Event: "release-1", Timing: "ticks"})
	case "HOS-A", "CHU-A":
		markers = append(markers, AnimationMarker{Event: "impact", Timing: "ticks"})
	default:
		// Local executors apply area impact on their final active tick.
		markers = append(markers, AnimationMarker{Event: "impact", Tick: durationTicks - 1, Timing: "ticks"})
	}
	return append(markers, AnimationMarker{Event: "attack_end", Timing: "event"})
}

func expandVisualInterface(ctx context.Context, source interfaceRegister, text *RussianText) (_ []VisualRecord, err error) {
	defer mon.Task()(&ctx)(&err)

	var records []VisualRecord
	groups := []struct {
		prefix string
		kind   string
		items  []string
	}{
		{"UI-HUD-", "hud", source.HUD},
		{"UI-MENU-", "menu", source.Menus},
		{"UI-LEDGER-", "ledger", source.Ledger},
		{"UI-", "recovery", source.Recovery},
		{"UI-MARKER-", "marker", source.Markers},
	}
	textBindings := map[string][]string{
		"UI-HUD-BODY":                 {"TXT-HUD-HEALTH", "TXT-HUD-LOW", "TXT-HUD-NO-LEGS", "TXT-HUD-NO-ARMS"},
		"UI-HUD-PAYLOAD":              {"TXT-HUD-FECES", "TXT-HUD-EMPTY"},
		"UI-HUD-CREDIT":               {"TXT-HUD-CREDIT", "TXT-HUD-DEBT"},
		"UI-HUD-OBJECTIVE":            {"TXT-HUD-OBJECTIVE"},
		"UI-HUD-BOSS":                 {"TXT-HUD-PROTECTED", "TXT-HUD-EXPOSED", "TXT-HUD-PHASE"},
		"UI-HUD-CHANNEL":              {"TXT-HUD-CHANNEL-COST", "TXT-HUD-INTERRUPTED"},
		"UI-HUD-NOTICE":               {"TXT-RECEIPT-HEADING", "TXT-CONSEQUENCE-HEADING", "TXT-ATTEMPT-HEADING"},
		"UI-HUD-CAPTION":              {"CAP-DIRECTION", "CAP-WARN-HAZARD"},
		"UI-HUD-SAVE":                 {"TXT-SAVE-PENDING", "TXT-SAVE-OK", "TXT-SAVE-ERROR"},
		"UI-MENU-TITLE":               {"TXT-TITLE-SUBTITLE"},
		"UI-MENU-ADULT":               {"TXT-NOTICE-ADULT"},
		"UI-MENU-FOCUS":               {"TXT-MENU-FOCUS", "TXT-MENU-CONTROLLER"},
		"UI-MENU-PAUSE":               {"TXT-MENU-RESUME", "TXT-MENU-LEDGER", "TXT-MENU-RETURN", "TXT-MENU-TITLE"},
		"UI-MENU-SETTINGS":            {"TXT-SET-DISPLAY", "TXT-SET-AUDIO", "TXT-SET-INTERFACE", "TXT-SET-ACCESS", "TXT-SET-CONTROLS"},
		"UI-MENU-DISPLAY":             {"TXT-SET-DISPLAY", "TXT-SET-WINDOWED", "TXT-SET-BORDERLESS", "TXT-SET-FULLSCREEN", "TXT-SET-RESOLUTION", "TXT-SET-VSYNC", "TXT-SET-APPLY", "TXT-SET-DISPLAY-CONFIRM", "TXT-SET-DISPLAY-FAIL"},
		"UI-MENU-AUDIO":               {"TXT-SET-AUDIO", "TXT-SET-MASTER", "TXT-SET-MUSIC", "TXT-SET-EFFECTS", "TXT-SET-AMBIENCE", "TXT-SET-VOICE"},
		"UI-MENU-INTERFACE":           {"TXT-SET-INTERFACE", "TXT-SET-TEXT-SIZE", "TXT-SET-DIALOGUE-CAPTIONS", "TXT-SET-SOUND-CAPTIONS", "TXT-SET-CAPTION-BACK"},
		"UI-MENU-ACCESSIBILITY":       {"TXT-SET-ACCESS", "TXT-SET-SHAKE", "TXT-SET-MOTION", "TXT-SET-CONTRAST", "TXT-SET-BLOOD", "TXT-SET-HOLD", "TXT-SET-RANGE", "TXT-SET-MONO"},
		"UI-MENU-CONTROLS":            {"TXT-SET-CONTROLS", "TXT-CTRL-MENU", "TXT-CTRL-RECOVERY", "TXT-SET-DUPLICATE", "TXT-SET-DEFAULTS"},
		"UI-MENU-CONFIRM":             {"TXT-MENU-CONFIRM", "TXT-MENU-CANCEL", "TXT-MENU-QUIT-WARN"},
		"UI-MENU-DISABLED":            {"TXT-HUD-BLOCKED"},
		"UI-MENU-TUTORIAL":            {"TUT-MOV", "TUT-INT", "TUT-LIM", "TUT-REC"},
		"UI-MENU-PROMPT":              {"TXT-HUD-BLOCKED", "TXT-EVD-READY", "TXT-REASON-EVIDENCE"},
		"UI-MENU-END":                 {"TXT-MENU-END", "TXT-MENU-REPLAY", "TXT-REPLAY-INFO", "TXT-ENDING-ACK"},
		"UI-LEDGER-SERVICES":          {"TXT-LED-SERVICES", "TXT-LED-BATCH", "TXT-LED-TRACK"},
		"UI-LEDGER-CONSEQUENCES":      {"TXT-LED-CONSEQUENCES", "TXT-CONSEQUENCE-DETAIL"},
		"UI-LEDGER-TUTORIALS":         {"TXT-LED-TUTORIALS", "TUT-MOV"},
		"UI-LEDGER-AVAILABLE":         {"TXT-LED-AVAILABLE"},
		"UI-LEDGER-ACTIVE":            {"TXT-LED-ACTIVE"},
		"UI-LEDGER-READY":             {"TXT-LED-READY", "TXT-EVD-READY"},
		"UI-LEDGER-CERTIFIED":         {"TXT-LED-CERTIFIED", "TXT-EVD-EXISTING"},
		"UI-LEDGER-FAILED":            {"TXT-LED-FAILED", "TXT-EVD-FAILED", "TXT-LED-RESET"},
		"UI-LEDGER-WARNING":           {"TXT-LED-WARNING"},
		"UI-LEDGER-EMERGENCY":         {"TXT-LED-EMERGENCY", "TXT-EMERGENCY"},
		"UI-LEDGER-AFTERMATH":         {"TXT-LED-AFTERMATH"},
		"UI-MARKER-DANGER":            {"TXT-EMERGENCY"},
		"UI-MARKER-REFUGE":            {"TXT-EMERGENCY-ESCAPE"},
		"UI-MARKER-INTERACTION":       {"TXT-HUD-BLOCKED"},
		"UI-MARKER-RECEPTOR":          {"TXT-FAIL-TARGET"},
		"UI-MARKER-PROTECTED":         {"TXT-BOSS-PROTECTION"},
		"UI-MARKER-EXPOSED":           {"TXT-BOSS-EXPOSED"},
		"UI-MARKER-TRAJECTORY":        {"TUT-UTL"},
		"UI-MARKER-IMPACT-TERRAIN":    {"TUT-UTL"},
		"UI-MARKER-IMPACT-VALID":      {"TUT-UTL"},
		"UI-MARKER-IMPACT-INVALID":    {"TXT-FAIL-TARGET"},
		"UI-MARKER-LOW-HEALTH":        {"TXT-HUD-LOW"},
		"UI-MARKER-HURT-IMMUNITY":     {"CAP-HURT"},
		"UI-MARKER-REACTION-IMMUNITY": {"CAP-FEC-SLIP"},
		"UI-LEDGER-RECEIPT":           {"TXT-LED-RECEIPT", "TXT-RECEIPT-DETAIL"},
		"UI-SAV-PENDING":              {"TXT-SAVE-PENDING"},
		"UI-SAV-OK":                   {"TXT-SAVE-OK"},
		"UI-SAV-ERR":                  {"TXT-SAVE-ERROR", "TXT-SAVE-DETAIL", "TXT-SAVE-RETRY", "TXT-SAVE-RELOAD", "TXT-SAVE-EXIT", "TXT-SAVE-DISCARD"},
		"UI-REC-DEAD":                 {"TXT-DEATH-HEADING", "TXT-DEATH-DESTINATION"},
		"UI-REC-RETURN":               {"TXT-RETURN-WARN", "TXT-RETURN-NOFEE"},
		"UI-REC-LOAD":                 {"TXT-REC-LOAD", "TXT-REC-RESET"},
		"UI-REC-READY":                {"TXT-REC-READY"},
		"UI-REC-OLDER":                {"TXT-REC-OLDER", "TXT-REC-UNKNOWN"},
		"UI-REC-BROKEN":               {"TXT-REC-BROKEN"},
		"UI-REC-SETTINGS":             {"TXT-REC-SETTINGS"},
	}
	directions := map[string]string{
		"UI-HUD-BODY":                 "Upper-left health number, ten-segment bar and labelled four-socket selection diagram; stable broken heart below 25 health.",
		"UI-HUD-PAYLOAD":              "Below body: selected limb or feces icon and count; show missing-arm restrictions.",
		"UI-HUD-CREDIT":               "Upper-right committed credits out of 100 and separate debt line; never subtract debt from credit.",
		"UI-HUD-OBJECTIVE":            "Under credit: tracked ticket and next action; long details go to ledger.",
		"UI-HUD-BOSS":                 "Upper-center named boss bar with 70 and 35 percent marks, phase and protected/exposed label.",
		"UI-HUD-CHANNEL":              "Labeled target-side progress ring; show actual channel, cost and cancellation.",
		"UI-HUD-NOTICE":               "Right-side queue of separate receipt, consequence and failure notices; warnings take priority and pause stops expiry.",
		"UI-HUD-CAPTION":              "Bottom caption band with opaque backing, source and direction; never cover a normal landing.",
		"UI-HUD-SAVE":                 "Document symbol near credits, distinct pending, saved and error labels; no pending receipt stamp.",
		"UI-MENU-TITLE":               "Chair emblem over title, Russian subtitle and empty service counter; Continue shows saved location, anchor and total.",
		"UI-MENU-ADULT":               "18+ notice before title with Continue, Exit, audio and accessibility access; not a rating claim.",
		"UI-MENU-PAUSE":               "Pause list with Resume, ledger, controls, settings, return and title; footer shows saved anchor and total.",
		"UI-MENU-SETTINGS":            "Five accessible tabs with clear labels and persistent focus.",
		"UI-MENU-DISPLAY":             "Display mode, resolution, vsync and apply controls; show confirmation and failure without clipping.",
		"UI-MENU-AUDIO":               "Independent master, music, effects, ambience and voice controls.",
		"UI-MENU-INTERFACE":           "Text-size, caption and caption-background controls; reflow all text sizes.",
		"UI-MENU-ACCESSIBILITY":       "Motion, contrast, blood, hold assistance, mono and dynamic-range options; never change gameplay.",
		"UI-MENU-CONTROLS":            "Keyboard, mouse and controller bindings with current tokens, recovery inputs and duplicate-binding explanation.",
		"UI-MENU-CONFIRM":             "Consistent confirm and cancel positions; destructive choices initially focus Cancel.",
		"UI-MENU-FOCUS":               "Thick rectangular focus outline and label; display focus-loss and controller-loss notices.",
		"UI-MENU-DISABLED":            "Disabled entries remain readable and explain their unavailable reason.",
		"UI-MENU-TUTORIAL":            "Player-acknowledged first-use card with one action and reason, current binding token and archive access.",
		"UI-MENU-PROMPT":              "Target-side interaction brackets and current binding; explain missing prerequisites without consuming resources.",
		"UI-MENU-END":                 "Completion, ending replay and title choices; replay never displays a new receipt or fee.",
		"UI-LEDGER-SERVICES":          "Filterable institution and family rows, one tracked ticket and six-ticket batch counts.",
		"UI-LEDGER-CONSEQUENCES":      "Group consequences by institution and causal ticket; no moral score.",
		"UI-LEDGER-TUTORIALS":         "Navigable tutorial archive using current control bindings.",
		"UI-LEDGER-AVAILABLE":         "Available row symbol and label, with acceptance action.",
		"UI-LEDGER-ACTIVE":            "Active row symbol and label, with tracked next action.",
		"UI-LEDGER-READY":             "Evidence-ready symbol and label; not yet a receipt.",
		"UI-LEDGER-CERTIFIED":         "Certified row symbol and label opens the existing receipt, not an acceptance action.",
		"UI-LEDGER-FAILED":            "Crossed evidence sheet with failure reason and reset action.",
		"UI-LEDGER-RECEIPT":           "Rectangular receipt with service, evidence, institution and committed ordinal.",
		"UI-LEDGER-WARNING":           "Warning symbol and label attached to its causal ticket.",
		"UI-LEDGER-EMERGENCY":         "Emergency symbol and persistent warning with safe-refuge arrow.",
		"UI-LEDGER-AFTERMATH":         "Recorded aftermath symbol and label linked to its causal ticket.",
		"UI-SAV-PENDING":              "Document progress symbol and saving label; freeze gameplay and retain last committed total.",
		"UI-SAV-OK":                   "Closed document symbol and saved label; show the acknowledged event only.",
		"UI-SAV-ERR":                  "Storage error, last committed total and named unsaved event; offer retry, reload and unsaved exit with cancel-first confirmation.",
		"UI-REC-DEAD":                 "Frozen failure background, exact death heading, one committed fee, saved credits, anchor, Retry and Quit to Title.",
		"UI-REC-RETURN":               "Voluntary rollback warning with no death fee; destructive confirmation initially focuses Cancel.",
		"UI-REC-LOAD":                 "Restoration screen names institution, anchor, saved total and reset of uncertified attempt work.",
		"UI-REC-READY":                "Restored pause asks for explicit Resume; no automatic gameplay.",
		"UI-REC-OLDER":                "Older revision choice reports its actual anchor, total and known loss or unknown difference.",
		"UI-REC-BROKEN":               "Preserve damaged files; offer explicit new campaign or exit without inventing receipts.",
		"UI-REC-SETTINGS":             "Settings reset notice states campaign progress is unchanged.",
		"UI-MARKER-DANGER":            "Serrated boundary with inward triangles; show the entire damaging region.",
		"UI-MARKER-REFUGE":            "Doorway pictogram and double-line safe boundary with exit arrow.",
		"UI-MARKER-INTERACTION":       "Square corner brackets identify an eligible interaction.",
		"UI-MARKER-RECEPTOR":          "Ring with selected payload symbol identifies a valid receptor.",
		"UI-MARKER-PROTECTED":         "Closed hatch pattern and protected text, including relay name when relevant.",
		"UI-MARKER-EXPOSED":           "Broken brackets and exposed label distinguish vulnerable recovery.",
		"UI-MARKER-TRAJECTORY":        "Dotted throw path above gore and below interface text; no target tracking.",
		"UI-MARKER-IMPACT-TERRAIN":    "Distinct first-impact shape for terrain.",
		"UI-MARKER-IMPACT-VALID":      "Distinct first-impact shape for a valid receptor.",
		"UI-MARKER-IMPACT-INVALID":    "Distinct first-impact shape for an invalid receptor with explanation.",
		"UI-MARKER-LOW-HEALTH":        "Stable broken-heart symbol below 25 health, without full-screen flashes.",
		"UI-MARKER-HURT-IMMUNITY":     "Continuous hatched outline for the full ordinary-hit immunity interval.",
		"UI-MARKER-REACTION-IMMUNITY": "Small crossed-slip icon, distinct from hurt immunity.",
	}
	for _, group := range groups {
		for _, item := range group.items {
			id := group.prefix + item
			bindings := slices.Clone(textBindings[id])
			direction, ok := directions[id]
			if !ok || !allText(direction) || len(bindings) == 0 {
				return nil, fmt.Errorf("%s: missing interface requirement or text", id)
			}
			for _, textID := range bindings {
				if _, ok := text.Entry(textID); !ok {
					return nil, fmt.Errorf("%s: missing text %s: %w", id, textID, ErrMissingReference)
				}
			}
			if group.kind == "recovery" {
				bindings = append(bindings, id)
			}
			direction += " Safe margin 24 logical pixels; normal text 24, supporting text at least 20; reflow at all supported sizes."
			records = append(records, visualBase(id, group.kind, direction, bindings...))
		}
	}
	for _, effect := range source.Effects {
		direction := map[string]string{
			"BODY":   "Explicit blood, torn fabric, exposed bone and directional limb-removal sprays, not clean toy parts; reduced particles retain wounds and missing limbs.",
			"FEC":    "Opaque brown, weighted feces impacts and visible smears; distinguish player feces and harmless smears from damaging ministry waste.",
			"IMPACT": "Directional hit spray, wet impact and bounded comic collapse around the unchanged collision box; no gameplay hit-stop or full-screen red filter.",
			"HAZARD": "Readable marked steam, fire, arcs, harmful waste and warning areas; preserve essential boundaries and safe routes in reduced mode.",
		}[effect]
		record := visualBase("FX-"+effect, "effect", direction)
		record.States = []string{"FULL", "REDUCED"}
		record.Effect = &EffectRequirement{HitSprayMaxMS: 400, RemovalSprayMaxMS: 600, DecorativeStainLimit: 48, RoomParticleLimit: 512, FadeOldestFirst: true, ExcludeEssentialMarkers: true, ReducedPreservesHarm: true}
		records = append(records, record)
	}
	for _, id := range source.Signs {
		entry, ok := text.Entry(id)
		if !ok || entry.Reference == "" {
			return nil, fmt.Errorf("missing separate sign text %s", id)
		}
		records = append(records, visualBase("ART-"+id, "sign-layer", "Blank art panel with independently rendered Russian text; no essential instruction baked into art.", id, entry.Reference))
	}
	return records, nil
}

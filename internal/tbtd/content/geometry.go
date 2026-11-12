// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"fmt"
	"io/fs"
	"math"
)

// BayPoint is a local-coordinate service object origin. Y increases upward.
type BayPoint struct {
	Role ObjectRole `json:"role"`
	X    float64    `json:"x"`
	Y    float64    `json:"y"`
}

// BayFeature describes a named local-coordinate action feature, not a collider.
type BayFeature struct {
	ID     string  `json:"id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// BayJump describes a running jump from the bay floor to a named feature.
// The view is the authored pre-takeoff camera marker in world coordinates.
type BayJump struct {
	ID        string  `json:"id"`
	TakeoffX  float64 `json:"takeoff_x"`
	Speed     float64 `json:"speed"`
	Landing   string  `json:"landing"`
	Camera    Rect    `json:"camera"`
	Occluders []Rect  `json:"occluders"`
}

// BayTemplate describes the shared floor, action origins, and features of a
// family. It is not a complete terrain, safety, or route definition.
type BayTemplate struct {
	ID       string       `json:"id"`
	Width    float64      `json:"width"`
	Height   float64      `json:"height"`
	Floor    Rect         `json:"floor"`
	Objects  []BayPoint   `json:"objects"`
	Features []BayFeature `json:"features"`
	Jumps    []BayJump    `json:"jumps"`
}

type bayTemplateDocument struct {
	SchemaVersion int           `json:"schema_version"`
	Vestibule     []BayPoint    `json:"vestibule"`
	Templates     []BayTemplate `json:"templates"`
}

// BayEnemy describes an enemy placement in a ticket variant.
type BayEnemy struct {
	Kind string  `json:"kind"`
	X    float64 `json:"x"`
}

// BayVariant describes the authored placements selected by a ticket number.
type BayVariant struct {
	Number        int        `json:"number"`
	Enemies       []BayEnemy `json:"enemies"`
	Stations      []float64  `json:"stations"`
	Mode          string     `json:"mode"`
	Tutorial      bool       `json:"tutorial,omitempty"`
	CoverX        float64    `json:"cover_x,omitempty"`
	WaitingAlcove bool       `json:"waiting_alcove,omitempty"`
}

type bayVariantDocument struct {
	SchemaVersion int          `json:"schema_version"`
	Variants      []BayVariant `json:"variants"`
}

// PlacedServiceObject binds a campaign object identity to its local origin.
type PlacedServiceObject struct {
	Object Object
	X      float64
	Y      float64
}

// ServiceBay binds one existing campaign ticket to the shared authored data.
// Only the selected ticket's object origins are included. This record alone is
// insufficient to construct a playable world or certify route safety.
type ServiceBay struct {
	Deed      DeedID
	Encounter EncounterID
	Anchor    AnchorID
	Emergency EmergencyID
	Template  BayTemplate
	Variant   BayVariant
	Vestibule []BayPoint
	Objects   []PlacedServiceObject
}

// LoadServiceBays loads shared bay metadata and expands the existing 96 service
// tickets. It does not validate terrain, boss arenas, or emergency routes.
func LoadServiceBays(ctx context.Context, filesystem fs.FS, templatePath, variantPath string, campaign *Campaign) (_ []ServiceBay, err error) {
	defer mon.Task()(&ctx)(&err)

	if campaign == nil {
		return nil, ErrInvalid.New("service bays: missing campaign")
	}
	var templates bayTemplateDocument
	if err = readCampaignDocument(ctx, filesystem, templatePath, &templates); err != nil {
		return nil, err
	}
	var variants bayVariantDocument
	if err = readCampaignDocument(ctx, filesystem, variantPath, &variants); err != nil {
		return nil, err
	}
	return expandServiceBays(ctx, templates, variants, campaign)
}

func expandServiceBays(ctx context.Context, templates bayTemplateDocument, variants bayVariantDocument, campaign *Campaign) (_ []ServiceBay, err error) {
	defer mon.Task()(&ctx)(&err)

	if templates.SchemaVersion != 1 || variants.SchemaVersion != 1 || len(templates.Templates) != 4 || len(variants.Variants) != 6 {
		return nil, ErrInvalid.New("service bays: invalid schema or inventory")
	}
	if len(templates.Vestibule) != 3 {
		return nil, ErrInvalid.New("service bays: incomplete vestibule")
	}
	stations := map[ObjectRole]float64{RoleCheckpoint: 96, RoleConsole: 176, RoleRefill: 208}
	seenStations := make(map[ObjectRole]bool, len(stations))
	for _, point := range templates.Vestibule {
		x, ok := stations[point.Role]
		if !ok || seenStations[point.Role] || point.X != x || point.Y != 0 {
			return nil, ErrInvalid.New("service bays: invalid vestibule station %s", point.Role)
		}
		seenStations[point.Role] = true
	}
	byFamily := make(map[string]BayTemplate, 4)
	for _, template := range templates.Templates {
		if err = validateBayTemplate(template); err != nil {
			return nil, ErrInvalid.Wrap(err)
		}
		family := template.ID[4:]
		if _, exists := byFamily[family]; exists {
			return nil, ErrInvalid.New("service bays: duplicate template %s", template.ID)
		}
		byFamily[family] = template
	}
	byNumber := make(map[int]BayVariant, 6)
	for _, variant := range variants.Variants {
		if err = validateBayVariant(variant); err != nil {
			return nil, ErrInvalid.Wrap(err)
		}
		if _, exists := byNumber[variant.Number]; exists {
			return nil, ErrInvalid.New("service bays: duplicate variant %d", variant.Number)
		}
		byNumber[variant.Number] = variant
	}
	objects := make(map[ObjectID]Object)
	for _, object := range campaign.Objects(ctx) {
		if _, exists := objects[object.ID]; exists {
			return nil, ErrInvalid.New("service bays: duplicate campaign object %s", object.ID)
		}
		objects[object.ID] = object
	}
	var result []ServiceBay
	seen := make(map[DeedID]bool)
	for _, deed := range campaign.Deeds(ctx) {
		if deed.Family == "END" {
			continue
		}
		template, ok := byFamily[deed.Family]
		if !ok {
			return nil, ErrInvalid.New("service bays: missing family %s", deed.Family)
		}
		variant, ok := byNumber[deed.Variant]
		if !ok || seen[deed.Deed.ID] {
			return nil, ErrInvalid.New("service bays: invalid ticket %s", deed.Deed.ID)
		}
		seen[deed.Deed.ID] = true
		bay := ServiceBay{Deed: deed.Deed.ID, Encounter: deed.Deed.Encounter, Anchor: deed.Deed.Anchor, Emergency: deed.Emergency, Template: cloneBayTemplate(template), Variant: cloneBayVariant(variant), Vestibule: append([]BayPoint(nil), templates.Vestibule...)}
		for _, id := range deed.Deed.Objects {
			object, exists := objects[id]
			if !exists || object.Encounter != bay.Encounter || (object.Deed != "" && object.Deed != bay.Deed) {
				return nil, ErrInvalid.New("service bays: invalid object reference %s", id)
			}
			var origin *BayPoint
			for i := range template.Objects {
				if template.Objects[i].Role == object.Role {
					origin = &template.Objects[i]
					break
				}
			}
			if origin == nil {
				return nil, ErrInvalid.New("service bays: no origin for %s", id)
			}
			bay.Objects = append(bay.Objects, PlacedServiceObject{Object: object, X: origin.X, Y: origin.Y})
		}
		result = append(result, bay)
	}
	if len(result) != 96 {
		return nil, ErrInvalid.New("service bays: expected 96 service tickets, got %d", len(result))
	}
	return result, nil
}

func validateBayTemplate(template BayTemplate) error {
	roles := map[string][]ObjectRole{
		"BAY-SUP": {RoleTrolley, RolePad, RoleTerminal, RoleCrusher},
		"BAY-RES": {RoleActor, RolePlate, RoleCrate, RoleGate, RoleLatch, RoleRefuge, RoleTerminal},
		"BAY-UTL": {RolePaddle, RoleSwitch, RoleFuse, RoleTerminal},
		"BAY-PRV": {RoleRecorder, RoleCamera, RoleTerminal},
	}
	required, ok := roles[template.ID]
	if !ok || template.Width != 1536 || template.Height != 560 || len(template.Objects) != len(required) {
		return fmt.Errorf("%s: invalid template or bounds", template.ID)
	}
	if !validRect(template.Floor) || template.Floor.X != 0 || template.Floor.Width != template.Width || template.Floor.Y+template.Floor.Height != 0 {
		return fmt.Errorf("%s: floor must continuously support the bay at height zero", template.ID)
	}
	seen := map[ObjectRole]bool{}
	for _, point := range template.Objects {
		if seen[point.Role] || !finite(point.X) || !finite(point.Y) || point.X < 0 || point.X >= template.Width || point.Y < 0 || point.Y >= template.Height {
			return fmt.Errorf("%s: invalid object %s", template.ID, point.Role)
		}
		seen[point.Role] = true
	}
	for _, role := range required {
		if !seen[role] {
			return fmt.Errorf("%s: missing object %s", template.ID, role)
		}
	}
	// The PDD fixes these action positions; unspecified positions remain authored.
	fixedX := map[string]map[ObjectRole]float64{
		"BAY-SUP": {RoleTrolley: 320, RolePad: 1320, RoleCrusher: 720},
		"BAY-RES": {RolePlate: 432, RoleCrate: 352, RoleGate: 672, RoleLatch: 736, RoleRefuge: 1312},
		"BAY-PRV": {RoleRecorder: 384, RoleTerminal: 1344},
	}
	points := make(map[ObjectRole]BayPoint, len(template.Objects))
	for _, point := range template.Objects {
		if x, fixed := fixedX[template.ID][point.Role]; fixed && point.X != x {
			return fmt.Errorf("%s: wrong position for %s", template.ID, point.Role)
		}
		// Receptors are elevated targets. All remaining objects in these
		// templates start on the continuous local floor, not in mid-air.
		if point.Role != RolePaddle && point.Role != RoleCamera &&
			(point.Y != template.Floor.Y+template.Floor.Height || point.X < template.Floor.X || point.X >= template.Floor.X+template.Floor.Width) {
			return fmt.Errorf("%s: %s must start on the floor", template.ID, point.Role)
		}
		switch point.Role {
		case RoleTerminal, RolePad, RoleRefuge:
			if point.X < 1280 || point.X >= 1536 {
				return fmt.Errorf("%s: %s must be in the receiving area", template.ID, point.Role)
			}
		default:
			if point.X < 240 || point.X >= 1280 {
				return fmt.Errorf("%s: %s must be in the working area", template.ID, point.Role)
			}
		}
		points[point.Role] = point
	}
	if template.ID == "BAY-RES" && points[RoleActor].X <= points[RoleGate].X {
		return fmt.Errorf("%s: adult must start beyond the rescue gate", template.ID)
	}
	features := map[string]BayFeature{}
	for _, feature := range template.Features {
		rect := Rect{X: feature.X, Y: feature.Y, Width: feature.Width, Height: feature.Height}
		if _, duplicate := features[feature.ID]; feature.ID == "" || duplicate || !validRect(rect) || !inside(rect, Rect{Width: template.Width, Height: template.Height}) {
			return fmt.Errorf("%s: invalid feature %s", template.ID, feature.ID)
		}
		features[feature.ID] = feature
	}
	requiredFeatures := map[string][]string{
		"BAY-SUP": {"CRUSHER", "CATWALK"},
		"BAY-UTL": {"STEP-1", "STEP-2", "OPENING", "PADDLE"},
		"BAY-PRV": {"LEDGE", "CAMERA", "REFILL"},
	}
	if len(features) != len(requiredFeatures[template.ID]) {
		return fmt.Errorf("%s: wrong feature inventory", template.ID)
	}
	for _, id := range requiredFeatures[template.ID] {
		if _, exists := features[id]; !exists {
			return fmt.Errorf("%s: missing feature %s", template.ID, id)
		}
	}
	jumpTargets := map[string]string{"BAY-UTL": "STEP-1", "BAY-PRV": "LEDGE"}
	target, needsJump := jumpTargets[template.ID]
	if (needsJump && len(template.Jumps) != 1) || (!needsJump && len(template.Jumps) != 0) {
		return fmt.Errorf("%s: incorrect mandatory jump inventory", template.ID)
	}
	for _, jump := range template.Jumps {
		if jump.ID != "JUMP-"+template.ID[4:]+"-LEDGE" || jump.Landing != target {
			return fmt.Errorf("%s: incorrect mandatory jump reference", template.ID)
		}
		if err := validateFloorJump(template.Floor, jump, features[jump.Landing]); err != nil {
			return fmt.Errorf("%s: %s: %w", template.ID, jump.ID, err)
		}
	}
	switch template.ID {
	case "BAY-SUP":
		crusher, catwalk := features["CRUSHER"], features["CATWALK"]
		if crusher.X != 720 || crusher.Width != 96 || catwalk.X != 560 || catwalk.Width != 416 || catwalk.Y != 80 {
			return fmt.Errorf("%s: incorrect crusher or catwalk", template.ID)
		}
	case "BAY-UTL":
		first, second := features["STEP-1"], features["STEP-2"]
		opening, paddle := features["OPENING"], features["PADDLE"]
		if first.X != 480 || first.Y != 80 || second.X != 608 || second.Y != 80 || opening.Height != 96 || opening.X != 640 || paddle.Width != 64 || paddle.Height != 64 || paddle.X+paddle.Width/2-opening.X != 180 || points[RolePaddle].X != paddle.X+paddle.Width/2 || points[RolePaddle].Y != paddle.Y+paddle.Height/2 {
			return fmt.Errorf("%s: incorrect throwing geometry", template.ID)
		}
	case "BAY-PRV":
		ledge, camera, refill := features["LEDGE"], features["CAMERA"], features["REFILL"]
		if ledge.X+ledge.Width != 640 || ledge.Y != 80 || camera.Width != 64 || camera.Height != 64 || camera.X+camera.Width/2-(ledge.X+ledge.Width) != 160 || camera.Y+camera.Height/2-ledge.Y != 40 || points[RoleCamera].X != camera.X+camera.Width/2 || points[RoleCamera].Y != camera.Y+camera.Height/2 || refill.X != 608 || refill.Y != 0 {
			return fmt.Errorf("%s: incorrect camera geometry", template.ID)
		}
	}
	return nil
}

// validateFloorJump checks a particular held-jump trajectory, not merely the
// separate rise and distance limits. It assumes an unobstructed run-up on the
// authored floor and constant horizontal launch speed. The full room still
// needs collision and camera validation against all of its geometry.
func validateFloorJump(floor Rect, jump BayJump, landing BayFeature) error {
	if !validRect(floor) || !finite(jump.TakeoffX) || !finite(jump.Speed) || jump.Speed <= 0 || jump.Speed > 320 {
		return fmt.Errorf("invalid takeoff")
	}
	runup := jump.Speed*jump.Speed/(2*2400) + jump.Speed/120
	if jump.TakeoffX-runup-14 < floor.X || jump.TakeoffX+14 > floor.X+floor.Width {
		return fmt.Errorf("takeoff has insufficient run-up or support")
	}
	rise := landing.Y - (floor.Y + floor.Height)
	distance := landing.X - jump.TakeoffX
	if !finite(rise) || !finite(distance) || rise < 0 || rise > 100 || distance < 0 || distance > 180 || landing.Width < 28 || landing.Height <= 0 {
		return fmt.Errorf("jump exceeds mandatory rise, distance, or landing limits")
	}
	if rise > 0 && distance < 14 {
		return fmt.Errorf("takeoff overlaps the landing side")
	}
	landingRect := Rect{X: landing.X, Y: landing.Y, Width: landing.Width, Height: landing.Height}
	takeoffRect := Rect{X: jump.TakeoffX - 14, Y: floor.Y + floor.Height, Width: 28, Height: 58}
	if !validRect(landingRect) || !validRect(jump.Camera) || jump.Camera.Width != 1280 || jump.Camera.Height != 720 || !inside(takeoffRect, jump.Camera) || !inside(landingRect, jump.Camera) {
		return fmt.Errorf("takeoff or landing outside pre-jump view")
	}
	for _, occluder := range jump.Occluders {
		if !validRect(occluder) || overlaps(occluder, landingRect) {
			return fmt.Errorf("invalid or obscured landing")
		}
	}

	// Semi-implicit 120 Hz integration with two legs. Interpolate within a tick
	// for swept contact with the landing, as the world collision contract does.
	x, y, vy := jump.TakeoffX, floor.Y+floor.Height, 760.0
	for tick := 0; tick < 240; tick++ {
		previousX, previousY := x, y
		vy -= 2200.0 / 120
		x += jump.Speed / 120
		y += vy / 120
		if previousX+14 <= landing.X && x+14 > landing.X {
			fraction := (landing.X - 14 - previousX) / (x - previousX)
			if previousY+fraction*(y-previousY) < landing.Y {
				return fmt.Errorf("jump strikes landing side")
			}
		}
		if vy < 0 && previousY >= landing.Y && y <= landing.Y {
			fraction := (previousY - landing.Y) / (previousY - y)
			landingX := previousX + fraction*(x-previousX)
			if landingX < landing.X+14 || landingX > landing.X+landing.Width-14 {
				return fmt.Errorf("combined rise and distance are unreachable at landing")
			}
			return nil
		}
	}
	return fmt.Errorf("jump cannot reach landing height")
}

func validateBayVariant(variant BayVariant) error {
	if variant.Number < 1 || variant.Number > 6 || len(variant.Enemies) == 0 || len(variant.Enemies) > 2 || len(variant.Stations) == 0 || len(variant.Stations) > 2 {
		return fmt.Errorf("variant %d: invalid inventory", variant.Number)
	}
	if variant.Mode != "single" && variant.Mode != "alternating" && variant.Mode != "simultaneous" {
		return fmt.Errorf("variant %d: invalid mode", variant.Number)
	}
	if (variant.Mode == "alternating" || variant.Mode == "simultaneous") && len(variant.Stations) != 2 {
		return fmt.Errorf("variant %d: expected two stations", variant.Number)
	}
	for _, enemy := range variant.Enemies {
		if (enemy.Kind != "MELEE" && enemy.Kind != "RANGED") || !finite(enemy.X) || enemy.X < 240 || enemy.X >= 1280 {
			return fmt.Errorf("variant %d: invalid enemy", variant.Number)
		}
	}
	for i, x := range variant.Stations {
		if !finite(x) || x < 400 || x >= 1280 || (i > 0 && x <= variant.Stations[i-1]) {
			return fmt.Errorf("variant %d: invalid station", variant.Number)
		}
	}
	if math.IsNaN(variant.CoverX) || math.IsInf(variant.CoverX, 0) || variant.CoverX < 0 || variant.CoverX >= 1280 {
		return fmt.Errorf("variant %d: invalid cover", variant.Number)
	}
	expectedEnemies := [][]BayEnemy{
		{{Kind: "MELEE", X: 960}},
		{{Kind: "RANGED", X: 1040}},
		{{Kind: "MELEE", X: 880}, {Kind: "MELEE", X: 1120}},
		{{Kind: "MELEE", X: 880}, {Kind: "RANGED", X: 1120}},
		{{Kind: "MELEE", X: 880}, {Kind: "MELEE", X: 1120}},
		{{Kind: "MELEE", X: 880}, {Kind: "RANGED", X: 1120}},
	}
	expected := expectedEnemies[variant.Number-1]
	if len(variant.Enemies) != len(expected) {
		return fmt.Errorf("variant %d: incorrect enemy inventory", variant.Number)
	}
	for i := range expected {
		if variant.Enemies[i] != expected[i] {
			return fmt.Errorf("variant %d: incorrect enemy placement", variant.Number)
		}
	}
	if (variant.Number <= 3 && (len(variant.Stations) != 1 || variant.Stations[0] != 760)) ||
		(variant.Number == 4 && (len(variant.Stations) != 2 || variant.Stations[0] != 680 || variant.Stations[1] != 1040 || !variant.WaitingAlcove)) ||
		(variant.Number >= 4 && len(variant.Stations) != 2) ||
		(variant.Number <= 4 && variant.Mode != "single") ||
		(variant.Number == 5 && variant.Mode != "alternating") ||
		(variant.Number == 6 && variant.Mode != "simultaneous") ||
		(variant.Number == 1 && !variant.Tutorial) ||
		(variant.Number == 2 && variant.CoverX != 880) {
		return fmt.Errorf("variant %d: incorrect hazard plan", variant.Number)
	}
	return nil
}

func cloneBayTemplate(template BayTemplate) BayTemplate {
	template.Objects = append([]BayPoint(nil), template.Objects...)
	template.Features = append([]BayFeature(nil), template.Features...)
	template.Jumps = append([]BayJump(nil), template.Jumps...)
	for i := range template.Jumps {
		template.Jumps[i].Occluders = append([]Rect(nil), template.Jumps[i].Occluders...)
	}
	return template
}

func cloneBayVariant(variant BayVariant) BayVariant {
	variant.Enemies = append([]BayEnemy(nil), variant.Enemies...)
	variant.Stations = append([]float64(nil), variant.Stations...)
	return variant
}

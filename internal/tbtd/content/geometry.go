// Copyright (C) 2026 crawter
// All rights reserved.

package content

import (
	"context"
	"fmt"
	"io/fs"
	"math"
	"path"
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
	Direction int     `json:"direction,omitempty"`
	Support   string  `json:"support,omitempty"`
	Landing   string  `json:"landing"`
	Camera    Rect    `json:"camera"`
	Occluders []Rect  `json:"occluders"`
	Blockers  []Rect  `json:"blockers,omitempty"`
}

// BayCollider is physical terrain. One-way platforms support players from
// above; player-only barriers block bodies but allow projectiles through.
// Projectile targets accept impacts without blocking player movement. Gate and
// crusher colliders are controlled by their respective actions.
type BayCollider struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Bounds Rect   `json:"bounds"`
}

// BayPassage identifies a required clear, floor-supported passage.
type BayPassage struct {
	ID     string `json:"id"`
	Bounds Rect   `json:"bounds"`
}

// BayTemplate describes shared floor, action objects and physical terrain.
type BayTemplate struct {
	ID       string        `json:"id"`
	Width    float64       `json:"width"`
	Height   float64       `json:"height"`
	Floor    Rect          `json:"floor"`
	Objects  []BayPoint    `json:"objects"`
	Features []BayFeature  `json:"features"`
	Terrain  []BayCollider `json:"terrain"`
	Passages []BayPassage  `json:"passages"`
	Jumps    []BayJump     `json:"jumps"`
}

// BayRouteJump links a supported takeoff to a bypass platform.
type BayRouteJump struct {
	Support int     `json:"support"`
	Target  int     `json:"target"`
	Jump    BayJump `json:"jump"`
}

// BayRouteState contains obstacles present in one durable emergency stage.
type BayRouteState struct {
	Name   string `json:"name"`
	Solids []Rect `json:"solids"`
	Lethal []Rect `json:"lethal"`
	Damage []Rect `json:"damage,omitempty"`
}

// BaySafety is the shared, one-way overhead bypass and its route states.
// Protection volumes prevent attacks from crossing into or out of safe areas;
// they do not block movement between the vestibule and working floor.
type BaySafety struct {
	Platforms        []Rect          `json:"platforms"`
	Jumps            []BayRouteJump  `json:"jumps"`
	States           []BayRouteState `json:"states"`
	Refuge           GeometryPoint   `json:"refuge"`
	VestibuleProtect Rect            `json:"vestibule_protection"`
	RefugeProtect    Rect            `json:"refuge_protection"`
}

type bayTemplateDocument struct {
	SchemaVersion int           `json:"schema_version"`
	Vestibule     []BayPoint    `json:"vestibule"`
	Safety        BaySafety     `json:"safety"`
	Templates     []BayTemplate `json:"templates"`
}

// BayEnemy describes an enemy placement in a ticket variant.
type BayEnemy struct {
	Kind string  `json:"kind"`
	X    float64 `json:"x"`
}

// BayVariant describes the authored placements selected by a ticket number.
// Cover blocks players and projectiles, not trolleys or rescue adults.
type BayVariant struct {
	Number        int        `json:"number"`
	Enemies       []BayEnemy `json:"enemies"`
	Stations      []float64  `json:"stations"`
	Mode          string     `json:"mode"`
	Tutorial      bool       `json:"tutorial,omitempty"`
	CoverX        float64    `json:"cover_x,omitempty"`
	WaitingAlcove bool       `json:"waiting_alcove,omitempty"`
	CoverKind     string     `json:"cover_kind,omitempty"`
	Cover         Rect       `json:"cover,omitempty"`
	Alcove        Rect       `json:"alcove,omitempty"`
}

type bayVariantDocument struct {
	SchemaVersion int          `json:"schema_version"`
	Variants      []BayVariant `json:"variants"`
}

// BayHazard describes a marked station and the safe floor immediately before it.
type BayHazard struct {
	Bounds     Rect
	Waiting    Rect
	WarningMS  int
	ActiveMS   int
	InactiveMS int
	OffsetMS   int
	Lethal     bool
}

// PlacedServiceObject binds a campaign object identity to its local origin.
type PlacedServiceObject struct {
	Object Object
	X      float64
	Y      float64
}

// GeometryPoint is a bottom-centre position in world pixels.
type GeometryPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// EmergencySpace specifies the marked safe area and hazard envelope. Wave
// start and end are swept at Speed pixels/second and stop at Barrier.
type EmergencySpace struct {
	Kind       string  `json:"kind"`
	RefugeName string  `json:"refuge_name"`
	Refuge     Rect    `json:"refuge"`
	Envelope   Rect    `json:"envelope"`
	Barrier    Rect    `json:"barrier"`
	Start      Rect    `json:"start,omitempty"`
	End        Rect    `json:"end,omitempty"`
	Speed      float64 `json:"speed,omitempty"`
}

// InstitutionBay binds a shared bay to its campaign encounter and emergency.
type InstitutionBay struct {
	Encounter         EncounterID    `json:"encounter"`
	Anchor            AnchorID       `json:"anchor"`
	Emergency         EmergencyID    `json:"emergency"`
	Family            string         `json:"family"`
	Hazard            string         `json:"hazard"`
	Danger            Rect           `json:"danger"`
	Aftermath         Rect           `json:"aftermath"`
	AftermathBlocking bool           `json:"aftermath_blocking,omitempty"`
	Space             EmergencySpace `json:"space"`
}

// BossJump identifies its support (-1 is the floor) and landing platform.
type BossJump struct {
	Support int     `json:"support"`
	Target  int     `json:"target"`
	Jump    BayJump `json:"jump"`
}

// BossGeometry describes the approach, arena, and post-fight closure area.
// Platforms are one-way; solids and lethal volumes are separate. Protection
// blocks attacks in both directions, including firing from refill alcoves.
type BossGeometry struct {
	Encounter        EncounterID     `json:"encounter"`
	Anchor           AnchorID        `json:"anchor"`
	OutAnchor        AnchorID        `json:"out_anchor"`
	Closure          DeedID          `json:"closure"`
	Terminal         ObjectID        `json:"terminal"`
	Bounds           Rect            `json:"bounds"`
	Floor            Rect            `json:"floor"`
	Checkpoint       GeometryPoint   `json:"checkpoint"`
	Refills          []GeometryPoint `json:"refills"`
	Exit             GeometryPoint   `json:"exit"`
	TerminalPosition GeometryPoint   `json:"terminal_position"`
	Platforms        []Rect          `json:"platforms"`
	Protection       []Rect          `json:"protection"`
	Features         []BayFeature    `json:"features"`
	Jumps            []BossJump      `json:"jumps"`
	Solids           []Rect          `json:"solids"`
	Lethal           []Rect          `json:"lethal"`
}

// PrologueGeometry describes a non-credit prologue encounter and safe anchor.
type PrologueGeometry struct {
	Encounter EncounterID   `json:"encounter"`
	Anchor    AnchorID      `json:"anchor"`
	Action    string        `json:"action"`
	Bounds    Rect          `json:"bounds"`
	Floor     Rect          `json:"floor"`
	Entry     GeometryPoint `json:"entry"`
	Spawn     GeometryPoint `json:"spawn"`
	Objective GeometryPoint `json:"objective"`
	Platforms []Rect        `json:"platforms"`
	Jumps     []BossJump    `json:"jumps"`
	Features  []BayFeature  `json:"features"`
	Solids    []Rect        `json:"solids"`
	Lethal    []Rect        `json:"lethal"`
}

// ReturnLobby is the protected ministry permit counter and its recovery anchor.
type ReturnLobby struct {
	Anchor     AnchorID      `json:"anchor"`
	Bounds     Rect          `json:"bounds"`
	Floor      Rect          `json:"floor"`
	Protection Rect          `json:"protection"`
	Entry      GeometryPoint `json:"entry"`
	Spawn      GeometryPoint `json:"spawn"`
	Counter    GeometryPoint `json:"counter"`
	Solids     []Rect        `json:"solids"`
	Lethal     []Rect        `json:"lethal"`
}

// EncounterLink connects adjacent local-coordinate rooms through their bypass.
type EncounterLink struct {
	From  EncounterID   `json:"from"`
	To    EncounterID   `json:"to"`
	Exit  GeometryPoint `json:"exit"`
	Entry GeometryPoint `json:"entry"`
}

// InstitutionGeometry is the authored location and its five encounters.
type InstitutionGeometry struct {
	SchemaVersion int                `json:"schema_version"`
	Location      LocationID         `json:"location"`
	Bays          []InstitutionBay   `json:"bays"`
	Prologue      []PrologueGeometry `json:"prologue,omitempty"`
	Links         []EncounterLink    `json:"links"`
	ExitGate      GateID             `json:"exit_gate"`
	ExitLocation  LocationID         `json:"exit_location"`
	ReturnLobbies []ReturnLobby      `json:"return_lobbies,omitempty"`
	Boss          BossGeometry       `json:"boss"`
}

// ServiceBay binds one existing campaign ticket to shared and location data.
type ServiceBay struct {
	Deed        DeedID
	Encounter   EncounterID
	Anchor      AnchorID
	Emergency   EmergencyID
	Template    BayTemplate
	Variant     BayVariant
	Vestibule   []BayPoint
	Safety      BaySafety
	Hazards     []BayHazard
	Objects     []PlacedServiceObject
	Institution InstitutionGeometry
}

// LoadServiceBays loads and validates the authored institution documents beside
// templatePath and expands the existing 96 service tickets.
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
	bays, err := expandServiceBays(ctx, templates, variants, campaign)
	if err != nil {
		return nil, err
	}
	layouts := make(map[LocationID]InstitutionGeometry, 4)
	for _, name := range []string{"hospital.json", "church.json", "ministry.json", "institute.json"} {
		var layout InstitutionGeometry
		if err = readCampaignDocument(ctx, filesystem, path.Join(path.Dir(templatePath), name), &layout); err != nil {
			return nil, err
		}
		if err = validateInstitution(ctx, layout, campaign); err != nil {
			return nil, ErrInvalid.Wrap(fmt.Errorf("%s: %w", name, err))
		}
		if _, duplicate := layouts[layout.Location]; duplicate {
			return nil, ErrInvalid.New("duplicate institution %s", layout.Location)
		}
		layouts[layout.Location] = layout
	}
	locations := make(map[DeedID]LocationID, len(bays))
	for _, deed := range campaign.Deeds(ctx) {
		locations[deed.Deed.ID] = deed.Location
	}
	for i := range bays {
		layout, ok := layouts[locations[bays[i].Deed]]
		if !ok {
			return nil, ErrInvalid.New("missing institution for %s", bays[i].Deed)
		}
		found := false
		for _, room := range layout.Bays {
			if room.Encounter != bays[i].Encounter {
				continue
			}
			found = true
			safety := cloneBaySafety(bays[i].Safety)
			safety.States[1].Damage = append(safety.States[1].Damage, room.Space.Envelope)
			safety.States[1].Solids = append(safety.States[1].Solids, room.Space.Barrier)
			safety.States[2].Solids = append(safety.States[2].Solids, room.Space.Barrier)
			if room.AftermathBlocking {
				safety.States[2].Solids = append(safety.States[2].Solids, room.Aftermath)
			}
			for _, state := range safety.States[1:] {
				obstacles := append(append(append([]Rect(nil), state.Solids...), state.Lethal...), state.Damage...)
				center := room.Space.Refuge.X + room.Space.Refuge.Width/2
				if !clearFloorPath(1501, center, obstacles) {
					return nil, ErrInvalid.New("%s: %s refuge is disconnected", room.Encounter, state.Name)
				}
				for _, obstacle := range obstacles {
					if overlaps(room.Space.Refuge, obstacle) {
						return nil, ErrInvalid.New("%s: %s refuge is obstructed", room.Encounter, state.Name)
					}
				}
			}
			if err = validateBaySafety(bays[i].Template, bays[i].Vestibule, safety); err != nil {
				return nil, ErrInvalid.Wrap(fmt.Errorf("%s: %w", room.Encounter, err))
			}
			if err = validateVariantGeometry(bays[i].Template, bays[i].Vestibule, bays[i].Variant, bays[i].Hazards, safety); err != nil {
				return nil, ErrInvalid.Wrap(fmt.Errorf("%s: %w", bays[i].Deed, err))
			}
			bays[i].Safety = safety
		}
		if !found {
			return nil, ErrInvalid.New("missing room for %s", bays[i].Encounter)
		}
		bays[i].Institution = cloneInstitution(layout)
	}
	return bays, nil
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
		if err = validateBaySafety(template, templates.Vestibule, templates.Safety); err != nil {
			return nil, ErrInvalid.Wrap(fmt.Errorf("%s: %w", template.ID, err))
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
		bay := ServiceBay{Deed: deed.Deed.ID, Encounter: deed.Deed.Encounter, Anchor: deed.Deed.Anchor, Emergency: deed.Emergency, Template: cloneBayTemplate(template), Variant: cloneBayVariant(variant), Vestibule: append([]BayPoint(nil), templates.Vestibule...), Safety: cloneBaySafety(templates.Safety)}
		bay.Hazards = expandBayHazards(template.ID, variant)
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
		"BAY-SUP": {"CRUSHER", "CATWALK", "CRUSHER-CONTROL-L", "CRUSHER-CONTROL-R", "SIDE-PASSAGE"},
		"BAY-UTL": {"STEP-1", "STEP-2", "OPENING", "PADDLE", "RETURN-LOW", "RETURN-HIGH"},
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
	if err := validateBayTerrain(template, features, points); err != nil {
		return fmt.Errorf("%s: %w", template.ID, err)
	}
	jumpTargets := map[string][]string{"BAY-SUP": {"CATWALK"}, "BAY-UTL": {"STEP-1", "RETURN-LOW", "RETURN-HIGH"}, "BAY-PRV": {"LEDGE"}}
	jumpIDs := map[string][]string{"BAY-SUP": {"JUMP-SUP-LEDGE"}, "BAY-UTL": {"JUMP-UTL-LEDGE", "JUMP-UTL-RETURN-LOW", "JUMP-UTL-RETURN-HIGH"}, "BAY-PRV": {"JUMP-PRV-LEDGE"}}
	if len(template.Jumps) != len(jumpTargets[template.ID]) {
		return fmt.Errorf("%s: incorrect mandatory jump inventory", template.ID)
	}
	for i, jump := range template.Jumps {
		supportID := ""
		if template.ID == "BAY-UTL" && i == 2 {
			supportID = "RETURN-LOW"
		}
		if jump.ID != jumpIDs[template.ID][i] || jump.Landing != jumpTargets[template.ID][i] || jump.Support != supportID || (template.ID == "BAY-UTL" && i > 0 && jump.Direction != -1) {
			return fmt.Errorf("%s: incorrect mandatory jump reference", template.ID)
		}
		support := bayJumpSupport(template.Floor, features[jump.Support], jump.Support)
		if err := validateFloorJump(support, jump, features[jump.Landing]); err != nil {
			return fmt.Errorf("%s: %s: %w", template.ID, jump.ID, err)
		}
		if err := validateReverseDrop(support, jump, features[jump.Landing]); err != nil {
			return fmt.Errorf("%s: %s return: %w", template.ID, jump.ID, err)
		}
	}
	switch template.ID {
	case "BAY-SUP":
		crusher, catwalk := features["CRUSHER"], features["CATWALK"]
		left, right, passage := features["CRUSHER-CONTROL-L"], features["CRUSHER-CONTROL-R"], features["SIDE-PASSAGE"]
		if crusher.X != 720 || crusher.Width != 96 || catwalk.X != 560 || catwalk.Width != 416 || catwalk.Y != 80 || left.X >= crusher.X || right.X <= crusher.X+crusher.Width || left.Y != 0 || right.Y != 0 || passage.X > left.X || passage.X+passage.Width < right.X+right.Width || passage.Y != 0 || passage.Height < 58 {
			return fmt.Errorf("%s: incorrect crusher or catwalk", template.ID)
		}
	case "BAY-UTL":
		first, second := features["STEP-1"], features["STEP-2"]
		opening, paddle := features["OPENING"], features["PADDLE"]
		low, high := features["RETURN-LOW"], features["RETURN-HIGH"]
		if first.X != 480 || first.Y != 80 || second.X != 608 || second.Y != 80 || opening.Height != 96 || opening.X != 640 || paddle.Width != 64 || paddle.Height != 64 || paddle.X+paddle.Width/2-opening.X != 180 || points[RolePaddle].X != paddle.X+paddle.Width/2 || points[RolePaddle].Y != paddle.Y+paddle.Height/2 || low.X != 720 || low.Y != 40 || low.Width != 80 || high.X != 640 || high.Y != 80 || high.Width != 80 {
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

func validateBayTerrain(template BayTemplate, features map[string]BayFeature, points map[ObjectRole]BayPoint) error {
	kinds := map[string]map[string]string{
		"BAY-SUP": {"CATWALK": "player_platform", "CRUSHER": "crusher"},
		"BAY-RES": {"GATE": "gate"},
		"BAY-UTL": {"STEP-1": "one_way", "STEP-2": "one_way", "RETURN-LOW": "one_way", "RETURN-HIGH": "one_way", "OPENING": "player_only", "PADDLE": "projectile_target"},
		"BAY-PRV": {"LEDGE": "one_way", "CAMERA": "projectile_target"},
	}[template.ID]
	if len(template.Terrain) != len(kinds) || len(template.Passages) != 1 {
		return fmt.Errorf("missing action terrain or passage")
	}
	seen := make(map[string]bool, len(kinds))
	for _, collider := range template.Terrain {
		kind, ok := kinds[collider.ID]
		if !ok || seen[collider.ID] || collider.Kind != kind || !validRect(collider.Bounds) || !inside(collider.Bounds, Rect{Width: template.Width, Height: template.Height}) {
			return fmt.Errorf("invalid action collider %s", collider.ID)
		}
		seen[collider.ID] = true
		var expected Rect
		if collider.ID == "GATE" {
			expected = Rect{X: points[RoleGate].X, Y: 0, Width: 16, Height: 128}
		} else {
			feature := features[collider.ID]
			expected = Rect{X: feature.X, Y: feature.Y, Width: feature.Width, Height: feature.Height}
			if kind == "one_way" || kind == "player_platform" {
				expected.Y -= feature.Height
			}
		}
		if collider.Bounds != expected {
			return fmt.Errorf("collider %s does not match action geometry", collider.ID)
		}
	}
	passage := template.Passages[0]
	name := map[string]string{"BAY-SUP": "SIDE-PASSAGE", "BAY-RES": "RESCUE-ROUTE", "BAY-UTL": "RETURN-PASSAGE", "BAY-PRV": "UNDER-LEDGE"}[template.ID]
	if passage.ID != name || !validRect(passage.Bounds) || passage.Bounds.Y != 0 || passage.Bounds.Height < 58 || passage.Bounds.X < template.Floor.X || passage.Bounds.X+passage.Bounds.Width > template.Floor.X+template.Floor.Width {
		return fmt.Errorf("invalid required passage")
	}
	if template.ID == "BAY-SUP" {
		feature := features["SIDE-PASSAGE"]
		if passage.Bounds != (Rect{X: feature.X, Y: feature.Y, Width: feature.Width, Height: feature.Height}) {
			return fmt.Errorf("side passage does not match its feature")
		}
	} else if passage.Bounds.X != 240 || passage.Bounds.Width != 1296 {
		return fmt.Errorf("required floor passage is disconnected")
	}
	return nil
}

func baySolidTerrain(template BayTemplate) []Rect {
	var solids []Rect
	for _, collider := range template.Terrain {
		if collider.Kind == "player_only" || collider.Kind == "solid" {
			solids = append(solids, collider.Bounds)
		}
	}
	return solids
}

func bayJumpSupport(floor Rect, feature BayFeature, id string) Rect {
	if id == "" {
		return floor
	}
	return Rect{X: feature.X, Y: feature.Y - feature.Height, Width: feature.Width, Height: feature.Height}
}

// validateFloorJump checks the run-up and a held-jump trajectory at constant
// horizontal speed. Callers supply the solids and hazards for the current state.
func validateFloorJump(floor Rect, jump BayJump, landing BayFeature) error {
	if jump.Direction != 0 && jump.Direction != 1 && jump.Direction != -1 {
		return fmt.Errorf("invalid jump direction")
	}
	if jump.Direction == -1 {
		floor = mirrorRect(floor)
		landing.X = -landing.X - landing.Width
		jump.TakeoffX = -jump.TakeoffX
		jump.Camera = mirrorRect(jump.Camera)
		jump.Occluders = mirrorRects(jump.Occluders)
		jump.Blockers = mirrorRects(jump.Blockers)
	}
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
	for _, blocker := range jump.Blockers {
		if !validRect(blocker) || overlaps(blocker, Rect{X: jump.TakeoffX - runup - 14, Y: takeoffRect.Y, Width: runup + 28, Height: 58}) {
			return fmt.Errorf("invalid or obstructed run-up")
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
		swept := Rect{X: previousX - 14, Y: math.Min(previousY, y), Width: x - previousX + 28, Height: math.Abs(y-previousY) + 58}
		for _, blocker := range jump.Blockers {
			if overlaps(swept, blocker) {
				return fmt.Errorf("obstructed jump trajectory")
			}
		}
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

// validateReverseDrop checks the return from a one-way landing to its support.
// The player walks off the near edge at 160 pixels/second, without a jump.
func validateReverseDrop(support Rect, jump BayJump, landing BayFeature) error {
	if !validRect(support) || !validRect(jump.Camera) || !finite(landing.X) || !finite(landing.Y) || !finite(landing.Width) || landing.Width < 28 {
		return fmt.Errorf("invalid drop geometry")
	}
	targetY := support.Y + support.Height
	if landing.Y-targetY <= 0 || landing.Y-targetY > 100 {
		return fmt.Errorf("invalid drop height")
	}
	direction := -1.0
	x := landing.X - 14
	if jump.Direction == -1 {
		direction = 1
		x = landing.X + landing.Width + 14
	}
	y, vy := landing.Y, 0.0
	start := Rect{X: x - 14, Y: y, Width: 28, Height: 58}
	if !inside(start, jump.Camera) {
		return fmt.Errorf("drop takeoff outside view")
	}
	for tick := 0; tick < 120; tick++ {
		previousX, previousY := x, y
		vy -= 2200.0 / 120
		x += direction * 160 / 120
		y += vy / 120
		endX, endY := x, y
		if y < targetY {
			fraction := (previousY - targetY) / (previousY - y)
			endX, endY = previousX+fraction*(x-previousX), targetY
		}
		swept := Rect{X: math.Min(previousX, endX) - 14, Y: endY, Width: math.Abs(endX-previousX) + 28, Height: previousY - endY + 58}
		for _, blocker := range jump.Blockers {
			if !validRect(blocker) || overlaps(swept, blocker) {
				return fmt.Errorf("obstructed return drop")
			}
		}
		if previousY >= targetY && y <= targetY {
			fraction := (previousY - targetY) / (previousY - y)
			landingX := previousX + fraction*(x-previousX)
			marker := Rect{X: landingX - 14, Y: targetY, Width: 28, Height: 8}
			if landingX-14 < support.X || landingX+14 > support.X+support.Width || !inside(marker, jump.Camera) {
				return fmt.Errorf("drop landing unavailable or outside view")
			}
			for _, occluder := range jump.Occluders {
				if !validRect(occluder) || overlaps(marker, occluder) {
					return fmt.Errorf("drop landing obscured")
				}
			}
			return nil
		}
	}
	return fmt.Errorf("drop does not reach support")
}

// validateOtherPlatforms rejects a jump whose first descending contact would
// be an unintended one-way platform, before it reaches its named landing.
func validateOtherPlatforms(support Rect, jump BayJump, landing BayFeature, platforms []Rect) error {
	direction := 1.0
	if jump.Direction == -1 {
		direction = -1
	}
	x, y, vy := jump.TakeoffX, support.Y+support.Height, 760.0
	for tick := 0; tick < 240; tick++ {
		previousX, previousY := x, y
		vy -= 2200.0 / 120
		x += direction * jump.Speed / 120
		y += vy / 120
		if vy >= 0 {
			continue
		}
		for _, platform := range platforms {
			top := platform.Y + platform.Height
			if top <= landing.Y || previousY < top || y > top {
				continue
			}
			fraction := (previousY - top) / (previousY - y)
			contactX := previousX + fraction*(x-previousX)
			if contactX+14 > platform.X && contactX-14 < platform.X+platform.Width {
				return fmt.Errorf("jump lands on an unintended platform")
			}
		}
		if previousY >= landing.Y && y <= landing.Y {
			return nil
		}
	}
	return fmt.Errorf("jump never reaches its landing")
}

func mirrorRect(rect Rect) Rect {
	rect.X = -rect.X - rect.Width
	return rect
}

func mirrorRects(rects []Rect) []Rect {
	result := make([]Rect, len(rects))
	for i, rect := range rects {
		result[i] = mirrorRect(rect)
	}
	return result
}

func expandBayHazards(templateID string, variant BayVariant) []BayHazard {
	hazards := make([]BayHazard, 0, len(variant.Stations))
	for i, x := range variant.Stations {
		bounds := Rect{X: x - 32, Y: 0, Width: 64, Height: 96}
		hazard := BayHazard{Bounds: bounds, WarningMS: 900, ActiveMS: 700, InactiveMS: 2000}
		if templateID == "BAY-SUP" && i == 0 {
			hazard.Bounds = Rect{X: 720, Y: 0, Width: 96, Height: 160}
			hazard.WarningMS, hazard.ActiveMS, hazard.InactiveMS = 1200, 500, 2500
			hazard.Lethal = true
		}
		if variant.Mode == "alternating" && i == 1 {
			hazard.OffsetMS = 1800
		}
		hazard.Waiting = Rect{X: hazard.Bounds.X - 160, Y: 0, Width: 160, Height: 58}
		hazards = append(hazards, hazard)
	}
	return hazards
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
	if variant.Number == 2 {
		if !validRect(variant.Cover) || variant.Cover.X != variant.CoverX || variant.Cover.Y != 0 || variant.Cover.Width < 28 || variant.Cover.Height < 58 || variant.CoverKind != "player_projectile" || !inside(variant.Cover, Rect{X: 240, Y: 0, Width: 1040, Height: 560}) {
			return fmt.Errorf("variant %d: invalid cover geometry", variant.Number)
		}
	} else if variant.Cover != (Rect{}) || variant.CoverKind != "" {
		return fmt.Errorf("variant %d: unexpected cover", variant.Number)
	}
	if variant.Number == 4 {
		if !validRect(variant.Alcove) || variant.Alcove.Y != 0 || variant.Alcove.Width < 160 || variant.Alcove.Height < 58 || variant.Alcove.X < 240 || variant.Alcove.X+variant.Alcove.Width > 1008 {
			return fmt.Errorf("variant %d: invalid waiting alcove", variant.Number)
		}
	} else if variant.Alcove != (Rect{}) {
		return fmt.Errorf("variant %d: unexpected waiting alcove", variant.Number)
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
	template.Terrain = append([]BayCollider(nil), template.Terrain...)
	template.Passages = append([]BayPassage(nil), template.Passages...)
	template.Jumps = append([]BayJump(nil), template.Jumps...)
	for i := range template.Jumps {
		template.Jumps[i].Occluders = append([]Rect(nil), template.Jumps[i].Occluders...)
		template.Jumps[i].Blockers = append([]Rect(nil), template.Jumps[i].Blockers...)
	}
	return template
}

func cloneBayVariant(variant BayVariant) BayVariant {
	variant.Enemies = append([]BayEnemy(nil), variant.Enemies...)
	variant.Stations = append([]float64(nil), variant.Stations...)
	return variant
}

func validateBaySafety(template BayTemplate, vestibule []BayPoint, safety BaySafety) error {
	if len(safety.Platforms) != 5 || len(safety.Jumps) != 6 || len(safety.States) != 3 || safety.Refuge.Y != 0 || safety.Refuge.X < 1280 || safety.Refuge.X > 1450 {
		return fmt.Errorf("incomplete bypass or refuge")
	}
	bounds := Rect{X: 0, Y: -8, Width: 1536, Height: 568}
	if !validRect(safety.VestibuleProtect) || !inside(safety.VestibuleProtect, bounds) || !validRect(safety.RefugeProtect) || !inside(safety.RefugeProtect, bounds) || !inside(Rect{X: safety.Refuge.X - 14, Y: safety.Refuge.Y, Width: 28, Height: 58}, safety.RefugeProtect) {
		return fmt.Errorf("missing protection for refuge or vestibule")
	}
	for _, point := range vestibule {
		if !inside(Rect{X: point.X - 14, Y: point.Y, Width: 28, Height: 58}, safety.VestibuleProtect) {
			return fmt.Errorf("unprotected vestibule station %s", point.Role)
		}
	}
	heights := []float64{80, 160, 240, 160, 80}
	for i, platform := range safety.Platforms {
		if !validRect(platform) || !inside(platform, bounds) || platform.Width < 28 || platform.Y+platform.Height != heights[i] {
			return fmt.Errorf("invalid bypass platform %d", i)
		}
	}
	supports := []int{-1, 0, 1, -1, 4, 3}
	targets := []int{0, 1, 2, 4, 3, 2}
	seenJumps := make(map[string]bool, len(safety.Jumps)+len(template.Jumps))
	for _, jump := range template.Jumps {
		seenJumps[jump.ID] = true
	}
	for i, route := range safety.Jumps {
		if route.Support != supports[i] || route.Target != targets[i] || route.Jump.Landing != fmt.Sprintf("BYPASS-%d", route.Target) || len(route.Jump.ID) > 160 || !identifierPattern.MatchString(route.Jump.ID) || seenJumps[route.Jump.ID] || (i < 3 && route.Jump.Direction == -1) || (i >= 3 && route.Jump.Direction != -1) {
			return fmt.Errorf("invalid or duplicate bypass jump %d", i)
		}
		seenJumps[route.Jump.ID] = true
	}
	platforms := append([]Rect(nil), safety.Platforms...)
	for _, collider := range template.Terrain {
		if collider.Kind == "one_way" || collider.Kind == "player_platform" {
			platforms = append(platforms, collider.Bounds)
		}
	}
	for stateIndex, state := range safety.States {
		if state.Name != []string{"normal", "emergency", "aftermath"}[stateIndex] {
			return fmt.Errorf("missing route state")
		}
		obstacles := append(append(append(baySolidTerrain(template), state.Solids...), state.Lethal...), state.Damage...)
		for _, obstacle := range obstacles {
			if !validRect(obstacle) || !inside(obstacle, bounds) || overlaps(obstacle, template.Floor) || overlaps(obstacle, safety.VestibuleProtect) || overlaps(obstacle, safety.RefugeProtect) {
				return fmt.Errorf("invalid or unprotected state obstacle")
			}
		}
		for i, solid := range state.Solids {
			for _, other := range state.Solids[i+1:] {
				if overlaps(solid, other) {
					return fmt.Errorf("%s has overlapping solids", state.Name)
				}
			}
			for _, platform := range safety.Platforms {
				if overlaps(solid, platform) {
					return fmt.Errorf("%s blocks bypass support", state.Name)
				}
			}
		}
		for _, platform := range safety.Platforms {
			walk := Rect{X: platform.X, Y: platform.Y + platform.Height, Width: platform.Width, Height: 58}
			for _, obstacle := range obstacles {
				if overlaps(walk, obstacle) {
					return fmt.Errorf("%s obstructs bypass", state.Name)
				}
			}
		}
		for i, route := range safety.Jumps {
			support := template.Floor
			if route.Support >= 0 {
				support = safety.Platforms[route.Support]
			}
			platform := safety.Platforms[route.Target]
			width := math.Min(platform.Width, 128)
			x := platform.X
			if route.Jump.Direction == -1 {
				x += platform.Width - width
			}
			landing := BayFeature{ID: route.Jump.Landing, X: x, Y: platform.Y + platform.Height, Width: width, Height: platform.Height}
			jump := route.Jump
			jump.Blockers = append(append([]Rect(nil), jump.Blockers...), obstacles...)
			jump.Occluders = append(append([]Rect(nil), jump.Occluders...), state.Solids...)
			if err := validateFloorJump(support, jump, landing); err != nil {
				return fmt.Errorf("%s bypass jump %d: %w", state.Name, i, err)
			}
			if err := validateOtherPlatforms(support, jump, landing, platforms); err != nil {
				return fmt.Errorf("%s bypass jump %d: %w", state.Name, i, err)
			}
			if err := validateReverseDrop(support, jump, landing); err != nil {
				return fmt.Errorf("%s bypass return %d: %w", state.Name, i, err)
			}
		}
		points := append(append([]BayPoint(nil), vestibule...), template.Objects...)
		points = append(points, BayPoint{Role: RoleRefuge, X: safety.Refuge.X, Y: safety.Refuge.Y})
		for _, feature := range template.Features {
			if feature.ID == "REFILL" || feature.ID == "CRUSHER-CONTROL-L" || feature.ID == "CRUSHER-CONTROL-R" {
				points = append(points, BayPoint{Role: RoleRefill, X: feature.X + feature.Width/2, Y: feature.Y})
			}
		}
		for _, point := range points {
			if point.Role == RoleCamera || point.Role == RolePaddle {
				continue
			}
			if !clearFloorPath(35, point.X, obstacles) && !clearFloorPath(1501, point.X, obstacles) {
				return fmt.Errorf("%s blocks %s at %g", state.Name, point.Role, point.X)
			}
		}
		if err := validateProjectileLane(template, state.Solids); err != nil {
			return fmt.Errorf("%s: %w", state.Name, err)
		}
		if stateIndex == 0 {
			for _, passage := range template.Passages {
				for _, obstacle := range obstacles {
					if overlaps(passage.Bounds, obstacle) {
						return fmt.Errorf("normal state obstructs %s", passage.ID)
					}
				}
			}
		}
		if stateIndex == 0 && (template.ID == "BAY-SUP" || template.ID == "BAY-RES") {
			from, to := 320.0, 1320.0
			if template.ID == "BAY-RES" {
				from, to = 800, 1312
			}
			if !clearFloorPath(from, to, obstacles) {
				return fmt.Errorf("normal state blocks trolley or adult route")
			}
		}
		if math.Abs(safety.Refuge.X-1344) > 120 {
			return fmt.Errorf("refuge too far from certification")
		}
		for _, route := range template.Jumps {
			support := template.Floor
			for _, feature := range template.Features {
				if feature.ID == route.Support {
					support = bayJumpSupport(template.Floor, feature, route.Support)
				}
			}
			for _, feature := range template.Features {
				if feature.ID == route.Landing {
					jump := route
					jump.Blockers = append(append([]Rect(nil), jump.Blockers...), obstacles...)
					jump.Occluders = append(append([]Rect(nil), jump.Occluders...), state.Solids...)
					if err := validateFloorJump(support, jump, feature); err != nil {
						return fmt.Errorf("%s action jump: %w", state.Name, err)
					}
					if err := validateOtherPlatforms(support, jump, feature, platforms); err != nil {
						return fmt.Errorf("%s action jump: %w", state.Name, err)
					}
					if err := validateReverseDrop(support, jump, feature); err != nil {
						return fmt.Errorf("%s action return: %w", state.Name, err)
					}
				}
			}
		}
	}
	return nil
}

// validateProjectileLane keeps the marked opening and receptor reachable.
// Player-only barriers deliberately do not stop the corresponding projectile.
func validateProjectileLane(template BayTemplate, solids []Rect) error {
	var lane Rect
	switch template.ID {
	case "BAY-UTL":
		lane = Rect{X: 640, Y: 119, Width: 180, Height: 2}
	case "BAY-PRV":
		lane = Rect{X: 640, Y: 119, Width: 160, Height: 2}
	default:
		return nil
	}
	for _, collider := range template.Terrain {
		if collider.Kind == "solid" && overlaps(lane, collider.Bounds) {
			return fmt.Errorf("solid blocks projectile lane")
		}
	}
	for _, solid := range solids {
		if overlaps(lane, solid) {
			return fmt.Errorf("solid blocks projectile lane")
		}
	}
	return nil
}

func clearFloorPath(from, to float64, obstacles []Rect) bool {
	walk := Rect{X: math.Min(from, to) - 14, Y: 0, Width: math.Abs(to-from) + 28, Height: 58}
	for _, obstacle := range obstacles {
		if overlaps(walk, obstacle) {
			return false
		}
	}
	return true
}

func validateVariantGeometry(template BayTemplate, vestibule []BayPoint, variant BayVariant, hazards []BayHazard, safety BaySafety) error {
	platforms := append([]Rect(nil), safety.Platforms...)
	for _, collider := range template.Terrain {
		if collider.Kind == "one_way" || collider.Kind == "player_platform" {
			platforms = append(platforms, collider.Bounds)
		}
	}
	if variant.CoverKind == "player_projectile" {
		for _, enemy := range variant.Enemies {
			body := Rect{X: enemy.X - 14, Y: 0, Width: 28, Height: 58}
			if overlaps(variant.Cover, body) {
				return fmt.Errorf("enemy initially overlaps cover")
			}
		}
		for _, collider := range template.Terrain {
			if overlaps(variant.Cover, collider.Bounds) {
				return fmt.Errorf("cover overlaps action terrain")
			}
		}
		for _, platform := range platforms {
			walk := Rect{X: platform.X, Y: platform.Y + platform.Height, Width: platform.Width, Height: 58}
			if overlaps(variant.Cover, platform) || overlaps(variant.Cover, walk) {
				return fmt.Errorf("cover obstructs a supported route")
			}
		}
		points := append(append([]BayPoint(nil), vestibule...), template.Objects...)
		for _, point := range points {
			body := Rect{X: point.X - 14, Y: point.Y, Width: 28, Height: 58}
			if overlaps(variant.Cover, body) {
				return fmt.Errorf("cover overlaps a required object")
			}
		}
	}
	for stateIndex, state := range safety.States {
		obstacles := append(append(append(baySolidTerrain(template), state.Solids...), state.Lethal...), state.Damage...)
		if variant.CoverKind == "player_projectile" {
			for _, obstacle := range obstacles {
				if overlaps(variant.Cover, obstacle) {
					return fmt.Errorf("%s obstructs variant cover", state.Name)
				}
			}
			obstacles = append(obstacles, variant.Cover)
			if err := validateProjectileLane(template, append(append([]Rect(nil), state.Solids...), variant.Cover)); err != nil {
				return fmt.Errorf("%s: %w", state.Name, err)
			}
			if err := validateCoverRoutes(template, variant.Cover, safety, obstacles, platforms); err != nil {
				return fmt.Errorf("%s: %w", state.Name, err)
			}
		}
		for _, hazard := range hazards {
			for _, obstacle := range obstacles {
				if overlaps(hazard.Waiting, obstacle) {
					return fmt.Errorf("%s obstructs hazard waiting floor", state.Name)
				}
			}
		}
		if variant.WaitingAlcove {
			for _, obstacle := range obstacles {
				if overlaps(variant.Alcove, obstacle) {
					return fmt.Errorf("%s obstructs waiting alcove", state.Name)
				}
			}
			center := variant.Alcove.X + variant.Alcove.Width/2
			if !clearFloorPath(35, center, obstacles) && !clearFloorPath(1501, center, obstacles) {
				return fmt.Errorf("%s disconnects waiting alcove", state.Name)
			}
		}
		if stateIndex == 0 {
			for _, enemy := range variant.Enemies {
				body := Rect{X: enemy.X - 14, Y: 0, Width: 28, Height: 58}
				for _, obstacle := range obstacles {
					if overlaps(body, obstacle) {
						return fmt.Errorf("enemy initially overlaps terrain")
					}
				}
			}
		}
	}
	return nil
}

// The cover blocks player movement and projectiles while trolleys and rescue
// adults use the continuous floor. Both player crossings are validated.
func validateCoverRoutes(template BayTemplate, cover Rect, safety BaySafety, obstacles, platforms []Rect) error {
	check := func(support Rect, jump BayJump, landing BayFeature, drop bool) error {
		jump.Blockers = append(append([]Rect(nil), jump.Blockers...), obstacles...)
		jump.Occluders = append(append([]Rect(nil), jump.Occluders...), cover)
		if err := validateFloorJump(support, jump, landing); err != nil {
			return err
		}
		if err := validateOtherPlatforms(support, jump, landing, platforms); err != nil {
			return err
		}
		if drop {
			return validateReverseDrop(support, jump, landing)
		}
		return nil
	}
	for _, route := range safety.Jumps {
		support := template.Floor
		if route.Support >= 0 {
			support = safety.Platforms[route.Support]
		}
		platform := safety.Platforms[route.Target]
		width := math.Min(platform.Width, 128)
		x := platform.X
		if route.Jump.Direction == -1 {
			x += platform.Width - width
		}
		landing := BayFeature{ID: route.Jump.Landing, X: x, Y: platform.Y + platform.Height, Width: width, Height: platform.Height}
		if err := check(support, route.Jump, landing, true); err != nil {
			return fmt.Errorf("cover blocks bypass %s: %w", route.Jump.ID, err)
		}
	}
	for _, jump := range template.Jumps {
		support := template.Floor
		var landing BayFeature
		for _, feature := range template.Features {
			if feature.ID == jump.Support {
				support = bayJumpSupport(template.Floor, feature, jump.Support)
			}
			if feature.ID == jump.Landing {
				landing = feature
			}
		}
		if err := check(support, jump, landing, true); err != nil {
			return fmt.Errorf("cover blocks action %s: %w", jump.ID, err)
		}
	}
	camera := Rect{X: 0, Y: -160, Width: 1280, Height: 720}
	cross := BayJump{TakeoffX: 820, Speed: 320, Camera: camera}
	if err := check(template.Floor, cross, BayFeature{X: 960, Y: 0, Width: 110, Height: 8}, false); err != nil {
		return fmt.Errorf("cover blocks forward crossing: %w", err)
	}
	cross = BayJump{TakeoffX: 1000, Speed: 250, Direction: -1, Camera: camera}
	landing := BayFeature{X: 800, Y: 0, Width: 80, Height: 8}
	if template.ID == "BAY-SUP" {
		cross.TakeoffX = 1040
		landing = BayFeature{X: 560, Y: 80, Width: 416, Height: 8}
	}
	if err := check(template.Floor, cross, landing, false); err != nil {
		return fmt.Errorf("cover blocks return crossing: %w", err)
	}
	return nil
}

func cloneBaySafety(safety BaySafety) BaySafety {
	safety.Platforms = append([]Rect(nil), safety.Platforms...)
	safety.Jumps = append([]BayRouteJump(nil), safety.Jumps...)
	for i := range safety.Jumps {
		jump := &safety.Jumps[i].Jump
		jump.Occluders = append([]Rect(nil), jump.Occluders...)
		jump.Blockers = append([]Rect(nil), jump.Blockers...)
	}
	safety.States = append([]BayRouteState(nil), safety.States...)
	for i := range safety.States {
		safety.States[i].Solids = append([]Rect(nil), safety.States[i].Solids...)
		safety.States[i].Lethal = append([]Rect(nil), safety.States[i].Lethal...)
		safety.States[i].Damage = append([]Rect(nil), safety.States[i].Damage...)
	}
	return safety
}

func cloneInstitution(layout InstitutionGeometry) InstitutionGeometry {
	layout.Bays = append([]InstitutionBay(nil), layout.Bays...)
	layout.Links = append([]EncounterLink(nil), layout.Links...)
	layout.ReturnLobbies = append([]ReturnLobby(nil), layout.ReturnLobbies...)
	for i := range layout.ReturnLobbies {
		lobby := &layout.ReturnLobbies[i]
		lobby.Solids = append([]Rect(nil), lobby.Solids...)
		lobby.Lethal = append([]Rect(nil), lobby.Lethal...)
	}
	layout.Prologue = append([]PrologueGeometry(nil), layout.Prologue...)
	for i := range layout.Prologue {
		room := &layout.Prologue[i]
		room.Platforms = append([]Rect(nil), room.Platforms...)
		room.Features = append([]BayFeature(nil), room.Features...)
		room.Solids = append([]Rect(nil), room.Solids...)
		room.Lethal = append([]Rect(nil), room.Lethal...)
		room.Jumps = append([]BossJump(nil), room.Jumps...)
		for j := range room.Jumps {
			jump := &room.Jumps[j].Jump
			jump.Occluders = append([]Rect(nil), jump.Occluders...)
			jump.Blockers = append([]Rect(nil), jump.Blockers...)
		}
	}
	layout.Boss.Refills = append([]GeometryPoint(nil), layout.Boss.Refills...)
	layout.Boss.Platforms = append([]Rect(nil), layout.Boss.Platforms...)
	layout.Boss.Protection = append([]Rect(nil), layout.Boss.Protection...)
	layout.Boss.Features = append([]BayFeature(nil), layout.Boss.Features...)
	layout.Boss.Jumps = append([]BossJump(nil), layout.Boss.Jumps...)
	for i := range layout.Boss.Jumps {
		jump := &layout.Boss.Jumps[i].Jump
		jump.Occluders = append([]Rect(nil), jump.Occluders...)
		jump.Blockers = append([]Rect(nil), jump.Blockers...)
	}
	layout.Boss.Solids = append([]Rect(nil), layout.Boss.Solids...)
	layout.Boss.Lethal = append([]Rect(nil), layout.Boss.Lethal...)
	return layout
}

func validateInstitution(ctx context.Context, layout InstitutionGeometry, campaign *Campaign) (err error) {
	defer mon.Task()(&ctx)(&err)

	if layout.SchemaVersion != 1 || len(layout.Bays) != 4 {
		return fmt.Errorf("invalid institution inventory")
	}
	batches := campaign.Batches(ctx)
	var expected []CampaignBatch
	for _, batch := range batches {
		if batch.Institution == layout.Location {
			expected = append(expected, batch)
		}
	}
	if len(expected) != 4 {
		return fmt.Errorf("unknown institution %s", layout.Location)
	}
	if err = validatePrologue(layout); err != nil {
		return err
	}
	if err = validateReturnLobby(layout); err != nil {
		return err
	}
	hazards := map[LocationID][]string{
		"LOC-HOS": {"steam", "beds", "pressure", "fire"},
		"LOC-CHU": {"candles", "turnstile", "bell", "exhaust"},
		"LOC-MIN": {"paperwork", "desks", "waste", "samples"},
		"LOC-INS": {"boxes", "arch", "lighting", "cables"},
	}[layout.Location]
	bounds := Rect{X: 0, Y: -8, Width: 1536, Height: 568}
	kinds := map[LocationID][]string{
		"LOC-HOS": {"area", "wave", "area", "area"},
		"LOC-CHU": {"area", "area", "fall", "area"},
		"LOC-MIN": {"wave", "wave", "area", "area"},
		"LOC-INS": {"wave", "fall", "area", "area"},
	}[layout.Location]
	refuges := map[LocationID][]string{
		"LOC-HOS": {"BYPASS-ENTRY", "SIDE-REFUGE", "MANUAL-BYPASS", "FIRE-SHUTTER"},
		"LOC-CHU": {"SIDE-GALLERY", "ADULT-REFUGE", "BELL-SHELTER", "EXHAUST-BARRIER"},
		"LOC-MIN": {"PAPER-ALCOVE", "DESK-BYPASS", "DRY-BYPASS", "SAMPLE-CABINET"},
		"LOC-INS": {"REST-ROOM", "SEPARATE-EXIT", "ISOLATED-RAMP", "CABLE-SHELTER"},
	}[layout.Location]
	for i, bay := range layout.Bays {
		batch := expected[i]
		if bay.Encounter != batch.Emergency.Encounter || bay.Anchor != batch.Emergency.Anchor || bay.Emergency != batch.Emergency.ID || bay.Family != batch.Family || bay.Hazard != hazards[i] || !validRect(bay.Danger) || !validRect(bay.Aftermath) || !inside(bay.Danger, bounds) || !inside(bay.Aftermath, bounds) {
			return fmt.Errorf("invalid bay binding %s", bay.Encounter)
		}
		if spaceErr := validateEmergencySpace(bay, bounds, kinds[i], refuges[i]); spaceErr != nil {
			return fmt.Errorf("%s: %w", bay.Encounter, spaceErr)
		}
	}
	var closure *CampaignDeed
	deeds := campaign.Deeds(ctx)
	for i := range deeds {
		if deeds[i].Location == layout.Location && deeds[i].Family == "END" {
			closure = &deeds[i]
		}
	}
	boss := layout.Boss
	if closure == nil || boss.Closure != closure.Deed.ID || boss.Encounter != closure.Deed.Encounter || boss.OutAnchor != closure.Deed.Anchor || len(closure.Deed.Objects) != 1 || boss.Terminal != closure.Deed.Objects[0] || boss.Anchor != AnchorID("ANC-"+string(layout.Location)[4:]+"-05") {
		return fmt.Errorf("invalid boss or closure binding")
	}
	if len(layout.Links) != 4 {
		return fmt.Errorf("missing encounter connection")
	}
	for i, link := range layout.Links {
		next := boss.Encounter
		entry := boss.Checkpoint
		if i < 3 {
			next = expected[i+1].Emergency.Encounter
			entry = GeometryPoint{X: 35, Y: 0}
		}
		if link.From != expected[i].Emergency.Encounter || link.To != next || link.Exit != (GeometryPoint{X: 1501, Y: 0}) || link.Entry != entry {
			return fmt.Errorf("invalid encounter connection %s", link.From)
		}
	}
	destinations := map[LocationID]struct {
		gate     GateID
		location LocationID
	}{
		"LOC-HOS": {gate: "G-CHU-IN", location: "LOC-CHU"},
		"LOC-CHU": {gate: "G-MIN-IN", location: "LOC-MIN"},
		"LOC-MIN": {gate: "G-INS-IN", location: "LOC-INS"},
		"LOC-INS": {gate: "G-PERMIT", location: "LOC-MIN"},
	}
	destination := destinations[layout.Location]
	if layout.ExitGate != destination.gate || layout.ExitLocation != destination.location {
		return fmt.Errorf("invalid institution transit")
	}
	gateFound := false
	for _, gate := range campaign.Gates(ctx) {
		if gate.ID == layout.ExitGate {
			gateFound = true
		}
	}
	if !gateFound {
		return fmt.Errorf("unknown transit gate")
	}
	if !validRect(boss.Bounds) || !validRect(boss.Floor) || !inside(boss.Floor, boss.Bounds) || boss.Floor.Y+boss.Floor.Height != 0 || boss.Floor.X > -240 || boss.Floor.X+boss.Floor.Width < 1520 || len(boss.Refills) != 2 {
		return fmt.Errorf("invalid boss floor or stations")
	}
	for _, rect := range append(append(append([]Rect(nil), boss.Platforms...), boss.Solids...), boss.Lethal...) {
		if !validRect(rect) || !inside(rect, boss.Bounds) {
			return fmt.Errorf("invalid boss geometry")
		}
	}
	heights := map[LocationID][]float64{
		"LOC-HOS": {80, 80},
		"LOC-CHU": {80, 80},
		"LOC-MIN": {80, 160, 80},
		"LOC-INS": {80, 160, 80, 160},
	}[layout.Location]
	if len(boss.Platforms) != len(heights) {
		return fmt.Errorf("incorrect boss platform inventory")
	}
	for i, platform := range boss.Platforms {
		if platform.Y+platform.Height != heights[i] || platform.Width < 28 {
			return fmt.Errorf("invalid boss platform height or width")
		}
	}
	if layout.Location == "LOC-HOS" && (boss.Platforms[0].X != 320 || boss.Platforms[1].X != 880) {
		return fmt.Errorf("incorrect hospital inspection platforms")
	}
	if layout.Location == "LOC-MIN" && (boss.Platforms[1].X != 0 || boss.Platforms[1].Width != 1280) {
		return fmt.Errorf("ministry gantry must span the arena")
	}
	// Each supported route must remain walkable between its jump landing,
	// subsequent takeoff and any stations. In particular this certifies the
	// continuous ministry gantry rather than just its two end ascents.
	bossObstacles := append(append([]Rect(nil), boss.Solids...), boss.Lethal...)
	for _, platform := range boss.Platforms {
		walk := Rect{X: platform.X, Y: platform.Y + platform.Height, Width: platform.Width, Height: 58}
		for _, obstacle := range bossObstacles {
			if overlaps(walk, obstacle) {
				return fmt.Errorf("obstructed supported boss route")
			}
		}
	}
	for i, solid := range boss.Solids {
		for _, other := range boss.Solids[i+1:] {
			if overlaps(solid, other) {
				return fmt.Errorf("overlapping boss solids")
			}
		}
		for _, platform := range boss.Platforms {
			if overlaps(solid, platform) {
				return fmt.Errorf("boss solid intersects platform")
			}
		}
	}
	expectedSupports := []int{-1, -1}
	expectedTargets := []int{0, 1}
	if layout.Location == "LOC-MIN" {
		expectedSupports = []int{-1, 0, -1, 2}
		expectedTargets = []int{0, 1, 2, 1}
	} else if layout.Location == "LOC-INS" {
		expectedSupports = []int{-1, 0, -1, 2}
		expectedTargets = []int{0, 1, 2, 3}
	}
	if len(boss.Jumps) != len(expectedSupports) {
		return fmt.Errorf("missing boss ascent")
	}
	seenJumps := make(map[string]bool, len(boss.Jumps))
	for i, route := range boss.Jumps {
		if route.Support != expectedSupports[i] || route.Target != expectedTargets[i] || seenJumps[route.Jump.ID] {
			return fmt.Errorf("disconnected or duplicate boss ascent")
		}
		seenJumps[route.Jump.ID] = true
		if route.Target < 0 || route.Target >= len(boss.Platforms) || route.Support < -1 || route.Support >= len(boss.Platforms) {
			return fmt.Errorf("invalid boss jump reference")
		}
		support := boss.Floor
		if route.Support >= 0 {
			support = boss.Platforms[route.Support]
		}
		platform := boss.Platforms[route.Target]
		width := math.Min(platform.Width, 160)
		x := platform.X
		if route.Jump.Direction == -1 {
			x += platform.Width - width
		}
		landing := BayFeature{ID: route.Jump.Landing, X: x, Y: platform.Y + platform.Height, Width: width, Height: platform.Height}
		jump := route.Jump
		jump.Blockers = append(append(append([]Rect(nil), jump.Blockers...), boss.Solids...), boss.Lethal...)
		jump.Occluders = append(append([]Rect(nil), jump.Occluders...), boss.Solids...)
		if jump.ID == "" || jump.Landing != fmt.Sprintf("PLATFORM-%d", route.Target) {
			return fmt.Errorf("invalid boss jump identity or landing")
		}
		if jumpErr := validateFloorJump(support, jump, landing); jumpErr != nil {
			return fmt.Errorf("boss jump %s: %w", jump.ID, jumpErr)
		}
		if jumpErr := validateReverseDrop(support, jump, landing); jumpErr != nil {
			return fmt.Errorf("boss return %s: %w", jump.ID, jumpErr)
		}
	}
	positions := append([]GeometryPoint{boss.Checkpoint, boss.Exit, boss.TerminalPosition}, boss.Refills...)
	for _, point := range positions {
		body := Rect{X: point.X - 14, Y: point.Y, Width: 28, Height: 58}
		if !finite(point.X) || !finite(point.Y) || !inside(body, boss.Bounds) || !supported(point, boss.Floor, boss.Platforms) {
			return fmt.Errorf("unsupported or out-of-bounds boss station")
		}
		for _, obstacle := range append(append([]Rect(nil), boss.Solids...), boss.Lethal...) {
			if overlaps(body, obstacle) {
				return fmt.Errorf("unsafe boss station or anchor")
			}
		}
	}
	if boss.Checkpoint.X >= 0 || boss.Exit.X <= 1280 || boss.TerminalPosition.X <= 1280 || boss.Refills[0].X >= boss.Refills[1].X {
		return fmt.Errorf("invalid approach, alcoves or closure")
	}
	protected := append([]GeometryPoint{boss.Checkpoint}, boss.Refills...)
	if len(boss.Protection) != len(protected) {
		return fmt.Errorf("missing boss protection")
	}
	for i, area := range boss.Protection {
		p := protected[i]
		body := Rect{X: p.X - 14, Y: p.Y, Width: 28, Height: 58}
		if !validRect(area) || !inside(area, boss.Bounds) || !inside(body, area) {
			return fmt.Errorf("unprotected boss station")
		}
	}
	expectedFeatures := map[LocationID][]string{
		"LOC-HOS": {"CONVEYOR-L", "CONVEYOR-R", "EXTRACTION-RIG"},
		"LOC-CHU": {"PENDULUM", "PULPIT", "SCREENS"},
		"LOC-MIN": {"SUMP", "GANTRY", "LIFT"},
		"LOC-INS": {"RELAY-A", "RELAY-B", "BROADCAST-CAGE"},
	}[layout.Location]
	if len(boss.Features) != len(expectedFeatures) {
		return fmt.Errorf("missing boss features")
	}
	for i, feature := range boss.Features {
		rect := Rect{X: feature.X, Y: feature.Y, Width: feature.Width, Height: feature.Height}
		if feature.ID != expectedFeatures[i] || !validRect(rect) || !inside(rect, boss.Bounds) {
			return fmt.Errorf("invalid boss feature %s", feature.ID)
		}
	}
	if layout.Location == "LOC-MIN" {
		gantry := boss.Features[1]
		lift := boss.Features[2]
		if (Rect{X: gantry.X, Y: gantry.Y, Width: gantry.Width, Height: gantry.Height}) != boss.Platforms[1] || lift.Y != 160 || lift.X < boss.Platforms[1].X || lift.X+lift.Width > boss.Platforms[1].X+boss.Platforms[1].Width {
			return fmt.Errorf("ministry combat area is disconnected from gantry")
		}
		for _, refill := range boss.Refills {
			if refill.Y != 160 || !supported(refill, boss.Platforms[1], nil) {
				return fmt.Errorf("ministry refill is disconnected from gantry")
			}
		}
	}
	if layout.Location == "LOC-INS" && (boss.Features[0].X+boss.Features[0].Width/2 != 320 || boss.Features[1].X+boss.Features[1].Width/2 != 960) {
		return fmt.Errorf("invalid institute relay positions")
	}
	walk := Rect{X: boss.Checkpoint.X - 14, Y: 0, Width: boss.Exit.X - boss.Checkpoint.X + 28, Height: 58}
	for _, obstacle := range append(append([]Rect(nil), boss.Solids...), boss.Lethal...) {
		if overlaps(walk, obstacle) || overlaps(boss.Floor, obstacle) {
			return fmt.Errorf("obstructed boss approach or closure route")
		}
	}
	return nil
}

func validateEmergencySpace(bay InstitutionBay, bounds Rect, kind, refugeName string) error {
	space := bay.Space
	if space.Kind != kind || space.RefugeName != refugeName || !validRect(space.Refuge) || !validRect(space.Envelope) || !validRect(space.Barrier) || !inside(space.Refuge, bounds) || !inside(space.Envelope, bounds) || !inside(space.Barrier, bounds) || !inside(bay.Danger, space.Envelope) {
		return fmt.Errorf("missing emergency refuge, envelope or barrier")
	}
	if space.Refuge.Y != 0 || space.Refuge.Width < 28 || space.Refuge.Height < 58 || math.Abs(space.Refuge.X+space.Refuge.Width/2-1344) > 120 || overlaps(space.Refuge, space.Envelope) || overlaps(space.Refuge, space.Barrier) || space.Barrier.X != space.Envelope.X+space.Envelope.Width || space.Barrier.Y != 0 || space.Barrier.Height < 58 {
		return fmt.Errorf("unsafe emergency refuge or barrier")
	}
	if kind != "wave" {
		if space.Envelope != bay.Danger || space.Start != (Rect{}) || space.End != (Rect{}) || space.Speed != 0 {
			return fmt.Errorf("invalid stationary emergency envelope")
		}
		return nil
	}
	if !validRect(space.Start) || !validRect(space.End) || space.Start != bay.Danger || space.End.Y != space.Start.Y || space.End.Width != space.Start.Width || space.End.Height != space.Start.Height || space.End.X <= space.Start.X || space.End.X+space.End.Width != space.Barrier.X || space.Speed != 220 || (space.End.X-space.Start.X)/space.Speed > 8 || space.Envelope != (Rect{X: space.Start.X, Y: space.Start.Y, Width: space.End.X + space.End.Width - space.Start.X, Height: space.Start.Height}) {
		return fmt.Errorf("invalid wave travel or stop barrier")
	}
	return nil
}

func validateReturnLobby(layout InstitutionGeometry) error {
	if layout.Location != "LOC-MIN" {
		if len(layout.ReturnLobbies) != 0 {
			return fmt.Errorf("unexpected permit lobby")
		}
		return nil
	}
	if len(layout.ReturnLobbies) != 1 {
		return fmt.Errorf("missing permit lobby")
	}
	lobby := layout.ReturnLobbies[0]
	if lobby.Anchor != "ANC-MIN-PERMIT" || !validRect(lobby.Bounds) || !validRect(lobby.Floor) || !validRect(lobby.Protection) || !inside(lobby.Floor, lobby.Bounds) || !inside(lobby.Protection, lobby.Bounds) || lobby.Floor.Y+lobby.Floor.Height != 0 {
		return fmt.Errorf("invalid permit lobby")
	}
	obstacles := append(append([]Rect(nil), lobby.Solids...), lobby.Lethal...)
	for _, obstacle := range obstacles {
		if !validRect(obstacle) || !inside(obstacle, lobby.Bounds) || overlaps(obstacle, lobby.Floor) {
			return fmt.Errorf("invalid permit lobby obstacle")
		}
	}
	for _, point := range []GeometryPoint{lobby.Entry, lobby.Spawn, lobby.Counter} {
		body := Rect{X: point.X - 14, Y: point.Y, Width: 28, Height: 58}
		if !inside(body, lobby.Protection) || !supported(point, lobby.Floor, nil) || !clearFloorPath(lobby.Entry.X, point.X, obstacles) {
			return fmt.Errorf("unsafe permit lobby route or anchor")
		}
	}
	if math.Abs(lobby.Spawn.X-lobby.Counter.X) > 48 {
		return fmt.Errorf("permit anchor too far from counter")
	}
	return nil
}

func validatePrologue(layout InstitutionGeometry) error {
	expected := map[LocationID][]EncounterID{
		"LOC-HOS": {"ENC-PRO-01", "ENC-PRO-02"},
		"LOC-CHU": {"ENC-PRO-03"},
	}[layout.Location]
	if len(layout.Prologue) != len(expected) {
		return fmt.Errorf("missing prologue encounters")
	}
	for i, room := range layout.Prologue {
		if room.Encounter != expected[i] || room.Anchor != AnchorID("ANC-"+string(room.Encounter)[4:]) {
			return fmt.Errorf("invalid prologue identity")
		}
		if !validRect(room.Bounds) || !validRect(room.Floor) || !inside(room.Floor, room.Bounds) || room.Floor.Y+room.Floor.Height != 0 {
			return fmt.Errorf("invalid prologue floor")
		}
		obstacles := append(append([]Rect(nil), room.Solids...), room.Lethal...)
		for _, rect := range append(append([]Rect(nil), room.Platforms...), obstacles...) {
			if !validRect(rect) || !inside(rect, room.Bounds) || overlaps(rect, room.Floor) {
				return fmt.Errorf("invalid prologue terrain")
			}
		}
		for _, point := range []GeometryPoint{room.Entry, room.Spawn, room.Objective} {
			body := Rect{X: point.X - 14, Y: point.Y, Width: 28, Height: 58}
			if !inside(body, room.Bounds) || !supported(point, room.Floor, room.Platforms) {
				return fmt.Errorf("unsafe prologue point")
			}
			for _, obstacle := range obstacles {
				if overlaps(body, obstacle) {
					return fmt.Errorf("blocked prologue point")
				}
			}
		}
		for _, feature := range room.Features {
			rect := Rect{X: feature.X, Y: feature.Y, Width: feature.Width, Height: feature.Height}
			if feature.ID == "" || !validRect(rect) || !inside(rect, room.Bounds) {
				return fmt.Errorf("invalid prologue feature")
			}
		}
		switch room.Encounter {
		case "ENC-PRO-01":
			if room.Action != "DONATION" || len(room.Jumps) != 2 || len(room.Platforms) != 2 || len(room.Features) != 1 || room.Features[0].ID != "DONATION-BOX" || room.Objective.Y != 160 {
				return fmt.Errorf("invalid annex objective")
			}
			if room.Features[0].X != room.Objective.X || room.Features[0].Y != room.Objective.Y || !clearFloorPath(room.Entry.X, room.Jumps[0].Jump.TakeoffX, obstacles) {
				return fmt.Errorf("blocked donation approach")
			}
			seenJumps := make(map[string]bool, len(room.Jumps))
			for j, route := range room.Jumps {
				if len(route.Jump.ID) > 160 || !identifierPattern.MatchString(route.Jump.ID) || seenJumps[route.Jump.ID] {
					return fmt.Errorf("invalid or duplicate prologue jump identity")
				}
				seenJumps[route.Jump.ID] = true
				platform := room.Platforms[j]
				if platform.Y+platform.Height != float64((j+1)*80) {
					return fmt.Errorf("incorrect annex step height")
				}
				walk := Rect{X: platform.X, Y: platform.Y + platform.Height, Width: platform.Width, Height: 58}
				for _, obstacle := range obstacles {
					if overlaps(walk, obstacle) {
						return fmt.Errorf("blocked annex step")
					}
				}
				if route.Support != j-1 || route.Target != j || route.Jump.Landing != fmt.Sprintf("PLATFORM-%d", j) {
					return fmt.Errorf("disconnected annex steps")
				}
				support := room.Floor
				if j > 0 {
					support = room.Platforms[j-1]
				}
				landing := BayFeature{ID: route.Jump.Landing, X: platform.X, Y: platform.Y + platform.Height, Width: platform.Width, Height: platform.Height}
				jump := route.Jump
				jump.Blockers = append(append([]Rect(nil), jump.Blockers...), obstacles...)
				jump.Occluders = append(append([]Rect(nil), jump.Occluders...), room.Solids...)
				if err := validateFloorJump(support, jump, landing); err != nil {
					return fmt.Errorf("prologue jump %d: %w", j, err)
				}
				if err := validateReverseDrop(support, jump, landing); err != nil {
					return fmt.Errorf("prologue return %d: %w", j, err)
				}
			}
		case "ENC-PRO-02":
			if room.Action != "HEART-ATTACK" || room.Objective.X-room.Entry.X != 320 || len(room.Jumps) != 0 || !clearFloorPath(room.Entry.X, room.Objective.X, obstacles) {
				return fmt.Errorf("invalid heart-attack corridor")
			}
		case "ENC-PRO-03":
			if room.Action != "SERVICE-COUNTER" || len(room.Jumps) != 0 || len(room.Features) != 1 || room.Features[0].ID != "COFFIN" || math.Abs(room.Spawn.X-room.Objective.X) > 48 || !clearFloorPath(room.Entry.X, room.Objective.X, obstacles) {
				return fmt.Errorf("invalid courtyard or service counter")
			}
		}
	}
	return nil
}

func supported(point GeometryPoint, floor Rect, platforms []Rect) bool {
	for _, surface := range append([]Rect{floor}, platforms...) {
		if point.Y == surface.Y+surface.Height && point.X-14 >= surface.X && point.X+14 <= surface.X+surface.Width {
			return true
		}
	}
	return false
}

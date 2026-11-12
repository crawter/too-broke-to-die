// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"slices"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
)

// ServiceStatus describes transient work, not an acknowledged receipt.
type ServiceStatus string

const (
	// ServiceAvailable is an authored request whose prerequisites are met.
	ServiceAvailable ServiceStatus = "available"
	// ServiceBlocked is an authored request with missing prerequisites.
	ServiceBlocked ServiceStatus = "blocked"
	// ServiceCertified is represented by an acknowledged receipt.
	ServiceCertified ServiceStatus = "certified"
	// ServiceActive is an accepted, unfinished attempt.
	ServiceActive ServiceStatus = "active"
	// ServiceEvidenceReady means the certification hold is now possible.
	ServiceEvidenceReady ServiceStatus = "evidence_ready"
	// ServiceFailed requires a request reset before further work.
	ServiceFailed ServiceStatus = "attempt_failed"
	// ServicePending means a completed action awaits a durable transition.
	ServicePending ServiceStatus = "pending"
)

// ServiceView is a detached description of one attempt. A candidate is not a receipt.
type ServiceView struct {
	Deed              content.DeedID
	Attempt           uint64
	Family            string
	Variant           int
	Title             string
	Status            ServiceStatus
	Objective         string
	Failure           string
	Invalid           string
	RecorderStarted   bool
	CameraInterrupted bool
	CameraFouled      bool
	PaddleLatched     bool
	BypassClosed      bool
	FuseTripped       bool
	SealBroken        bool
	AdultFollowing    bool
	AdultWaiting      bool
	AdultInRefuge     bool
	Receipt           *campaign.Receipt
}

type serviceState struct {
	deed                             content.CampaignDeed
	attempt                          campaign.Attempt
	roles                            map[content.ObjectRole]content.ObjectID
	ticketObjects                    []content.ObjectID
	hazards                          []content.BayHazard
	status                           ServiceStatus
	invalid                          string
	recorder, camera, paddle, bypass bool
	fouled, fuseTripped, sealBroken  bool
	following, waiting, refuge       bool
	waitIndex                        int
	pending                          *campaign.CertificationCandidate
	failure                          bool
	prepared                         *campaign.Snapshot
	attemptChange                    *campaign.AttemptChange
	committed                        campaign.Snapshot
	lastReceipt                      *campaign.Receipt
}

func copyService(state serviceState) serviceState {
	roles := make(map[content.ObjectRole]content.ObjectID, len(state.roles))
	for role, id := range state.roles {
		roles[role] = id
	}
	state.roles = roles
	state.ticketObjects = slices.Clone(state.ticketObjects)
	state.hazards = slices.Clone(state.hazards)
	if state.pending != nil {
		candidate := *state.pending
		candidate.Event.EvidenceObjects = slices.Clone(candidate.Event.EvidenceObjects)
		state.pending = &candidate
	}
	if state.attemptChange != nil {
		change := *state.attemptChange
		state.attemptChange = &change
	}
	if state.lastReceipt != nil {
		receipt := *state.lastReceipt
		receipt.Certification.EvidenceObjects = slices.Clone(receipt.Certification.EvidenceObjects)
		state.lastReceipt = &receipt
	}
	return state
}

func (world *World) installService(ctx context.Context, manifest *content.Campaign, bays []content.ServiceBay, committed campaign.Snapshot) (err error) {
	defer mon.Task()(&ctx)(&err)
	world.service.committed = committed
	attempt, ok := committed.ActiveAttempt()
	if !ok {
		return nil
	}
	var deed content.CampaignDeed
	for _, entry := range manifest.Deeds(ctx) {
		if entry.Deed.ID == attempt.Deed {
			deed = entry
			break
		}
	}
	if deed.Family == "END" || deed.Deed.Encounter != world.state.Encounter {
		return nil
	}
	var bay *content.ServiceBay
	for i := range bays {
		if bays[i].Deed == attempt.Deed {
			bay = &bays[i]
			break
		}
	}
	if bay == nil || deed.Deed.ID == "" {
		return fmt.Errorf("missing active service binding %s", attempt.Deed)
	}
	service := serviceState{deed: deed, attempt: attempt, roles: make(map[content.ObjectRole]content.ObjectID), hazards: slices.Clone(bay.Hazards), status: ServiceActive, committed: committed}
	for _, placed := range bay.Objects {
		object := placed.Object
		service.roles[object.Role] = object.ID
	}
	// Candidate evidence follows the manifest order, not geometry ordering.
	for _, id := range deed.Deed.Objects {
		for _, placed := range bay.Objects {
			if placed.Object.ID == id && placed.Object.Deed == deed.Deed.ID {
				service.ticketObjects = append(service.ticketObjects, id)
			}
		}
	}
	world.service = service
	// Keep only the active certification target selectable. Sibling receipts
	// remain accessible through the protected request console.
	terminals := make(map[string]bool)
	for _, entity := range world.state.Entities {
		if entity.Role == content.RoleTerminal && entity.ID != service.roles[content.RoleTerminal] {
			terminals[string(entity.ID)] = true
		}
	}
	world.objects.interactions = slices.DeleteFunc(world.objects.interactions, func(target InteractionTarget) bool {
		return terminals[target.ID]
	})
	// The receiving pad and terminal share an edge with the trolley. Give the
	// terminal a one-pixel approach side so the trolley's push prompt cannot
	// mask the required hold when the trolley is parked on the pad.
	if deed.Family == "SUP" {
		for i := range world.objects.interactions {
			if world.objects.interactions[i].ID == string(service.roles[content.RoleTerminal]) {
				world.objects.interactions[i].Bounds.X++
			}
		}
	}
	// Inactive siblings remain represented in the restored entity inventory,
	// but cannot take the active adult's prompt or absorb its projectiles.
	world.combat.civilians = slices.DeleteFunc(world.combat.civilians, func(civilian Civilian) bool {
		for _, entity := range world.state.Entities {
			if string(entity.ID) == civilian.ID && entity.Role == content.RoleActor {
				return entity.Ticket != attempt.Deed
			}
		}
		return false
	})
	return nil
}

// ServiceView reports current evidence and an actionable next objective.
func (world *World) ServiceView(ctx context.Context) ServiceView {
	defer mon.Task()(&ctx)(nil)
	if world == nil || world.service.attempt.Deed == "" {
		return ServiceView{}
	}
	s := copyService(world.service)
	status := s.status
	if status == ServiceActive && world.serviceReady() {
		status = ServiceEvidenceReady
	}
	objective := world.serviceObjective()
	failure := ""
	if status == ServiceFailed {
		failure = s.deed.Failure
	}
	invalid := s.invalid
	if invalid == "" {
		invalid = world.objects.invalid
	}
	return ServiceView{Deed: s.attempt.Deed, Attempt: s.attempt.Number, Family: s.deed.Family, Variant: s.deed.Variant, Title: s.deed.Title, Status: status, Objective: objective, Failure: failure, Invalid: invalid, RecorderStarted: s.recorder, CameraInterrupted: s.camera, CameraFouled: s.fouled, PaddleLatched: s.paddle, BypassClosed: s.bypass, FuseTripped: s.fuseTripped, SealBroken: s.sealBroken, AdultFollowing: s.following, AdultWaiting: s.waiting, AdultInRefuge: s.refuge, Receipt: s.lastReceipt}
}

// ServiceTicketView combines authored request information with committed state.
// Missing and Receipt are detached from the supplied snapshot.
type ServiceTicketView struct {
	Service ServiceView
	Missing []string
	Receipt *campaign.Receipt
}

// ServiceTicket reports an authored ticket without accepting it or creating a
// receipt. Callers pass only the last durably acknowledged snapshot.
func (world *World) ServiceTicket(ctx context.Context, owner *campaign.Campaign, manifest *content.Campaign, committed campaign.Snapshot, id content.DeedID) (_ ServiceTicketView, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || owner == nil || manifest == nil {
		return ServiceTicketView{}, fmt.Errorf("missing service catalogue")
	}
	if err = owner.Validate(ctx, committed); err != nil {
		return ServiceTicketView{}, err
	}
	var deed content.CampaignDeed
	for _, item := range manifest.Deeds(ctx) {
		if item.Deed.ID == id && item.Family != "END" {
			deed = item
			break
		}
	}
	if deed.Deed.ID == "" {
		return ServiceTicketView{}, fmt.Errorf("unknown service %s", id)
	}
	result := ServiceTicketView{Service: ServiceView{Deed: id, Family: deed.Family, Variant: deed.Variant, Title: deed.Title, Objective: deed.Evidence}}
	for _, receipt := range committed.Data(ctx).Receipts {
		if receipt.Certification.Deed == id {
			result.Service.Status = ServiceCertified
			result.Service.Attempt = receipt.Certification.Attempt
			result.Receipt = &receipt
			return result, nil
		}
	}
	attempt, active := committed.ActiveAttempt()
	if active && attempt.Deed == id {
		result.Service.Status, result.Service.Attempt = ServiceActive, attempt.Number
		if world.service.attempt.Deed == id && world.service.attempt.Number == attempt.Number {
			result.Service = world.ServiceView(ctx)
		}
		return result, nil
	}
	result.Missing, err = owner.MissingDeed(ctx, committed, id)
	if err != nil {
		return ServiceTicketView{}, err
	}
	if len(result.Missing) != 0 || active {
		result.Service.Status = ServiceBlocked
		if active {
			result.Missing = append(result.Missing, "active:"+string(attempt.Deed))
		}
	} else {
		result.Service.Status = ServiceAvailable
	}
	return result, nil
}

func (world *World) serviceReady() bool {
	s := &world.service
	if s.status != ServiceActive {
		return false
	}
	switch s.deed.Family {
	case "SUP":
		return world.supplyReady()
	case "RES":
		return s.refuge
	case "UTL":
		return s.paddle && s.bypass
	case "PRV":
		return s.recorder && s.camera
	}
	return false
}

func (world *World) serviceObjective() string {
	s := &world.service
	if s.status == ServiceFailed {
		return "Reset Request: " + s.deed.Failure
	}
	if s.status == ServicePending {
		return "saving certification"
	}
	if s.status == ServiceCertified {
		return "show the committed receipt"
	}
	if world.serviceReady() {
		return "hold certification terminal"
	}
	switch s.deed.Family {
	case "SUP":
		return "push the sealed trolley to the receiving pad"
	case "RES":
		if !world.rescueGateLatched() {
			return "weight the plate and latch the gate"
		}
		if !s.following {
			return "ask the named adult to follow"
		}
		if s.waiting {
			return "clear the hazard and release the waiting adult"
		}
		return "guide the named adult to refuge"
	case "UTL":
		if !s.paddle {
			return "throw a limb into the marked paddle"
		}
		return "close the bypass switch"
	case "PRV":
		if !s.recorder {
			return "start the evidence recorder"
		}
		return "hit the marked camera with feces"
	}
	return ""
}

func (world *World) servicePrompts() {
	s := &world.service
	for i := range world.objects.interactions {
		target := &world.objects.interactions[i]
		if s.deed.Family == "RES" && target.ID == string(s.roles[content.RoleLatch]) {
			target.Reason = ""
			if !world.rescueWeighted() {
				target.Reason = "requires_crate_or_settled_limb"
			}
		}
		if target.ID != string(s.roles[content.RoleTerminal]) {
			continue
		}
		target.Prerequisite = world.serviceObjective()
		target.Reason = ""
		if s.status == ServiceFailed {
			target.Reason = "reset_failed_request"
		} else if s.status != ServiceCertified && !world.serviceReady() {
			target.Reason = "service_evidence_incomplete"
		}
	}
}

// ServiceCommand combines the ordinary object tick with the current authored
// hazard states. Hazard execution supplies one flag per station, in bay order.
// The crusher flag is meaningful only when its authored volume is occupied.
type ServiceCommand struct {
	Objects       ObjectCommand
	CrusherActive bool
	Hazards       []bool
}

// StepServices advances the actual world and collects only completed actions.
// A pending certification or failure must be resolved through storage before
// another tick; restoring a world clears transient work instead.
func (world *World) StepServices(ctx context.Context, command ServiceCommand) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || world.grid == nil {
		return fmt.Errorf("invalid service world")
	}
	if world.service.pending != nil || world.service.failure || world.service.attemptChange != nil || world.service.prepared != nil || world.feces.pending != nil {
		return fmt.Errorf("service transition awaiting commit or restoration")
	}
	if len(command.Hazards) != 0 && len(command.Hazards) != len(world.service.hazards) {
		return fmt.Errorf("invalid hazard state count")
	}
	before := copyService(world.service)
	objects := cloneObjects(world.objects)
	state, previous, motion := cloneState(world.state), cloneState(world.previous), world.motion
	combat, limbs, feces := cloneCombat(world.combat), cloneLimbs(world.limbs), cloneFeces(world.feces)
	defer func() {
		if err != nil {
			world.service, world.objects = before, objects
			world.state, world.previous, world.motion = state, previous, motion
			world.combat, world.limbs, world.feces = combat, limbs, feces
		}
	}()
	world.service.invalid = ""
	world.servicePrompts()
	player := &command.Objects.Feces.Limb.Combat.Player
	catwalkPush := world.trolleyCatwalkPush(player.Move)
	if catwalkPush {
		run, _, _ := legSpeeds(world.motion.limbs)
		if run > 100 {
			player.Move *= 100 / run
		}
		world.state.Player.Velocity.X = math.Max(-100, math.Min(100, world.state.Player.Velocity.X))
	}
	if err = world.StepObjects(ctx, command.Objects); err != nil {
		return err
	}
	if catwalkPush && world.motion.action == FreeAction && world.state.Player.Support == state.Player.Support && world.state.Player.Bounds.Y == state.Player.Bounds.Y {
		if err = world.moveCatwalkTrolley(ctx, world.state.Player.Bounds.X-state.Player.Bounds.X); err != nil {
			return err
		}
	}
	if world.service.attempt.Deed == "" || (world.service.status != ServiceActive && world.service.status != ServiceCertified) {
		return nil
	}
	world.serviceObjects(ctx, command)
	world.servicePrompts()
	return nil
}

func (world *World) serviceObjects(ctx context.Context, command ServiceCommand) {
	defer mon.Task()(&ctx)(nil)
	s := &world.service
	// Channel completions precede projectile impacts and lethal resolution.
	for _, event := range world.objects.events {
		if event.Kind == "completed" {
			world.serviceInteraction(ctx, event.Target, command.Hazards)
		}
		if s.deed.Family == "UTL" && event.Kind == "paddle_latched" && event.Target == string(s.roles[content.RolePaddle]) {
			s.paddle = true
		}
	}
	if s.status == ServiceActive && s.deed.Family == "PRV" {
		world.privacyImpacts()
	}
	if s.status == ServiceActive && s.deed.Family == "SUP" && command.CrusherActive {
		world.supplyCrusher()
	}
	if s.status == ServiceActive && s.deed.Family == "RES" {
		world.advanceRescue(command.Hazards)
	}
}

func (world *World) serviceInteraction(ctx context.Context, target string, hazards []bool) {
	defer mon.Task()(&ctx)(nil)
	s := &world.service
	if s.status == ServiceCertified && target == string(s.roles[content.RoleTerminal]) {
		for _, receipt := range s.committed.Data(ctx).Receipts {
			if receipt.Certification.Deed == s.attempt.Deed {
				s.lastReceipt = &receipt
				return
			}
		}
	}
	if s.status != ServiceActive {
		return
	}
	if target == string(s.roles[content.RoleTerminal]) {
		if !world.serviceReady() {
			s.invalid = "service_evidence_incomplete"
			return
		}
		s.pending = &campaign.CertificationCandidate{Completed: true, Event: campaign.Certification{Deed: s.attempt.Deed, Attempt: s.attempt.Number, Evidence: s.deed.Evidence, EvidenceObjects: slices.Clone(s.ticketObjects), Trigger: s.deed.Trigger, Terminal: s.roles[content.RoleTerminal], HoldMilliseconds: s.deed.Deed.HoldMilliseconds}}
		s.status = ServicePending
		return
	}
	switch s.deed.Family {
	case "UTL":
		world.utilityInteraction(target)
	case "PRV":
		if target == string(s.roles[content.RoleRecorder]) {
			s.recorder = true
		}
	case "RES":
		world.rescueInteraction(target, hazards)
	}
}

func (world *World) failService() {
	world.service.status = ServiceFailed
	world.service.failure = true
}

// PrepareService asks campaign to prepare the pending certification or failure.
// The proposal is not a receipt and must be committed before publication. The
// caller supplies the stable operation identity and any same-tick death/contact.
func (world *World) PrepareService(ctx context.Context, owner *campaign.Campaign, committed campaign.Snapshot, transition campaign.Transition) (_ campaign.Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || owner == nil || (world.service.pending == nil && !world.service.failure && world.service.attemptChange == nil) || transition.Certification != nil || transition.Failure || transition.Attempt != nil {
		return campaign.Snapshot{}, fmt.Errorf("no pending service transition")
	}
	if !reflect.DeepEqual(committed.Data(ctx), world.service.committed.Data(ctx)) && (world.service.prepared == nil || !reflect.DeepEqual(committed.Data(ctx), world.service.prepared.Data(ctx))) {
		return campaign.Snapshot{}, fmt.Errorf("service transition uses a different committed snapshot")
	}
	if world.service.pending != nil {
		copy := copyService(world.service)
		transition.Certification = copy.pending
	} else if world.service.failure {
		transition.Failure = true
	} else {
		change := *world.service.attemptChange
		transition.Attempt = &change
	}
	if world.service.attemptChange == nil && world.feces.pending != nil {
		contact := world.feces.pending
		if contact.Attempt.Deed != world.service.attempt.Deed || contact.Attempt.Number != world.service.attempt.Number {
			return campaign.Snapshot{}, fmt.Errorf("waste contact belongs to another attempt")
		}
		if !slices.Contains(transition.Contacts, contact.Contact) {
			transition.Contacts = append(slices.Clone(transition.Contacts), contact.Contact)
		}
	}
	data := committed.Data(ctx)
	if world.service.attemptChange == nil && (data.LatestOperation == nil || transition.Sequence != data.OperationSequence) {
		attempt, ok := committed.ActiveAttempt()
		if !ok || attempt.Deed != world.service.attempt.Deed || attempt.Number != world.service.attempt.Number {
			return campaign.Snapshot{}, fmt.Errorf("service attempt changed")
		}
	}
	proposed, err := owner.Prepare(ctx, committed, transition)
	if err != nil {
		return campaign.Snapshot{}, err
	}
	if world.service.prepared != nil && !reflect.DeepEqual(world.service.prepared.Data(ctx), proposed.Data(ctx)) {
		return campaign.Snapshot{}, fmt.Errorf("service proposal changed")
	}
	world.service.prepared = &proposed
	return proposed, nil
}

// PrepareServiceWaste prepares a reporting refill independently when no
// certification or failure completed in the same tick. The same transition
// must be retried after a failed or ambiguous write.
func (world *World) PrepareServiceWaste(ctx context.Context, owner *campaign.Campaign, committed campaign.Snapshot, transition campaign.Transition) (_ campaign.Snapshot, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || owner == nil || world.feces.pending == nil || world.service.pending != nil || world.service.failure || world.service.attemptChange != nil || transition.Attempt != nil || transition.Certification != nil || transition.Failure {
		return campaign.Snapshot{}, fmt.Errorf("no independent service waste transition")
	}
	if !reflect.DeepEqual(committed.Data(ctx), world.service.committed.Data(ctx)) && (world.service.prepared == nil || !reflect.DeepEqual(committed.Data(ctx), world.service.prepared.Data(ctx))) {
		return campaign.Snapshot{}, fmt.Errorf("service waste uses a different committed snapshot")
	}
	proposal, err := world.PrepareWaste(ctx, owner, committed, transition)
	if err != nil {
		return campaign.Snapshot{}, err
	}
	if world.service.prepared != nil && !reflect.DeepEqual(world.service.prepared.Data(ctx), proposal.Data(ctx)) {
		return campaign.Snapshot{}, fmt.Errorf("service proposal changed")
	}
	world.service.prepared = &proposal
	return proposal, nil
}

// RequestService selects a ticket after a completed protected console hold.
// A certified ticket returns its committed receipt without creating an event.
// Other selections freeze simulation until PrepareService and acknowledgment.
func (world *World) RequestService(ctx context.Context, owner *campaign.Campaign, manifest *content.Campaign, committed campaign.Snapshot, action campaign.AttemptAction, id content.DeedID) (_ *campaign.Receipt, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || owner == nil || manifest == nil || world.service.pending != nil || world.service.failure || world.service.attemptChange != nil || world.feces.pending != nil {
		return nil, fmt.Errorf("service request unavailable")
	}
	if err = owner.Validate(ctx, committed); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(world.service.committed.Data(ctx), committed.Data(ctx)) {
		return nil, fmt.Errorf("service request uses a different committed snapshot")
	}
	console := false
	for _, event := range world.objects.events {
		if event.Kind != "completed" {
			continue
		}
		for _, entity := range world.state.Entities {
			if string(entity.ID) == event.Target && entity.Protected && (entity.Role == content.RoleConsole || entity.Role == content.RoleTerminal) {
				console = true
			}
		}
	}
	if !console {
		return nil, fmt.Errorf("hold a protected service console")
	}
	var selected content.CampaignDeed
	for _, deed := range manifest.Deeds(ctx) {
		if deed.Deed.ID == id && deed.Family != "END" {
			selected = deed
			break
		}
	}
	if selected.Deed.ID == "" {
		return nil, fmt.Errorf("unknown service ticket %s", id)
	}
	if selected.Deed.Encounter != world.state.Encounter {
		missing, missingErr := owner.MissingDeed(ctx, committed, id)
		if missingErr != nil {
			return nil, missingErr
		}
		if len(missing) != 0 {
			return nil, fmt.Errorf("%w: missing %v", campaign.ErrBlocked, missing)
		}
		return nil, fmt.Errorf("%w: ticket is not in this encounter", campaign.ErrBlocked)
	}
	if action == campaign.AttemptAccept {
		for _, receipt := range committed.Data(ctx).Receipts {
			if receipt.Certification.Deed == id {
				world.service.lastReceipt = &receipt
				result := receipt
				result.Certification.EvidenceObjects = slices.Clone(receipt.Certification.EvidenceObjects)
				return &result, nil
			}
		}
		if active, ok := committed.ActiveAttempt(); ok && active.Deed == id {
			return nil, nil
		}
		_, err = owner.Accept(ctx, committed, id)
	} else if action == campaign.AttemptReset || action == campaign.AttemptAbandon {
		attempt, ok := committed.ActiveAttempt()
		if !ok || attempt.Deed != id {
			return nil, fmt.Errorf("wrong active request")
		}
		if action == campaign.AttemptReset {
			_, err = owner.Reset(ctx, committed)
		} else {
			_, err = owner.Abandon(ctx, committed)
		}
	} else {
		return nil, fmt.Errorf("invalid request action")
	}
	if err != nil {
		return nil, err
	}
	world.service.attemptChange = &campaign.AttemptChange{Action: action, Deed: id}
	return nil, nil
}

// ServiceAcknowledgment is supplied by the storage owner after it resolves a
// commit. Failed or ambiguous results have Succeeded false and publish nothing.
type ServiceAcknowledgment struct {
	Succeeded  bool
	CampaignID string
	Sequence   uint64
	Snapshot   campaign.Snapshot
}

// AcknowledgeService applies only the exact successfully saved proposal.
// Attempt changes rebuild the local bay while preserving the living player.
func (world *World) AcknowledgeService(ctx context.Context, owner *campaign.Campaign, manifest *content.Campaign, bays []content.ServiceBay, result ServiceAcknowledgment) (err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || owner == nil || world.service.prepared == nil {
		return fmt.Errorf("no prepared service transition")
	}
	if !result.Succeeded {
		return fmt.Errorf("service save has not succeeded")
	}
	committed := result.Snapshot
	data := committed.Data(ctx)
	if result.CampaignID == "" || result.CampaignID != data.CampaignID || result.Sequence == 0 || result.Sequence != data.OperationSequence {
		return fmt.Errorf("service acknowledgment identity mismatch")
	}
	if err = owner.Validate(ctx, committed); err != nil {
		return err
	}
	if !reflect.DeepEqual(world.service.prepared.Data(ctx), committed.Data(ctx)) {
		return fmt.Errorf("acknowledged service result differs from proposal")
	}
	if world.service.attemptChange != nil {
		return world.acknowledgeAttempt(ctx, owner, manifest, bays, committed)
	}
	plan, err := owner.RestorePlan(ctx, committed)
	if err != nil {
		return err
	}
	world.state.Anchor = plan.Anchor
	world.state.Routes = slices.Clone(plan.Routes)
	world.state.ItemRoutes = slices.Clone(plan.ItemRoutes)
	world.state.OwnedItems = slices.Clone(plan.OwnedItems)
	world.state.Installed = slices.Clone(plan.Installed)
	world.state.Defeated = slices.Clone(plan.Defeated)
	world.state.Receipts = nil
	for _, receipt := range plan.Receipts {
		world.state.Receipts = append(world.state.Receipts, receipt.Certification.Deed)
	}
	slices.Sort(world.state.Receipts)
	if slices.Contains(plan.Defeated, world.state.Encounter) {
		world.state.BossActive = false
	}
	for _, emergency := range plan.Emergencies {
		found := false
		for i := range world.state.Emergencies {
			existing := &world.state.Emergencies[i]
			if existing.ID == emergency.Emergency.ID {
				if existing.Stage != emergency.Emergency.Stage {
					existing.Stage = emergency.Emergency.Stage
					existing.WarningTicks = 0
				}
				found = true
				break
			}
		}
		if !found {
			warning := 0
			if emergency.Emergency.Stage == campaign.EmergencyActive {
				warning = 180
			}
			world.state.Emergencies = append(world.state.Emergencies, EmergencyState{ID: emergency.Emergency.ID, Encounter: emergency.Encounter, Stage: emergency.Emergency.Stage, WarningTicks: warning, SafeBoundary: true, BypassUsable: true})
		}
	}
	slices.SortFunc(world.state.Emergencies, func(a, b EmergencyState) int { return compareID(string(a.ID), string(b.ID)) })
	if world.service.pending != nil {
		id := world.service.attempt.Deed
		world.state.ActiveTicket = ""
		world.state.Objective = ""
		world.feces.attempt = campaign.Attempt{}
		world.service.status = ServiceCertified
		for _, receipt := range committed.Data(ctx).Receipts {
			if receipt.Certification.Deed == id {
				world.service.lastReceipt = &receipt
				break
			}
		}
	}
	if world.feces.pending != nil {
		pending := world.feces.pending
		for _, incident := range committed.Data(ctx).Incidents {
			if incident.Kind == campaign.IncidentWaste && incident.Deed == pending.Attempt.Deed && incident.Attempt == pending.Attempt.Number {
				world.feces.pending = nil
				break
			}
		}
	}
	world.service.pending = nil
	world.service.failure = false
	world.service.prepared = nil
	world.service.committed = committed
	return nil
}

func (world *World) acknowledgeAttempt(ctx context.Context, owner *campaign.Campaign, manifest *content.Campaign, bays []content.ServiceBay, committed campaign.Snapshot) (err error) {
	defer mon.Task()(&ctx)(&err)
	var platforms []Platform
	for _, platform := range world.state.Platforms {
		if !world.isObjectPlatform(platform.Collider.ID) {
			platforms = append(platforms, platform)
		}
	}
	fresh, err := NewWorld(ctx, owner, manifest, committed, bays, platforms)
	if err != nil {
		return err
	}
	if fresh.state.Encounter != world.state.Encounter {
		return fmt.Errorf("request does not belong to the current encounter")
	}
	// Live attacks belong to the old attempt. Spent ammunition stays spent;
	// detached limbs remain recoverable at the safe vestibule, but cannot
	// continue an old damaging flight or hold a freshly reset plate.
	safe := Vector{X: centerX(fresh.state.Player.Bounds), Y: fresh.state.Player.Bounds.Y}
	fresh.state.Player = world.state.Player
	fresh.state.Health, fresh.state.Limbs, fresh.state.Feces = world.state.Health, world.state.Limbs, world.state.Feces
	fresh.state.Tick, fresh.state.Mode = world.state.Tick, world.state.Mode
	fresh.motion = world.motion
	fresh.motion.buffer = ""
	if fresh.motion.action == AttackAction {
		fresh.motion.action = FreeAction
		fresh.motion.actionUntil = 0
	}
	fresh.limbs = cloneLimbs(world.limbs)
	fresh.limbs.throw = limbThrow{}
	fresh.limbs.throwPending = ""
	for i := range fresh.limbs.objects {
		limb := &fresh.limbs.objects[i]
		if limb.Carried {
			continue
		}
		retire := limb.Airborne || limb.Damaging
		for _, object := range fresh.objects.items {
			if object.Kind == ObjectPlate && limb.Position.X >= object.Bounds.X && limb.Position.X <= object.Bounds.X+object.Bounds.Width && limb.Position.Y >= object.Bounds.Y && limb.Position.Y <= object.Bounds.Y+object.Bounds.Height {
				retire = true
			}
		}
		if retire {
			limb.Position, limb.Velocity = safe, Vector{}
			limb.Airborne, limb.Damaging = false, false
			limb.ignored = ""
		}
	}
	fresh.feces.smears = slices.Clone(world.feces.smears)
	fresh.feces.pickups = slices.Clone(world.feces.pickups)
	for i := range fresh.state.Emergencies {
		for _, existing := range world.state.Emergencies {
			if existing.ID == fresh.state.Emergencies[i].ID {
				fresh.state.Emergencies[i] = existing
			}
		}
	}
	// The request terminal is protected. Do not materialize reset props on a
	// player who reached a terminal from the working area.
	for i := range fresh.objects.items {
		object := &fresh.objects.items[i]
		if objectBlocks(*object) && overlaps(object.Bounds, fresh.state.Player.Bounds) {
			object.Restoring = true
		}
	}
	fresh.syncObjectPlatforms()
	fresh.previous = cloneState(fresh.state)
	*world = *fresh
	return nil
}

// ServiceReceipt returns an existing receipt only from the supplied validated
// committed snapshot. It never creates a certification or reward.
func (world *World) ServiceReceipt(ctx context.Context, owner *campaign.Campaign, committed campaign.Snapshot, id content.DeedID) (_ *campaign.Receipt, err error) {
	defer mon.Task()(&ctx)(&err)
	if world == nil || owner == nil {
		return nil, fmt.Errorf("missing campaign")
	}
	if err = owner.Validate(ctx, committed); err != nil {
		return nil, err
	}
	for _, receipt := range committed.Data(ctx).Receipts {
		if receipt.Certification.Deed == id {
			return &receipt, nil
		}
	}
	return nil, nil
}

// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"strconv"
	"strings"

	"too-broke-to-die/internal/tbtd/campaign"
	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
	"too-broke-to-die/internal/tbtd/settings"
)

func initialObjective(deed content.CampaignDeed) string {
	switch deed.Family {
	case "SUP":
		return "TXT-ACT-SUP-PAD"
	case "RES":
		return "TXT-ACT-RES-GATE"
	case "UTL":
		return "TXT-ACT-UTL-PADDLE"
	case "PRV":
		return "TXT-ACT-PRV-RECORDER"
	case "END":
		return "TXT-ACT-END-" + strings.TrimPrefix(string(deed.Location), "LOC-")
	default:
		return ""
	}
}

func (ui *Interface) trackedObjective(ctx context.Context, data session.FrameViewData) (_ string, _ string, err error) {
	defer mon.Task()(&ctx)(&err)
	id := ui.tracked
	if id == "" {
		id = data.Tracked
	}
	for _, deed := range ui.campaign.Deeds(ctx) {
		if deed.Deed.ID != id {
			continue
		}
		for _, receipt := range data.Progress.Receipts {
			if receipt.Deed == id {
				text, err := ui.text(ctx, "TXT-EVD-EXISTING", nil)
				return deed.Title, text, err
			}
		}
		objective := initialObjective(deed)
		if id == data.Tracked && data.ObjectiveID != "" {
			objective = string(data.ObjectiveID)
		}
		if objective == "" {
			return "", "", content.ErrInvalid.New("missing objective for %s", id)
		}
		text, err := ui.text(ctx, objective, nil)
		return deed.Title, text, err
	}
	if id != "" {
		return "", "", content.ErrInvalid.New("unknown tracked service %s", id)
	}
	return "", "", nil
}

func (ui *Interface) ledger(ctx context.Context, screen *Screen, data session.FrameViewData) (err error) {
	defer mon.Task()(&ctx)(&err)
	locations := []content.LocationID{"LOC-HOS", "LOC-CHU", "LOC-MIN", "LOC-INS"}
	location := locations[ui.institution]
	tab := []string{"TXT-LED-SERVICES", "TXT-LED-CONSEQUENCES", "TXT-LED-TUTORIALS"}[ui.ledgerTab]
	label, err := ui.text(ctx, tab, nil)
	if err != nil {
		return err
	}
	screen.Lines = append(screen.Lines, label)
	name, err := ui.text(ctx, "TXT-SIGN-"+strings.TrimPrefix(string(location), "LOC-"), nil)
	if err != nil {
		return err
	}
	screen.Lines = append(screen.Lines, name)
	tabItem, err := ui.item(ctx, tab, "ui_tab", "")
	if err != nil {
		return err
	}
	screen.Items = append(screen.Items, tabItem, Item{Label: name, Action: "ui_institution"})
	deeds := ui.campaign.Deeds(ctx)
	if ui.ledgerTab == 0 {
		if len(data.Tickets) != 0 {
			counts := map[string]int{"SUP": 0, "RES": 0, "UTL": 0, "PRV": 0}
			closure := "TXT-LED-AVAILABLE"
			for _, deed := range deeds {
				if deed.Location != location {
					continue
				}
				for _, receipt := range data.Progress.Receipts {
					if receipt.Deed == deed.Deed.ID {
						if deed.Family == "END" {
							closure = "TXT-LED-CERTIFIED"
						} else {
							counts[deed.Family]++
						}
					}
				}
				if deed.Family == "END" && closure != "TXT-LED-CERTIFIED" {
					for _, ticket := range data.Tickets {
						if ticket.Deed == deed.Deed.ID && ticket.Status == "blocked" {
							closure = "TXT-HUD-BLOCKED"
						}
					}
				}
			}
			closureLabel := "Недоступно"
			if closure != "TXT-HUD-BLOCKED" {
				closureLabel, err = ui.text(ctx, closure, nil)
				if err != nil {
					return err
				}
			}
			batch, batchErr := ui.text(ctx, "TXT-LED-BATCH", map[string]string{"supply": strconv.Itoa(counts["SUP"]), "rescue": strconv.Itoa(counts["RES"]), "utility": strconv.Itoa(counts["UTL"]), "privacy": strconv.Itoa(counts["PRV"]), "closure": closureLabel})
			if batchErr != nil {
				return batchErr
			}
			screen.Lines = append(screen.Lines, batch)
		}
		title, objective, objectiveErr := ui.trackedObjective(ctx, data)
		if objectiveErr != nil {
			return objectiveErr
		}
		if title != "" {
			text, textErr := ui.text(ctx, "TXT-HUD-OBJECTIVE", map[string]string{"service": title, "action": objective})
			if textErr != nil {
				return textErr
			}
			screen.Lines = append(screen.Lines, text)
		}
		for _, deed := range deeds {
			if deed.Location != location {
				continue
			}
			row := deed.Title
			for _, ticket := range data.Tickets {
				if ticket.Deed != deed.Deed.ID {
					continue
				}
				statusID := map[string]string{"available": "TXT-LED-AVAILABLE", "active": "TXT-LED-ACTIVE", "evidence_ready": "TXT-LED-READY", "attempt_failed": "TXT-LED-FAILED", "pending": "TXT-SAVE-PENDING"}[ticket.Status]
				if statusID != "" {
					status, statusErr := ui.text(ctx, statusID, nil)
					if statusErr != nil {
						return statusErr
					}
					row += " — " + status
				}
				if ticket.Status == "blocked" {
					var reasons []string
					for _, reason := range ticket.Missing {
						reasons = append(reasons, ui.eventName(ctx, strings.TrimPrefix(reason, "active:")))
					}
					if len(reasons) == 0 {
						reasons = append(reasons, "условия услуги")
					}
					message, reasonErr := ui.text(ctx, "TXT-HUD-BLOCKED", map[string]string{"reason": strings.Join(reasons, "; ")})
					if reasonErr != nil {
						return reasonErr
					}
					row += " — " + message
				}
				if ticket.FailureID != "" {
					reason, reasonErr := ui.text(ctx, string(ticket.FailureID), nil)
					if reasonErr != nil {
						return reasonErr
					}
					message, reasonErr := ui.text(ctx, "TXT-EVD-FAILED", map[string]string{"reason": reason})
					if reasonErr != nil {
						return reasonErr
					}
					row += " — " + message
				}
			}
			for _, receipt := range data.Progress.Receipts {
				if receipt.Deed != deed.Deed.ID {
					continue
				}
				status, textErr := ui.text(ctx, "TXT-LED-CERTIFIED", nil)
				if textErr != nil {
					return textErr
				}
				row += " — " + status
				if ui.tracked == receipt.Deed {
					evidenceID := "TXT-EVIDENCE-" + deed.Family
					if deed.Family == "END" {
						evidenceID = initialObjective(deed)
					}
					evidence, evidenceErr := ui.text(ctx, evidenceID, nil)
					if evidenceErr != nil {
						return evidenceErr
					}
					detail, detailErr := ui.text(ctx, "TXT-LED-RECEIPT", map[string]string{"service": receipt.Title, "institution": receipt.InstitutionName, "evidence": evidence, "count": strconv.Itoa(receipt.Ordinal)})
					if detailErr != nil {
						return detailErr
					}
					screen.Lines = append(screen.Lines, detail)
				}
			}
			if len(data.Tickets) == 0 && deed.Deed.ID == data.Tracked {
				statusID := map[string]string{"active": "TXT-LED-ACTIVE", "evidence_ready": "TXT-LED-READY", "attempt_failed": "TXT-LED-FAILED"}[data.ServiceStatus]
				if statusID != "" {
					status, textErr := ui.text(ctx, statusID, nil)
					if textErr != nil {
						return textErr
					}
					row += " — " + status
				}
			}
			screen.Items = append(screen.Items, Item{Label: row, Action: "ui_track", Target: string(deed.Deed.ID)})
		}
	} else if ui.ledgerTab == 1 {
		for _, deed := range deeds {
			if deed.Location != location {
				continue
			}
			for _, consequence := range data.Progress.Consequences {
				if consequence != deed.Deed.Consequence {
					continue
				}
				id := "TXT-AFTER-" + strings.TrimPrefix(string(location), "LOC-") + "-" + deed.Family
				var fields map[string]string
				if deed.Family != "END" {
					fields = map[string]string{"service": deed.Title}
				}
				text, textErr := ui.text(ctx, id, fields)
				if textErr != nil {
					return textErr
				}
				screen.Items = append(screen.Items, Item{Label: text, Action: "ui_noop"})
			}
		}
		for _, emergency := range data.Progress.Emergencies {
			if !strings.HasPrefix(string(emergency.ID), "EM-"+strings.TrimPrefix(string(location), "LOC-")+"-") {
				continue
			}
			id := "TXT-LED-EMERGENCY"
			if emergency.Stage == campaign.EmergencyAftermath {
				id = "TXT-LED-AFTERMATH"
			} else if emergency.ID == data.Emergency && data.EmergencyWarning {
				id = "TXT-LED-WARNING"
			}
			status, textErr := ui.text(ctx, id, nil)
			if textErr != nil {
				return textErr
			}
			danger, textErr := ui.text(ctx, "TXT-DANGER-"+strings.TrimPrefix(string(emergency.ID), "EM-"), nil)
			if textErr != nil {
				return textErr
			}
			screen.Items = append(screen.Items, Item{Label: status + ": " + danger, Action: "ui_noop"})
		}
	} else {
		for _, tutorial := range []struct {
			id     string
			fields map[string][]settings.Action
		}{
			{"TUT-MOV", map[string][]settings.Action{"move": {settings.MoveLeft, settings.MoveRight}, "jump": {settings.Jump}, "down": {settings.Down}}},
			{"TUT-INT", map[string][]settings.Action{"interact": {settings.Interact}}},
			{"TUT-LED", map[string][]settings.Action{"ledger": {settings.Ledger}}},
			{"TUT-CBT", map[string][]settings.Action{"melee": {settings.Melee}, "evade": {settings.Evade}}},
			{"TUT-CRD", map[string][]settings.Action{"interact": {settings.Interact}}},
			{"TUT-LIM", map[string][]settings.Action{"select": {settings.SelectLeftArm, settings.SelectRightArm, settings.SelectLeftLeg, settings.SelectRightLeg}, "remove": {settings.RemoveLimb}, "interact": {settings.Interact}, "regenerate": {settings.Regenerate}}},
			{"TUT-LIM-PLATE", map[string][]settings.Action{"interact": {settings.Interact}}},
			{"TUT-UTL", map[string][]settings.Action{"aim": {settings.Aim}, "throw": {settings.Throw}, "interact": {settings.Interact}}},
			{"TUT-FEC", map[string][]settings.Action{"select": {settings.SelectPayload}, "aim": {settings.Aim}, "throw": {settings.Throw}, "interact": {settings.Interact}}},
			{"TUT-FEC-PRACTICE", nil}, {"TUT-REC", nil}, {"TUT-REC-DEATH", nil}, {"TUT-BOSS", nil}, {"TUT-SOURCE", nil},
		} {
			values := make(map[string]string, len(tutorial.fields))
			for name, actions := range tutorial.fields {
				var labels []string
				for _, action := range actions {
					tokens := ui.sampler.Tokens(ctx, action, ui.sampler.ActiveDevice())
					if len(tokens) == 0 {
						return content.ErrInvalid.New("%s: missing binding for %s", tutorial.id, action)
					}
					for _, token := range tokens {
						labels = append(labels, token.Label)
					}
				}
				values[name] = strings.Join(labels, " / ")
			}
			text, textErr := ui.text(ctx, tutorial.id, values)
			if textErr != nil {
				return textErr
			}
			screen.Items = append(screen.Items, Item{Label: text, Action: "ui_noop", Target: tutorial.id})
		}
	}
	return nil
}

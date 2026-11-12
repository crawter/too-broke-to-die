// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"too-broke-to-die/internal/tbtd/content"
)

func (world *World) privacyImpacts() {
	service := &world.service
	for _, impact := range world.feces.impacts {
		if impact.Kind != "receptor" {
			continue
		}
		if impact.Target != string(service.roles[content.RoleCamera]) || impact.Ticket != service.attempt.Deed {
			service.invalid = "invalid_camera_target"
			continue
		}
		if !service.recorder {
			service.fouled = true
			world.failService()
			return
		}
		service.camera = true
	}
}

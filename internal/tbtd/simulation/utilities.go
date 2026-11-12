// Copyright (C) 2026 crawter
// All rights reserved.

package simulation

import (
	"too-broke-to-die/internal/tbtd/content"
)

func (world *World) utilityInteraction(target string) {
	service := &world.service
	if target != string(service.roles[content.RoleSwitch]) {
		return
	}
	if !service.paddle {
		service.fuseTripped = true
		world.failService()
		return
	}
	service.bypass = true
}

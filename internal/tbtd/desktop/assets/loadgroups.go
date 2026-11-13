// Copyright (C) 2026 crawter
// All rights reserved.

package assets

import (
	"context"

	"too-broke-to-die/internal/tbtd/content"
)

// LoadLocation replaces the current location while retaining the shared set.
// Transit stops are authored as part of their corresponding location group.
// Use "ending" for the authored ending group and "shared" for the title.
func (cache *Cache) LoadLocation(ctx context.Context, location string) (err error) {
	defer mon.Task()(&ctx)(&err)
	var group string
	switch location {
	case "LOC-HOS":
		group = "location-hos"
	case "LOC-CHU":
		group = "location-chu"
	case "LOC-MIN":
		group = "location-min"
	case "LOC-INS":
		group = "location-ins"
	case "ending", "shared":
		group = location
	default:
		return content.ErrInvalid.New("%s: unknown presentation load group", location)
	}
	return cache.replaceGroup(ctx, group, false)
}

// LoadTransit retains the shared set and only the authored transit exports of
// the named institution; the previous working location is retired on success.
func (cache *Cache) LoadTransit(ctx context.Context, location string) (err error) {
	defer mon.Task()(&ctx)(&err)
	var group string
	switch location {
	case "LOC-HOS":
		group = "location-hos"
	case "LOC-CHU":
		group = "location-chu"
	case "LOC-MIN":
		group = "location-min"
	case "LOC-INS":
		group = "location-ins"
	default:
		return content.ErrInvalid.New("%s: unknown transit load group", location)
	}
	return cache.replaceGroup(ctx, group, true)
}

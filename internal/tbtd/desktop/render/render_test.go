// Copyright (C) 2026 crawter
// All rights reserved.

package render

import (
	"context"
	"testing"

	"too-broke-to-die/internal/tbtd/desktop/assets"
)

func TestRendererConstructionOwnsInterfaceDependency(t *testing.T) {
	ctx := context.Background()
	cache := &assets.Cache{}
	thread := func() uint64 { return 7 }
	ui, _ := interfaceFixture(t)
	for _, interfaces := range [][]*Interface{nil, {nil}, {ui, ui}} {
		if _, err := New(ctx, cache, thread, interfaces...); err == nil {
			t.Fatalf("accepted interface dependencies %v", interfaces)
		}
	}
	renderer, err := New(ctx, cache, thread, ui)
	if err != nil || renderer.interfaceUI != ui {
		t.Fatalf("construction: %v", err)
	}
	if err = renderer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err = renderer.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

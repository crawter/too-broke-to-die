// Copyright (C) 2026 crawter
// All rights reserved.

package desktop

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
)

func TestContentDiagnosticUsesOnlyManifestIdentity(t *testing.T) {
	ctx := context.Background()
	manifest := &content.Manifest{Records: []content.ManifestRecord{{ID: "FONT-NOTO-REGULAR"}}}
	cause := fmt.Errorf("/private/user/font: %w", session.ContentInvalid)
	result, err := PresentContentError(ctx, cause, manifest, "FONT-NOTO-REGULAR")
	if err != nil || !strings.Contains(result.Message, "FONT-NOTO-REGULAR") || strings.Contains(result.Message, "/private") {
		t.Fatalf("diagnostic: %+v %v", result, err)
	}
	for _, id := range []string{"/private", "FONT-MISSING", ""} {
		if _, err := PresentContentError(ctx, cause, manifest, id); err == nil {
			t.Fatalf("accepted diagnostic %q", id)
		}
	}
	if _, err := PresentContentError(ctx, session.StorageFull, manifest, "FONT-NOTO-REGULAR"); err == nil {
		t.Fatal("attached content ID to storage error")
	}
}

func TestErrorHandlerLogsOncePerOccurrence(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	handler, err := NewErrorHandler(logger)
	if err != nil {
		t.Fatal(err)
	}
	occurrence := &Failure{}
	cause := fmt.Errorf("private path: %w", session.Internal)
	for i := 0; i < 3; i++ {
		if _, err = handler.Handle(context.Background(), occurrence, cause); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(output.String(), "desktop failure") != 1 || !strings.Contains(output.String(), "class=internal") || strings.Contains(output.String(), "private") {
		t.Fatalf("log: %s", output.String())
	}
	if _, err = handler.Handle(context.Background(), &Failure{}, session.InvalidAction); err != nil || strings.Count(output.String(), "desktop failure") != 1 {
		t.Fatal("invalid action logged as a technical failure")
	}
	if _, err = NewErrorHandler(nil); err == nil {
		t.Fatal("missing logger accepted")
	}
}

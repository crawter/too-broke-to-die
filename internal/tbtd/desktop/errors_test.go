// Copyright (C) 2026 crawter
// All rights reserved.

package desktop

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"too-broke-to-die/internal/tbtd/content"
	"too-broke-to-die/internal/tbtd/session"
)

func TestPresentErrorIdentity(t *testing.T) {
	for _, code := range []session.ErrorCode{session.InvalidAction, session.ContentInvalid, session.StorageDenied, session.StorageFull, session.StorageConflict, session.SaveCorrupt, session.SaveTooNew, session.ContentIncompatible, session.GraphicsUnavailable, session.AudioUnavailable, session.Internal} {
		result := PresentError(fmt.Errorf("secret path: %w", code))
		if result.Code != code || result.Message == "" || strings.Contains(result.Message, "secret") {
			t.Fatalf("%s: %+v", code, result)
		}
	}
	if result := PresentError(content.ErrInvalid.New("FONT-NOTO-REGULAR: private path")); result.Code != session.ContentInvalid || strings.Contains(result.Message, "private") {
		t.Fatalf("content class: %+v", result)
	}
	if result := PresentError(errors.New("storage_full: secret")); result.Code != session.Internal || strings.Contains(result.Message, "secret") {
		t.Fatalf("must not parse error text: %+v", result)
	}
	if result := PresentError(session.AudioUnavailable); len(result.Choices) != 2 || result.Choices[0] != "continue_silent" {
		t.Fatalf("audio choices: %+v", result)
	}
	if PresentError(nil).Message != "" {
		t.Fatal("nil error must not display a failure")
	}
}

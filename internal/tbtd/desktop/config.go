// Copyright (C) 2026 crawter
// All rights reserved.

package desktop

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Config supplies process-boundary paths and player-requested launch options.
// ContentDir is the installation root, not the current working directory.
// Sensitive contains the local account name for diagnostic redaction.
type Config struct {
	ContentDir     string
	DataDir        string
	ProductVersion string
	Sensitive      []string
	Windowed       bool
	ResetSettings  bool
	Diagnostics    bool
	DrainBudget    time.Duration
}

// Validate rejects incomplete or unsafe process configuration before acquisition.
func (config Config) Validate(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return err
	}
	if !filepath.IsAbs(config.ContentDir) || !filepath.IsAbs(config.DataDir) || strings.TrimSpace(config.ProductVersion) == "" || config.DrainBudget <= 0 || config.DrainBudget > 10*time.Second {
		return fmt.Errorf("invalid desktop configuration")
	}
	if len(config.Sensitive) == 0 {
		return fmt.Errorf("missing diagnostic redaction terms")
	}
	for _, term := range config.Sensitive {
		if term == "" {
			return fmt.Errorf("empty diagnostic redaction term")
		}
	}
	return nil
}

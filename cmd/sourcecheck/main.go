// Copyright (c) TOO BROKE TO DIE contributors

package main

import (
	"context"
	"fmt"
	"os"

	monkit "github.com/spacemonkeygo/monkit/v3"

	"github.com/crawter/too-broke-to-die/internal/sourcecheck"
)

var mon = monkit.Package()

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)

	diagnostics, err := sourcecheck.Check(ctx, ".")
	if err != nil {
		return err
	}
	for _, diagnostic := range diagnostics {
		fmt.Fprintln(os.Stderr, diagnostic.String())
	}
	if len(diagnostics) != 0 {
		return fmt.Errorf("%d source rule violation(s)", len(diagnostics))
	}
	return nil
}

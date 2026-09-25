// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/prodimagetags"
)

func init() {
	register(Gate{
		Name:    "prod-image-tags",
		Summary: "Forbids mutable :latest-style image tags in production manifests.",
		Setup:   setupProdImageTags,
	})
}

func setupProdImageTags(fs *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		scanPaths := fs.Args() // optional positional scan-path overrides
		o, err := prodimagetags.Check(prodimagetags.Config{Root: inv.Root, ScanPaths: scanPaths})
		if err != nil {
			return Fail, err
		}
		if !o.Pass {
			fmt.Fprintf(inv.Stderr, "✗ VIOLATION: %d mutable image tag(s) in production-scoped manifests:\n", len(o.Violations))
			for _, v := range o.Violations {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "Per ADR-0020 D1: production manifests MUST use immutable :sha7 tags.")
			fmt.Fprintln(inv.Stderr, "To override an intentional case, add '# loopzero:mutable-tag-allowed' + ADR ref.")
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "✓ Scanned %d file(s). No :prod or :latest tags in production manifests.\n", o.ScannedFiles)
		return Pass, nil
	}
}

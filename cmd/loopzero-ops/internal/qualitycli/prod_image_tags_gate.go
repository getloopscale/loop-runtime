// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/prodimagetags"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "prod-image-tags",
		Summary: "Forbids mutable :latest-style image tags in production manifests.",
		Setup:   setupProdImageTags,
	})
}

func setupProdImageTags(fs *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		scanPaths := fs.Args() // optional positional scan-path overrides
		o, err := prodimagetags.Check(prodimagetags.Config{Root: inv.Root, ScanPaths: scanPaths})
		if err != nil {
			return nil, err
		}
		if o.Verdict() == verdict.Fail {
			inv.Stderr.Printf("✗ VIOLATION: %d mutable image tag(s) in production-scoped manifests:\n", len(o.Violations))
			for _, v := range o.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Per ADR-0020 D1: production manifests MUST use immutable :sha7 tags.")
			inv.Stderr.Println("To override an intentional case, add '# loopzero:mutable-tag-allowed' + ADR ref.")
			return o, nil
		}
		inv.Stdout.Printf("✓ Scanned %d file(s). No :prod or :latest tags in production manifests.\n", o.ScannedFiles)
		return o, nil
	}
}

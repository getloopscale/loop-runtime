// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/baseimagedigest"
)

func init() {
	register(Gate{
		Name:    "base-image-digest",
		Summary: "Base images are pinned by digest, not floating tags.",
		Setup:   setupBaseImageDigest,
	})
}

func setupBaseImageDigest(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o := baseimagedigest.Check(baseimagedigest.Config{Root: inv.Root})
		if !o.Pass {
			for _, v := range o.Violations {
				fmt.Fprintf(inv.Stdout, "BASE-IMAGE-DIGEST VIOLATION: %s\n", v)
			}
			fmt.Fprintf(inv.Stderr, "\nFAIL — %d tag-only base image(s).\n", len(o.Violations))
			fmt.Fprintln(inv.Stderr, "Fix: replace 'tag' with 'tag@sha256:<digest>' (Renovate pinDigests: true),")
			fmt.Fprintln(inv.Stderr, "OR add '# pragma: allow-tag-pin' on the line above the FROM, OR refresh the")
			fmt.Fprintln(inv.Stderr, "baseline in cmd/loopzero-ops/internal/quality/baseimagedigest if the bump is intentional + grandfathered.")
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "PASS — %d FROM directive(s); every base image is digest-pinned, baselined, or pragma'd.\n", o.Scanned)
		return Pass, nil
	}
}

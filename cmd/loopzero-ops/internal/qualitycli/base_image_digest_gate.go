// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

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
				inv.Stdout.Printf("BASE-IMAGE-DIGEST VIOLATION: %s\n", v)
			}
			inv.Stderr.Printf("\nFAIL — %d tag-only base image(s).\n", len(o.Violations))
			inv.Stderr.Println("Fix: replace 'tag' with 'tag@sha256:<digest>' (Renovate pinDigests: true),")
			inv.Stderr.Println("OR add '# pragma: allow-tag-pin' on the line above the FROM, OR refresh the")
			inv.Stderr.Println("baseline in cmd/loopzero-ops/internal/quality/baseimagedigest if the bump is intentional + grandfathered.")
			return Fail, nil
		}
		inv.Stdout.Printf("PASS — %d FROM directive(s); every base image is digest-pinned, baselined, or pragma'd.\n", o.Scanned)
		return Pass, nil
	}
}

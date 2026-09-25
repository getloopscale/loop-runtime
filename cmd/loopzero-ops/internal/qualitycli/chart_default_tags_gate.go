// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/chartdefaulttags"
)

func init() {
	register(Gate{
		Name:    "chart-default-tags",
		Summary: "loopzero-owned charts must not default their image tag to a mutable ref (#232).",
		Setup:   setupChartDefaultTags,
	})
}

// setupChartDefaultTags is the #232 sibling gate: loopzero-owned Helm charts must not
// DEFAULT their image tag to a mutable ref. prod-image-tags scans the deployed
// values and cannot see chart defaults. Registered but NOT wired — RED today,
// fix lives in infra/helm/ (governance-gated).
func setupChartDefaultTags(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := chartdefaulttags.Check(chartdefaulttags.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if out.Skipped {
			fmt.Fprintf(inv.Stdout, "chart-default-tags: SKIP — %s\n", out.Reason)
			return Pass, nil
		}
		if out.Pass {
			fmt.Fprintf(inv.Stdout, "chart-default-tags: PASS — %s\n", out.Reason)
			return Pass, nil
		}
		fmt.Fprintln(inv.Stderr, "chart-default-tags: FAIL")
		for _, f := range out.Findings {
			fmt.Fprintf(inv.Stderr, "  %s:%d — chart %s defaults to mutable tag %q\n", f.File, f.Line, f.Chart, f.Tag)
		}
		fmt.Fprintln(inv.Stderr, "Fix: prefer `tag: \"\"` — an empty default fails loudly if nothing overrides it, where `latest` fails silently (#232).")
		return Fail, nil
	}
}

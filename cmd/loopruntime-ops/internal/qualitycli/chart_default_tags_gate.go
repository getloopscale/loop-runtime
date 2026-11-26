// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/chartdefaulttags"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "chart-default-tags",
		Summary: "loopruntime-owned charts must not default their image tag to a mutable ref (#232).",
		Setup:   setupChartDefaultTags,
	})
}

// setupChartDefaultTags is the #232 sibling gate: loopruntime-owned Helm charts must not
// DEFAULT their image tag to a mutable ref. prod-image-tags scans the deployed
// values and cannot see chart defaults. Registered but NOT wired — RED today,
// fix lives in infra/helm/ (governance-gated).
func setupChartDefaultTags(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := chartdefaulttags.Check(chartdefaulttags.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Skip {
			inv.Stdout.Printf("chart-default-tags: SKIP — %s\n", out.Reason)
			return out, nil
		}
		if out.Verdict() == verdict.Pass {
			inv.Stdout.Printf("chart-default-tags: PASS — %s\n", out.Reason)
			return out, nil
		}
		inv.Stderr.Println("chart-default-tags: FAIL")
		for _, f := range out.Findings {
			inv.Stderr.Printf("  %s:%d — chart %s defaults to mutable tag %q\n", f.File, f.Line, f.Chart, f.Tag)
		}
		inv.Stderr.Println("Fix: prefer `tag: \"\"` — an empty default fails loudly if nothing overrides it, where `latest` fails silently (#232).")
		return out, nil
	}
}

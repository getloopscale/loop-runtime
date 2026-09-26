// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/chartpairs"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "chart-pairs",
		Summary: "No chart exists twice across infra/helm and charts/ — by name, or as a template-identical copy under another name.",
		Setup:   setupChartPairs,
	})
}

func setupChartPairs(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := chartpairs.Check(chartpairs.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("chart-pairs: FAIL — %v\n", out.Violation)
			return out, nil
		}
		inv.Stdout.Printf("chart-pairs: PASS — %d chart(s) in infra/helm, %d in charts/, none of them twice\n",
			out.InfraHelm, out.Charts)
		return out, nil
	}
}

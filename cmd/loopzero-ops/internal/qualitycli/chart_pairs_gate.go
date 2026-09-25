// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/chartpairs"
)

func init() {
	register(Gate{
		Name:    "chart-pairs",
		Summary: "A chart that exists in both trees under the same name must be declared.",
		Setup:   setupChartPairs,
	})
}

func setupChartPairs(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := chartpairs.Check(chartpairs.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, chartpairs.ErrUndeclared, chartpairs.ErrStale) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "chart-pairs: FAIL — %v\n", err)
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "chart-pairs: PASS — %d in infra/helm, %d in charts/, %d declared pair(s): %v\n",
			out.InfraHelm, out.Charts, len(out.Pairs), out.Pairs)
		return Pass, nil
	}
}

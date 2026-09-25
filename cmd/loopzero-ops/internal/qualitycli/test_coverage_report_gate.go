// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testcoveragereport"
)

func init() {
	register(Gate{
		Name:    "test-coverage-report",
		Summary: "Hermetic coverage gate for infra/tests/{unit,charts}/... — runs `go test -cover -json` against configured scope + threshold (default 30%). Advisory-fallthrough when `go` binary missing. M23 T5.1 closes F22 (coverage_pct not measured).",
		Setup:   setupTestCoverageReport,
	})
}

// setupTestCoverageReport is the Go home for the test-coverage-report gate
// (M23 T5.1 F22 closure — coverage_pct not measured). Runs `go test -cover -json`
// against configured scope + threshold. Advisory-fallthrough when `go` binary
// absent.
func setupTestCoverageReport(fs *flag.FlagSet) Check {
	threshold := fs.Float64("threshold", testcoveragereport.DefaultCoverageThreshold, "coverage percentage floor (0-100)")
	tags := fs.String("tags", "", "go build tags (e.g., helm_render)")
	return func(inv Invocation) (Outcome, error) {
		outcome, err := testcoveragereport.Check(testcoveragereport.Config{
			RepoRoot:          inv.Root,
			CoverageThreshold: *threshold,
			BuildTags:         *tags,
		})
		if err != nil {
			return Fail, err
		}
		if outcome.Skipped {
			inv.Stdout.Printf("test-coverage-report: SKIP — %s\n", outcome.SkipReason)
			return Pass, nil
		}
		if !outcome.Pass {
			inv.Stderr.Printf("test-coverage-report: FAIL — coverage=%.2f%% (threshold=%.2f%%)", outcome.CoveragePct, *threshold)
			if len(outcome.FailedPackages) > 0 {
				inv.Stderr.Printf("; failed packages:")
				for _, p := range outcome.FailedPackages {
					inv.Stderr.Printf(" %s", p)
				}
			}
			inv.Stderr.Println()
			return Fail, nil
		}
		inv.Stdout.Printf("test-coverage-report: PASS — coverage=%.2f%% (threshold=%.2f%%)\n", outcome.CoveragePct, *threshold)
		return Pass, nil
	}
}

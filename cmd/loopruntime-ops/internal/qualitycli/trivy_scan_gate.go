// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"context"
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/trivyscan"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "trivy-scan",
		Summary: "Runs Trivy CVE scanning on filesystem/images.",
		Setup:   setupTrivyScan,
	})
}

// setupTrivyScan wraps `trivy fs --severity CRITICAL,HIGH` as a quality gate.
// It fails closed: a missing scanner or a scan that could not complete never
// exits 0 — it is reported as a check that could not complete (exit 5), with
// the reason, and a finding exits 1.
func setupTrivyScan(fs *flag.FlagSet) Check {
	severities := fs.String("severities", "CRITICAL,HIGH", "comma-separated severity filter")
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := trivyscan.Check(context.Background(), trivyscan.Config{
			RepoRoot:   inv.Root,
			Severities: *severities,
		})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("trivy-scan: FAIL\n%s\n", outcome.Reason)
			return outcome, nil
		}
		inv.Stdout.Printf("trivy-scan: PASS — %s\n", outcome.Reason)
		return outcome, nil
	}
}

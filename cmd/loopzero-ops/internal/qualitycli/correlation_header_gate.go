// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/correlationheader"
)

func init() {
	register(Gate{
		Name:    "correlation-header",
		Summary: "Correlation-ID header is forwarded across the request path.",
		Setup:   setupCorrelationHeader,
	})
}

// setupCorrelationHeader scans api/internal/routes/*.go for direct
// r.Header.Get("(X-)?Correlation-ID") literals. Routes MUST go through
// api/internal/correlation.Read(r) instead (T6.1 of plan
// 2026-06-01-p3-infra-observability-test-debt, ADR D5 — closes val-002).
func setupCorrelationHeader(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := correlationheader.Check(correlationheader.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass() {
			inv.Stderr.Printf("correlation-header: FAIL — %d direct r.Header.Get(\"(X-)?Correlation-ID\") call(s) in routes/:\n", len(outcome.Findings))
			for _, v := range outcome.Findings {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Fix: replace with `id := correlation.Read(r)` (api/internal/correlation).")
			inv.Stderr.Println("The helper handles backward-compat (X-Correlation-ID fallback) + deprecation WARN.")
			return Fail, nil
		}
		inv.Stdout.Printf("correlation-header: PASS — no direct r.Header.Get((X-)?Correlation-ID) in api/internal/routes/ (%s)\n", outcome.Summary())
		return Pass, nil
	}
}

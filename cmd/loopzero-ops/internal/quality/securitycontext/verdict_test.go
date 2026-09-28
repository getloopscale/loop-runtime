// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package securitycontext

import (
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func TestAnEnforcedAuditFailsOnAnUnexemptedChart(t *testing.T) {
	if got := (Outcome{Gated: true, Violations: []string{"x"}}).Verdict(); got != verdict.Fail {
		t.Fatalf("Verdict() = %v, want FAIL", got)
	}
	if got := (Outcome{Gated: true, Pass: true}).Verdict(); got != verdict.Pass {
		t.Fatalf("Verdict() = %v, want PASS", got)
	}
}

func TestAReportRunNeverFails(t *testing.T) {
	if got := (Outcome{Violations: []string{"x"}}).Verdict(); got != verdict.Pass {
		t.Fatalf("Verdict() = %v, want PASS", got)
	}
}

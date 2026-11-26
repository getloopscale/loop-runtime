// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vapdeleteguardrail

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func TestReportVerdictFailsOnFindingsNotOnWarnings(t *testing.T) {
	if got := (Report{Findings: []Finding{{}}}).Verdict(); got != verdict.Fail {
		t.Fatalf("with a finding: Verdict() = %v, want FAIL", got)
	}
	if got := (Report{Warnings: []string{"helm absent"}}).Verdict(); got != verdict.Pass {
		t.Fatalf("warnings only: Verdict() = %v, want PASS", got)
	}
}

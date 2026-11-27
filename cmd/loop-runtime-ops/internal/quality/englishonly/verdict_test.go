// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package englishonly

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func TestReportVerdictFailsOnAnyFinding(t *testing.T) {
	if got := (Report{Files: 1, Findings: []Finding{{Path: "a.go", Line: 1}}}).Verdict(); got != verdict.Fail {
		t.Fatalf("with a finding: Verdict() = %v, want FAIL", got)
	}
	if got := (Report{Files: 1, Exempted: 2}).Verdict(); got != verdict.Pass {
		t.Fatalf("clean: Verdict() = %v, want PASS", got)
	}
}

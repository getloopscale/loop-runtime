// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package gomodauthorg

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func TestReportVerdictFailsOnAnyFinding(t *testing.T) {
	if got := (Report{Findings: []Finding{{File: "ci.yml"}}}).Verdict(); got != verdict.Fail {
		t.Fatalf("with a finding: Verdict() = %v, want FAIL", got)
	}
	if got := (Report{RequiredOrgs: []string{"getloopscale"}}).Verdict(); got != verdict.Pass {
		t.Fatalf("clean: Verdict() = %v, want PASS", got)
	}
}

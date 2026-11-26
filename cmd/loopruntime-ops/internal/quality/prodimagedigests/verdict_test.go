// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package prodimagedigests

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func TestVerdictIsPassOnlyWhenTheCheckPassed(t *testing.T) {
	if got := (Outcome{Pass: true, Violations: []Violation{{}}}).Verdict(); got != verdict.Pass {
		t.Fatalf("warn-only violations: Verdict() = %v, want PASS", got)
	}
	if got := (Outcome{}).Verdict(); got != verdict.Fail {
		t.Fatalf("failing outcome: Verdict() = %v, want FAIL", got)
	}
}

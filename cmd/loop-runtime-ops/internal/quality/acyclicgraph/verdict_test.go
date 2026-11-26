// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package acyclicgraph

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func TestVerdictIsPassOnlyWhenTheCheckPassed(t *testing.T) {
	if got := (Outcome{Pass: true, Skipped: []string{"loop-runtime-cli"}}).Verdict(); got != verdict.Pass {
		t.Fatalf("pass with an absent sibling: Verdict() = %v, want PASS", got)
	}
	if got := (Outcome{Violations: []string{"x"}}).Verdict(); got != verdict.Fail {
		t.Fatalf("failing outcome: Verdict() = %v, want FAIL", got)
	}
}

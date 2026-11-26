// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dashboardpromtool

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func TestVerdictIsPassOnlyWhenTheCheckPassed(t *testing.T) {
	if got := (Outcome{Pass: true, PromtoolSkipped: true}).Verdict(); got != verdict.Pass {
		t.Fatalf("promtool skipped: Verdict() = %v, want PASS", got)
	}
	if got := (Outcome{SyntaxErrors: []string{"a.json"}}).Verdict(); got != verdict.Fail {
		t.Fatalf("syntax error: Verdict() = %v, want FAIL", got)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package readinesssummary

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func TestVerdictIsPassOnlyWhenTheCheckPassed(t *testing.T) {
	if got := (Outcome{Pass: true}).Verdict(); got != verdict.Pass {
		t.Fatalf("passing outcome: Verdict() = %v, want PASS", got)
	}
	if got := (Outcome{}).Verdict(); got != verdict.Fail {
		t.Fatalf("failing outcome: Verdict() = %v, want FAIL", got)
	}
}

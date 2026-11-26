// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package recordstreesplit

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func TestOutcomeVerdictOfAnAbsentLiveTreeIsSkip(t *testing.T) {
	if got := (Outcome{LiveMissing: true}).Verdict(); got != verdict.Skip {
		t.Fatalf("live tree absent: Verdict() = %v, want SKIP", got)
	}
}

func TestOutcomeVerdictFollowsPass(t *testing.T) {
	if got := (Outcome{Pass: true}).Verdict(); got != verdict.Pass {
		t.Fatalf("passing outcome: Verdict() = %v, want PASS", got)
	}
	if got := (Outcome{Violations: []string{"x"}}).Verdict(); got != verdict.Fail {
		t.Fatalf("failing outcome: Verdict() = %v, want FAIL", got)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package recordstreesplit

import (
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func TestOutcomeVerdictOfAnAbsentLiveTreeIsNotVerified(t *testing.T) {
	if got := (Outcome{LiveMissing: true}).Verdict(); got != verdict.NotVerified {
		t.Fatalf("live tree absent: Verdict() = %v, want NOT VERIFIED", got)
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

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package drillcredisolation

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func TestResultVerdictFollowsTheIsolationOutcome(t *testing.T) {
	cases := map[int]verdict.Verdict{0: verdict.Pass, 1: verdict.Fail, 2: verdict.NotVerified}
	for code, want := range cases {
		if got := (Result{ExitCode: code}).Verdict(); got != want {
			t.Errorf("ExitCode %d: Verdict() = %v, want %v", code, got, want)
		}
	}
}

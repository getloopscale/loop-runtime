// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package cliexitcodes

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func TestVerdictFailsOnAnyViolation(t *testing.T) {
	if got := (Outcome{Checked: true, ScannedFiles: 1, Violations: []string{"x"}}).Verdict(); got != verdict.Fail {
		t.Fatalf("Verdict() = %v, want FAIL", got)
	}
}

func TestVerdictOfARunWithNoCLISourceIsAPass(t *testing.T) {
	// The report says PARTIAL; the exit status stays 0, as documented on the gate.
	if got := (Outcome{Checked: false}).Verdict(); got != verdict.Pass {
		t.Fatalf("Verdict() = %v, want PASS", got)
	}
}

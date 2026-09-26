// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package spotcheck

import (
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func TestASelectionIsAPass(t *testing.T) {
	if got := (Outcome{Seed: 1, Dimension: "d"}).Verdict(); got != verdict.Pass {
		t.Fatalf("Verdict() = %v, want PASS", got)
	}
}

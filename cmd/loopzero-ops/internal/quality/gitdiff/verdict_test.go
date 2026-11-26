// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitdiff

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func TestAListingIsAPassEvenWhenEmpty(t *testing.T) {
	if got := (Pairs{}).Verdict(); got != verdict.Pass {
		t.Fatalf("Verdict() = %v, want PASS", got)
	}
}

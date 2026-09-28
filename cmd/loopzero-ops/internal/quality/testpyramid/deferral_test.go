// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testpyramid

import (
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func TestTheDeferLabelSkipsTheRatchet(t *testing.T) {
	d, ok := Deferral("bug, pyramid-defer ,docs")

	if !ok || d.Verdict() != verdict.Skip {
		t.Fatalf("Deferral = (%v, %v), want a Skip", d.Verdict(), ok)
	}
}

func TestOtherLabelsDoNotDefer(t *testing.T) {
	if _, ok := Deferral("bug,pyramid-deferred"); ok {
		t.Fatal("only the exact label defers")
	}
}

func TestARefreezeIsAPass(t *testing.T) {
	if got := (Refrozen{Total: 3}).Verdict(); got != verdict.Pass {
		t.Fatalf("Verdict() = %v, want PASS", got)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domainshttp

import (
	"testing"

	"github.com/getloopscale/loopzero/pkg/statemachine"
)

func TestRemovalPhase_AsksTheMachineWithoutPanicking(t *testing.T) {
	for _, current := range []string{"pending_verification", "pending_ns", "active", "failed"} {
		if next, err := removalPhase(current); err != nil || next != statemachine.DPhRemoving {
			t.Errorf("removalPhase(%q) = (%q, %v), want (removing, nil)", current, next, err)
		}
	}
	for _, current := range []string{"removing", "not-a-phase"} {
		if _, err := removalPhase(current); err == nil {
			t.Errorf("removalPhase(%q) succeeded; a terminal or unknown phase has no removal", current)
		}
	}
}

func TestVerificationPhase_VerifiesPendingAndRetriesFailed(t *testing.T) {
	for _, current := range []string{"pending_verification", "failed"} {
		if next, err := verificationPhase(current); err != nil || next != statemachine.DPhVerified {
			t.Errorf("verificationPhase(%q) = (%q, %v), want (verified, nil)", current, next, err)
		}
	}
	for _, current := range []string{"pending_ns", "degraded", "removing"} {
		if _, err := verificationPhase(current); err == nil {
			t.Errorf("verificationPhase(%q) succeeded; it has no verification transition", current)
		}
	}
}

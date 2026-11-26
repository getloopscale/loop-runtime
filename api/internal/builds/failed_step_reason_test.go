// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"testing"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
)

func TestWorkflowFailureReason_NamesTheStepAndItsMessage(t *testing.T) {
	got := workflowFailureReason(&deploycontracts.WorkflowStatus{
		Phase: "Failed", FailedStep: "install-deps", FailedStepMessage: "main: Error (exit code 1)",
	})

	if want := `Build failed at step "install-deps": main: Error (exit code 1). The step's log has the cause.`; got != want {
		t.Errorf("reason = %q\n  want = %q", got, want)
	}
}

func TestWorkflowFailureReason_AStepWithNoMessage(t *testing.T) {
	got := workflowFailureReason(&deploycontracts.WorkflowStatus{Phase: "Failed", FailedStep: "build-apps"})

	if want := `Build failed at step "build-apps". The step's log has the cause.`; got != want {
		t.Errorf("reason = %q\n  want = %q", got, want)
	}
}

func TestWorkflowFailureReason_WithoutAStepKeepsThePhase(t *testing.T) {
	if got := workflowFailureReason(&deploycontracts.WorkflowStatus{Phase: "Error"}); got != "Build error" {
		t.Errorf("reason = %q, want %q", got, "Build error")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// Beta Logs Gate Criterion 6 — timeout reasons must include:
//   - The SM state name (QUEUED / RUNNING / RELEASING / cancellation)
//   - A "Likely cause:" phrase
//   - A "Next:" actionable hint
//
// The case-insensitive checks below capture the contract without
// pinning the exact wording (so we can keep tightening the messages
// without test churn).

func TestReaperReason_QueueTimeout(t *testing.T) {
	reason := reaperReason(statemachine.BPEventQueueTimeout)
	lower := strings.ToLower(reason)
	if !strings.Contains(lower, "queued") {
		t.Errorf("expected QUEUED state mention, got %q", reason)
	}
	if !strings.Contains(lower, "5 minute") {
		t.Errorf("expected budget mention (5 min), got %q", reason)
	}
	if !strings.Contains(lower, "likely cause") {
		t.Errorf("expected `Likely cause` phrase (Beta Logs Gate Criterion 6), got %q", reason)
	}
	if !strings.Contains(lower, "next:") {
		t.Errorf("expected `Next:` actionable hint, got %q", reason)
	}
}

func TestReaperReason_BuildTimeout(t *testing.T) {
	reason := reaperReason(statemachine.BPEventTimeout)
	lower := strings.ToLower(reason)
	if !strings.Contains(lower, "running") {
		t.Errorf("expected RUNNING state mention, got %q", reason)
	}
	if !strings.Contains(lower, "30 minute") {
		t.Errorf("expected budget mention (30 min), got %q", reason)
	}
	if !strings.Contains(lower, "likely cause") {
		t.Errorf("expected `Likely cause` phrase, got %q", reason)
	}
	if !strings.Contains(lower, "loopzero logs") {
		t.Errorf("expected `loopzero logs` next-step hint, got %q", reason)
	}
}

func TestReaperReason_ReleasingTimeout(t *testing.T) {
	// Beta Logs Gate gap closed by iter 6 — the RELEASING state had
	// no dedicated reason branch (fell through to "Build reaped due
	// to timeout"). Now produces an actionable RELEASING-specific
	// message.
	reason := reaperReason(statemachine.BPEventReleaseTimeout)
	lower := strings.ToLower(reason)
	if !strings.Contains(lower, "releasing") {
		t.Errorf("expected RELEASING state mention, got %q", reason)
	}
	if !strings.Contains(lower, "10 minute") {
		t.Errorf("expected budget mention (10 min), got %q", reason)
	}
	if !strings.Contains(lower, "likely cause") {
		t.Errorf("expected `Likely cause` phrase, got %q", reason)
	}
}

func TestReaperReason_CancelTimeout(t *testing.T) {
	reason := reaperReason(statemachine.BPEventCancelTimeout)
	lower := strings.ToLower(reason)
	if !strings.Contains(lower, "cancellation") {
		t.Errorf("expected cancellation mention, got %q", reason)
	}
	if !strings.Contains(lower, "60s") {
		t.Errorf("expected budget mention (60s), got %q", reason)
	}
	if !strings.Contains(lower, "next:") {
		t.Errorf("expected `Next:` actionable hint, got %q", reason)
	}
}

func TestReaperReason_UnknownEvent(t *testing.T) {
	reason := reaperReason(statemachine.BuildPhaseEvent("some_unknown_event"))
	if !strings.Contains(strings.ToLower(reason), "timeout") {
		t.Errorf("expected generic timeout reason, got %q", reason)
	}
	// Even the fallback must still point at a CLI command.
	if !strings.Contains(strings.ToLower(reason), "loopzero logs") {
		t.Errorf("fallback must still suggest `loopzero logs`, got %q", reason)
	}
}

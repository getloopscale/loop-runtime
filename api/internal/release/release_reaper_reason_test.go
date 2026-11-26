// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// Beta Logs Gate Criterion 6 — release reaper reason messages must
// include: SM state name + budget + likely cause + actionable hint.

func TestReleaseReaperReason_GitPushTimeout(t *testing.T) {
	reason := releaseReaperReason(statemachine.RPEventGitPushTimeout)
	lower := strings.ToLower(reason)
	if !strings.Contains(lower, "ready") {
		t.Errorf("expected READY state mention, got %q", reason)
	}
	if !strings.Contains(lower, "5 minute") {
		t.Errorf("expected budget mention (5 min), got %q", reason)
	}
	if !strings.Contains(lower, "likely cause") {
		t.Errorf("expected `Likely cause` phrase, got %q", reason)
	}
	if !strings.Contains(lower, "loopzero deploy") {
		t.Errorf("expected `loopzero deploy` retry hint, got %q", reason)
	}
}

func TestReleaseReaperReason_SyncTimeout(t *testing.T) {
	reason := releaseReaperReason(statemachine.RPEventSyncTimeout)
	lower := strings.ToLower(reason)
	if !strings.Contains(lower, "pending") {
		t.Errorf("expected PENDING state mention, got %q", reason)
	}
	if !strings.Contains(lower, "10 minute") {
		t.Errorf("expected budget mention (10 min), got %q", reason)
	}
	if !strings.Contains(lower, "likely cause") {
		t.Errorf("expected `Likely cause` phrase, got %q", reason)
	}
	// Crucial: runtime logs (NOT build logs) is the right command
	// for a release that's stuck after the manifests were pushed —
	// the pod is the suspect.
	if !strings.Contains(lower, "loopzero logs --runtime") {
		t.Errorf("expected `loopzero logs --runtime` hint (pod is the suspect for PENDING-stuck), got %q", reason)
	}
}

func TestReleaseReaperReason_UnknownEvent(t *testing.T) {
	reason := releaseReaperReason(statemachine.ReleasePhaseEvent("unknown"))
	if !strings.Contains(strings.ToLower(reason), "timeout") {
		t.Errorf("expected generic timeout fallback, got %q", reason)
	}
	if !strings.Contains(strings.ToLower(reason), "loopzero") {
		t.Errorf("fallback must still suggest a loopzero command, got %q", reason)
	}
}

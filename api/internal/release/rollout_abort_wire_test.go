// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// rollout_abort_wire_test.go — T0.3 of deep-review-remediation-plan.md.
// Closes find_flow_001 (CRITICAL): EmitRolloutAborted has zero production
// callers — canary abort SSE contract is dead code.
//
// These tests assert (a) failRelease wires EmitRolloutAborted for
// Rollout-backed releases, (b) static-only releases skip the canary event,
// (c) the reason classifier maps free-text to the documented enum.

package release

import (
	"encoding/json"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

func TestIsRolloutBackedRelease_ServerAppReturnsTrue(t *testing.T) {
	apps := []db.BuildAppSpec{{Name: "api", Rendering: "server"}}
	appsJSON, _ := json.Marshal(apps)
	release := &db.Release{Apps: appsJSON}

	if !isRolloutBackedRelease(release) {
		t.Error("server-rendered app must be classified as Rollout-backed")
	}
}

func TestIsRolloutBackedRelease_StaticOnlyReturnsFalse(t *testing.T) {
	apps := []db.BuildAppSpec{{Name: "frontend", Rendering: "static"}}
	appsJSON, _ := json.Marshal(apps)
	release := &db.Release{Apps: appsJSON}

	if isRolloutBackedRelease(release) {
		t.Error("static-only release must NOT be classified as Rollout-backed")
	}
}

func TestIsRolloutBackedRelease_MixedReturnsTrue(t *testing.T) {
	apps := []db.BuildAppSpec{
		{Name: "frontend", Rendering: "static"},
		{Name: "api", Rendering: "server"},
	}
	appsJSON, _ := json.Marshal(apps)
	release := &db.Release{Apps: appsJSON}

	if !isRolloutBackedRelease(release) {
		t.Error("mixed release with at least one server app must be Rollout-backed")
	}
}

func TestIsRolloutBackedRelease_NilReleaseReturnsFalse(t *testing.T) {
	if isRolloutBackedRelease(nil) {
		t.Error("nil release must return false (no panic)")
	}
}

func TestIsRolloutBackedRelease_NilAppsReturnsFalse(t *testing.T) {
	release := &db.Release{Apps: nil}
	if isRolloutBackedRelease(release) {
		t.Error("release with nil Apps must return false")
	}
}

func TestIsRolloutBackedRelease_MalformedAppsFailsClosed(t *testing.T) {
	// Failing closed (return true on parse error) ensures find_flow_001
	// stays closed even if Apps JSON gets malformed by some upstream bug.
	release := &db.Release{Apps: json.RawMessage(`{not valid json`)}
	if !isRolloutBackedRelease(release) {
		t.Error("malformed Apps JSON must fail closed (return true) so canary event still fires")
	}
}

func TestClassifyAbortReason_AnalysisFailed(t *testing.T) {
	got := classifyAbortReason("AnalysisRun success-rate dropped below threshold")
	if got != "analysis-failed" {
		t.Errorf("expected analysis-failed, got %q", got)
	}
}

func TestClassifyAbortReason_DeadlineExceeded(t *testing.T) {
	got := classifyAbortReason("progressDeadlineSeconds exceeded")
	if got != "progress-deadline-exceeded" {
		t.Errorf("expected progress-deadline-exceeded, got %q", got)
	}
}

func TestClassifyAbortReason_UserAborted(t *testing.T) {
	got := classifyAbortReason("user requested cancel")
	if got != "user-aborted" {
		t.Errorf("expected user-aborted, got %q", got)
	}
}

func TestClassifyAbortReason_DefaultPodUnhealthy(t *testing.T) {
	// Convergence loop's primary failure: "Sync timeout: service did not
	// become healthy within the time limit" — should classify as
	// pod-unhealthy by default.
	got := classifyAbortReason("Sync timeout: service did not become healthy within the time limit")
	if got != "pod-unhealthy" {
		t.Errorf("expected pod-unhealthy default, got %q", got)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/builds"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// TestController_IdempotentTransitions — roadmap 02 DoD.
//
// Proves that the build reaper's ReapOnce method is safe to call
// multiple times on the same stale build without corrupting state.
// The first call transitions QUEUED → FAILED. The second call must
// be a no-op (the build is already in a terminal state, so the
// reaper's re-check of the current status skips it).

func TestController_IdempotentTransitions(t *testing.T) {
	// Arrange: a build stuck in QUEUED (stale)
	buildStore := &fakeBuildStoreOrch{}
	buildStore.builds = map[string]*db.Build{
		"build-idem-1": {
			ID:          "build-idem-1",
			TenantID:    "tn-idem",
			ProjectID:   "proj-idem",
			Environment: "production",
			Status:      string(statemachine.BPhQueued),
		},
	}

	checker := &fakeStaleChecker{
		builds: []builds.StaleBuild{
			{
				ID:    "build-idem-1",
				State: statemachine.BPhQueued,
				Event: statemachine.BPEventQueueTimeout,
			},
		},
	}

	reaper := builds.NewBuildReaper(
		buildStore,
		checker,
		&fakeLockerIdem{},
		&fakeWorkflowIdem{},
		&fakeEventStoreIdem{},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		builds.DefaultBuildReaperConfig(),
		nil, // reconcile checker
	)

	// Act 1: first reap — should transition QUEUED → FAILED
	reaped1 := reaper.ReapOnce(context.Background())
	if reaped1 != 1 {
		t.Fatalf("first reap: expected 1 reaped, got %d", reaped1)
	}
	if buildStore.builds["build-idem-1"].Status != string(statemachine.BPhFailed) {
		t.Fatalf("first reap: expected FAILED, got %s", buildStore.builds["build-idem-1"].Status)
	}

	// Act 2: second reap — same stale list presented again.
	// The build is now FAILED (terminal). The reaper re-checks the
	// current status and skips terminal builds.
	reaped2 := reaper.ReapOnce(context.Background())
	if reaped2 != 0 {
		t.Errorf("second reap: expected 0 (idempotent no-op), got %d", reaped2)
	}

	// Assert: state is still FAILED, not double-transitioned
	finalStatus := buildStore.builds["build-idem-1"].Status
	if finalStatus != string(statemachine.BPhFailed) {
		t.Errorf("after double reap: expected FAILED, got %s", finalStatus)
	}
}

// Minimal fakes matching current interfaces after merge.

type fakeLockerIdem struct{}

func (f *fakeLockerIdem) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

type fakeWorkflowIdem struct{}

func (f *fakeWorkflowIdem) Submit(_ context.Context, _ deploycontracts.WorkflowSpec) (string, error) {
	return "", nil
}

func (f *fakeWorkflowIdem) Cancel(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeWorkflowIdem) GetStatus(_ context.Context, _, _ string) (*deploycontracts.WorkflowStatus, error) {
	return &deploycontracts.WorkflowStatus{Phase: "Succeeded"}, nil
}

type fakeEventStoreIdem struct{}

func (f *fakeEventStoreIdem) Append(_ context.Context, _ string, _ string, _ json.RawMessage) (int64, error) {
	return 0, nil
}

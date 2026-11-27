// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds_test

import (
	"context"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/builds"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/pkg/logging"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// --- fake stale checker ---

type fakeStaleChecker struct {
	builds []builds.StaleBuild
	err    error
}

func (f *fakeStaleChecker) FindStaleBuilds(_ context.Context, _ string, _, _, _, _ time.Duration) ([]builds.StaleBuild, error) {
	return f.builds, f.err
}

func TestBuildReaper_ReapsStaleBuild(t *testing.T) {
	// Arrange
	buildStore := &fakeBuildStoreOrch{}
	buildStore.builds = map[string]*db.Build{
		"build-stale-1": {
			ID:          "build-stale-1",
			TenantID:    "acme",
			ProjectID:   "proj1",
			Environment: "production",
			Status:      string(statemachine.BPhRunning),
		},
	}

	checker := &fakeStaleChecker{
		builds: []builds.StaleBuild{
			{ID: "build-stale-1", State: statemachine.BPhRunning, Event: statemachine.BPEventTimeout},
		},
	}

	locker := &fakeEnvironmentLocker{}
	logger := logging.NewLogger("test")

	reaper := builds.NewBuildReaper(buildStore, checker, locker, nil, nil, logger, builds.BuildReaperConfig{
		QueueTimeout:   5 * time.Minute,
		RunningTimeout: 30 * time.Minute,
		CancelTimeout:  60 * time.Second,
		Interval:       1 * time.Second,
	}, nil)

	// Act
	reaped := reaper.ReapOnce(context.Background())

	// Assert
	if reaped != 1 {
		t.Errorf("expected 1 reaped build, got %d", reaped)
	}

	build := buildStore.builds["build-stale-1"]
	if build.Status != string(statemachine.BPhFailed) {
		t.Errorf("expected build status FAILED, got %s", build.Status)
	}
}

func TestBuildReaper_SkipsTerminalBuilds(t *testing.T) {
	// Arrange: stale checker reports a build, but it's already FAILED
	buildStore := &fakeBuildStoreOrch{}
	buildStore.builds = map[string]*db.Build{
		"build-done": {
			ID:          "build-done",
			TenantID:    "acme",
			ProjectID:   "proj1",
			Environment: "production",
			Status:      string(statemachine.BPhFailed),
		},
	}

	checker := &fakeStaleChecker{
		builds: []builds.StaleBuild{
			{ID: "build-done", State: statemachine.BPhRunning, Event: statemachine.BPEventTimeout},
		},
	}

	locker := &fakeEnvironmentLocker{}
	logger := logging.NewLogger("test")

	reaper := builds.NewBuildReaper(buildStore, checker, locker, nil, nil, logger, builds.DefaultBuildReaperConfig(), nil)

	// Act
	reaped := reaper.ReapOnce(context.Background())

	// Assert: should skip because build is already terminal
	if reaped != 0 {
		t.Errorf("expected 0 reaped builds, got %d", reaped)
	}
}

func TestBuildReaper_ReapsQueuedBuild(t *testing.T) {
	buildStore := &fakeBuildStoreOrch{}
	buildStore.builds = map[string]*db.Build{
		"build-queued": {
			ID:          "build-queued",
			TenantID:    "acme",
			ProjectID:   "proj1",
			Environment: "staging",
			Status:      string(statemachine.BPhQueued),
		},
	}

	checker := &fakeStaleChecker{
		builds: []builds.StaleBuild{
			{ID: "build-queued", State: statemachine.BPhQueued, Event: statemachine.BPEventQueueTimeout},
		},
	}

	locker := &fakeEnvironmentLocker{}
	logger := logging.NewLogger("test")

	reaper := builds.NewBuildReaper(buildStore, checker, locker, nil, nil, logger, builds.DefaultBuildReaperConfig(), nil)

	reaped := reaper.ReapOnce(context.Background())

	if reaped != 1 {
		t.Errorf("expected 1 reaped build, got %d", reaped)
	}

	build := buildStore.builds["build-queued"]
	if build.Status != string(statemachine.BPhFailed) {
		t.Errorf("expected build status FAILED, got %s", build.Status)
	}
}

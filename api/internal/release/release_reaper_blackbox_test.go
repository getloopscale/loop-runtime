// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release_test

import (
	"context"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/release"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/pkg/logging"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// --- fake stale release checker ---

type fakeStaleReleaseChecker struct {
	releases []release.StaleRelease
	err      error
}

func (f *fakeStaleReleaseChecker) FindStaleReleases(_ context.Context, _ string, _, _ time.Duration) ([]release.StaleRelease, error) {
	return f.releases, f.err
}

func TestReleaseReaper_ReapsReadyRelease(t *testing.T) {
	// Arrange: a release stuck in READY past timeout should transition to GIT_MIRROR_FAILED
	releaseStore := newFakeReleaseStoreOrch()
	releaseStore.releases["rel-stale-ready"] = &db.Release{
		ID:          "rel-stale-ready",
		BuildID:     "build-123",
		TenantID:    "acme",
		ProjectID:   "proj1",
		Environment: "production",
		Status:      string(statemachine.RPhReady),
		CreatedAt:   time.Now().Add(-10 * time.Minute),
	}

	checker := &fakeStaleReleaseChecker{
		releases: []release.StaleRelease{
			{
				ID:          "rel-stale-ready",
				State:       statemachine.RPhReady,
				Event:       statemachine.RPEventGitPushTimeout,
				TenantID:    "acme",
				ProjectID:   "proj1",
				Environment: "production",
				BuildID:     "build-123",
			},
		},
	}

	locker := &fakeEnvironmentLocker{}
	eventStore := newFakeEventStoreOrch()
	logger := logging.NewLogger("test")

	reaper := release.NewReleaseReaper(releaseStore, checker, locker, eventStore, logger, release.ReleaseReaperConfig{
		ReadyTimeout:   5 * time.Minute,
		PendingTimeout: 2 * time.Minute,
		Interval:       1 * time.Second,
	})

	// Act
	reaped := reaper.ReapOnce(context.Background())

	// Assert
	if reaped != 1 {
		t.Errorf("expected 1 reaped release, got %d", reaped)
	}

	release := releaseStore.releases["rel-stale-ready"]
	if release.Status != string(statemachine.RPhGitMirrorFailed) {
		t.Errorf("expected release status GIT_MIRROR_FAILED, got %s", release.Status)
	}

	// Verify SSE event was emitted
	if len(eventStore.events) != 1 {
		t.Fatalf("expected 1 SSE event, got %d", len(eventStore.events))
	}
	if eventStore.events[0].EventType != "deploy.git_push_failed" {
		t.Errorf("expected SSE event type deploy.git_push_failed, got %s", eventStore.events[0].EventType)
	}
}

func TestReleaseReaper_ReapsPendingRelease(t *testing.T) {
	// Arrange: a release stuck in PENDING past timeout should transition to SYNC_FAILED
	releaseStore := newFakeReleaseStoreOrch()
	releaseStore.releases["rel-stale-pending"] = &db.Release{
		ID:          "rel-stale-pending",
		BuildID:     "build-456",
		TenantID:    "acme",
		ProjectID:   "proj1",
		Environment: "staging",
		Status:      string(statemachine.RPhPending),
		CreatedAt:   time.Now().Add(-10 * time.Minute),
	}

	checker := &fakeStaleReleaseChecker{
		releases: []release.StaleRelease{
			{
				ID:          "rel-stale-pending",
				State:       statemachine.RPhPending,
				Event:       statemachine.RPEventSyncTimeout,
				TenantID:    "acme",
				ProjectID:   "proj1",
				Environment: "staging",
				BuildID:     "build-456",
			},
		},
	}

	locker := &fakeEnvironmentLocker{}
	eventStore := newFakeEventStoreOrch()
	logger := logging.NewLogger("test")

	reaper := release.NewReleaseReaper(releaseStore, checker, locker, eventStore, logger, release.DefaultReleaseReaperConfig())

	// Act
	reaped := reaper.ReapOnce(context.Background())

	// Assert
	if reaped != 1 {
		t.Errorf("expected 1 reaped release, got %d", reaped)
	}

	release := releaseStore.releases["rel-stale-pending"]
	if release.Status != string(statemachine.RPhSyncFailed) {
		t.Errorf("expected release status SYNC_FAILED, got %s", release.Status)
	}

	// Verify SSE event was emitted
	if len(eventStore.events) != 1 {
		t.Fatalf("expected 1 SSE event, got %d", len(eventStore.events))
	}
	if eventStore.events[0].EventType != "deploy.sync_failed" {
		t.Errorf("expected SSE event type deploy.sync_failed, got %s", eventStore.events[0].EventType)
	}
}

func TestReleaseReaper_SkipsTerminalReleases(t *testing.T) {
	// Arrange: stale checker reports a release, but it's already in a terminal state
	releaseStore := newFakeReleaseStoreOrch()
	releaseStore.releases["rel-done"] = &db.Release{
		ID:          "rel-done",
		BuildID:     "build-789",
		TenantID:    "acme",
		ProjectID:   "proj1",
		Environment: "production",
		Status:      string(statemachine.RPhSuperseded),
		CreatedAt:   time.Now().Add(-10 * time.Minute),
	}

	checker := &fakeStaleReleaseChecker{
		releases: []release.StaleRelease{
			{
				ID:          "rel-done",
				State:       statemachine.RPhReady,
				Event:       statemachine.RPEventGitPushTimeout,
				TenantID:    "acme",
				ProjectID:   "proj1",
				Environment: "production",
				BuildID:     "build-789",
			},
		},
	}

	locker := &fakeEnvironmentLocker{}
	logger := logging.NewLogger("test")

	reaper := release.NewReleaseReaper(releaseStore, checker, locker, nil, logger, release.DefaultReleaseReaperConfig())

	// Act
	reaped := reaper.ReapOnce(context.Background())

	// Assert: should skip because release is already terminal
	if reaped != 0 {
		t.Errorf("expected 0 reaped releases, got %d", reaped)
	}
}

func TestReleaseReaper_SkipsReleasesWithinTimeout(t *testing.T) {
	// Arrange: checker returns empty (no stale releases found)
	releaseStore := newFakeReleaseStoreOrch()

	checker := &fakeStaleReleaseChecker{
		releases: []release.StaleRelease{}, // no stale releases
	}

	locker := &fakeEnvironmentLocker{}
	logger := logging.NewLogger("test")

	reaper := release.NewReleaseReaper(releaseStore, checker, locker, nil, logger, release.DefaultReleaseReaperConfig())

	// Act
	reaped := reaper.ReapOnce(context.Background())

	// Assert
	if reaped != 0 {
		t.Errorf("expected 0 reaped releases, got %d", reaped)
	}
}

func TestReleaseReaper_SkipsAlreadyAdvancedRelease(t *testing.T) {
	// Arrange: checker reports a release as stale, but by the time we fetch it,
	// it has already advanced to ACTIVE (another goroutine completed the sync)
	releaseStore := newFakeReleaseStoreOrch()
	releaseStore.releases["rel-advanced"] = &db.Release{
		ID:          "rel-advanced",
		BuildID:     "build-adv",
		TenantID:    "acme",
		ProjectID:   "proj1",
		Environment: "production",
		Status:      string(statemachine.RPhActive), // already advanced
		CreatedAt:   time.Now().Add(-10 * time.Minute),
	}

	checker := &fakeStaleReleaseChecker{
		releases: []release.StaleRelease{
			{
				ID:          "rel-advanced",
				State:       statemachine.RPhPending,
				Event:       statemachine.RPEventSyncTimeout,
				TenantID:    "acme",
				ProjectID:   "proj1",
				Environment: "production",
				BuildID:     "build-adv",
			},
		},
	}

	locker := &fakeEnvironmentLocker{}
	logger := logging.NewLogger("test")

	reaper := release.NewReleaseReaper(releaseStore, checker, locker, nil, logger, release.DefaultReleaseReaperConfig())

	// Act
	reaped := reaper.ReapOnce(context.Background())

	// Assert: should skip because release already advanced past reapable states
	if reaped != 0 {
		t.Errorf("expected 0 reaped releases, got %d", reaped)
	}

	// Verify release was not modified
	release := releaseStore.releases["rel-advanced"]
	if release.Status != string(statemachine.RPhActive) {
		t.Errorf("expected release to remain ACTIVE, got %s", release.Status)
	}
}

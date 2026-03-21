package unit

import (
	"context"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/pkg/logging"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// --- fake stale checker ---

type fakeStaleChecker struct {
	builds []services.StaleBuild
	err    error
}

func (f *fakeStaleChecker) FindStaleBuilds(_ context.Context, _, _, _ time.Duration) ([]services.StaleBuild, error) {
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
			Status:      string(statemachine.BuildRunning),
		},
	}

	checker := &fakeStaleChecker{
		builds: []services.StaleBuild{
			{ID: "build-stale-1", State: statemachine.BuildRunning, Event: statemachine.BuildTimeout},
		},
	}

	locker := &fakeEnvironmentLocker{}
	logger := logging.NewLogger("test")

	reaper := services.NewBuildReaper(buildStore, checker, locker, nil, nil, logger, services.BuildReaperConfig{
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
	if build.Status != string(statemachine.BuildFailed) {
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
			Status:      string(statemachine.BuildFailed),
		},
	}

	checker := &fakeStaleChecker{
		builds: []services.StaleBuild{
			{ID: "build-done", State: statemachine.BuildRunning, Event: statemachine.BuildTimeout},
		},
	}

	locker := &fakeEnvironmentLocker{}
	logger := logging.NewLogger("test")

	reaper := services.NewBuildReaper(buildStore, checker, locker, nil, nil, logger, services.DefaultBuildReaperConfig(), nil)

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
			Status:      string(statemachine.BuildQueued),
		},
	}

	checker := &fakeStaleChecker{
		builds: []services.StaleBuild{
			{ID: "build-queued", State: statemachine.BuildQueued, Event: statemachine.BuildQueueTimeout},
		},
	}

	locker := &fakeEnvironmentLocker{}
	logger := logging.NewLogger("test")

	reaper := services.NewBuildReaper(buildStore, checker, locker, nil, nil, logger, services.DefaultBuildReaperConfig(), nil)

	reaped := reaper.ReapOnce(context.Background())

	if reaped != 1 {
		t.Errorf("expected 1 reaped build, got %d", reaped)
	}

	build := buildStore.builds["build-queued"]
	if build.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected build status FAILED, got %s", build.Status)
	}
}

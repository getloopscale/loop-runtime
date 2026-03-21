package unit

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// --- Fakes for StartupReconciler tests ---

// fakeReconcilerBuildStore implements db.BuildStore for reconciler tests.
type fakeReconcilerBuildStore struct {
	builds       map[string]*db.Build
	activeBuilds []db.Build
	findErr      error
	getErr       error
	updateErr    error
}

func newFakeReconcilerBuildStore() *fakeReconcilerBuildStore {
	return &fakeReconcilerBuildStore{
		builds: make(map[string]*db.Build),
	}
}

func (f *fakeReconcilerBuildStore) Create(_ context.Context, _ db.DBTX, build *db.Build) error {
	f.builds[build.ID] = build
	return nil
}

func (f *fakeReconcilerBuildStore) GetByID(_ context.Context, id string) (*db.Build, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	b, ok := f.builds[id]
	if !ok {
		return nil, errors.New("build not found")
	}
	c := *b
	return &c, nil
}

func (f *fakeReconcilerBuildStore) HasActiveBuild(_ context.Context, _ db.DBTX, _, _, _ string) (bool, error) {
	return false, nil
}

func (f *fakeReconcilerBuildStore) UpdateStatus(_ context.Context, _ db.DBTX, id string, event string) (*db.Build, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	b, ok := f.builds[id]
	if !ok {
		return nil, errors.New("build not found")
	}
	newState, err := safeFakeTransition(buildMachine, statemachine.State(b.Status), statemachine.Event(event))
	if err != nil {
		return nil, err
	}
	b.Status = string(newState)
	c := *b
	return &c, nil
}

func (f *fakeReconcilerBuildStore) SetWorkflowID(_ context.Context, _ db.DBTX, id string, wfID string) error {
	b, ok := f.builds[id]
	if !ok {
		return errors.New("build not found")
	}
	b.WorkflowID = wfID
	return nil
}

func (f *fakeReconcilerBuildStore) SetErrorMessage(_ context.Context, _ db.DBTX, id string, msg string) error {
	b, ok := f.builds[id]
	if !ok {
		return errors.New("build not found")
	}
	b.ErrorMessage = msg
	return nil
}

func (f *fakeReconcilerBuildStore) ListByProject(_ context.Context, _, _ string, _, _ int) ([]db.Build, error) {
	return nil, nil
}

func (f *fakeReconcilerBuildStore) FindActiveBuilds(_ context.Context) ([]db.Build, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.activeBuilds, nil
}

func (f *fakeReconcilerBuildStore) SetAppsSpec(_ context.Context, _ db.DBTX, _ string, _ []db.BuildAppSpec) error {
	return nil
}

func (f *fakeReconcilerBuildStore) GetBuildApps(_ context.Context, _ string) ([]db.BuildAppSpec, error) {
	return nil, nil
}

func (f *fakeReconcilerBuildStore) CancelActiveBuilds(_ context.Context, _ db.DBTX, _, _, _ string) (int, error) {
	return 0, nil
}

// addBuild inserts a build into the store and optionally the active list.
func (f *fakeReconcilerBuildStore) addBuild(b *db.Build) {
	f.builds[b.ID] = b
	if b.Status == string(statemachine.BuildQueued) || b.Status == string(statemachine.BuildRunning) {
		f.activeBuilds = append(f.activeBuilds, *b)
	}
}

// fakeReconcilerWorkflow implements services.WorkflowSubmitter for reconciler tests.
type fakeReconcilerWorkflow struct {
	statuses map[string]*services.WorkflowStatus // workflowID → status (nil = not found)
	getErr   error
}

func newFakeReconcilerWorkflow() *fakeReconcilerWorkflow {
	return &fakeReconcilerWorkflow{
		statuses: make(map[string]*services.WorkflowStatus),
	}
}

func (f *fakeReconcilerWorkflow) Submit(_ context.Context, _ services.WorkflowSpec) (string, error) {
	return "wf-unused", nil
}

func (f *fakeReconcilerWorkflow) Cancel(_ context.Context, _ string) error {
	return nil
}

func (f *fakeReconcilerWorkflow) GetStatus(_ context.Context, workflowID string) (*services.WorkflowStatus, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	status, ok := f.statuses[workflowID]
	if !ok {
		return nil, nil // not found
	}
	return status, nil
}

// --- Tests ---

func TestStartupReconciler_NoOrphanedBuilds(t *testing.T) {
	// Arrange: no active builds
	store := newFakeReconcilerBuildStore()
	wf := newFakeReconcilerWorkflow()
	locker := &fakeEnvironmentLocker{}
	logger := slog.Default()

	reconciler := services.NewStartupReconciler(store, wf, locker, logger)

	// Act
	reconciled := reconciler.Reconcile(context.Background())

	// Assert
	if reconciled != 0 {
		t.Errorf("expected 0 reconciled builds, got %d", reconciled)
	}
}

func TestStartupReconciler_QueuedBuildWithNoWorkflow(t *testing.T) {
	// Arrange: build in QUEUED with no workflow ID (crashed before submission)
	store := newFakeReconcilerBuildStore()
	build := &db.Build{
		ID:          "build-orphan-1",
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Status:      string(statemachine.BuildQueued),
		WorkflowID:  "", // never submitted
	}
	store.addBuild(build)

	wf := newFakeReconcilerWorkflow()
	locker := &fakeEnvironmentLocker{}
	logger := slog.Default()

	reconciler := services.NewStartupReconciler(store, wf, locker, logger)

	// Act
	reconciled := reconciler.Reconcile(context.Background())

	// Assert
	if reconciled != 1 {
		t.Errorf("expected 1 reconciled build, got %d", reconciled)
	}

	// Build should be transitioned to FAILED
	updated, err := store.GetByID(context.Background(), "build-orphan-1")
	if err != nil {
		t.Fatalf("expected build in store, got error: %v", err)
	}
	if updated.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected build status FAILED, got %q", updated.Status)
	}
	if updated.ErrorMessage == "" {
		t.Error("expected error message to be set on failed build")
	}
}

func TestStartupReconciler_RunningBuildWithNoWorkflow(t *testing.T) {
	// Arrange: build in RUNNING but workflow not found (TTL cleanup)
	store := newFakeReconcilerBuildStore()
	build := &db.Build{
		ID:          "build-orphan-2",
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		Environment: "staging",
		Status:      string(statemachine.BuildRunning),
		WorkflowID:  "wf-gone",
	}
	store.addBuild(build)

	wf := newFakeReconcilerWorkflow()
	// wf-gone not in statuses → GetStatus returns nil, nil (not found)

	locker := &fakeEnvironmentLocker{}
	logger := slog.Default()

	reconciler := services.NewStartupReconciler(store, wf, locker, logger)

	// Act
	reconciled := reconciler.Reconcile(context.Background())

	// Assert
	if reconciled != 1 {
		t.Errorf("expected 1 reconciled build, got %d", reconciled)
	}

	updated, err := store.GetByID(context.Background(), "build-orphan-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected FAILED, got %q", updated.Status)
	}
}

func TestStartupReconciler_RunningBuildWithCompletedWorkflow(t *testing.T) {
	// Arrange: build in RUNNING, workflow Succeeded but callback lost
	store := newFakeReconcilerBuildStore()
	build := &db.Build{
		ID:          "build-orphan-3",
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Status:      string(statemachine.BuildRunning),
		WorkflowID:  "wf-succeeded",
	}
	store.addBuild(build)

	wf := newFakeReconcilerWorkflow()
	wf.statuses["wf-succeeded"] = &services.WorkflowStatus{Phase: "Succeeded"}

	locker := &fakeEnvironmentLocker{}
	logger := slog.Default()

	reconciler := services.NewStartupReconciler(store, wf, locker, logger)

	// Act
	reconciled := reconciler.Reconcile(context.Background())

	// Assert: should mark as FAILED (callback lost, can't complete activation)
	if reconciled != 1 {
		t.Errorf("expected 1 reconciled build, got %d", reconciled)
	}

	updated, err := store.GetByID(context.Background(), "build-orphan-3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected FAILED, got %q", updated.Status)
	}
	if updated.ErrorMessage == "" {
		t.Error("expected error message about lost callback")
	}
}

func TestStartupReconciler_RunningBuildWithFailedWorkflow(t *testing.T) {
	// Arrange: build in RUNNING, workflow Failed in Argo
	store := newFakeReconcilerBuildStore()
	build := &db.Build{
		ID:          "build-orphan-4",
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Status:      string(statemachine.BuildRunning),
		WorkflowID:  "wf-failed",
	}
	store.addBuild(build)

	wf := newFakeReconcilerWorkflow()
	wf.statuses["wf-failed"] = &services.WorkflowStatus{Phase: "Failed"}

	locker := &fakeEnvironmentLocker{}
	logger := slog.Default()

	reconciler := services.NewStartupReconciler(store, wf, locker, logger)

	// Act
	reconciled := reconciler.Reconcile(context.Background())

	// Assert
	if reconciled != 1 {
		t.Errorf("expected 1 reconciled build, got %d", reconciled)
	}

	updated, err := store.GetByID(context.Background(), "build-orphan-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected FAILED, got %q", updated.Status)
	}
}

func TestStartupReconciler_RunningBuildWithActiveWorkflow(t *testing.T) {
	// Arrange: build in RUNNING, workflow still Running → reattach (no-op)
	store := newFakeReconcilerBuildStore()
	build := &db.Build{
		ID:          "build-active-1",
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Status:      string(statemachine.BuildRunning),
		WorkflowID:  "wf-running",
	}
	store.addBuild(build)

	wf := newFakeReconcilerWorkflow()
	wf.statuses["wf-running"] = &services.WorkflowStatus{Phase: "Running"}

	locker := &fakeEnvironmentLocker{}
	logger := slog.Default()

	reconciler := services.NewStartupReconciler(store, wf, locker, logger)

	// Act
	reconciled := reconciler.Reconcile(context.Background())

	// Assert: reattach counts as reconciled (action != "")
	if reconciled != 1 {
		t.Errorf("expected 1 reconciled build (reattach), got %d", reconciled)
	}

	// Build status should NOT change — still RUNNING
	updated, err := store.GetByID(context.Background(), "build-active-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != string(statemachine.BuildRunning) {
		t.Errorf("expected RUNNING (unchanged), got %q", updated.Status)
	}
}

func TestStartupReconciler_BuildAlreadyTerminal_SkippedByFindActive(t *testing.T) {
	// Arrange: build already in terminal FAILED state — FindActiveBuilds should
	// NOT return it because it only returns QUEUED and RUNNING builds.
	store := newFakeReconcilerBuildStore()
	build := &db.Build{
		ID:          "build-terminal-1",
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Status:      string(statemachine.BuildFailed),
		WorkflowID:  "wf-done",
	}
	// Add to builds map but NOT to activeBuilds (status is terminal)
	store.builds[build.ID] = build

	wf := newFakeReconcilerWorkflow()
	locker := &fakeEnvironmentLocker{}
	logger := slog.Default()

	reconciler := services.NewStartupReconciler(store, wf, locker, logger)

	// Act
	reconciled := reconciler.Reconcile(context.Background())

	// Assert: terminal builds are not in the active list, so nothing to reconcile
	if reconciled != 0 {
		t.Errorf("expected 0 reconciled builds (terminal not in active list), got %d", reconciled)
	}
}

func TestStartupReconciler_BuildBecomesTerminal_BetweenFindAndLock(t *testing.T) {
	// Arrange: build starts as QUEUED (active), but transitions to FAILED before
	// the reconciler can process it inside the lock (concurrent cancel).
	// The re-fetch inside failBuild sees FAILED and skips the transition.
	store := newFakeReconcilerBuildStore()
	build := &db.Build{
		ID:          "build-race-1",
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Status:      string(statemachine.BuildQueued),
		WorkflowID:  "", // no workflow → triggers failBuild path
	}
	store.addBuild(build)

	wf := newFakeReconcilerWorkflow()
	locker := &fakeEnvironmentLocker{}
	logger := slog.Default()

	// Simulate race: between FindActiveBuilds and the lock acquisition,
	// another process moved the build to FAILED.
	store.builds["build-race-1"].Status = string(statemachine.BuildFailed)

	reconciler := services.NewStartupReconciler(store, wf, locker, logger)

	// Act
	reconciled := reconciler.Reconcile(context.Background())

	// Assert: failBuild runs, re-fetch inside lock sees FAILED, returns nil (no error),
	// but the action string is still set because failBuild always returns "failed: ..."
	// when err is nil. This is acceptable — the reconciler counts it as "handled".
	if reconciled != 1 {
		t.Errorf("expected 1 reconciled build (race handled gracefully), got %d", reconciled)
	}

	// Build should still be FAILED (not re-transitioned)
	updated, _ := store.GetByID(context.Background(), "build-race-1")
	if updated.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected FAILED (unchanged), got %q", updated.Status)
	}
}

func TestStartupReconciler_FindActiveBuildsError(t *testing.T) {
	// Arrange: database error on FindActiveBuilds
	store := newFakeReconcilerBuildStore()
	store.findErr = errors.New("database connection lost")

	wf := newFakeReconcilerWorkflow()
	locker := &fakeEnvironmentLocker{}
	logger := slog.Default()

	reconciler := services.NewStartupReconciler(store, wf, locker, logger)

	// Act
	reconciled := reconciler.Reconcile(context.Background())

	// Assert: graceful degradation, returns 0
	if reconciled != 0 {
		t.Errorf("expected 0 on database error, got %d", reconciled)
	}
}

func TestStartupReconciler_WorkflowGetStatusError(t *testing.T) {
	// Arrange: GetStatus returns an error for a specific workflow
	store := newFakeReconcilerBuildStore()
	build := &db.Build{
		ID:          "build-status-err",
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Status:      string(statemachine.BuildRunning),
		WorkflowID:  "wf-unreachable",
	}
	store.addBuild(build)

	wf := newFakeReconcilerWorkflow()
	wf.getErr = errors.New("argo API unreachable")

	locker := &fakeEnvironmentLocker{}
	logger := slog.Default()

	reconciler := services.NewStartupReconciler(store, wf, locker, logger)

	// Act
	reconciled := reconciler.Reconcile(context.Background())

	// Assert: error checking workflow → skip (no action taken)
	if reconciled != 0 {
		t.Errorf("expected 0 reconciled (workflow status error), got %d", reconciled)
	}

	// Build should remain unchanged
	updated, _ := store.GetByID(context.Background(), "build-status-err")
	if updated.Status != string(statemachine.BuildRunning) {
		t.Errorf("expected RUNNING (unchanged on error), got %q", updated.Status)
	}
}

func TestStartupReconciler_MultipleBuilds(t *testing.T) {
	// Arrange: mix of builds in different states
	store := newFakeReconcilerBuildStore()

	// Build 1: QUEUED with no workflow → should FAIL
	store.addBuild(&db.Build{
		ID: "build-multi-1", TenantID: "t1", ProjectID: "p1", Environment: "prod",
		Status: string(statemachine.BuildQueued), WorkflowID: "",
	})
	// Build 2: RUNNING with active workflow → should reattach
	store.addBuild(&db.Build{
		ID: "build-multi-2", TenantID: "t1", ProjectID: "p2", Environment: "staging",
		Status: string(statemachine.BuildRunning), WorkflowID: "wf-active",
	})
	// Build 3: RUNNING with missing workflow → should FAIL
	store.addBuild(&db.Build{
		ID: "build-multi-3", TenantID: "t2", ProjectID: "p1", Environment: "prod",
		Status: string(statemachine.BuildRunning), WorkflowID: "wf-missing",
	})

	wf := newFakeReconcilerWorkflow()
	wf.statuses["wf-active"] = &services.WorkflowStatus{Phase: "Running"}
	// wf-missing not in statuses → nil (not found)

	locker := &fakeEnvironmentLocker{}
	logger := slog.Default()

	reconciler := services.NewStartupReconciler(store, wf, locker, logger)

	// Act
	reconciled := reconciler.Reconcile(context.Background())

	// Assert: all 3 should be reconciled (2 failed + 1 reattach)
	if reconciled != 3 {
		t.Errorf("expected 3 reconciled builds, got %d", reconciled)
	}
}

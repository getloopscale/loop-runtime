package unit

import (
	"context"
	"fmt"
	"testing"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/services"
)

// --- Fakes for ProjectDeleter tests ---

// fakeDeleteBuildStore implements db.BuildStore (only CancelActiveBuilds is used by ProjectDeleter).
type fakeDeleteBuildStore struct {
	cancelledByEnv map[string]int // key: "tenant:project:env" -> count
	cancelErr      error
}

func newFakeDeleteBuildStore() *fakeDeleteBuildStore {
	return &fakeDeleteBuildStore{
		cancelledByEnv: make(map[string]int),
	}
}

func (f *fakeDeleteBuildStore) CancelActiveBuilds(_ context.Context, _ db.DBTX, tenantID, projectID, env string) (int, error) {
	if f.cancelErr != nil {
		return 0, f.cancelErr
	}
	key := fmt.Sprintf("%s:%s:%s", tenantID, projectID, env)
	count := f.cancelledByEnv[key]
	return count, nil
}

// Unused methods — satisfy the interface.
func (f *fakeDeleteBuildStore) Create(context.Context, db.DBTX, *db.Build) error        { return nil }
func (f *fakeDeleteBuildStore) GetByID(context.Context, string) (*db.Build, error)       { return nil, nil }
func (f *fakeDeleteBuildStore) HasActiveBuild(context.Context, db.DBTX, string, string, string) (bool, error) {
	return false, nil
}
func (f *fakeDeleteBuildStore) UpdateStatus(context.Context, db.DBTX, string, string) (*db.Build, error) {
	return nil, nil
}
func (f *fakeDeleteBuildStore) SetErrorMessage(context.Context, db.DBTX, string, string) error {
	return nil
}
func (f *fakeDeleteBuildStore) SetWorkflowID(context.Context, db.DBTX, string, string) error {
	return nil
}
func (f *fakeDeleteBuildStore) SetAppsSpec(context.Context, db.DBTX, string, []db.BuildAppSpec) error {
	return nil
}
func (f *fakeDeleteBuildStore) GetBuildApps(context.Context, string) ([]db.BuildAppSpec, error) {
	return nil, nil
}
func (f *fakeDeleteBuildStore) ListByProject(context.Context, string, string, int, int) ([]db.Build, error) {
	return nil, nil
}
func (f *fakeDeleteBuildStore) FindActiveBuilds(context.Context) ([]db.Build, error) {
	return nil, nil
}

// fakeDeleteEnvTargetStore implements db.EnvironmentTargetStore for delete tests.
type fakeDeleteEnvTargetStore struct {
	targets   []db.EnvironmentTarget
	listErr   error
	deleteErr error
	deleted   []string // track deleted environments
}

func newFakeDeleteEnvTargetStore() *fakeDeleteEnvTargetStore {
	return &fakeDeleteEnvTargetStore{}
}

func (f *fakeDeleteEnvTargetStore) ListByProject(_ context.Context, _, _ string) ([]db.EnvironmentTarget, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.targets, nil
}

func (f *fakeDeleteEnvTargetStore) DeleteTarget(_ context.Context, _ db.DBTX, _, _, env string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, env)
	return nil
}

func (f *fakeDeleteEnvTargetStore) SetDesired(context.Context, db.DBTX, string, string, string, string, string, string) error {
	return nil
}
func (f *fakeDeleteEnvTargetStore) SetActive(context.Context, db.DBTX, string, string, string, string) error {
	return nil
}
func (f *fakeDeleteEnvTargetStore) Get(context.Context, string, string, string) (*db.EnvironmentTarget, error) {
	return nil, nil
}

// fakeDeleteProjectStore implements services.ProjectDeleteProjectStore.
type fakeDeleteProjectStore struct {
	deleteErr error
	deleted   bool
}

func (f *fakeDeleteProjectStore) DeleteProject(_ context.Context, _, _ string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = true
	return nil
}

// fakeDeleteLocker implements services.EnvironmentLocker for delete tests.
// Executes fn immediately with nil tx (no real lock).
type fakeDeleteLocker struct {
	lockErr error
}

func (f *fakeDeleteLocker) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	if f.lockErr != nil {
		return f.lockErr
	}
	return fn(nil)
}

// fakeDeleteGitOps implements services.GitOpsPusher for delete tests.
type fakeDeleteGitOps struct {
	removeErr    error
	removeCalled []services.GitOpsRemoveOpts
}

func (f *fakeDeleteGitOps) Push(_ context.Context, _ services.GitOpsPushOpts) (string, error) {
	return "fake-sha", nil
}

func (f *fakeDeleteGitOps) Remove(_ context.Context, opts services.GitOpsRemoveOpts) (string, error) {
	f.removeCalled = append(f.removeCalled, opts)
	if f.removeErr != nil {
		return "", f.removeErr
	}
	return "fake-remove-sha", nil
}

// --- Tests ---

func TestProjectDeleterDeleteSingleEnvironment(t *testing.T) {
	// Arrange
	buildStore := newFakeDeleteBuildStore()
	buildStore.cancelledByEnv["tenant-1:proj-abc:production"] = 2

	envTargetStore := newFakeDeleteEnvTargetStore()
	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}

	deleter := services.NewProjectDeleter(services.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := routes.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "production",
		DeletedBy:     "user-1",
		CorrelationID: "corr-001",
	}

	// Act
	result, err := deleter.Delete(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ProjectName != "my-saas" {
		t.Errorf("expected project name my-saas, got %q", result.ProjectName)
	}
	if len(result.DeletedEnvironments) != 1 {
		t.Fatalf("expected 1 deleted environment, got %d", len(result.DeletedEnvironments))
	}
	if result.DeletedEnvironments[0] != "production" {
		t.Errorf("expected deleted environment production, got %q", result.DeletedEnvironments[0])
	}
	if result.CancelledBuilds != 2 {
		t.Errorf("expected 2 cancelled builds, got %d", result.CancelledBuilds)
	}
	// Should NOT delete the project record (single env delete)
	if projectStore.deleted {
		t.Error("project record should NOT be deleted for single-env delete")
	}
	// GitOps remove should be called
	if len(gitops.removeCalled) != 1 {
		t.Fatalf("expected 1 gitops remove call, got %d", len(gitops.removeCalled))
	}
	if gitops.removeCalled[0].Environment != "production" {
		t.Errorf("expected gitops remove for production, got %q", gitops.removeCalled[0].Environment)
	}
	if gitops.removeCalled[0].CorrelationID != "corr-001" {
		t.Errorf("expected correlation-id corr-001, got %q", gitops.removeCalled[0].CorrelationID)
	}
}

func TestProjectDeleterDeleteAllEnvironments(t *testing.T) {
	// Arrange
	buildStore := newFakeDeleteBuildStore()
	buildStore.cancelledByEnv["tenant-1:proj-abc:production"] = 1
	buildStore.cancelledByEnv["tenant-1:proj-abc:staging"] = 3

	envTargetStore := newFakeDeleteEnvTargetStore()
	envTargetStore.targets = []db.EnvironmentTarget{
		{TenantID: "tenant-1", ProjectID: "proj-abc", Environment: "production"},
		{TenantID: "tenant-1", ProjectID: "proj-abc", Environment: "staging"},
	}

	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}

	deleter := services.NewProjectDeleter(services.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := routes.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "", // empty = all environments
		DeletedBy:     "user-1",
		CorrelationID: "corr-002",
	}

	// Act
	result, err := deleter.Delete(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result.DeletedEnvironments) != 2 {
		t.Fatalf("expected 2 deleted environments, got %d", len(result.DeletedEnvironments))
	}
	if result.CancelledBuilds != 4 {
		t.Errorf("expected 4 cancelled builds (1+3), got %d", result.CancelledBuilds)
	}
	// Should delete the project record (all envs)
	if !projectStore.deleted {
		t.Error("project record should be deleted when all envs are removed")
	}
	// GitOps remove should be called for each env
	if len(gitops.removeCalled) != 2 {
		t.Fatalf("expected 2 gitops remove calls, got %d", len(gitops.removeCalled))
	}
}

func TestProjectDeleterDeleteNoActiveBuilds(t *testing.T) {
	// Arrange
	buildStore := newFakeDeleteBuildStore()
	// No active builds for this env

	envTargetStore := newFakeDeleteEnvTargetStore()
	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}

	deleter := services.NewProjectDeleter(services.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := routes.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "production",
		DeletedBy:     "user-1",
		CorrelationID: "corr-003",
	}

	// Act
	result, err := deleter.Delete(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.CancelledBuilds != 0 {
		t.Errorf("expected 0 cancelled builds, got %d", result.CancelledBuilds)
	}
	if len(result.DeletedEnvironments) != 1 {
		t.Errorf("expected 1 deleted environment, got %d", len(result.DeletedEnvironments))
	}
}

func TestProjectDeleterLockFailure(t *testing.T) {
	// Arrange
	buildStore := newFakeDeleteBuildStore()
	envTargetStore := newFakeDeleteEnvTargetStore()
	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{lockErr: fmt.Errorf("lock timeout")}
	gitops := &fakeDeleteGitOps{}

	deleter := services.NewProjectDeleter(services.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := routes.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "production",
		DeletedBy:     "user-1",
		CorrelationID: "corr-004",
	}

	// Act
	_, err := deleter.Delete(context.Background(), req)

	// Assert
	if err == nil {
		t.Fatal("expected error when lock fails, got nil")
	}
}

func TestProjectDeleterCancelBuildError(t *testing.T) {
	// Arrange
	buildStore := newFakeDeleteBuildStore()
	buildStore.cancelErr = fmt.Errorf("db connection lost")

	envTargetStore := newFakeDeleteEnvTargetStore()
	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}

	deleter := services.NewProjectDeleter(services.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := routes.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "production",
		DeletedBy:     "user-1",
		CorrelationID: "corr-005",
	}

	// Act
	_, err := deleter.Delete(context.Background(), req)

	// Assert
	if err == nil {
		t.Fatal("expected error when cancel builds fails, got nil")
	}
}

func TestProjectDeleterGitOpsFailureIsNonFatal(t *testing.T) {
	// Arrange — GitOps remove fails, but the operation should still succeed
	// because DB state is already clean. This follows the BLOCKER-2 pattern.
	buildStore := newFakeDeleteBuildStore()
	envTargetStore := newFakeDeleteEnvTargetStore()
	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{removeErr: fmt.Errorf("gitea unreachable")}

	deleter := services.NewProjectDeleter(services.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := routes.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "production",
		DeletedBy:     "user-1",
		CorrelationID: "corr-006",
	}

	// Act
	result, err := deleter.Delete(context.Background(), req)

	// Assert — should succeed despite GitOps failure
	if err != nil {
		t.Fatalf("expected no error (gitops failure is non-fatal), got %v", err)
	}
	if len(result.DeletedEnvironments) != 1 {
		t.Errorf("expected 1 deleted environment, got %d", len(result.DeletedEnvironments))
	}
}

func TestProjectDeleterDeleteAllEnvsProjectDeleteFails(t *testing.T) {
	// Arrange — project record deletion fails, but environments are already cleaned.
	// This is non-fatal (logged as warning).
	buildStore := newFakeDeleteBuildStore()
	envTargetStore := newFakeDeleteEnvTargetStore()
	envTargetStore.targets = []db.EnvironmentTarget{
		{TenantID: "tenant-1", ProjectID: "proj-abc", Environment: "production"},
	}

	projectStore := &fakeDeleteProjectStore{deleteErr: fmt.Errorf("db error")}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}

	deleter := services.NewProjectDeleter(services.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := routes.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "", // all envs
		DeletedBy:     "user-1",
		CorrelationID: "corr-007",
	}

	// Act
	result, err := deleter.Delete(context.Background(), req)

	// Assert — should succeed (project delete failure is non-fatal)
	if err != nil {
		t.Fatalf("expected no error (project delete failure is non-fatal), got %v", err)
	}
	if len(result.DeletedEnvironments) != 1 {
		t.Errorf("expected 1 deleted environment, got %d", len(result.DeletedEnvironments))
	}
}

func TestProjectDeleterListByProjectError(t *testing.T) {
	// Arrange
	buildStore := newFakeDeleteBuildStore()
	envTargetStore := newFakeDeleteEnvTargetStore()
	envTargetStore.listErr = fmt.Errorf("connection refused")

	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}

	deleter := services.NewProjectDeleter(services.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := routes.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "", // all envs → triggers ListByProject
		DeletedBy:     "user-1",
		CorrelationID: "corr-008",
	}

	// Act
	_, err := deleter.Delete(context.Background(), req)

	// Assert
	if err == nil {
		t.Fatal("expected error when ListByProject fails, got nil")
	}
}

func TestProjectDeleterDeleteTargetError(t *testing.T) {
	// Arrange — DeleteTarget fails inside the lock
	buildStore := newFakeDeleteBuildStore()
	envTargetStore := newFakeDeleteEnvTargetStore()
	envTargetStore.deleteErr = fmt.Errorf("FK constraint violation")

	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}

	deleter := services.NewProjectDeleter(services.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := routes.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "production",
		DeletedBy:     "user-1",
		CorrelationID: "corr-009",
	}

	// Act
	_, err := deleter.Delete(context.Background(), req)

	// Assert
	if err == nil {
		t.Fatal("expected error when DeleteTarget fails, got nil")
	}
}

func TestProjectDeleterDeleteAllEnvsNoEnvironments(t *testing.T) {
	// Arrange — project has no environments (already cleaned or never deployed)
	buildStore := newFakeDeleteBuildStore()
	envTargetStore := newFakeDeleteEnvTargetStore()
	envTargetStore.targets = []db.EnvironmentTarget{} // empty

	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}

	deleter := services.NewProjectDeleter(services.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := routes.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "", // all envs
		DeletedBy:     "user-1",
		CorrelationID: "corr-010",
	}

	// Act
	result, err := deleter.Delete(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result.DeletedEnvironments) != 0 {
		t.Errorf("expected 0 deleted environments, got %d", len(result.DeletedEnvironments))
	}
	// Even with no environments, the project record should be deleted
	if !projectStore.deleted {
		t.Error("project record should be deleted when all (zero) envs are removed")
	}
	// No gitops calls
	if len(gitops.removeCalled) != 0 {
		t.Errorf("expected 0 gitops remove calls, got %d", len(gitops.removeCalled))
	}
}

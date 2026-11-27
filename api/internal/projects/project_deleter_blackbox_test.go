// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package projects_test

import (
	"context"
	"fmt"
	"github.com/getloopscale/loop-runtime/api/internal/projects"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// --- Fakes for ProjectDeleter tests ---

// fakeDeleteBuildStore implements db.BuildStore (only CancelActiveBuilds is used by ProjectDeleter).
type fakeDeleteBuildStore struct {
	cancelledByEnv map[string]int      // key: "tenant:project:env" -> count
	workflowsByEnv map[string][]string // key: "tenant:project:env" -> workflow IDs
	cancelErr      error
}

func newFakeDeleteBuildStore() *fakeDeleteBuildStore {
	return &fakeDeleteBuildStore{
		cancelledByEnv: make(map[string]int),
		workflowsByEnv: make(map[string][]string),
	}
}

func (f *fakeDeleteBuildStore) CancelActiveBuilds(_ context.Context, _ db.DBTX, tenantID, projectID, env string) (int, []string, error) {
	if f.cancelErr != nil {
		return 0, nil, f.cancelErr
	}
	key := fmt.Sprintf("%s:%s:%s", tenantID, projectID, env)
	count := f.cancelledByEnv[key]
	wfIDs := f.workflowsByEnv[key]
	return count, wfIDs, nil
}

// Unused methods — satisfy the interface.
func (f *fakeDeleteBuildStore) Create(context.Context, db.DBTX, *db.Build) error   { return nil }
func (f *fakeDeleteBuildStore) GetByID(context.Context, string) (*db.Build, error) { return nil, nil }
func (f *fakeDeleteBuildStore) GetByIDForTenant(context.Context, string, string) (*db.Build, error) {
	return nil, nil
}
func (f *fakeDeleteBuildStore) HasActiveBuild(context.Context, db.DBTX, string, string, string) (bool, error) {
	return false, nil
}
func (f *fakeDeleteBuildStore) UpdateStatus(context.Context, db.DBTX, string, string) (*db.Build, error) {
	return nil, nil
}

func (f *fakeDeleteBuildStore) UpdateStatusIfCurrent(_ context.Context, _, _ string, _ statemachine.BuildPhase, _ string) (*db.Build, error) {
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
func (f *fakeDeleteBuildStore) ListByProjectAndEnv(context.Context, string, string, string, int, int) ([]db.Build, int, error) {
	return nil, 0, nil
}
func (f *fakeDeleteBuildStore) ListEnvsForProject(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (f *fakeDeleteBuildStore) FindActiveBuilds(context.Context, string) ([]db.Build, error) {
	return nil, nil
}

// fakeDeleteEnvTargetStore implements store.EnvironmentTargetStore for delete tests.
type fakeDeleteEnvTargetStore struct {
	targets   []store.EnvironmentTarget
	listErr   error
	deleteErr error
	deleted   []string // track deleted environments
}

func newFakeDeleteEnvTargetStore() *fakeDeleteEnvTargetStore {
	return &fakeDeleteEnvTargetStore{}
}

func (f *fakeDeleteEnvTargetStore) ListByProject(_ context.Context, _, _ string) ([]store.EnvironmentTarget, error) {
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
func (f *fakeDeleteEnvTargetStore) Get(context.Context, string, string, string) (*store.EnvironmentTarget, error) {
	return nil, nil
}

// 2026-06-07 D2/D5/D6/D7 — preview-as-category interface extensions.
func (f *fakeDeleteEnvTargetStore) SetPreviewDesired(context.Context, db.DBTX, string, string, string, string, string, string) error {
	return nil
}
func (f *fakeDeleteEnvTargetStore) CountPermanent(context.Context, string) (int, error) {
	return 0, nil
}
func (f *fakeDeleteEnvTargetStore) ListPermanent(context.Context, string) ([]string, error) {
	return []string{}, nil
}
func (f *fakeDeleteEnvTargetStore) DeleteByKind(context.Context, db.DBTX, string, string, string, string) error {
	return nil
}

// fakeDeleteProjectStore implements platform.ProjectDeleteProjectStore.
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

// fakeDeleteLocker implements deploycontracts.EnvironmentLocker for delete tests.
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

// fakeDeleteGitOps implements deploycontracts.GitOpsPusher for delete tests.
type fakeDeleteGitOps struct {
	removeErr    error
	removeCalled []deploycontracts.GitOpsRemoveOpts
}

func (f *fakeDeleteGitOps) Push(_ context.Context, _ deploycontracts.GitOpsPushOpts) (string, error) {
	return "fake-sha", nil
}

func (f *fakeDeleteGitOps) Remove(_ context.Context, opts deploycontracts.GitOpsRemoveOpts) (string, error) {
	f.removeCalled = append(f.removeCalled, opts)
	if f.removeErr != nil {
		return "", f.removeErr
	}
	return "fake-remove-sha", nil
}

// fakeWorkflowCanceller implements platform.WorkflowCanceller for delete tests.
type fakeWorkflowCanceller struct {
	cancelledIDs []string
	cancelErr    error
}

func (f *fakeWorkflowCanceller) Cancel(_ context.Context, _, workflowID string) error {
	f.cancelledIDs = append(f.cancelledIDs, workflowID)
	if f.cancelErr != nil {
		return f.cancelErr
	}
	return nil
}

// --- Tests ---

func TestProjectDeleterDeleteSingleEnvironment(t *testing.T) {
	// Arrange
	buildStore := newFakeDeleteBuildStore()
	buildStore.cancelledByEnv["tenant-1:my-saas:production"] = 2

	envTargetStore := newFakeDeleteEnvTargetStore()
	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := projects.ProjectDeleteInput{
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
	buildStore.cancelledByEnv["tenant-1:my-saas:production"] = 1
	buildStore.cancelledByEnv["tenant-1:my-saas:staging"] = 3

	envTargetStore := newFakeDeleteEnvTargetStore()
	envTargetStore.targets = []store.EnvironmentTarget{
		{TenantID: "tenant-1", ProjectID: "my-saas", Environment: "production"},
		{TenantID: "tenant-1", ProjectID: "my-saas", Environment: "staging"},
	}

	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := projects.ProjectDeleteInput{
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

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := projects.ProjectDeleteInput{
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

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := projects.ProjectDeleteInput{
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

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := projects.ProjectDeleteInput{
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

func TestProjectDeleterGitOpsFailureIsFatal(t *testing.T) {
	// Arrange — GitOps remove fails. The delete must FAIL because leaving
	// orphaned manifests in the gitops repo degrades ArgoCD Application health
	// and blocks notifications for all projects in the tenant.
	buildStore := newFakeDeleteBuildStore()
	envTargetStore := newFakeDeleteEnvTargetStore()
	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{removeErr: fmt.Errorf("gitea unreachable")}

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := projects.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "production",
		DeletedBy:     "user-1",
		CorrelationID: "corr-006",
	}

	// Act
	_, err := deleter.Delete(context.Background(), req)

	// Assert — must fail when gitops remove fails
	if err == nil {
		t.Fatal("expected error when gitops remove fails, got nil")
	}
	if !strings.Contains(err.Error(), "gitops remove failed") {
		t.Errorf("expected gitops remove error, got: %v", err)
	}
}

func TestProjectDeleterDeleteAllEnvsProjectDeleteFails(t *testing.T) {
	// Arrange — project record deletion fails, but environments are already cleaned.
	// This is non-fatal (logged as warning).
	buildStore := newFakeDeleteBuildStore()
	envTargetStore := newFakeDeleteEnvTargetStore()
	envTargetStore.targets = []store.EnvironmentTarget{
		{TenantID: "tenant-1", ProjectID: "my-saas", Environment: "production"},
	}

	projectStore := &fakeDeleteProjectStore{deleteErr: fmt.Errorf("db error")}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := projects.ProjectDeleteInput{
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

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := projects.ProjectDeleteInput{
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

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := projects.ProjectDeleteInput{
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
	envTargetStore.targets = []store.EnvironmentTarget{} // empty

	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:     buildStore,
		EnvTargetStore: envTargetStore,
		ProjectStore:   projectStore,
		Locker:         locker,
		GitOps:         gitops,
	})

	req := projects.ProjectDeleteInput{
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
	// Fallback: even with no environment_targets, gitops.Remove is called for "production"
	// to clean up manifests that may exist in the GitOps repo without a DB record.
	if len(result.DeletedEnvironments) != 1 || result.DeletedEnvironments[0] != "production" {
		t.Errorf("expected fallback delete of [production], got %v", result.DeletedEnvironments)
	}
	// Even with no environments, the project record should be deleted
	if !projectStore.deleted {
		t.Error("project record should be deleted when all (zero) envs are removed")
	}
	// Fallback gitops remove for production
	if len(gitops.removeCalled) != 1 {
		t.Errorf("expected 1 fallback gitops remove call, got %d", len(gitops.removeCalled))
	}
	if len(gitops.removeCalled) == 1 && gitops.removeCalled[0].Environment != "production" {
		t.Errorf("expected fallback remove for production, got %s", gitops.removeCalled[0].Environment)
	}
}

func TestProjectDeleterCancelsActiveWorkflows(t *testing.T) {
	// Arrange: 2 builds RUNNING with workflow IDs
	buildStore := newFakeDeleteBuildStore()
	buildStore.cancelledByEnv["tenant-1:my-saas:production"] = 2
	buildStore.workflowsByEnv["tenant-1:my-saas:production"] = []string{"wf-aaa", "wf-bbb"}

	envTargetStore := newFakeDeleteEnvTargetStore()
	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}
	wfCanceller := &fakeWorkflowCanceller{}

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:        buildStore,
		EnvTargetStore:    envTargetStore,
		ProjectStore:      projectStore,
		Locker:            locker,
		GitOps:            gitops,
		WorkflowCanceller: wfCanceller,
	})

	req := projects.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "production",
		DeletedBy:     "user-1",
		CorrelationID: "corr-wf-1",
	}

	// Act
	result, err := deleter.Delete(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.CancelledBuilds != 2 {
		t.Errorf("expected 2 cancelled builds, got %d", result.CancelledBuilds)
	}
	if len(wfCanceller.cancelledIDs) != 2 {
		t.Fatalf("expected 2 workflow cancellations, got %d", len(wfCanceller.cancelledIDs))
	}
	if wfCanceller.cancelledIDs[0] != "wf-aaa" || wfCanceller.cancelledIDs[1] != "wf-bbb" {
		t.Errorf("expected workflows [wf-aaa, wf-bbb], got %v", wfCanceller.cancelledIDs)
	}
}

func TestProjectDeleterSkipsBuildWithoutWorkflowID(t *testing.T) {
	// Arrange: 1 build QUEUED without workflow_id (empty string filtered out by store)
	buildStore := newFakeDeleteBuildStore()
	buildStore.cancelledByEnv["tenant-1:my-saas:production"] = 1
	// No workflow IDs — build was QUEUED, never got a workflow assigned

	envTargetStore := newFakeDeleteEnvTargetStore()
	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}
	wfCanceller := &fakeWorkflowCanceller{}

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:        buildStore,
		EnvTargetStore:    envTargetStore,
		ProjectStore:      projectStore,
		Locker:            locker,
		GitOps:            gitops,
		WorkflowCanceller: wfCanceller,
	})

	req := projects.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "production",
		DeletedBy:     "user-1",
		CorrelationID: "corr-wf-2",
	}

	// Act
	_, err := deleter.Delete(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(wfCanceller.cancelledIDs) != 0 {
		t.Errorf("expected 0 workflow cancellations (no workflow IDs), got %d", len(wfCanceller.cancelledIDs))
	}
}

func TestProjectDeleterContinuesOnWorkflowCancelFailure(t *testing.T) {
	// Arrange: workflow cancel fails but delete should still succeed
	buildStore := newFakeDeleteBuildStore()
	buildStore.cancelledByEnv["tenant-1:my-saas:production"] = 1
	buildStore.workflowsByEnv["tenant-1:my-saas:production"] = []string{"wf-failing"}

	envTargetStore := newFakeDeleteEnvTargetStore()
	projectStore := &fakeDeleteProjectStore{}
	locker := &fakeDeleteLocker{}
	gitops := &fakeDeleteGitOps{}
	wfCanceller := &fakeWorkflowCanceller{cancelErr: fmt.Errorf("argo: connection refused")}

	deleter := projects.NewProjectDeleter(projects.ProjectDeleterDeps{
		BuildStore:        buildStore,
		EnvTargetStore:    envTargetStore,
		ProjectStore:      projectStore,
		Locker:            locker,
		GitOps:            gitops,
		WorkflowCanceller: wfCanceller,
	})

	req := projects.ProjectDeleteInput{
		TenantID:      "tenant-1",
		ProjectID:     "proj-abc",
		ProjectName:   "my-saas",
		Environment:   "production",
		DeletedBy:     "user-1",
		CorrelationID: "corr-wf-3",
	}

	// Act
	result, err := deleter.Delete(context.Background(), req)

	// Assert — delete succeeds even though workflow cancel failed (best-effort)
	if err != nil {
		t.Fatalf("expected no error (best-effort cancel), got %v", err)
	}
	if result.CancelledBuilds != 1 {
		t.Errorf("expected 1 cancelled build, got %d", result.CancelledBuilds)
	}
	// Workflow cancel was attempted
	if len(wfCanceller.cancelledIDs) != 1 {
		t.Errorf("expected 1 cancel attempt, got %d", len(wfCanceller.cancelledIDs))
	}
}

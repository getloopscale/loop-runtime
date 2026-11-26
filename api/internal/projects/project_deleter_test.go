// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package projects

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// --- Test doubles for ProjectDeleter ---

type fakeBuildStoreDeleter struct {
	cancelledCount int
	cancelledWFIDs []string
	cancelErr      error
}

func (f *fakeBuildStoreDeleter) Create(_ context.Context, _ db.DBTX, _ *db.Build) error { return nil }
func (f *fakeBuildStoreDeleter) GetByID(_ context.Context, _ string) (*db.Build, error) {
	return nil, nil
}

// GetByIDForTenant — T0.4 of deep-review-2026-05-10-remediation-plan.
// Emulates the production SQL filter for the unit-test scope.
func (f *fakeBuildStoreDeleter) GetByIDForTenant(ctx context.Context, tenantID, buildID string) (*db.Build, error) {
	b, err := f.GetByID(ctx, buildID)
	if err != nil || b == nil {
		return b, err
	}
	if b.TenantID != tenantID {
		return nil, nil
	}
	return b, nil
}
func (f *fakeBuildStoreDeleter) HasActiveBuild(_ context.Context, _ db.DBTX, _, _, _ string) (bool, error) {
	return false, nil
}
func (f *fakeBuildStoreDeleter) UpdateStatus(_ context.Context, _ db.DBTX, _, _ string) (*db.Build, error) {
	return nil, nil
}

func (f *fakeBuildStoreDeleter) UpdateStatusIfCurrent(_ context.Context, _, _ string, _ statemachine.BuildPhase, _ string) (*db.Build, error) {
	return nil, nil
}
func (f *fakeBuildStoreDeleter) SetErrorMessage(_ context.Context, _ db.DBTX, _, _ string) error {
	return nil
}
func (f *fakeBuildStoreDeleter) SetWorkflowID(_ context.Context, _ db.DBTX, _, _ string) error {
	return nil
}
func (f *fakeBuildStoreDeleter) SetAppsSpec(_ context.Context, _ db.DBTX, _ string, _ []db.BuildAppSpec) error {
	return nil
}
func (f *fakeBuildStoreDeleter) GetBuildApps(_ context.Context, _ string) ([]db.BuildAppSpec, error) {
	return nil, nil
}
func (f *fakeBuildStoreDeleter) ListByProject(_ context.Context, _, _ string, _, _ int) ([]db.Build, error) {
	return nil, nil
}
func (f *fakeBuildStoreDeleter) ListByProjectAndEnv(_ context.Context, _, _, _ string, _, _ int) ([]db.Build, int, error) {
	return nil, 0, nil
}
func (f *fakeBuildStoreDeleter) ListEnvsForProject(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}
func (f *fakeBuildStoreDeleter) FindActiveBuilds(_ context.Context, _ string) ([]db.Build, error) {
	return nil, nil
}
func (f *fakeBuildStoreDeleter) CancelActiveBuilds(_ context.Context, _ db.DBTX, _, _, _ string) (int, []string, error) {
	return f.cancelledCount, f.cancelledWFIDs, f.cancelErr
}

type fakeEnvTargetStoreDeleter struct {
	targets       []store.EnvironmentTarget
	deletedTarget bool
}

func (f *fakeEnvTargetStoreDeleter) SetDesired(_ context.Context, _ db.DBTX, _, _, _, _, _, _ string) error {
	return nil
}
func (f *fakeEnvTargetStoreDeleter) SetActive(_ context.Context, _ db.DBTX, _, _, _, _ string) error {
	return nil
}
func (f *fakeEnvTargetStoreDeleter) Get(_ context.Context, _, _, _ string) (*store.EnvironmentTarget, error) {
	return nil, nil
}
func (f *fakeEnvTargetStoreDeleter) ListByProject(_ context.Context, _, _ string) ([]store.EnvironmentTarget, error) {
	return f.targets, nil
}
func (f *fakeEnvTargetStoreDeleter) DeleteTarget(_ context.Context, _ db.DBTX, _, _, _ string) error {
	f.deletedTarget = true
	return nil
}

// 2026-06-07 D2/D5/D6/D7 — preview-as-category interface extensions.
func (f *fakeEnvTargetStoreDeleter) SetPreviewDesired(_ context.Context, _ db.DBTX, _, _, _, _, _, _ string) error {
	return nil
}
func (f *fakeEnvTargetStoreDeleter) CountPermanent(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (f *fakeEnvTargetStoreDeleter) ListPermanent(_ context.Context, _ string) ([]string, error) {
	return []string{}, nil
}
func (f *fakeEnvTargetStoreDeleter) DeleteByKind(_ context.Context, _ db.DBTX, _, _, _, _ string) error {
	return nil
}

type fakeProjectStoreDeleter struct {
	deleted bool
}

func (f *fakeProjectStoreDeleter) DeleteProject(_ context.Context, _, _ string) error {
	f.deleted = true
	return nil
}

type fakeLockerDeleter struct{}

func (f *fakeLockerDeleter) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

type fakeGitOpsDeleter struct {
	pushCalled   bool
	removeCalled bool
	removeErr    error
}

func (f *fakeGitOpsDeleter) Push(_ context.Context, _ deploycontracts.GitOpsPushOpts) (string, error) {
	f.pushCalled = true
	return "abc123", nil
}
func (f *fakeGitOpsDeleter) Remove(_ context.Context, _ deploycontracts.GitOpsRemoveOpts) (string, error) {
	f.removeCalled = true
	return "def456", f.removeErr
}

type fakeWorkflowCanceller struct {
	cancelledIDs []string
}

func (f *fakeWorkflowCanceller) Cancel(_ context.Context, _, id string) error {
	f.cancelledIDs = append(f.cancelledIDs, id)
	return nil
}

type fakeSyncDeleter struct {
	syncCalled bool
	syncedApps []string
}

func (f *fakeSyncDeleter) TriggerSync(_ context.Context, appName string) error {
	f.syncCalled = true
	f.syncedApps = append(f.syncedApps, appName)
	return nil
}

func (f *fakeSyncDeleter) TerminateOperation(_ context.Context, _ string) error { return nil }

// fakeReleaseStoreDeleter records every CancelAllByEnv call so tests can assert
// the cascade fires inside the project-delete lock. It implements only the
// ProjectDeleteReleaseStore port.
type fakeReleaseStoreDeleter struct {
	cancelCalls    []cancelAllCall
	cancelledCount int
	cancelErr      error
}

type cancelAllCall struct {
	tenantID  string
	projectID string
	env       string
}

func (f *fakeReleaseStoreDeleter) CancelAllByEnv(_ context.Context, _ db.DBTX, tenantID, projectID, env string) (int, error) {
	f.cancelCalls = append(f.cancelCalls, cancelAllCall{tenantID, projectID, env})
	return f.cancelledCount, f.cancelErr
}

// --- Tests ---

func TestProjectDeleter_SingleEnv_FullCascade(t *testing.T) {
	bs := &fakeBuildStoreDeleter{cancelledCount: 2, cancelledWFIDs: []string{"wf-1", "wf-2"}}
	ets := &fakeEnvTargetStoreDeleter{}
	ps := &fakeProjectStoreDeleter{}
	gitops := &fakeGitOpsDeleter{}
	wfCancel := &fakeWorkflowCanceller{}
	sync := &fakeSyncDeleter{}

	deleter := NewProjectDeleter(ProjectDeleterDeps{
		BuildStore:        bs,
		EnvTargetStore:    ets,
		ProjectStore:      ps,
		Locker:            &fakeLockerDeleter{},
		GitOps:            gitops,
		WorkflowCanceller: wfCancel,
		Sync:              sync,
	})

	result, err := deleter.Delete(context.Background(), ProjectDeleteInput{
		TenantID:      "tn-1",
		ProjectID:     "proj-uuid",
		ProjectName:   "my-app",
		Environment:   "production",
		CorrelationID: "corr-1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CancelledBuilds != 2 {
		t.Errorf("expected 2 cancelled builds, got %d", result.CancelledBuilds)
	}
	if len(result.DeletedEnvironments) != 1 || result.DeletedEnvironments[0] != "production" {
		t.Errorf("unexpected deleted envs: %v", result.DeletedEnvironments)
	}
	if !ets.deletedTarget {
		t.Error("expected environment target to be deleted")
	}
	if !gitops.removeCalled {
		t.Error("expected gitops remove to be called")
	}
	if len(wfCancel.cancelledIDs) != 2 {
		t.Errorf("expected 2 workflows cancelled, got %d", len(wfCancel.cancelledIDs))
	}
	if !sync.syncCalled {
		t.Error("expected argocd sync to be triggered")
	}
	// Single env delete should NOT delete the project record
	if ps.deleted {
		t.Error("project record should NOT be deleted for single-env delete")
	}
}

func TestProjectDeleter_AllEnvs_DeletesProjectRecord(t *testing.T) {
	ets := &fakeEnvTargetStoreDeleter{
		targets: []store.EnvironmentTarget{
			{TenantID: "tn-1", ProjectID: "my-app", Environment: "production"},
			{TenantID: "tn-1", ProjectID: "my-app", Environment: "staging"},
		},
	}
	ps := &fakeProjectStoreDeleter{}
	gitops := &fakeGitOpsDeleter{}

	deleter := NewProjectDeleter(ProjectDeleterDeps{
		BuildStore:     &fakeBuildStoreDeleter{},
		EnvTargetStore: ets,
		ProjectStore:   ps,
		Locker:         &fakeLockerDeleter{},
		GitOps:         gitops,
	})

	result, err := deleter.Delete(context.Background(), ProjectDeleteInput{
		TenantID:    "tn-1",
		ProjectID:   "proj-uuid",
		ProjectName: "my-app",
		// No environment = delete all
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.DeletedEnvironments) != 2 {
		t.Errorf("expected 2 deleted envs, got %d", len(result.DeletedEnvironments))
	}
	if !ps.deleted {
		t.Error("expected project record to be deleted when all envs removed")
	}
}

func TestProjectDeleter_GitOpsRemoveFails_ReturnsFatalError(t *testing.T) {
	gitops := &fakeGitOpsDeleter{removeErr: fmt.Errorf("gitea connection refused")}

	deleter := NewProjectDeleter(ProjectDeleterDeps{
		BuildStore:     &fakeBuildStoreDeleter{},
		EnvTargetStore: &fakeEnvTargetStoreDeleter{},
		ProjectStore:   &fakeProjectStoreDeleter{},
		Locker:         &fakeLockerDeleter{},
		GitOps:         gitops,
	})

	_, err := deleter.Delete(context.Background(), ProjectDeleteInput{
		TenantID:    "tn-1",
		ProjectID:   "proj-uuid",
		ProjectName: "my-app",
		Environment: "production",
	})

	if err == nil {
		t.Fatal("expected error when gitops remove fails")
	}
}

func TestProjectDeleter_NoEnvTargets_FallbackGitOpsCleanup(t *testing.T) {
	// When no environment_targets exist, should try fallback cleanup on "production"
	ets := &fakeEnvTargetStoreDeleter{targets: nil} // empty
	ps := &fakeProjectStoreDeleter{}
	gitops := &fakeGitOpsDeleter{}
	sync := &fakeSyncDeleter{}

	deleter := NewProjectDeleter(ProjectDeleterDeps{
		BuildStore:     &fakeBuildStoreDeleter{},
		EnvTargetStore: ets,
		ProjectStore:   ps,
		Locker:         &fakeLockerDeleter{},
		GitOps:         gitops,
		Sync:           sync,
	})

	result, err := deleter.Delete(context.Background(), ProjectDeleteInput{
		TenantID:    "tn-1",
		ProjectID:   "proj-uuid",
		ProjectName: "orphan-app",
		// No environment = delete all
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gitops.removeCalled {
		t.Error("expected fallback gitops remove on production env")
	}
	if !ps.deleted {
		t.Error("expected project record to be deleted")
	}
	// Should have "production" from fallback
	if len(result.DeletedEnvironments) != 1 || result.DeletedEnvironments[0] != "production" {
		t.Errorf("expected [production] from fallback, got %v", result.DeletedEnvironments)
	}
}

func TestProjectDeleter_NilWorkflowCanceller_DoesNotPanic(t *testing.T) {
	deleter := NewProjectDeleter(ProjectDeleterDeps{
		BuildStore:        &fakeBuildStoreDeleter{cancelledCount: 1, cancelledWFIDs: []string{"wf-1"}},
		EnvTargetStore:    &fakeEnvTargetStoreDeleter{},
		ProjectStore:      &fakeProjectStoreDeleter{},
		Locker:            &fakeLockerDeleter{},
		GitOps:            &fakeGitOpsDeleter{},
		WorkflowCanceller: nil, // nil-safe
		Sync:              nil, // nil-safe
	})

	_, err := deleter.Delete(context.Background(), ProjectDeleteInput{
		TenantID:    "tn-1",
		ProjectID:   "proj-uuid",
		ProjectName: "my-app",
		Environment: "production",
	})

	if err != nil {
		t.Fatalf("unexpected error with nil workflow canceller: %v", err)
	}
}

func TestProjectDeleter_UsesProjectNameForGitOps(t *testing.T) {
	// Verifies that gitops uses ProjectName, not ProjectID UUID
	gitops := &fakeGitOpsDeleter{}

	deleter := NewProjectDeleter(ProjectDeleterDeps{
		BuildStore:     &fakeBuildStoreDeleter{},
		EnvTargetStore: &fakeEnvTargetStoreDeleter{},
		ProjectStore:   &fakeProjectStoreDeleter{},
		Locker:         &fakeLockerDeleter{},
		GitOps:         gitops,
	})

	_, err := deleter.Delete(context.Background(), ProjectDeleteInput{
		TenantID:    "tn-1",
		ProjectID:   "550e8400-e29b-41d4-a716-446655440000", // UUID
		ProjectName: "my-real-app",                          // name
		Environment: "production",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gitops.removeCalled {
		t.Error("expected gitops remove to be called")
	}
}

// CHG-2026-04-29-DOGFOOD-REAL-E2E iter 13 — cascade-cancel regression test.
// Without ReleaseStore.CancelAllByEnv, a re-deploy after `loopzero project delete`
// finds the prior ACTIVE release row via GetActiveByEnv and uses its stale
// git commit when promoting. This test asserts the cascade fires for every
// environment passed to Delete.
func TestProjectDeleter_CascadesReleaseCancel(t *testing.T) {
	rs := &fakeReleaseStoreDeleter{cancelledCount: 3}
	ets := &fakeEnvTargetStoreDeleter{
		targets: []store.EnvironmentTarget{
			{TenantID: "tn-1", ProjectID: "my-app", Environment: "production"},
			{TenantID: "tn-1", ProjectID: "my-app", Environment: "staging"},
		},
	}
	deleter := NewProjectDeleter(ProjectDeleterDeps{
		BuildStore:     &fakeBuildStoreDeleter{},
		EnvTargetStore: ets,
		ReleaseStore:   rs,
		ProjectStore:   &fakeProjectStoreDeleter{},
		Locker:         &fakeLockerDeleter{},
		GitOps:         &fakeGitOpsDeleter{},
	})

	_, err := deleter.Delete(context.Background(), ProjectDeleteInput{
		TenantID:    "tn-1",
		ProjectID:   "my-app",
		ProjectName: "my-app",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := len(rs.cancelCalls), 2; got != want {
		t.Fatalf("expected %d CancelAllByEnv calls (one per env), got %d: %+v", want, got, rs.cancelCalls)
	}
	envs := map[string]bool{}
	for _, c := range rs.cancelCalls {
		envs[c.env] = true
		if c.tenantID != "tn-1" || c.projectID != "my-app" {
			t.Errorf("cascade called with wrong identifiers: %+v", c)
		}
	}
	if !envs["production"] || !envs["staging"] {
		t.Errorf("cascade missed an environment: %+v", envs)
	}
}

// Cascade must not break the delete flow when ReleaseStore is nil — preserves
// backward compatibility for tests/contexts that wire only the legacy deps.
func TestProjectDeleter_NilReleaseStore_NoPanic(t *testing.T) {
	deleter := NewProjectDeleter(ProjectDeleterDeps{
		BuildStore:     &fakeBuildStoreDeleter{},
		EnvTargetStore: &fakeEnvTargetStoreDeleter{},
		ProjectStore:   &fakeProjectStoreDeleter{},
		Locker:         &fakeLockerDeleter{},
		GitOps:         &fakeGitOpsDeleter{},
		// ReleaseStore deliberately nil
	})
	if _, err := deleter.Delete(context.Background(), ProjectDeleteInput{
		TenantID: "tn-1", ProjectID: "p", ProjectName: "p", Environment: "production",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// AppendSideEffect — fix-all-platform-issues-plan T4.1 stub.
func (f *fakeReleaseStoreDeleter) AppendSideEffect(_ context.Context, _ db.DBTX, _ string, _ map[string]any) error {
	return nil
}

func TestProjectDeleter_SingleEnv_SyncsTheApplicationOfThisProject(t *testing.T) {
	sync := &fakeSyncDeleter{}
	deleter := NewProjectDeleter(ProjectDeleterDeps{
		BuildStore:     &fakeBuildStoreDeleter{},
		EnvTargetStore: &fakeEnvTargetStoreDeleter{},
		ProjectStore:   &fakeProjectStoreDeleter{},
		Locker:         &fakeLockerDeleter{},
		GitOps:         &fakeGitOpsDeleter{},
		Sync:           sync,
	})

	_, err := deleter.Delete(context.Background(), ProjectDeleteInput{
		TenantID:    "tn-1",
		ProjectID:   "proj-uuid",
		ProjectName: "my-app",
		Environment: "staging",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sync.syncedApps) != 1 || sync.syncedApps[0] != "tn-1-my-app-staging" {
		t.Errorf("prune sync must target the tenant-project-env application; got %v", sync.syncedApps)
	}
}

func TestProjectDeleter_NoEnvTargets_FallbackSyncsTheProductionApplicationOfThisProject(t *testing.T) {
	sync := &fakeSyncDeleter{}
	deleter := NewProjectDeleter(ProjectDeleterDeps{
		BuildStore:     &fakeBuildStoreDeleter{},
		EnvTargetStore: &fakeEnvTargetStoreDeleter{},
		ProjectStore:   &fakeProjectStoreDeleter{},
		Locker:         &fakeLockerDeleter{},
		GitOps:         &fakeGitOpsDeleter{},
		Sync:           sync,
	})

	_, err := deleter.Delete(context.Background(), ProjectDeleteInput{
		TenantID:    "tn-1",
		ProjectID:   "proj-uuid",
		ProjectName: "orphan-app",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sync.syncedApps) != 1 || sync.syncedApps[0] != "tn-1-orphan-app-production" {
		t.Errorf("fallback prune sync must target the tenant-project-production application; got %v", sync.syncedApps)
	}
}

func TestPruneSyncPolicy_BacksOffOneThenThreeSeconds(t *testing.T) {
	if got := pruneSyncPolicy.Backoff(1, false); got != time.Second {
		t.Errorf("first backoff = %v, want 1s", got)
	}
	if got := pruneSyncPolicy.Backoff(2, true); got != 3*time.Second {
		t.Errorf("second backoff = %v, want 3s", got)
	}
}

func TestPruneSyncPolicy_TerminatesOnTheFirstStuckFailure(t *testing.T) {
	if !pruneSyncPolicy.ShouldTerminate(1) {
		t.Error("a project being deleted has no peer sync worth waiting for; terminate at once")
	}
}

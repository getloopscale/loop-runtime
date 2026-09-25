// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/builds/buildshttp"
	"github.com/getloopscale/loopzero/api/internal/logs/logshttp"
	"github.com/getloopscale/loopzero/api/internal/status/statushttp"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// --- BuildOrchestratorAdapter: SubmitBuild defaults ---

// fakeBuildOrchestrator captures SubmitBuild calls to inspect app normalization.
// We cannot use the real BuildOrchestrator (requires DB, workflows, etc.),
// so we test the adapter's type translation logic via the GetBuildStatus path.

// fakeBuildStore implements db.BuildStore with minimal stub methods.
type fakeBuildStore struct {
	build   *db.Build
	apps    []db.BuildAppSpec
	appsErr error
}

func (f *fakeBuildStore) GetByID(_ context.Context, id string) (*db.Build, error) {
	if f.build != nil && f.build.ID == id {
		return f.build, nil
	}
	return nil, db.ErrNoRows
}

// GetByIDForTenant — T0.4 of deep-review-2026-05-10-remediation-plan.
// Emulates the production SQL filter for the unit-test scope.
func (f *fakeBuildStore) GetByIDForTenant(ctx context.Context, tenantID, buildID string) (*db.Build, error) {
	b, err := f.GetByID(ctx, buildID)
	if err != nil || b == nil {
		return b, err
	}
	if b.TenantID != tenantID {
		return nil, nil
	}
	return b, nil
}

func (f *fakeBuildStore) GetBuildApps(_ context.Context, _ string) ([]db.BuildAppSpec, error) {
	return f.apps, f.appsErr
}

// Stubs for interface satisfaction (not used in these tests).
func (f *fakeBuildStore) Create(_ context.Context, _ db.DBTX, _ *db.Build) error { return nil }
func (f *fakeBuildStore) HasActiveBuild(_ context.Context, _ db.DBTX, _, _, _ string) (bool, error) {
	return false, nil
}
func (f *fakeBuildStore) UpdateStatus(_ context.Context, _ db.DBTX, _ string, _ string) (*db.Build, error) {
	return nil, nil
}
func (f *fakeBuildStore) UpdateStatusIfCurrent(_ context.Context, _, _ string, _ statemachine.BuildPhase, _ string) (*db.Build, error) {
	return nil, nil
}
func (f *fakeBuildStore) SetErrorMessage(_ context.Context, _ db.DBTX, _, _ string) error { return nil }
func (f *fakeBuildStore) SetWorkflowID(_ context.Context, _ db.DBTX, _, _ string) error   { return nil }
func (f *fakeBuildStore) SetAppsSpec(_ context.Context, _ db.DBTX, _ string, _ []db.BuildAppSpec) error {
	return nil
}
func (f *fakeBuildStore) ListByProject(_ context.Context, _, _ string, _, _ int) ([]db.Build, error) {
	return nil, nil
}
func (f *fakeBuildStore) ListByProjectAndEnv(_ context.Context, _, _, _ string, _, _ int) ([]db.Build, int, error) {
	return nil, 0, nil
}
func (f *fakeBuildStore) ListEnvsForProject(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}
func (f *fakeBuildStore) FindActiveBuilds(_ context.Context, _ string) ([]db.Build, error) {
	return nil, nil
}
func (f *fakeBuildStore) CancelActiveBuilds(_ context.Context, _ db.DBTX, _, _, _ string) (int, []string, error) {
	return 0, nil, nil
}

// --- GetBuildStatus tests ---

func TestBuildOrchestratorAdapter_GetBuildStatus_TimestampFormat(t *testing.T) {
	// Arrange: build with StartedAt and FinishedAt
	now := time.Date(2026, 4, 22, 10, 30, 0, 0, time.UTC)
	started := now.Add(-2 * time.Minute)
	finished := now
	dur := 120

	store := &fakeBuildStore{
		build: &db.Build{
			ID:              "bld-ts-001",
			ProjectID:       "my-app",
			Environment:     "production",
			Status:          "SUCCEEDED",
			CommitSHA:       "abc123",
			CreatedBy:       "user@test.com",
			CreatedAt:       now.Add(-3 * time.Minute),
			StartedAt:       &started,
			FinishedAt:      &finished,
			DurationSeconds: &dur,
		},
		apps: []db.BuildAppSpec{{Name: "web", Type: "server"}},
	}

	adapter := &BuildOrchestratorAdapter{BuildStore: store}

	// Act
	resp, err := adapter.GetBuildStatus(context.Background(), "bld-ts-001")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CreatedAt must be RFC3339 UTC
	if _, parseErr := time.Parse(time.RFC3339, resp.CreatedAt); parseErr != nil {
		t.Errorf("CreatedAt not RFC3339: %q, error: %v", resp.CreatedAt, parseErr)
	}
	if _, parseErr := time.Parse(time.RFC3339, resp.StartedAt); parseErr != nil {
		t.Errorf("StartedAt not RFC3339: %q, error: %v", resp.StartedAt, parseErr)
	}
	if _, parseErr := time.Parse(time.RFC3339, resp.FinishedAt); parseErr != nil {
		t.Errorf("FinishedAt not RFC3339: %q, error: %v", resp.FinishedAt, parseErr)
	}

	// Verify apps populated
	if len(resp.Apps) != 1 || resp.Apps[0].Name != "web" {
		t.Errorf("expected 1 app 'web', got %+v", resp.Apps)
	}
}

func TestBuildOrchestratorAdapter_GetBuildStatus_NilTimestamps(t *testing.T) {
	// Arrange: build with nil StartedAt and FinishedAt (QUEUED state)
	now := time.Date(2026, 4, 22, 10, 30, 0, 0, time.UTC)

	store := &fakeBuildStore{
		build: &db.Build{
			ID:          "bld-nil-001",
			ProjectID:   "my-app",
			Environment: "staging",
			Status:      "QUEUED",
			CommitSHA:   "def456",
			CreatedBy:   "user@test.com",
			CreatedAt:   now,
			StartedAt:   nil,
			FinishedAt:  nil,
		},
	}

	adapter := &BuildOrchestratorAdapter{BuildStore: store}

	// Act
	resp, err := adapter.GetBuildStatus(context.Background(), "bld-nil-001")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StartedAt != "" {
		t.Errorf("expected empty StartedAt for nil, got %q", resp.StartedAt)
	}
	if resp.FinishedAt != "" {
		t.Errorf("expected empty FinishedAt for nil, got %q", resp.FinishedAt)
	}
}

// TestAdapters_EmptyStore_ReturnsErrNoRows exercises every Getter method on
// every Adapter wrapping fakeBuildStore against an EMPTY store. Each must
// return db.ErrNoRows (the canonical "not found" sentinel). Phase 3 helper:
// consolidates 5 near-clone test functions across 4 different adapter types
// + 5 different method names into one table-driven test.
func TestAdapters_EmptyStore_ReturnsErrNoRows(t *testing.T) {
	type getterCall func(store *fakeBuildStore) error
	cases := []struct {
		name string
		call getterCall
	}{
		{
			name: "BuildOrchestratorAdapter_GetBuildStatus",
			call: func(store *fakeBuildStore) error {
				_, err := (&BuildOrchestratorAdapter{BuildStore: store}).GetBuildStatus(context.Background(), "bld-nope")
				return err
			},
		},
		{
			name: "BuildOrchestratorAdapter_GetBuildTenantID",
			call: func(store *fakeBuildStore) error {
				_, err := (&BuildOrchestratorAdapter{BuildStore: store}).GetBuildTenantID(context.Background(), "bld-nope")
				return err
			},
		},
		{
			name: "BuildStoreReaderAdapter_GetByID",
			call: func(store *fakeBuildStore) error {
				_, err := (&BuildStoreReaderAdapter{Store: store}).GetByID(context.Background(), "bld-nope")
				return err
			},
		},
		{
			name: "LogBuildStoreAdapter_GetByID",
			call: func(store *fakeBuildStore) error {
				_, err := (&LogBuildStoreAdapter{Store: store}).GetByID(context.Background(), "bld-nope")
				return err
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := &fakeBuildStore{}
			if err := c.call(store); !errors.Is(err, db.ErrNoRows) {
				t.Errorf("expected db.ErrNoRows, got %v", err)
			}
		})
	}
}

// --- BuildStoreReaderAdapter ---

func TestBuildStoreReaderAdapter_GetByID_MapsFields(t *testing.T) {
	store := &fakeBuildStore{
		build: &db.Build{
			ID:       "bld-sse-001",
			TenantID: "tn-acme",
			Status:   "RUNNING",
		},
	}

	adapter := &BuildStoreReaderAdapter{Store: store}

	info, err := adapter.GetByID(context.Background(), "bld-sse-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "bld-sse-001" || info.TenantID != "tn-acme" || info.Status != "RUNNING" {
		t.Errorf("unexpected mapping: %+v", info)
	}
}

// --- LogBuildStoreAdapter ---

func TestLogBuildStoreAdapter_GetByID_MapsFields(t *testing.T) {
	store := &fakeBuildStore{
		build: &db.Build{
			ID:       "bld-log-001",
			TenantID: "tn-acme",
			Status:   "SUCCEEDED",
		},
	}

	adapter := &LogBuildStoreAdapter{Store: store}

	logBuild, err := adapter.GetByID(context.Background(), "bld-log-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logBuild.ID != "bld-log-001" || logBuild.TenantID != "tn-acme" || logBuild.Status != "SUCCEEDED" {
		t.Errorf("unexpected mapping: %+v", logBuild)
	}
}

// --- StatusBuildStoreAdapter ---

func TestStatusBuildStoreAdapter_ListByProject_MapsFields(t *testing.T) {
	now := time.Now()
	dur := 45
	store := &fakeStatusBuildStore{
		builds: []db.Build{
			{
				ID: "bld-st-001", Environment: "production", Status: "SUCCEEDED",
				CommitSHA: "abc", CreatedAt: now, DurationSeconds: &dur,
			},
			{
				ID: "bld-st-002", Environment: "staging", Status: "FAILED",
				CommitSHA: "def", CreatedAt: now, ErrorMessage: "oom",
			},
		},
	}

	adapter := &StatusBuildStoreAdapter{Store: store}

	results, err := adapter.ListByProject(context.Background(), "tn-acme", "my-app", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "bld-st-001" || results[0].Status != "SUCCEEDED" {
		t.Errorf("unexpected first result: %+v", results[0])
	}
	if results[1].ErrorMessage != "oom" {
		t.Errorf("expected error message 'oom', got %q", results[1].ErrorMessage)
	}
}

// --- StatusReleaseStoreAdapter ---

func TestStatusReleaseStoreAdapter_GetActiveByEnv_NilRelease(t *testing.T) {
	store := &fakeStatusReleaseStore{release: nil}
	adapter := &StatusReleaseStoreAdapter{Store: store}

	result, err := adapter.GetActiveByEnv(context.Background(), "tn-acme", "my-app", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for no active release, got %+v", result)
	}
}

func TestStatusReleaseStoreAdapter_GetActiveByEnv_MapsFields(t *testing.T) {
	now := time.Now()
	store := &fakeStatusReleaseStore{
		release: &db.Release{
			ID: "rel-st-001", Status: "ACTIVE", GitCommit: "abc123", CreatedAt: now,
		},
	}
	adapter := &StatusReleaseStoreAdapter{Store: store}

	result, err := adapter.GetActiveByEnv(context.Background(), "tn-acme", "my-app", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ID != "rel-st-001" || result.Status != "ACTIVE" || result.GitCommit != "abc123" {
		t.Errorf("unexpected mapping: %+v", result)
	}
}

// --- BuildListStoreAdapter ---

func TestBuildListStoreAdapter_ListByProjectAndEnv_MapsFields(t *testing.T) {
	now := time.Now()
	store := &fakeBuildListStore{
		builds: []db.Build{
			{ID: "bld-list-001", ProjectID: "my-app", Environment: "staging", Status: "SUCCEEDED", CreatedAt: now, CreatedBy: "user@test.com"},
		},
		total: 1,
	}

	adapter := &BuildListStoreAdapter{Store: store}

	results, total, err := adapter.ListByProjectAndEnv(context.Background(), "tn-acme", "my-app", "staging", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ProjectID != "my-app" || results[0].CreatedBy != "user@test.com" {
		t.Errorf("unexpected mapping: %+v", results[0])
	}
}

// --- Fake stores for adapter tests ---

// fakeStatusBuildStore implements only ListByProject for StatusBuildStoreAdapter tests.
type fakeStatusBuildStore struct {
	fakeBuildStore
	builds []db.Build
}

func (f *fakeStatusBuildStore) ListByProject(_ context.Context, _, _ string, _, _ int) ([]db.Build, error) {
	return f.builds, nil
}

// fakeStatusReleaseStore implements db.ReleaseStore minimally.
type fakeStatusReleaseStore struct {
	release *db.Release
}

func (f *fakeStatusReleaseStore) GetActiveByEnv(_ context.Context, _, _, _ string) (*db.Release, error) {
	return f.release, nil
}

// Stubs for interface satisfaction.
func (f *fakeStatusReleaseStore) Create(_ context.Context, _ db.DBTX, _ *db.Release) error {
	return nil
}
func (f *fakeStatusReleaseStore) GetByID(_ context.Context, _ string) (*db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) UpdateStatus(_ context.Context, _ db.DBTX, _ string, _ string) (*db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) SetGitCommit(_ context.Context, _ db.DBTX, _, _ string) error {
	return nil
}
func (f *fakeStatusReleaseStore) GetActiveByEnvTx(_ context.Context, _ db.DBTX, _, _, _ string) (*db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) GetPreviousRelease(_ context.Context, _, _, _ string) (*db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) GetByIDForTenant(_ context.Context, _, _ string) (*db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) GetByIDTx(_ context.Context, _ db.DBTX, _ string) (*db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) FindPendingReleases(_ context.Context, _ string) ([]db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) FindActiveReleases(_ context.Context, _ string) ([]db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) CancelAllByEnv(_ context.Context, _ db.DBTX, _, _, _ string) (int, error) {
	return 0, nil
}
func (f *fakeStatusReleaseStore) FindPendingReleasesByTenantEnv(_ context.Context, _, _ string) ([]db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) ListByEnv(_ context.Context, _, _, _ string, _ int) ([]db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) GetLatestByBuildID(_ context.Context, _ string) (*db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) AppendSideEffect(_ context.Context, _ db.DBTX, _ string, _ map[string]any) error {
	return nil
}

// fakeBuildListStore implements ListByProjectAndEnv for BuildListStoreAdapter tests.
type fakeBuildListStore struct {
	fakeBuildStore
	builds []db.Build
	total  int
}

func (f *fakeBuildListStore) ListByProjectAndEnv(_ context.Context, _, _, _ string, _, _ int) ([]db.Build, int, error) {
	return f.builds, f.total, nil
}

// --- Verify adapter types satisfy interfaces ---

var _ buildshttp.BuildOrchestratorService = (*BuildOrchestratorAdapter)(nil)
var _ logshttp.LogBuildStore = (*LogBuildStoreAdapter)(nil)
var _ statushttp.StatusBuildStore = (*StatusBuildStoreAdapter)(nil)
var _ statushttp.StatusReleaseStore = (*StatusReleaseStoreAdapter)(nil)

// --- IsBuildTerminal ---

// GetBuildTenantID/BuildStoreReaderAdapter/LogBuildStoreAdapter
// NotFound cases consolidated into TestAdapters_EmptyStore_ReturnsErrNoRows
// above (Phase 3 v26 refactor).

// --- GetBuildTenantID ---

func TestBuildOrchestratorAdapter_GetBuildTenantID(t *testing.T) {
	store := &fakeBuildStore{
		build: &db.Build{ID: "bld-sec-001", TenantID: "tn-acme"},
	}
	adapter := &BuildOrchestratorAdapter{BuildStore: store}

	tenant, err := adapter.GetBuildTenantID(context.Background(), "bld-sec-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tenant != "tn-acme" {
		t.Errorf("tenant = %q, want tn-acme", tenant)
	}
}

// Consolidated into TestAdapters_EmptyStore_ReturnsErrNoRows above.

// --- StatusReleaseStoreAdapter error ---

func TestStatusReleaseStoreAdapter_GetActiveByEnv_Error(t *testing.T) {
	store := &fakeStatusReleaseStoreWithError{err: errors.New("db down")}
	adapter := &StatusReleaseStoreAdapter{Store: store}

	_, err := adapter.GetActiveByEnv(context.Background(), "tn-acme", "app", "prod")
	if err == nil {
		t.Fatal("expected error")
	}
}

type fakeStatusReleaseStoreWithError struct {
	fakeStatusReleaseStore
	err error
}

func (f *fakeStatusReleaseStoreWithError) GetActiveByEnv(_ context.Context, _, _, _ string) (*db.Release, error) {
	return nil, f.err
}

// --- BuildListStoreAdapter error ---

func TestBuildListStoreAdapter_ListByProjectAndEnv_Error(t *testing.T) {
	store := &fakeBuildListStoreWithError{err: errors.New("db down")}
	adapter := &BuildListStoreAdapter{Store: store}

	_, _, err := adapter.ListByProjectAndEnv(context.Background(), "tn", "proj", "prod", 10, 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

type fakeBuildListStoreWithError struct {
	fakeBuildStore
	err error
}

func (f *fakeBuildListStoreWithError) ListByProjectAndEnv(_ context.Context, _, _, _ string, _, _ int) ([]db.Build, int, error) {
	return nil, 0, f.err
}

// --- StatusBuildStoreAdapter error ---

func TestStatusBuildStoreAdapter_ListByProject_Error(t *testing.T) {
	store := &fakeStatusBuildStoreWithError{err: errors.New("db down")}
	adapter := &StatusBuildStoreAdapter{Store: store}

	_, err := adapter.ListByProject(context.Background(), "tn", "proj", 10, 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

type fakeStatusBuildStoreWithError struct {
	fakeBuildStore
	err error
}

func (f *fakeStatusBuildStoreWithError) ListByProject(_ context.Context, _, _ string, _, _ int) ([]db.Build, error) {
	return nil, f.err
}

// TestGetRedeploySource_PreservesRuntime — canonical regression-protector for
// loopzero#185 (root cause found during Cleanroom-UP-LIVE mandate 2026-07-04):
// `GetRedeploySource` copied Name/Type/Port/Path/Build/Start/Rendering/Secrets/
// Replicas/Autoscaling but silently dropped `Runtime`. Cascading effect: redeployed
// build had empty Runtime → build_plan resolveLanguageStrategy returned
// unsupportedStrategy → "Failed to build manifests with custom domains" → no
// release ACTIVE → 13 dogfood wrappers assuming deployed baseline failed.
// The persisted BuildAppSpec HAD runtime="node" (first webhook autodeploy build);
// the redeploy adapter was the leak.
func TestGetRedeploySource_PreservesRuntime(t *testing.T) {
	store := &fakeBuildStore{
		build: &db.Build{
			ID:          "build-source",
			TenantID:    "tn-1",
			ProjectID:   "teste",
			Environment: "production",
			CommitSHA:   "dcd30d4a3a70b6775684e2a9e32945d57663a9f0",
			Status:      "SUCCEEDED",
		},
		apps: []db.BuildAppSpec{
			{
				Name:    "app",
				Type:    "server",
				Runtime: "node", // ← the field the adapter used to drop
				Path:    ".",
				Port:    3000,
				Build:   "buildpack",
				Start:   "npm start",
			},
		},
	}
	adapter := &BuildOrchestratorAdapter{BuildStore: store}
	src, err := adapter.GetRedeploySource(context.Background(), "tn-1", "build-source")
	if err != nil {
		t.Fatalf("GetRedeploySource err=%v", err)
	}
	if len(src.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(src.Apps))
	}
	if got := src.Apps[0].Runtime; got != "node" {
		t.Fatalf("Runtime dropped by redeploy adapter: got=%q want=%q — reproduces loopzero#185 cascade", got, "node")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// --- pgxRowAdapter: Scan maps pgx.ErrNoRows to db.ErrNoRows ---

// fakeRow is a minimal pgx.Row implementation for testing.
type fakeRow struct {
	err error
}

func (r *fakeRow) Scan(_ ...any) error { return r.err }

func TestPgxRowAdapter_Scan_MapsErrNoRows(t *testing.T) {
	// Arrange: a pgx.Row that returns pgx.ErrNoRows
	adapter := &pgxRowAdapter{row: &fakeRow{err: pgx.ErrNoRows}}

	// Act
	err := adapter.Scan()

	// Assert: should be translated to db.ErrNoRows
	if !errors.Is(err, db.ErrNoRows) {
		t.Errorf("expected db.ErrNoRows, got %v", err)
	}
}

func TestPgxRowAdapter_Scan_PassesThroughOtherErrors(t *testing.T) {
	// Arrange: a pgx.Row that returns a generic error
	genericErr := errors.New("connection refused")
	adapter := &pgxRowAdapter{row: &fakeRow{err: genericErr}}

	// Act
	err := adapter.Scan()

	// Assert: should be passed through unchanged
	if err != genericErr {
		t.Errorf("expected original error %v, got %v", genericErr, err)
	}
}

func TestPgxRowAdapter_Scan_SuccessReturnsNil(t *testing.T) {
	// Arrange: a pgx.Row that succeeds
	adapter := &pgxRowAdapter{row: &fakeRow{err: nil}}

	// Act
	err := adapter.Scan()

	// Assert
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// --- BuildOrchestratorAdapter: SubmitBuild defaults ---

// fakeBuildOrchestrator captures SubmitBuild calls to inspect app normalization.
// We cannot use the real BuildOrchestrator (requires DB, workflows, etc.),
// so we test the adapter's type translation logic via the GetBuildStatus path.

// fakeBuildStore implements db.BuildStore with minimal stub methods.
type fakeBuildStore struct {
	build *db.Build
	apps  []db.BuildAppSpec
}

func (f *fakeBuildStore) GetByID(_ context.Context, id string) (*db.Build, error) {
	if f.build != nil && f.build.ID == id {
		return f.build, nil
	}
	return nil, db.ErrNoRows
}

func (f *fakeBuildStore) GetBuildApps(_ context.Context, _ string) ([]db.BuildAppSpec, error) {
	return f.apps, nil
}

// Stubs for interface satisfaction (not used in these tests).
func (f *fakeBuildStore) Create(_ context.Context, _ db.DBTX, _ *db.Build) error      { return nil }
func (f *fakeBuildStore) HasActiveBuild(_ context.Context, _ db.DBTX, _, _, _ string) (bool, error) {
	return false, nil
}
func (f *fakeBuildStore) UpdateStatus(_ context.Context, _ db.DBTX, _ string, _ string) (*db.Build, error) {
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

func TestBuildOrchestratorAdapter_GetBuildStatus_NotFound(t *testing.T) {
	// Arrange: no build in store
	store := &fakeBuildStore{}
	adapter := &BuildOrchestratorAdapter{BuildStore: store}

	// Act
	_, err := adapter.GetBuildStatus(context.Background(), "bld-nonexistent")

	// Assert
	if !errors.Is(err, db.ErrNoRows) {
		t.Errorf("expected db.ErrNoRows, got %v", err)
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
func (f *fakeStatusReleaseStore) FindPendingReleasesByTenantEnv(_ context.Context, _, _ string) ([]db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) ListByEnv(_ context.Context, _, _, _ string, _ int) ([]db.Release, error) {
	return nil, nil
}
func (f *fakeStatusReleaseStore) GetLatestByBuildID(_ context.Context, _ string) (*db.Release, error) {
	return nil, nil
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

var _ routes.BuildOrchestratorService = (*BuildOrchestratorAdapter)(nil)
var _ routes.LogBuildStore = (*LogBuildStoreAdapter)(nil)
var _ routes.StatusBuildStore = (*StatusBuildStoreAdapter)(nil)
var _ routes.StatusReleaseStore = (*StatusReleaseStoreAdapter)(nil)

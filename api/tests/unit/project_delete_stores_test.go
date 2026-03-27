// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// --- ProjectStore.GetByName tests ---

func TestProjectStoreGetByNameFound(t *testing.T) {
	// Arrange
	fake := &fakeDBTXForProject{
		queryRows: &fakeProjectRows{
			project: &routes.ProjectResult{
				ID:     "proj-abc12345",
				Name:   "my-saas",
				TeamID: "tenant-1",
			},
		},
	}
	store := &db.PostgresProjectStore{DB: fake}

	// Act
	result, err := store.GetByName(context.Background(), "tenant-1", "my-saas")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.ID != "proj-abc12345" {
		t.Errorf("expected ID=proj-abc12345, got %q", result.ID)
	}
	if result.Name != "my-saas" {
		t.Errorf("expected Name=my-saas, got %q", result.Name)
	}
	if result.TeamID != "tenant-1" {
		t.Errorf("expected TeamID=tenant-1, got %q", result.TeamID)
	}
}

func TestProjectStoreGetByNameNotFound(t *testing.T) {
	// Arrange
	fake := &fakeDBTXForProject{
		queryRows: &emptyRows{},
	}
	store := &db.PostgresProjectStore{DB: fake}

	// Act
	result, err := store.GetByName(context.Background(), "tenant-1", "nonexistent")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil for nonexistent project, got %+v", result)
	}
}

func TestProjectStoreGetByNameQueryError(t *testing.T) {
	// Arrange
	fake := &fakeDBTXForProject{
		queryErr: fmt.Errorf("connection refused"),
	}
	store := &db.PostgresProjectStore{DB: fake}

	// Act
	_, err := store.GetByName(context.Background(), "tenant-1", "my-saas")

	// Assert
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- ProjectStore.DeleteProject tests ---

func TestProjectStoreDeleteProjectSuccess(t *testing.T) {
	// Arrange
	fake := &fakeDBTXForProject{
		execResult: db.ExecResult{RowsAffected: 1},
	}
	store := &db.PostgresProjectStore{DB: fake}

	// Act
	err := store.DeleteProject(context.Background(), "tenant-1", "proj-abc12345")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestProjectStoreDeleteProjectNotFound(t *testing.T) {
	// Arrange
	fake := &fakeDBTXForProject{
		execResult: db.ExecResult{RowsAffected: 0},
	}
	store := &db.PostgresProjectStore{DB: fake}

	// Act
	err := store.DeleteProject(context.Background(), "tenant-1", "proj-nonexistent")

	// Assert
	if err == nil {
		t.Fatal("expected error for nonexistent project, got nil")
	}
}

func TestProjectStoreDeleteProjectDBError(t *testing.T) {
	// Arrange
	fake := &fakeDBTXForProject{
		execErr: fmt.Errorf("connection refused"),
	}
	store := &db.PostgresProjectStore{DB: fake}

	// Act
	err := store.DeleteProject(context.Background(), "tenant-1", "proj-abc12345")

	// Assert
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- EnvironmentTargetStore.ListByProject tests ---

func TestEnvironmentTargetStoreListByProjectFound(t *testing.T) {
	// Arrange
	now := time.Now().Truncate(time.Second)
	activeRelease := "rel-active"
	fakeQuery := &fakeQueryForTarget{
		rows: &fakeMultiTargetRows{
			targets: []db.EnvironmentTarget{
				{
					TenantID:         "tenant-1",
					ProjectID:        "proj-abc",
					Environment:      "production",
					DesiredReleaseID: "rel-001",
					ActiveReleaseID:  &activeRelease,
					UpdatedBy:        "user-1",
					CorrelationID:    "corr-001",
					UpdatedAt:        now,
				},
				{
					TenantID:         "tenant-1",
					ProjectID:        "proj-abc",
					Environment:      "staging",
					DesiredReleaseID: "rel-002",
					ActiveReleaseID:  nil,
					UpdatedBy:        "user-2",
					CorrelationID:    "corr-002",
					UpdatedAt:        now,
				},
			},
		},
	}
	store := db.NewPostgresEnvironmentTargetStore(nil, fakeQuery)

	// Act
	targets, err := store.ListByProject(context.Background(), "tenant-1", "proj-abc")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	if targets[0].Environment != "production" {
		t.Errorf("expected first target environment=production, got %q", targets[0].Environment)
	}
	if targets[1].Environment != "staging" {
		t.Errorf("expected second target environment=staging, got %q", targets[1].Environment)
	}
}

func TestEnvironmentTargetStoreListByProjectEmpty(t *testing.T) {
	// Arrange
	fakeQuery := &fakeQueryForTarget{
		rows: &emptyRows{},
	}
	store := db.NewPostgresEnvironmentTargetStore(nil, fakeQuery)

	// Act
	targets, err := store.ListByProject(context.Background(), "tenant-1", "proj-abc")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if targets == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(targets) != 0 {
		t.Fatalf("expected 0 targets, got %d", len(targets))
	}
}

// --- EnvironmentTargetStore.DeleteTarget tests ---

func TestEnvironmentTargetStoreDeleteTargetSuccess(t *testing.T) {
	// Arrange
	fakeExec := &fakeExecForTarget{
		result: db.ExecResult{RowsAffected: 1},
	}
	store := db.NewPostgresEnvironmentTargetStore(fakeExec, nil)

	// Act
	err := store.DeleteTarget(context.Background(), nil, "tenant-1", "proj-abc", "production")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestEnvironmentTargetStoreDeleteTargetDBError(t *testing.T) {
	// Arrange
	fakeExec := &fakeExecForTarget{
		err: fmt.Errorf("connection refused"),
	}
	store := db.NewPostgresEnvironmentTargetStore(fakeExec, nil)

	// Act
	err := store.DeleteTarget(context.Background(), nil, "tenant-1", "proj-abc", "production")

	// Assert
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- BuildStore.CancelActiveBuilds tests ---

func TestBuildStoreCancelActiveBuildsSuccess(t *testing.T) {
	// Arrange: 3 builds cancelled, 2 with workflow IDs
	fake := newFakeBuildDBForCancel([]string{"wf-1", "wf-2", ""})
	store := db.NewPostgresBuildStore(fake)

	// Act
	count, wfIDs, err := store.CancelActiveBuilds(context.Background(), nil, "tenant-1", "proj-abc", "production")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 cancelled builds, got %d", count)
	}
	if len(wfIDs) != 2 {
		t.Errorf("expected 2 workflow IDs (empty ones filtered), got %d: %v", len(wfIDs), wfIDs)
	}
}

func TestBuildStoreCancelActiveBuildsNone(t *testing.T) {
	// Arrange
	fake := newFakeBuildDBForCancel(nil)
	store := db.NewPostgresBuildStore(fake)

	// Act
	count, wfIDs, err := store.CancelActiveBuilds(context.Background(), nil, "tenant-1", "proj-abc", "production")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 cancelled builds, got %d", count)
	}
	if len(wfIDs) != 0 {
		t.Errorf("expected 0 workflow IDs, got %v", wfIDs)
	}
}

func TestBuildStoreCancelActiveBuildsDBError(t *testing.T) {
	// Arrange
	fake := &fakeBuildDBForCancel{queryErr: fmt.Errorf("connection refused")}
	store := db.NewPostgresBuildStore(fake)

	// Act
	_, _, err := store.CancelActiveBuilds(context.Background(), nil, "tenant-1", "proj-abc", "production")

	// Assert
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- Fakes for project delete tests ---

// fakeDBTXForProject implements db.DBTX for testing ProjectStore with GetByName + DeleteProject.
type fakeDBTXForProject struct {
	execResult db.ExecResult
	execErr    error
	queryRows  db.Rows
	queryErr   error
}

func (f *fakeDBTXForProject) Exec(ctx context.Context, sql string, args ...any) (db.ExecResult, error) {
	return f.execResult, f.execErr
}

func (f *fakeDBTXForProject) Query(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	return f.queryRows, f.queryErr
}

func (f *fakeDBTXForProject) QueryRow(ctx context.Context, sql string, args ...any) db.Row {
	return &fakeRow{}
}

// fakeProjectRows implements db.Rows returning a single ProjectResult.
type fakeProjectRows struct {
	called  bool
	project *routes.ProjectResult
}

func (r *fakeProjectRows) Next() bool {
	if !r.called {
		r.called = true
		return true
	}
	return false
}

func (r *fakeProjectRows) Scan(dest ...any) error {
	*(dest[0].(*string)) = r.project.ID
	*(dest[1].(*string)) = r.project.Name
	*(dest[2].(*string)) = r.project.TeamID
	return nil
}

func (r *fakeProjectRows) Err() error { return nil }
func (r *fakeProjectRows) Close()     {}

// fakeMultiTargetRows implements db.Rows returning multiple EnvironmentTarget rows.
type fakeMultiTargetRows struct {
	targets []db.EnvironmentTarget
	index   int
}

func (r *fakeMultiTargetRows) Next() bool {
	return r.index < len(r.targets)
}

func (r *fakeMultiTargetRows) Scan(dest ...any) error {
	t := r.targets[r.index]
	r.index++
	*(dest[0].(*string)) = t.TenantID
	*(dest[1].(*string)) = t.ProjectID
	*(dest[2].(*string)) = t.Environment
	*(dest[3].(*string)) = t.DesiredReleaseID
	*(dest[4].(**string)) = t.ActiveReleaseID
	*(dest[5].(*string)) = t.UpdatedBy
	*(dest[6].(*string)) = t.CorrelationID
	*(dest[7].(*time.Time)) = t.UpdatedAt
	return nil
}

func (r *fakeMultiTargetRows) Err() error { return nil }
func (r *fakeMultiTargetRows) Close()     {}

// fakeBuildDBForCancel implements db.DBTX for testing CancelActiveBuilds.
// CancelActiveBuilds uses Query with RETURNING to get workflow IDs.
type fakeBuildDBForCancel struct {
	workflowIDs []string // workflow IDs to return from RETURNING clause
	queryErr    error
}

func newFakeBuildDBForCancel(workflowIDs []string) *fakeBuildDBForCancel {
	return &fakeBuildDBForCancel{workflowIDs: workflowIDs}
}

func (f *fakeBuildDBForCancel) Exec(_ context.Context, _ string, _ ...any) (db.ExecResult, error) {
	return db.ExecResult{}, nil
}

func (f *fakeBuildDBForCancel) Query(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return &fakeCancelRows{workflowIDs: f.workflowIDs, pos: -1}, nil
}

func (f *fakeBuildDBForCancel) QueryRow(_ context.Context, _ string, _ ...any) db.Row {
	return &fakeRow{}
}

// fakeCancelRows implements db.Rows for CancelActiveBuilds RETURNING workflow_id.
type fakeCancelRows struct {
	workflowIDs []string
	pos         int
}

func (r *fakeCancelRows) Next() bool {
	r.pos++
	return r.pos < len(r.workflowIDs)
}

func (r *fakeCancelRows) Scan(dest ...any) error {
	if r.pos >= len(r.workflowIDs) {
		return fmt.Errorf("no more rows")
	}
	if p, ok := dest[0].(*string); ok {
		*p = r.workflowIDs[r.pos]
	}
	return nil
}

func (r *fakeCancelRows) Err() error { return nil }
func (r *fakeCancelRows) Close()     {}

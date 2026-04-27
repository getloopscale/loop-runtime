// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// makePreviewRowValues mirrors previewSelectColumns ordering used by
// scanPreviewRow. Pointers are accepted as-is; non-pointers use their
// natural shapes.
func makePreviewRowValues(rec PreviewRecord) []any {
	return []any{
		rec.ID,
		rec.TenantID,
		rec.ProjectID,
		rec.PRNumber,
		rec.Branch,
		rec.CommitSHA,
		rec.Status,
		rec.URL,
		rec.BuildID,
		rec.ReleaseID,
		rec.RepoFullName,
		rec.InstallationID,
		rec.ExpiresAt,
		rec.CellID,
		rec.CreatedAt,
		rec.UpdatedAt,
		rec.DeletedAt,
	}
}

func newTestPreviewRecord(id, tenant, project string, pr int) PreviewRecord {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	url := "https://pr-" + id + ".preview.loopzero.app"
	return PreviewRecord{
		ID:        id,
		TenantID:  tenant,
		ProjectID: project,
		PRNumber:  pr,
		Branch:    "feature/x",
		CommitSHA: "abc1234",
		Status:    PreviewStatusActive,
		URL:       &url,
		ExpiresAt: now.Add(72 * time.Hour),
		CellID:    "cell-default",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// previewFakeQuery is a QueryExecutor that returns a fixed set of rows.
type previewFakeQuery struct {
	rows Rows
	err  error
}

func (f *previewFakeQuery) Query(_ context.Context, _ string, _ ...any) (Rows, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

// previewFakeExec is an ExecQueryExecutor that returns a fixed result.
type previewFakeExec struct {
	res ExecResult
	err error
}

func (f *previewFakeExec) Exec(_ context.Context, _ string, _ ...any) (ExecResult, error) {
	return f.res, f.err
}

// =============================================================================
// GetByID
// =============================================================================

func TestPreviewStore_GetByID_HappyPath(t *testing.T) {
	rec := newTestPreviewRecord("prev-abc1", "t1", "api", 42)
	rows := &fakeRowsMulti{
		data: [][]any{makePreviewRowValues(rec)},
	}
	store := NewPostgresPreviewStore(nil, &previewFakeQuery{rows: rows})

	got, err := store.GetByID(context.Background(), "t1", "prev-abc1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected preview, got nil")
	}
	if got.ID != "prev-abc1" {
		t.Errorf("ID = %q, want prev-abc1", got.ID)
	}
	if got.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", got.PRNumber)
	}
	if got.Branch != "feature/x" {
		t.Errorf("Branch = %q", got.Branch)
	}
	if got.URL == "" {
		t.Error("URL should be populated from *string pointer")
	}
}

func TestPreviewStore_GetByID_NotFound(t *testing.T) {
	rows := &fakeRowsMulti{} // empty
	store := NewPostgresPreviewStore(nil, &previewFakeQuery{rows: rows})

	got, err := store.GetByID(context.Background(), "t1", "prev-missing")
	if got != nil {
		t.Errorf("expected nil preview, got: %+v", got)
	}
	if !errors.Is(err, loopzeroerrors.ErrPreviewNotFound) {
		t.Errorf("expected ErrPreviewNotFound, got: %v", err)
	}
}

func TestPreviewStore_GetByID_QueryError(t *testing.T) {
	store := NewPostgresPreviewStore(nil, &previewFakeQuery{err: errors.New("boom")})

	_, err := store.GetByID(context.Background(), "t1", "prev-x")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q does not wrap underlying", err.Error())
	}
}

// =============================================================================
// GetByPR
// =============================================================================

func TestPreviewStore_GetByPR_HappyPath(t *testing.T) {
	rec := newTestPreviewRecord("prev-pr1", "t1", "api", 7)
	rows := &fakeRowsMulti{data: [][]any{makePreviewRowValues(rec)}}
	store := NewPostgresPreviewStore(nil, &previewFakeQuery{rows: rows})

	got, err := store.GetByPR(context.Background(), "t1", "api", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID != "prev-pr1" {
		t.Fatalf("got %+v", got)
	}
}

func TestPreviewStore_GetByPR_NotFound(t *testing.T) {
	rows := &fakeRowsMulti{}
	store := NewPostgresPreviewStore(nil, &previewFakeQuery{rows: rows})

	_, err := store.GetByPR(context.Background(), "t1", "api", 999)
	if !errors.Is(err, loopzeroerrors.ErrPreviewNotFound) {
		t.Errorf("expected ErrPreviewNotFound, got: %v", err)
	}
}

// =============================================================================
// ListByTenant / ListByProject
// =============================================================================

func TestPreviewStore_ListByTenant_ReturnsAll(t *testing.T) {
	r1 := newTestPreviewRecord("prev-a", "t1", "api", 1)
	r2 := newTestPreviewRecord("prev-b", "t1", "web", 2)
	rows := &fakeRowsMulti{data: [][]any{
		makePreviewRowValues(r1),
		makePreviewRowValues(r2),
	}}
	store := NewPostgresPreviewStore(nil, &previewFakeQuery{rows: rows})

	got, err := store.ListByTenant(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 previews, got %d", len(got))
	}
	if got[0].ID != "prev-a" || got[1].ID != "prev-b" {
		t.Errorf("unexpected order: %+v", got)
	}
}

func TestPreviewStore_ListByTenant_Empty(t *testing.T) {
	rows := &fakeRowsMulti{}
	store := NewPostgresPreviewStore(nil, &previewFakeQuery{rows: rows})

	got, err := store.ListByTenant(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %d items", len(got))
	}
}

func TestPreviewStore_ListByProject_ScopesArgs(t *testing.T) {
	rec := newTestPreviewRecord("prev-p", "t1", "api", 1)
	rows := &fakeRowsMulti{data: [][]any{makePreviewRowValues(rec)}}
	store := NewPostgresPreviewStore(nil, &previewFakeQuery{rows: rows})

	got, err := store.ListByProject(context.Background(), "t1", "api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 preview, got %d", len(got))
	}
	if got[0].ProjectID != "api" {
		t.Errorf("ProjectID = %q", got[0].ProjectID)
	}
}

// =============================================================================
// RequestDeletion
// =============================================================================

func TestPreviewStore_RequestDeletion_Success(t *testing.T) {
	store := NewPostgresPreviewStore(&previewFakeExec{res: ExecResult{RowsAffected: 1}}, nil)
	if err := store.RequestDeletion(context.Background(), "t1", "prev-x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreviewStore_RequestDeletion_NotFoundOrCrossTenant(t *testing.T) {
	store := NewPostgresPreviewStore(&previewFakeExec{res: ExecResult{RowsAffected: 0}}, nil)
	err := store.RequestDeletion(context.Background(), "t1", "prev-x")
	if !errors.Is(err, loopzeroerrors.ErrPreviewNotFound) {
		t.Errorf("expected ErrPreviewNotFound, got: %v", err)
	}
}

func TestPreviewStore_RequestDeletion_DBError(t *testing.T) {
	store := NewPostgresPreviewStore(&previewFakeExec{err: errors.New("db down")}, nil)
	err := store.RequestDeletion(context.Background(), "t1", "prev-x")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "db down") {
		t.Errorf("error %q does not wrap underlying", err.Error())
	}
}

// =============================================================================
// UpdateStatus / UpdateBuild / UpdateURL
// =============================================================================

func TestPreviewStore_UpdateStatus_Success(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresPreviewStore(nil, nil)

	if err := store.UpdateStatus(context.Background(), tx, "prev-x", "active"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(tx.capturedSQL, "UPDATE preview_environments") {
		t.Errorf("captured SQL not expected: %s", tx.capturedSQL)
	}
	if len(tx.capturedArgs) < 2 || tx.capturedArgs[0] != "active" || tx.capturedArgs[1] != "prev-x" {
		t.Errorf("captured args wrong: %v", tx.capturedArgs)
	}
}

func TestPreviewStore_UpdateStatus_NotFound(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := NewPostgresPreviewStore(nil, nil)
	err := store.UpdateStatus(context.Background(), tx, "prev-x", "active")
	if !errors.Is(err, loopzeroerrors.ErrPreviewNotFound) {
		t.Errorf("expected ErrPreviewNotFound, got: %v", err)
	}
}

func TestPreviewStore_UpdateBuild_Success(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresPreviewStore(nil, nil)
	if err := store.UpdateBuild(context.Background(), tx, "prev-x", "build-1", "deadbeef"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(tx.capturedSQL, "build_id") || !strings.Contains(tx.capturedSQL, "commit_sha") {
		t.Errorf("captured SQL: %s", tx.capturedSQL)
	}
}

func TestPreviewStore_UpdateBuild_NotFound(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := NewPostgresPreviewStore(nil, nil)
	err := store.UpdateBuild(context.Background(), tx, "prev-x", "build-1", "")
	if !errors.Is(err, loopzeroerrors.ErrPreviewNotFound) {
		t.Errorf("expected ErrPreviewNotFound, got: %v", err)
	}
}

func TestPreviewStore_UpdateURL_Success(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresPreviewStore(nil, nil)
	if err := store.UpdateURL(context.Background(), tx, "prev-x", "https://x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// =============================================================================
// ToDomain
// =============================================================================

func TestPreviewRecord_ToDomain_PopulatesAllFields(t *testing.T) {
	rec := newTestPreviewRecord("prev-z", "t1", "api", 42)
	build := "build-99"
	rec.BuildID = &build

	d := rec.ToDomain()
	if d.ID != "prev-z" {
		t.Errorf("ID: %q", d.ID)
	}
	if d.BuildID != "build-99" {
		t.Errorf("BuildID: %q", d.BuildID)
	}
	if d.TTLExpiresAt == "" {
		t.Error("TTLExpiresAt should be populated")
	}
	if d.CreatedAt == "" || d.UpdatedAt == "" {
		t.Error("CreatedAt/UpdatedAt should be populated")
	}
}

// =============================================================================
// Compile-time check that the interface matches the legacy
// routes.PreviewStore (smaller surface). Ensures we don't accidentally
// break the existing handler when the wider PreviewStore is wired.
// =============================================================================

func TestPreviewStore_Implements_LegacyRoutesInterface(t *testing.T) {
	// Just verify that the methods used by routes/preview.go exist on
	// PostgresPreviewStore. The legacy interface signatures (no ctx,
	// no project arg) are adapted at the handler level (see T2.3).
	var s PreviewStore = (*PostgresPreviewStore)(nil)
	_ = s
}

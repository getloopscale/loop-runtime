package unit

import (
	"context"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
)

// fakeExecForTarget implements db.ExecQueryExecutor for testing SetDesired/SetActive.
type fakeExecForTarget struct {
	lastSQL  string
	lastArgs []any
	result   db.ExecResult
	err      error
}

func (f *fakeExecForTarget) Exec(ctx context.Context, sql string, args ...any) (db.ExecResult, error) {
	f.lastSQL = sql
	f.lastArgs = args
	return f.result, f.err
}

// fakeQueryForTarget implements db.QueryExecutor for testing Get.
type fakeQueryForTarget struct {
	rows db.Rows
	err  error
}

func (f *fakeQueryForTarget) Query(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	return f.rows, f.err
}

// fakeTargetRows implements db.Rows returning a single EnvironmentTarget row.
type fakeTargetRows struct {
	called  bool
	scanned bool
	target  db.EnvironmentTarget
}

func (r *fakeTargetRows) Next() bool {
	if !r.called {
		r.called = true
		return true
	}
	return false
}

func (r *fakeTargetRows) Scan(dest ...any) error {
	r.scanned = true
	// Map fields in the same order as the SELECT in Get
	*(dest[0].(*string)) = r.target.TenantID
	*(dest[1].(*string)) = r.target.ProjectID
	*(dest[2].(*string)) = r.target.Environment
	*(dest[3].(*string)) = r.target.DesiredReleaseID
	*(dest[4].(**string)) = r.target.ActiveReleaseID
	*(dest[5].(*string)) = r.target.UpdatedBy
	*(dest[6].(*string)) = r.target.CorrelationID
	*(dest[7].(*time.Time)) = r.target.UpdatedAt
	return nil
}

func (r *fakeTargetRows) Err() error { return nil }
func (r *fakeTargetRows) Close()     {}

// emptyRows returns no rows (simulates not found).
type emptyRows struct{}

func (r *emptyRows) Next() bool              { return false }
func (r *emptyRows) Scan(dest ...any) error   { return nil }
func (r *emptyRows) Err() error               { return nil }
func (r *emptyRows) Close()                   {}

func TestSetDesiredInsert(t *testing.T) {
	// Arrange
	fake := &fakeExecForTarget{
		result: db.ExecResult{RowsAffected: 1},
	}
	store := db.NewPostgresEnvironmentTargetStore(fake, nil)

	// Act
	err := store.SetDesired(context.Background(), nil, "tenant-1", "proj-abc", "production", "rel-001", "user-1", "corr-001")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if fake.lastSQL == "" {
		t.Fatal("expected SQL to be executed")
	}
	// Verify the args match expected order
	expectedArgs := []any{"tenant-1", "proj-abc", "production", "rel-001", "user-1", "corr-001", "rel-001", "user-1", "corr-001"}
	if len(fake.lastArgs) != len(expectedArgs) {
		t.Fatalf("expected %d args, got %d", len(expectedArgs), len(fake.lastArgs))
	}
	for i, expected := range expectedArgs {
		if fake.lastArgs[i] != expected {
			t.Errorf("arg[%d]: expected %v, got %v", i, expected, fake.lastArgs[i])
		}
	}
}

func TestSetDesiredUpsert(t *testing.T) {
	// Arrange — second call with different release_id still succeeds (UPSERT)
	fake := &fakeExecForTarget{
		result: db.ExecResult{RowsAffected: 1},
	}
	store := db.NewPostgresEnvironmentTargetStore(fake, nil)

	// Act — first call
	err := store.SetDesired(context.Background(), nil, "tenant-1", "proj-abc", "production", "rel-001", "user-1", "corr-001")
	if err != nil {
		t.Fatalf("first SetDesired: expected no error, got %v", err)
	}

	// Act — second call with different release
	err = store.SetDesired(context.Background(), nil, "tenant-1", "proj-abc", "production", "rel-002", "user-2", "corr-002")

	// Assert
	if err != nil {
		t.Fatalf("second SetDesired: expected no error, got %v", err)
	}
	// Verify the upsert args reflect the updated release
	expectedArgs := []any{"tenant-1", "proj-abc", "production", "rel-002", "user-2", "corr-002", "rel-002", "user-2", "corr-002"}
	if len(fake.lastArgs) != len(expectedArgs) {
		t.Fatalf("expected %d args, got %d", len(expectedArgs), len(fake.lastArgs))
	}
	for i, expected := range expectedArgs {
		if fake.lastArgs[i] != expected {
			t.Errorf("arg[%d]: expected %v, got %v", i, expected, fake.lastArgs[i])
		}
	}
}

func TestSetActive(t *testing.T) {
	// Arrange
	fake := &fakeExecForTarget{
		result: db.ExecResult{RowsAffected: 1},
	}
	store := db.NewPostgresEnvironmentTargetStore(fake, nil)

	// Act
	err := store.SetActive(context.Background(), nil, "tenant-1", "proj-abc", "production", "rel-001")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expectedArgs := []any{"rel-001", "tenant-1", "proj-abc", "production"}
	if len(fake.lastArgs) != len(expectedArgs) {
		t.Fatalf("expected %d args, got %d", len(expectedArgs), len(fake.lastArgs))
	}
	for i, expected := range expectedArgs {
		if fake.lastArgs[i] != expected {
			t.Errorf("arg[%d]: expected %v, got %v", i, expected, fake.lastArgs[i])
		}
	}
}

func TestGetExisting(t *testing.T) {
	// Arrange
	now := time.Now().Truncate(time.Second)
	activeRelease := "rel-active"
	fakeQuery := &fakeQueryForTarget{
		rows: &fakeTargetRows{
			target: db.EnvironmentTarget{
				TenantID:         "tenant-1",
				ProjectID:        "proj-abc",
				Environment:      "production",
				DesiredReleaseID: "rel-001",
				ActiveReleaseID:  &activeRelease,
				UpdatedBy:        "user-1",
				CorrelationID:    "corr-001",
				UpdatedAt:        now,
			},
		},
	}
	store := db.NewPostgresEnvironmentTargetStore(nil, fakeQuery)

	// Act
	target, err := store.Get(context.Background(), "tenant-1", "proj-abc", "production")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if target == nil {
		t.Fatal("expected target, got nil")
	}
	if target.TenantID != "tenant-1" {
		t.Errorf("expected TenantID=tenant-1, got %q", target.TenantID)
	}
	if target.ProjectID != "proj-abc" {
		t.Errorf("expected ProjectID=proj-abc, got %q", target.ProjectID)
	}
	if target.Environment != "production" {
		t.Errorf("expected Environment=production, got %q", target.Environment)
	}
	if target.DesiredReleaseID != "rel-001" {
		t.Errorf("expected DesiredReleaseID=rel-001, got %q", target.DesiredReleaseID)
	}
	if target.ActiveReleaseID == nil || *target.ActiveReleaseID != "rel-active" {
		t.Errorf("expected ActiveReleaseID=rel-active, got %v", target.ActiveReleaseID)
	}
	if target.UpdatedBy != "user-1" {
		t.Errorf("expected UpdatedBy=user-1, got %q", target.UpdatedBy)
	}
	if target.CorrelationID != "corr-001" {
		t.Errorf("expected CorrelationID=corr-001, got %q", target.CorrelationID)
	}
	if !target.UpdatedAt.Equal(now) {
		t.Errorf("expected UpdatedAt=%v, got %v", now, target.UpdatedAt)
	}
}

func TestGetNonexistent(t *testing.T) {
	// Arrange
	fakeQuery := &fakeQueryForTarget{
		rows: &emptyRows{},
	}
	store := db.NewPostgresEnvironmentTargetStore(nil, fakeQuery)

	// Act
	target, err := store.Get(context.Background(), "tenant-1", "proj-abc", "staging")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if target != nil {
		t.Fatalf("expected nil for nonexistent target, got %+v", target)
	}
}

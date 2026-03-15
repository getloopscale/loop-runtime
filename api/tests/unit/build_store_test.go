package unit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// --- Fake DBTX for build store tests ---

// fakeBuildDB implements db.DBTX for testing BuildStore.
// It stores builds in-memory and simulates SQL behavior.
type fakeBuildDB struct {
	builds       map[string]*db.Build
	insertOrder  []string // track insertion order for list tests
	execErr      error
	queryErr     error
	queryRowErr  error
	scanErr      error
	noRows       bool
}

func newFakeBuildDB() *fakeBuildDB {
	return &fakeBuildDB{
		builds: make(map[string]*db.Build),
	}
}

func (f *fakeBuildDB) Exec(ctx context.Context, sql string, args ...any) (db.ExecResult, error) {
	if f.execErr != nil {
		return db.ExecResult{}, f.execErr
	}
	return db.ExecResult{RowsAffected: 1}, nil
}

func (f *fakeBuildDB) Query(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}

	// For ListByProject, return builds in reverse insertion order
	var results []*db.Build
	for _, id := range f.insertOrder {
		b := f.builds[id]
		if len(args) >= 2 {
			tenantID, _ := args[0].(string)
			projectID, _ := args[1].(string)
			if b.TenantID == tenantID && b.ProjectID == projectID {
				results = append(results, b)
			}
		}
	}
	// Reverse for DESC order
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}

	// Apply limit/offset if provided
	if len(args) >= 4 {
		limit, _ := args[2].(int)
		offset, _ := args[3].(int)
		if offset > len(results) {
			results = nil
		} else {
			results = results[offset:]
			if limit > 0 && limit < len(results) {
				results = results[:limit]
			}
		}
	}

	return &fakeBuildRows{builds: results, index: -1, scanErr: f.scanErr}, nil
}

func (f *fakeBuildDB) QueryRow(ctx context.Context, sql string, args ...any) db.Row {
	if f.queryRowErr != nil {
		return &fakeRow{err: f.queryRowErr}
	}
	if f.noRows {
		return &fakeRow{err: db.ErrNoRows}
	}

	// For GetByID — first arg is the ID
	if len(args) >= 1 {
		id, ok := args[0].(string)
		if ok {
			if b, exists := f.builds[id]; exists {
				return &fakeBuildRow{build: b}
			}
		}
	}
	return &fakeRow{err: db.ErrNoRows}
}

// addBuild adds a build to the fake DB.
func (f *fakeBuildDB) addBuild(b *db.Build) {
	f.builds[b.ID] = b
	f.insertOrder = append(f.insertOrder, b.ID)
}

// --- Fake rows ---

type fakeBuildRows struct {
	builds  []*db.Build
	index   int
	scanErr error
}

func (r *fakeBuildRows) Next() bool {
	r.index++
	return r.index < len(r.builds)
}

func (r *fakeBuildRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	b := r.builds[r.index]
	// Scan: id, tenant_id, project_id, environment, status, commit_sha, correlation_id, created_by, created_at, started_at, finished_at, error_message, duration_seconds, workflow_id
	if len(dest) >= 14 {
		*dest[0].(*string) = b.ID
		*dest[1].(*string) = b.TenantID
		*dest[2].(*string) = b.ProjectID
		*dest[3].(*string) = b.Environment
		*dest[4].(*string) = b.Status
		*dest[5].(*string) = b.CommitSHA
		*dest[6].(*string) = b.CorrelationID
		*dest[7].(*string) = b.CreatedBy
		*dest[8].(*time.Time) = b.CreatedAt
		*dest[9].(**time.Time) = b.StartedAt
		*dest[10].(**time.Time) = b.FinishedAt
		*dest[11].(*string) = b.ErrorMessage
		*dest[12].(**int) = b.DurationSeconds
		*dest[13].(*string) = b.WorkflowID
	}
	return nil
}

func (r *fakeBuildRows) Err() error  { return nil }
func (r *fakeBuildRows) Close()      {}

type fakeBuildRow struct {
	build *db.Build
}

func (r *fakeBuildRow) Scan(dest ...any) error {
	b := r.build
	if len(dest) >= 14 {
		*dest[0].(*string) = b.ID
		*dest[1].(*string) = b.TenantID
		*dest[2].(*string) = b.ProjectID
		*dest[3].(*string) = b.Environment
		*dest[4].(*string) = b.Status
		*dest[5].(*string) = b.CommitSHA
		*dest[6].(*string) = b.CorrelationID
		*dest[7].(*string) = b.CreatedBy
		*dest[8].(*time.Time) = b.CreatedAt
		*dest[9].(**time.Time) = b.StartedAt
		*dest[10].(**time.Time) = b.FinishedAt
		*dest[11].(*string) = b.ErrorMessage
		*dest[12].(**int) = b.DurationSeconds
		*dest[13].(*string) = b.WorkflowID
	}
	return nil
}

type fakeRow struct {
	err error
}

func (r *fakeRow) Scan(dest ...any) error {
	return r.err
}

// --- Fake for HasActiveBuild (returns bool via QueryRow scan) ---

type fakeHasActiveDB struct {
	db.DBTX
	hasActive bool
	err       error
}

func (f *fakeHasActiveDB) QueryRow(ctx context.Context, sql string, args ...any) db.Row {
	if f.err != nil {
		return &fakeRow{err: f.err}
	}
	return &fakeBoolRow{val: f.hasActive}
}

type fakeBoolRow struct {
	val bool
}

func (r *fakeBoolRow) Scan(dest ...any) error {
	if len(dest) >= 1 {
		*dest[0].(*bool) = r.val
	}
	return nil
}

// --- Tests ---

func TestBuildStore_Create(t *testing.T) {
	t.Run("creates build with correct fields and QUEUED status", func(t *testing.T) {
		fakeDB := newFakeBuildDB()
		store := db.NewPostgresBuildStore(fakeDB)

		build := &db.Build{
			TenantID:      "tenant-1",
			ProjectID:     "proj-abc12345",
			Environment:   "production",
			CommitSHA:     "abc123def456",
			CorrelationID: "corr-001",
			CreatedBy:     "user-1",
		}

		err := store.Create(context.Background(), fakeDB, build)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// ID should be generated
		if build.ID == "" {
			t.Error("expected build ID to be generated")
		}

		// Status must be QUEUED
		if build.Status != string(statemachine.BuildQueued) {
			t.Errorf("expected status QUEUED, got %q", build.Status)
		}
	})

	t.Run("preserves existing ID if set", func(t *testing.T) {
		fakeDB := newFakeBuildDB()
		store := db.NewPostgresBuildStore(fakeDB)

		build := &db.Build{
			ID:            "build-existing1",
			TenantID:      "tenant-1",
			ProjectID:     "proj-abc12345",
			Environment:   "production",
			CommitSHA:     "abc123",
			CorrelationID: "corr-001",
			CreatedBy:     "user-1",
		}

		err := store.Create(context.Background(), fakeDB, build)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if build.ID != "build-existing1" {
			t.Errorf("expected ID to be preserved, got %q", build.ID)
		}
	})
}

func TestBuildStore_GetByID(t *testing.T) {
	t.Run("returns build when found", func(t *testing.T) {
		fakeDB := newFakeBuildDB()
		now := time.Now().UTC().Truncate(time.Second)
		fakeDB.addBuild(&db.Build{
			ID:            "build-11111111",
			TenantID:      "tenant-1",
			ProjectID:     "proj-abc12345",
			Environment:   "production",
			Status:        "QUEUED",
			CommitSHA:     "abc123",
			CorrelationID: "corr-001",
			CreatedBy:     "user-1",
			CreatedAt:     now,
		})

		store := db.NewPostgresBuildStore(fakeDB)
		build, err := store.GetByID(context.Background(), "build-11111111")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if build.ID != "build-11111111" {
			t.Errorf("expected ID build-11111111, got %q", build.ID)
		}
		if build.TenantID != "tenant-1" {
			t.Errorf("expected TenantID tenant-1, got %q", build.TenantID)
		}
		if build.Status != "QUEUED" {
			t.Errorf("expected status QUEUED, got %q", build.Status)
		}
	})

	t.Run("returns build with all nullable fields NULL", func(t *testing.T) {
		// Reproduces the exact state of build-3746a6e5: RUNNING with
		// NULL started_at, finished_at, duration_seconds, empty correlation_id
		fakeDB := newFakeBuildDB()
		now := time.Now().UTC().Truncate(time.Second)
		fakeDB.addBuild(&db.Build{
			ID:              "build-3746a6e5",
			TenantID:        "tenant-1",
			ProjectID:       "proj-abc12345",
			Environment:     "production",
			Status:          "RUNNING",
			CommitSHA:       "dev-local",
			CorrelationID:   "",
			CreatedBy:       "user-1",
			CreatedAt:       now,
			StartedAt:       nil, // NULL
			FinishedAt:      nil, // NULL
			DurationSeconds: nil, // NULL
			WorkflowID:      "",
		})

		store := db.NewPostgresBuildStore(fakeDB)
		build, err := store.GetByID(context.Background(), "build-3746a6e5")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if build.ID != "build-3746a6e5" {
			t.Errorf("expected ID build-3746a6e5, got %q", build.ID)
		}
		if build.Status != "RUNNING" {
			t.Errorf("expected status RUNNING, got %q", build.Status)
		}
		if build.StartedAt != nil {
			t.Errorf("expected StartedAt nil, got %v", build.StartedAt)
		}
		if build.FinishedAt != nil {
			t.Errorf("expected FinishedAt nil, got %v", build.FinishedAt)
		}
		if build.DurationSeconds != nil {
			t.Errorf("expected DurationSeconds nil, got %v", build.DurationSeconds)
		}
	})

	t.Run("returns ErrBuildNotFound when not found", func(t *testing.T) {
		fakeDB := newFakeBuildDB()
		store := db.NewPostgresBuildStore(fakeDB)

		_, err := store.GetByID(context.Background(), "build-nonexist")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
			t.Errorf("expected ErrBuildNotFound, got %v", err)
		}
	})
}

func TestBuildStore_HasActiveBuild(t *testing.T) {
	t.Run("returns true when QUEUED build exists", func(t *testing.T) {
		fakeDB := &fakeHasActiveDB{hasActive: true}
		store := db.NewPostgresBuildStore(fakeDB)

		has, err := store.HasActiveBuild(context.Background(), fakeDB, "tenant-1", "proj-abc", "production")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !has {
			t.Error("expected HasActiveBuild to return true")
		}
	})

	t.Run("returns false when no active builds", func(t *testing.T) {
		fakeDB := &fakeHasActiveDB{hasActive: false}
		store := db.NewPostgresBuildStore(fakeDB)

		has, err := store.HasActiveBuild(context.Background(), fakeDB, "tenant-1", "proj-abc", "production")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if has {
			t.Error("expected HasActiveBuild to return false")
		}
	})

	t.Run("returns false when build is SUCCEEDED", func(t *testing.T) {
		// SUCCEEDED is not in ('QUEUED', 'RUNNING'), so HasActiveBuild should return false
		fakeDB := &fakeHasActiveDB{hasActive: false}
		store := db.NewPostgresBuildStore(fakeDB)

		has, err := store.HasActiveBuild(context.Background(), fakeDB, "tenant-1", "proj-abc", "production")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if has {
			t.Error("expected HasActiveBuild to return false for SUCCEEDED builds")
		}
	})
}

func TestBuildStore_UpdateStatus(t *testing.T) {
	t.Run("valid transition QUEUED to RUNNING", func(t *testing.T) {
		fakeDB := newFakeBuildDB()
		now := time.Now().UTC().Truncate(time.Second)
		fakeDB.addBuild(&db.Build{
			ID:            "build-22222222",
			TenantID:      "tenant-1",
			ProjectID:     "proj-abc12345",
			Environment:   "production",
			Status:        "QUEUED",
			CommitSHA:     "abc123",
			CorrelationID: "corr-001",
			CreatedBy:     "user-1",
			CreatedAt:     now,
		})

		store := db.NewPostgresBuildStore(fakeDB)
		build, err := store.UpdateStatus(context.Background(), fakeDB, "build-22222222", string(statemachine.BuildStarted))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if build.Status != string(statemachine.BuildRunning) {
			t.Errorf("expected status RUNNING, got %q", build.Status)
		}
	})

	t.Run("invalid transition returns ErrInvalidTransition", func(t *testing.T) {
		fakeDB := newFakeBuildDB()
		now := time.Now().UTC().Truncate(time.Second)
		fakeDB.addBuild(&db.Build{
			ID:            "build-33333333",
			TenantID:      "tenant-1",
			ProjectID:     "proj-abc12345",
			Environment:   "production",
			Status:        "SUCCEEDED",
			CommitSHA:     "abc123",
			CorrelationID: "corr-001",
			CreatedBy:     "user-1",
			CreatedAt:     now,
		})

		store := db.NewPostgresBuildStore(fakeDB)
		_, err := store.UpdateStatus(context.Background(), fakeDB, "build-33333333", string(statemachine.BuildStarted))
		if err == nil {
			t.Fatal("expected error for invalid transition, got nil")
		}
		if !errors.Is(err, loopzeroerrors.ErrInvalidTransition) {
			t.Errorf("expected ErrInvalidTransition, got %v", err)
		}
	})

	t.Run("returns ErrBuildNotFound when build does not exist", func(t *testing.T) {
		fakeDB := newFakeBuildDB()
		store := db.NewPostgresBuildStore(fakeDB)

		_, err := store.UpdateStatus(context.Background(), fakeDB, "build-nonexist", string(statemachine.BuildStarted))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
			t.Errorf("expected ErrBuildNotFound, got %v", err)
		}
	})
}

func TestBuildStore_SetErrorMessage(t *testing.T) {
	t.Run("sets error_message on existing build", func(t *testing.T) {
		// Arrange
		fakeDB := newFakeBuildDB()
		now := time.Now().UTC().Truncate(time.Second)
		fakeDB.addBuild(&db.Build{
			ID:            "build-err00001",
			TenantID:      "tenant-1",
			ProjectID:     "proj-abc12345",
			Environment:   "production",
			Status:        "FAILED",
			CommitSHA:     "abc123",
			CorrelationID: "corr-001",
			CreatedBy:     "user-1",
			CreatedAt:     now,
			ErrorMessage:  "",
		})

		store := db.NewPostgresBuildStore(fakeDB)

		// Act
		err := store.SetErrorMessage(context.Background(), fakeDB, "build-err00001", "Build pipeline failed at: validate-workspace")

		// Assert
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("returns error when executor fails", func(t *testing.T) {
		// Arrange — simulate DB error
		fakeDB := newFakeBuildDB()
		fakeDB.execErr = errors.New("connection refused")
		store := db.NewPostgresBuildStore(fakeDB)

		// Act
		err := store.SetErrorMessage(context.Background(), fakeDB, "build-err00001", "some error")

		// Assert — should propagate the DB error
		if err == nil {
			t.Fatal("expected error from DB failure, got nil")
		}
	})
}

func TestBuildStore_ListByProject(t *testing.T) {
	t.Run("returns builds ordered by created_at DESC with limit/offset", func(t *testing.T) {
		fakeDB := newFakeBuildDB()
		now := time.Now().UTC().Truncate(time.Second)

		// Add 3 builds in chronological order
		for i, id := range []string{"build-aaa00001", "build-aaa00002", "build-aaa00003"} {
			fakeDB.addBuild(&db.Build{
				ID:            id,
				TenantID:      "tenant-1",
				ProjectID:     "proj-abc12345",
				Environment:   "production",
				Status:        "QUEUED",
				CommitSHA:     "abc",
				CorrelationID: "corr",
				CreatedBy:     "user-1",
				CreatedAt:     now.Add(time.Duration(i) * time.Minute),
			})
		}

		store := db.NewPostgresBuildStore(fakeDB)
		builds, err := store.ListByProject(context.Background(), "tenant-1", "proj-abc12345", 2, 0)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(builds) != 2 {
			t.Fatalf("expected 2 builds, got %d", len(builds))
		}
		// Should be in DESC order — newest first
		if builds[0].ID != "build-aaa00003" {
			t.Errorf("expected first build to be build-aaa00003 (newest), got %q", builds[0].ID)
		}
		if builds[1].ID != "build-aaa00002" {
			t.Errorf("expected second build to be build-aaa00002, got %q", builds[1].ID)
		}
	})

	t.Run("returns empty slice when no builds", func(t *testing.T) {
		fakeDB := newFakeBuildDB()
		store := db.NewPostgresBuildStore(fakeDB)

		builds, err := store.ListByProject(context.Background(), "tenant-1", "proj-xxx", 10, 0)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(builds) != 0 {
			t.Errorf("expected 0 builds, got %d", len(builds))
		}
	})
}

// Suppress unused import warning for json
var _ = json.Marshal

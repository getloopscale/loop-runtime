// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

// --- Fake DBTX for release store tests ---

type fakeReleaseDB struct {
	releases map[string]*db.Release
	execErr  error
}

func newFakeReleaseDB() *fakeReleaseDB {
	return &fakeReleaseDB{
		releases: make(map[string]*db.Release),
	}
}

func (f *fakeReleaseDB) Exec(ctx context.Context, sql string, args ...any) (db.ExecResult, error) {
	if f.execErr != nil {
		return db.ExecResult{}, f.execErr
	}
	return db.ExecResult{RowsAffected: 1}, nil
}

func (f *fakeReleaseDB) Query(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	return &fakeReleaseRows{releases: nil, index: -1}, nil
}

func (f *fakeReleaseDB) QueryRow(ctx context.Context, sql string, args ...any) db.Row {
	if len(args) == 0 {
		return &fakeRow{err: db.ErrNoRows}
	}

	// For GetByID — first arg is the ID
	id, ok := args[0].(string)
	if !ok {
		return &fakeRow{err: db.ErrNoRows}
	}

	// Check if this is a GetActiveByEnv query (3 args: tenant, project, env)
	if len(args) == 3 {
		tenantID := args[0].(string)
		projectID := args[1].(string)
		env := args[2].(string)
		for _, r := range f.releases {
			if r.TenantID == tenantID && r.ProjectID == projectID &&
				r.Environment == env && r.Status == string(statemachine.ReleaseActive) {
				return &fakeReleaseRow{release: r}
			}
		}
		return &fakeRow{err: db.ErrNoRows}
	}

	if r, exists := f.releases[id]; exists {
		return &fakeReleaseRow{release: r}
	}
	return &fakeRow{err: db.ErrNoRows}
}

func (f *fakeReleaseDB) addRelease(r *db.Release) {
	f.releases[r.ID] = r
}

type fakeReleaseRows struct {
	releases []*db.Release
	index    int
}

func (r *fakeReleaseRows) Next() bool  { r.index++; return r.index < len(r.releases) }
func (r *fakeReleaseRows) Scan(...any) error { return nil }
func (r *fakeReleaseRows) Err() error  { return nil }
func (r *fakeReleaseRows) Close()      {}

type fakeReleaseRow struct {
	release *db.Release
}

func (r *fakeReleaseRow) Scan(dest ...any) error {
	rel := r.release
	if len(dest) >= 14 {
		*dest[0].(*string) = rel.ID
		*dest[1].(*string) = rel.BuildID
		*dest[2].(*string) = rel.TenantID
		*dest[3].(*string) = rel.ProjectID
		*dest[4].(*string) = rel.Environment
		*dest[5].(*string) = rel.Status
		*dest[6].(*string) = rel.CorrelationID
		*dest[7].(*string) = rel.CreatedBy
		*dest[8].(*time.Time) = rel.CreatedAt
		*dest[9].(*string) = rel.GitCommit
		*dest[10].(*string) = rel.GitMirrorStatus
		*dest[11].(*json.RawMessage) = rel.Apps
		*dest[12].(*json.RawMessage) = rel.Artifacts
		*dest[13].(*json.RawMessage) = rel.SideEffects
	}
	return nil
}

// --- Tests ---

func TestReleaseStore_Create(t *testing.T) {
	t.Run("creates release with READY status", func(t *testing.T) {
		fakeDB := newFakeReleaseDB()
		store := db.NewPostgresReleaseStore(fakeDB)

		release := &db.Release{
			BuildID:       "build-11111111",
			TenantID:      "tenant-1",
			ProjectID:     "proj-abc12345",
			Environment:   "production",
			CorrelationID: "corr-001",
			CreatedBy:     "user-1",
		}

		err := store.Create(context.Background(), fakeDB, release)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if release.ID == "" {
			t.Error("expected release ID to be generated")
		}

		if release.Status != string(statemachine.ReleaseReady) {
			t.Errorf("expected status READY, got %q", release.Status)
		}
	})

	t.Run("preserves existing ID if set", func(t *testing.T) {
		fakeDB := newFakeReleaseDB()
		store := db.NewPostgresReleaseStore(fakeDB)

		release := &db.Release{
			ID:            "rel-existing1",
			BuildID:       "build-11111111",
			TenantID:      "tenant-1",
			ProjectID:     "proj-abc12345",
			Environment:   "production",
			CorrelationID: "corr-001",
			CreatedBy:     "user-1",
		}

		err := store.Create(context.Background(), fakeDB, release)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if release.ID != "rel-existing1" {
			t.Errorf("expected ID preserved, got %q", release.ID)
		}
	})
}

func TestReleaseStore_GetByID(t *testing.T) {
	t.Run("returns release when found", func(t *testing.T) {
		fakeDB := newFakeReleaseDB()
		now := time.Now().UTC().Truncate(time.Second)
		fakeDB.addRelease(&db.Release{
			ID:            "rel-11111111",
			BuildID:       "build-11111111",
			TenantID:      "tenant-1",
			ProjectID:     "proj-abc12345",
			Environment:   "production",
			Status:        "READY",
			CorrelationID: "corr-001",
			CreatedBy:     "user-1",
			CreatedAt:     now,
			GitMirrorStatus: "pending",
		})

		store := db.NewPostgresReleaseStore(fakeDB)
		release, err := store.GetByID(context.Background(), "rel-11111111")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if release.ID != "rel-11111111" {
			t.Errorf("expected ID rel-11111111, got %q", release.ID)
		}
		if release.Status != "READY" {
			t.Errorf("expected status READY, got %q", release.Status)
		}
	})

	t.Run("returns ErrReleaseNotFound when not found", func(t *testing.T) {
		fakeDB := newFakeReleaseDB()
		store := db.NewPostgresReleaseStore(fakeDB)

		_, err := store.GetByID(context.Background(), "rel-nonexist")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, loopzeroerrors.ErrReleaseNotFound) {
			t.Errorf("expected ErrReleaseNotFound, got %v", err)
		}
	})
}

func TestReleaseStore_UpdateStatus(t *testing.T) {
	t.Run("valid transition READY to PENDING via GitMirrorCompleted", func(t *testing.T) {
		fakeDB := newFakeReleaseDB()
		now := time.Now().UTC().Truncate(time.Second)
		fakeDB.addRelease(&db.Release{
			ID:            "rel-22222222",
			BuildID:       "build-11111111",
			TenantID:      "tenant-1",
			ProjectID:     "proj-abc12345",
			Environment:   "production",
			Status:        "READY",
			CorrelationID: "corr-001",
			CreatedBy:     "user-1",
			CreatedAt:     now,
			GitMirrorStatus: "pending",
		})

		store := db.NewPostgresReleaseStore(fakeDB)
		release, err := store.UpdateStatus(context.Background(), fakeDB, "rel-22222222", string(statemachine.ReleaseGitMirrorCompleted))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if release.Status != string(statemachine.ReleasePending) {
			t.Errorf("expected status PENDING, got %q", release.Status)
		}
	})

	t.Run("invalid transition returns ErrInvalidTransition", func(t *testing.T) {
		fakeDB := newFakeReleaseDB()
		now := time.Now().UTC().Truncate(time.Second)
		fakeDB.addRelease(&db.Release{
			ID:            "rel-33333333",
			BuildID:       "build-11111111",
			TenantID:      "tenant-1",
			ProjectID:     "proj-abc12345",
			Environment:   "production",
			Status:        string(statemachine.ReleaseSuperseded), // terminal
			CorrelationID: "corr-001",
			CreatedBy:     "user-1",
			CreatedAt:     now,
			GitMirrorStatus: "pending",
		})

		store := db.NewPostgresReleaseStore(fakeDB)
		_, err := store.UpdateStatus(context.Background(), fakeDB, "rel-33333333", string(statemachine.ReleaseGitMirrorCompleted))
		if err == nil {
			t.Fatal("expected error for invalid transition, got nil")
		}
		if !errors.Is(err, loopzeroerrors.ErrInvalidTransition) {
			t.Errorf("expected ErrInvalidTransition, got %v", err)
		}
	})

	t.Run("returns ErrReleaseNotFound when release does not exist", func(t *testing.T) {
		fakeDB := newFakeReleaseDB()
		store := db.NewPostgresReleaseStore(fakeDB)

		_, err := store.UpdateStatus(context.Background(), fakeDB, "rel-nonexist", string(statemachine.ReleaseGitMirrorCompleted))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, loopzeroerrors.ErrReleaseNotFound) {
			t.Errorf("expected ErrReleaseNotFound, got %v", err)
		}
	})
}

func TestReleaseStore_GetActiveByEnv(t *testing.T) {
	t.Run("returns active release", func(t *testing.T) {
		fakeDB := newFakeReleaseDB()
		now := time.Now().UTC().Truncate(time.Second)
		fakeDB.addRelease(&db.Release{
			ID:            "rel-44444444",
			BuildID:       "build-11111111",
			TenantID:      "tenant-1",
			ProjectID:     "proj-abc12345",
			Environment:   "production",
			Status:        string(statemachine.ReleaseActive),
			CorrelationID: "corr-001",
			CreatedBy:     "user-1",
			CreatedAt:     now,
			GitMirrorStatus: "pending",
		})

		store := db.NewPostgresReleaseStore(fakeDB)
		release, err := store.GetActiveByEnv(context.Background(), "tenant-1", "proj-abc12345", "production")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if release == nil {
			t.Fatal("expected active release, got nil")
		}
		if release.ID != "rel-44444444" {
			t.Errorf("expected ID rel-44444444, got %q", release.ID)
		}
	})

	t.Run("returns nil when no active release", func(t *testing.T) {
		fakeDB := newFakeReleaseDB()
		store := db.NewPostgresReleaseStore(fakeDB)

		release, err := store.GetActiveByEnv(context.Background(), "tenant-1", "proj-abc12345", "production")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if release != nil {
			t.Errorf("expected nil release, got %+v", release)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/loopzero/loopzero/api/internal/domain"
)

// =============================================================================
// Bootstrap Store tests (0% → covered)
// =============================================================================

func TestBootstrapStore_Bootstrap_HappyPath(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresBootstrapStore{DB: fake}

	userID, tenantID, created, err := store.Bootstrap(context.Background(), domain.BootstrapRequest{
		AdminLogin:        "octocat",
		AdminGitHubID:     "12345",
		TenantSlug:        "acme",
		TenantDisplayName: "Acme Corp",
		Plan:              "pro",
		CellID:            "cell-sfo3",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userID != "github-octocat" {
		t.Errorf("expected github-octocat, got: %s", userID)
	}
	if tenantID != "tn-acme" {
		t.Errorf("expected tn-acme, got: %s", tenantID)
	}
	if !created {
		t.Error("expected created=true when rows affected")
	}
}

func TestBootstrapStore_Bootstrap_DefaultPlanAndCell(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresBootstrapStore{DB: fake}

	_, _, _, err := store.Bootstrap(context.Background(), domain.BootstrapRequest{
		AdminLogin:    "user1",
		AdminGitHubID: "111",
		TenantSlug:    "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBootstrapStore_Bootstrap_ExecError(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("connection refused")}
	store := &PostgresBootstrapStore{DB: fake}

	_, _, _, err := store.Bootstrap(context.Background(), domain.BootstrapRequest{
		AdminLogin:    "user1",
		AdminGitHubID: "111",
		TenantSlug:    "test",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBootstrapStore_Bootstrap_Idempotent(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresBootstrapStore{DB: fake}

	_, _, created, err := store.Bootstrap(context.Background(), domain.BootstrapRequest{
		AdminLogin:    "user1",
		AdminGitHubID: "111",
		TenantSlug:    "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Error("expected created=false when no rows affected (idempotent)")
	}
}

// =============================================================================
// Tenant Deletion Store tests — DELETED in T1.4 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 6 sub-tests previously here (TestTenantDeletionStore_CancelDeletion_
// Success/Error, _RequestDeletion_ExecError, _WriteTombstone_Success/
// WithFailed/ExecError) ALL used fakeDBTX stub responses to verify
// PostgresTenantDeletionStore behaviors. With T1.4 deleting
// PostgresTenantDeletionStore and replacing it with
// TenantDeletionStoreSqlc (sqlc-backed via SqlcBridge), the stub-based
// pattern is incompatible.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` line 31 (coverage_boost_test.go MIXED):
//
//   - KEEP-INVARIANT preserved structurally by TenantDeletionStoreSqlc:
//     * Idempotent CancelDeletion (DELETE-on-no-rows is a no-op via
//       sqlc's :exec query shape)
//     * Idempotent WriteTombstone (ON CONFLICT (tenant_id) DO NOTHING
//       preserves the original record)
//     * Error wrapping with "tenant deletion store:" prefix preserved
//       verbatim in TenantDeletionStoreSqlc methods
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: NotFoundReturnsNil,
//     CancelDeletion_Success, _Error stub-pass-through tests — sqlc
//     compile-time + sqlcsync gate replace.
//
// Per matrix § Phase 2 reshape window: KEEP-INVARIANT subset moves to
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness is
// wired.

// TestStores_Getters_NotFoundAndQueryError consolidates the canonical
// {NotFound returns nil+nil, QueryError returns err} pair for read-side
// Getter methods on PostgresRepositoryStore. Phase 3 helper.
//
// T1.4 (plan 2026-05-29-sqlc-migration v1.3): removed the
// TenantDeletionStore_GetPendingDeletion case along with the legacy
// PostgresTenantDeletionStore deletion. The KEEP-INVARIANT "NoRows →
// (nil, nil) mapping" for GetPendingDeletion is preserved by
// TenantDeletionStoreSqlc.GetPendingDeletion (explicit
// `errors.Is(err, pgx.ErrNoRows)` check in tenant_wrapper.go); Phase
// 2 reshape moves the behavioral test to a real-PG harness.
func TestStores_Getters_NotFoundAndQueryError(t *testing.T) {
	type getterCall func(fake *fakeDBTX) (interface{}, error)
	cases := []struct {
		name string
		call getterCall
	}{
		{
			name: "RepositoryStore_GetInstallation",
			call: func(fake *fakeDBTX) (interface{}, error) {
				return (&PostgresRepositoryStore{DB: fake}).GetInstallation(context.Background(), 12345)
			},
		},
		{
			name: "RepositoryStore_GetByRepo",
			call: func(fake *fakeDBTX) (interface{}, error) {
				return (&PostgresRepositoryStore{DB: fake}).GetByRepo(context.Background(), "octocat/myapp")
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name+"_NotFound", func(t *testing.T) {
			result, err := c.call(&fakeDBTX{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Reflect-free nil check: typed-nil interface holds a non-nil
			// runtime value, so we need to check the underlying concrete value.
			// Each store returns a *Pointer; nil pointer wrapped in interface
			// is what we expect for the "not found" case.
			if !isNilPointer(result) {
				t.Errorf("expected nil for not-found case, got: %+v", result)
			}
		})
		t.Run(c.name+"_QueryError", func(t *testing.T) {
			_, err := c.call(&fakeDBTX{queryErr: errors.New("db down")})
			if err == nil {
				t.Fatal("expected error from QueryError fake")
			}
		})
	}
}

// isNilPointer returns true if the interface holds a typed nil pointer.
// Workaround for Go interface { *T }-equals-nil gotcha: comparing an
// interface containing a nil *T to literal nil returns false.
func isNilPointer(v interface{}) bool {
	if v == nil {
		return true
	}
	return fmt.Sprintf("%v", v) == "<nil>"
}

// TestTenantDeletionStore_* (4 tests) — DELETED in T1.4 along with the
// Tenant Deletion Store section block above. See tombstone block at
// the top of this section for classification + Phase 2 reshape
// obligation.

// =============================================================================
// Repository Store tests (0% → covered)
// =============================================================================

func TestRepositoryStore_SaveInstallation_Success(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.SaveInstallation(context.Background(), 12345, "octocat", "User", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepositoryStore_SaveInstallation_Error(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("db down")}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.SaveInstallation(context.Background(), 12345, "octocat", "User", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

// GetInstallation NotFound/QueryError consolidated into TestStores_Getters_NotFoundAndQueryError.

func TestRepositoryStore_DeleteInstallation_Success(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.DeleteInstallation(context.Background(), 12345)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepositoryStore_DeleteInstallation_Error(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("db down")}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.DeleteInstallation(context.Background(), 12345)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRepositoryStore_LinkRepository_Success(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.LinkRepository(context.Background(), "tn-acme", "myapp", "octocat/myapp", 12345, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// CHG-2026-04-29-DOGFOOD-REAL-E2E iter 13 / Achado #23.
// project_repositories.repo_full_name has a UNIQUE constraint and the CLI
// `loopzero connect` contract requires idempotency (re-run with same params must
// succeed). Without ON CONFLICT, the second LinkRepository call exploded with
// a constraint violation, surfacing as 500. This test asserts the upsert
// pattern is in the generated SQL so the next refactor can't silently lose it.
func TestRepositoryStore_LinkRepository_UsesUpsertForIdempotency(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresRepositoryStore{DB: fake}

	if err := store.LinkRepository(context.Background(), "tn-acme", "myapp", "octocat/myapp", 12345, "main"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(fake.capturedSQL, "ON CONFLICT (repo_full_name)") {
		t.Errorf("expected upsert ON CONFLICT (repo_full_name), got SQL:\n%s", fake.capturedSQL)
	}
	if !strings.Contains(fake.capturedSQL, "DO UPDATE SET") {
		t.Errorf("expected DO UPDATE SET in upsert, got SQL:\n%s", fake.capturedSQL)
	}
	if !strings.Contains(fake.capturedSQL, "updated_at = now()") {
		t.Errorf("expected updated_at refresh in conflict path, got SQL:\n%s", fake.capturedSQL)
	}
}

func TestRepositoryStore_LinkRepository_Error(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("constraint violation")}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.LinkRepository(context.Background(), "tn-acme", "myapp", "octocat/myapp", 12345, "main")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRepositoryStore_UnlinkRepository_Success(t *testing.T) {
	// AUDIT-21: success requires RowsAffected > 0. Zero rows -> ErrRepositoryNotLinked.
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.UnlinkRepository(context.Background(), "tn-acme", "myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// CHG-2026-04-23-AUDIT-21: previously UnlinkRepository returned nil regardless
// of whether any row was deleted, causing DELETE /projects/{name}/connect to
// return 200 OK even when there was nothing to disconnect. Now the store
// returns ErrRepositoryNotLinked when RowsAffected == 0, letting the handler
// surface 404 instead of a misleading success.
func TestRepositoryStore_UnlinkRepository_NotLinked(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.UnlinkRepository(context.Background(), "tn-acme", "myapp")
	if !errors.Is(err, ErrRepositoryNotLinked) {
		t.Fatalf("want ErrRepositoryNotLinked, got %v", err)
	}
}

func TestRepositoryStore_UnlinkRepository_Error(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("db down")}
	store := &PostgresRepositoryStore{DB: fake}

	err := store.UnlinkRepository(context.Background(), "tn-acme", "myapp")
	if err == nil {
		t.Fatal("expected error")
	}
}

// GetByRepo NotFound/QueryError consolidated into TestStores_Getters_NotFoundAndQueryError.

func TestRepositoryStore_GetByProject_NotFound(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresRepositoryStore{DB: fake}

	repo, err := store.GetByProject(context.Background(), "tn-acme", "myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo != nil {
		t.Error("expected nil for nonexistent project")
	}
}

func TestRepositoryStore_GetByProject_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: errors.New("db down")}
	store := &PostgresRepositoryStore{DB: fake}

	_, err := store.GetByProject(context.Background(), "tn-acme", "myapp")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerateRepoID_HasPrefix(t *testing.T) {
	id := generateRepoID()
	if len(id) < 6 || id[:5] != "repo-" {
		t.Errorf("expected repo- prefix, got: %s", id)
	}
}

func TestGenerateRepoID_IsUnique(t *testing.T) {
	id1 := generateRepoID()
	id2 := generateRepoID()
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
}

// =============================================================================
// Domain Store - CreateWithCFZone tests (0% → covered)
// =============================================================================

func TestDomainStore_CreateWithCFZone_Success(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresDomainStore{DB: fake}

	zoneID := "zone-123"
	err := store.CreateWithCFZone(context.Background(), &domain.CustomDomain{
		ID:       "dom-1",
		TenantID: "tn-acme",
		Domain:   "example.com",
		Status:   "pending_ns",
		CFZoneID: &zoneID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_CreateWithCFZone_ExecError(t *testing.T) {
	fake := &fakeDBTX{execErr: errors.New("db down")}
	store := &PostgresDomainStore{DB: fake}

	err := store.CreateWithCFZone(context.Background(), &domain.CustomDomain{
		ID:       "dom-1",
		TenantID: "tn-acme",
		Domain:   "example.com",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// Release Store — DELETED in T1.1c2c of plan 2026-05-29-sqlc-migration
// v1.3.
// =============================================================================
//
// The 7 release-store sub-tests previously here
// (FindPendingReleases_WithCellID, FindPendingReleases_QueryError,
// FindActiveReleases_WithCellID, FindActiveReleases_QueryError,
// FindPendingReleasesByTenantEnv_QueryError, ListByEnv_QueryError,
// GetLatestByBuildID_NotFound) ALL used `fakeDBTX` with empty / error
// responses to verify that PostgresReleaseStore propagated the
// query-error path and returned empty slices on no-result. Per T0.6
// reshape matrix line 31, `coverage_boost_test.go` is classified MIXED
// with "Mostly stub-error-path padding → DELETE-SCHEMA-MIRROR-DUPLICATE";
// these 7 tests fall in that subset. The error-propagation invariant is
// now structurally guaranteed by sqlc-generated code wrapping
// `pgx.Conn.Query` (errors bubble up unmodified), and the "empty result
// → empty slice" guarantee is preserved by ReleaseStoreSqlc.
//
// The cell-scoped routing invariant (cellID → WHERE cell_id = $1) is
// covered by the static-content tests in cell_scoped_queries_test.go
// against `queries/releases.sql` — a STRONGER invariant than runtime-
// captured SQL since it scans the canonical SoT directly.

// =============================================================================
// Domain Store — ListByTenant query error
// =============================================================================

func TestDomainStore_ListByTenant_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: errors.New("db down")}
	store := &PostgresDomainStore{DB: fake}

	_, _, err := store.ListByTenant(context.Background(), "tn-test", domain.DomainListFilter{})
	if err == nil {
		t.Fatal("expected error")
	}
}

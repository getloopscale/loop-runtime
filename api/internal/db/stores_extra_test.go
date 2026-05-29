// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/domain"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// =============================================================================
// Release Store additional tests — DELETED in T1.1c2c of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// The 7 stub-class tests previously here (TestReleaseStore_GetActiveByEnv_
// Success, TestReleaseStore_GetActiveByEnvTx_NotFoundReturnsNil,
// TestReleaseStore_GetPreviousRelease_Success, TestReleaseStore_GetByID
// ForTenant_Success, TestReleaseStore_GetLatestByBuildID_Success,
// TestReleaseStore_UpdateStatus_NilTxUsesPool, TestReleaseStore_SetGit
// Commit_ExecError) ALL used `fakeDBTX` stub responses to verify that
// PostgresReleaseStore returned what the stub fed it — a pure pass-
// through pattern.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` (T0.6) line 36:
//
//   - 4 are DELETE-SCHEMA-MIRROR-DUPLICATE (stub-pass-through; sqlc
//     compile-time guarantee replaces them):
//     * GetActiveByEnv_Success, GetByIDForTenant_Success,
//       UpdateStatus_NilTxUsesPool, SetGitCommit_ExecError
//
//   - 3 are KEEP-INVARIANT subset reshape (per matrix:
//     GetActiveByEnvTx_NotFoundReturnsNil, GetPreviousRelease_Success,
//     GetLatestByBuildID_Success). These behaviors ARE preserved by
//     ReleaseStoreSqlc (errors.Is(err, pgx.ErrNoRows) → nil mapping,
//     state-machine SafeTransition discipline) AND are exercised by
//     the integration suite under `api/tests/integration/` against
//     real PG via `NewReleaseStoreSqlc`.
//
// Per matrix § "Per-phase action plan" — Phase 2 (T2.1-T2.4 batches) is
// the canonical reshape window for `stores_extra_test.go`. With
// PostgresReleaseStore deleted in T1.1c2c, the stub-based versions
// COULD NOT compile any longer, so the deletion landed here; the Phase
// 2 reshape window now owns the integration coverage for the
// KEEP-INVARIANT subset against a real PG instance.
//
// Cross-refs:
//   - `.claude/knowledge-base/audits/2026-05-29-sqlc-test-reshape-matrix.md`
//   - `api/internal/db/release_wrapper.go` (ReleaseStoreSqlc)
//   - `api/tests/integration/release_store_column_cascade_test.go`
//     (M07 Rekor URL persistence regression — reshaped in T1.1c2c to
//     use NewReleaseStoreSqlc)
//   - `api/tests/integration/rollback_test.go` (rollback flow against
//     real PG; reshaped in T1.1c2c)
//   - `api/tests/integration/concurrent_builds_test.go` (concurrent-
//     deploy 1×202+1×409 invariant; reshaped in T1.1c2c)

// =============================================================================
// Build Store tests — DELETED in T1.2b of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// Build-related stub tests (Setters_NilTxUsesPool, GetBuildApps_NullJSON,
// FindActiveBuilds_Empty, ListByProject_WithData, CancelActiveBuilds_
// QueryError, UpdateStatus_QueuedToFailed, UpdateStatus_RunningToCancelling,
// ListByProjectAndEnv_NoEnvFilter, ListByProjectAndEnv_QueryError,
// ListByProjectAndEnv_NoEnvQueryError, ListByProject_QueryError) ALL used
// fakeDBTX stub responses to verify PostgresBuildStore behaviors. With
// T1.2b deleting PostgresBuildStore and replacing it with BuildStoreSqlc
// (sqlc-backed via SqlcBridge), the stub-based pattern is incompatible.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-test-
// reshape-matrix.md` line 36 (stores_extra_test.go MIXED):
//
//   - KEEP-INVARIANT subset preserved by BuildStoreSqlc structurally:
//       * UpdateStatus_QueuedToFailed + UpdateStatus_RunningToCancelling
//         — state-machine SafeTransition at store layer (D4) + 3-way
//         dispatch (ToRunning/ToTerminal/Other) covered by integration
//         suite under api/tests/integration/concurrent_builds_test.go
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: NilTxUsesPool family,
//     NullJSON, FindActiveBuilds_Empty, ListByProject_WithData,
//     CancelActiveBuilds_QueryError, ListByProjectAndEnv_*Error — sqlc
//     compile-time + sqlcsync gate replace.
//
// Per matrix § Phase 2 reshape window: KEEP-INVARIANT subset moves to
// real-PG-gated unit tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired.


// =============================================================================
// Environment Target Store additional tests
// =============================================================================

// Refactored into TestEnvironmentTargetStore_Mutators in stores_test.go.

func TestEnvironmentTargetStore_SetDesired_ExecError(t *testing.T) {
	fake := &fakeDBTX{execErr: fmt.Errorf("pg down")}
	store := NewPostgresEnvironmentTargetStore(fake, fake)

	err := store.SetDesired(context.Background(), fake, "t1", "p1", "prod", "rel-1", "u1", "c1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// Domain Store additional tests
// =============================================================================

func TestDomainStore_UpdateStatus_ExecError(t *testing.T) {
	fake := &fakeDBTX{execErr: fmt.Errorf("connection refused")}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateStatus(context.Background(), "test.com", "active")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDomainStore_UpdateTLS_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresDomainStore{DB: fake}

	now := time.Now()
	err := store.UpdateTLS(context.Background(), "example.com", "active", &now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_UpdateFailure_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateFailure(context.Background(), "example.com", "failed", "DNS timeout")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_UpdateCNAMECheck_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateCNAMECheck(context.Background(), "example.com", time.Now(), 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// =============================================================================
// Team Store additional tests — DELETED in T2.1 #5 along with the
// Team Store section in stores_test.go (same tombstone block applies)
// =============================================================================


// =============================================================================
// Idempotency Store additional tests
// =============================================================================

func TestIdempotencyStore_Get_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: fmt.Errorf("db down")}
	store := &PostgresIdempotencyStore{DB: fake}

	_, err := store.Get(context.Background(), "key-1", "t1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestIdempotencyStore_Cleanup_Error(t *testing.T) {
	fake := &fakeDBTX{execErr: fmt.Errorf("db locked")}
	store := &PostgresIdempotencyStore{DB: fake}

	_, err := store.Cleanup(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// WriteAuditLog tests — DELETED in T1.5 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 2 sub-tests previously here (TestWriteAuditLog_Success +
// _ExecError) used fakeDBTX stubs to verify legacy
// ExecQueryExecutor-based WriteAuditLog. With T1.5 widening the
// signature to db.DBTX + dispatching through SqlcBridge to sqlc-
// generated InsertAuditLog, the fake pattern is incompatible — the
// wrapper returns a typed error when the DBTX doesn't satisfy
// SqlcBridge (fail-fast per T0.6 reshape matrix).
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` line 36 (stores_extra_test.go MIXED):
//
//   - KEEP-INVARIANT preserved structurally by sqlc-backed
//     WriteAuditLog:
//     * Successful INSERT: sqlc-generated InsertAuditLog dispatches
//       the same VALUES tuple from queries/audit.sql — column shape
//       unchanged (M-3 audit-callsite-diff verified by SQL inspection
//       in the queries file vs the deleted audit.go INSERT)
//     * ExecError wrap with "audit: insert:" prefix preserved
//       verbatim in audit_wrapper.go
//
// Per matrix § Phase 2 reshape window: KEEP-INVARIANT subset moves to
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired (out of T1.5 scope).

// =============================================================================
// Domain Store Background methods tests
// =============================================================================

func TestDomainStore_FindAndClaimVerified_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresDomainStore{DB: rec}

	domains, err := store.FindAndClaimVerified(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if domains == nil {
		t.Error("expected non-nil slice")
	}
	if len(domains) != 0 {
		t.Errorf("expected 0, got: %d", len(domains))
	}
}

func TestDomainStore_FindRemoving_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresDomainStore{DB: rec}

	domains, err := store.FindRemoving(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if domains == nil {
		t.Error("expected non-nil slice")
	}
}

// TestDomainStore_Finders_EmptyReturnsNonNilSlice exercises every read-side
// finder on PostgresDomainStore against an empty fake DB. Each must return
// a non-nil empty slice (NEVER nil) — the contract that downstream loops
// can `for _, d := range result` without nil-check. Phase 3 helper: was
// 4 near-clone test functions each doing the same 8-line assertion.
func TestDomainStore_Finders_EmptyReturnsNonNilSlice(t *testing.T) {
	type finderCall func(s *PostgresDomainStore) ([]domain.CustomDomain, error)
	cases := []struct {
		name string
		call finderCall
	}{
		{
			name: "FindActiveForCNAMECheck",
			call: func(s *PostgresDomainStore) ([]domain.CustomDomain, error) {
				return s.FindActiveForCNAMECheck(context.Background(), time.Now().Add(-24*time.Hour))
			},
		},
		{
			name: "FindStalePending",
			call: func(s *PostgresDomainStore) ([]domain.CustomDomain, error) {
				return s.FindStalePending(context.Background(), 24*time.Hour)
			},
		},
		{
			name: "FindStaleProvisioning",
			call: func(s *PostgresDomainStore) ([]domain.CustomDomain, error) {
				return s.FindStaleProvisioning(context.Background(), 1*time.Hour)
			},
		},
		{
			name: "FindActiveWithTLSExpiry",
			call: func(s *PostgresDomainStore) ([]domain.CustomDomain, error) {
				return s.FindActiveWithTLSExpiry(context.Background(), 30)
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &recordingDBTX{}
			store := &PostgresDomainStore{DB: rec}
			domains, err := c.call(store)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if domains == nil {
				t.Error("expected non-nil slice")
			}
		})
	}
}

func TestDomainStore_ListActiveByApp_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresDomainStore{DB: rec}

	domains, err := store.ListActiveByApp(context.Background(), "t1", "proj", "api", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if domains == nil {
		t.Error("expected non-nil slice")
	}
}

// =============================================================================
// Domain Store CF Zone methods tests
// =============================================================================

func TestDomainStore_UpdateCFZoneStatus_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateCFZoneStatus(context.Background(), "example.com", "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_UpdateCFZoneStatus_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateCFZoneStatus(context.Background(), "nonexistent.com", "active")
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

func TestDomainStore_FindPendingNS_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresDomainStore{DB: rec}

	domains, err := store.FindPendingNS(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if domains == nil {
		t.Error("expected non-nil slice")
	}
}

func TestDomainStore_FindNSActive_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresDomainStore{DB: rec}

	domains, err := store.FindNSActive(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if domains == nil {
		t.Error("expected non-nil slice")
	}
}

// =============================================================================
// Cell Store tests
// =============================================================================

func TestCellStore_CellTenantCounts_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresCellStore{DB: rec}

	counts, err := store.CellTenantCounts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if counts == nil {
		t.Error("expected non-nil map")
	}
}

func TestCellStore_CountByCell_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresCellStore{DB: rec}

	counts, err := store.CountByCell(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if counts == nil {
		t.Error("expected non-nil map")
	}
}

func TestCellStore_GetTenantCellID_NotFound(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresCellStore{DB: fake}

	_, err := store.GetTenantCellID(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent tenant")
	}
}

func TestCellStore_CellTenantCounts_WithData(t *testing.T) {
	fake := &fakeDBTX{
		queryRows: &fakeRowsMulti{
			data: [][]any{
				{"cell-sfo3", int(3)},
				{"cell-nyc3", int(5)},
			},
		},
	}
	store := &PostgresCellStore{DB: fake}

	counts, err := store.CellTenantCounts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if counts["cell-sfo3"] != 3 {
		t.Errorf("expected 3 for cell-sfo3, got: %d", counts["cell-sfo3"])
	}
}

func TestCellStore_CellTenantCounts_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: fmt.Errorf("db down")}
	store := &PostgresCellStore{DB: fake}

	_, err := store.CellTenantCounts(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCellStore_GetTenantCellID_Success(t *testing.T) {
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{values: []any{"cell-sfo3"}},
	}
	store := &PostgresCellStore{DB: fake}

	cellID, err := store.GetTenantCellID(context.Background(), "tn-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cellID != "cell-sfo3" {
		t.Errorf("expected cell-sfo3, got: %s", cellID)
	}
}

// Keep import anchored
var _ = loopzeroerrors.ErrBuildNotFound

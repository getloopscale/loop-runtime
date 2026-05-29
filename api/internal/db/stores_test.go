// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/domain"
)

// =============================================================================
// Test fakes — enhanced DBTX implementation for unit testing
// =============================================================================

// fakeDBTX implements DBTX with configurable responses per call.
type fakeDBTX struct {
	// execResult is returned by Exec calls.
	execResult ExecResult
	// execErr is returned by Exec calls.
	execErr error
	// queryRows is returned by Query calls.
	queryRows Rows
	// queryErr is returned by Query calls.
	queryErr error
	// queryRowResult is returned by QueryRow calls.
	queryRowResult Row
	// capturedSQL records the last SQL executed via Exec.
	capturedSQL string
	// capturedArgs records the last args passed to Exec.
	capturedArgs []any
	// execCalls counts how many times Exec was called.
	execCalls int
}

func (f *fakeDBTX) Query(_ context.Context, sql string, args ...any) (Rows, error) {
	f.capturedSQL = sql
	f.capturedArgs = args
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.queryRows != nil {
		return f.queryRows, nil
	}
	return &sliceRows{}, nil
}

func (f *fakeDBTX) Exec(_ context.Context, sql string, args ...any) (ExecResult, error) {
	f.capturedSQL = sql
	f.capturedArgs = args
	f.execCalls++
	return f.execResult, f.execErr
}

func (f *fakeDBTX) QueryRow(_ context.Context, sql string, args ...any) Row {
	f.capturedSQL = sql
	f.capturedArgs = args
	if f.queryRowResult != nil {
		return f.queryRowResult
	}
	return emptyRow{}
}

// fakeRowWithData returns preset values on Scan.
type fakeRowWithData struct {
	values []any
	err    error
}

func (r *fakeRowWithData) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		if i >= len(r.values) {
			break
		}
		assignValue(d, r.values[i])
	}
	return nil
}

// fakeRowsMulti supports returning multiple rows via the Rows interface.
type fakeRowsMulti struct {
	data  [][]any
	idx   int
	err   error
	scErr error
}

func (r *fakeRowsMulti) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRowsMulti) Scan(dest ...any) error {
	if r.scErr != nil {
		return r.scErr
	}
	row := r.data[r.idx-1]
	for i, d := range dest {
		if i >= len(row) {
			break
		}
		assignValue(d, row[i])
	}
	return nil
}

func (r *fakeRowsMulti) Err() error { return r.err }
func (r *fakeRowsMulti) Close()     {}

// assignValue assigns src into the dest pointer using type assertion.
func assignValue(dest any, src any) {
	switch d := dest.(type) {
	case *string:
		if v, ok := src.(string); ok {
			*d = v
		}
	case *int:
		if v, ok := src.(int); ok {
			*d = v
		}
	case *int64:
		if v, ok := src.(int64); ok {
			*d = v
		}
	case *bool:
		if v, ok := src.(bool); ok {
			*d = v
		}
	case *time.Time:
		if v, ok := src.(time.Time); ok {
			*d = v
		}
	case **time.Time:
		if v, ok := src.(*time.Time); ok {
			*d = v
		}
	case **int:
		if v, ok := src.(*int); ok {
			*d = v
		}
	case **string:
		if v, ok := src.(*string); ok {
			*d = v
		}
	case *json.RawMessage:
		switch v := src.(type) {
		case json.RawMessage:
			*d = v
		case []byte:
			*d = json.RawMessage(v)
		case string:
			*d = json.RawMessage(v)
		}
	case *[]byte:
		switch v := src.(type) {
		case []byte:
			*d = v
		case string:
			*d = []byte(v)
		}
	}
}

// makeBuildRowValues creates a build row values slice matching scanBuild order.
func makeBuildRowValues(b Build) []any {
	return []any{
		b.ID, b.TenantID, b.ProjectID, b.Environment, b.Status,
		b.CommitSHA, b.CorrelationID, b.CreatedBy,
		b.CreatedAt, b.StartedAt, b.FinishedAt, b.ErrorMessage,
		b.DurationSeconds, b.WorkflowID,
	}
}

// makeReleaseRowValues creates a release row values slice matching scanRelease order.
// CHG-2026-04-25-ATTESTATION-WIRE: includes the 3 nullable signature columns.
func makeReleaseRowValues(r Release) []any {
	return []any{
		r.ID, r.BuildID, r.TenantID, r.ProjectID, r.Environment,
		r.Status, r.CorrelationID, r.CreatedBy,
		r.CreatedAt, r.GitCommit, r.GitMirrorStatus,
		r.Apps, r.Artifacts, r.SideEffects,
		r.ImageDigest, r.ImageSignature, r.ImageSignedAt,
	}
}

// =============================================================================
// safeTransition tests (DELETED iter 38)
// =============================================================================
//
// TestSafeTransition was deleted iter 38 after the local `safeTransition`
// helper in build_store.go was removed. Coverage moved to
// `pkg/statemachine/safe_transition_test.go` which exercises every
// typed wrapper's `SafeTransition` method uniformly (Build + Release +
// 9 other machines). The previous Build+Release-specific table-driven
// test is subsumed by the broader per-machine coverage.


// =============================================================================
// Build Store tests — DELETED in T1.2b of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 22 sub-tests previously here (TestBuildStore_Create_*,
// _GetByID_*, _HasActiveBuild_*, _UpdateStatus_*,
// _Setters_*, _SetAppsSpec_*, _GetBuildApps_*,
// _ListByProject*, _CancelActiveBuilds_*) ALL used `fakeDBTX` to verify
// PostgresBuildStore behaviors against stub responses. With T1.2b
// deleting PostgresBuildStore and replacing it with BuildStoreSqlc
// (sqlc-backed via SqlcBridge), these tests can no longer compile.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` line 37 (`stores_test.go` MIXED):
//
//   - KEEP-INVARIANT subset preserved structurally by BuildStoreSqlc:
//       * Create_UniqueViolationReturnsDeployInProgress —
//         isPGUniqueViolation in build_wrapper.go covers pgconn.PgError
//         23505 + legacy UniqueViolationError dual paths
//       * HasActiveBuild_True/False — concurrent-deploy invariant
//         (Rule 6 + ARCH-002) verified by integration suite under
//         api/tests/integration/concurrent_builds_test.go (T1.2b
//         reshape to NewBuildStoreSqlc)
//       * UpdateStatus_ValidTransition + UpdateStatus_InvalidTransition
//         panic discipline — guaranteed by state-machine SafeTransition
//         at store layer (D4) + buildMachine TypedMachine compile-time
//         constants
//       * ListByProjectAndEnv_ClampsLimit — limit clamp (1-50, default
//         20) preserved verbatim in BuildStoreSqlc.ListByProjectAndEnv
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: Create_SetsQueuedStatus*,
//     Create_PreservesExistingID, Create_GenericErrorWrapped,
//     Setters_Success_NotFound_NilTx, SetAppsSpec_MarshalAndExec,
//     GetBuildApps_*, *NilTxUsesPool family, _EmptyReturnsEmptySlice —
//     sqlc compile-time + sqlcsync gate replace.
//
// Per matrix § "Per-phase action plan" Phase 2 (T2.1-T2.4 batches):
// additional KEEP-INVARIANT subset reshape for build store belongs to
// Phase 2 when a real-PG-gated unit test harness is wired (pgxmock or
// LOOPZERO_E2E_DATABASE_URL).
// =============================================================================
// Release Store tests — DELETED in T1.1c2c of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 19 sub-tests previously here (TestReleaseStore_Create_*,
// _GetByID_*, _UpdateStatus_*, _GetActiveByEnv_*, _GetPreviousRelease_*,
// _GetByIDForTenant_*, _GetLatestByBuildID_*, _SetGitCommit_*,
// _FindPendingReleases_*, _FindActiveReleases_*,
// _FindPendingReleasesByTenantEnv_*, _ListByEnv_*, _GetByIDTx_*) ALL
// used `fakeDBTX` to verify PostgresReleaseStore behaviors against stub
// responses. With T1.1c2c deleting PostgresReleaseStore and replacing
// it with ReleaseStoreSqlc (sqlc-backed via SqlcBridge), these tests
// can no longer compile.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` line 37 (`stores_test.go` MIXED):
//
//   - KEEP-INVARIANT subset (per matrix): UpdateStatus_ValidTransition,
//     UpdateStatus_InvalidTransition (panic discipline), UpdateStatus_
//     NotFound (NoRows → ErrReleaseNotFound), *_NotFoundReturnsNil
//     family. These behaviors ARE preserved by ReleaseStoreSqlc:
//       - State-machine SafeTransition at store layer (D4 invariant) —
//         verified by compile-time assertion `var _ ReleaseStore =
//         (*ReleaseStoreSqlc)(nil)` in release_wrapper.go
//       - NoRows → ErrReleaseNotFound mapping explicit in wrapper
//         (release_wrapper.go GetByID, GetByIDTx, GetByIDForTenant)
//       - Integration suite under api/tests/integration/ exercises the
//         same behaviors against real PG via NewReleaseStoreSqlc
//         (reshaped in T1.1c2c)
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: Create_SetsReadyStatus*,
//     Create_PreservesExistingID, Create_ExecError, _NilTxUsesPool
//     family, _ReturnsEmptySlice family, _Empty family — sqlc compile-
//     time + sqlcsync gate replace.
//
// Per matrix § "Per-phase action plan" Phase 2 (T2.1-T2.4 batches):
// additional KEEP-INVARIANT subset reshape for release belongs to Phase
// 2 when a real-PG-gated unit test harness is wired (pgxmock or
// LOOPZERO_E2E_DATABASE_URL).


// =============================================================================
// Domain Store tests
// =============================================================================

func TestDomainStore_Create_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresDomainStore{DB: fake}

	d := &domain.CustomDomain{
		ID: "dom-1", TenantID: "t1", ProjectName: "p1",
		AppName: "api", Environment: "prod", Domain: "example.com",
		Status: "pending_verification", VerificationToken: "tok-1",
	}

	err := store.Create(context.Background(), d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_Create_UniqueViolation(t *testing.T) {
	fake := &fakeDBTX{
		execErr: &UniqueViolationError{Constraint: "custom_domains_domain_key"},
	}
	store := &PostgresDomainStore{DB: fake}

	d := &domain.CustomDomain{
		ID: "dom-1", TenantID: "t1", ProjectName: "p1",
		AppName: "api", Environment: "prod", Domain: "example.com",
		Status: "pending_verification", VerificationToken: "tok-1",
	}

	err := store.Create(context.Background(), d)
	if !errors.Is(err, domain.ErrDomainExists) {
		t.Errorf("expected ErrDomainExists, got: %v", err)
	}
}

// TestDomainStore_StatusMutators exercises UpdateStatus + TransitionStatus
// with their canonical {Success, RowsAffected=0 error} pair. Phase 3 helper:
// was 4 near-clone test functions. The "0 rows affected" error message
// differs ("domain not found" vs "transition failed") so the table includes
// an errSubstring assertion.
func TestDomainStore_StatusMutators(t *testing.T) {
	type mutatorCall func(s *PostgresDomainStore) error
	cases := []struct {
		name         string
		call         mutatorCall
		errSubstring string // expected substring in "0 rows affected" error path
	}{
		{
			name:         "UpdateStatus",
			call:         func(s *PostgresDomainStore) error { return s.UpdateStatus(context.Background(), "example.com", "active") },
			errSubstring: "domain not found",
		},
		{
			name:         "TransitionStatus",
			call:         func(s *PostgresDomainStore) error { return s.TransitionStatus(context.Background(), "example.com", "verified", "provisioning") },
			errSubstring: "transition failed",
		},
	}
	for _, c := range cases {
		t.Run(c.name+"_Success", func(t *testing.T) {
			fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
			store := &PostgresDomainStore{DB: fake}
			if err := c.call(store); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		t.Run(c.name+"_NotFoundOrStale", func(t *testing.T) {
			fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
			store := &PostgresDomainStore{DB: fake}
			err := c.call(store)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.errSubstring) {
				t.Errorf("expected %q, got: %v", c.errSubstring, err)
			}
		})
	}
}

func TestDomainStore_Delete_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresDomainStore{DB: fake}

	err := store.Delete(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_Delete_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.Delete(context.Background(), "nonexistent.com")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDomainStore_CountByTenant_Success(t *testing.T) {
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{values: []any{int(5)}},
	}
	store := &PostgresDomainStore{DB: fake}

	count, err := store.CountByTenant(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5, got: %d", count)
	}
}

func TestDomainStore_UpdateVerified_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateVerified(context.Background(), "example.com", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDomainStore_UpdateVerified_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateVerified(context.Background(), "nonexistent.com", time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDomainStore_UpdateTLS_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateTLS(context.Background(), "nonexistent.com", "active", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDomainStore_UpdateFailure_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateFailure(context.Background(), "nonexistent.com", "failed", "timeout")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDomainStore_UpdateCNAMECheck_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresDomainStore{DB: fake}

	err := store.UpdateCNAMECheck(context.Background(), "nonexistent.com", time.Now(), 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDomainStore_GetByDomainAndTenant_NotFoundReturnsNil(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresDomainStore{DB: rec}

	d, err := store.GetByDomainAndTenant(context.Background(), "example.com", "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != nil {
		t.Error("expected nil for not found")
	}
}

// =============================================================================
// Project Store tests — DELETED in T2.1 #4 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 8 sub-tests previously here (TestProjectStore_CreateProject_Success/
// UniqueViolation, _EnsureProject_Success/ConflictIsIdempotent,
// _DeleteProject_Success/NotFound, _GetByName_NotFoundReturnsNil,
// _ListByTenant_EmptyReturnsEmptySlice) used fakeDBTX/recordingDBTX
// stubs to verify PostgresProjectStore behaviors. With T2.1 #4
// migrating to sqlc-backed via SqlcBridge, the fake pattern is
// incompatible.
//
// KEEP-INVARIANT preserved structurally by project_wrapper.go:
//   - 23505 unique_violation → domain.ErrProjectExists via
//     isPGUniqueViolation helper
//   - EnsureProject idempotent (ON CONFLICT DO NOTHING in
//     queries/projects.sql)
//   - DeleteProject 0-rows-affected → "project not found" error
//   - GetByName NoRows → (nil, nil) via errors.Is(err, pgx.ErrNoRows)
//   - ListByTenant empty result → empty slice (make([]T, 0, len(rows))
//     pattern; tenant predicate in SQL)
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired.

func TestGenerateProjectID_Format(t *testing.T) {
	id := GenerateProjectID()
	if !strings.HasPrefix(id, "proj-") {
		t.Errorf("expected prefix 'proj-', got: %s", id)
	}
	// proj- + 8 hex chars = 13 chars
	if len(id) != 13 {
		t.Errorf("expected length 13, got: %d", len(id))
	}
}

func TestGenerateProjectID_Unique(t *testing.T) {
	ids := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := GenerateProjectID()
		if ids[id] {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}

// =============================================================================
// Tenant Store tests — DELETED in T1.4 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 5 sub-tests previously here (TestTenantStore_ListTenantIDsByPattern_
// Success/Empty, _DeleteTenantsFKOrdered_EmptyNoOp/ExecutesAllSteps/
// ErrorStopsExecution) + the failOnNthExecDBTX wrapper + TestTruncateSQL
// (testing the now-removed truncateSQL helper that lived in
// tenant_store.go) — ALL used fakeDBTX-based stubs to verify
// PostgresTenantStore behaviors. With T1.4 deleting PostgresTenantStore
// and replacing it with TenantStoreSqlc (sqlc-backed via SqlcBridge),
// the stub pattern is incompatible.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` line 37 (stores_test.go MIXED):
//
//   - KEEP-INVARIANT subset preserved structurally by TenantStoreSqlc:
//     * Empty-input no-op (`if len(tenantIDs) == 0 { return counts, nil }`
//       in TenantStoreSqlc.DeleteTenantsFKOrdered) — direct early
//       return, structurally identical to legacy
//     * 8-step FK-ordered cascade preserved verbatim — same step
//       sequence (environment_targets → releases → build_apps →
//       builds → projects → subscriptions → tenant_members → tenants)
//       with step-name annotated error wrapping (more informative
//       than legacy truncateSQL output)
//     * Error stops execution: the wrapper iterates the steps slice
//       and returns on first non-nil err — same loop discipline as
//       legacy
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: ListTenantIDsByPattern_
//     Success/Empty stub-pass-through; failOnNthExecDBTX helper
//     (legacy-specific test scaffolding); TestTruncateSQL (testing a
//     helper that no longer exists). sqlc compile-time replaces.
//
// Per matrix § Phase 2 reshape window: KEEP-INVARIANT subset moves to
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness is
// wired.


// =============================================================================
// Team Store tests — DELETED in T2.1 #5 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 7 sub-tests previously here (TestTeamStore_AddMember_Success,
// _RemoveMember_Success, _UpdateMemberRole_Success/NotFound,
// _ListMembers_Empty, _GetMember_NotFound, _GetUserByUsername_NotFound)
// used fakeDBTX/recordingDBTX stubs to verify PostgresTeamStore.
// With T2.1 #5 migrating to sqlc-backed via SqlcBridge, the fake
// pattern is incompatible.
//
// KEEP-INVARIANT preserved structurally by team_wrapper.go:
//   - AddMember idempotent (ON CONFLICT DO NOTHING in
//     queries/teams.sql)
//   - RemoveMember idempotent (silent on missing rows)
//   - UpdateMemberRole 0-rows-affected → domain.ErrMemberNotFound
//   - GetMember/GetUsernameByID/GetUserByUsername NoRows mappings
//   - AUDIT-7 username via JOIN to users.username (provider-agnostic)
//   - ListMembers returns empty slice (not nil) for consistent JSON
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired.

// CHG-2026-04-23-AUDIT-7: TestStripGitHubPrefix removed along with the
// helper it tested. Username is now a column on users, not derived from
// the id. See migration 000037 and team_store.go.

// =============================================================================
// Membership Store tests
// =============================================================================

func TestMembershipStore_GetMemberships_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresMembershipStore{DB: rec}

	memberships, err := store.GetMemberships(context.Background(), "github-user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memberships) != 0 {
		t.Error("expected nil or empty slice")
	}
}

func TestMembershipStore_GetMemberships_Success(t *testing.T) {
	fake := &fakeDBTX{
		queryRows: &fakeRowsMulti{
			data: [][]any{
				{"tenant-1", "admin"},
				{"tenant-2", "member"},
			},
		},
	}
	store := &PostgresMembershipStore{DB: fake}

	memberships, err := store.GetMemberships(context.Background(), "github-user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memberships) != 2 {
		t.Fatalf("expected 2, got: %d", len(memberships))
	}
	if memberships[0].TenantID != "tenant-1" {
		t.Errorf("expected tenant-1, got: %s", memberships[0].TenantID)
	}
	if memberships[0].Role != "admin" {
		t.Errorf("expected admin, got: %s", memberships[0].Role)
	}
}

func TestMembershipStore_GetMemberships_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: fmt.Errorf("db down")}
	store := &PostgresMembershipStore{DB: fake}

	_, err := store.GetMemberships(context.Background(), "user1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// Environment Target Store tests — DELETED in T2.1 #6 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 8 sub-tests previously here (TestEnvironmentTargetStore_Mutators
// with 3 mutators × 2 variants = 6 sub-tests +
// _Get_NotFoundReturnsNil + _ListByProject_EmptyReturnsEmptySlice)
// used fakeDBTX/recordingDBTX stubs to verify
// PostgresEnvironmentTargetStore. With T2.1 #6 migrating to sqlc-
// backed via SqlcBridge, the fake pattern is incompatible.
//
// KEEP-INVARIANT preserved structurally by
// environment_target_wrapper.go:
//   - SetDesired UPSERT (ON CONFLICT (tenant_id, project_id,
//     environment) DO UPDATE in queries/environment_targets.sql)
//   - SetActive runs after ArgoCD confirms sync (caller discipline)
//   - tx-required form for mutating methods (sqlctxprop gate
//     validates)
//   - Nil-tx fallback to s.Exec preserved verbatim in
//     resolveSqlcDBTX helper
//   - Get NoRows → (nil, nil) via errors.Is(err, pgx.ErrNoRows)
//   - ListByProject empty → empty slice (make([]T, 0, len(rows)))
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired.

// =============================================================================
// Idempotency Store tests — DELETED in T2.1 (Batch A) of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 4 sub-tests previously here (TestIdempotencyStore_Get_NotFound
// ReturnsNil, _Get_Success, _Store_Success, _Cleanup_Success) used
// fakeDBTX-based stubs to verify PostgresIdempotencyStore behaviors.
// With T2.1 widening the DB field to db.DBTX + sqlc-backed methods
// dispatching through SqlcBridge, the fake pattern is incompatible.
//
// KEEP-INVARIANT preserved structurally by sqlc-backed
// PostgresIdempotencyStore (idempotency_wrapper.go):
//   - Get NoRows → (nil, nil) via errors.Is(err, pgx.ErrNoRows)
//   - Tenant predicate on Get (queries/idempotency.sql carries
//     `tenant_id = $2`, gate enforces)
//   - INSERT shape preserved (sqlc emits same VALUES tuple)
//   - Cleanup returns int64 row count (sqlc :execrows shape matches
//     legacy)
//   - 24-hour TTL applied at Store-time via idempotencyTTL constant
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired.

// =============================================================================
// Audit Store tests
// =============================================================================

func TestAuditStore_ListAuditEntries_NilDB(t *testing.T) {
	store := &PostgresAuditStore{DB: nil}

	entries, total, err := store.ListAuditEntries(context.Background(), "t1", domain.AuditFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 total, got: %d", total)
	}
	if entries != nil {
		t.Error("expected nil entries for nil DB")
	}
}

// TestAuditStore_ListAuditEntries_EmptyResult — DELETED in T1.5 of
// plan 2026-05-29-sqlc-migration v1.3.
//
// The fakeDBTX-based test cannot run against the sqlc-backed
// PostgresAuditStore.ListAuditEntries — the wrapper bridges to
// generated.DBTX via SqlcBridge, which fakeDBTX deliberately doesn't
// implement (T0.6 reshape matrix). The empty-result behavior is
// PRESERVED structurally: when CountAuditLogForTenant returns 0 +
// ListAuditLogForTenant returns no rows, the wrapper emits
// `make([]domain.AuditEntryResult, 0, len(rows))` which is a non-nil
// empty slice (matches legacy `entries = []domain.AuditEntryResult{}`
// fallback). Phase 2 reshape moves this to a real-PG integration test.
//
// TestAuditStore_ListAuditEntries_NilDB (above) STILL PASSES — the
// nil-pool early return runs BEFORE the SqlcBridge dispatch, so the
// nil-DB invariant is hermetically testable.

// =============================================================================
// Build GC Store tests
// =============================================================================

func TestBuildGCStore_DeleteOldBuilds_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 15}}
	store := &PostgresBuildGCStore{DB: fake}

	count, err := store.DeleteOldBuilds(context.Background(), 30*24*time.Hour, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 15 {
		t.Errorf("expected 15, got: %d", count)
	}
}

// (TestBuildGCStore_DeleteOldEvents_Success removed in
//  dogfood-2026-05-08-fixes T1.1 / ADR D2: the production method was
//  deleted along with the inexistent `build_events` table reference.)

func TestBuildGCStore_DeleteOldWebhookDeliveries_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 50}}
	store := &PostgresBuildGCStore{DB: fake}

	count, err := store.DeleteOldWebhookDeliveries(context.Background(), 7*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 50 {
		t.Errorf("expected 50, got: %d", count)
	}
}

func TestBuildGCStore_DeleteOldBuilds_Error(t *testing.T) {
	fake := &fakeDBTX{execErr: fmt.Errorf("table locked")}
	store := &PostgresBuildGCStore{DB: fake}

	_, err := store.DeleteOldBuilds(context.Background(), 30*24*time.Hour, 10)
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// UniqueViolationError tests
// =============================================================================

func TestUniqueViolationError_ErrorMessage(t *testing.T) {
	err := &UniqueViolationError{Constraint: "builds_pkey"}
	if err.Error() != "unique violation: builds_pkey" {
		t.Errorf("expected 'unique violation: builds_pkey', got: %s", err.Error())
	}
}

// =============================================================================
// DatabaseConfig tests
// =============================================================================

func TestDatabaseConfig_DSN_DefaultSSLMode(t *testing.T) {
	cfg := DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "loopzero",
		User:     "loopzero",
		Password: "secret",
	}

	dsn := cfg.DSN()
	if !strings.Contains(dsn, "sslmode=") {
		t.Error("expected sslmode in DSN")
	}
	if !strings.Contains(dsn, "loopzero:secret@localhost:5432/loopzero") {
		t.Errorf("unexpected DSN format: %s", dsn)
	}
}

func TestGetDatabaseConfig_Defaults(t *testing.T) {
	cfg := GetDatabaseConfig()
	if cfg.Host == "" {
		t.Error("expected non-empty host")
	}
	if cfg.Port == 0 {
		t.Error("expected non-zero port")
	}
	if cfg.Database == "" {
		t.Error("expected non-empty database")
	}
}

// =============================================================================
// Connection helper tests
// =============================================================================

func TestGetEnvOrDefault_FallbackValue(t *testing.T) {
	val := getEnvOrDefault("LOOPZERO_NONEXISTENT_ENV_VAR_FOR_TESTING", "fallback")
	if val != "fallback" {
		t.Errorf("expected 'fallback', got: %s", val)
	}
}

// =============================================================================
// Domain Store — ListByTenant filter building
// =============================================================================

func TestDomainStore_ListByTenant_DefaultLimitIs50(t *testing.T) {
	// The count query returns 0, the list query returns empty
	queryCallCount := 0
	fake := &countingDBTX{
		queryRowResult: &fakeRowWithData{values: []any{int(0)}},
		queryRows:      &fakeRowsMulti{data: [][]any{}},
		queryCallCount: &queryCallCount,
	}
	store := &PostgresDomainStore{DB: fake}

	_, _, err := store.ListByTenant(context.Background(), "t1", domain.DomainListFilter{
		Limit: 0, // Should default to 50
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the limit arg is 50 (the second-to-last arg in the list query)
	if len(fake.capturedArgs) > 0 {
		// The last query's args should include limit=50
		foundLimit := false
		for _, arg := range fake.capturedArgs {
			if v, ok := arg.(int); ok && v == 50 {
				foundLimit = true
			}
		}
		if !foundLimit {
			t.Errorf("expected limit=50 in args, got: %v", fake.capturedArgs)
		}
	}
}

// countingDBTX wraps fakeDBTX to count Query calls and track args across calls.
type countingDBTX struct {
	queryRowResult Row
	queryRows      Rows
	queryCallCount *int
	capturedSQL    string
	capturedArgs   []any
}

func (c *countingDBTX) Query(_ context.Context, sql string, args ...any) (Rows, error) {
	*c.queryCallCount++
	c.capturedSQL = sql
	c.capturedArgs = args
	if c.queryRows != nil {
		return c.queryRows, nil
	}
	return &sliceRows{}, nil
}

func (c *countingDBTX) Exec(_ context.Context, _ string, _ ...any) (ExecResult, error) {
	return ExecResult{}, nil
}

func (c *countingDBTX) QueryRow(_ context.Context, sql string, args ...any) Row {
	c.capturedSQL = sql
	c.capturedArgs = args
	if c.queryRowResult != nil {
		return c.queryRowResult
	}
	return emptyRow{}
}

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

	"github.com/loopzero/loopzero/api/internal/middleware"
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
// Project Store tests
// =============================================================================

func TestProjectStore_CreateProject_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresProjectStore{DB: fake}

	result, err := store.CreateProject(context.Background(), domain.CreateProjectInput{
		TenantID: "t1", Name: "my-project", CreatedBy: "user1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "my-project" {
		t.Errorf("expected 'my-project', got: %s", result.Name)
	}
	if !strings.HasPrefix(result.ID, "proj-") {
		t.Errorf("expected ID to start with 'proj-', got: %s", result.ID)
	}
}

func TestProjectStore_CreateProject_UniqueViolation(t *testing.T) {
	fake := &fakeDBTX{
		execErr: &UniqueViolationError{Constraint: "projects_tenant_id_name"},
	}
	store := &PostgresProjectStore{DB: fake}

	_, err := store.CreateProject(context.Background(), domain.CreateProjectInput{
		TenantID: "t1", Name: "my-project", CreatedBy: "user1",
	})
	if !errors.Is(err, domain.ErrProjectExists) {
		t.Errorf("expected ErrProjectExists, got: %v", err)
	}
}

func TestProjectStore_EnsureProject_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresProjectStore{DB: fake}

	err := store.EnsureProject(context.Background(), fake, "t1", "my-project", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectStore_EnsureProject_ConflictIsIdempotent(t *testing.T) {
	// ON CONFLICT DO NOTHING — exec returns 0 rows but no error
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresProjectStore{DB: fake}

	err := store.EnsureProject(context.Background(), fake, "t1", "my-project", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectStore_DeleteProject_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresProjectStore{DB: fake}

	err := store.DeleteProject(context.Background(), "t1", "proj-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectStore_DeleteProject_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresProjectStore{DB: fake}

	err := store.DeleteProject(context.Background(), "t1", "proj-nonexistent")
	if err == nil {
		t.Fatal("expected error for not found")
	}
	if !strings.Contains(err.Error(), "project not found") {
		t.Errorf("expected 'project not found', got: %v", err)
	}
}

func TestProjectStore_GetByName_NotFoundReturnsNil(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresProjectStore{DB: rec}

	result, err := store.GetByName(context.Background(), "t1", "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil for not found")
	}
}

func TestProjectStore_ListByTenant_EmptyReturnsEmptySlice(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresProjectStore{DB: rec}

	results, err := store.ListByTenant(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results == nil {
		t.Error("expected non-nil slice")
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got: %d", len(results))
	}
}

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
// Tenant Store tests
// =============================================================================

func TestTenantStore_ListTenantIDsByPattern_Success(t *testing.T) {
	fake := &fakeDBTX{
		queryRows: &fakeRowsMulti{
			data: [][]any{
				{"tn-lt-l3-001"},
				{"tn-lt-l3-002"},
			},
		},
	}
	store := &PostgresTenantStore{DB: fake}

	ids, err := store.ListTenantIDsByPattern(context.Background(), "tn-lt-l3-%")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 IDs, got: %d", len(ids))
	}
	if ids[0] != "tn-lt-l3-001" {
		t.Errorf("expected tn-lt-l3-001, got: %s", ids[0])
	}
}

func TestTenantStore_ListTenantIDsByPattern_Empty(t *testing.T) {
	fake := &fakeDBTX{
		queryRows: &fakeRowsMulti{data: [][]any{}},
	}
	store := &PostgresTenantStore{DB: fake}

	ids, err := store.ListTenantIDsByPattern(context.Background(), "no-match-%")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0, got: %d", len(ids))
	}
}

func TestTenantStore_DeleteTenantsFKOrdered_EmptyNoOp(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresTenantStore{DB: fake}

	counts, err := store.DeleteTenantsFKOrdered(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if counts.Tenants != 0 || counts.Builds != 0 {
		t.Errorf("expected zero counts for empty input, got: %+v", counts)
	}
	if fake.execCalls != 0 {
		t.Errorf("expected 0 exec calls for empty input, got: %d", fake.execCalls)
	}
}

func TestTenantStore_DeleteTenantsFKOrdered_ExecutesAllSteps(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresTenantStore{DB: fake}

	counts, err := store.DeleteTenantsFKOrdered(context.Background(), []string{"tn-1", "tn-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should execute 8 steps
	if fake.execCalls != 8 {
		t.Errorf("expected 8 exec calls, got: %d", fake.execCalls)
	}
	// Each step returns RowsAffected=1
	if counts.Tenants != 1 {
		t.Errorf("expected tenants=1, got: %d", counts.Tenants)
	}
}

func TestTenantStore_DeleteTenantsFKOrdered_ErrorStopsExecution(t *testing.T) {
	callCount := 0
	fake := &fakeDBTX{
		execResult: ExecResult{RowsAffected: 1},
	}
	// Override Exec to fail on step 3
	originalExec := fake.execErr
	_ = originalExec
	store := &PostgresTenantStore{DB: &failOnNthExecDBTX{
		base:    fake,
		failAt:  3,
		failErr: fmt.Errorf("FK violation"),
		counter: &callCount,
	}}

	_, err := store.DeleteTenantsFKOrdered(context.Background(), []string{"tn-1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "step 3") {
		t.Errorf("expected error at step 3, got: %v", err)
	}
}

// failOnNthExecDBTX wraps a DBTX and fails on the Nth Exec call.
type failOnNthExecDBTX struct {
	base    *fakeDBTX
	failAt  int
	failErr error
	counter *int
}

func (f *failOnNthExecDBTX) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	return f.base.Query(ctx, sql, args...)
}

func (f *failOnNthExecDBTX) Exec(ctx context.Context, sql string, args ...any) (ExecResult, error) {
	*f.counter++
	if *f.counter == f.failAt {
		return ExecResult{}, f.failErr
	}
	return f.base.Exec(ctx, sql, args...)
}

func (f *failOnNthExecDBTX) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return f.base.QueryRow(ctx, sql, args...)
}

func TestTruncateSQL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"short", "short"},
		{"exactly 40 chars exactly 40 chars exact", "exactly 40 chars exactly 40 chars exact"},
		{"this string is way longer than 40 characters so it should be truncated", "this string is way longer than 40 charac..."},
	}
	for _, tt := range tests {
		result := truncateSQL(tt.input)
		if result != tt.expected {
			t.Errorf("truncateSQL(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// =============================================================================
// Team Store tests
// =============================================================================

func TestTeamStore_AddMember_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresTeamStore{DB: fake}

	err := store.AddMember(context.Background(), "t1", "github-user1", "member")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTeamStore_RemoveMember_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresTeamStore{DB: fake}

	err := store.RemoveMember(context.Background(), "t1", "github-user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTeamStore_UpdateMemberRole_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresTeamStore{DB: fake}

	err := store.UpdateMemberRole(context.Background(), "t1", "github-user1", "admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTeamStore_UpdateMemberRole_NotFound(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := &PostgresTeamStore{DB: fake}

	err := store.UpdateMemberRole(context.Background(), "t1", "github-nonexistent", "admin")
	if !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("expected ErrMemberNotFound, got: %v", err)
	}
}

func TestTeamStore_ListMembers_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresTeamStore{DB: rec}

	members, err := store.ListMembers(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if members == nil {
		t.Error("expected non-nil slice")
	}
	if len(members) != 0 {
		t.Errorf("expected 0, got: %d", len(members))
	}
}

func TestTeamStore_GetMember_NotFound(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresTeamStore{DB: fake}

	member, err := store.GetMember(context.Background(), "t1", "github-nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if member != nil {
		t.Error("expected nil for not found")
	}
}

func TestTeamStore_GetUserByUsername_NotFound(t *testing.T) {
	fake := &fakeDBTX{}
	store := &PostgresTeamStore{DB: fake}

	user, err := store.GetUserByUsername(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != nil {
		t.Error("expected nil for not found")
	}
}

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
// Environment Target Store tests
// =============================================================================

// TestEnvironmentTargetStore_Mutators exercises every single-row mutation
// method on EnvironmentTargetStore (SetDesired, SetActive, DeleteTarget)
// with the canonical {Success, NilTxUsesFallback} pair. Phase 3 helper:
// was 6 near-clone test functions across stores_test.go and stores_extra_test.go.
func TestEnvironmentTargetStore_Mutators(t *testing.T) {
	type mutatorCall func(s EnvironmentTargetStore, tx DBTX) error
	cases := []struct {
		name string
		call mutatorCall // canonical happy path; reused for nil-tx variant
	}{
		{
			name: "SetDesired",
			call: func(s EnvironmentTargetStore, tx DBTX) error {
				return s.SetDesired(context.Background(), tx, "t1", "p1", "prod", "rel-1", "user1", "corr-1")
			},
		},
		{
			name: "SetActive",
			call: func(s EnvironmentTargetStore, tx DBTX) error {
				return s.SetActive(context.Background(), tx, "t1", "p1", "prod", "rel-1")
			},
		},
		{
			name: "DeleteTarget",
			call: func(s EnvironmentTargetStore, tx DBTX) error {
				return s.DeleteTarget(context.Background(), tx, "t1", "p1", "prod")
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name+"_Success", func(t *testing.T) {
			fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
			store := NewPostgresEnvironmentTargetStore(fake, fake)
			if err := c.call(store, fake); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		t.Run(c.name+"_NilTxUsesFallback", func(t *testing.T) {
			fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
			store := NewPostgresEnvironmentTargetStore(fake, fake)
			if err := c.call(store, nil); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestEnvironmentTargetStore_Get_NotFoundReturnsNil(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresEnvironmentTargetStore(rec, rec)

	target, err := store.Get(context.Background(), "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != nil {
		t.Error("expected nil for not found")
	}
}

func TestEnvironmentTargetStore_ListByProject_EmptyReturnsEmptySlice(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresEnvironmentTargetStore(rec, rec)

	targets, err := store.ListByProject(context.Background(), "t1", "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targets == nil {
		t.Error("expected non-nil slice")
	}
	if len(targets) != 0 {
		t.Errorf("expected 0, got: %d", len(targets))
	}
}

// SetDesired_NilTxUsesFallback / SetActive_Success / DeleteTarget_{Success,
// NilTxUsesFallback} consolidated into TestEnvironmentTargetStore_Mutators
// above (Phase 3 v12 refactor).

// =============================================================================
// Idempotency Store tests
// =============================================================================

func TestIdempotencyStore_Get_NotFoundReturnsNil(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresIdempotencyStore{DB: rec}

	rec1, err := store.Get(context.Background(), "key-1", "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec1 != nil {
		t.Error("expected nil for not found")
	}
}

func TestIdempotencyStore_Get_Success(t *testing.T) {
	fake := &fakeDBTX{
		queryRows: &fakeRowsMulti{
			data: [][]any{
				{"key-1", "t1", "/api/v1/builds", "hash123", int(202), []byte(`{"id":"bld-1"}`)},
			},
		},
	}
	store := &PostgresIdempotencyStore{DB: fake}

	rec, err := store.Get(context.Background(), "key-1", "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil record")
	}
	if rec.Key != "key-1" {
		t.Errorf("expected key-1, got: %s", rec.Key)
	}
	if rec.ResponseStatus != 202 {
		t.Errorf("expected 202, got: %d", rec.ResponseStatus)
	}
}

func TestIdempotencyStore_Store_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := &PostgresIdempotencyStore{DB: fake}

	err := store.Store(context.Background(), &middleware.IdempotencyRecord{
		Key:            "key-1",
		TenantID:       "t1",
		Endpoint:       "/api/v1/builds",
		RequestHash:    "hash123",
		ResponseStatus: 202,
		ResponseBody:   []byte(`{"id":"bld-1"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIdempotencyStore_Cleanup_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 42}}
	store := &PostgresIdempotencyStore{DB: fake}

	count, err := store.Cleanup(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 42 {
		t.Errorf("expected 42, got: %d", count)
	}
}

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

func TestAuditStore_ListAuditEntries_EmptyResult(t *testing.T) {
	// Need a fake that returns 0 from count query, then empty rows
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{values: []any{int(0)}},
		queryRows:      &fakeRowsMulti{data: [][]any{}},
	}
	store := &PostgresAuditStore{DB: fake}

	entries, total, err := store.ListAuditEntries(context.Background(), "t1", domain.AuditFilter{
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0, got: %d", total)
	}
	if entries == nil {
		t.Error("expected non-nil slice")
	}
}

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

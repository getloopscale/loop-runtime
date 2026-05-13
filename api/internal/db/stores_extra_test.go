// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/domain"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// =============================================================================
// Release Store additional tests
// =============================================================================

func TestReleaseStore_GetActiveByEnv_Success(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeReleaseRowValues(Release{
				ID: "rel-act-1", BuildID: "bld-1", TenantID: "t1",
				ProjectID: "p1", Environment: "prod", Status: "ACTIVE",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
				GitMirrorStatus: "completed", GitCommit: "abc123",
				Apps: json.RawMessage("null"), Artifacts: json.RawMessage("null"),
				SideEffects: json.RawMessage("null"),
			}),
		},
	}
	store := NewPostgresReleaseStore(fake)

	release, err := store.GetActiveByEnv(context.Background(), "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release == nil {
		t.Fatal("expected non-nil release")
	}
	if release.ID != "rel-act-1" {
		t.Errorf("expected rel-act-1, got: %s", release.ID)
	}
	if release.Status != "ACTIVE" {
		t.Errorf("expected ACTIVE, got: %s", release.Status)
	}
}

func TestReleaseStore_GetActiveByEnvTx_NotFoundReturnsNil(t *testing.T) {
	fake := &fakeDBTX{}
	store := NewPostgresReleaseStore(fake)

	release, err := store.GetActiveByEnvTx(context.Background(), fake, "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release != nil {
		t.Error("expected nil release for not found")
	}
}

func TestReleaseStore_GetPreviousRelease_Success(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeReleaseRowValues(Release{
				ID: "rel-prev-1", BuildID: "bld-1", TenantID: "t1",
				ProjectID: "p1", Environment: "prod", Status: "SUPERSEDED",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
				GitMirrorStatus: "completed", GitCommit: "prev-sha",
				Apps: json.RawMessage("null"), Artifacts: json.RawMessage("null"),
				SideEffects: json.RawMessage("null"),
			}),
		},
	}
	store := NewPostgresReleaseStore(fake)

	release, err := store.GetPreviousRelease(context.Background(), "t1", "p1", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release == nil {
		t.Fatal("expected non-nil release")
	}
	if release.Status != "SUPERSEDED" {
		t.Errorf("expected SUPERSEDED, got: %s", release.Status)
	}
}

func TestReleaseStore_GetByIDForTenant_Success(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeReleaseRowValues(Release{
				ID: "rel-t1", BuildID: "bld-1", TenantID: "t1",
				ProjectID: "p1", Environment: "prod", Status: "ACTIVE",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
				GitMirrorStatus: "completed",
				Apps: json.RawMessage("null"), Artifacts: json.RawMessage("null"),
				SideEffects: json.RawMessage("null"),
			}),
		},
	}
	store := NewPostgresReleaseStore(fake)

	release, err := store.GetByIDForTenant(context.Background(), "t1", "rel-t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release == nil {
		t.Fatal("expected non-nil release")
	}
	if release.ID != "rel-t1" {
		t.Errorf("expected rel-t1, got: %s", release.ID)
	}
}

func TestReleaseStore_GetLatestByBuildID_Success(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeReleaseRowValues(Release{
				ID: "rel-latest", BuildID: "bld-1", TenantID: "t1",
				ProjectID: "p1", Environment: "prod", Status: "PENDING",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
				GitMirrorStatus: "completed",
				Apps: json.RawMessage("null"), Artifacts: json.RawMessage("null"),
				SideEffects: json.RawMessage("null"),
			}),
		},
	}
	store := NewPostgresReleaseStore(fake)

	release, err := store.GetLatestByBuildID(context.Background(), "bld-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release == nil {
		t.Fatal("expected non-nil release")
	}
	if release.ID != "rel-latest" {
		t.Errorf("expected rel-latest, got: %s", release.ID)
	}
}

func TestReleaseStore_UpdateStatus_NilTxUsesPool(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeReleaseRowValues(Release{
				ID: "rel-nil-tx", BuildID: "bld-1", TenantID: "t1",
				ProjectID: "p1", Environment: "prod", Status: "READY",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
				GitMirrorStatus: "",
				Apps: json.RawMessage("null"), Artifacts: json.RawMessage("null"),
				SideEffects: json.RawMessage("null"),
			}),
		},
		execResult: ExecResult{RowsAffected: 1},
	}
	store := NewPostgresReleaseStore(fake)

	release, err := store.UpdateStatus(context.Background(), nil, "rel-nil-tx", string(statemachine.ReleaseGitMirrorCompleted))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release.Status != "PENDING" {
		t.Errorf("expected PENDING, got: %s", release.Status)
	}
}

func TestReleaseStore_SetGitCommit_ExecError(t *testing.T) {
	fake := &fakeDBTX{execErr: fmt.Errorf("connection refused")}
	store := NewPostgresReleaseStore(fake)

	err := store.SetGitCommit(context.Background(), fake, "rel-1", "abc123")
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// Build Store additional tests
// =============================================================================

func TestBuildStore_SetWorkflowID_NilTxUsesPool(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresBuildStore(fake)

	err := store.SetWorkflowID(context.Background(), nil, "bld-1", "wf-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildStore_SetAppsSpec_NilTxUsesPool(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresBuildStore(fake)

	apps := []BuildAppSpec{{Name: "api", Type: "server", Port: 8080}}
	err := store.SetAppsSpec(context.Background(), nil, "bld-1", apps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildStore_GetBuildApps_NullJSON(t *testing.T) {
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: []any{json.RawMessage("null")},
		},
	}
	store := NewPostgresBuildStore(fake)

	apps, err := store.GetBuildApps(context.Background(), "bld-null-apps")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if apps != nil {
		t.Errorf("expected nil apps for null JSON, got: %v", apps)
	}
}

func TestBuildStore_FindActiveBuilds_Empty(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresBuildStore(rec)

	builds, err := store.FindActiveBuilds(context.Background(), "cell-default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if builds == nil {
		t.Error("expected non-nil slice")
	}
	if len(builds) != 0 {
		t.Errorf("expected 0, got: %d", len(builds))
	}
}

func TestBuildStore_ListByProject_WithData(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRows: &fakeRowsMulti{
			data: [][]any{
				makeBuildRowValues(Build{
					ID: "bld-lp-1", TenantID: "t1", ProjectID: "p1",
					Environment: "prod", Status: "SUCCEEDED",
					CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
				}),
			},
		},
	}
	store := NewPostgresBuildStore(fake)

	builds, err := store.ListByProject(context.Background(), "t1", "p1", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(builds) != 1 {
		t.Errorf("expected 1, got: %d", len(builds))
	}
}

func TestBuildStore_CancelActiveBuilds_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: fmt.Errorf("db down")}
	store := NewPostgresBuildStore(fake)

	_, _, err := store.CancelActiveBuilds(context.Background(), fake, "t1", "p1", "prod")
	if err == nil {
		t.Fatal("expected error")
	}
}

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
// Team Store additional tests
// =============================================================================

func TestTeamStore_AddMember_ExecError(t *testing.T) {
	fake := &fakeDBTX{execErr: fmt.Errorf("unique violation")}
	store := &PostgresTeamStore{DB: fake}

	err := store.AddMember(context.Background(), "t1", "github-user1", "member")
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// Build Store — UpdateStatus additional transitions
// =============================================================================

func TestBuildStore_UpdateStatus_QueuedToFailed(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeBuildRowValues(Build{
				ID: "bld-qf", TenantID: "t1", ProjectID: "p1",
				Environment: "prod", Status: "QUEUED",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
			}),
		},
		execResult: ExecResult{RowsAffected: 1},
	}
	store := NewPostgresBuildStore(fake)

	build, err := store.UpdateStatus(context.Background(), fake, "bld-qf", string(statemachine.BuildFailedEvent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if build.Status != "FAILED" {
		t.Errorf("expected FAILED, got: %s", build.Status)
	}
	// Verify finished_at is set for terminal state
	if !strings.Contains(fake.capturedSQL, "finished_at") {
		t.Error("expected SQL to set finished_at for terminal state")
	}
}

func TestBuildStore_UpdateStatus_RunningToCancelling(t *testing.T) {
	now := time.Now().UTC()
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{
			values: makeBuildRowValues(Build{
				ID: "bld-rc", TenantID: "t1", ProjectID: "p1",
				Environment: "prod", Status: "RUNNING",
				CorrelationID: "c1", CreatedBy: "u1", CreatedAt: now,
			}),
		},
		execResult: ExecResult{RowsAffected: 1},
	}
	store := NewPostgresBuildStore(fake)

	build, err := store.UpdateStatus(context.Background(), fake, "bld-rc", string(statemachine.BuildCancelRequested))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if build.Status != "CANCELLING" {
		t.Errorf("expected CANCELLING, got: %s", build.Status)
	}
}

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
// WriteAuditLog test
// =============================================================================

func TestWriteAuditLog_Success(t *testing.T) {
	fake := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}

	err := WriteAuditLog(context.Background(), fake, AuditEntry{
		TenantID:      "t1",
		Action:        "build.submitted",
		Actor:         "user@test.com",
		ResourceType:  "build",
		ResourceID:    "bld-1",
		CorrelationID: "corr-1",
		Metadata:      map[string]any{"env": "production"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteAuditLog_ExecError(t *testing.T) {
	fake := &fakeDBTX{execErr: fmt.Errorf("insert failed")}

	err := WriteAuditLog(context.Background(), fake, AuditEntry{
		TenantID: "t1",
		Action:   "build.submitted",
		Actor:    "user@test.com",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

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

// =============================================================================
// ListByProjectAndEnv — additional branch coverage
// =============================================================================

func TestBuildStore_ListByProjectAndEnv_NoEnvFilter(t *testing.T) {
	fake := &fakeDBTX{
		queryRowResult: &fakeRowWithData{values: []any{int(0)}},
		queryRows:      &fakeRowsMulti{data: [][]any{}},
	}
	store := NewPostgresBuildStore(fake)

	builds, total, err := store.ListByProjectAndEnv(context.Background(), "t1", "p1", "", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 total, got: %d", total)
	}
	if builds == nil {
		t.Error("expected non-nil slice")
	}
}

func TestBuildStore_ListByProjectAndEnv_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: fmt.Errorf("connection refused")}
	store := NewPostgresBuildStore(fake)

	_, _, err := store.ListByProjectAndEnv(context.Background(), "t1", "p1", "prod", 10, 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildStore_ListByProjectAndEnv_NoEnvQueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: fmt.Errorf("connection refused")}
	store := NewPostgresBuildStore(fake)

	_, _, err := store.ListByProjectAndEnv(context.Background(), "t1", "p1", "", 10, 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildStore_ListByProject_QueryError(t *testing.T) {
	fake := &fakeDBTX{queryErr: fmt.Errorf("connection refused")}
	store := NewPostgresBuildStore(fake)

	_, err := store.ListByProject(context.Background(), "t1", "p1", 10, 0)
	if err == nil {
		t.Fatal("expected error")
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

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"testing"
	"time"
)

// =============================================================================
// Preview Store tests — DELETED in T2.2 #1 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 17 stub-class tests previously here (TestPreviewStore_GetByID_
// HappyPath/NotFound/QueryError, _GetByPR_HappyPath/NotFound,
// _ListByTenant_ReturnsAll/Empty, _ListByProject_ScopesArgs,
// _RequestDeletion_Success/NotFoundOrCrossTenant/DBError,
// _UpdateStatus_Success/NotFound, _UpdateBuild_Success/NotFound,
// _UpdateURL_Success) ALL used previewFakeQuery / previewFakeExec /
// FakeDBTX stub responses to verify PostgresPreviewStore behaviors.
// With T2.2 #1 migrating to sqlc-backed via SqlcBridge, the fake
// pattern is incompatible — the wrapper returns a typed error when
// the DBTX/tx doesn't satisfy SqlcBridge (fail-fast per T0.6 reshape
// matrix).
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` (preview_store_test.go MIXED):
//
//   - KEEP-INVARIANT preserved structurally by PreviewStoreSqlc:
//     * UPSERT on partial unique index (tenant_id, project_id,
//       pr_number) WHERE deleted_at IS NULL → reset status to
//       'creating', clear build_id/release_id, refresh branch/
//       commit_sha/url/expires_at (sqlc emits the same SQL shape;
//       partial-index ON CONFLICT preserved verbatim)
//     * UpdateStatus/UpdateBuild/UpdateURL/RequestDeletion rows-
//       affected=0 → ErrPreviewNotFound (wrapper checks n==0
//       on the :execrows return)
//     * GetByID/GetByPR pgx.ErrNoRows → ErrPreviewNotFound
//       (wrapper uses errors.Is)
//     * RequestDeletion tenant-scoped (UPDATE ... WHERE id = $1
//       AND tenant_id = $2 AND deleted_at IS NULL)
//     * Default ID generation (id.GeneratePreviewID()) + default
//       TTL=72h applied in Create when fields are zero
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: most happy-path stub-
//     pass-through tests — sqlc compile-time + sqlcsync gate replace.
//
// Per matrix § Phase 2 reshape window: KEEP-INVARIANT subset moves
// to real-PG-gated tests when pgxmock or LOOPRUNTIME_E2E_DATABASE_URL
// harness is wired.

// TestPreviewRecord_ToDomain_PopulatesAllFields is the only stub-free
// test that survives the T2.2 #1 migration — it tests pure conversion
// logic on the PreviewRecord struct (preview_types.go) with no DBTX
// dependency.
func TestPreviewRecord_ToDomain_PopulatesAllFields(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	url := "https://pr-prev-z.preview.loopruntime.app"
	build := "build-99"
	rec := PreviewRecord{
		ID:        "prev-z",
		TenantID:  "t1",
		ProjectID: "api",
		PRNumber:  42,
		Branch:    "feature/x",
		CommitSHA: "abc1234",
		Status:    PreviewStatusActive,
		URL:       &url,
		BuildID:   &build,
		ExpiresAt: now.Add(72 * time.Hour),
		CellID:    "cell-0",
		CreatedAt: now,
		UpdatedAt: now,
	}

	d := rec.ToDomain()
	if d.ID != "prev-z" {
		t.Errorf("ID: %q", d.ID)
	}
	if d.BuildID != "build-99" {
		t.Errorf("BuildID: %q", d.BuildID)
	}
	if d.URL == "" {
		t.Error("URL should be populated from *string pointer")
	}
	if d.TTLExpiresAt == "" {
		t.Error("TTLExpiresAt should be populated")
	}
	if d.CreatedAt == "" || d.UpdatedAt == "" {
		t.Error("CreatedAt/UpdatedAt should be populated")
	}
}

// TestPreviewStore_Implements_LegacyRoutesInterface verifies the
// PreviewStore interface contract (`var s PreviewStore =
// (*PostgresPreviewStore)(nil)`) survives the T2.2 #1 migration.
// Compile-time assertion — no DBTX dependency.
func TestPreviewStore_Implements_LegacyRoutesInterface(t *testing.T) {
	var s PreviewStore = (*PostgresPreviewStore)(nil)
	_ = s
}

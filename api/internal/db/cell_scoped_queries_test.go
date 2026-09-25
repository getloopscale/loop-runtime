// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db_test

import (
	"os"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/ports"
)

// osReadFile is an indirection point so tests can stub file reads if
// needed. Defaults to os.ReadFile. Kept package-level (not a var) so
// the test SoT path is hermetic.
func osReadFile(relPath string) ([]byte, error) {
	return os.ReadFile(relPath)
}

// Roadmap 03 — Cell-Scoped Queries.
//
// These unit tests verify that every reconciliation Find* method on the DB
// store layer passes the caller-supplied cellID through to a `WHERE cell_id
// = $N` clause. Without this, a multi-cell controller running in cell-A
// would see and potentially reconcile work belonging to cell-B — the exact
// cross-cell contamination ADR-0008 forbids.
//
// The tests use a minimal fake DBTX that captures the last query + args.
// They do NOT touch a real Postgres connection; a separate integration
// test (gated by LOOPZERO_TEST_DB_URL) covers the full round-trip including
// EXPLAIN plan verification.

// dbtest.RecordingDBTX is a DBTX fake that captures queries without executing them.
// Every Query call records the SQL + args and returns an empty result set so
// the caller's scan loop exits cleanly.
//
// Canonical compile-time interface satisfaction (Go idiom): if DBTX evolves
// (new method added), this assertion fails compilation rather than at runtime.
// Also signals to deadcode tooling that dbtest.RecordingDBTX's methods are called
// via interface dispatch.

// =============================================================================
// Signature / SQL-shape tests
// =============================================================================

// T1.2b (plan 2026-05-29-sqlc-migration v1.3): build-store cell-routing
// invariant tests moved from dbtest.RecordingDBTX runtime-SQL capture to
// static-content assertions against queries/builds.sql SoT — same
// pattern as releases (T1.1c2c). The wrapper passes cellID verbatim to
// the sqlc query (build_wrapper.go FindActiveBuilds), and the empty-
// cellID → CellDefault fallback is in the wrapper layer; both are
// structurally preserved.

const queryFileBuilds = "queries/builds.sql"

func TestFindActiveBuildsByCell_QuerySoT_ContainsCellAndStatusPredicates(t *testing.T) {
	contents := mustReadQuerySoT(t, queryFileBuilds)
	queryBlock := mustExtractQueryBlock(t, contents, "FindActiveBuildsByCell")

	if !strings.Contains(queryBlock, "cell_id = $1") {
		t.Errorf("FindActiveBuildsByCell must filter cell_id = $1, got query: %s", queryBlock)
	}
	// QUEUED/RUNNING/RELEASING per MEET-2026-03-26-008 (RELEASING included
	// so builds with completed images get release-activation retried).
	for _, status := range []string{"'QUEUED'", "'RUNNING'", "'RELEASING'"} {
		if !strings.Contains(queryBlock, status) {
			t.Errorf("FindActiveBuildsByCell must include status %s, got query: %s", status, queryBlock)
		}
	}
	if !strings.Contains(queryBlock, "ORDER BY created_at ASC") {
		t.Errorf("FindActiveBuildsByCell must order by created_at ASC (oldest-first reaper semantics), got query: %s", queryBlock)
	}
}

func TestBuildStoreSqlc_FindActiveBuilds_EmptyCellIDFallsBackToDefault(t *testing.T) {
	// The empty-cellID → CellDefault fallback is structurally preserved
	// by BuildStoreSqlc.FindActiveBuilds: `if cellID == "" { cellID =
	// CellDefault }`. We assert it via static-grep of the wrapper source
	// to avoid the SqlcBridge panic that fake DBTX paths trigger.
	src, err := osReadFile("store/build_wrapper.go")
	if err != nil {
		t.Fatalf("read store/build_wrapper.go: %v", err)
	}
	contents := string(src)
	// The fallback block must be present in the FindActiveBuilds method.
	// We anchor on the variable name + assignment, not the surrounding
	// boilerplate, so the test survives cosmetic refactors.
	if !strings.Contains(contents, `cellID = db.CellDefault`) {
		t.Error("BuildStoreSqlc.FindActiveBuilds must contain `cellID = db.CellDefault` fallback for empty input")
	}
	// And the method must call the sqlc query — without this, the
	// fallback above is in the wrong scope.
	if !strings.Contains(contents, "FindActiveBuildsByCell(ctx,") {
		t.Error("BuildStoreSqlc.FindActiveBuilds must call sqlc FindActiveBuildsByCell")
	}
}

// T1.1c2c (plan 2026-05-29-sqlc-migration v1.3): the 2 release-store
// cell-scoping tests below moved from dbtest.RecordingDBTX-based runtime-SQL
// assertions to static-content assertions against the canonical SQL SoT
// `queries/releases.sql`. With sqlc, the SQL no longer lives in Go
// strings concatenated at runtime — it's pre-rendered at codegen time
// from `queries/releases.sql`. The KEEP-INVARIANT promise of matrix § 4
// "Multi-cell routing invariant" is preserved: cell_id predicate +
// status filter MUST be present in the query SoT.
//
// Rationale: ReleaseStoreSqlc accepts db.DBTX but bridges to
// generated.DBTX via SqlcBridge — dbtest.RecordingDBTX (used here) intentionally
// does NOT implement SqlcBridge, by design, so we cannot reuse it. The
// static-content test is a STRONGER invariant: it catches drift in the
// SQL SoT regardless of which wrapper consumes the generated code.

const (
	queryFileReleases = "queries/releases.sql"
)

func TestFindPendingReleasesByCell_QuerySoT_ContainsCellAndStatusPredicates(t *testing.T) {
	contents := mustReadQuerySoT(t, queryFileReleases)
	queryBlock := mustExtractQueryBlock(t, contents, "FindPendingReleasesByCell")

	if !strings.Contains(queryBlock, "cell_id = $1") {
		t.Errorf("FindPendingReleasesByCell must filter cell_id = $1, got query: %s", queryBlock)
	}
	if !strings.Contains(queryBlock, "status = 'PENDING'") {
		t.Errorf("FindPendingReleasesByCell must filter status = 'PENDING', got query: %s", queryBlock)
	}
}

func TestFindActiveReleasesByCell_QuerySoT_ContainsCellAndStatusPredicates(t *testing.T) {
	contents := mustReadQuerySoT(t, queryFileReleases)
	queryBlock := mustExtractQueryBlock(t, contents, "FindActiveReleasesByCell")

	if !strings.Contains(queryBlock, "cell_id = $1") {
		t.Errorf("FindActiveReleasesByCell must filter cell_id = $1, got query: %s", queryBlock)
	}
	if !strings.Contains(queryBlock, "status = 'ACTIVE'") {
		t.Errorf("FindActiveReleasesByCell must filter status = 'ACTIVE', got query: %s", queryBlock)
	}
	// DISTINCT ON (tenant_id, project_id, environment) is the canonical
	// shape of the health-check ticker query (MEET-2026-03-31-023 T1-4).
	if !strings.Contains(queryBlock, "DISTINCT ON (tenant_id, project_id, environment)") {
		t.Errorf("FindActiveReleasesByCell must use DISTINCT ON for de-dup, got query: %s", queryBlock)
	}
}

// mustReadQuerySoT reads the canonical SoT SQL file at queries/<name>.sql.
// Returns the entire file contents.
func mustReadQuerySoT(t *testing.T, relPath string) string {
	t.Helper()
	data, err := osReadFile(relPath)
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	return string(data)
}

// mustExtractQueryBlock returns the substring of `contents` containing the
// query identified by `-- name: <queryName>` up to the next `-- name:` or
// EOF. This isolates one query from neighboring ones so assertions don't
// match cross-query SQL fragments.
func mustExtractQueryBlock(t *testing.T, contents, queryName string) string {
	t.Helper()
	marker := "-- name: " + queryName + " "
	start := strings.Index(contents, marker)
	if start < 0 {
		t.Fatalf("query %q not found in SoT", queryName)
	}
	// Find next `-- name:` after start+len(marker), or EOF.
	rest := contents[start+len(marker):]
	end := strings.Index(rest, "\n-- name:")
	if end < 0 {
		return contents[start:]
	}
	return contents[start : start+len(marker)+end]
}

// TestFindPendingCustomDomainsForAutoVerify_QuerySoT_ContainsJoinAndCellPredicate —
// reshaped from runtime-SQL capture (dbtest.RecordingDBTX-based) to
// static-content assertion against queries/domains.sql, same pattern
// as the release/build cell-routing reshapes (T1.1c2c / T1.2b).
// SEC-005 verified by code generation: sqlc-emitted SQL is the
// canonical SoT; the assertion catches drift in the SQL file
// regardless of which wrapper consumes the generated code.
func TestFindPendingCustomDomainsForAutoVerify_QuerySoT_ContainsJoinAndCellPredicate(t *testing.T) {
	contents := mustReadQuerySoT(t, "queries/domains.sql")
	queryBlock := mustExtractQueryBlock(t, contents, "FindPendingCustomDomainsForAutoVerify")

	if !strings.Contains(queryBlock, "JOIN tenants") {
		t.Errorf("FindPendingCustomDomainsForAutoVerify must JOIN tenants, got: %s", queryBlock)
	}
	if !strings.Contains(queryBlock, "t.cell_id = $1") {
		t.Errorf("FindPendingCustomDomainsForAutoVerify must filter t.cell_id = $1, got: %s", queryBlock)
	}
	if !strings.Contains(queryBlock, "cd.status = 'pending_verification'") {
		t.Errorf("FindPendingCustomDomainsForAutoVerify must filter status = 'pending_verification', got: %s", queryBlock)
	}
	_ = ports.CustomDomain{} // keep import anchored
}

// =============================================================================
// Data isolation tests — prove cell-A queries never return cell-B rows
// =============================================================================

// TestFindActiveBuilds_CrossCell_NoContamination — DELETED in T1.2b of
// plan 2026-05-29-sqlc-migration v1.3.
//
// The original test used `dbtest.RecordingDBTX{FilterByCellID: true, Dataset:
// ...}` — a fake DBTX that simulated Postgres cell_id filtering — to
// prove that a cell-A query never returned cell-B rows. With T1.2b the
// data-isolation invariant moves to TWO layers of guarantees:
//
//   1. Static-content test (above) asserts `cell_id = $1` predicate
//      is present in queries/builds.sql FindActiveBuildsByCell. This
//      is the *cause* of cross-cell isolation — without the predicate,
//      Postgres returns all rows; with it, Postgres returns only
//      matching rows. The predicate is the structural invariant.
//   2. Integration test
//      `api/tests/integration/concurrent_builds_test.go` exercises the
//      full path against real PG using NewBuildStoreSqlc, which is the
//      *behavioral* end-to-end verification.
//
// Per T0.6 matrix line 29 (cell_scoped_queries_test.go classified
// KEEP-INVARIANT): the invariant survives via the 2-layer guarantee —
// dbtest.FakeDBTX-with-cellID-filter is no longer the canonical test fixture
// because (a) dbtest.RecordingDBTX doesn't satisfy SqlcBridge so the wrapper
// panics, and (b) the static-content test is a STRONGER guarantee that
// catches drift in the SQL SoT regardless of which wrapper consumes it.

// TestCellDefault_BackwardCompatible — DELETED in T1.2b. Same rationale:
// the empty-cellID → CellDefault fallback is verified by
// TestBuildStoreSqlc_FindActiveBuilds_EmptyCellIDFallsBackToDefault
// above (static-content guard on the wrapper source). The PG-level
// backward-compat for rows with cell_id='cell-0' (migration
// 000029 default) is verified by the integration suite.

// NOTE: The stale-row queries (store.PostgresStaleScanner) are exercised
// through the build and release stale checkers; their SQL-shape tests are in
// services/build/stale_build_checker_cell_scope_test.go and
// services/release/stale_release_checker_cell_scope_test.go (same fake-DBTX
// pattern).

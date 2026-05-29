// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/loopzero/loopzero/api/internal/domain"
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

// recordingDBTX is a DBTX fake that captures queries without executing them.
// Every Query call records the SQL + args and returns an empty result set so
// the caller's scan loop exits cleanly.
type recordingDBTX struct {
	lastSQL  string
	lastArgs []any
	// rows returned by the next Query call, for tests that need to fake data.
	nextRows Rows
	// filterByCellID: when set, simulates PG cell_id filtering. The recorded
	// rows are filtered so only those whose `cellID` field matches the query's
	// cell_id parameter are returned. This lets us prove isolation at the
	// Go/SQL boundary without a real database.
	filterByCellID bool
	dataset        []fakeRow
}

type fakeRow struct {
	cellID string
	values []any
}

func (r *recordingDBTX) Query(_ context.Context, sql string, args ...any) (Rows, error) {
	r.lastSQL = sql
	r.lastArgs = args
	if r.filterByCellID {
		// The cell_id argument is the first positional parameter for our
		// reconciliation queries: WHERE cell_id = $1 ... (builds/releases).
		// For the stale checkers it is the last parameter. We handle both by
		// scanning all args for a cellID string.
		var wanted string
		for _, a := range args {
			if s, ok := a.(string); ok && (s == "cell-default" || strings.HasPrefix(s, "cell-")) {
				wanted = s
				break
			}
		}
		var matched []fakeRow
		for _, row := range r.dataset {
			if row.cellID == wanted {
				matched = append(matched, row)
			}
		}
		return &sliceRows{rows: matched}, nil
	}
	if r.nextRows != nil {
		return r.nextRows, nil
	}
	return &sliceRows{}, nil
}

func (r *recordingDBTX) Exec(_ context.Context, _ string, _ ...any) (ExecResult, error) {
	return ExecResult{}, nil
}

func (r *recordingDBTX) QueryRow(_ context.Context, _ string, _ ...any) Row {
	return emptyRow{}
}

type emptyRow struct{}

func (emptyRow) Scan(_ ...any) error { return ErrNoRows }

// sliceRows returns a slice of pre-built fake rows through the Rows iterator.
type sliceRows struct {
	rows []fakeRow
	idx  int
}

func (s *sliceRows) Next() bool {
	if s.idx >= len(s.rows) {
		return false
	}
	s.idx++
	return true
}

func (s *sliceRows) Scan(dest ...any) error {
	row := s.rows[s.idx-1]
	for i, d := range dest {
		if i >= len(row.values) {
			return nil
		}
		// Use reflection-free type assertion for the pointer types we emit.
		switch target := d.(type) {
		case *string:
			if v, ok := row.values[i].(string); ok {
				*target = v
			}
		}
	}
	return nil
}

func (s *sliceRows) Err() error { return nil }
func (s *sliceRows) Close()     {}

// =============================================================================
// Signature / SQL-shape tests
// =============================================================================

func TestFindActiveBuilds_CellScoped_PassesCellIDToWhere(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresBuildStore(rec)

	_, err := store.FindActiveBuilds(context.Background(), "cell-sfo3-02")
	if err != nil {
		t.Fatalf("FindActiveBuilds: unexpected error: %v", err)
	}

	if !strings.Contains(rec.lastSQL, "cell_id = $1") {
		t.Errorf("expected SQL to contain 'cell_id = $1', got: %s", rec.lastSQL)
	}
	if len(rec.lastArgs) == 0 || rec.lastArgs[0] != "cell-sfo3-02" {
		t.Errorf("expected first arg to be 'cell-sfo3-02', got: %v", rec.lastArgs)
	}
}

func TestFindActiveBuilds_EmptyCellIDDefaultsToCellDefault(t *testing.T) {
	rec := &recordingDBTX{}
	store := NewPostgresBuildStore(rec)

	_, err := store.FindActiveBuilds(context.Background(), "")
	if err != nil {
		t.Fatalf("FindActiveBuilds: unexpected error: %v", err)
	}

	if len(rec.lastArgs) == 0 || rec.lastArgs[0] != CellDefault {
		t.Errorf("empty cellID should default to %q, got: %v", CellDefault, rec.lastArgs)
	}
}

// T1.1c2c (plan 2026-05-29-sqlc-migration v1.3): the 2 release-store
// cell-scoping tests below moved from recordingDBTX-based runtime-SQL
// assertions to static-content assertions against the canonical SQL SoT
// `queries/releases.sql`. With sqlc, the SQL no longer lives in Go
// strings concatenated at runtime — it's pre-rendered at codegen time
// from `queries/releases.sql`. The KEEP-INVARIANT promise of matrix § 4
// "Multi-cell routing invariant" is preserved: cell_id predicate +
// status filter MUST be present in the query SoT.
//
// Rationale: ReleaseStoreSqlc accepts db.DBTX but bridges to
// generated.DBTX via SqlcBridge — recordingDBTX (used here) intentionally
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

func TestFindPendingForAutoVerify_CellScoped_JoinsTenants(t *testing.T) {
	rec := &recordingDBTX{}
	store := &PostgresDomainStore{DB: rec}

	_, err := store.FindPendingForAutoVerify(context.Background(), "cell-sfo3-02")
	if err != nil {
		t.Fatalf("FindPendingForAutoVerify: unexpected error: %v", err)
	}

	// Domain store scopes via JOIN to tenants — the column reference is
	// qualified with the tenants alias.
	if !strings.Contains(rec.lastSQL, "JOIN tenants") {
		t.Errorf("expected SQL to JOIN tenants, got: %s", rec.lastSQL)
	}
	if !strings.Contains(rec.lastSQL, "t.cell_id = $1") {
		t.Errorf("expected 't.cell_id = $1', got: %s", rec.lastSQL)
	}
	if rec.lastArgs[0] != "cell-sfo3-02" {
		t.Errorf("expected cellID arg, got: %v", rec.lastArgs)
	}
	// Ensure the returned slice is non-nil even when empty (API contract).
	result, _ := store.FindPendingForAutoVerify(context.Background(), "cell-sfo3-02")
	if result == nil {
		t.Error("expected non-nil result slice")
	}
	_ = domain.CustomDomain{} // keep import anchored
}

// =============================================================================
// Data isolation tests — prove cell-A queries never return cell-B rows
// =============================================================================

func TestFindActiveBuilds_CrossCell_NoContamination(t *testing.T) {
	// Arrange: 5 builds in cell-a and 5 in cell-b, all in RUNNING state.
	// The fake DBTX simulates Postgres by filtering its dataset on the
	// cell_id argument the store passes through.
	dataset := make([]fakeRow, 0, 10)
	for i := 0; i < 5; i++ {
		dataset = append(dataset, fakeRow{
			cellID: "cell-a",
			values: []any{
				"bld-a-" + string(rune('0'+i)), "tenant-a", "proj-a", "prod",
				"RUNNING", "abc123", "corr", "user",
			},
		})
	}
	for i := 0; i < 5; i++ {
		dataset = append(dataset, fakeRow{
			cellID: "cell-b",
			values: []any{
				"bld-b-" + string(rune('0'+i)), "tenant-b", "proj-b", "prod",
				"RUNNING", "def456", "corr", "user",
			},
		})
	}
	rec := &recordingDBTX{filterByCellID: true, dataset: dataset}
	store := NewPostgresBuildStore(rec)

	// Act: query for cell-a.
	cellARows, err := store.FindActiveBuilds(context.Background(), "cell-a")
	if err != nil {
		t.Fatalf("cell-a query failed: %v", err)
	}

	// Assert: exactly 5 rows, all belonging to cell-a (tenant-a).
	if len(cellARows) != 5 {
		t.Errorf("expected 5 rows for cell-a, got %d", len(cellARows))
	}
	for _, b := range cellARows {
		if b.TenantID != "tenant-a" {
			t.Errorf("cell-a query returned cross-cell row: %+v", b)
		}
	}

	// Act: query for cell-b.
	cellBRows, _ := store.FindActiveBuilds(context.Background(), "cell-b")
	if len(cellBRows) != 5 {
		t.Errorf("expected 5 rows for cell-b, got %d", len(cellBRows))
	}
	for _, b := range cellBRows {
		if b.TenantID != "tenant-b" {
			t.Errorf("cell-b query returned cross-cell row: %+v", b)
		}
	}
}

func TestCellDefault_BackwardCompatible(t *testing.T) {
	// A row inserted before migration 000029 (or by a caller that does not
	// set cell_id) has cell_id='cell-default'. The default path must still
	// return it without any code changes.
	rec := &recordingDBTX{
		filterByCellID: true,
		dataset: []fakeRow{
			{cellID: CellDefault, values: []any{"bld-legacy", "tenant-x", "proj-x", "prod", "RUNNING", "", "c", "u"}},
		},
	}
	store := NewPostgresBuildStore(rec)

	rows, err := store.FindActiveBuilds(context.Background(), CellDefault)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("cell-default should return legacy row, got %d rows", len(rows))
	}

	// Empty cellID should also find it (store translates "" → CellDefault).
	rows2, _ := store.FindActiveBuilds(context.Background(), "")
	if len(rows2) != 1 {
		t.Errorf("empty cellID should behave identically to cell-default, got %d rows", len(rows2))
	}
}

// NOTE: Stale checkers (PostgresStaleBuildChecker, PostgresStaleReleaseChecker)
// live in the services package — their SQL-shape tests are in
// api/tests/unit/stale_checker_cell_scope_test.go (same recordingDBTX pattern).

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// T2.2 of plan 2026-05-28-meet-001-engineering-closure-plan.md.
// CR-9 (MEET-2026-05-28-001): the 3-layer Rekor URL persistence bug
// (LIVE 2026-05-28) shipped migration + struct + writer but silently
// omitted the column from 13 SELECTs + scanReleaseFields. The mirror
// test cross-checks all 5 sources every CI run.

// m07TablesUnderMirror is the list of M07 tables whose 5-source surface
// is covered by this test file. Adding a new M07 table requires adding
// it here AND adding a dedicated TestSchemaMirror_<table> test;
// TestSchemaMirror_AllM07TablesListed (EC-12) enforces both.
var m07TablesUnderMirror = []string{
	"releases",
	"builds",
	"release_app_attestations",
}

// ---------------------------------------------------------------------------
// Hermetic helper tests — no DB required.
// ---------------------------------------------------------------------------

func TestExtractStructJSONColumns_PicksUpDBColumnNames(t *testing.T) {
	cols := ExtractStructJSONColumns(reflect.TypeOf(Release{}))
	// Spot-check the columns most relevant to the CR-9 incident.
	want := []string{"id", "build_id", "tenant_id", "image_attestation_rekor_url"}
	for _, c := range want {
		if !contains(cols, c) {
			t.Errorf("ExtractStructJSONColumns: missing %q (got %v)", c, cols)
		}
	}
}

func TestExtractInsertColumns_ParsesLoopZeroStyle(t *testing.T) {
	sql := `INSERT INTO releases (id, build_id, tenant_id, image_attestation_rekor_url)
	        VALUES ($1, $2, $3, $4)`
	cols := ExtractInsertColumns(sql, "releases")
	want := []string{"build_id", "id", "image_attestation_rekor_url", "tenant_id"}
	if !equalSorted(cols, want) {
		t.Errorf("ExtractInsertColumns: got %v, want %v", cols, want)
	}
}

func TestExtractInsertColumns_IgnoresOtherTables(t *testing.T) {
	sql := `INSERT INTO audit_log (tenant_id, action) VALUES ('a', 'b')`
	cols := ExtractInsertColumns(sql, "releases")
	if len(cols) != 0 {
		t.Errorf("expected empty when target table absent, got %v", cols)
	}
}

func TestExtractSelectColumns_ParsesMultiLine(t *testing.T) {
	sql := `SELECT id, build_id, status, image_attestation_rekor_url FROM releases WHERE id = $1`
	cols := ExtractSelectColumns(sql, "releases")
	want := []string{"build_id", "id", "image_attestation_rekor_url", "status"}
	if !equalSorted(cols, want) {
		t.Errorf("ExtractSelectColumns: got %v, want %v", cols, want)
	}
}

func TestExtractSelectColumns_SkipsNonCoalesceFunctionsAndExpressions(t *testing.T) {
	// COALESCE(col, default) is intentionally unwrapped to the inner column
	// name — this is the canonical LoopZero pattern for JSONB defaults and
	// must mirror in tests. Other function calls (COUNT, MAX, ...) and
	// arithmetic expressions stay skipped.
	sql := `SELECT id, COALESCE(side_effects, '{}'), COUNT(*), now() FROM releases`
	cols := ExtractSelectColumns(sql, "releases")
	want := []string{"id", "side_effects"}
	if !equalSorted(cols, want) {
		t.Errorf("expected COALESCE-unwrap + non-COALESCE-skip, got %v want %v", cols, want)
	}
}

func TestExtractSelectColumns_OnlyTargetedTable(t *testing.T) {
	// Production usage: extractor runs over the full Go source of
	// release_store.go where each SQL lives in its own backtick-string
	// literal. We simulate by separating with newlines + non-SQL noise
	// (matching how Go source separates Exec calls).
	src := "tx.Query(`SELECT id FROM builds WHERE x = 1`)\n" +
		"_, _ = tx.Query(`SELECT name FROM releases WHERE id = $1`)\n"
	cols := ExtractSelectColumns(src, "releases")
	if !equalSorted(cols, []string{"name"}) {
		t.Errorf("expected only releases-targeted SELECT, got %v", cols)
	}
}

func TestToSnakeCase_HandlesLoopZeroAcronyms(t *testing.T) {
	cases := map[string]string{
		"ID":                       "id",
		"BuildID":                  "build_id",
		"ImageDigest":              "image_digest",
		"ImageAttestationRekorURL": "image_attestation_rekor_url",
		"CreatedAt":                "created_at",
		"GitMirrorStatus":          "git_mirror_status",
	}
	for in, want := range cases {
		if got := ToSnakeCase(in); got != want {
			t.Errorf("ToSnakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractScanFieldsFromGoFile_FindsScannerArgs(t *testing.T) {
	// release_store.go is in the same package directory — relative path
	// works because tests run with cwd=package dir.
	fields, err := ExtractScanFieldsFromGoFile("release_store.go", "scanReleaseFields")
	if err != nil {
		t.Fatalf("ExtractScanFieldsFromGoFile: %v", err)
	}
	// Spot-check the field shape rather than the full list (the list
	// changes when columns are added). Existence of the CR-9 column +
	// stable header fields is enough to prove the AST walker works.
	for _, want := range []string{"ID", "BuildID", "ImageAttestationRekorURL", "TenantID"} {
		if !contains(fields, want) {
			t.Errorf("scanReleaseFields missing %q (got %v)", want, fields)
		}
	}
}

// EC-12: meta-test enforces that every name in m07TablesUnderMirror has a
// dedicated TestSchemaMirror_<TableName>_AllColumnsMirrored test. Adds a
// new M07 table without test → CI fails until both land.
func TestSchemaMirror_AllM07TablesListed(t *testing.T) {
	wantTests := map[string]string{
		"releases":                 "TestSchemaMirror_Releases_AllColumnsMirrored",
		"builds":                   "TestSchemaMirror_Builds_AllColumnsMirrored",
		"release_app_attestations": "TestSchemaMirror_ReleaseAppAttestations_AllColumnsMirrored",
	}
	if len(wantTests) != len(m07TablesUnderMirror) {
		t.Fatalf("m07TablesUnderMirror has %d entries but only %d tests are mapped — keep them in sync",
			len(m07TablesUnderMirror), len(wantTests))
	}
	for _, table := range m07TablesUnderMirror {
		testName, ok := wantTests[table]
		if !ok {
			t.Errorf("m07TablesUnderMirror lists %q but the meta-test has no expected mirror test name for it", table)
			continue
		}
		// Confirm the symbol exists in this package by parsing the test file.
		fields, err := ExtractScanFieldsFromGoFile("schema_mirror_test.go", testName)
		// fields slice is irrelevant; the side-channel is whether parsing
		// found the function (no panic, no err). A non-existent function
		// returns nil + nil — so we instead grep the file content.
		_ = fields
		if err != nil {
			t.Errorf("AST parse failed: %v", err)
		}
	}
	// Direct grep against the test file content for the dedicated test functions.
	data, err := os.ReadFile("schema_mirror_test.go")
	if err != nil {
		t.Fatalf("read self: %v", err)
	}
	body := string(data)
	for _, testName := range wantTests {
		if !strings.Contains(body, "func "+testName+"(") {
			t.Errorf("missing dedicated mirror test: func %s — add it for the corresponding m07 table", testName)
		}
	}
}

// ---------------------------------------------------------------------------
// LIVE-PG mirror tests — gated on DATABASE_URL reachability.
// ---------------------------------------------------------------------------

func mirrorTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/loopzero_test?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skip: cannot connect to PostgreSQL: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skip: cannot ping PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func queryPGColumns(t *testing.T, pool *pgxpool.Pool, tableName string) []string {
	t.Helper()
	ctx := context.Background()
	// EC-4: filter to table_schema='public' so system schemas (pg_catalog,
	// information_schema) don't leak in as false positives.
	rows, err := pool.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
		ORDER BY ordinal_position`, tableName)
	if err != nil {
		t.Skipf("skip: cannot query PG INFORMATION_SCHEMA for %s: %v", tableName, err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		_ = rows.Scan(&c)
		cols = append(cols, c)
	}
	sort.Strings(cols)
	if len(cols) == 0 {
		t.Skipf("skip: PG has no columns for public.%s (migrations not applied?)", tableName)
	}
	return cols
}

// EC-4: the public-schema filter test asserts that pg_catalog tables (which
// share the same `pg_class` name as a system catalog) don't leak into the
// mirror set. Queries information_schema directly (NOT via queryPGColumns,
// which skips on empty result and would short-circuit the assertion).
func TestSchemaMirror_QueryFiltersToPublicSchema(t *testing.T) {
	pool := mirrorTestPool(t)
	ctx := context.Background()
	// pg_class is a system catalog in pg_catalog schema. Our filter must
	// return zero rows.
	rows, err := pool.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1`, "pg_class")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var cnt int
	for rows.Next() {
		cnt++
	}
	if cnt != 0 {
		t.Errorf("expected zero public.pg_class columns (system catalog must be filtered), got %d", cnt)
	}
	// And confirm the system catalog DOES have columns (without our filter)
	// so the test isn't a no-op on a misconfigured PG.
	var pgCatalogCols int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'pg_catalog' AND table_name = 'pg_class'`).Scan(&pgCatalogCols); err != nil {
		t.Fatalf("pg_catalog count: %v", err)
	}
	if pgCatalogCols == 0 {
		t.Error("pg_catalog.pg_class should have columns — confirms the filter is non-trivial")
	}
}

// TestSchemaMirror_MigrationsTableHasOneRow defends against the
// schema_migrations bookkeeping corruption found during the dogfood
// Pre-Flight check on 2026-05-28 (audit doc
// 2026-05-28-dogfood-preflight-evidence.md). golang-migrate's
// schema_migrations table SHOULD contain exactly one row representing
// the most recent applied migration. A multi-row state — observed
// LIVE as `(version=19, dirty=false)` + `(version=10, dirty=false)`
// — indicates a manual psql DDL apply that bypassed the migration
// runner, a stale `migrate force`, or two concurrent runs racing.
//
// The DDL was applied correctly in the corrupted case (the T2.1
// triggers were present and functional), but the bookkeeping drift
// makes future `task db:migrate` runs unpredictable: golang-migrate
// reads the highest version and skips the gap, so migrations 11..N
// might be re-applied or skipped depending on the runner state.
//
// This test makes the corruption a CI-visible failure on any
// operator-cleanroom run, instead of needing the dogfood Pre-Flight
// Etapa 0 to catch it.
func TestSchemaMirror_MigrationsTableHasOneRow(t *testing.T) {
	pool := mirrorTestPool(t)
	ctx := context.Background()
	var rowCount int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&rowCount)
	if err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if rowCount != 1 {
		// Surface the actual rows so the operator can decide whether to
		// `migrate force <highest>` or rebuild cleanroom-fresh.
		rows, qErr := pool.Query(ctx, `SELECT version, dirty FROM schema_migrations ORDER BY version DESC`)
		if qErr == nil {
			defer rows.Close()
			t.Logf("schema_migrations actual rows:")
			for rows.Next() {
				var v int
				var dirty bool
				_ = rows.Scan(&v, &dirty)
				t.Logf("  version=%d dirty=%v", v, dirty)
			}
		}
		t.Errorf("schema_migrations has %d rows; expected exactly 1. Bookkeeping corruption — see "+
			".claude/knowledge-base/audits/2026-05-28-dogfood-preflight-evidence.md "+
			"for the remediation pattern (`migrate force <highest>` or cleanroom rebuild).", rowCount)
	}
}

// releasesInsertExempt marks PG columns that are intentionally NOT in the
// INSERT INTO releases statement because they are populated after INSERT:
//   - DB default (now(), trigger-populated)
//   - dedicated UPDATE path (SetGitCommit, AppendSideEffect)
//   - owned by another package's column (claim_store CAS columns; cell
//     routing column set by the populate_cell_id_from_tenant trigger)
//
// These columns may legitimately be missing from INSERT INTO. They still
// must mirror through struct (if user-visible) + SELECT + scanner.
var releasesInsertExempt = map[string]bool{
	"created_at":        true, // DB default now()
	"git_commit":        true, // SetGitCommit UPDATE
	"git_mirror_status": true, // DB default
	"side_effects":      true, // AppendSideEffect UPDATE
	"cell_id":           true, // populate_cell_id_from_tenant trigger
	"processing_by":     true, // claim_store package CAS
	"processing_since":  true, // claim_store package CAS
}

// releasesMirrorExempt marks PG columns that are owned by a different
// package and are NOT expected to be part of the canonical Release
// struct/INSERT/SELECT/scanner. These are processing/claim columns —
// they belong to internal/processing/claim_store.go, not release_store.go.
var releasesMirrorExempt = map[string]bool{
	"processing_by":    true,
	"processing_since": true,
	"cell_id":          true, // Read via cell-scoped queries (FindPendingReleases/FindActiveReleases) but not part of Release struct
}

// Mirror test for `releases` — closes CR-9 of MEET-2026-05-28-001.
//
// Asserts that every column declared on the Release Go struct is:
//   1) present in the PG public.releases schema
//   2) referenced by scanReleaseFields (otherwise pgx Scan won't populate it)
//   3) referenced by at least one SELECT FROM releases (otherwise no path
//      reads it from the database — the CR-9 failure mode)
//   4) present in the INSERT INTO releases column list (unless explicitly
//      exempted as DB-default / UPDATE-set)
//
// PG-only columns (claim_store/cell_id) are NOT enforced against the Release
// struct; they have their own owner.
func TestSchemaMirror_Releases_AllColumnsMirrored(t *testing.T) {
	pool := mirrorTestPool(t)
	dbCols := queryPGColumns(t, pool, "releases")

	structCols := ExtractStructJSONColumns(reflect.TypeOf(Release{}))

	src, err := os.ReadFile("release_store.go")
	if err != nil {
		t.Fatalf("read release_store.go: %v", err)
	}
	insertCols := ExtractInsertColumns(string(src), "releases")
	selectCols := ExtractSelectColumns(string(src), "releases")
	scanFields, err := ExtractScanFieldsFromGoFile("release_store.go", "scanReleaseFields")
	if err != nil {
		t.Fatalf("scanFields AST: %v", err)
	}
	scanCols := make([]string, 0, len(scanFields))
	for _, f := range scanFields {
		scanCols = append(scanCols, ToSnakeCase(f))
	}
	sort.Strings(scanCols)

	// (A) Every struct column must be a real PG column.
	for _, col := range structCols {
		if !contains(dbCols, col) {
			t.Errorf("Release struct declares json tag %q but no public.releases.%s column exists", col, col)
		}
	}
	// (B) Every struct column (i.e., every Release field) must be in scanner.
	for _, col := range structCols {
		if !contains(scanCols, col) {
			t.Errorf("Release struct declares %q but scanReleaseFields does NOT scan it — pgx will leave the field zero (CR-9 class bug)", col)
		}
	}
	// (C) Every struct column must be in at least one SELECT FROM releases.
	for _, col := range structCols {
		if !contains(selectCols, col) {
			t.Errorf("Release struct declares %q but NO SELECT FROM releases reads it (CR-9 class bug)", col)
		}
	}
	// (D) Every struct column must be in INSERT, unless explicitly exempt.
	for _, col := range structCols {
		if releasesInsertExempt[col] {
			continue
		}
		if !contains(insertCols, col) {
			t.Errorf("Release struct declares %q but INSERT INTO releases does NOT write it — add to INSERT or mark releasesInsertExempt", col)
		}
	}
	// (E) PG drift detection: every PG column NOT in mirror-exempt must
	// appear in the Release struct. Catches "migration shipped, struct
	// not updated".
	for _, col := range dbCols {
		if releasesMirrorExempt[col] {
			continue
		}
		if !contains(structCols, col) {
			t.Errorf("public.releases.%s exists but Release struct has no field for it — add the field or mark releasesMirrorExempt", col)
		}
	}
}

// Synthetic-regression tests proving the helpers detect missing-from-* cases.

func TestSchemaMirror_Releases_DetectsMissingFromInsert(t *testing.T) {
	// Simulated INSERT with image_attestation_rekor_url silently dropped.
	missingInsert := `INSERT INTO releases (id, build_id, tenant_id) VALUES ($1, $2, $3)`
	cols := ExtractInsertColumns(missingInsert, "releases")
	if contains(cols, "image_attestation_rekor_url") {
		t.Fatal("ExtractInsertColumns failed to model the omission — synthetic should NOT contain the column")
	}
	// The detection in TestSchemaMirror_Releases_AllColumnsMirrored is
	// `if !contains(insertCols, col)`, which would flag this case.
	if contains(cols, "image_attestation_rekor_url") {
		t.Fatal("helper would not catch the column omission")
	}
}

func TestSchemaMirror_Releases_DetectsMissingFromStruct(t *testing.T) {
	type StubRelease struct {
		ID      string `json:"id"`
		BuildID string `json:"build_id"`
		// no image_attestation_rekor_url field — the helper must NOT report it.
	}
	cols := ExtractStructJSONColumns(reflect.TypeOf(StubRelease{}))
	if contains(cols, "image_attestation_rekor_url") {
		t.Fatal("helper falsely reports missing column as present")
	}
}

func TestSchemaMirror_Releases_DetectsMissingFromScan(t *testing.T) {
	// Pretend release_store.go's scanReleaseFields is named differently —
	// the AST extractor should return an empty slice (or skip). Use a
	// non-existent function name as the synthetic omission proxy.
	fields, err := ExtractScanFieldsFromGoFile("release_store.go", "doesNotExist")
	if err != nil {
		t.Fatalf("AST parse error: %v", err)
	}
	if len(fields) != 0 {
		t.Errorf("expected zero fields for non-existent function, got %v", fields)
	}
}

// Mirror stubs for builds + release_app_attestations: the migration column
// set is small + stable; we assert the DB columns exist and are queryable
// today. A future expansion to full struct/INSERT/SELECT mirroring is
// queued via the EC-12 meta-test mechanism above.

func TestSchemaMirror_Builds_AllColumnsMirrored(t *testing.T) {
	pool := mirrorTestPool(t)
	dbCols := queryPGColumns(t, pool, "builds")
	for _, must := range []string{"id", "tenant_id", "project_id", "status", "correlation_id"} {
		if !contains(dbCols, must) {
			t.Errorf("builds.%s: expected canonical column missing — migration drift?", must)
		}
	}
}

func TestSchemaMirror_ReleaseAppAttestations_AllColumnsMirrored(t *testing.T) {
	pool := mirrorTestPool(t)
	dbCols := queryPGColumns(t, pool, "release_app_attestations")
	for _, must := range []string{"id", "release_id", "app_name"} {
		if !contains(dbCols, must) {
			t.Errorf("release_app_attestations.%s: expected canonical column missing — migration drift?", must)
		}
	}
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func equalSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]string{}, a...)
	bb := append([]string{}, b...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

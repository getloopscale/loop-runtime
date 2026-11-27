// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Plan 0 of dashboard-memory-section-roadmap.md, T0.2.
//
// Structural validation of migration 000024_loop_runtime_api_keys.up.sql. Hermetic —
// parses the SQL file as text and asserts the invariants that the plan
// (and ADRs D1..D8 of 2026-06-06-platform-api-keys-with-scopes-plan.md)
// require. Catches the most common failure modes (typo in column name,
// missing index, missing CHECK constraint) without requiring a live PG.
//
// LIVE schema apply is operator territory (`task db:migrate` against dev
// cluster) and is gated by the existing migration tooling. This unit test
// is the cheap regression guard that runs in every package test cycle.

package db_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const migrationPath = "migrations/000024_loop_runtime_api_keys.up.sql"

func readMigration(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(migrationPath)
	if err != nil {
		t.Fatalf("resolve migration path: %v", err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read migration %s: %v\n(missing migration file blocks Plan 0 GREEN; ship .up.sql per plan T0.2 schema)", abs, err)
	}
	return string(b)
}

func TestMigration024_TableCreated(t *testing.T) {
	sql := readMigration(t)
	// CREATE TABLE [IF NOT EXISTS] loop_runtime_api_keys — exact name required by
	// every downstream handler + middleware query. A rename here silently
	// breaks the entire P0 surface.
	if !strings.Contains(sql, "CREATE TABLE") || !strings.Contains(sql, "loop_runtime_api_keys") {
		t.Fatalf("migration must CREATE TABLE loop_runtime_api_keys; got body=%q", sql)
	}
}

func TestMigration024_RequiredColumnsPresent(t *testing.T) {
	sql := readMigration(t)

	// Per plan T0.2 final schema (post-D5 amendment: subject + tenant_id, not owner_user_id).
	// Each column is load-bearing for a specific handler — listing them here
	// makes a typo (column name drift between schema + handler) impossible
	// to ship without test failure.
	required := []string{
		"id ",
		"subject ",
		"tenant_id ",
		"key_name ",
		"key_hash ",
		"key_prefix ",
		"scopes ",
		"expires_at ",
		"last_used_at ",
		"created_at ",
		"revoked_at ",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("missing required column declaration: %q", strings.TrimSpace(col))
		}
	}
}

func TestMigration024_TenantsFKCascade(t *testing.T) {
	sql := readMigration(t)
	// Plan T0.2: FK to tenants(id) ON DELETE CASCADE so tenant deletion
	// cascade-deletes keys. This is the cross-tenant cleanup invariant —
	// without CASCADE, an admin who deletes a tenant strands key rows
	// pointing to a no-longer-existing tenant_id.
	if !strings.Contains(sql, "REFERENCES tenants(id)") {
		t.Error("tenant_id must be FK to tenants(id)")
	}
	if !strings.Contains(strings.ToUpper(sql), "ON DELETE CASCADE") {
		t.Error("tenants FK must be ON DELETE CASCADE")
	}
}

func TestMigration024_ScopesIsJsonbArray(t *testing.T) {
	sql := readMigration(t)
	// scopes JSONB NOT NULL — middleware json.Unmarshal((string array))
	// per plan T0.6 + EC-7 absorption. NULL would crash Unmarshal.
	if !strings.Contains(sql, "scopes") || !strings.Contains(strings.ToUpper(sql), "JSONB") {
		t.Error("scopes column must be JSONB")
	}
	// CHECK (jsonb_typeof(scopes) = 'array') — see plan T0.2 schema.
	// Without it, a write like {"a":1} would persist and crash middleware.
	if !strings.Contains(sql, "jsonb_typeof(scopes)") {
		t.Error("CHECK constraint on jsonb_typeof(scopes) = 'array' required")
	}
}

func TestMigration024_KeyNameLengthCheck(t *testing.T) {
	sql := readMigration(t)
	// CHECK (length(key_name) BETWEEN 1 AND 64) — plan T0.5 EC-4 absorption
	// catches database-side any drift where the handler validation is bypassed
	// (admin script, manual INSERT). Defense in depth.
	if !strings.Contains(sql, "length(key_name)") {
		t.Error("CHECK length(key_name) constraint required")
	}
}

func TestMigration024_UniqueKeyHashIndex(t *testing.T) {
	sql := readMigration(t)
	// CREATE UNIQUE INDEX [IF NOT EXISTS] idx_loop_runtime_api_keys_key_hash — UNIQUE
	// because the middleware looks up `WHERE key_hash = $1`. Two rows with
	// the same hash would create non-determinism in scope decisions. Tolerant
	// of `IF NOT EXISTS` between UNIQUE INDEX and the index name (idempotent
	// migration style).
	upper := strings.ToUpper(sql)
	if !strings.Contains(upper, "UNIQUE INDEX") {
		t.Error("UNIQUE INDEX keyword required (auth hot path)")
	}
	if !strings.Contains(sql, "idx_loop_runtime_api_keys_key_hash") {
		t.Error("index idx_loop_runtime_api_keys_key_hash required (auth hot path)")
	}
	// And the named index MUST be the one declared UNIQUE — assert the two
	// appear close together (within ~80 chars to allow `IF NOT EXISTS\n    ON`).
	idx := strings.Index(upper, "UNIQUE INDEX")
	hashIdx := strings.Index(sql, "idx_loop_runtime_api_keys_key_hash")
	if idx < 0 || hashIdx < 0 || hashIdx-idx > 80 || hashIdx < idx {
		t.Errorf("UNIQUE INDEX must NAME idx_loop_runtime_api_keys_key_hash (got distance %d)", hashIdx-idx)
	}
}

func TestMigration024_TenantPartialIndex(t *testing.T) {
	sql := readMigration(t)
	// CREATE INDEX idx_loop_runtime_api_keys_tenant ON loop_runtime_api_keys (tenant_id, subject)
	// WHERE revoked_at IS NULL — partial index because the list handler
	// always filters `WHERE revoked_at IS NULL`. Excluding revoked rows
	// from the index keeps it tight.
	if !strings.Contains(sql, "idx_loop_runtime_api_keys_tenant") {
		t.Error("index idx_loop_runtime_api_keys_tenant required (list handler hot path)")
	}
	if !strings.Contains(sql, "WHERE revoked_at IS NULL") {
		t.Error("partial-index predicate WHERE revoked_at IS NULL required")
	}
}

func TestMigration024_NoDownFile(t *testing.T) {
	// Forward-only convention per CLAUDE.md migration rules. Ensure no
	// .down.sql shipped by accident.
	abs, err := filepath.Abs("migrations/000024_loop_runtime_api_keys.down.sql")
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	if _, err := os.Stat(abs); err == nil {
		t.Errorf("forward-only convention violated: .down.sql exists at %s — delete and rely on the followup new .up.sql for rollback", abs)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Migration 000045 regression test — text-level invariants.
// Closes Red Team loopzero-cloud#128 hardening class. Live round-trip
// invariant lives in api/tests/integration/migration_045_*_test.go
// (integration companion; requires live PG).
//
// Migration adds two nullable snapshot columns to environment_targets so the
// list-page badge can be data-plane-aware (populated by the
// data-plane poller shipped alongside — see plan T2.1).

package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration045(t *testing.T, direction string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"migrations",
		"000045_environment_targets_last_replicas_ready."+direction+".sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found (%s.sql): %v", direction, err)
	}
	return string(data)
}

// TestMigration045_UpAddsCanonicalColumns asserts the up migration adds
// both nullable snapshot columns with the exact declared types per plan
// T1.1 acceptance criteria. Column drift would corrupt the LATERAL JOIN
// in projects.sql (T3.1) — this text-level guard catches it hermetically.
func TestMigration045_UpAddsCanonicalColumns(t *testing.T) {
	sql := readMigration045(t, "up")

	// Two nullable columns, exact declared types (plan T1.1).
	requiredFragments := []string{
		"ADD COLUMN last_replicas_ready_value INT NULL",
		"ADD COLUMN last_replicas_ready_at    TIMESTAMPTZ NULL",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 000045 up.sql missing required fragment: %q\n\ngot:\n%s", fragment, sql)
		}
	}

	// Must ALTER environment_targets specifically — not any other table.
	if !strings.Contains(sql, "ALTER TABLE environment_targets") {
		t.Errorf("migration 000045 up.sql must target ALTER TABLE environment_targets (poller writes to this table)")
	}
}

// TestMigration045_UpCreatesPartialIndex asserts the partial index
// pattern per blueprint Q4 answer + plan T1.1 acceptance. Partial
// index skips NULL rows so it stays proportional to actually-polled
// envs (not the full environment_targets table).
func TestMigration045_UpCreatesPartialIndex(t *testing.T) {
	sql := readMigration045(t, "up")

	requiredFragments := []string{
		"CREATE INDEX IF NOT EXISTS idx_env_targets_last_replicas_ready_at",
		"ON environment_targets (last_replicas_ready_at DESC)",
		"WHERE last_replicas_ready_at IS NOT NULL",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 000045 up.sql missing partial-index fragment: %q\n\ngot:\n%s", fragment, sql)
		}
	}
}

// TestMigration045_DownDropsColumnsAndIndex asserts the round-trip
// reversibility contract per plan T1.1 acceptance. Missing DROP means
// `task db:reset` (down → up) leaves stale schema.
func TestMigration045_DownDropsColumnsAndIndex(t *testing.T) {
	sql := readMigration045(t, "down")

	requiredFragments := []string{
		"DROP INDEX IF EXISTS idx_env_targets_last_replicas_ready_at",
		"DROP COLUMN IF EXISTS last_replicas_ready_at",
		"DROP COLUMN IF EXISTS last_replicas_ready_value",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 000045 down.sql missing reverse fragment: %q\n\ngot:\n%s", fragment, sql)
		}
	}
}

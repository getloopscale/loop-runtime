// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Migration 000015 regression test — text-level invariants for the
// build_apps.build_id index (T3.2 of deep-review-2026-05-10-remediation-plan,
// find_data_missing_fk_index HIGH).

package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration015(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"..", "..", "internal", "db", "migrations",
		"000015_build_apps_index.up.sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration015_CreatesBtreeIndexOnBuildID(t *testing.T) {
	sql := readMigration015(t)

	// MUST create an index on (build_id) — the FK cascade requires it.
	if !strings.Contains(sql, "ON build_apps (build_id)") {
		t.Error("migration must create index on build_apps(build_id)")
	}
	// MUST be idempotent (CREATE INDEX IF NOT EXISTS) — migrations
	// re-applied during recovery must not error.
	if !strings.Contains(sql, "IF NOT EXISTS") {
		t.Error("migration must use IF NOT EXISTS for idempotency")
	}
	// MUST reference the index by canonical name so the down migration
	// can drop it without ambiguity.
	if !strings.Contains(sql, "idx_build_apps_build_id") {
		t.Error("migration must use canonical index name idx_build_apps_build_id")
	}
}

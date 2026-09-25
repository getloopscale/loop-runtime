// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Migration 000016 regression test — text-level invariants for the
// `builds.bundle_digest` column (T2.2 of deep-review-2026-05-10-remediation-plan,
// find_sec_no_bundle_integrity HIGH).

package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration016(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"migrations",
		"000016_builds_bundle_digest.up.sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration016_AddsBundleDigestColumn(t *testing.T) {
	sql := readMigration016(t)

	if !strings.Contains(sql, "ADD COLUMN IF NOT EXISTS bundle_digest") {
		t.Error("migration must add bundle_digest column")
	}
	if !strings.Contains(sql, "ALTER TABLE builds") {
		t.Error("migration must target the builds table")
	}
	if !strings.Contains(sql, "COMMENT ON COLUMN builds.bundle_digest") {
		t.Error("migration must include COMMENT for governance discoverability")
	}
}

func TestMigration016_ColumnIsNullableForBackwardCompat(t *testing.T) {
	sql := readMigration016(t)
	// Inspect ONLY the ALTER statement line — comments may mention the
	// future NOT NULL flip, and that is intentional documentation.
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		if strings.Contains(trimmed, "ADD COLUMN") && strings.Contains(strings.ToUpper(trimmed), "NOT NULL") {
			t.Errorf("ADD COLUMN line uses NOT NULL — must be nullable for backward compat:\n  %s", trimmed)
		}
	}
}

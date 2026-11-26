// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// T3.1 of deep-review-remediation-plan (closes find_test_002 HIGH).
//
// Migration 000010 (B37 fix) restored ROLLING_BACK and ROLLED_BACK to the
// releases.status CHECK constraint after migration 000008's DROP+RECREATE
// silently dropped them. CLAUDE.md mandates that every bug fix produces a
// regression test BEFORE the fix; the original B37 fix shipped without one.
// This text-level test prevents future migrations from re-introducing the
// regression by silently dropping these states from the constraint.
//
// Live invariant against the running DB is enforced by
// TestStateMachineCheckConstraintInvariant in api/tests/integration/. This
// unit test catches text-level drift even before migrations are applied.

package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration010(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"migrations",
		"000010_release_rollback_states.up.sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration010_ContainsRollingBack(t *testing.T) {
	sql := readMigration010(t)
	if !strings.Contains(sql, "'ROLLING_BACK'") {
		t.Fatal("migration 000010 must include 'ROLLING_BACK' in the CHECK constraint — find_data_001 / B37 regression class")
	}
}

func TestMigration010_ContainsRolledBack(t *testing.T) {
	sql := readMigration010(t)
	if !strings.Contains(sql, "'ROLLED_BACK'") {
		t.Fatal("migration 000010 must include 'ROLLED_BACK' in the CHECK constraint — find_data_001 / B37 regression class")
	}
}

func TestMigration010_DropsAndAddsConstraint(t *testing.T) {
	sql := readMigration010(t)
	if !strings.Contains(sql, "DROP CONSTRAINT IF EXISTS releases_status_check") {
		t.Fatal("migration 000010 must DROP existing constraint before re-adding (idempotent forward migration)")
	}
	if !strings.Contains(sql, "ADD CONSTRAINT releases_status_check") {
		t.Fatal("migration 000010 must ADD the canonical constraint after dropping")
	}
}

// TestMigration010_TargetsCorrectTable — ensures the constraint name matches
// what TestStateMachineCheckConstraintInvariant queries
// (releases_status_check, not chk_releases_status). T0.4 fixed the canary
// in database_hardening_test.go; this test pins the migration side.
func TestMigration010_TargetsCorrectTable(t *testing.T) {
	sql := readMigration010(t)
	if !strings.Contains(sql, "ALTER TABLE releases") {
		t.Fatal("migration 000010 must target the releases table")
	}
	if strings.Contains(sql, "chk_releases_status") {
		t.Fatal("migration 000010 must use auto-generated constraint name 'releases_status_check', not 'chk_releases_status'")
	}
}

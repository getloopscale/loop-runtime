// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Migration 000011 regression test — text-level invariants.
// Closes find_data_001 (CRITICAL) + find_data_002 (CRITICAL) regression
// guard. Live invariant lives in TestStateMachineCheckConstraintInvariant
// (api/tests/integration/).

package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration011(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"migrations",
		"000011_release_check_constraint_canonical.up.sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration011_ContainsAllCanonicalStates(t *testing.T) {
	sql := readMigration011(t)
	required := []string{
		"'READY'", "'PENDING'", "'ACTIVE'",
		"'GIT_MIRROR_FAILED'", "'SYNC_FAILED'",
		"'DRIFT_ALERT'", "'SUPERSEDED'",
		"'ROLLING_BACK'", "'ROLLED_BACK'", "'ROLLBACK_FAILED'",
		"'CANCELLED'", "'ARCHIVED'",
	}
	for _, s := range required {
		if !strings.Contains(sql, s) {
			t.Errorf("migration 000011 missing required state %s — find_data_001 regression", s)
		}
	}
}

func TestMigration011_DropsPhantomStates(t *testing.T) {
	sql := readMigration011(t)
	// The migration MUST NOT re-introduce BUILDING or FAILED phantom states.
	// We check the CHECK clause specifically (not header comments). The
	// CHECK clause is the lines between "CHECK (status IN (" and "))".
	checkStart := strings.Index(sql, "CHECK (status IN (")
	if checkStart == -1 {
		t.Fatal("migration 000011 must have CHECK (status IN (...))")
	}
	checkEnd := strings.Index(sql[checkStart:], "))")
	if checkEnd == -1 {
		t.Fatal("migration 000011 CHECK clause must close with ))")
	}
	checkBody := sql[checkStart : checkStart+checkEnd]
	for _, phantom := range []string{"'BUILDING'", "'FAILED'"} {
		if strings.Contains(checkBody, phantom) {
			t.Errorf("migration 000011 CHECK still contains phantom state %s — find_data_002 regression", phantom)
		}
	}
}

func TestMigration011_HasPreMigrationGuard(t *testing.T) {
	sql := readMigration011(t)
	// EC-1 of edge-case review: SQL guard must abort migration if phantom
	// rows are present. The guard uses RAISE EXCEPTION inside DO $$ block.
	if !strings.Contains(sql, "RAISE EXCEPTION") {
		t.Fatal("migration 000011 must include RAISE EXCEPTION guard against phantom rows (EC-1)")
	}
	if !strings.Contains(sql, "phantom states") {
		t.Fatal("migration 000011 guard must reference 'phantom states' (audit traceability)")
	}
}

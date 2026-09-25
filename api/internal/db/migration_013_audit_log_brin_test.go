// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Migration 000013 regression test — text-level invariants for the BRIN
// time-index on audit_log. Closes find_data_005 partial regression guard.

package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration013(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"migrations",
		"000013_audit_log_time_index.up.sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration013_UsesBRINIndex(t *testing.T) {
	sql := readMigration013(t)
	if !strings.Contains(sql, "USING BRIN (created_at)") {
		t.Fatal("migration 000013 must use BRIN on created_at — BTREE would be 1000x larger for time-series")
	}
}

func TestMigration013_TargetsAuditLog(t *testing.T) {
	sql := readMigration013(t)
	if !strings.Contains(sql, "ON audit_log") {
		t.Fatal("migration 000013 must target audit_log table")
	}
}

func TestMigration013_IsIdempotent(t *testing.T) {
	sql := readMigration013(t)
	if !strings.Contains(sql, "CREATE INDEX IF NOT EXISTS") {
		t.Fatal("migration 000013 must use IF NOT EXISTS — re-application after manual partial-apply must not error")
	}
}

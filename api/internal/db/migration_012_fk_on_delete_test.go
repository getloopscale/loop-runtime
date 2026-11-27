// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Migration 000012 regression test — text-level invariants.
// Closes find_data_004 (HIGH) regression guard. Live invariant lives in
// TestForeignKeyConstraintsAreExplicit (api/tests/integration/).

package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration012(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"migrations",
		"000012_fk_on_delete_explicit.up.sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration012_TouchesAll15FKs(t *testing.T) {
	sql := readMigration012(t)
	// Every FK constraint added/replaced should appear by name. If the
	// migration accidentally drops one, tests detect drift.
	required := []string{
		"tenants_owner_id_fkey",
		"tenant_members_tenant_id_fkey",
		"tenant_members_user_id_fkey",
		"projects_tenant_id_fkey",
		"projects_created_by_fkey",
		"build_apps_build_id_fkey",
		"releases_build_id_fkey",
		"environment_targets_desired_release_id_fkey",
		"environment_targets_active_release_id_fkey",
		"subscriptions_tenant_id_fkey",
		"usage_records_tenant_id_fkey",
		"user_identity_map_user_id_fkey",
		"github_installations_tenant_id_fkey",
		"project_repositories_installation_id_fkey",
		"tenants_pending_deletion_tenant_id_fkey",
	}
	for _, fk := range required {
		if !strings.Contains(sql, fk) {
			t.Errorf("migration 000012 missing FK %s — drift from find_data_004 baseline", fk)
		}
	}
}

func TestMigration012_NoBareReferencesWithoutOnDelete(t *testing.T) {
	sql := readMigration012(t)
	// Every FOREIGN KEY in this migration MUST have ON DELETE clause. Strip
	// comments first (header explanation has REFERENCES tokens).
	codeOnly := stripSQLComments(sql)
	lines := strings.Split(codeOnly, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "FOREIGN KEY") {
			continue
		}
		// Find the matching ON DELETE within the next 3 lines (allowing for
		// REFERENCES on a separate line).
		hasOnDelete := false
		for j := i; j < i+4 && j < len(lines); j++ {
			if strings.Contains(lines[j], "ON DELETE") {
				hasOnDelete = true
				break
			}
		}
		if !hasOnDelete {
			t.Errorf("migration 000012 line %d FOREIGN KEY missing ON DELETE: %q", i, trimmed)
		}
	}
}

// stripSQLComments removes -- single-line comments from SQL text.
func stripSQLComments(sql string) string {
	out := []string{}
	for _, line := range strings.Split(sql, "\n") {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

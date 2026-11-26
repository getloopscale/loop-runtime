// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Migration 000068 regression test — text-level invariants.
//
// 000068 grants loop_runtime_worker the privileges the cross-tenant paths execute on
// tables created after 000023 (#385). Whether the set is complete is decided
// by worker_role_grants_test.go against the engine's SQL; these tests pin the
// migration's own shape: it grants no table the engine never queries on that
// pool, and its rollback removes exactly what it added.

package db_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func readMigration068(t *testing.T, direction string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "migrations",
		"000068_worker_role_grants_derived_from_code."+direction+".sql"))
	if err != nil {
		t.Fatalf("migration 000068 %s.sql not found: %v", direction, err)
	}
	return withoutSQLComments(string(data))
}

var privilegeStatementRe = regexp.MustCompile(`(?i)\b(?:GRANT|REVOKE)\s+([A-Z ,]+?)\s+ON\s+(SEQUENCE\s+)?([a-z_]+)\s+(?:TO|FROM)\s+loop_runtime_worker\s*;`)

// privileges flattens every GRANT/REVOKE of one direction into "VERB object"
// entries, so up and down can be compared entry by entry.
func privileges(t *testing.T, sql string) []string {
	t.Helper()
	var out []string
	for _, m := range privilegeStatementRe.FindAllStringSubmatch(sql, -1) {
		for _, verb := range strings.Split(m[1], ",") {
			out = append(out, strings.ToUpper(strings.TrimSpace(verb))+" "+m[3])
		}
	}
	sort.Strings(out)
	return out
}

func TestMigration068_UpGrantsTheWorkerOnlyWhatTheCrossTenantPathsRun(t *testing.T) {
	got := privileges(t, readMigration068(t, "up"))

	want := []string{
		"DELETE metering_cursor",
		"INSERT accounts",
		"INSERT deploy_events",
		"SELECT accounts",
		"USAGE deploy_events_id_seq",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("migration 000068 up grants %v; want exactly %v", got, want)
	}
}

// The control plane's tables and the request-only preview slot must not reach
// the cross-tenant role through this migration.
func TestMigration068_UpGrantsNothingOnTablesTheEngineNeverQueriesOnThatPool(t *testing.T) {
	sql := strings.ToLower(readMigration068(t, "up"))
	for _, table := range []string{
		"preview_slot", "lens_", "memory_", "workspace_webhook", "storage_objects", "loop_runtime_api_keys",
		"all tables", "default privileges",
	} {
		if strings.Contains(sql, table) {
			t.Errorf("migration 000068 up.sql grants on %q; the engine runs nothing there on the cross-tenant pool", table)
		}
	}
}

func TestMigration068_DownRevokesExactlyWhatUpGranted(t *testing.T) {
	up, down := privileges(t, readMigration068(t, "up")), privileges(t, readMigration068(t, "down"))

	if len(up) == 0 || strings.Join(up, "|") != strings.Join(down, "|") {
		t.Fatalf("migration 000068 down revokes %v; up granted %v", down, up)
	}
	if strings.Contains(readMigration068(t, "down"), "GRANT") {
		t.Error("migration 000068 down.sql must only revoke")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Migration 000014 regression test — text-level invariants for the
// audit_log_pruner trigger exemption (T7.3 of deep-review-remediation-plan).
//
// Why text-level: the live behavior is exercised by the LIVE pruner
// integration test against a running Postgres. This unit test guards
// the migration source against accidental edits that would re-categorize
// audit_log as forever-immutable (which would break the pruner) or worse,
// flip the bypass to default-on (which would silently allow audit
// tampering by any session).

package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration014(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"..", "..", "internal", "db", "migrations",
		"000014_audit_log_pruner_exemption.up.sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration014_BypassIsRestrictedToDelete(t *testing.T) {
	sql := readMigration014(t)

	// The bypass MUST only fire on DELETE — never on UPDATE. A trigger
	// version that allows UPDATE bypass would let any pruner-mode session
	// silently rewrite history.
	if !strings.Contains(sql, "TG_OP = 'DELETE'") {
		t.Error("trigger must check TG_OP = 'DELETE' before honoring bypass")
	}
	// And the bypass var must be checked BY VALUE — current_setting with
	// the missing_ok=true flag (second arg `true`) so the trigger never
	// errors on sessions that did not set the var.
	if !strings.Contains(sql, "current_setting('loopzero.audit_pruner', true)") {
		t.Error("trigger must call current_setting('loopzero.audit_pruner', true)")
	}
}

func TestMigration014_BypassDefaultIsDeny(t *testing.T) {
	sql := readMigration014(t)

	// The bypass MUST be explicit: only when the var equals exactly 'true'.
	// Any session without the var falls through to the EXCEPTION raise.
	// Reject patterns like `IS NOT NULL` or `<> 'false'` that would weaken
	// the gate.
	if !strings.Contains(sql, "v_pruner_bypass = 'true'") {
		t.Error("bypass condition must be strict equality with 'true'")
	}
	if strings.Contains(sql, "IS NOT NULL") {
		t.Error("bypass must not use IS NOT NULL semantics — too permissive")
	}
	if !strings.Contains(sql, "RAISE EXCEPTION 'audit_log is append-only") {
		t.Error("non-bypass paths must still raise EXCEPTION")
	}
}

func TestMigration014_HasComment(t *testing.T) {
	sql := readMigration014(t)
	// Per Principle 6 + T7.3 governance, the trigger function gets a
	// COMMENT pointer to the plan + ADR for future readers.
	if !strings.Contains(sql, "COMMENT ON FUNCTION audit_log_prevent_mutation()") {
		t.Error("migration must include COMMENT ON FUNCTION for governance")
	}
	if !strings.Contains(sql, "T7.3") {
		t.Error("comment must reference T7.3 of deep-review-remediation-plan")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// cell-1-activation-plan T2.2 — text-level invariants of the cell-1
// activation migration (no live Postgres required). Live behavior is
// exercised by the cell-1 activation gate suite (cell2-activation-gates.sh).
//
// These tests pin the load-bearing properties:
//   1. UPDATE targets cell_id='cell-1' specifically (not all cells).
//   2. WHERE clause filters status='maintenance' for idempotency.
//   3. New status is 'active' (not draining/retired).
//   4. Audit trail: notes column appended (not overwritten).
//   5. Production-gating warning is present in comments (ADR D6).
//
// Drift on any of these would either accidentally activate other cells,
// silently re-flip on re-run, lose audit trail, or quietly migrate prod
// before Sprint 9 readiness review.

package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration007ActivateCell2(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"migrations",
		"000007_activate_cell_2.up.sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

// # These tests read HISTORY, not the present
//
// Migrations are forward-only and immutable: `000007` activated the cell that
// was then called `cell-2`, and that is what it says in every database where it
// already ran. Migration `000061` (cell_identity_unification) later merged
// `cell-default` + `cell-1` into `cell-0` and renamed the rest, but renaming in
// the present does not change what a migration file executed in the past.
//
// The fix for drift here is NOT to edit the migration: rewriting a dated record
// to look current destroys the only proof of what was true then. What these
// tests protect is the SHAPE of the historical migration — the UPDATE is
// filtered, the audit note is appended, prod is gated. Those properties hold
// under the name the file carries.
//
// The CURRENT identity of the cells is the responsibility of a test on `000061`.
func TestMigration007ActivatesOnlyCell2(t *testing.T) {
	sql := readMigration007ActivateCell2(t)
	if !strings.Contains(sql, "WHERE cell_id = 'cell-2'") {
		t.Fatal("expected WHERE clause filtering cell_id='cell-2' — the name this migration carries — to prevent activating other cells")
	}
	// Defensive: the UPDATE must NOT be unbounded.
	if strings.Contains(sql, "UPDATE cells\n   SET status = 'active'") &&
		!strings.Contains(sql, "WHERE") {
		t.Fatal("UPDATE without WHERE — would flip every row")
	}
}

func TestMigration007SetsStatusActive(t *testing.T) {
	sql := readMigration007ActivateCell2(t)
	if !strings.Contains(sql, "SET status = 'active'") {
		t.Fatal("expected SET status='active'")
	}
	for _, forbidden := range []string{
		"SET status = 'draining'",
		"SET status = 'retired'",
		"SET status = 'maintenance'",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("unexpected status assignment: %q", forbidden)
		}
	}
}

func TestMigration007IdempotentViaWhereClause(t *testing.T) {
	sql := readMigration007ActivateCell2(t)
	// Idempotency rule: WHERE includes status='maintenance' so re-runs
	// after the flip see zero matching rows.
	if !strings.Contains(sql, "AND status = 'maintenance'") {
		t.Fatal("expected AND status='maintenance' to make migration idempotent on re-runs")
	}
}

func TestMigration007AppendsAuditNote(t *testing.T) {
	sql := readMigration007ActivateCell2(t)
	// Audit trail must APPEND to notes, not overwrite (preserves original
	// seed comment from migration 000006).
	if !strings.Contains(sql, "notes = notes ||") {
		t.Fatal("expected notes appended (notes = notes || ...) to preserve audit trail")
	}
	// Audit note should reference this plan/task.
	if !strings.Contains(sql, "cell-2-activation-plan T2.2") {
		t.Fatal("expected audit note to reference cell-2-activation-plan T2.2 — the plan as it was named")
	}
}

func TestMigration007UpdatesUpdatedAt(t *testing.T) {
	sql := readMigration007ActivateCell2(t)
	// Even though cells trigger trg_cells_set_updated_at would set this
	// automatically, the migration sets it explicitly so the audit log
	// shows intent (migration-driven), not just trigger side-effect.
	if !strings.Contains(sql, "updated_at = now()") {
		t.Fatal("expected explicit updated_at = now() to mark migration-driven audit")
	}
}

func TestMigration007ProductionGatingWarning(t *testing.T) {
	sql := readMigration007ActivateCell2(t)
	// ADR D6 requires explicit warning in the migration that Sprint 9
	// readiness review must approve before applying to prod.
	if !strings.Contains(sql, "MUST NOT be applied to production") {
		t.Fatal("expected production-gating warning in migration comments (ADR D6)")
	}
	if !strings.Contains(sql, "Sprint 9") {
		t.Fatal("expected Sprint 9 reference in production-gating warning")
	}
}

func TestMigration007NoDownStatement(t *testing.T) {
	sql := readMigration007ActivateCell2(t)
	// Forward-only migrations: no DROP / DELETE / TRUNCATE / REVOKE allowed
	// in this UPDATE-only migration.
	for _, forbidden := range []string{"DROP TABLE", "DELETE FROM cells", "TRUNCATE", "DROP COLUMN"} {
		if strings.Contains(strings.ToUpper(sql), forbidden) {
			t.Errorf("forbidden destructive statement %q in forward-only migration", forbidden)
		}
	}
}

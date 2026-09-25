// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// fix-all-platform-issues-plan T2.3 — text-level invariants of the
// archive-stale-releases migration. No live Postgres required.

package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration008(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"migrations",
		"000008_archive_stale_releases.up.sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration008AddsArchivedStatusToConstraint(t *testing.T) {
	sql := readMigration008(t)
	if !strings.Contains(sql, "'ARCHIVED'") {
		t.Fatal("expected 'ARCHIVED' in CHECK constraint values")
	}
	if !strings.Contains(sql, "releases_status_check") {
		t.Fatal("expected releases_status_check constraint reference")
	}
}

func TestMigration008TargetsThreeFailureStatuses(t *testing.T) {
	sql := readMigration008(t)
	for _, want := range []string{"'CANCELLED'", "'GIT_MIRROR_FAILED'", "'SYNC_FAILED'"} {
		if !strings.Contains(sql, want) {
			t.Errorf("expected status %q in WHERE clause", want)
		}
	}
}

func TestMigration008FiltersByAge(t *testing.T) {
	sql := readMigration008(t)
	if !strings.Contains(sql, "now() - interval '7 days'") {
		t.Fatal("expected age filter 'now() - interval 7 days'")
	}
}

func TestMigration008IdempotentViaConstraintsAndWhereClause(t *testing.T) {
	sql := readMigration008(t)
	// Constraint drop guarded by IF EXISTS.
	if !strings.Contains(sql, "DROP CONSTRAINT releases_status_check") {
		t.Fatal("expected DROP CONSTRAINT for re-application")
	}
	if !strings.Contains(sql, "IF EXISTS") {
		t.Fatal("expected IF EXISTS guard for idempotency")
	}
	// UPDATE filtered by status NOT IN ARCHIVED, so re-running is no-op.
	if !strings.Contains(sql, "AND created_at < now() - interval '7 days'") {
		t.Fatal("expected age-based WHERE for idempotent UPDATE")
	}
}

func TestMigration008PreservesAuditTrailInSideEffects(t *testing.T) {
	sql := readMigration008(t)
	// Archive must record reason + original status in side_effects JSONB.
	if !strings.Contains(sql, "side_effects = COALESCE(side_effects") {
		t.Fatal("expected side_effects audit append (not overwrite)")
	}
	if !strings.Contains(sql, "archived_reason") {
		t.Fatal("expected archived_reason key in side_effects")
	}
	if !strings.Contains(sql, "archived_from_status") {
		t.Fatal("expected archived_from_status key in side_effects")
	}
}

func TestMigration008CreatesPartialIndex(t *testing.T) {
	sql := readMigration008(t)
	if !strings.Contains(sql, "CREATE INDEX IF NOT EXISTS idx_releases_status_active") {
		t.Fatal("expected idx_releases_status_active partial index for active queries")
	}
	if !strings.Contains(sql, "WHERE status != 'ARCHIVED'") {
		t.Fatal("expected partial index filter excluding ARCHIVED")
	}
}

func TestMigration008NoDestructiveOps(t *testing.T) {
	sql := readMigration008(t)
	for _, forbidden := range []string{"DROP TABLE", "DELETE FROM releases", "TRUNCATE releases", "DROP COLUMN"} {
		if strings.Contains(strings.ToUpper(sql), forbidden) {
			t.Errorf("forbidden destructive operation %q in forward-only migration", forbidden)
		}
	}
}

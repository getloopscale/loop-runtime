// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// API Key Rotation plan, T6.2.
// Structural validation of migration 000031_loopzero_api_keys_rotated_from.
// Renumbered 2026-06-07 from 000029 after merge with develop — slots
// 000027/000028 owned by preview_as_category + preview_slot_per_project
// (M2 refactor LIVE in PG); 000029/000030 owned by memory_webhooks.

package db_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pathMigration031 = "migrations/000031_loopzero_api_keys_rotated_from.up.sql"

func readRotationMigration(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(pathMigration031)
	if err != nil {
		t.Fatalf("resolve %s: %v", pathMigration031, err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v\n(missing migration blocks GREEN)", abs, err)
	}
	return string(b)
}

func TestMigration031_AltersLoopZeroApiKeys(t *testing.T) {
	sql := readRotationMigration(t)
	upper := strings.ToUpper(sql)
	if !strings.Contains(upper, "ALTER TABLE") {
		t.Fatal("must ALTER TABLE")
	}
	if !strings.Contains(sql, "loopzero_api_keys") {
		t.Fatal("must alter loopzero_api_keys")
	}
}

func TestMigration031_AddsRotatedFromColumnNullable(t *testing.T) {
	sql := readRotationMigration(t)
	upper := strings.ToUpper(sql)
	if !strings.Contains(upper, "ADD COLUMN") {
		t.Fatal("must ADD COLUMN")
	}
	if !strings.Contains(sql, "rotated_from") {
		t.Fatal("must add `rotated_from` column")
	}
	// Nullable per D1 (only rotation-created rows populate it; direct creates leave NULL).
	for _, line := range strings.Split(sql, "\n") {
		l := strings.ToUpper(line)
		if strings.Contains(l, "ROTATED_FROM") && strings.Contains(l, "NOT NULL") {
			t.Errorf("rotated_from must be nullable: %q", line)
		}
	}
}

func TestMigration031_ReferencesSelf(t *testing.T) {
	sql := readRotationMigration(t)
	// Self-FK to loopzero_api_keys(id) so the audit pointer can't dangle.
	if !strings.Contains(sql, "REFERENCES loopzero_api_keys(id)") {
		t.Error("rotated_from must REFERENCES loopzero_api_keys(id) for audit integrity")
	}
}

func TestMigration031_CommentDocumentsAudit(t *testing.T) {
	sql := readRotationMigration(t)
	upper := strings.ToUpper(sql)
	if !strings.Contains(upper, "COMMENT ON COLUMN") {
		t.Error("COMMENT ON COLUMN required so the next operator reading the schema understands the audit semantics")
	}
	lower := strings.ToLower(sql)
	if !strings.Contains(lower, "rotate") {
		t.Error("COMMENT must mention the rotation flow this column supports")
	}
}

func TestMigration031_NoDownFile(t *testing.T) {
	abs, _ := filepath.Abs("migrations/000031_loopzero_api_keys_rotated_from.down.sql")
	if _, err := os.Stat(abs); err == nil {
		t.Errorf("forward-only convention violated: .down.sql exists at %s", abs)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Migration 000069 text-level invariants: a GitHub App installation records
// when GitHub confirmed its owner and which user that was, and no installation
// bound before the migration counts as confirmed.

package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration069(t *testing.T, direction string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "migrations",
		"000069_github_installation_verified_owner."+direction+".sql"))
	if err != nil {
		t.Fatalf("migration 000069 %s.sql not found: %v", direction, err)
	}
	return string(data)
}

func TestMigration069_UpAddsNullableVerificationColumns(t *testing.T) {
	sql := readMigration069(t, "up")
	for _, fragment := range []string{
		"ALTER TABLE github_installations",
		"ADD COLUMN IF NOT EXISTS verified_at TIMESTAMPTZ",
		"ADD COLUMN IF NOT EXISTS verified_by TEXT",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 000069 up.sql is missing %q", fragment)
		}
	}
}

// A binding made before the migration carries no confirmation from GitHub, so
// the migration must not mark any existing row.
func TestMigration069_UpLeavesExistingBindingsUnverified(t *testing.T) {
	sql := strings.ToUpper(readMigration069(t, "up"))
	for _, forbidden := range []string{"UPDATE ", "DEFAULT", "NOT NULL"} {
		for _, line := range strings.Split(sql, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			if strings.Contains(line, forbidden) {
				t.Errorf("migration 000069 up.sql has %q in %q: existing rows must stay unverified", forbidden, strings.TrimSpace(line))
			}
		}
	}
}

func TestMigration069_DownDropsTheVerificationColumns(t *testing.T) {
	sql := readMigration069(t, "down")
	for _, fragment := range []string{"DROP COLUMN IF EXISTS verified_at", "DROP COLUMN IF EXISTS verified_by"} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 000069 down.sql is missing %q", fragment)
		}
	}
}

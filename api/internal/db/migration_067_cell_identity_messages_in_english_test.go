// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Migration 000067 regression test — text-level invariants.
//
// Migration 000061 wrote the cell trigger's error and the notes of the two
// unified cells in Portuguese. 000061 is applied history and stays as it is;
// 000067 replaces that text in English on every database, and its rollback
// restores exactly what 000061 left (#380).

package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration067(t *testing.T, direction string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "migrations",
		"000067_cell_identity_messages_in_english."+direction+".sql"))
	if err != nil {
		t.Fatalf("migration 000067 %s.sql not found: %v", direction, err)
	}
	return string(data)
}

// The trigger keeps failing loudly for an unresolved cell; only the language
// of the message changes.
func TestMigration067_UpRaisesTheUnresolvedCellInEnglish(t *testing.T) {
	sql := readMigration067(t, "up")
	for _, fragment := range []string{
		"CREATE OR REPLACE FUNCTION populate_cell_id_from_tenant() RETURNS trigger",
		"SELECT cell_id INTO NEW.cell_id FROM tenants WHERE id = NEW.tenant_id;",
		"'cell_id not resolved for tenant % in table %: the tenant does not exist or has no cell'",
		"NEW.tenant_id, TG_TABLE_NAME;",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 000067 up.sql is missing %q", fragment)
		}
	}
}

// An operator who already edited a cell's notes keeps the edit: the rewrite
// only replaces the exact text 000061 wrote.
func TestMigration067_UpRewritesOnlyTheNotesMigration061Wrote(t *testing.T) {
	sql := readMigration067(t, "up")
	for _, cell := range []string{"cell-0", "cell-1"} {
		guard := "WHERE cell_id = '" + cell + "'\n   AND notes = "
		if !strings.Contains(sql, guard) {
			t.Errorf("migration 000067 up.sql must rewrite %s's notes only where they still hold the 000061 text (want %q)", cell, guard)
		}
	}
}

// The rollback is real: it returns the message 000061 defined.
func TestMigration067_DownRestoresTheMigration061Message(t *testing.T) {
	sql := readMigration067(t, "down")
	if !strings.Contains(sql, "CREATE OR REPLACE FUNCTION populate_cell_id_from_tenant() RETURNS trigger") {
		t.Error("migration 000067 down.sql must redefine the trigger function")
	}
	if strings.Contains(sql, "cell_id not resolved") {
		t.Error("migration 000067 down.sql still carries the English message; a rollback returns 000061's text")
	}
}

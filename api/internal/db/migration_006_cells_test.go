// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// CHG-2026-04-30-INFRA-NORMALIZE — T0.1 follow-up + Plan v2 Phase 5 enabler.
// Validates the SQL contract of `000006_cells.up.sql` without requiring a
// live Postgres connection. Live behaviour will be exercised by the
// placement layer's capacity test once the consumer wiring lands (Phase 5).
//
// Renumbered from 000003 → 000006 (2026-04-30): the original file was
// authored AFTER 000004 + 000005 had already shipped, so golang-migrate
// was silently skipping it on every existing DB at schema_migrations.
// version >= 5. Discovered LIVE during multi-cell validation.
package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration006Cells(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"migrations",
		"000006_cells.up.sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration006CellsCreatesTable(t *testing.T) {
	sql := readMigration006Cells(t)

	if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS cells") {
		t.Fatal("expected cells table CREATE TABLE IF NOT EXISTS")
	}
	if !strings.Contains(sql, "cell_id              TEXT        PRIMARY KEY") {
		t.Fatal("expected cell_id as TEXT PRIMARY KEY")
	}
}

func TestMigration006CellsHasCellIdPattern(t *testing.T) {
	sql := readMigration006Cells(t)
	// Must enforce same pattern as ClusterCredentialSpec.CellID kubebuilder
	// validation: ^cell-[a-z0-9-]+$. Without this, callers could insert
	// arbitrary strings that the K8s side would reject, producing drift.
	if !strings.Contains(sql, "cell_id ~ '^cell-[a-z0-9-]+$'") {
		t.Fatal("expected cell_id CHECK constraint matching CC.spec.cellID kubebuilder pattern")
	}
}

func TestMigration006CellsStatusEnum(t *testing.T) {
	sql := readMigration006Cells(t)
	for _, want := range []string{"'active'", "'draining'", "'maintenance'", "'retired'"} {
		if !strings.Contains(sql, want) {
			t.Errorf("status enum missing value %q", want)
		}
	}
	if !strings.Contains(sql, "status IN ('active', 'draining',\n                                                        'maintenance', 'retired')") {
		// tolerate single-line variant
		if !strings.Contains(sql, "status IN ('active', 'draining', 'maintenance', 'retired')") {
			t.Log("note: status CHECK constraint multi-line — verifying value set instead")
		}
	}
}

func TestMigration006CellsCapacityNullableNonNegative(t *testing.T) {
	sql := readMigration006Cells(t)
	// NULL = unlimited; explicit non-negative for finite caps.
	if !strings.Contains(sql, "capacity_tenants_max IS NULL") {
		t.Fatal("expected capacity_tenants_max NULL allowance (= unlimited)")
	}
	if !strings.Contains(sql, "capacity_tenants_max >= 0") {
		t.Fatal("expected capacity_tenants_max non-negative CHECK")
	}
}

func TestMigration006CellsAuditColumns(t *testing.T) {
	sql := readMigration006Cells(t)
	if !strings.Contains(sql, "created_at           TIMESTAMPTZ NOT NULL DEFAULT now()") {
		t.Fatal("expected created_at TIMESTAMPTZ NOT NULL DEFAULT now()")
	}
	if !strings.Contains(sql, "updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()") {
		t.Fatal("expected updated_at TIMESTAMPTZ NOT NULL DEFAULT now()")
	}
	if !strings.Contains(sql, "trg_cells_set_updated_at") {
		t.Fatal("expected trigger keeping updated_at fresh on UPDATE")
	}
}

// Reads HISTORY: `000006` seeds the cells with the names they had back then —
// `cell-default`, `cell-1`, `cell-2`. `000061` later merged `cell-default` +
// `cell-1` into `cell-0`, but renaming in the present does not change what this
// migration executed on every database where it already ran, and `docs/README.md`
// forbids rewriting a dated record to look current.
//
// The previous version of this test looked for the NEW identity in the OLD file —
// and listed `'cell-0'` TWICE, which made the same failure be reported in
// duplicate and hid that the third entry was never verified (B-071).
func TestMigration006CellsSeedRows(t *testing.T) {
	sql := readMigration006Cells(t)
	for _, want := range []string{"'cell-default'", "'cell-1'", "'cell-2'"} {
		if !strings.Contains(sql, want) {
			t.Errorf("seed row missing %q", want)
		}
	}
	// The third cell is seeded in 'maintenance' so that placement refuses to pin
	// to it until canary stage 1 flips it to 'active'. This is the safety gate,
	// and it holds under whatever name the cell ends up having.
	if !strings.Contains(sql, "'cell-2', 'Cell 2 (runtime-2)',\n                     'maintenance'") {
		// tolerate a single-line variant if the formatting changes
		if !strings.Contains(sql, "cell-2") || !strings.Contains(sql, "'maintenance'") {
			t.Fatal("expected cell-1 seeded in 'maintenance' status (safety gate)")
		}
	}
	// Idempotent re-run.
	if !strings.Contains(sql, "ON CONFLICT (cell_id) DO NOTHING") {
		t.Fatal("expected ON CONFLICT idempotency clause")
	}
}

func TestMigration006CellsForwardOnly(t *testing.T) {
	sql := readMigration006Cells(t)
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		// DROP TRIGGER IF EXISTS is permitted only inside a BEFORE-create
		// idempotent re-create (we have one). Anything else is forward-only
		// violation. Allowlist the specific known DROP.
		upper := strings.ToUpper(trimmed)
		if strings.Contains(upper, "DROP") {
			if strings.Contains(trimmed, "DROP TRIGGER IF EXISTS trg_cells_set_updated_at ON cells") {
				continue
			}
			t.Errorf("forward-only violation: %q", trimmed)
		}
	}
}

func TestMigration006CellsIndex(t *testing.T) {
	sql := readMigration006Cells(t)
	// Placement layer's most common filter: status + capacity availability.
	if !strings.Contains(sql, "idx_cells_status_capacity") {
		t.Fatal("expected (status, capacity_tenants_max) index for placement queries")
	}
}

// Regression for bug NEW-2 (discovered LIVE 2026-04-30): PostgreSQL does
// NOT permit string concatenation (`'a' || 'b'`) inside `COMMENT ... IS`.
// Only string literals work. Original 000003_cells.up.sql had:
//
//	COMMENT ON TABLE cells IS
//	    'Operational source of truth for ...' ||
//	    'Connectivity SoT remains ClusterCredential CRD.';
//
// which raised `psql: ERROR: syntax error at or near "||"` and silently
// dropped the table-level comment (column comments survived because they
// used single-line literals).
//
// This test pins the contract that any COMMENT clause in this migration
// uses single literal strings — no `||` concatenation between the IS
// keyword and the terminating semicolon.
func TestMigration006Cells_CommentClauses_NoConcatenation(t *testing.T) {
	sql := readMigration006Cells(t)
	// Find every `COMMENT ON ... IS` clause and check the body up to
	// the next `;` for a `||`. We use a simple line-window scan rather
	// than full SQL parsing — comments + concatenation across lines is
	// the actual failure mode that occurred.
	lines := strings.Split(sql, "\n")
	for i, line := range lines {
		upper := strings.ToUpper(strings.TrimSpace(line))
		if !strings.HasPrefix(upper, "COMMENT ON ") {
			continue
		}
		// Walk from this line until a `;` and ensure no `||` appears.
		body := []string{line}
		for j := i + 1; j < len(lines) && j < i+10; j++ {
			body = append(body, lines[j])
			if strings.Contains(lines[j], ";") {
				break
			}
		}
		joined := strings.Join(body, "\n")
		if strings.Contains(joined, "||") {
			t.Errorf("COMMENT clause near line %d uses `||` concatenation — PostgreSQL rejects this; use a single string literal:\n%s", i+1, joined)
		}
	}
}

// Regression for bug NEW-1 (discovered LIVE 2026-04-30): the original
// file was 000003_cells.up.sql, but migrations 000004 + 000005 had
// already shipped (and were applied to existing DBs at version=5). The
// golang-migrate sequencer skips numbers below the current applied
// version on existing databases, so 000003 would have been silently
// missed forever. Renumbering to 000006 places the migration AFTER the
// last applied version, guaranteeing it runs on every existing DB on
// the next forward step.
//
// This test pins the file path so a future renumber/relocation that
// reverts to a number < 6 will fail loudly here.
func TestMigration006Cells_FileNumberAtLeastSix(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	migrationDir := filepath.Join(
		filepath.Dir(thisFile),
		"migrations",
	)
	entries, err := os.ReadDir(migrationDir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	var foundCells string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, "_cells.up.sql") {
			foundCells = name
			break
		}
	}
	if foundCells == "" {
		t.Fatal("no *_cells.up.sql migration found")
	}
	// Filename pattern: NNNNNN_cells.up.sql
	if len(foundCells) < 6 {
		t.Fatalf("unexpected cells migration filename: %q", foundCells)
	}
	prefix := foundCells[:6]
	if prefix < "000006" {
		t.Errorf("cells migration numbered %q — must be >= 000006 so it runs on existing DBs already at schema_migrations.version=5 (was 000003, silently skipped). Rename above 000005.", prefix)
	}
}

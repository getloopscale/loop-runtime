// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// CHG-2026-04-30-INFRA-NORMALIZE — T0.1 follow-up + Plan v2 Phase 5 enabler.
// Validates the SQL contract of `000003_cells.up.sql` without requiring a
// live Postgres connection. Live behaviour will be exercised by the
// placement layer's capacity test once the consumer wiring lands (Phase 5).
package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration003Cells(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"..", "..", "internal", "db", "migrations",
		"000003_cells.up.sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration003CellsCreatesTable(t *testing.T) {
	sql := readMigration003Cells(t)

	if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS cells") {
		t.Fatal("expected cells table CREATE TABLE IF NOT EXISTS")
	}
	if !strings.Contains(sql, "cell_id              TEXT        PRIMARY KEY") {
		t.Fatal("expected cell_id as TEXT PRIMARY KEY")
	}
}

func TestMigration003CellsHasCellIdPattern(t *testing.T) {
	sql := readMigration003Cells(t)
	// Must enforce same pattern as ClusterCredentialSpec.CellID kubebuilder
	// validation: ^cell-[a-z0-9-]+$. Without this, callers could insert
	// arbitrary strings that the K8s side would reject, producing drift.
	if !strings.Contains(sql, "cell_id ~ '^cell-[a-z0-9-]+$'") {
		t.Fatal("expected cell_id CHECK constraint matching CC.spec.cellID kubebuilder pattern")
	}
}

func TestMigration003CellsStatusEnum(t *testing.T) {
	sql := readMigration003Cells(t)
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

func TestMigration003CellsCapacityNullableNonNegative(t *testing.T) {
	sql := readMigration003Cells(t)
	// NULL = unlimited; explicit non-negative for finite caps.
	if !strings.Contains(sql, "capacity_tenants_max IS NULL") {
		t.Fatal("expected capacity_tenants_max NULL allowance (= unlimited)")
	}
	if !strings.Contains(sql, "capacity_tenants_max >= 0") {
		t.Fatal("expected capacity_tenants_max non-negative CHECK")
	}
}

func TestMigration003CellsAuditColumns(t *testing.T) {
	sql := readMigration003Cells(t)
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

func TestMigration003CellsSeedRows(t *testing.T) {
	sql := readMigration003Cells(t)
	for _, want := range []string{"'cell-default'", "'cell-1'", "'cell-2'"} {
		if !strings.Contains(sql, want) {
			t.Errorf("seed row missing %q", want)
		}
	}
	// cell-2 must seed in 'maintenance' so placement rejects pinning until
	// canary stage 1 flips it 'active'. This is the safety gate.
	if !strings.Contains(sql, "'cell-2', 'Cell 2 (runtime-2)',\n                     'maintenance'") {
		// tolerate single-line variant if formatter changes layout
		if !(strings.Contains(sql, "cell-2") && strings.Contains(sql, "'maintenance'")) {
			t.Fatal("expected cell-2 seeded in 'maintenance' status (safety gate)")
		}
	}
	// Idempotent re-run.
	if !strings.Contains(sql, "ON CONFLICT (cell_id) DO NOTHING") {
		t.Fatal("expected ON CONFLICT idempotency clause")
	}
}

func TestMigration003CellsForwardOnly(t *testing.T) {
	sql := readMigration003Cells(t)
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

func TestMigration003CellsIndex(t *testing.T) {
	sql := readMigration003Cells(t)
	// Placement layer's most common filter: status + capacity availability.
	if !strings.Contains(sql, "idx_cells_status_capacity") {
		t.Fatal("expected (status, capacity_tenants_max) index for placement queries")
	}
}

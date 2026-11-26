// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Sprint 6 / T0.1 — Per-app attestation sidecar table tests.
// Validates the SQL contract of the forward-only migration without
// requiring a live Postgres connection. Live behavior is exercised by
// the unit tests in api/internal/db/app_attestation_store_test.go and by
// the E2E supply-chain test (T4.1).
package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration005(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(
		filepath.Dir(thisFile),
		"migrations",
		"000005_release_app_attestations.up.sql",
	)
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration005Tables(t *testing.T) {
	sql := readMigration005(t)

	t.Run("creates release_app_attestations table", func(t *testing.T) {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS release_app_attestations") {
			t.Fatal("expected release_app_attestations table")
		}
	})

	t.Run("foreign key to releases with cascade", func(t *testing.T) {
		if !strings.Contains(sql, "REFERENCES releases(id) ON DELETE CASCADE") {
			t.Fatal("expected ON DELETE CASCADE FK to releases")
		}
	})
}

func TestMigration005Constraints(t *testing.T) {
	sql := readMigration005(t)

	t.Run("id format CHECK enforces raa- prefix", func(t *testing.T) {
		if !strings.Contains(sql, "CHECK (id LIKE 'raa-%')") {
			t.Fatal("expected CHECK (id LIKE 'raa-%') constraint")
		}
	})

	t.Run("digest format CHECK enforces sha256: prefix", func(t *testing.T) {
		if !strings.Contains(sql, "CHECK (image_digest LIKE 'sha256:%')") {
			t.Fatal("expected CHECK on image_digest sha256: prefix")
		}
	})

	t.Run("sbom_format restricted to known values", func(t *testing.T) {
		if !strings.Contains(sql, "'cyclonedx-json'") || !strings.Contains(sql, "'spdx-json'") {
			t.Fatal("expected sbom_format CHECK with cyclonedx-json/spdx-json")
		}
	})

	t.Run("unique key on (release_id, app_name)", func(t *testing.T) {
		if !strings.Contains(sql, "UNIQUE (release_id, app_name)") {
			t.Fatal("expected UNIQUE (release_id, app_name)")
		}
	})

	t.Run("non-negative SBOM component count", func(t *testing.T) {
		if !strings.Contains(sql, "sbom_component_count >= 0") {
			t.Fatal("expected non-negative sbom_component_count CHECK")
		}
	})
}

func TestMigration005RLS(t *testing.T) {
	sql := readMigration005(t)

	t.Run("ENABLE ROW LEVEL SECURITY", func(t *testing.T) {
		if !strings.Contains(sql, "ALTER TABLE release_app_attestations ENABLE ROW LEVEL SECURITY") {
			t.Fatal("expected ENABLE ROW LEVEL SECURITY")
		}
	})

	t.Run("CREATE POLICY tenant_isolation", func(t *testing.T) {
		if !strings.Contains(sql, "CREATE POLICY release_app_attestations_tenant_isolation") {
			t.Fatal("expected RLS tenant_isolation policy")
		}
	})

	t.Run("RLS uses app.tenant_id GUC", func(t *testing.T) {
		if !strings.Contains(sql, "current_setting('app.tenant_id'") {
			t.Fatal("expected RLS to read app.tenant_id GUC")
		}
	})
}

func TestMigration005Triggers(t *testing.T) {
	sql := readMigration005(t)

	t.Run("populate_cell_id_from_tenant trigger present", func(t *testing.T) {
		if !strings.Contains(sql, "trg_release_app_attestations_populate_cell_id") {
			t.Fatal("expected populate_cell_id trigger")
		}
		if !strings.Contains(sql, "EXECUTE FUNCTION populate_cell_id_from_tenant()") {
			t.Fatal("expected EXECUTE FUNCTION populate_cell_id_from_tenant()")
		}
	})

	t.Run("updated_at trigger present", func(t *testing.T) {
		if !strings.Contains(sql, "trg_release_app_attestations_updated_at") {
			t.Fatal("expected updated_at trigger")
		}
	})
}

func TestMigration005Indexes(t *testing.T) {
	sql := readMigration005(t)

	t.Run("index on release_id", func(t *testing.T) {
		if !strings.Contains(sql, "idx_release_app_attestations_release") {
			t.Fatal("expected index on release_id")
		}
	})

	t.Run("index on tenant_id, created_at", func(t *testing.T) {
		if !strings.Contains(sql, "idx_release_app_attestations_tenant") {
			t.Fatal("expected index on tenant_id, created_at")
		}
	})

	t.Run("partial index on signed_at IS NOT NULL", func(t *testing.T) {
		if !strings.Contains(sql, "idx_release_app_attestations_signed") {
			t.Fatal("expected partial index on signed_at")
		}
		if !strings.Contains(sql, "WHERE signed_at IS NOT NULL") {
			t.Fatal("expected partial index WHERE signed_at IS NOT NULL")
		}
	})
}

func TestMigration005Idempotent(t *testing.T) {
	sql := readMigration005(t)

	t.Run("table creation uses IF NOT EXISTS", func(t *testing.T) {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS release_app_attestations") {
			t.Fatal("expected idempotent CREATE TABLE")
		}
	})

	t.Run("policy creation uses DROP POLICY IF EXISTS", func(t *testing.T) {
		if !strings.Contains(sql, "DROP POLICY IF EXISTS release_app_attestations_tenant_isolation") {
			t.Fatal("expected idempotent DROP POLICY IF EXISTS")
		}
	})

	t.Run("triggers use DROP TRIGGER IF EXISTS", func(t *testing.T) {
		if !strings.Contains(sql, "DROP TRIGGER IF EXISTS trg_release_app_attestations_populate_cell_id") {
			t.Fatal("expected idempotent DROP TRIGGER for populate_cell_id")
		}
		if !strings.Contains(sql, "DROP TRIGGER IF EXISTS trg_release_app_attestations_updated_at") {
			t.Fatal("expected idempotent DROP TRIGGER for updated_at")
		}
	})

	t.Run("indexes use IF NOT EXISTS", func(t *testing.T) {
		if !strings.Contains(sql, "CREATE INDEX IF NOT EXISTS") {
			t.Fatal("expected indexes with IF NOT EXISTS")
		}
	})
}

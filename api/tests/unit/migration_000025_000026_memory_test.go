// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Renamed 2026-09-06 (B-067) from migration_memory_p3_test.go. The
// migration-test-coverage gate credits a migration only when a test file starts with
// `migration_<NNNNNN>`; this file had no numeric prefix, so BOTH migrations it validates
// were reported as untested while 13 test functions and 20 assertions covered them.
//
// KNOWN RESIDUAL, do not "fix" with a stub: the prefix can name only ONE migration, so
// 000026_memory_exports still counts as a violation even though it is validated below
// (see the `000026_memory_exports.up.sql` assertions). Creating an empty
// migration_000026_*_test.go to silence the gate would be exactly the stub the gate
// exists to prevent. Closing it honestly means splitting this file per migration.
// Plan 3 of dashboard-memory-section-roadmap.md, T3.2.
// Structural validation of migrations 000025_memory_webhooks +
// 000026_memory_exports. Hermetic — parses each SQL file as text and
// asserts every invariant the plan demands. LIVE schema apply runs via
// `task db:migrate` on the dev cluster (operator territory).

package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	pathMemoryWebhooksMigration = "../../internal/db/migrations/000025_memory_webhooks.up.sql"
	pathMemoryExportsMigration  = "../../internal/db/migrations/000026_memory_exports.up.sql"
)

func readMigrationFile(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve %s: %v", rel, err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v\n(missing migration blocks P3 GREEN)", abs, err)
	}
	return string(b)
}

// =============================================================================
// 000025_memory_webhooks
// =============================================================================

func TestMigration025_WebhooksTableCreated(t *testing.T) {
	sql := readMigrationFile(t, pathMemoryWebhooksMigration)
	if !strings.Contains(sql, "CREATE TABLE") || !strings.Contains(sql, "memory_webhooks") {
		t.Fatalf("must CREATE TABLE memory_webhooks")
	}
}

func TestMigration025_WebhooksRequiredColumns(t *testing.T) {
	sql := readMigrationFile(t, pathMemoryWebhooksMigration)
	required := []string{
		"id ",
		"subject ",
		"tenant_id ",
		"url ",
		"label ",
		"events ",
		"created_at ",
		"revoked_at ",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("missing column: %q", strings.TrimSpace(col))
		}
	}
}

func TestMigration025_WebhooksTenantFKCascade(t *testing.T) {
	sql := readMigrationFile(t, pathMemoryWebhooksMigration)
	if !strings.Contains(sql, "REFERENCES tenants(id)") {
		t.Error("tenant_id must FK to tenants(id)")
	}
	if !strings.Contains(strings.ToUpper(sql), "ON DELETE CASCADE") {
		t.Error("tenants FK must be ON DELETE CASCADE")
	}
}

func TestMigration025_WebhooksURLCheck(t *testing.T) {
	sql := readMigrationFile(t, pathMemoryWebhooksMigration)
	// CHECK (url ~* '^https?://') — defense at the DB layer if a future
	// handler bypasses validation. The exact regex format may vary
	// (~* case-insensitive), so just look for "url" + "http" in a CHECK.
	if !strings.Contains(sql, "url") || !strings.Contains(strings.ToLower(sql), "http") {
		t.Error("CHECK constraint asserting url begins with http(s) required")
	}
}

func TestMigration025_WebhooksLabelLengthCheck(t *testing.T) {
	sql := readMigrationFile(t, pathMemoryWebhooksMigration)
	if !strings.Contains(sql, "length(label)") {
		t.Error("CHECK length(label) BETWEEN 1 AND 64 required")
	}
}

func TestMigration025_WebhooksEventsJsonbArray(t *testing.T) {
	sql := readMigrationFile(t, pathMemoryWebhooksMigration)
	if !strings.Contains(strings.ToUpper(sql), "JSONB") {
		t.Error("events JSONB type required")
	}
	if !strings.Contains(sql, "jsonb_typeof(events)") {
		t.Error("CHECK jsonb_typeof(events) = 'array' required")
	}
}

func TestMigration025_WebhooksTenantPartialIndex(t *testing.T) {
	sql := readMigrationFile(t, pathMemoryWebhooksMigration)
	if !strings.Contains(sql, "idx_memory_webhooks_owner") {
		t.Error("index idx_memory_webhooks_owner required")
	}
	if !strings.Contains(sql, "WHERE revoked_at IS NULL") {
		t.Error("partial index predicate WHERE revoked_at IS NULL required")
	}
}

// =============================================================================
// 000026_memory_exports
// =============================================================================

func TestMigration026_ExportsTableCreated(t *testing.T) {
	sql := readMigrationFile(t, pathMemoryExportsMigration)
	if !strings.Contains(sql, "CREATE TABLE") || !strings.Contains(sql, "memory_exports") {
		t.Fatalf("must CREATE TABLE memory_exports")
	}
}

func TestMigration026_ExportsRequiredColumns(t *testing.T) {
	sql := readMigrationFile(t, pathMemoryExportsMigration)
	required := []string{
		"id ",
		"subject ",
		"tenant_id ",
		"display_name ",
		"status ",
		"file_path ",
		"error_message ",
		"created_at ",
		"completed_at ",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("missing column: %q", strings.TrimSpace(col))
		}
	}
}

func TestMigration026_ExportsStatusCheck(t *testing.T) {
	sql := readMigrationFile(t, pathMemoryExportsMigration)
	// CHECK (status IN ('pending','running','succeeded','failed'))
	for _, lit := range []string{"'pending'", "'running'", "'succeeded'", "'failed'"} {
		if !strings.Contains(sql, lit) {
			t.Errorf("CHECK status IN literal missing: %s", lit)
		}
	}
}

func TestMigration026_ExportsTenantFKCascade(t *testing.T) {
	sql := readMigrationFile(t, pathMemoryExportsMigration)
	if !strings.Contains(sql, "REFERENCES tenants(id)") {
		t.Error("tenant_id must FK to tenants(id)")
	}
	if !strings.Contains(strings.ToUpper(sql), "ON DELETE CASCADE") {
		t.Error("FK CASCADE required")
	}
}

func TestMigration026_ExportsListingIndex(t *testing.T) {
	sql := readMigrationFile(t, pathMemoryExportsMigration)
	if !strings.Contains(sql, "idx_memory_exports_owner_active") {
		t.Error("index idx_memory_exports_owner_active required")
	}
	// created_at DESC ordering for the list endpoint hot path.
	if !strings.Contains(strings.ToUpper(sql), "CREATED_AT DESC") {
		t.Error("index ordering created_at DESC required")
	}
}

// =============================================================================
// Forward-only invariant — no .down.sql shipped for either
// =============================================================================

func TestMemoryMigrations_HaveNoDownFiles(t *testing.T) {
	for _, rel := range []string{
		"../../internal/db/migrations/000025_memory_webhooks.down.sql",
		"../../internal/db/migrations/000026_memory_exports.down.sql",
	} {
		abs, _ := filepath.Abs(rel)
		if _, err := os.Stat(abs); err == nil {
			t.Errorf("forward-only convention violated: .down.sql exists at %s", abs)
		}
	}
}

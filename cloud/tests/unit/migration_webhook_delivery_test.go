// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Memory Webhook Delivery plan, T5.2 + T5.3.
// Structural validation of migrations 000027 (ADD COLUMN secret) +
// 000028 (NEW memory_webhook_deliveries table).

package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	pathMigration027 = "../../../api/internal/db/migrations/000027_memory_webhooks_add_secret.up.sql"
	pathMigration028 = "../../../api/internal/db/migrations/000028_memory_webhook_deliveries.up.sql"
)

func readWebhookDeliveryMigration(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve %s: %v", rel, err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v\n(missing migration blocks GREEN)", abs, err)
	}
	return string(b)
}

// =============================================================================
// 000027 — ALTER memory_webhooks ADD COLUMN secret
// =============================================================================

func TestMigration027_AltersMemoryWebhooks(t *testing.T) {
	sql := readWebhookDeliveryMigration(t, pathMigration027)
	upper := strings.ToUpper(sql)
	if !strings.Contains(upper, "ALTER TABLE") {
		t.Fatal("must ALTER TABLE")
	}
	if !strings.Contains(sql, "memory_webhooks") {
		t.Fatal("must alter memory_webhooks table")
	}
}

func TestMigration027_AddsSecretColumnNullable(t *testing.T) {
	sql := readWebhookDeliveryMigration(t, pathMigration027)
	upper := strings.ToUpper(sql)
	if !strings.Contains(upper, "ADD COLUMN") {
		t.Fatal("must ADD COLUMN")
	}
	if !strings.Contains(sql, "secret") {
		t.Fatal("must add `secret` column")
	}
	// Column intentionally nullable per Plan D1 consequences (pre-plan webhooks
	// stay NULL; dispatcher logs warn + skips signing). Migration MUST NOT
	// include NOT NULL on the new column.
	// Heuristic: secret declaration line should not be followed immediately by
	// "NOT NULL" on the same line.
	for _, line := range strings.Split(sql, "\n") {
		l := strings.ToUpper(line)
		if strings.Contains(l, "SECRET") && strings.Contains(l, "NOT NULL") {
			t.Errorf("secret column must be nullable (D1 consequence): %q", line)
		}
	}
}

func TestMigration027_CommentDocumentsV01Disclosure(t *testing.T) {
	sql := readWebhookDeliveryMigration(t, pathMigration027)
	upper := strings.ToUpper(sql)
	if !strings.Contains(upper, "COMMENT ON COLUMN") {
		t.Error("COMMENT ON COLUMN required for operator documentation")
	}
	// Comment should mention v0.1-era webhook + NULL behavior so an operator
	// reading the schema understands the signing semantics.
	lower := strings.ToLower(sql)
	if !strings.Contains(lower, "null") {
		t.Error("COMMENT must reference NULL behavior for pre-plan webhooks")
	}
	if !strings.Contains(lower, "sign") {
		t.Error("COMMENT must reference signing semantics")
	}
}

// =============================================================================
// 000028 — NEW memory_webhook_deliveries
// =============================================================================

func TestMigration028_TableCreated(t *testing.T) {
	sql := readWebhookDeliveryMigration(t, pathMigration028)
	if !strings.Contains(sql, "CREATE TABLE") || !strings.Contains(sql, "memory_webhook_deliveries") {
		t.Fatal("must CREATE TABLE memory_webhook_deliveries")
	}
}

func TestMigration028_RequiredColumns(t *testing.T) {
	sql := readWebhookDeliveryMigration(t, pathMigration028)
	required := []string{
		"id ",
		"webhook_id ",
		"event_type ",
		"payload ",
		"status ",
		"attempt_count ",
		"next_attempt_at ",
		"last_response_status ",
		"last_response_body ",
		"last_error_message ",
		"created_at ",
		"completed_at ",
	}
	for _, col := range required {
		if !strings.Contains(sql, col) {
			t.Errorf("missing column declaration: %q", strings.TrimSpace(col))
		}
	}
}

func TestMigration028_WebhookFKCascade(t *testing.T) {
	sql := readWebhookDeliveryMigration(t, pathMigration028)
	if !strings.Contains(sql, "REFERENCES memory_webhooks(id)") {
		t.Error("webhook_id must FK to memory_webhooks(id)")
	}
	if !strings.Contains(strings.ToUpper(sql), "ON DELETE CASCADE") {
		t.Error("FK CASCADE required so webhook revocation cleans up deliveries")
	}
}

func TestMigration028_StatusCheckAll5Literals(t *testing.T) {
	sql := readWebhookDeliveryMigration(t, pathMigration028)
	// All 5 status literals MUST be in the CHECK clause exactly.
	for _, lit := range []string{"'pending'", "'delivering'", "'delivered'", "'failed'", "'dead_letter'"} {
		if !strings.Contains(sql, lit) {
			t.Errorf("CHECK status IN missing literal: %s", lit)
		}
	}
}

func TestMigration028_PayloadJsonbType(t *testing.T) {
	sql := readWebhookDeliveryMigration(t, pathMigration028)
	if !strings.Contains(strings.ToUpper(sql), "JSONB") {
		t.Error("payload JSONB type required for indexed semantic querying")
	}
}

func TestMigration028_DispatcherHotPathPartialIndex(t *testing.T) {
	sql := readWebhookDeliveryMigration(t, pathMigration028)
	// Hot path = `WHERE next_attempt_at <= NOW() AND status IN ('pending','failed')`.
	// Partial index on `(next_attempt_at)` with that predicate keeps it tight.
	if !strings.Contains(sql, "idx_memory_webhook_deliveries_due") {
		t.Error("dispatcher hot-path partial index required")
	}
	// Predicate filter: must reference the 2 statuses dispatched.
	upper := strings.ToUpper(sql)
	if !strings.Contains(upper, "WHERE STATUS") {
		t.Error("partial index WHERE status IN ('pending','failed') predicate required")
	}
}

func TestMigration028_WebhookHistoryIndex(t *testing.T) {
	sql := readWebhookDeliveryMigration(t, pathMigration028)
	if !strings.Contains(sql, "idx_memory_webhook_deliveries_webhook") {
		t.Error("webhook history index required for dashboard list")
	}
	// Ordering DESC so the dashboard hits the most-recent end of the BTree.
	if !strings.Contains(strings.ToUpper(sql), "CREATED_AT DESC") {
		t.Error("history index ordering created_at DESC required")
	}
}

// Forward-only invariant — no .down.sql for either migration.
func TestMigrations_WebhookDelivery_NoDownFiles(t *testing.T) {
	for _, rel := range []string{
		"../../../api/internal/db/migrations/000027_memory_webhooks_add_secret.down.sql",
		"../../../api/internal/db/migrations/000028_memory_webhook_deliveries.down.sql",
	} {
		abs, _ := filepath.Abs(rel)
		if _, err := os.Stat(abs); err == nil {
			t.Errorf("forward-only convention violated: .down.sql exists at %s", abs)
		}
	}
}

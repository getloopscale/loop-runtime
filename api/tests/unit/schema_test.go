// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"strings"
	"testing"
)

func TestMigrationSQL(t *testing.T) {
	migrationPath := "../../internal/db/migrations/000001_initial_schema.up.sql"
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("failed to read migration file: %v", err)
	}

	sql := string(content)

	t.Run("all 6 tables are defined", func(t *testing.T) {
		tables := []string{
			`CREATE TABLE IF NOT EXISTS "builds"`,
			`CREATE TABLE IF NOT EXISTS "releases"`,
			`CREATE TABLE IF NOT EXISTS "environment_targets"`,
			`CREATE TABLE IF NOT EXISTS "audit_log"`,
			`CREATE TABLE IF NOT EXISTS "idempotency_keys"`,
			`CREATE TABLE IF NOT EXISTS "sse_events"`,
		}
		for _, table := range tables {
			if !strings.Contains(sql, table) {
				t.Errorf("expected migration to contain %q", table)
			}
		}
	})

	t.Run("migration is idempotent (uses IF NOT EXISTS)", func(t *testing.T) {
		// All CREATE TABLE must use IF NOT EXISTS for task setup re-runnability
		tableCount := strings.Count(sql, "CREATE TABLE")
		ifNotExistsCount := strings.Count(sql, "CREATE TABLE IF NOT EXISTS")
		if tableCount != ifNotExistsCount {
			t.Errorf("found %d CREATE TABLE but only %d with IF NOT EXISTS — migration is not idempotent", tableCount, ifNotExistsCount)
		}
		// All CREATE INDEX must use IF NOT EXISTS
		indexCount := strings.Count(sql, "CREATE INDEX")
		indexIfNotExistsCount := strings.Count(sql, "CREATE INDEX IF NOT EXISTS")
		if indexCount != indexIfNotExistsCount {
			t.Errorf("found %d CREATE INDEX but only %d with IF NOT EXISTS — migration is not idempotent", indexCount, indexIfNotExistsCount)
		}
	})

	t.Run("builds table has required columns", func(t *testing.T) {
		columns := []string{
			`"id" text PRIMARY KEY`,
			`"tenant_id" text NOT NULL`,
			`"project_id" text NOT NULL`,
			`"environment" text NOT NULL`,
			`"status" text NOT NULL`,
			`"correlation_id" text NOT NULL`,
			`"created_at" timestamp with time zone`,
			`"created_by" text NOT NULL`,
			`"started_at" timestamp with time zone`,
			`"finished_at" timestamp with time zone`,
			`"error_message" text`,
			`"duration_seconds" integer`,
			`"workflow_id" text`,
		}
		for _, col := range columns {
			if !strings.Contains(sql, col) {
				t.Errorf("builds table missing column: %s", col)
			}
		}
	})

	t.Run("releases table has foreign key to builds", func(t *testing.T) {
		if !strings.Contains(sql, `REFERENCES "builds"("id")`) {
			t.Error("expected releases.build_id FK to builds.id")
		}
	})

	t.Run("environment_targets has composite primary key", func(t *testing.T) {
		if !strings.Contains(sql, `PRIMARY KEY ("tenant_id", "project_id", "environment")`) {
			t.Error("expected composite PK on environment_targets")
		}
	})

	t.Run("environment_targets has foreign keys to releases", func(t *testing.T) {
		if !strings.Contains(sql, `"desired_release_id"`) {
			t.Error("expected desired_release_id column")
		}
		if !strings.Contains(sql, `"active_release_id"`) {
			t.Error("expected active_release_id column")
		}
	})

	t.Run("idempotency_keys has index on key and tenant_id", func(t *testing.T) {
		if !strings.Contains(sql, `"idempotency_keys_key_tenant_idx"`) {
			t.Error("expected index on (key, tenant_id)")
		}
	})

	t.Run("sse_events has index on build_id and id", func(t *testing.T) {
		if !strings.Contains(sql, `"idx_sse_events_build"`) {
			t.Error("expected index on (build_id, id)")
		}
	})

	t.Run("migration is forward-only (no DROP statements)", func(t *testing.T) {
		upperSQL := strings.ToUpper(sql)
		if strings.Contains(upperSQL, "DROP TABLE") || strings.Contains(upperSQL, "DROP COLUMN") {
			t.Error("migration contains DROP statements — violates forward-only rule")
		}
	})

	t.Run("JSONB columns present", func(t *testing.T) {
		jsonbColumns := []string{
			`"artifacts" jsonb`,
			`"side_effects" jsonb`,
			`"metadata" jsonb`,
			`"data" jsonb`,
			`"response_body" jsonb`,
		}
		for _, col := range jsonbColumns {
			if !strings.Contains(sql, col) {
				t.Errorf("expected JSONB column: %s", col)
			}
		}
	})
}

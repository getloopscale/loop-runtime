// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// fk_explicit_on_delete_test.go — T7.2 of deep-review-remediation-plan.md.
// Closes find_data_004 (HIGH): 15 FK references in migration 000001 lacked
// ON DELETE clause. Migration 000012 makes the behavior explicit. This test
// asserts no FK on the application schema retains the implicit NO ACTION
// default (pg_constraint.confdeltype = 'a').
//
// PostgreSQL confdeltype values:
//
//	'a' — NO ACTION (default — what we want to eliminate)
//	'r' — RESTRICT
//	'c' — CASCADE
//	'n' — SET NULL
//	'd' — SET DEFAULT
package integration

import (
	"context"
	"testing"
)

// TestForeignKeyConstraintsAreExplicit asserts that no FK in the public
// schema uses the implicit NO ACTION default. This is a fail-closed
// guardrail against future migrations regressing find_data_004.
func TestForeignKeyConstraintsAreExplicit(t *testing.T) {
	pool := setupHardeningTestDB(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
		SELECT
			conrelid::regclass::text AS table_name,
			conname,
			confdeltype
		FROM pg_constraint
		WHERE contype = 'f'
		  AND connamespace = 'public'::regnamespace
		ORDER BY table_name, conname
	`)
	if err != nil {
		t.Fatalf("query pg_constraint: %v", err)
	}
	defer rows.Close()

	type fkRow struct {
		Table       string
		Name        string
		ConfDelType string
	}

	var implicit []fkRow
	var total int
	for rows.Next() {
		var r fkRow
		if err := rows.Scan(&r.Table, &r.Name, &r.ConfDelType); err != nil {
			t.Fatalf("scan: %v", err)
		}
		total++
		if r.ConfDelType == "a" {
			implicit = append(implicit, r)
		}
	}
	if rows.Err() != nil {
		t.Fatalf("iter rows: %v", rows.Err())
	}

	// CANARY (T0.4 lesson): if we found ZERO FKs at all the test is broken,
	// not the schema.
	if total == 0 {
		t.Fatalf("zero foreign keys found in public schema — test is broken or migrations did not apply")
	}

	if len(implicit) > 0 {
		t.Errorf(
			"%d/%d FK constraints still use implicit NO ACTION (find_data_004 regression). "+
				"Add explicit ON DELETE behavior in a forward migration:\n%v",
			len(implicit), total, implicit,
		)
	}
}

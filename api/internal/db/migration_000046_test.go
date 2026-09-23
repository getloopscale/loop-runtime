// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestMigration000046_Schema — T2.1 of plan metering-compute-loop. PG-REAL:
// asserts the compute-metering schema is in effect against a real Postgres
// (LOOPZERO_E2E_DATABASE_URL). Skips when the real-PG harness is not wired.
//
// Covers: (a) cost_counters.metric CHECK admits the compute metrics;
// (b) metering_cursor PK is (tenant_id, metric, cell_id) with NO period column
// (SC-4 period-independence lock); (c) metering_config is seeded enabled=false
// (EC-5 observe-only ships off); (d) both metering tables have RLS DISABLED.
func TestMigration000046_Schema(t *testing.T) {
	dsn := os.Getenv("LOOPZERO_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("LOOPZERO_E2E_DATABASE_URL not set — real-PG migration test skipped")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// (a) CHECK admits both compute metrics.
	var checkDef string
	if err := conn.QueryRow(ctx,
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint
		 WHERE conrelid='cost_counters'::regclass AND conname='cost_counters_metric_check'`,
	).Scan(&checkDef); err != nil {
		t.Fatalf("read metric CHECK: %v", err)
	}
	for _, m := range []string{"cpu_core_seconds", "mem_gb_seconds", "egress_bytes"} {
		if !strings.Contains(checkDef, m) {
			t.Errorf("cost_counters.metric CHECK missing %q: %s", m, checkDef)
		}
	}

	// (b) metering_cursor PK = (tenant_id, metric, cell_id), NO period column.
	var pkCols string
	if err := conn.QueryRow(ctx,
		`SELECT string_agg(a.attname, ',' ORDER BY array_position(i.indkey, a.attnum))
		 FROM pg_index i JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=ANY(i.indkey)
		 WHERE i.indrelid='metering_cursor'::regclass AND i.indisprimary`,
	).Scan(&pkCols); err != nil {
		t.Fatalf("read metering_cursor PK: %v", err)
	}
	if pkCols != "tenant_id,metric,cell_id" {
		t.Errorf("metering_cursor PK = %q, want tenant_id,metric,cell_id (SC-4: no period column)", pkCols)
	}
	if strings.Contains(pkCols, "period") {
		t.Errorf("metering_cursor PK contains a period column (%q) — breaks SC-4 period-independence", pkCols)
	}

	// (c) metering_config seeded enabled=false.
	var enabled bool
	var rows int
	if err := conn.QueryRow(ctx, `SELECT count(*), bool_or(enabled) FROM metering_config`).Scan(&rows, &enabled); err != nil {
		t.Fatalf("read metering_config: %v", err)
	}
	if rows != 1 {
		t.Errorf("metering_config row count = %d, want 1 (singleton kill-switch)", rows)
	}
	if enabled {
		t.Error("metering_config.enabled = true, want false (EC-5: observe-only ships disabled)")
	}

	// (d) Row-level security. 000046 created both tables without RLS; 000062
	// (rls_fail_closed) then put every tenant_id-scoped table under a
	// fail-closed policy, metering_cursor included. The live schema is the
	// result of the whole chain, so metering_cursor carries RLS and the
	// singleton metering_config (no tenant_id) still does not.
	for tbl, want := range map[string]bool{"metering_cursor": true, "metering_config": false} {
		var rls bool
		if err := conn.QueryRow(ctx,
			`SELECT relrowsecurity FROM pg_class WHERE relname=$1`, tbl,
		).Scan(&rls); err != nil {
			t.Fatalf("read RLS for %s: %v", tbl, err)
		}
		if rls != want {
			t.Errorf("%s relrowsecurity = %v, want %v", tbl, rls, want)
		}
	}
}

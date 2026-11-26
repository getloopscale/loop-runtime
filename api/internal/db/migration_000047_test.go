// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestMigration000047_UsageQuantityBigint — T2.2 of plan metering-compute-loop.
// PG-REAL: usage_records.quantity is widened INTEGER -> BIGINT so a compute
// counter (cpu_core_seconds over a month across many pods) cannot overflow the
// 2^31 INTEGER ceiling. Asserting the column type is BIGINT is sufficient — a
// bigint column stores up to 2^63-1 by definition, well past 2^31. Skips when
// the real-PG harness is not wired.
func TestMigration000047_UsageQuantityBigint(t *testing.T) {
	dsn := os.Getenv("LOOPRUNTIME_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("LOOPRUNTIME_E2E_DATABASE_URL not set — real-PG migration test skipped")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var dataType string
	if err := conn.QueryRow(ctx,
		`SELECT data_type FROM information_schema.columns
		 WHERE table_name='usage_records' AND column_name='quantity'`,
	).Scan(&dataType); err != nil {
		t.Fatalf("read usage_records.quantity type: %v", err)
	}
	if dataType != "bigint" {
		t.Errorf("usage_records.quantity data_type = %q, want bigint (INTEGER would overflow at 2^31 for compute counters)", dataType)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
)

// seedPinCell creates a cell with the given status and capacity (nil =
// unlimited) and removes it, and every tenant pinned to it, on cleanup.
func seedPinCell(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cellID, status string, capacity *int) {
	t.Helper()
	cleanup := func() {
		_, _ = pool.Exec(ctx, `UPDATE tenants SET cell_id='cell-0' WHERE cell_id=$1`, cellID)
		_, _ = pool.Exec(ctx, `DELETE FROM cells WHERE cell_id=$1`, cellID)
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := pool.Exec(ctx, `INSERT INTO cells(cell_id, display_name, status, capacity_tenants_max) VALUES ($1,$1,$2,$3)`,
		cellID, status, capacity); err != nil {
		t.Fatalf("seed cell: %v", err)
	}
}

func tenantCell(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) string {
	t.Helper()
	var c string
	if err := pool.QueryRow(ctx, `SELECT cell_id FROM tenants WHERE id=$1`, tenantID).Scan(&c); err != nil {
		t.Fatalf("read tenant cell: %v", err)
	}
	return c
}

func pinStore(pool *pgxpool.Pool) *PostgresCellStore {
	return &PostgresCellStore{DB: &db.PgxAdapter{Pool: pool}}
}

func TestPinTenantToCell_MovesTheTenantAndReturnsThePreviousCell(t *testing.T) {
	ctx, pool := meteringPool(t)
	seedGCTenant(t, ctx, pool, "tn-pin-move")
	seedPinCell(t, ctx, pool, "cell-pin-move", "active", nil)

	prev, err := pinStore(pool).PinTenantToCell(ctx, "tn-pin-move", "cell-pin-move")
	if err != nil {
		t.Fatalf("PinTenantToCell: %v", err)
	}
	if prev != "cell-0" || tenantCell(t, ctx, pool, "tn-pin-move") != "cell-pin-move" {
		t.Errorf("prev=%q now=%q, want cell-0 -> cell-pin-move", prev, tenantCell(t, ctx, pool, "tn-pin-move"))
	}
}

func TestPinTenantToCell_UnknownTenantIsTenantNotFound(t *testing.T) {
	ctx, pool := meteringPool(t)
	seedPinCell(t, ctx, pool, "cell-pin-nt", "active", nil)
	if _, err := pinStore(pool).PinTenantToCell(ctx, "tn-pin-absent", "cell-pin-nt"); !errors.Is(err, ErrCellPinnerTenantNotFound) {
		t.Fatalf("want ErrCellPinnerTenantNotFound, got %v", err)
	}
}

func TestPinTenantToCell_UnknownCellIsCellNotFound(t *testing.T) {
	ctx, pool := meteringPool(t)
	seedGCTenant(t, ctx, pool, "tn-pin-nc")
	if _, err := pinStore(pool).PinTenantToCell(ctx, "tn-pin-nc", "cell-pin-absent"); !errors.Is(err, ErrCellPinnerCellNotFound) {
		t.Fatalf("want ErrCellPinnerCellNotFound, got %v", err)
	}
}

func TestPinTenantToCell_InactiveCellIsNotPinnable(t *testing.T) {
	ctx, pool := meteringPool(t)
	seedGCTenant(t, ctx, pool, "tn-pin-drain")
	seedPinCell(t, ctx, pool, "cell-pin-drain", "draining", nil)
	if _, err := pinStore(pool).PinTenantToCell(ctx, "tn-pin-drain", "cell-pin-drain"); !errors.Is(err, ErrCellPinnerNotPinnable) {
		t.Fatalf("want ErrCellPinnerNotPinnable, got %v", err)
	}
	if got := tenantCell(t, ctx, pool, "tn-pin-drain"); got != "cell-0" {
		t.Errorf("a refused pin moved the tenant to %q", got)
	}
}

func TestPinTenantToCell_FullCellIsAtCapacity(t *testing.T) {
	ctx, pool := meteringPool(t)
	one := 1
	seedPinCell(t, ctx, pool, "cell-pin-full", "active", &one)
	seedGCTenant(t, ctx, pool, "tn-pin-occupant")
	seedGCTenant(t, ctx, pool, "tn-pin-mover")
	if _, err := pinStore(pool).PinTenantToCell(ctx, "tn-pin-occupant", "cell-pin-full"); err != nil {
		t.Fatalf("fill the cell: %v", err)
	}

	if _, err := pinStore(pool).PinTenantToCell(ctx, "tn-pin-mover", "cell-pin-full"); !errors.Is(err, ErrCellPinnerAtCapacity) {
		t.Fatalf("want ErrCellPinnerAtCapacity, got %v", err)
	}
}

func TestPinTenantToCell_RepinningToItsOwnFullCellIsAllowed(t *testing.T) {
	ctx, pool := meteringPool(t)
	one := 1
	seedPinCell(t, ctx, pool, "cell-pin-self", "active", &one)
	seedGCTenant(t, ctx, pool, "tn-pin-self")
	for i := 0; i < 2; i++ {
		if _, err := pinStore(pool).PinTenantToCell(ctx, "tn-pin-self", "cell-pin-self"); err != nil {
			t.Fatalf("pin #%d: %v", i+1, err)
		}
	}
}

// The capacity check and the move must be one atomic step, serialized with
// placement: otherwise two concurrent pins both read "room for one" and both
// move in. While another transaction holds the placement lock, a pin must not
// complete — it waits for the lock, and here gives up when its context expires.
func TestPinTenantToCell_WaitsForThePlacementLock(t *testing.T) {
	ctx, pool := meteringPool(t)
	seedGCTenant(t, ctx, pool, "tn-pin-lock")
	seedPinCell(t, ctx, pool, "cell-pin-lock", "active", nil)

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", dbtx.PlacementLockID); err != nil {
		t.Fatalf("hold placement lock: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if _, err := pinStore(pool).PinTenantToCell(waitCtx, "tn-pin-lock", "cell-pin-lock"); err == nil {
		t.Fatal("the pin completed while placement was locked by another transaction")
	}
	if got := tenantCell(t, ctx, pool, "tn-pin-lock"); got != "cell-0" {
		t.Fatalf("the tenant moved to %q without the placement lock", got)
	}

	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	if _, err := pinStore(pool).PinTenantToCell(ctx, "tn-pin-lock", "cell-pin-lock"); err != nil {
		t.Fatalf("pin after the lock was released: %v", err)
	}
}

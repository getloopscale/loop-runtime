// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 5 T5.5 — CellPinner service.
// Pins a tenant to a specific cell after validating:
//   1. Tenant exists.
//   2. Cell exists in `cells` table.
//   3. Cell.status is 'active' (rejects 'draining', 'maintenance', 'retired').
//   4. Cell.capacity_tenants_max not exceeded by current tenant count.
//
// Uses a single SERIALIZABLE transaction so the capacity check + UPDATE
// is atomic (no race between two pin requests filling the last slot).
//
// Returns previous cell_id so callers can rollback (`pin tenant cell-prev`).

package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CellPinnerStore is the persistence interface CellPinner depends on.
// Implementation lives in db/cell_pinner_store.go (sibling, ships in
// follow-up commit; for now the interface is enough to wire admin
// handler tests + CLI command).
type CellPinnerStore interface {
	// PinTenantToCell runs the SERIALIZABLE transaction described above.
	// Returns previous cell_id, or one of:
	//   ErrTenantNotFound
	//   ErrCellNotFound
	//   ErrCellNotPinnable  (status != active)
	//   ErrCellAtCapacity   (capacity_tenants_max reached)
	PinTenantToCell(ctx context.Context, tenantID, cellID string) (previousCell string, err error)
}

// Sentinel errors. The HTTP handler maps these to status codes via
// substring match (see admin_pin_cell.go); this package exposes the
// concrete values for unit tests against the service.
//
// Note: ErrCellAtCapacity already exists in services/admission.go for
// the build-admission gate ("cell at build capacity"). The pinner uses
// a distinct sentinel ErrCellAtTenantCapacity to avoid collision and
// disambiguate the two limits (build concurrency vs tenant population).
var (
	ErrTenantNotFound       = errors.New("tenant not found")
	ErrCellNotFound         = errors.New("cell not found")
	ErrCellNotPinnable      = errors.New("cell not pinnable")
	ErrCellAtTenantCapacity = errors.New("cell at capacity")
)

// CellPinner is the routes.CellPinner consumer (see admin_pin_cell.go).
// It's a thin orchestration over CellPinnerStore so the route layer
// doesn't depend on db internals.
type CellPinner struct {
	store CellPinnerStore
}

// NewCellPinner constructs a pinner with the given persistence backend.
func NewCellPinner(store CellPinnerStore) *CellPinner {
	return &CellPinner{store: store}
}

// Pin satisfies the routes.CellPinner interface from admin_pin_cell.go.
func (p *CellPinner) Pin(ctx context.Context, tenantID, cellID string) (string, error) {
	if p.store == nil {
		return "", fmt.Errorf("cell pinner: store not configured")
	}
	return p.store.PinTenantToCell(ctx, tenantID, cellID)
}

// pinTenantToCellSQL is the canonical query the store implementation
// runs in a SERIALIZABLE transaction. Exposed as a package-level
// constant so tests can reference it without coupling to pgx.
//
//revive:disable-next-line:unused-receiver
const pinTenantToCellSQL = `
WITH
-- 1) Tenant must exist.
target_tenant AS (
    SELECT id, cell_id AS prev_cell_id FROM tenants WHERE id = $1
),
-- 2) Cell must exist + be active. capacity_tenants_max is NULL = unlimited.
target_cell AS (
    SELECT cell_id, status, capacity_tenants_max FROM cells WHERE cell_id = $2
),
-- 3) Current tenant count on the target cell (excluding the moving tenant
--    so a re-pin to the same cell doesn't fail capacity off-by-one).
cell_population AS (
    SELECT COUNT(*) AS current_count FROM tenants
     WHERE cell_id = $2 AND id <> $1
)
UPDATE tenants
   SET cell_id = $2
 WHERE id = $1
   AND EXISTS (SELECT 1 FROM target_tenant)
   AND EXISTS (SELECT 1 FROM target_cell tc WHERE tc.status = 'active')
   AND (
       (SELECT capacity_tenants_max FROM target_cell) IS NULL
       OR (SELECT capacity_tenants_max FROM target_cell) >
          (SELECT current_count FROM cell_population)
   )
RETURNING (SELECT prev_cell_id FROM target_tenant);
`

// _ = pinTenantToCellSQL exists to prevent unused-import errors in tests
// that don't reference it. Concrete db implementation will use this
// SQL in db/cell_pinner_store.go (NEW file ships with the migration
// 000003_cells reconciler).
var _ = pinTenantToCellSQL

// _ Tx is a type-system check that the store-side will need pgx.Tx-like
// semantics. Documented here so any future driver swap doesn't drift.
type _ pgx.Tx

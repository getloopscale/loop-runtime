// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package placement implements the Cell placement and routing layer.
// ADR-0008 Phase 1+: Multi-cell only — no DefaultEngine / single-cell mode.
//
// Production wiring:
//
//	DynamicRouter   ← discovers cells from ClusterCredential CRs
//	RoundRobinEngine ← assigns NEW tenants to least-loaded cell, sticky for
//	                   existing tenants. Wrapped by an advisory-lock
//	                   Serializer in production to prevent TOCTOU on
//	                   concurrent tenant creation.
//
// CHG-2026-04-30-DELETE-SINGLE-CELL deleted DefaultEngine, the Engine
// interface, ConstraintEngine and CellLabelQuerier, all unused in production
// paths. ExtractRegion (below) and DefaultCellID (cells.go) were kept: the
// first feeds CellStatus.Region, the second names the dev single-cluster
// cell. The cell-0 value of tenants.cell_id remains as the SQL DEFAULT — this
// is data, not code, and has its own comment in 000001_initial_schema.up.sql.
package placement

import (
	"context"
	"fmt"
	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
)

// TenantCellQuerier abstracts tenant-to-cell lookups against the DB.
type TenantCellQuerier interface {
	// CountByCell returns tenant count per cell_id.
	CountByCell(ctx context.Context) (map[string]int, error)
	// GetTenantCellID returns the cell_id for a specific tenant.
	GetTenantCellID(ctx context.Context, tenantID string) (string, error)
	// CapacityByCell returns the tenant capacity each cell declares
	// (cells.capacity_tenants_max). A cell absent from the map declares none
	// and is bounded by the platform-wide capacity.
	CapacityByCell(ctx context.Context) (map[string]int, error)
}

// ExtractRegion derives a region label from a cell_id.
// Convention: cell-{region}-{nn}. Examples:
//
//	DefaultCellID  → "default" (legacy single-cell installs)
//	"cell-sfo3-02" → "sfo3"
//	"cell-1"       → "1" (no region encoded; rare)
//
// Used by services/cell_service.go to populate CellStatus.Region in
// API responses.
func ExtractRegion(cellID string) string {
	if cellID == DefaultCellID {
		return "default"
	}
	if len(cellID) > 5 { // strip "cell-" prefix
		rest := cellID[5:]
		for i := len(rest) - 1; i >= 0; i-- {
			if rest[i] == '-' {
				return rest[:i]
			}
		}
		return rest
	}
	return cellID
}

// Serializer wraps a callback in a critical section so concurrent callers
// cannot both read the same tenant count and both assign to the same cell.
// In production the wrapper acquires a process-wide `pg_advisory_xact_lock`
// on PlacementLockID so multi-replica API deployments serialize their
// placement decisions through Postgres. In tests a `sync.Mutex.Lock`
// wrapper is enough.
//
// The callback must be called exactly once and its error returned verbatim.
// The Serializer must NOT swallow the callback error.
type Serializer func(ctx context.Context, fn func(ctx context.Context) error) error

// PlacementLockID is the Postgres advisory lock id used by production
// Serializer implementations. Chosen from the user-reserved lock space so
// it never collides with built-in Postgres locks or with LoopZero's environment
// lock family (which uses hashes of (tenant, project, env)).
// "plcmtlck" in hex. The value is declared once, in dbtx, because the admin
// cell pin must take the same lock (see dbtx.PlacementLockID).
const PlacementLockID = dbtx.PlacementLockID

// RoundRobinEngine assigns new tenants to the Cell with fewest tenants.
// Existing tenants keep their cell_id (sticky assignment).
type RoundRobinEngine struct {
	router     Router
	querier    TenantCellQuerier
	maxPerCell int
	serialize  Serializer // nil → single-writer mode (tests only)
}

// NewRoundRobinEngine creates a placement engine that distributes tenants
// across Cells using a bin-packing (least-loaded) strategy.
// maxPerCell is the platform-wide capacity (MAX_TENANTS_PER_CELL), applied to
// every cell that declares no capacity of its own (default 500 if <= 0).
func NewRoundRobinEngine(router Router, querier TenantCellQuerier, maxPerCell int) *RoundRobinEngine {
	if maxPerCell <= 0 {
		maxPerCell = 500
	}
	return &RoundRobinEngine{
		router:     router,
		querier:    querier,
		maxPerCell: maxPerCell,
	}
}

// WithSerializer returns a copy of the engine that runs every AssignCell
// call inside the given serializer. Use this in production with a
// pg_advisory_xact_lock wrapper so concurrent tenant creations cannot
// both observe the same counts and both pick the same cell (TOCTOU).
//
//	engine := placement.NewRoundRobinEngine(router, querier, 500).
//	    WithSerializer(advisoryLockSerializer(pool))
//
// The original engine is not mutated — callers may continue to use it if
// they need an unserialized variant (e.g. tests that assert the race).
func (e *RoundRobinEngine) WithSerializer(s Serializer) *RoundRobinEngine {
	cp := *e
	cp.serialize = s
	return &cp
}

// AssignCell returns the cell_id of the Cell with fewest tenants (bin-packing).
// Returns error if all Cells are at capacity.
func (e *RoundRobinEngine) AssignCell(ctx context.Context) (string, error) {
	var cellID string
	run := func(ctx context.Context) error {
		picked, err := e.assignLocked(ctx)
		if err != nil {
			return err
		}
		cellID = picked
		return nil
	}
	if e.serialize != nil {
		if err := e.serialize(ctx, run); err != nil {
			return "", err
		}
		return cellID, nil
	}
	if err := run(ctx); err != nil {
		return "", err
	}
	return cellID, nil
}

// assignLocked performs the actual count-and-pick. It does NOT acquire any
// lock on its own — the caller is responsible for serialization if the
// engine is used from multiple goroutines / replicas.
func (e *RoundRobinEngine) assignLocked(ctx context.Context) (string, error) {
	cells, err := e.router.ListCells(ctx)
	if err != nil {
		return "", fmt.Errorf("placement: list cells: %w", err)
	}
	if len(cells) == 0 {
		return "", fmt.Errorf("placement: no cells available")
	}

	counts, err := e.querier.CountByCell(ctx)
	if err != nil {
		return "", fmt.Errorf("placement: count tenants by cell: %w", err)
	}
	declared, err := e.querier.CapacityByCell(ctx)
	if err != nil {
		return "", fmt.Errorf("placement: read cell capacities: %w", err)
	}

	// The least-loaded cell that still has room under its own capacity.
	bestCell := ""
	bestCount := 0
	for _, cell := range cells {
		count := counts[cell.CellID] // 0 if no tenants yet
		if count >= e.capacityOf(cell.CellID, declared) {
			continue
		}
		if bestCell == "" || count < bestCount {
			bestCell, bestCount = cell.CellID, count
		}
	}

	if bestCell == "" {
		return "", fmt.Errorf("placement: all %d cells at capacity (platform default %d tenants per cell)", len(cells), e.maxPerCell)
	}

	return bestCell, nil
}

// capacityOf is the one definition of a cell's tenant capacity: the capacity
// the cell declares, else the platform-wide default.
func (e *RoundRobinEngine) capacityOf(cellID string, declared map[string]int) int {
	if c, ok := declared[cellID]; ok {
		return c
	}
	return e.maxPerCell
}

// AssignAndExecute picks a cell AND runs the given callback inside the same
// serialized transaction. This fixes the TOCTOU bug (MEET-053 D-01) where
// AssignCell runs under the advisory lock but the tenant INSERT runs outside it.
func (e *RoundRobinEngine) AssignAndExecute(ctx context.Context, fn func(ctx context.Context, cellID string) error) error {
	run := func(txCtx context.Context) error {
		cellID, err := e.assignLocked(txCtx)
		if err != nil {
			return err
		}
		return fn(txCtx, cellID)
	}
	if e.serialize != nil {
		return e.serialize(ctx, run)
	}
	return run(ctx)
}

// GetCell returns the cell_id for an existing tenant (reads from DB).
// The tenant's cell assignment is sticky — it does not change.
func (e *RoundRobinEngine) GetCell(ctx context.Context, tenantID string) (string, error) {
	cellID, err := e.querier.GetTenantCellID(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("placement: get tenant cell: %w", err)
	}
	return cellID, nil
}

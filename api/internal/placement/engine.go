// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package placement implements the Cell placement and routing layer.
// ADR-0008: Cell-Based Architecture for 50k+ app scale.
//
// Phase 0: DefaultEngine returns "cell-default" for all tenants.
// Phase 1+: Real engines implement bin-packing, affinity, and capacity constraints.
package placement

import (
	"context"
	"fmt"
)

// DefaultCellID is the cell_id used when only one Cell exists (Phase 0).
const DefaultCellID = "cell-default"

// Engine decides which Cell a tenant should be placed in.
// Implementations must be safe for concurrent use.
type Engine interface {
	// AssignCell returns the cell_id for a new tenant.
	// Called during tenant creation (bootstrap).
	AssignCell(ctx context.Context) (cellID string, err error)

	// GetCell returns the cell_id for an existing tenant.
	// Called during operations to resolve the target Cell.
	GetCell(ctx context.Context, tenantID string) (cellID string, err error)
}

// DefaultEngine always returns DefaultCellID.
// This is the Phase 0 implementation — all tenants in one Cell.
type DefaultEngine struct{}

// NewDefaultEngine creates a Phase 0 placement engine.
func NewDefaultEngine() *DefaultEngine {
	return &DefaultEngine{}
}

// AssignCell returns "cell-default" for all new tenants.
func (e *DefaultEngine) AssignCell(_ context.Context) (string, error) {
	return DefaultCellID, nil
}

// GetCell returns "cell-default" for all existing tenants.
func (e *DefaultEngine) GetCell(_ context.Context, _ string) (string, error) {
	return DefaultCellID, nil
}

// ExtractRegion derives region from cell_id.
// "cell-default" → "default", "cell-sfo3-02" → "sfo3".
func ExtractRegion(cellID string) string {
	if cellID == DefaultCellID {
		return "default"
	}
	if len(cellID) > 5 { // "cell-" prefix
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

// TenantCellQuerier abstracts tenant-to-cell lookups.
// Implemented by the DB layer.
type TenantCellQuerier interface {
	// CountByCell returns tenant count per cell_id.
	CountByCell(ctx context.Context) (map[string]int, error)
	// GetTenantCellID returns the cell_id for a specific tenant.
	GetTenantCellID(ctx context.Context, tenantID string) (string, error)
}

// Serializer wraps a callback in a critical section so concurrent callers
// cannot both read the same tenant count and both assign to the same cell.
// In production the wrapper acquires a process-wide `pg_advisory_xact_lock`
// on a well-known id so even multi-replica API deployments serialize their
// placement decisions through Postgres. In tests a simple `sync.Mutex.Lock`
// wrapper is enough.
//
// The callback must be called exactly once and its error returned verbatim.
// The Serializer must NOT swallow the callback error.
//
// Roadmap 06 — fixes the Read-Count → Decide → (race) → Insert TOCTOU in
// AssignCell that hot-spots all concurrent tenant creates into the same
// cell before any of them persist.
type Serializer func(ctx context.Context, fn func(ctx context.Context) error) error

// PlacementLockID is the Postgres advisory lock id used by production
// Serializer implementations. Chosen from the user-reserved lock space so
// it never collides with built-in Postgres locks or with LoopZero's environment
// lock family (which uses hashes of (tenant, project, env)).
// "plcmtlck" in hex.
const PlacementLockID int64 = 0x706C636D746C636B

// RoundRobinEngine assigns new tenants to the Cell with fewest tenants.
// Existing tenants keep their cell_id (sticky assignment).
// ADR-0008 Phase 1.
type RoundRobinEngine struct {
	router     Router
	querier    TenantCellQuerier
	maxPerCell int
	serialize  Serializer // optional — nil means no serialization (single-writer safe only)
}

// NewRoundRobinEngine creates a placement engine that distributes tenants
// across Cells using a bin-packing (least-loaded) strategy.
// maxPerCell is the capacity limit per Cell (default 500 if <= 0).
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

// WithSerializer returns a shallow copy of the engine that runs every
// AssignCell call inside the given serializer. Use this in production with
// a pg_advisory_xact_lock wrapper so concurrent tenant creations cannot
// both observe the same counts and both pick the same cell (TOCTOU).
//
// The method returns a pointer to the new engine so call sites can chain:
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
//
// When a Serializer was attached via WithSerializer, the entire
// count-and-pick operation runs inside the serialized critical section so
// concurrent AssignCell calls execute sequentially from the database's
// perspective and cannot both pick the same cell.
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

	// Find the Cell with the fewest tenants that is not at capacity.
	bestCell := ""
	bestCount := e.maxPerCell + 1 // start above max so any Cell wins

	for _, cell := range cells {
		count := counts[cell.CellID] // 0 if no tenants yet
		if count < e.maxPerCell && count < bestCount {
			bestCell = cell.CellID
			bestCount = count
		}
	}

	if bestCell == "" {
		return "", fmt.Errorf("placement: all cells at capacity (%d tenants each)", e.maxPerCell)
	}

	return bestCell, nil
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

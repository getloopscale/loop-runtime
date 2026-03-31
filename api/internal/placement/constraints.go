// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"fmt"
	"sort"
)

// PlacementOpts configures placement constraints for a new tenant.
// ADR-0008 Phase 3 (P3-5).
type PlacementOpts struct {
	// PreferredRegion is the region closest to the tenant's users.
	// If set, Cells in this region are preferred (soft constraint).
	PreferredRegion string

	// RequiredLabels are label key-value pairs that the target Cell MUST have.
	// Example: {"tier": "premium"} — only Cells with tier=premium are eligible.
	RequiredLabels map[string]string
}

// CellLabelQuerier abstracts Cell label lookups.
type CellLabelQuerier interface {
	// GetCellLabels returns labels for a Cell.
	GetCellLabels(ctx context.Context, cellID string) (map[string]string, error)
}

// ConstraintEngine assigns tenants to Cells respecting affinity and constraints.
// It extends RoundRobinEngine with region affinity and label-based filtering.
// ADR-0008 Phase 3.
type ConstraintEngine struct {
	router     Router
	querier    TenantCellQuerier
	labels     CellLabelQuerier
	maxPerCell int
}

// NewConstraintEngine creates a placement engine with full constraint support.
func NewConstraintEngine(router Router, querier TenantCellQuerier, labels CellLabelQuerier, maxPerCell int) *ConstraintEngine {
	if maxPerCell <= 0 {
		maxPerCell = 500
	}
	return &ConstraintEngine{
		router:     router,
		querier:    querier,
		labels:     labels,
		maxPerCell: maxPerCell,
	}
}

// AssignCell returns the cell_id of the best Cell matching the given constraints.
// Priority: (1) filter by required labels, (2) filter by capacity, (3) prefer region, (4) least-loaded.
func (e *ConstraintEngine) AssignCell(ctx context.Context) (string, error) {
	return e.AssignCellWithOpts(ctx, PlacementOpts{})
}

// AssignCellWithOpts assigns a Cell with explicit placement constraints.
func (e *ConstraintEngine) AssignCellWithOpts(ctx context.Context, opts PlacementOpts) (string, error) {
	cells, err := e.router.ListCells(ctx)
	if err != nil {
		return "", fmt.Errorf("placement: list cells: %w", err)
	}
	if len(cells) == 0 {
		return "", fmt.Errorf("placement: no cells available")
	}

	counts, err := e.querier.CountByCell(ctx)
	if err != nil {
		return "", fmt.Errorf("placement: count tenants: %w", err)
	}

	// Step 1: Filter by required labels
	var eligible []CellConfig
	for _, cell := range cells {
		if len(opts.RequiredLabels) > 0 && e.labels != nil {
			cellLabels, err := e.labels.GetCellLabels(ctx, cell.CellID)
			if err != nil {
				continue // skip cells with label lookup errors
			}
			if !matchesLabels(cellLabels, opts.RequiredLabels) {
				continue
			}
		}
		eligible = append(eligible, cell)
	}

	if len(eligible) == 0 {
		return "", fmt.Errorf("placement: no cells match required labels")
	}

	// Step 2: Filter by capacity
	var available []CellConfig
	for _, cell := range eligible {
		if counts[cell.CellID] < e.maxPerCell {
			available = append(available, cell)
		}
	}

	if len(available) == 0 {
		return "", fmt.Errorf("placement: all eligible cells at capacity (%d)", e.maxPerCell)
	}

	// Step 3: Sort by preference (region affinity first, then least-loaded)
	sort.Slice(available, func(i, j int) bool {
		iRegion := ExtractRegion(available[i].CellID)
		jRegion := ExtractRegion(available[j].CellID)
		iCount := counts[available[i].CellID]
		jCount := counts[available[j].CellID]

		// Prefer matching region
		if opts.PreferredRegion != "" {
			iMatch := iRegion == opts.PreferredRegion
			jMatch := jRegion == opts.PreferredRegion
			if iMatch != jMatch {
				return iMatch // preferred region first
			}
		}

		// Then least-loaded
		return iCount < jCount
	})

	return available[0].CellID, nil
}

// GetCell returns the cell_id for an existing tenant (sticky).
func (e *ConstraintEngine) GetCell(ctx context.Context, tenantID string) (string, error) {
	cellID, err := e.querier.GetTenantCellID(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("placement: get tenant cell: %w", err)
	}
	return cellID, nil
}

// matchesLabels checks if cellLabels contains all required labels.
func matchesLabels(cellLabels, required map[string]string) bool {
	for k, v := range required {
		if cellLabels[k] != v {
			return false
		}
	}
	return true
}

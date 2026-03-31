// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package placement implements the Cell placement and routing layer.
// ADR-0008: Cell-Based Architecture for 50k+ app scale.
//
// Phase 0: DefaultEngine returns "cell-default" for all tenants.
// Phase 1+: Real engines implement bin-packing, affinity, and capacity constraints.
package placement

import "context"

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

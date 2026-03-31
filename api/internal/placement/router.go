// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"fmt"
)

// CellConfig holds connection details for a Cell's workload cluster.
// Phase 0: single CellConfig matching the current runtime cluster.
// Phase 1+: one CellConfig per Cell, resolved by Router.
type CellConfig struct {
	CellID        string // e.g., "cell-default", "cell-sfo3-02"
	KubeConfig    string // path to kubeconfig file
	KubeContext   string // context name within kubeconfig
	ArgocdURL     string // ArgoCD server URL within the Cell
	ArgocdToken   string // ArgoCD auth token for the Cell
	IngressDomain string // base domain for the Cell's ingress
}

// Router resolves cell_id to connection details.
// Implementations must be safe for concurrent use.
type Router interface {
	// Resolve returns the CellConfig for a given cell_id.
	// Returns error if the cell_id is unknown.
	Resolve(ctx context.Context, cellID string) (*CellConfig, error)

	// ListCells returns all known Cells.
	ListCells(ctx context.Context) ([]CellConfig, error)
}

// PassthroughRouter always returns the same CellConfig regardless of cell_id.
// This is the Phase 0 implementation — single Cell.
type PassthroughRouter struct {
	defaultCell CellConfig
}

// NewPassthroughRouter creates a Phase 0 router that always returns the given config.
func NewPassthroughRouter(cfg CellConfig) *PassthroughRouter {
	return &PassthroughRouter{defaultCell: cfg}
}

// Resolve returns the default CellConfig for any cell_id.
func (r *PassthroughRouter) Resolve(_ context.Context, cellID string) (*CellConfig, error) {
	if cellID == "" {
		return nil, fmt.Errorf("cell router: empty cell_id")
	}
	cfg := r.defaultCell
	cfg.CellID = cellID
	return &cfg, nil
}

// ListCells returns the single default Cell.
func (r *PassthroughRouter) ListCells(_ context.Context) ([]CellConfig, error) {
	return []CellConfig{r.defaultCell}, nil
}

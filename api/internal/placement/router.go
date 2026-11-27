// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placement

import (
	"context"
	"fmt"
)

// CellConfig holds connection details for a Cell's workload cluster.
// Phase 0: single CellConfig matching the current runtime cluster.
// Phase 1+: one CellConfig per Cell, resolved by Router.
type CellConfig struct {
	CellID        string // e.g., "cell-0", "cell-sfo3-02"
	KubeConfig    string // path to kubeconfig file (StaticRouter: from env)
	KubeContext   string // context name within kubeconfig
	ArgocdURL     string // ArgoCD server URL within the Cell
	ArgocdToken   string // ArgoCD auth token for the Cell
	IngressDomain string // base domain for the Cell's ingress
	APIServer     string // K8s API server URL (DynamicRouter: from CRD spec)

	// SecretRef identifies the kubeconfig Secret for this cell (DynamicRouter).
	// StaticRouter leaves these empty (uses KubeConfig path instead).
	// MEET-053 A-01: populated from ClusterCredential spec.secretRef.
	SecretRefName      string // Secret name containing kubeconfig
	SecretRefNamespace string // Secret namespace
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
//
// CHG-2026-04-30-DELETE-SINGLE-CELL: this router is no longer used in
// production. DynamicRouter is the only router used. PassthroughRouter
// is retained ONLY for unit-test fixtures that need a deterministic
// single-cell stub WITHOUT spinning up a fake K8sCellStore. A go vet
// rule (TODO) will eventually fail builds if this constructor is called
// from production paths. Do NOT introduce new production callers.
type PassthroughRouter struct {
	defaultCell CellConfig
}

// NewPassthroughRouter creates a single-cell stub router. TEST FIXTURE ONLY.
// CHG-2026-04-30-DELETE-SINGLE-CELL: no production callers permitted.
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

// DiscoveredCellCount returns 1 (always one cell in passthrough mode).
func (r *PassthroughRouter) DiscoveredCellCount() int { return 1 }

// IsDynamic returns false — PassthroughRouter is static.
func (r *PassthroughRouter) IsDynamic() bool { return false }

// StaticRouter resolves cell_id from a static list of CellConfigs.
// This is the Phase 1 implementation — configured at startup from
// ClusterCredential CRDs, Pulumi outputs, or environment variables.
// ADR-0008 Phase 1.
type StaticRouter struct {
	cells map[string]CellConfig // keyed by cell_id
}

// NewStaticRouter creates a router from a list of CellConfigs.
// Returns error if the list is empty or contains duplicate cell_ids.
func NewStaticRouter(configs []CellConfig) (*StaticRouter, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("cell router: no cells configured")
	}
	cells := make(map[string]CellConfig, len(configs))
	for _, cfg := range configs {
		if cfg.CellID == "" {
			return nil, fmt.Errorf("cell router: cell with empty cell_id")
		}
		if _, exists := cells[cfg.CellID]; exists {
			return nil, fmt.Errorf("cell router: duplicate cell_id %q", cfg.CellID)
		}
		cells[cfg.CellID] = cfg
	}
	return &StaticRouter{cells: cells}, nil
}

// Resolve returns the CellConfig for the given cell_id.
func (r *StaticRouter) Resolve(_ context.Context, cellID string) (*CellConfig, error) {
	if cellID == "" {
		return nil, fmt.Errorf("cell router: empty cell_id")
	}
	cfg, ok := r.cells[cellID]
	if !ok {
		return nil, fmt.Errorf("cell router: unknown cell_id %q", cellID)
	}
	return &cfg, nil
}

// ListCells returns all configured Cells.
func (r *StaticRouter) ListCells(_ context.Context) ([]CellConfig, error) {
	cells := make([]CellConfig, 0, len(r.cells))
	for _, cfg := range r.cells {
		cells = append(cells, cfg)
	}
	return cells, nil
}

// DiscoveredCellCount returns the number of statically configured cells.
func (r *StaticRouter) DiscoveredCellCount() int { return len(r.cells) }

// IsDynamic returns false — StaticRouter is static.
func (r *StaticRouter) IsDynamic() bool { return false }

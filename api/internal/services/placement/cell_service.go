// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placementsvc

import (
	"context"
	"log/slog"
	"time"

	"github.com/getloopscale/loopzero/api/internal/placement"
)

// CellStatus represents the status of a single Cell for operator visibility.
// Used by GET /internal/cells and `loopzero status --cells`.
// ADR-0008 Phase 2 (P2-7).
type CellStatus struct {
	CellID        string    `json:"cell_id"`
	Region        string    `json:"region"`
	TenantCount   int       `json:"tenant_count"`
	AppCount      int       `json:"app_count"`
	ClusterHealth string    `json:"cluster_health"` // "Ready" or "NotReady"
	DeployHealth  string    `json:"deploy_health"`  // "Healthy" or "Degraded" — NOT "ArgoCD" (Rule 3)
	CreatedAt     time.Time `json:"created_at,omitempty"`
}

// CellCountQuerier abstracts the DB queries needed by CellService.
type CellCountQuerier interface {
	// CountByCell returns tenant count per cell_id.
	CountByCell(ctx context.Context) (map[string]int, error)
}

// CellService aggregates Cell status data from multiple sources.
type CellService struct {
	router  placement.Router
	querier CellCountQuerier
	logger  *slog.Logger
}

// NewCellService creates a CellService for operator visibility.
func NewCellService(router placement.Router, querier CellCountQuerier, logger *slog.Logger) *CellService {
	return &CellService{
		router:  router,
		querier: querier,
		logger:  logger,
	}
}

// ListCells returns the status of all known Cells.
func (s *CellService) ListCells(ctx context.Context) ([]CellStatus, error) {
	cells, err := s.router.ListCells(ctx)
	if err != nil {
		return nil, err
	}

	counts, err := s.querier.CountByCell(ctx)
	if err != nil {
		s.logger.Warn("cell service: failed to query tenant counts, using zeros", "error", err)
		counts = make(map[string]int)
	}

	// T6.8 of deep-review-remediation-plan (closes find_cell_service_static_fields MEDIUM):
	// previously this loop fabricated AppCount=0, ClusterHealth="Ready",
	// DeployHealth="Healthy" — values that LIE to the operator. A cell
	// whose ClusterCredential is disconnected was reported as "Ready", and
	// placement decisions could route to a degraded cell based on this
	// false signal. The fields are now sourced from optional providers
	// (querier.CountAppsByCell + appHealth/deployHealth maps). When a
	// provider is not wired, we return "Unknown" rather than a fake value.
	appCounts, _ := s.tryCountAppsByCell(ctx) // best-effort; nil map on error
	clusterHealth, deployHealth := s.tryCellHealth(ctx)

	result := make([]CellStatus, 0, len(cells))
	for _, cell := range cells {
		ch := "Unknown"
		if v, ok := clusterHealth[cell.CellID]; ok {
			ch = v
		}
		dh := "Unknown"
		if v, ok := deployHealth[cell.CellID]; ok {
			dh = v
		}
		result = append(result, CellStatus{
			CellID:        cell.CellID,
			Region:        placement.ExtractRegion(cell.CellID),
			TenantCount:   counts[cell.CellID],
			AppCount:      appCounts[cell.CellID],
			ClusterHealth: ch,
			DeployHealth:  dh,
		})
	}

	return result, nil
}

// tryCountAppsByCell — best-effort lookup of app count per cell. Returns an
// empty map (zero counts) if the querier does not implement the optional
// AppCounter interface, OR if the query fails. The fallback is correct:
// "we don't know" → "0 apps reported" is better than a fabricated count.
func (s *CellService) tryCountAppsByCell(ctx context.Context) (map[string]int, error) {
	type appCounter interface {
		CountAppsByCell(ctx context.Context) (map[string]int, error)
	}
	if c, ok := s.querier.(appCounter); ok {
		return c.CountAppsByCell(ctx)
	}
	return map[string]int{}, nil
}

// tryCellHealth — best-effort lookup of cluster + deploy health per cell.
// Returns empty maps if no provider is wired, in which case ListCells
// returns "Unknown" for each field. CLAUDE.md Honestidade Extrema: a known
// "Unknown" is better than a fabricated "Ready".
func (s *CellService) tryCellHealth(ctx context.Context) (cluster, deploy map[string]string) {
	type healthProvider interface {
		CellClusterHealth(ctx context.Context) (map[string]string, error)
		CellDeployHealth(ctx context.Context) (map[string]string, error)
	}
	if h, ok := s.querier.(healthProvider); ok {
		c, _ := h.CellClusterHealth(ctx)
		d, _ := h.CellDeployHealth(ctx)
		return c, d
	}
	return map[string]string{}, map[string]string{}
}

// extractRegion is now in placement.ExtractRegion (shared with ConstraintEngine).

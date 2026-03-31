// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/api/internal/placement"
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

	result := make([]CellStatus, 0, len(cells))
	for _, cell := range cells {
		result = append(result, CellStatus{
			CellID:        cell.CellID,
			Region:        extractRegion(cell.CellID),
			TenantCount:   counts[cell.CellID],
			AppCount:      0, // TODO(phase-2): query actual app count per cell
			ClusterHealth: "Ready",   // TODO(phase-2): probe cluster health
			DeployHealth:  "Healthy", // TODO(phase-2): check ArgoCD health per cell
		})
	}

	return result, nil
}

// extractRegion derives region from cell_id.
// "cell-default" → "default", "cell-sfo3-02" → "sfo3".
func extractRegion(cellID string) string {
	if cellID == placement.DefaultCellID {
		return "default"
	}
	// cell-{region}-{seq} → extract region
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

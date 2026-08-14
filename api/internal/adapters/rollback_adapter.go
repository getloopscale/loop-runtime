package adapters

import (
	"context"
	"github.com/getloopscale/loopzero/api/internal/routes/rollback"

	"github.com/getloopscale/loopzero/api/internal/services/release"
)

// RollbackServiceAdapter bridges the rollback orchestrator to the route handler interface.
// The handler uses routes.RollbackInput/Output while the orchestrator uses release.RollbackRequest/Result.
type RollbackServiceAdapter struct {
	Orchestrator *release.RollbackOrchestrator
}

// Rollback converts route-layer types to service-layer types and delegates to the orchestrator.
func (a *RollbackServiceAdapter) Rollback(ctx context.Context, req rollback.RollbackInput) (*rollback.RollbackOutput, error) {
	result, err := a.Orchestrator.Rollback(ctx, release.RollbackRequest{
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectID,
		ProjectName:   req.ProjectName,
		Environment:   req.Environment,
		ToReleaseID:   req.ToReleaseID,
		CorrelationID: req.CorrelationID,
		UserID:        req.UserID,
		BaseDomain:    req.BaseDomain,
	})
	if err != nil {
		return nil, err
	}

	return &rollback.RollbackOutput{
		RolledBackReleaseID:  result.RolledBackRelease.ID,
		ReactivatedReleaseID: result.ReactivatedRelease.ID,
		Environment:          result.Environment,
		URL:                  result.URL,
	}, nil
}

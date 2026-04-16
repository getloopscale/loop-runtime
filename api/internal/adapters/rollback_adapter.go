package adapters

import (
	"context"

	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/services"
)

// RollbackServiceAdapter bridges the rollback orchestrator to the route handler interface.
// The handler uses routes.RollbackInput/Output while the orchestrator uses services.RollbackRequest/Result.
type RollbackServiceAdapter struct {
	Orchestrator *services.RollbackOrchestrator
}

// Rollback converts route-layer types to service-layer types and delegates to the orchestrator.
func (a *RollbackServiceAdapter) Rollback(ctx context.Context, req routes.RollbackInput) (*routes.RollbackOutput, error) {
	result, err := a.Orchestrator.Rollback(ctx, services.RollbackRequest{
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

	return &routes.RollbackOutput{
		RolledBackReleaseID:  result.RolledBackRelease.ID,
		ReactivatedReleaseID: result.ReactivatedRelease.ID,
		Environment:          result.Environment,
		URL:                  result.URL,
	}, nil
}

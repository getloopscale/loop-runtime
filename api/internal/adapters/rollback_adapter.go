package adapters

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/release/rollbackhttp"

	"github.com/getloopscale/loopzero/api/internal/release"
)

// RollbackServiceAdapter bridges the rollback orchestrator to the route handler interface.
// The handler uses routes.RollbackInput/Output while the orchestrator uses release.RollbackRequest/Result.
type RollbackServiceAdapter struct {
	Orchestrator *release.RollbackOrchestrator
}

// Rollback converts route-layer types to service-layer types and delegates to the orchestrator.
func (a *RollbackServiceAdapter) Rollback(ctx context.Context, req rollbackhttp.RollbackInput) (*rollbackhttp.RollbackOutput, error) {
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

	return &rollbackhttp.RollbackOutput{
		RolledBackReleaseID:  result.RolledBackRelease.ID,
		ReactivatedReleaseID: result.ReactivatedRelease.ID,
		Environment:          result.Environment,
		URL:                  result.URL,
	}, nil
}

// The port this adapter satisfies, asserted at compile time.
//
// B-058: this file did NOT declare, not even in prose, what it adapted — and the package's
// admission criterion was "it is an adapter", which refuses nothing. The port was discovered
// from the consumer, and is now written where the compiler checks it.
var _ rollbackhttp.RollbackService = (*RollbackServiceAdapter)(nil)

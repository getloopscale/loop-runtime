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

// A porta que este adaptador satisfaz, afirmada em tempo de compilacao.
//
// B-058: este arquivo NAO declarava, nem em prosa, o que adaptava — e o criterio de admissao
// do pacote era "e um adapter", que nao recusa nada. A porta foi descoberta pelo consumidor,
// e agora esta escrita onde o compilador a verifica.
var _ rollback.RollbackService = (*RollbackServiceAdapter)(nil)

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"github.com/getloopscale/loopzero/api/internal/routes/promote"

	"github.com/getloopscale/loopzero/api/internal/services/release"
)

// PromoteServiceAdapter bridges the promotion orchestrator to the route handler interface.
// The handler uses routes.PromoteInput/Output while the orchestrator uses release.PromotionRequest/Result.
type PromoteServiceAdapter struct {
	Orchestrator *release.PromotionOrchestrator
}

// Promote converts route-layer types to service-layer types and delegates to the orchestrator.
func (a *PromoteServiceAdapter) Promote(ctx context.Context, req promote.PromoteInput) (*promote.PromoteOutput, error) {
	result, err := a.Orchestrator.Promote(ctx, release.PromotionRequest{
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectID,
		ProjectName:   req.ProjectName,
		FromEnv:       req.FromEnv,
		ToEnv:         req.ToEnv,
		CorrelationID: req.CorrelationID,
		UserID:        req.UserID,
	})
	if err != nil {
		return nil, err
	}

	return &promote.PromoteOutput{
		PromotionID:     result.PromotionID,
		SourceReleaseID: result.SourceRelease.ID,
		NewReleaseID:    result.NewRelease.ID,
		FromEnv:         result.FromEnv,
		ToEnv:           result.ToEnv,
		URL:             result.URL,
	}, nil
}

// A porta que este adaptador satisfaz, afirmada em tempo de compilacao.
//
// B-058: este arquivo NAO declarava, nem em prosa, o que adaptava — e o criterio de admissao
// do pacote era "e um adapter", que nao recusa nada. A porta foi descoberta pelo consumidor,
// e agora esta escrita onde o compilador a verifica.
var _ promote.PromoteService = (*PromoteServiceAdapter)(nil)

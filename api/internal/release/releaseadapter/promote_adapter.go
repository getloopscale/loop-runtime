// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package releaseadapter

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/release/promotehttp"

	"github.com/getloopscale/loop-runtime/api/internal/release"
)

// PromoteServiceAdapter bridges the promotion orchestrator to the route handler interface.
// The handler uses routes.PromoteInput/Output while the orchestrator uses release.PromotionRequest/Result.
type PromoteServiceAdapter struct {
	Orchestrator *release.PromotionOrchestrator
}

// Promote converts route-layer types to service-layer types and delegates to the orchestrator.
func (a *PromoteServiceAdapter) Promote(ctx context.Context, req promotehttp.PromoteInput) (*promotehttp.PromoteOutput, error) {
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

	return &promotehttp.PromoteOutput{
		PromotionID:     result.PromotionID,
		SourceReleaseID: result.SourceRelease.ID,
		NewReleaseID:    result.NewRelease.ID,
		FromEnv:         result.FromEnv,
		ToEnv:           result.ToEnv,
		URL:             result.URL,
	}, nil
}

// The port this adapter satisfies, asserted at compile time.
//
// B-058: this file did NOT declare, not even in prose, what it adapted — and the package's
// admission criterion was "it is an adapter", which refuses nothing. The port was discovered
// from the consumer, and is now written where the compiler checks it.
var _ promotehttp.PromoteService = (*PromoteServiceAdapter)(nil)

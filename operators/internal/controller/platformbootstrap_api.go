// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"fmt"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// reconcileAPIPhase handles the WaitingForAPI phase.
// Steps: check API health, call bootstrap endpoint for DB seeding.
func (r *PlatformBootstrapReconciler) reconcileAPIPhase(ctx context.Context, pb *loopzerov1.PlatformBootstrap, maxRetries int) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	healthURL := pb.Spec.API.HealthEndpoint
	bootstrapURL := pb.Spec.API.BootstrapEndpoint

	// Step 1: Check API health.
	if err := r.checkAPIHealth(ctx, healthURL); err != nil {
		logger.Info("API not healthy", "url", healthURL, "error", err)
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("API not healthy: %v", err))
	}

	// Step 2: Call bootstrap endpoint for DB seeding.
	if err := r.callBootstrapEndpoint(ctx, bootstrapURL); err != nil {
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("bootstrap endpoint failed: %v", err))
	}

	r.setCondition(pb, conditions.TypeDatabaseSeeded, metav1.ConditionTrue, conditions.ReasonReady, "Database seeded via API bootstrap")
	r.setCondition(pb, conditions.TypeBootstrapComplete, metav1.ConditionTrue, conditions.ReasonReady, "Platform bootstrap complete")
	return r.transitionToNextPhase(ctx, pb, statemachine.PlatformBootstrapAPISeeded, "Bootstrap complete")
}

// checkAPIHealth checks if the API is healthy by calling GET on the health endpoint.
func (r *PlatformBootstrapReconciler) checkAPIHealth(ctx context.Context, healthURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("creating health request: %w", err)
	}
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("calling health endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

// callBootstrapEndpoint calls POST /internal/bootstrap for DB seeding.
// The endpoint is idempotent -- calling it multiple times is safe.
func (r *PlatformBootstrapReconciler) callBootstrapEndpoint(ctx context.Context, bootstrapURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, bootstrapURL, nil)
	if err != nil {
		return fmt.Errorf("creating bootstrap request: %w", err)
	}
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("calling bootstrap endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bootstrap endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

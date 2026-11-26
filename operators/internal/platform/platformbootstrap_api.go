// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package platform

import (
	"context"
	"fmt"
	"io"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
	"github.com/getloopscale/loop-runtime/operators/internal/reconcileloop"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// reconcileAPIPhase handles the WaitingForAPI phase.
// Steps: check API health, resolve bootstrap secret, call bootstrap endpoint for DB seeding.
func (r *PlatformBootstrapReconciler) reconcileAPIPhase(ctx context.Context, pb, base *loopruntimev1.PlatformBootstrap, maxRetries int) (ctrl.Result, error) {
	ctx, span := reconcileloop.StartChildSpan(ctx, "bootstrap.api.configure")
	defer span.End()
	logger := log.FromContext(ctx)

	healthURL := pb.Spec.API.HealthEndpoint
	bootstrapURL := pb.Spec.API.BootstrapEndpoint

	// Step 1: Check API health.
	if err := r.checkAPIHealth(ctx, healthURL); err != nil {
		logger.Info("API not healthy", "url", healthURL, "error", err)
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("API not healthy: %v", err))
	}

	// Step 2: Resolve bootstrap secret for SEC-003 auth (B1 fix).
	bootstrapSecret, err := r.resolveBootstrapSecret(ctx, pb)
	if err != nil {
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("resolve bootstrap secret: %v", err))
	}

	// Step 3: Call bootstrap endpoint for DB seeding.
	if err := r.callBootstrapEndpoint(ctx, bootstrapURL, bootstrapSecret); err != nil {
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("bootstrap endpoint failed: %v", err))
	}

	r.setCondition(pb, conditions.TypeDatabaseSeeded, metav1.ConditionTrue, conditions.ReasonReady, "Database seeded via API bootstrap")
	r.setCondition(pb, conditions.TypeBootstrapComplete, metav1.ConditionTrue, conditions.ReasonReady, "Platform bootstrap complete")
	return r.transitionToNextPhase(ctx, pb, base, statemachine.PBPEventAPISeeded)
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
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body) // drain for connection reuse
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

// resolveBootstrapSecret reads the bootstrap auth token from the Secret referenced
// in spec.api.bootstrapSecretRef. Returns empty string if no secretRef is configured
// (for backward compatibility with environments where SEC-003 is not enforced).
func (r *PlatformBootstrapReconciler) resolveBootstrapSecret(ctx context.Context, pb *loopruntimev1.PlatformBootstrap) (string, error) {
	if pb.Spec.API.BootstrapSecretRef == nil {
		return "", nil
	}

	secret, err := r.getSecret(ctx, pb.Spec.API.BootstrapSecretRef.Name, pb.Namespace)
	if err != nil {
		return "", fmt.Errorf("reading bootstrap secret %s: %w", pb.Spec.API.BootstrapSecretRef.Name, err)
	}

	token, ok := secret.Data["bootstrap-secret"]
	if !ok {
		return "", fmt.Errorf("bootstrap secret %s missing key 'bootstrap-secret'", pb.Spec.API.BootstrapSecretRef.Name)
	}

	return string(token), nil
}

// callBootstrapEndpoint calls POST /internal/bootstrap for DB seeding.
// The endpoint is idempotent -- calling it multiple times is safe.
// B1: When bootstrapSecret is non-empty, sends Authorization: Bearer header (SEC-003).
func (r *PlatformBootstrapReconciler) callBootstrapEndpoint(ctx context.Context, bootstrapURL, bootstrapSecret string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, bootstrapURL, nil)
	if err != nil {
		return fmt.Errorf("creating bootstrap request: %w", err)
	}
	if bootstrapSecret != "" {
		req.Header.Set("Authorization", "Bearer "+bootstrapSecret)
	}
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("calling bootstrap endpoint: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body) // drain for connection reuse
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bootstrap endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

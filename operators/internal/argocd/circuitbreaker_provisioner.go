// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"log/slog"

	"github.com/getloopscale/loopzero/pkg/circuitbreaker"
)

// Compile-time interface compliance check.
var _ Provisioner = (*CircuitBreakerProvisioner)(nil)

// CircuitBreakerProvisioner wraps an ArgoCD Provisioner with circuit breaker
// protection. MEET-052: prevents retry storms when ArgoCD is degraded.
type CircuitBreakerProvisioner struct {
	inner  Provisioner
	cb     *circuitbreaker.Breaker
	logger *slog.Logger
}

// NewCircuitBreakerProvisioner creates a Provisioner with circuit breaker protection.
func NewCircuitBreakerProvisioner(inner Provisioner, cb *circuitbreaker.Breaker, logger *slog.Logger) *CircuitBreakerProvisioner {
	return &CircuitBreakerProvisioner{
		inner:  inner,
		cb:     cb,
		logger: logger,
	}
}

// guarded runs fn through circuitbreaker.Execute, so a cancelled or timed-out
// call does not advance the failure counter (ADR-0031), and logs when the
// open breaker skipped the call. subject names what the call acts on.
func guarded[T any](ctx context.Context, p *CircuitBreakerProvisioner, op string, subject []any, fn func(context.Context) (T, error)) (T, error) {
	result, err := circuitbreaker.Execute(ctx, p.cb, fn)
	if err != nil && p.cb.State() == circuitbreaker.Open {
		args := append(subject, "cb_state", p.cb.State().String())
		p.logger.WarnContext(ctx, "argocd circuit breaker open, skipping "+op, args...)
	}
	return result, err
}

// noResult adapts an error-only call to guarded.
func noResult(fn func(context.Context) error) func(context.Context) (struct{}, error) {
	return func(ctx context.Context) (struct{}, error) { return struct{}{}, fn(ctx) }
}

// EnsureAppProject delegates with circuit breaker protection.
func (p *CircuitBreakerProvisioner) EnsureAppProject(ctx context.Context, spec AppProjectSpec) error {
	_, err := guarded(ctx, p, "EnsureAppProject", []any{"tenant", spec.TenantID}, noResult(func(ctx context.Context) error {
		return p.inner.EnsureAppProject(ctx, spec)
	}))
	return err
}

// EnsureApplication delegates with circuit breaker protection.
func (p *CircuitBreakerProvisioner) EnsureApplication(ctx context.Context, spec ApplicationSpec) error {
	_, err := guarded(ctx, p, "EnsureApplication", []any{"tenant", spec.TenantID}, noResult(func(ctx context.Context) error {
		return p.inner.EnsureApplication(ctx, spec)
	}))
	return err
}

// DeleteApplications delegates with circuit breaker protection.
func (p *CircuitBreakerProvisioner) DeleteApplications(ctx context.Context, tenantID string) error {
	_, err := guarded(ctx, p, "DeleteApplications", []any{"tenant", tenantID}, noResult(func(ctx context.Context) error {
		return p.inner.DeleteApplications(ctx, tenantID)
	}))
	return err
}

// DeleteAppProject delegates with circuit breaker protection.
func (p *CircuitBreakerProvisioner) DeleteAppProject(ctx context.Context, tenantID string) error {
	_, err := guarded(ctx, p, "DeleteAppProject", []any{"tenant", tenantID}, noResult(func(ctx context.Context) error {
		return p.inner.DeleteAppProject(ctx, tenantID)
	}))
	return err
}

// ListApplicationNames delegates with circuit breaker protection.
func (p *CircuitBreakerProvisioner) ListApplicationNames(ctx context.Context, tenantID string) ([]string, error) {
	return guarded(ctx, p, "ListApplicationNames", []any{"tenant", tenantID}, func(ctx context.Context) ([]string, error) {
		return p.inner.ListApplicationNames(ctx, tenantID)
	})
}

// DeleteApplication delegates with circuit breaker protection.
func (p *CircuitBreakerProvisioner) DeleteApplication(ctx context.Context, appName string) error {
	_, err := guarded(ctx, p, "DeleteApplication", []any{"app", appName}, noResult(func(ctx context.Context) error {
		return p.inner.DeleteApplication(ctx, appName)
	}))
	return err
}

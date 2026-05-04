// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"log/slog"

	"github.com/loopzero/loopzero/pkg/circuitbreaker"
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

// EnsureAppProject delegates with circuit breaker protection.
//
// Migrated to circuitbreaker.Execute (ADR-0031 + PKG-G16): context
// cancellation no longer falsely advances the failure counter; the
// inner provisioner observes ctx.Err on entry and aborts cleanly.
func (p *CircuitBreakerProvisioner) EnsureAppProject(ctx context.Context, spec AppProjectSpec) error {
	_, err := circuitbreaker.Execute(ctx, p.cb, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, p.inner.EnsureAppProject(ctx, spec)
	})
	if err != nil && p.cb.State() == circuitbreaker.Open {
		p.logger.WarnContext(ctx, "argocd circuit breaker open, skipping EnsureAppProject",
			"tenant", spec.TenantID, "cb_state", p.cb.State().String())
	}
	return err
}

// EnsureApplication delegates with circuit breaker protection.
func (p *CircuitBreakerProvisioner) EnsureApplication(ctx context.Context, spec ApplicationSpec) error {
	if err := p.cb.PreCheck(); err != nil {
		p.logger.WarnContext(ctx, "argocd circuit breaker open, skipping EnsureApplication",
			"tenant", spec.TenantID, "cb_state", p.cb.State().String())
		return err
	}
	err := p.inner.EnsureApplication(ctx, spec)
	p.cb.RecordResult(err)
	return err
}

// DeleteApplications delegates with circuit breaker protection.
func (p *CircuitBreakerProvisioner) DeleteApplications(ctx context.Context, tenantID string) error {
	if err := p.cb.PreCheck(); err != nil {
		p.logger.WarnContext(ctx, "argocd circuit breaker open, skipping DeleteApplications",
			"tenant", tenantID, "cb_state", p.cb.State().String())
		return err
	}
	err := p.inner.DeleteApplications(ctx, tenantID)
	p.cb.RecordResult(err)
	return err
}

// DeleteAppProject delegates with circuit breaker protection.
func (p *CircuitBreakerProvisioner) DeleteAppProject(ctx context.Context, tenantID string) error {
	if err := p.cb.PreCheck(); err != nil {
		p.logger.WarnContext(ctx, "argocd circuit breaker open, skipping DeleteAppProject",
			"tenant", tenantID, "cb_state", p.cb.State().String())
		return err
	}
	err := p.inner.DeleteAppProject(ctx, tenantID)
	p.cb.RecordResult(err)
	return err
}

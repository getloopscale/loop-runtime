// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package minio

import (
	"context"
	"log/slog"

	"github.com/getloopscale/loop-runtime/pkg/circuitbreaker"
)

// Compile-time interface compliance check.
var _ CredentialProvisioner = (*CircuitBreakerProvisioner)(nil)

// CircuitBreakerProvisioner wraps a CredentialProvisioner with circuit breaker
// protection for external MinIO API calls. MEET-052: prevents retry storms
// when MinIO is saturated during bulk tenant provisioning or deletion.
//
// Migrated to circuitbreaker.Execute (ADR-0031 + PKG-G16): context
// cancellation no longer falsely advances the failure counter.
type CircuitBreakerProvisioner struct {
	inner  CredentialProvisioner
	cb     *circuitbreaker.Breaker
	logger *slog.Logger
}

// NewCircuitBreakerProvisioner creates a CredentialProvisioner with circuit breaker protection.
func NewCircuitBreakerProvisioner(inner CredentialProvisioner, cb *circuitbreaker.Breaker, logger *slog.Logger) *CircuitBreakerProvisioner {
	return &CircuitBreakerProvisioner{
		inner:  inner,
		cb:     cb,
		logger: logger,
	}
}

// credPair is an unexported helper to bundle (accessKey, secretKey) so
// circuitbreaker.Execute (single-T-result) can return both at once.
type credPair struct {
	access, secret string
}

// EnsureTenantCredentials delegates to the inner provisioner with circuit breaker protection.
func (p *CircuitBreakerProvisioner) EnsureTenantCredentials(ctx context.Context, tenantID string) (string, string, error) {
	pair, err := circuitbreaker.Execute(ctx, p.cb, func(ctx context.Context) (credPair, error) {
		a, s, e := p.inner.EnsureTenantCredentials(ctx, tenantID)
		return credPair{access: a, secret: s}, e
	})
	if err != nil && p.cb.State() == circuitbreaker.Open {
		p.logger.WarnContext(ctx, "minio circuit breaker open, skipping EnsureTenantCredentials",
			"tenant", tenantID, "cb_state", p.cb.State().String())
	}
	return pair.access, pair.secret, err
}

// DeleteTenantCredentials delegates to the inner provisioner with circuit breaker protection.
func (p *CircuitBreakerProvisioner) DeleteTenantCredentials(ctx context.Context, tenantID string) error {
	_, err := circuitbreaker.Execute(ctx, p.cb, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, p.inner.DeleteTenantCredentials(ctx, tenantID)
	})
	if err != nil && p.cb.State() == circuitbreaker.Open {
		p.logger.WarnContext(ctx, "minio circuit breaker open, skipping DeleteTenantCredentials",
			"tenant", tenantID, "cb_state", p.cb.State().String())
	}
	return err
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package minio

import (
	"context"
	"log/slog"

	"github.com/loopzero/loopzero/pkg/circuitbreaker"
)

// Compile-time interface compliance check.
var _ CredentialProvisioner = (*CircuitBreakerProvisioner)(nil)

// CircuitBreakerProvisioner wraps a CredentialProvisioner with circuit breaker
// protection for external MinIO API calls. MEET-052: prevents retry storms
// when MinIO is saturated during bulk tenant provisioning or deletion.
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

// EnsureTenantCredentials delegates to the inner provisioner with circuit breaker protection.
func (p *CircuitBreakerProvisioner) EnsureTenantCredentials(ctx context.Context, tenantID string) (string, string, error) {
	if err := p.cb.PreCheck(); err != nil {
		p.logger.WarnContext(ctx, "minio circuit breaker open, skipping EnsureTenantCredentials",
			"tenant", tenantID, "cb_state", p.cb.State().String())
		return "", "", err
	}
	accessKey, secretKey, err := p.inner.EnsureTenantCredentials(ctx, tenantID)
	p.cb.RecordResult(err)
	return accessKey, secretKey, err
}

// DeleteTenantCredentials delegates to the inner provisioner with circuit breaker protection.
func (p *CircuitBreakerProvisioner) DeleteTenantCredentials(ctx context.Context, tenantID string) error {
	if err := p.cb.PreCheck(); err != nil {
		p.logger.WarnContext(ctx, "minio circuit breaker open, skipping DeleteTenantCredentials",
			"tenant", tenantID, "cb_state", p.cb.State().String())
		return err
	}
	err := p.inner.DeleteTenantCredentials(ctx, tenantID)
	p.cb.RecordResult(err)
	return err
}

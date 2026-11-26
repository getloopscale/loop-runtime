// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitea

import (
	"context"
	"log/slog"

	"github.com/getloopscale/loop-runtime/pkg/circuitbreaker"
)

// Compile-time interface compliance check.
var _ GitRepoProvisioner = (*CircuitBreakerProvisioner)(nil)

// CircuitBreakerProvisioner wraps a GitRepoProvisioner with circuit breaker
// protection for external Gitea API calls. MEET-052: prevents retry storms
// when Gitea is saturated during bulk tenant deletion.
//
// Migrated to circuitbreaker.Execute (ADR-0031 + PKG-G16): context
// cancellation no longer falsely advances the failure counter; the
// inner provisioner observes ctx.Err on entry and aborts cleanly.
type CircuitBreakerProvisioner struct {
	inner  GitRepoProvisioner
	cb     *circuitbreaker.Breaker
	logger *slog.Logger
}

// NewCircuitBreakerProvisioner creates a GitRepoProvisioner with circuit breaker protection.
func NewCircuitBreakerProvisioner(inner GitRepoProvisioner, cb *circuitbreaker.Breaker, logger *slog.Logger) *CircuitBreakerProvisioner {
	return &CircuitBreakerProvisioner{
		inner:  inner,
		cb:     cb,
		logger: logger,
	}
}

// EnsureRepo delegates to the inner provisioner with circuit breaker protection.
func (p *CircuitBreakerProvisioner) EnsureRepo(ctx context.Context, name string, branch string) (string, error) {
	url, err := circuitbreaker.Execute(ctx, p.cb, func(ctx context.Context) (string, error) {
		return p.inner.EnsureRepo(ctx, name, branch)
	})
	if err != nil && p.cb.State() == circuitbreaker.Open {
		p.logger.WarnContext(ctx, "gitea circuit breaker open, skipping EnsureRepo",
			"repo", name, "cb_state", p.cb.State().String())
	}
	return url, err
}

// ArchiveRepo delegates to the inner provisioner with circuit breaker protection.
func (p *CircuitBreakerProvisioner) ArchiveRepo(ctx context.Context, name string) error {
	_, err := circuitbreaker.Execute(ctx, p.cb, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, p.inner.ArchiveRepo(ctx, name)
	})
	if err != nil && p.cb.State() == circuitbreaker.Open {
		p.logger.WarnContext(ctx, "gitea circuit breaker open, skipping ArchiveRepo",
			"repo", name, "cb_state", p.cb.State().String())
	}
	return err
}

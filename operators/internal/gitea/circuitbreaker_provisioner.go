// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitea

import (
	"context"
	"log/slog"

	"github.com/loopzero/loopzero/pkg/circuitbreaker"
)

// Compile-time interface compliance check.
var _ GitRepoProvisioner = (*CircuitBreakerProvisioner)(nil)

// CircuitBreakerProvisioner wraps a GitRepoProvisioner with circuit breaker
// protection for external Gitea API calls. MEET-052: prevents retry storms
// when Gitea is saturated during bulk tenant deletion.
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
	if err := p.cb.PreCheck(); err != nil {
		p.logger.WarnContext(ctx, "gitea circuit breaker open, skipping EnsureRepo",
			"repo", name, "cb_state", p.cb.State().String())
		return "", err
	}
	url, err := p.inner.EnsureRepo(ctx, name, branch)
	p.cb.RecordResult(err)
	return url, err
}

// RepoExists delegates to the inner provisioner with circuit breaker protection.
func (p *CircuitBreakerProvisioner) RepoExists(ctx context.Context, name string) (bool, error) {
	if err := p.cb.PreCheck(); err != nil {
		p.logger.WarnContext(ctx, "gitea circuit breaker open, skipping RepoExists",
			"repo", name, "cb_state", p.cb.State().String())
		return false, err
	}
	exists, err := p.inner.RepoExists(ctx, name)
	p.cb.RecordResult(err)
	return exists, err
}

// ArchiveRepo delegates to the inner provisioner with circuit breaker protection.
func (p *CircuitBreakerProvisioner) ArchiveRepo(ctx context.Context, name string) error {
	if err := p.cb.PreCheck(); err != nil {
		p.logger.WarnContext(ctx, "gitea circuit breaker open, skipping ArchiveRepo",
			"repo", name, "cb_state", p.cb.State().String())
		return err
	}
	err := p.inner.ArchiveRepo(ctx, name)
	p.cb.RecordResult(err)
	return err
}

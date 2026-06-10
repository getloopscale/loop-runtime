// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package harbor

import (
	"context"
	"log/slog"

	"github.com/loopzero/loopzero/pkg/circuitbreaker"
)

// Compile-time interface compliance check.
var _ Provisioner = (*CircuitBreakerClient)(nil)

// CircuitBreakerClient wraps a Harbor Provisioner with circuit breaker
// protection for external Harbor API calls. MEET-052: prevents retry storms
// when Harbor is saturated during bulk tenant provisioning or deletion.
//
// Migrated to circuitbreaker.Execute (ADR-0031 + PKG-G16): context
// cancellation no longer falsely advances the failure counter.
type CircuitBreakerClient struct {
	inner  Provisioner
	cb     *circuitbreaker.Breaker
	logger *slog.Logger
}

// NewCircuitBreakerClient creates a Harbor Provisioner with circuit breaker protection.
func NewCircuitBreakerClient(inner Provisioner, cb *circuitbreaker.Breaker, logger *slog.Logger) *CircuitBreakerClient {
	return &CircuitBreakerClient{
		inner:  inner,
		cb:     cb,
		logger: logger,
	}
}

// EnsureProject delegates to the inner provisioner with circuit breaker protection.
func (c *CircuitBreakerClient) EnsureProject(ctx context.Context, name string, opts ProjectOpts) error {
	_, err := circuitbreaker.Execute(ctx, c.cb, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, c.inner.EnsureProject(ctx, name, opts)
	})
	if err != nil && c.cb.State() == circuitbreaker.Open {
		c.logger.WarnContext(ctx, "harbor circuit breaker open, skipping EnsureProject",
			"project", name, "cb_state", c.cb.State().String())
	}
	return err
}

// DeleteProject delegates to the inner provisioner with circuit breaker protection.
func (c *CircuitBreakerClient) DeleteProject(ctx context.Context, name string) error {
	_, err := circuitbreaker.Execute(ctx, c.cb, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, c.inner.DeleteProject(ctx, name)
	})
	if err != nil && c.cb.State() == circuitbreaker.Open {
		c.logger.WarnContext(ctx, "harbor circuit breaker open, skipping DeleteProject",
			"project", name, "cb_state", c.cb.State().String())
	}
	return err
}

// EnsureRobotAccount delegates to the inner provisioner with circuit breaker protection.
func (c *CircuitBreakerClient) EnsureRobotAccount(ctx context.Context, projectName string, opts RobotAccountOpts) (*RobotCredentials, error) {
	creds, err := circuitbreaker.Execute(ctx, c.cb, func(ctx context.Context) (*RobotCredentials, error) {
		return c.inner.EnsureRobotAccount(ctx, projectName, opts)
	})
	if err != nil && c.cb.State() == circuitbreaker.Open {
		c.logger.WarnContext(ctx, "harbor circuit breaker open, skipping EnsureRobotAccount",
			"project", projectName, "robot", opts.Name, "cb_state", c.cb.State().String())
	}
	return creds, err
}

// DeleteRobotAccount delegates to the inner provisioner with circuit breaker protection.
func (c *CircuitBreakerClient) DeleteRobotAccount(ctx context.Context, projectName string, robotName string) error {
	_, err := circuitbreaker.Execute(ctx, c.cb, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, c.inner.DeleteRobotAccount(ctx, projectName, robotName)
	})
	if err != nil && c.cb.State() == circuitbreaker.Open {
		c.logger.WarnContext(ctx, "harbor circuit breaker open, skipping DeleteRobotAccount",
			"project", projectName, "robot", robotName, "cb_state", c.cb.State().String())
	}
	return err
}

// Healthy delegates to the inner provisioner with circuit breaker protection.
func (c *CircuitBreakerClient) Healthy(ctx context.Context) error {
	_, err := circuitbreaker.Execute(ctx, c.cb, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, c.inner.Healthy(ctx)
	})
	if err != nil && c.cb.State() == circuitbreaker.Open {
		c.logger.WarnContext(ctx, "harbor circuit breaker open, skipping Healthy",
			"cb_state", c.cb.State().String())
	}
	return err
}

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
	if err := c.cb.PreCheck(); err != nil {
		c.logger.WarnContext(ctx, "harbor circuit breaker open, skipping EnsureProject",
			"project", name, "cb_state", c.cb.State().String())
		return err
	}
	err := c.inner.EnsureProject(ctx, name, opts)
	c.cb.RecordResult(err)
	return err
}

// DeleteProject delegates to the inner provisioner with circuit breaker protection.
func (c *CircuitBreakerClient) DeleteProject(ctx context.Context, name string) error {
	if err := c.cb.PreCheck(); err != nil {
		c.logger.WarnContext(ctx, "harbor circuit breaker open, skipping DeleteProject",
			"project", name, "cb_state", c.cb.State().String())
		return err
	}
	err := c.inner.DeleteProject(ctx, name)
	c.cb.RecordResult(err)
	return err
}

// EnsureRobotAccount delegates to the inner provisioner with circuit breaker protection.
func (c *CircuitBreakerClient) EnsureRobotAccount(ctx context.Context, projectName string, opts RobotAccountOpts) (*RobotCredentials, error) {
	if err := c.cb.PreCheck(); err != nil {
		c.logger.WarnContext(ctx, "harbor circuit breaker open, skipping EnsureRobotAccount",
			"project", projectName, "robot", opts.Name, "cb_state", c.cb.State().String())
		return nil, err
	}
	creds, err := c.inner.EnsureRobotAccount(ctx, projectName, opts)
	c.cb.RecordResult(err)
	return creds, err
}

// DeleteRobotAccount delegates to the inner provisioner with circuit breaker protection.
func (c *CircuitBreakerClient) DeleteRobotAccount(ctx context.Context, projectName string, robotName string) error {
	if err := c.cb.PreCheck(); err != nil {
		c.logger.WarnContext(ctx, "harbor circuit breaker open, skipping DeleteRobotAccount",
			"project", projectName, "robot", robotName, "cb_state", c.cb.State().String())
		return err
	}
	err := c.inner.DeleteRobotAccount(ctx, projectName, robotName)
	c.cb.RecordResult(err)
	return err
}

// Healthy delegates to the inner provisioner with circuit breaker protection.
func (c *CircuitBreakerClient) Healthy(ctx context.Context) error {
	if err := c.cb.PreCheck(); err != nil {
		c.logger.WarnContext(ctx, "harbor circuit breaker open, skipping Healthy",
			"cb_state", c.cb.State().String())
		return err
	}
	err := c.inner.Healthy(ctx)
	c.cb.RecordResult(err)
	return err
}

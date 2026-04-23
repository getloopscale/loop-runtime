// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package harbor

import "context"

// Provisioner defines the operations the Tenant controller needs from a Harbor
// registry. The concrete *Client implements this directly. The circuit breaker
// wrapper (CircuitBreakerClient) also implements it, allowing transparent
// protection against Harbor outages.
//
// When the value is nil in the reconciler, Harbor integration is disabled
// (backward compatibility with DOCR mode).
type Provisioner interface {
	// EnsureProject creates or updates a Harbor project. Idempotent.
	EnsureProject(ctx context.Context, name string, opts ProjectOpts) error

	// DeleteProject deletes a project and all its contents. Idempotent.
	DeleteProject(ctx context.Context, name string) error

	// EnsureRobotAccount creates or refreshes a robot account for a project.
	EnsureRobotAccount(ctx context.Context, projectName string, opts RobotAccountOpts) (*RobotCredentials, error)

	// DeleteRobotAccount deletes a robot account by name. Idempotent.
	DeleteRobotAccount(ctx context.Context, projectName string, robotName string) error

	// Healthy returns nil if Harbor is responding.
	Healthy(ctx context.Context) error
}

// Compile-time check: *Client implements Provisioner.
var _ Provisioner = (*Client)(nil)

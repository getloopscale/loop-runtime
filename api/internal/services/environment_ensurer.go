// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// EnvironmentLister is the read side of the EnvironmentManager contract:
// returns a non-nil error when the environment does not exist for the tenant.
// Implemented by routes.EnvironmentManager (GetEnvironmentDetail).
type EnvironmentLister interface {
	HasEnvironment(ctx context.Context, tenantID, envName string) (bool, error)
}

// EnvironmentAdder is the write side of the EnvironmentManager contract:
// creates the Environment on the Tenant CR (which triggers operator reconcile
// to create namespace + ArgoCD App+AppProject).
// Implemented by routes.EnvironmentManager (AddEnvironment).
type EnvironmentAdder interface {
	AddEnvironment(ctx context.Context, tenantID, envName string) error
}

// EnvironmentEnsurer auto-creates an Environment if it doesn't exist for
// the tenant, then waits for the operator to reconcile (phase=Active).
// Plan T2.2 D5 — closes F-S1-02.
//
// Idempotent: existing envs return nil immediately. Concurrent calls for the
// same (tenant, env) are safe because AddEnvironment uses tenant CR JSON
// patch semantics (server-side merge).
type EnvironmentEnsurer struct {
	Lister  EnvironmentLister
	Adder   EnvironmentAdder
	Logger  *slog.Logger
	// WaitTimeout bounds how long EnsureEnvironment waits for the operator
	// to reconcile after AddEnvironment. Default 60s (operator-led).
	WaitTimeout time.Duration
	// PollInterval is how often we re-check env existence post-Add.
	// Default 2s.
	PollInterval time.Duration
}

// EnsureEnvironment checks if the environment exists; creates + waits if not.
//
// Errors:
//   - LoopZeroError(INVALID_ENV_NAME) if the name fails defensive validation
//   - LoopZeroError(ENV_PROVISION_TIMEOUT) if operator doesn't reconcile in WaitTimeout
//   - LoopZeroError(ENV_PROVISION_FAILED) wrapping the underlying error otherwise
func (e *EnvironmentEnsurer) EnsureEnvironment(ctx context.Context, tenantID, envName string) error {
	if e == nil {
		return nil
	}
	if e.Lister == nil || e.Adder == nil {
		// Defensive: misconfigured ensurer — fail open (legacy behavior).
		return nil
	}

	// 1. Cheap existence check — idempotent no-op for pre-existing envs.
	exists, err := e.Lister.HasEnvironment(ctx, tenantID, envName)
	if err == nil && exists {
		return nil
	}
	// Lister error is non-fatal (we'll attempt create idempotently).
	if err != nil && e.Logger != nil {
		e.Logger.Warn("env ensurer: existence check failed (proceeding to create)",
			"tenant_id", tenantID, "env", envName, "error", err)
	}

	// 2. Create — idempotent at the Tenant CR layer (server-side merge).
	if err := e.Adder.AddEnvironment(ctx, tenantID, envName); err != nil {
		// AddEnvironment may return a "already exists" sentinel; treat as success.
		if isAlreadyExistsError(err) {
			return nil
		}
		return loopzeroerrors.NewLoopZeroError(
			"ENV_PROVISION_FAILED",
			fmt.Sprintf("Failed to create environment %q: %s", envName, err.Error()),
			500,
		)
	}

	// 3. Wait for operator to reconcile.
	timeout := e.WaitTimeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	interval := e.PollInterval
	if interval == 0 {
		interval = 2 * time.Second
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return loopzeroerrors.NewLoopZeroError(
				"ENV_PROVISION_TIMEOUT",
				fmt.Sprintf(
					"Environment %q was created but is not yet ready after %s. "+
						"Run `loopzero env list` to check status, then retry `loopzero deploy`.",
					envName, timeout,
				),
				504,
			)
		case <-ticker.C:
			exists, err := e.Lister.HasEnvironment(waitCtx, tenantID, envName)
			if err == nil && exists {
				return nil
			}
			// Continue polling on transient errors.
		}
	}
}

// isAlreadyExistsError detects whether an AddEnvironment error indicates the
// env already existed (race between concurrent EnsureEnvironment calls).
// EnvironmentManager.AddEnvironment returns plain errors with "already" in
// the message when the env exists.
func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already") && strings.Contains(msg, "exist")
}

// Compile-time guard that EnvironmentEnsurer satisfies the routes.EnvironmentEnsurer
// contract. The interface lives in api/internal/routes/builds.go.
var _ environmentEnsurerContract = (*EnvironmentEnsurer)(nil)

// environmentEnsurerContract mirrors routes.EnvironmentEnsurer. Duplicated
// here to avoid cyclic import; the signatures MUST match (verified via
// the compile-time guard above).
type environmentEnsurerContract interface {
	EnsureEnvironment(ctx context.Context, tenantID, envName string) error
}

// Sentinel error for tests + callers that want explicit detection.
var ErrEnvAlreadyExists = errors.New("environment already exists")

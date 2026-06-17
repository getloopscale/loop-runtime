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

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
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
//
// M2 v1.2 (2026-06-07): `project` is propagated end-to-end so the Tenant CR
// spec.environments entry carries the field expected by the canonical
// 3-segment namespace `loopzero-<tenant>-<project>-<env>`. Empty project
// preserves the legacy 2-segment behaviour (ADR D2 — production legacy).
type EnvironmentAdder interface {
	AddEnvironment(ctx context.Context, tenantID, project, envName string) error
}

// EnvironmentReadinessChecker is the deep-readiness side: queries the
// Environment CR `status.phase` directly. Returns true only after the
// environment-controller has finished provisioning namespace + RBAC +
// NetworkPolicy + ResourceQuota (CR phase=="Active"). Causally ordered
// AFTER the tenant-controller updates AppProject destinations, so a single
// "Active" signal is sufficient to gate Submit.
//
// Optional dependency — when nil, EnsureEnvironment falls back to the
// legacy shallow check (Lister.HasEnvironment only). Test fakes can pass
// nil to preserve existing unit-test surface area.
//
// Sprint A trio-fix 2026-06-01: introduced to close the race between
// API → AddEnvironment → poll (returns true on Tenant CR spec match) →
// Submit build → ArgoCD App stuck InvalidSpecError because AppProject
// destinations hadn't been updated by tenant-controller yet. See
// environment_cr_reader.go for the canonical implementation.
type EnvironmentReadinessChecker interface {
	// M2 v1.2: `project` selects the canonical 3-segment namespace
	// `loopzero-<tenant>-<project>-<env>` whose Environment CR
	// `status.phase` is probed. Empty project preserves the legacy
	// 2-segment lookup (`loopzero-<tenant>-<env>`) for production-legacy
	// tuples (ADR D2).
	IsReady(ctx context.Context, tenantID, project, envName string) (bool, error)
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
	// ReadinessChecker is the optional deep-readiness gate (Environment CR
	// status.phase). When set, EnsureEnvironment polls for BOTH the shallow
	// Lister check AND IsReady before returning success. Default nil =
	// legacy behavior. Sprint A trio-fix 2026-06-01.
	ReadinessChecker EnvironmentReadinessChecker
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
func (e *EnvironmentEnsurer) EnsureEnvironment(ctx context.Context, tenantID, project, envName string) error {
	if e == nil {
		return nil
	}
	if e.Lister == nil || e.Adder == nil {
		// Defensive: misconfigured ensurer — fail open (legacy behavior).
		return nil
	}

	// 1. Cheap existence check — idempotent no-op for pre-existing envs.
	// M2 v1.2 NOTE: HasEnvironment signature carries env-only by design.
	// The Lister (TenantCRManager.GetEnvironmentDetail-derived) returns
	// existence for the tenant-wide env name; per-(project, env) tuple
	// uniqueness is enforced at AddEnvironment via the operator (Tenant
	// CR JSON patch). The shallow check is intentionally project-agnostic.
	exists, err := e.Lister.HasEnvironment(ctx, tenantID, envName)
	if err == nil && exists {
		return nil
	}
	// Lister error is non-fatal (we'll attempt create idempotently).
	if err != nil && e.Logger != nil {
		e.Logger.Warn("env ensurer: existence check failed (proceeding to create)",
			"tenant_id", tenantID, "project", project, "env", envName, "error", err)
	}

	// 2. Create — idempotent at the Tenant CR layer (server-side merge).
	// M2 v1.2: project is propagated so the Tenant CR
	// spec.environments entry carries the project field used by the
	// operator to compute the canonical 3-segment namespace.
	if err := e.Adder.AddEnvironment(ctx, tenantID, project, envName); err != nil {
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
			if err != nil || !exists {
				// Continue polling on transient errors or env not yet present.
				continue
			}
			// Sprint A trio-fix 2026-06-01: shallow check (env in
			// tenant.spec.environments) is necessary but NOT sufficient.
			// Without the deep check, API returned here BEFORE the
			// tenant-controller updated AppProject destinations →
			// downstream ArgoCD App stuck InvalidSpecError → SYNC_FAILED.
			// The ReadinessChecker queries Environment CR.status.phase
			// which the environment-controller sets to "Active" only
			// after namespace + RBAC + NetworkPolicy + ResourceQuota
			// + (causally) AppProject destinations are all in place.
			// Nil checker (test fixtures, legacy boot) skips the gate.
			if e.ReadinessChecker == nil {
				return nil
			}
			ready, rerr := e.ReadinessChecker.IsReady(waitCtx, tenantID, project, envName)
			if rerr != nil {
				// Treat readiness errors as transient — keep polling.
				if e.Logger != nil {
					e.Logger.Warn("env ensurer: readiness check error (continuing poll)",
						"tenant_id", tenantID, "project", project, "env", envName, "error", rerr)
				}
				continue
			}
			if ready {
				return nil
			}
			// Not ready yet — keep polling.
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
	EnsureEnvironment(ctx context.Context, tenantID, project, envName string) error
}

// Sentinel error for tests + callers that want explicit detection.
var ErrEnvAlreadyExists = errors.New("environment already exists")

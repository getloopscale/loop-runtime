// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package environments is the environment feature: the environments of a
// project, created on first deploy (EnvironmentEnsurer) and served by
// environmentshttp.
package environments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// EnvironmentLister is the read side of the EnvironmentManager contract:
// returns true when the (project, env) tuple exists in the tenant's
// spec.environments. Implemented by routes.EnvironmentManager
// (GetEnvironmentDetail).
//
// M2 v1.2 plan m37-tenant-cr-project-sync-canonical-fix (2026-06-25):
// the shallow check is per-(project, env) tuple, NOT project-agnostic.
// The pre-m37 project-agnostic semantic silently swallowed new-project
// deploys against tenants that already had the same env name on a
// different project — Tenant CR never gained the new tuple,
// AppProject destinations never updated, ArgoCD sync failed with
// "namespace not permitted in project". Closes #37 scenario 4.
//
// Legacy production tuples (pre-M2) have empty Project field; calling
// HasEnvironment with `project=""` matches only those legacy entries
// (ADR D2 of M2 v1.2). See pkg/domain.EnvRef and
// TenantCRManager.GetEnvironmentDetail for the canonical impl.
type EnvironmentLister interface {
	HasEnvironment(ctx context.Context, tenantID, project, envName string) (bool, error)
}

// EnvironmentAdder is the write side of the EnvironmentManager contract:
// creates the Environment on the Tenant CR (which triggers operator reconcile
// to create namespace + ArgoCD App+AppProject).
// Implemented by routes.EnvironmentManager (AddEnvironment).
//
// M2 v1.2 (2026-06-07): `project` is propagated end-to-end so the Tenant CR
// spec.environments entry carries the field expected by the canonical
// 3-segment namespace `loopruntime-<tenant>-<project>-<env>`. Empty project
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
// environmentsadapter.EnvironmentCRReader for the Kubernetes implementation.
type EnvironmentReadinessChecker interface {
	// M2 v1.2: `project` selects the canonical 3-segment namespace
	// `loopruntime-<tenant>-<project>-<env>` whose Environment CR
	// `status.phase` is probed. Empty project preserves the legacy
	// 2-segment lookup (`loopruntime-<tenant>-<env>`) for production-legacy
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
	Lister EnvironmentLister
	Adder  EnvironmentAdder
	Logger *slog.Logger
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

// ErrEnvironmentEnsurerMisconfigured is returned by an ensurer wired without
// its Lister or Adder.
var ErrEnvironmentEnsurerMisconfigured = &loopruntimeerrors.LoopRuntimeError{
	Code:    loopruntimeerrors.ErrInternalError.Code,
	Message: "environment ensurer: Lister and Adder are required",
}

// EnsureEnvironment checks if the environment exists; creates + waits if not.
//
// Errors:
//   - LoopRuntimeError(INVALID_ENV_NAME) if the name fails defensive validation
//   - LoopRuntimeError(ENV_PROVISION_TIMEOUT) if operator doesn't reconcile in WaitTimeout
//   - LoopRuntimeError(ENV_PROVISION_FAILED) wrapping the underlying error otherwise
func (e *EnvironmentEnsurer) EnsureEnvironment(ctx context.Context, tenantID, project, envName string) error {
	// Callers skip an ensurer that is not wired at all (a nil interface). One
	// that IS wired but lacks a dependency is a wiring bug, and reporting
	// success would let the deploy proceed as if the environment existed.
	if e == nil || e.Lister == nil || e.Adder == nil {
		return ErrEnvironmentEnsurerMisconfigured
	}

	// 1. Cheap existence check — idempotent no-op for pre-existing tuples.
	// m37 plan canonical fix (2026-06-25): the shallow check is now
	// per-(project, env) tuple. The pre-m37 project-agnostic check
	// silently returned exists=true when the SAME env name existed on
	// a DIFFERENT project for the same tenant → AddEnvironment was
	// skipped → Tenant CR spec.environments[] never gained the new
	// tuple → AppProject destinations never extended → ArgoCD sync
	// failed with "namespace not permitted in project". Closes #37
	// scenario 4 (BUILD → RELEASE → URL chain). The downstream
	// AddEnvironment retains its server-side merge idempotency as
	// defense in depth.
	exists, err := e.Lister.HasEnvironment(ctx, tenantID, project, envName)
	if err == nil && exists {
		return nil
	}
	// Lister error is non-fatal (we'll attempt create idempotently).
	if err != nil && e.Logger != nil {
		e.Logger.Warn("env ensurer: existence check failed (proceeding to create)",
			"tenant_id", tenantID, "project", project, "env", envName, "error", err)
	}

	// 2. Create — idempotent at the Tenant CR layer (server-side merge): an
	// entry already present returns nil, so any error here is a failure.
	// M2 v1.2: project is propagated so the Tenant CR
	// spec.environments entry carries the project field used by the
	// operator to compute the canonical 3-segment namespace.
	if err := e.Adder.AddEnvironment(ctx, tenantID, project, envName); err != nil {
		// A concurrent ensure (or a stale existence check) added it first:
		// the environment exists, which is all this call wants.
		if errors.Is(err, ports.ErrEnvironmentExists) {
			return nil
		}
		return loopruntimeerrors.NewLoopRuntimeError(
			"ENV_PROVISION_FAILED",
			fmt.Sprintf("Failed to create environment %q: %s", envName, err.Error()))
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
			return loopruntimeerrors.NewLoopRuntimeError(
				"ENV_PROVISION_TIMEOUT",
				fmt.Sprintf(
					"Environment %q was created but is not yet ready after %s. "+
						"Run `loopscale env list` to check status, then retry `loopscale deploy`.",
					envName, timeout,
				))

		case <-ticker.C:
			exists, err := e.Lister.HasEnvironment(waitCtx, tenantID, project, envName)
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

// Compile-time guard that EnvironmentEnsurer satisfies the routes.EnvironmentEnsurer
// contract. The interface lives in api/internal/routes/builds.go.
var _ environmentEnsurerContract = (*EnvironmentEnsurer)(nil)

// environmentEnsurerContract mirrors routes.EnvironmentEnsurer. Duplicated
// here to avoid cyclic import; the signatures MUST match (verified via
// the compile-time guard above).
type environmentEnsurerContract interface {
	EnsureEnvironment(ctx context.Context, tenantID, project, envName string) error
}

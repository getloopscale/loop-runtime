// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secrets

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// namespaceTerminatingRetryWindow bounds how long ResyncEnv waits for a
// namespace stuck in `Terminating` state to be recreated before giving
// up. Namespace deletion + recreation typically completes in 10-30s
// (depending on finalizers + ESO operator cleanup). 60s gives ample
// headroom without blocking the deploy handler indefinitely.
//
// LIVE-caught dogfood-trio 2026-06-02: Journey 03 Secrets Lifecycle.
// Test sequence: deploy A → project delete (NS goes Terminating) →
// secret POST (resync tries to create ExternalSecret, namespace still
// terminating → forbidden) → deploy B (envEnsurer returns nil because
// env exists in Tenant CR, but ns not yet Active) → resync runs ONCE,
// fails on terminating, never retries → pod starts before ExternalSecret
// → CreateContainerConfigError → 5min timeout.
//
// Fix: retry the resync when the K8s error indicates the namespace is
// being terminated. The namespace WILL be recreated by the envEnsurer-
// triggered Tenant CR reconcile in the workload cluster; once Active,
// the create succeeds.
const namespaceTerminatingRetryWindow = 60 * time.Second
const namespaceTerminatingRetryInterval = 2 * time.Second

// SecretResyncer fetches secret rows from PG and (re)creates ExternalSecrets
// on the workload cluster via a K8s sync. V9 L20: closes the
// set-secret-BEFORE-first-deploy race documented in audit
// 2026-05-30-V9-L19-99percent.md.
//
// Used by routes.BuildHandler after envEnsurer.EnsureEnvironment so the
// workload namespace is guaranteed to exist before the resync runs.
type SecretResyncer struct {
	Store  SecretResyncStore
	K8s    SecretK8sCreator
	Logger *slog.Logger
}

// SecretResyncStore is the PG-side read contract — returns plaintext
// (name, app, value) tuples for resync. Implemented by
// store.PostgresSecretStore.ListForResync.
type SecretResyncStore interface {
	ListForResync(ctx context.Context, tenantID, projectID, env string) ([]store.SecretResyncEntry, error)
}

// SecretK8sCreator is the cell-0-cluster write contract — creates or
// updates ExternalSecrets. Same signature as the existing
// k8s.SecretK8sManager.CreateSecret used by POST /api/v1/secrets, so the
// resyncer can reuse the same wired implementation.
type SecretK8sCreator interface {
	CreateSecret(ctx context.Context, tenantID, projectID, env, app, name, value string) error
}

// ErrSecretResyncerMisconfigured is returned by a resyncer wired without its
// Store or K8s creator.
var ErrSecretResyncerMisconfigured = &loopzeroerrors.LoopZeroError{
	Code:    loopzeroerrors.ErrInternalError.Code,
	Message: "secret resyncer: Store and K8s are required",
}

// ResyncEnv lists all PG secrets in (tenant, project, env) and applies
// each to the workload cluster via K8s.CreateSecret. Idempotent:
// existing ExternalSecrets get an Update (per SecretK8sManager
// semantics), missing ones get Create.
//
// Per-secret errors are LOGGED but do NOT abort the resync — a single
// failing secret should not prevent the rest of the namespace from
// being repopulated. The deploy that triggered this resync will see a
// pod-start failure if a required secret remains missing.
func (r *SecretResyncer) ResyncEnv(ctx context.Context, tenantID, projectID, env string) error {
	// Callers skip a resyncer that is not wired at all (a nil interface). One
	// that IS wired but lacks a dependency is a wiring bug: reporting success
	// would re-introduce L20 — pods starting before their secrets exist.
	if r == nil || r.Store == nil || r.K8s == nil {
		return ErrSecretResyncerMisconfigured
	}
	entries, err := r.Store.ListForResync(ctx, tenantID, projectID, env)
	if err != nil {
		return fmt.Errorf("secret_resyncer: list: %w", err)
	}
	if len(entries) == 0 {
		return nil // nothing to resync
	}
	var firstErr error
	failed := 0
	for _, e := range entries {
		syncErr := r.syncOneWithTerminatingRetry(ctx, tenantID, projectID, env, e)
		if syncErr != nil {
			failed++
			if r.Logger != nil {
				r.Logger.Error("secret resync: ExternalSecret create/update failed",
					"tenant_id", tenantID,
					"project", projectID,
					"environment", env,
					"app", e.App,
					"secret_name", e.Name,
					"error", syncErr,
				)
			}
			if firstErr == nil {
				firstErr = syncErr
			}
		}
	}
	if firstErr != nil {
		return fmt.Errorf("secret_resyncer: %d/%d secrets failed to sync (first: %w)",
			failed, len(entries), firstErr)
	}
	if r.Logger != nil {
		r.Logger.Info("secret resync completed",
			"tenant_id", tenantID,
			"project", projectID,
			"environment", env,
			"count", len(entries),
		)
	}
	return nil
}

// syncOneWithTerminatingRetry wraps K8s.CreateSecret with bounded retry
// on the "namespace is being terminated" error class. The retry window
// (namespaceTerminatingRetryWindow) covers the typical NS deletion +
// recreation cycle (~10-30s in k3d, allow up to 60s for slow finalizers).
//
// Non-terminating errors are returned immediately — only the specific
// "is being terminated" admission error triggers retry, so genuine bugs
// (RBAC, network, malformed CR) fail fast.
//
// LIVE-caught dogfood-trio 2026-06-02 Journey 03 Secrets Lifecycle —
// see namespaceTerminatingRetryWindow comment for the full timeline.
func (r *SecretResyncer) syncOneWithTerminatingRetry(ctx context.Context, tenantID, projectID, env string, e store.SecretResyncEntry) error {
	deadline := time.Now().Add(namespaceTerminatingRetryWindow)
	attempt := 0
	for {
		attempt++
		err := r.K8s.CreateSecret(ctx, tenantID, projectID, env, e.App, e.Name, e.Value)
		if err == nil {
			if attempt > 1 && r.Logger != nil {
				r.Logger.Info("secret resync: succeeded after namespace-terminating retry",
					"tenant_id", tenantID,
					"project", projectID,
					"environment", env,
					"app", e.App,
					"secret_name", e.Name,
					"attempts", attempt,
				)
			}
			return nil
		}
		// Only retry on the specific "is being terminated" admission error.
		// Other errors (RBAC, malformed CR, network) fail fast.
		if !isNamespaceTerminatingError(err) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("namespace stayed terminating beyond %s window: %w", namespaceTerminatingRetryWindow, err)
		}
		if r.Logger != nil && attempt == 1 {
			r.Logger.Info("secret resync: namespace terminating, will retry (envEnsurer recreates)",
				"tenant_id", tenantID,
				"project", projectID,
				"environment", env,
				"app", e.App,
				"secret_name", e.Name,
				"retry_window", namespaceTerminatingRetryWindow.String(),
			)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(namespaceTerminatingRetryInterval):
		}
	}
}

// isNamespaceTerminatingError detects the Kubernetes admission error
// returned when a request creates a resource in a namespace whose
// `metadata.deletionTimestamp` is set (NS in Terminating phase).
//
// The NamespaceLifecycle admission plugin attaches the typed
// NamespaceTerminating status cause to that Forbidden error (documented on
// corev1.NamespaceTerminatingCause). Matching the cause, not the message
// text, keeps RBAC and PSA Forbidden errors — and any unrelated error whose
// text says "is being terminated" — out of the retry window.
func isNamespaceTerminatingError(err error) bool {
	return apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause)
}

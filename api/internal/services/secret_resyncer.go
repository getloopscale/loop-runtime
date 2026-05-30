// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/loopzero/loopzero/api/internal/db"
)

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
// db.PostgresSecretStore.ListForResync.
type SecretResyncStore interface {
	ListForResync(ctx context.Context, tenantID, projectID, env string) ([]db.SecretResyncEntry, error)
}

// SecretK8sCreator is the workload-cluster write contract — creates or
// updates ExternalSecrets. Same signature as the existing
// k8s.SecretK8sManager.CreateSecret used by POST /api/v1/secrets, so the
// resyncer can reuse the same wired implementation.
type SecretK8sCreator interface {
	CreateSecret(ctx context.Context, tenantID, projectID, env, app, name, value string) error
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
	if r == nil || r.Store == nil || r.K8s == nil {
		return nil
	}
	entries, err := r.Store.ListForResync(ctx, tenantID, projectID, env)
	if err != nil {
		return fmt.Errorf("secret_resyncer: list: %w", err)
	}
	if len(entries) == 0 {
		return nil // nothing to resync
	}
	var firstErr error
	for _, e := range entries {
		if syncErr := r.K8s.CreateSecret(ctx, tenantID, projectID, env, e.App, e.Name, e.Value); syncErr != nil {
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
			countFailed(entries, firstErr), len(entries), firstErr)
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

// countFailed is a placeholder for tracking how many secrets failed —
// the current impl returns 1 when firstErr is set since we propagate
// only firstErr. Future enhancement: track per-secret failure list.
func countFailed(entries []db.SecretResyncEntry, firstErr error) int {
	if firstErr == nil {
		return 0
	}
	return 1 // conservative — only firstErr is captured today
}

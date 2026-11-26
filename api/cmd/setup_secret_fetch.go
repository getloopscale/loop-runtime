// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// setup_secret_fetch.go — wires the Phase 4 broker endpoint
// `/internal/v1/secret-fetch/{tenant_id}/{secret_name}` to its three
// runtime dependencies (TenantCellBindingLookup, InternalSecretReader,
// SecretReadAuditWriter). Built in the 2026-06-10 J-22 walkthrough at the
// sponsor's request to finish implementing and close out the secrets
// system. Plan 2026-06-02-openbao-adoption v1.3 § T4.1
// declares the handler; this file completes Phase 4 by composing the
// production deps that the LIVE walkthrough surfaced as un-wired.

package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/config"
	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/pkg/vault"

	"github.com/jackc/pgx/v5"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/secrets/secretshttp"
)

// tenantCellQuerier is the narrow surface inlineTenantBinding uses on
// store.PostgresCellStore, so tests can supply a fake.
type tenantCellQuerier interface {
	GetTenantCellID(ctx context.Context, tenantID string) (string, error)
}

// secretValueReader is the narrow surface the broker reads secrets through.
// *store.PostgresSecretStore implements it with ReadValue, which decrypts via
// the store's one read-path dispatcher.
type secretValueReader interface {
	ReadValue(ctx context.Context, tenantID, projectID, env, name string) ([]byte, error)
}

// brokerSecretReader implements secrets.InternalSecretReader for the endpoint
// External Secrets Operator calls. It holds no SQL and no decrypt logic of its
// own: the `secrets` table is read only through the secret store, so the broker
// cannot drift from the store's encryption policy or its
// SecretDecryptFailedTotal reasons (it had, when this was a raw query here).
//
// EC-07 cross-tenant binding is enforced at the handler layer before this
// reader is invoked, so (tenantID, secretName) is trusted here.
type brokerSecretReader struct {
	store secretValueReader
}

// ReadSecret satisfies secrets.InternalSecretReader, scoped by the composite
// key (tenant, project, environment, name) — ADR D2 of
// 2026-06-10-j22-layer2-code-review-fixes. An absent secret maps to
// secrets.ErrSecretNotFound (404) through any wrapping.
func (r *brokerSecretReader) ReadSecret(ctx context.Context, tenantID, project, env, secretName string) ([]byte, error) {
	plain, err := r.store.ReadValue(ctx, tenantID, project, env, secretName)
	if errors.Is(err, store.ErrSecretNotFound) {
		return nil, secretshttp.ErrSecretNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("secret_fetch: %w", err)
	}
	return plain, nil
}

// Compile-time interface assertion.
var _ secretshttp.InternalSecretReader = (*brokerSecretReader)(nil)

// inlineTenantBinding satisfies secrets.TenantCellBindingLookup using the
// existing PostgresCellStore.GetTenantCellID.
type inlineTenantBinding struct {
	cellStore tenantCellQuerier
}

// GetTenantCellID satisfies secrets.TenantCellBindingLookup.
//
// EC-8 follow-up absorption (plan 2026-06-10-j22-layer2-code-review-fixes):
// canonical pgx no-rows detection via `errors.Is(pgx.ErrNoRows)` so the
// mapping survives intermediate wrapping (PostgresCellStore wraps once
// via `fmt.Errorf("cell store: get tenant cell_id: %w", err)`). The
// prior `strings.Contains(err.Error(), "not found")` was the parallel
// fragile pattern that EC-8 closed for the pgx.ErrNoRows path in
// the broker's secret read; closing it here too prevents any future
// intermediate wrapper from silently turning a 404 NOT_FOUND into a
// 500 INTERNAL.
func (b *inlineTenantBinding) GetTenantCellID(ctx context.Context, tenantID string) (string, error) {
	if b == nil || b.cellStore == nil {
		return "", errors.New("secret_fetch: cell store not configured")
	}
	cell, err := b.cellStore.GetTenantCellID(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", secretshttp.ErrTenantNotFound
		}
		return "", fmt.Errorf("secret_fetch: tenant binding: %w", err)
	}
	return cell, nil
}

// Compile-time interface assertion.
var _ secretshttp.TenantCellBindingLookup = (*inlineTenantBinding)(nil)

// setupSecretFetchDeps composes the three runtime dependencies for the
// broker handler. Returns nil when any prerequisite is missing so the
// caller can skip route registration cleanly (LIVE 404 → ESO retries).
func setupSecretFetchDeps(secretStore secretValueReader, cellStore tenantCellQuerier, auditLogger *store.AuditLogger) *secretshttp.SecretFetchHandlerDeps {
	if secretStore == nil || cellStore == nil || auditLogger == nil {
		return nil
	}
	return &secretshttp.SecretFetchHandlerDeps{
		TenantBinding: &inlineTenantBinding{cellStore: cellStore},
		SecretReader:  &brokerSecretReader{store: secretStore},
		AuditWriter:   secretshttp.NewSecretReadAuditAdapter(auditLogger),
	}
}

// brokerBearerMiddleware authenticates the External Secrets webhook of each cell by the
// bearer it presents: every cell has its own token, and the cell a request comes from is
// the cell whose token matched (#456). The handler then binds the tenant to that cell
// (EC-07), so a cell's credential can read only the tenants placed on it.
//
// The caller-supplied X-Cell-ID header is ignored — trusting it would let any holder of a
// broker token claim any cell (consensus-CRITICAL #13). Every configured token is compared
// in constant time, whether or not an earlier one matched, so the time taken does not say
// which cell's token was close. No configured token fails closed with 500.
func brokerBearerMiddleware(cellTokens map[string]string, next http.HandlerFunc) http.HandlerFunc {
	type cellToken struct{ cell, token string }
	tokens := make([]cellToken, 0, len(cellTokens))
	for cell, token := range cellTokens {
		if token != "" {
			tokens = append(tokens, cellToken{cell, token})
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if len(tokens) == 0 {
			http.Error(w, `{"error":"broker_token_not_configured","code":"INTERNAL"}`, http.StatusInternalServerError)
			return
		}
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			http.Error(w, `{"error":"missing_bearer","code":"AUTH_TOKEN_INVALID"}`, http.StatusUnauthorized)
			return
		}
		presented := []byte(strings.TrimPrefix(authHeader, prefix))
		cellID := ""
		for _, ct := range tokens {
			if subtle.ConstantTimeCompare(presented, []byte(ct.token)) == 1 {
				cellID = ct.cell
			}
		}
		if cellID == "" {
			http.Error(w, `{"error":"bad_bearer","code":"AUTH_TOKEN_INVALID"}`, http.StatusUnauthorized)
			return
		}
		// Inject a synthetic TokenReviewResult so the handler's
		// auth.CrossClusterFromContext check still passes.
		ctx := auth.InjectCrossClusterResultForBroker(r.Context(), &auth.TokenReviewResult{
			Authenticated: true,
			Username:      "broker:eso-webhook",
			CellID:        cellID,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// newSecretFetchBroker serves /internal/v1/secret-fetch/{tenant_id}/{name}
// for ESO's webhook ClusterSecretStore: it loads the vault:v1: ciphertext,
// decrypts it through OpenBao transit and returns plaintext_b64, behind the
// broker bearer token. Nil (route not registered) when transit, the database
// or the cross-tenant pool is missing, or when no broker token is configured.
//
// The cell binding reads which cell a tenant is placed on BEFORE the caller is
// authorized for that tenant, so it reads through the cross-tenant pool: over
// the request pool, with no scope yet, the tenant row is hidden by fail-closed
// RLS and every fetch answered 404 tenant_not_found. The secret read and the
// audit record run on the request pool, scoped by the handler once the binding
// has authorized the caller.
func newSecretFetchBroker(cfg config.Config, transit vault.Transit, dbAdapter, crossTenant *db.PgxAdapter,
	stores *StoreDeps, logger *slog.Logger) http.HandlerFunc {
	if transit == nil || dbAdapter == nil || crossTenant == nil || stores == nil {
		return nil
	}
	placement := &store.PostgresCellStore{DB: crossTenant}
	fetchDeps := setupSecretFetchDeps(stores.PgSecretStore, placement, &store.AuditLogger{DB: dbAdapter})
	if fetchDeps == nil {
		return nil
	}
	cellTokens, err := cfg.BrokerCellTokens()
	if err != nil {
		logger.Error("secret-fetch broker disabled: invalid broker tokens", "component", "broker", "error", err)
		return nil
	}
	if len(cellTokens) == 0 {
		logger.Warn("secret-fetch broker disabled: neither LOOPZERO_BROKER_TOKEN nor LOOPZERO_BROKER_TOKENS is set", "component", "broker")
		return nil
	}
	logger.Info("secret-fetch broker serving cells", "component", "broker", "cells", len(cellTokens))
	return brokerBearerMiddleware(cellTokens, secretshttp.NewSecretFetchHandler(*fetchDeps))
}

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

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/pkg/vault"

	"github.com/jackc/pgx/v5"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/placement"
	"github.com/getloopscale/loopzero/api/internal/secrets/secretshttp"
)

// brokerSingleClusterCellID names the cell the broker bearer middleware
// MUST inject into the synthetic TokenReviewResult for dev-public single
// cluster. It references `placement.DefaultCellID` as the canonical
// single source of truth (T1.1 of plan 2026-06-10-j22-layer2-code-review-fixes
// ADR D1 + EC-3 absorption — closes consensus-CRITICAL #13 X-Cell-ID
// forgery + the magic-literal `"cell-0"` sub-finding). There is no
// multi-cluster broker authentication yet: the broker serves this one cell,
// and serving another needs a per-cell TokenReview (ADR D1 follow-up).
const brokerSingleClusterCellID = placement.DefaultCellID

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

// brokerBearerMiddleware authenticates incoming broker requests using a
// constant-time comparison against a Bearer token shared between
// loopzero-api and the ESO ClusterSecretStore webhook backend. The token is
// supplied via LOOPZERO_BROKER_TOKEN; the matching value is mounted into ESO
// via a K8s Secret (`loopzero-broker-token`) — same opaque string both sides.
//
// Per `feedback_security_first_mandate`: this is the medium-strong path
// in the {TokenReview, mTLS, Bearer-via-Secret, env-shared} hierarchy.
// It is auditable (rotate by re-rolling the K8s Secret) and operationally
// simple — both consumers read from a single source of truth (OpenBao via
// ExternalSecrets in production, or directly from the Secret in dev).
//
// Successful auth injects a synthetic auth.TokenReviewResult into
// the request context so the existing handler at routes/internal_secret_fetch.go
// (which calls auth.CrossClusterFromContext) does not need a parallel
// auth code path. CellID is derived SERVER-SIDE from the package-canonical
// `placement.DefaultCellID` constant — the caller-supplied X-Cell-ID
// header is intentionally ignored (T1.1 of plan
// 2026-06-10-j22-layer2-code-review-fixes / ADR D1 / closes consensus-CRITICAL
// #13 X-Cell-ID forgery). A multi-cluster path would replace this with a
// per-cell TokenReview against the cell's K8s API; none exists yet.
func brokerBearerMiddleware(expectedToken string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if expectedToken == "" {
			http.Error(w, `{"error":"broker_token_not_configured","code":"INTERNAL"}`, http.StatusInternalServerError)
			return
		}
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			http.Error(w, `{"error":"missing_bearer","code":"AUTH_TOKEN_INVALID"}`, http.StatusUnauthorized)
			return
		}
		presented := strings.TrimPrefix(authHeader, prefix)
		// Constant-time compare to avoid timing-side-channel leaks.
		if subtle.ConstantTimeCompare([]byte(presented), []byte(expectedToken)) != 1 {
			http.Error(w, `{"error":"bad_bearer","code":"AUTH_TOKEN_INVALID"}`, http.StatusUnauthorized)
			return
		}
		// T1.1 / ADR D1 / closes #13: the caller-supplied X-Cell-ID
		// header is intentionally ignored. Trusting it would let any
		// holder of LOOPZERO_BROKER_TOKEN spoof any cell and read every
		// tenant's secrets (consensus-CRITICAL of /loop-code-review).
		// The cellID is derived server-side from the package-canonical
		// `placement.DefaultCellID` constant. A multi-cluster path
		// (per-cell TokenReview) is a follow-up that does not exist yet.
		_ = r.Header.Get("X-Cell-ID")
		cellID := brokerSingleClusterCellID
		// Inject synthetic TokenReviewResult so the handler's
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
// or the cell store is missing, or when no broker token is configured.
func newSecretFetchBroker(cfg config.Config, transit vault.Transit, dbAdapter *db.PgxAdapter,
	cellStore *store.PostgresCellStore, stores *StoreDeps, logger *slog.Logger) http.HandlerFunc {
	if transit == nil || dbAdapter == nil || cellStore == nil || stores == nil {
		return nil
	}
	fetchDeps := setupSecretFetchDeps(stores.PgSecretStore, cellStore, &store.AuditLogger{DB: dbAdapter})
	if fetchDeps == nil {
		return nil
	}
	if cfg.BrokerToken == "" {
		logger.Warn("secret-fetch broker disabled: LOOPZERO_BROKER_TOKEN not set", "component", "broker")
		return nil
	}
	return brokerBearerMiddleware(cfg.BrokerToken, secretshttp.NewSecretFetchHandler(*fetchDeps))
}

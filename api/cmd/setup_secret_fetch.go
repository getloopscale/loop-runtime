// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// setup_secret_fetch.go — wires the Phase 4 broker endpoint
// `/internal/v1/secret-fetch/{tenant_id}/{secret_name}` to its three
// runtime dependencies (TenantCellBindingLookup, InternalSecretReader,
// SecretReadAuditWriter). Built 2026-06-10 J-22 walkthrough per sponsor
// verbatim "Voce deve terminar de implementar, precisamos fechar nosso
// sistema de Scrects". Plan 2026-06-02-openbao-adoption v1.3 § T4.1
// declares the handler; this file completes Phase 4 by composing the
// production deps that the LIVE walkthrough surfaced as un-wired.

package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/placement"
	"github.com/getloopscale/loopzero/api/internal/routes/secrets"
	"github.com/getloopscale/loopzero/pkg/vault"
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

// pgxQuerier is the narrow surface rawSecretReader uses on pgxpool.Pool.
// T3.1 of plan 2026-06-10-j22-layer2-code-review-fixes (ADR D3): defined
// as interface so tests can supply a fake without adding the pgxmock
// dependency (the plan's deps-audit verdict was PASS_WITH_CAVEATS; adding
// a new test dep would have re-triggered the audit). *pgxpool.Pool
// implements this implicitly — production callers are unchanged.
type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// tenantCellQuerier is the narrow surface inlineTenantBinding uses on
// store.PostgresCellStore. Same DIP rationale as pgxQuerier above.
type tenantCellQuerier interface {
	GetTenantCellID(ctx context.Context, tenantID string) (string, error)
}

// rawSecretReader implements secrets.InternalSecretReader using a raw SQL
// query against the secrets table. We bypass the PostgresSecretStore for
// this read path because the existing store only exposes Set/Delete/List
// methods (no single-secret Get); adding a fourth Get method to the store
// would broaden the interface that all consumers depend on. The raw query
// keeps the broker read path isolated.
//
// EC-07 cross-tenant binding is enforced at the handler layer before this
// reader is invoked, so (tenantID, secretName) is trusted here.
type rawSecretReader struct {
	pool    pgxQuerier
	transit vault.Transit
}

// ReadSecret satisfies secrets.InternalSecretReader. T2.1 + ADR D2 of
// plan 2026-06-10-j22-layer2-code-review-fixes (closes HIGH #15 scope-drop):
// SQL query scopes by the canonical composite unique key
// (tenant_id, project, environment, name). The previous query scoped only
// by (tenant_id, name) which silently leaked production-env secret values
// into preview-env Secret materializations when the same `name` existed
// across envs.
//
// B-175: every decrypt failure here increments
// metrics.SecretDecryptFailedTotal under the SAME reason vocabulary
// PostgresSecretStore.decryptFromStore uses, so one PromQL query covers both
// readers. It matters more here than there: this is the endpoint External
// Secrets Operator calls for every user secret, while the instrumented store
// dispatcher is reached only by ListForResync, a deploy-time hook. Between
// B-126 and this item the counter existed, read zero on a revoked key, and an
// operator would have concluded nothing had broken.
func (r *rawSecretReader) ReadSecret(ctx context.Context, tenantID, project, env, secretName string) ([]byte, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("secret_fetch: pool not configured")
	}
	if r.transit == nil {
		return nil, errors.New("secret_fetch: transit client not configured")
	}

	// secrets table contract (verified LIVE 2026-06-10 against the running
	// schema): (tenant_id, project, environment, app, name) unique key;
	// encrypted_value is bytea. T2.1 scopes by (tenant_id, project, env, name)
	// — app is NOT in the URL contract per ADR D2 (alternatives considered:
	// encoding app into secret_name breaks the EC-08 regex; passing as 5th
	// URL segment forecloses per-env tenant-wide secrets). Same-name secrets
	// across apps within the same env collapse to LIMIT 1 by design.
	var encryptedValue []byte
	err := r.pool.QueryRow(ctx,
		`SELECT encrypted_value FROM secrets
		 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 AND name = $4
		 LIMIT 1`,
		tenantID, project, env, secretName,
	).Scan(&encryptedValue)
	if err != nil {
		// T4.2 + EC-8 absorption (plan 2026-06-10-j22-layer2-code-review-fixes):
		// canonical pgx no-rows detection via `errors.Is(pgx.ErrNoRows)` so
		// the mapping survives intermediate `fmt.Errorf("...: %w", err)`
		// wrappers (the prior `strings.Contains(err.Error(), "no rows")`
		// was fragile — any future wrapper that rephrased the message
		// would silently turn a 404 NOT_FOUND into a 500 INTERNAL).
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, secrets.ErrSecretNotFound
		}
		return nil, fmt.Errorf("secret_fetch: load: %w", err)
	}

	// B-175: the three exits below are the decrypt failures on this path, and
	// each carries the reason that names the recovery it calls for. Two error
	// exits are deliberately NOT counted, because neither is a decrypt: the
	// "not configured" guards above run only when setupSecretFetchDeps handed
	// back nil and the route was never registered, and ErrSecretNotFound means
	// a row that is absent — absent is not ciphertext, and counting it would
	// make the alert fire on ordinary "the user has not created that secret
	// yet" traffic.

	if len(encryptedValue) == 0 {
		// Data side, mirroring the store dispatcher's EC-06 NULL/empty guard:
		// a row exists whose encrypted_value is empty, which no write path in
		// this tree can produce. Before B-175 this case fell through to the
		// prefix check below and was reported as "not vault-encoded", which
		// sent the reader looking for a legacy row that was never there.
		metrics.SecretDecryptFailedTotal.WithLabelValues("empty_ciphertext").Inc()
		return nil, fmt.Errorf("secret_fetch: empty ciphertext for tenant %s (EC-06 NULL row guard)", tenantID)
	}

	// T3.4 dual-path dispatch: vault:v1: prefix → transit decrypt.
	if !strings.HasPrefix(string(encryptedValue), "vault:v1:") {
		// Data side. The store dispatcher reaches this same conclusion by
		// attempting the AES decrypt and failing; this path refuses before
		// attempting, because the legacy branch is disabled here post-J22.
		// The operator's response is identical either way — restore a backup,
		// not restore a key — so the reason is the same one.
		metrics.SecretDecryptFailedTotal.WithLabelValues("aes_legacy_error").Inc()
		return nil, errors.New("secret_fetch: row is not vault-encoded (legacy AES path disabled post-J22 cutover)")
	}
	plain, derr := r.transit.Decrypt(ctx, tenantID, string(encryptedValue))
	if derr != nil {
		// Key side: a revoked or rotated transit key lands here, and so does an
		// unreachable OpenBao. This is the exact failure B-126 was filed for,
		// on the path that serves users. The typed error stays in the chain for
		// the log rather than becoming a label, matching the store dispatcher.
		metrics.SecretDecryptFailedTotal.WithLabelValues("vault_transit_error").Inc()
		return nil, fmt.Errorf("secret_fetch: transit decrypt: %w", derr)
	}
	return plain, nil
}

// Compile-time interface assertion.
var _ secrets.InternalSecretReader = (*rawSecretReader)(nil)

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
// `rawSecretReader.ReadSecret`; closing it here too prevents any future
// intermediate wrapper from silently turning a 404 NOT_FOUND into a
// 500 INTERNAL.
func (b *inlineTenantBinding) GetTenantCellID(ctx context.Context, tenantID string) (string, error) {
	if b == nil || b.cellStore == nil {
		return "", errors.New("secret_fetch: cell store not configured")
	}
	cell, err := b.cellStore.GetTenantCellID(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", secrets.ErrTenantNotFound
		}
		return "", fmt.Errorf("secret_fetch: tenant binding: %w", err)
	}
	return cell, nil
}

// Compile-time interface assertion.
var _ secrets.TenantCellBindingLookup = (*inlineTenantBinding)(nil)

// setupSecretFetchDeps composes the three runtime dependencies for the
// broker handler. Returns nil when any prerequisite is missing so the
// caller can skip route registration cleanly (LIVE 404 → ESO retries).
func setupSecretFetchDeps(pool *pgxpool.Pool, transit vault.Transit, cellStore *store.PostgresCellStore, auditLogger *store.AuditLogger) *secrets.SecretFetchHandlerDeps {
	if pool == nil || transit == nil || cellStore == nil || auditLogger == nil {
		return nil
	}
	return &secrets.SecretFetchHandlerDeps{
		TenantBinding: &inlineTenantBinding{cellStore: cellStore},
		SecretReader:  &rawSecretReader{pool: pool, transit: transit},
		AuditWriter:   secrets.NewSecretReadAuditAdapter(auditLogger),
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

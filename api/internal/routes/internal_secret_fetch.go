// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

// internal_secret_fetch.go — T4.1 of plan 2026-06-02-openbao-adoption v1.3.
//
// POST /internal/v1/secret-fetch/{tenant_id}/{secret_name}
//
// Endpoint chamado pelo ESO em cell runtime clusters para resolver
// ExternalSecret CRs via the broker pattern. loopzero-api autoriza via
// cross-cluster TokenReview (middleware/cross_cluster_token_review.go),
// valida cell↔tenant binding (EC-07 MUST FIX P0), valida secret_name
// regex (EC-08 MUST FIX P1), e retorna plaintext base64-encoded.
//
// Contract:
//   Headers:
//     Authorization: Bearer <SA-token-from-cell>     (validated by middleware)
//     X-Cell-ID: cell-default                        (validated by middleware)
//   Path:
//     tenant_id (UUID-like; arbitrary opaque string accepted by router)
//     secret_name (UPPER_SNAKE_CASE per EC-08)
//
//   Response 200: {"plaintext_b64": "<base64>", "secret_name": "...", "tenant_id": "..."}
//   Response 400: {"error":"invalid_request","code":"INVALID_SECRET_NAME"} (EC-08)
//   Response 403: {"error":"cell_tenant_binding_missing","code":"FORBIDDEN"} (EC-07)
//   Response 404: {"error":"secret_not_found","code":"NOT_FOUND"}
//   Response 500: {"error":"internal","code":"INTERNAL"}

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/loopzero/loopzero/api/internal/middleware"
)

// EC-08 MUST FIX P1: secret_name regex validation uses the same
// secretNameRegex defined in secrets.go (write-side enforcement). Sharing
// the regex guarantees the read-side matches the write-side — names that
// pass POST /api/v1/secrets are exactly the names this endpoint accepts.

// SecretFetchHandlerDeps captures the runtime dependencies. Decoupled
// behind interfaces so handler tests run without DB or Vault.
type SecretFetchHandlerDeps struct {
	// TenantBinding resolves a tenant's placement cell. Per EC-07: handler
	// MUST verify the X-Cell-ID from the auth context matches this binding.
	TenantBinding TenantCellBindingLookup

	// SecretReader loads encrypted secret bytes by (tenant_id, secret_name).
	SecretReader InternalSecretReader

	// AuditWriter records both successful secret reads (audit trail) and
	// EC-07 violation attempts (cross_tenant_attempt event).
	AuditWriter SecretReadAuditWriter
}

// TenantCellBindingLookup is the EC-07 binding verifier. Returns the cell
// where the tenant was placed (sticky placement per ADR-0008).
// Implementations should call sqlc-generated GetTenantCellID.
type TenantCellBindingLookup interface {
	GetTenantCellID(ctx context.Context, tenantID string) (string, error)
}

// InternalSecretReader loads ciphertext + delegates decrypt to the dispatcher
// (encryptForStore/decryptFromStore — T3.4/T3.5 dual-path). Returns plaintext.
type InternalSecretReader interface {
	ReadSecret(ctx context.Context, tenantID, secretName string) ([]byte, error)
}

// SecretReadAuditWriter writes audit_log entries. Two event types:
//   - "secret.read.broker"        (success)
//   - "cross_tenant_attempt"       (EC-07 violation)
type SecretReadAuditWriter interface {
	WriteAudit(ctx context.Context, event SecretReadAuditEvent) error
}

// SecretReadAuditEvent is the canonical audit record. Fields chosen to match
// existing audit_log schema (action, resource_type, resource_id, actor, etc).
type SecretReadAuditEvent struct {
	EventType  string // "secret.read.broker" | "cross_tenant_attempt"
	TenantID   string
	SecretName string
	CellID     string // X-Cell-ID from request
	Actor      string // username from TokenReview (e.g. "system:serviceaccount:external-secrets:eso")
	Reason     string // optional human-readable detail for violations
}

// Common typed errors (consumed by handler + tests via errors.Is).
var (
	ErrSecretNotFound       = errors.New("secret not found")
	ErrTenantNotFound       = errors.New("tenant not found")
	ErrCellTenantBinding    = errors.New("cell does not own this tenant")
	ErrInvalidSecretName    = errors.New("invalid secret name (EC-08: must match ^[A-Z][A-Z0-9_]*$)")
)

// NewSecretFetchHandler returns an http.HandlerFunc for T4.1.
// The caller is expected to wrap this with the cross-cluster TokenReview
// middleware so r.Context() carries a *middleware.TokenReviewResult.
func NewSecretFetchHandler(deps SecretFetchHandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Pull validated identity from middleware-injected context.
		identity, ok := middleware.CrossClusterFromContext(ctx)
		if !ok || identity == nil || !identity.Authenticated {
			writeSecretFetchError(w, http.StatusUnauthorized, "auth_failed", "AUTH_TOKEN_INVALID", "")
			return
		}

		tenantID := chi.URLParam(r, "tenant_id")
		secretName := chi.URLParam(r, "secret_name")
		if tenantID == "" || secretName == "" {
			writeSecretFetchError(w, http.StatusBadRequest, "missing_path_params", "BAD_REQUEST", "")
			return
		}

		// EC-08 MUST FIX P1: validate secret_name regex BEFORE any DB lookup.
		// Rejects path traversal attempts (../platform/master etc).
		if !secretNameRegex.MatchString(secretName) {
			writeSecretFetchError(w, http.StatusBadRequest,
				"invalid_request", "INVALID_SECRET_NAME",
				"secret_name must match ^[A-Z][A-Z0-9_]*$")
			return
		}

		// EC-07 MUST FIX P0: cell↔tenant binding check.
		ownerCellID, err := deps.TenantBinding.GetTenantCellID(ctx, tenantID)
		if err != nil {
			if errors.Is(err, ErrTenantNotFound) {
				writeSecretFetchError(w, http.StatusNotFound,
					"tenant_not_found", "NOT_FOUND", "")
				return
			}
			writeSecretFetchError(w, http.StatusInternalServerError,
				"internal", "INTERNAL", err.Error())
			return
		}
		if ownerCellID != identity.CellID {
			// EC-07 violation: write audit event "cross_tenant_attempt"
			_ = deps.AuditWriter.WriteAudit(ctx, SecretReadAuditEvent{
				EventType:  "cross_tenant_attempt",
				TenantID:   tenantID,
				SecretName: secretName,
				CellID:     identity.CellID,
				Actor:      identity.Username,
				Reason:     fmt.Sprintf("X-Cell-ID=%s but tenant placed at %s", identity.CellID, ownerCellID),
			})
			writeSecretFetchError(w, http.StatusForbidden,
				"cell_tenant_binding_missing", "FORBIDDEN",
				"")
			return
		}

		// Authorized — load + decrypt
		plaintext, err := deps.SecretReader.ReadSecret(ctx, tenantID, secretName)
		if err != nil {
			if errors.Is(err, ErrSecretNotFound) {
				writeSecretFetchError(w, http.StatusNotFound,
					"secret_not_found", "NOT_FOUND", "")
				return
			}
			writeSecretFetchError(w, http.StatusInternalServerError,
				"internal", "INTERNAL", err.Error())
			return
		}

		// Audit success
		_ = deps.AuditWriter.WriteAudit(ctx, SecretReadAuditEvent{
			EventType:  "secret.read.broker",
			TenantID:   tenantID,
			SecretName: secretName,
			CellID:     identity.CellID,
			Actor:      identity.Username,
		})

		writeSecretFetchSuccess(w, tenantID, secretName, plaintext)
	}
}

type secretFetchResponse struct {
	TenantID     string `json:"tenant_id"`
	SecretName   string `json:"secret_name"`
	PlaintextB64 string `json:"plaintext_b64"`
}

type secretFetchErrorResponse struct {
	Error  string `json:"error"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

func writeSecretFetchSuccess(w http.ResponseWriter, tenantID, secretName string, plaintext []byte) {
	resp := secretFetchResponse{
		TenantID:     tenantID,
		SecretName:   secretName,
		PlaintextB64: base64.StdEncoding.EncodeToString(plaintext),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeSecretFetchError(w http.ResponseWriter, status int, errStr, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(secretFetchErrorResponse{
		Error: errStr, Code: code, Detail: detail,
	})
}

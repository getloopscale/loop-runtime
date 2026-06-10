// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	loopzerojwt "github.com/loopzero/loopzero/pkg/jwt"
)

// UserContext holds authenticated user info, set by the auth middleware.
type UserContext struct {
	Subject  string
	Email    string
	Name     string
	TenantID string
	Role     string // owner, admin, member (§5.1)
}

type contextKey string

const userCtxKey contextKey = "user"

// GetUser extracts the authenticated user from the request context.
func GetUser(ctx context.Context) *UserContext {
	uc, ok := ctx.Value(userCtxKey).(*UserContext)
	if !ok {
		return nil
	}
	return uc
}

// WithUser returns a copy of ctx with the given UserContext attached, using the
// same key that RequireJWT + RequireScope populate. Tests in other packages
// (cloud/tests/unit/) use this to construct authenticated requests without
// reaching for the unexported userCtxKey. Plan 0 / T0.3-T0.6 test support.
func WithUser(ctx context.Context, u *UserContext) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// RequireJWT returns chi middleware that validates JWT Bearer tokens.
// Accepts Hydra RSA JWTs (CLI) and HMAC session JWTs (dashboard).
// Uses pkg/jwt — single source of truth for JWT validation.
//
// T6.0 of plan 2026-05-27-backend-frontend-integration v1.1 (EC-04):
// the dashboard opens SSE streams via the native EventSource API, which
// cannot set custom request headers. For SSE-style paths we additionally
// accept the JWT as a `?token=<jwt>` query parameter. The query-param
// form is only consulted when the standard Bearer header is absent, so
// the security posture for non-SSE clients is unchanged.
//
// Security note: the token will appear in upstream access logs. Mitigated
// by short HMAC TTL (1h) + key rotation (pkg/jwt SessionTokenIssuer
// previous-secret window). Trade-off accepted vs the alternative of
// migrating every stream surface to WebSocket.
func RequireJWT(rsaValidator *loopzerojwt.Validator, sessValidator *loopzerojwt.SessionTokenIssuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerOrQueryToken(r)
			if token == "" {
				slog.Warn("auth: rejected — no Bearer token", "method", r.Method, "path", r.URL.Path)
				writeAuthError(w, "Not authenticated. Run: loopzero login")
				return
			}

			// Try RSA JWT (Hydra — CLI)
			if rsaValidator != nil {
				claims, err := rsaValidator.Validate(token)
				if err == nil {
					email := claims.Email
					if email == "" {
						email = claims.Ext.Email
					}
					name := claims.Name
					if name == "" {
						name = claims.Ext.Name
					}
					tenantID := claims.TenantID
					if tenantID == "" {
						tenantID = claims.Ext.TenantID
					}
					if tenantID == "" {
						// MEET-2026-04-07-039 B7: never fall back to "default" tenant.
						slog.Warn("auth: JWT has no tenant_id", "sub", claims.Subject, "path", r.URL.Path)
						writeAuthError(w, "Account setup required. Please try logging in again.")
						return
					}
					ctx := context.WithValue(r.Context(), userCtxKey, &UserContext{
						Subject:  claims.Subject,
						Email:    email,
						Name:     name,
						TenantID: tenantID,
					})
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Try HMAC session JWT (dashboard)
			if sessValidator != nil {
				claims, err := sessValidator.ValidateSession(token)
				if err == nil {
					tenantID := claims.TenantID
					if tenantID == "" {
						// MEET-2026-04-07-039 B7: never fall back to "default" tenant.
						slog.Warn("auth: session JWT has no tenant_id", "sub", claims.Subject, "path", r.URL.Path)
						writeAuthError(w, "Account setup required. Please try logging in again.")
						return
					}
					ctx := context.WithValue(r.Context(), userCtxKey, &UserContext{
						Subject:  claims.Subject,
						Email:    claims.Email,
						Name:     claims.Name,
						TenantID: tenantID,
					})
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			slog.Warn("auth: all JWT validators failed", "path", r.URL.Path, "token_len", len(token))
			writeAuthError(w, "Invalid or expired token. Run: loopzero login")
		})
	}
}

// extractBearerOrQueryToken returns the JWT carried by either the standard
// Authorization: Bearer header or the EventSource fallback ?token=<jwt>
// query parameter. Returns the empty string when neither is present.
//
// The header form is preferred — the query form is consulted only as a
// fallback so the security posture for non-SSE clients is unchanged.
// (T6.0 / EC-04 of plan 2026-05-27-backend-frontend-integration v1.1.)
func extractBearerOrQueryToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	// EventSource workaround — only relevant for SSE streams. Other clients
	// SHOULD use the Authorization header; the query form merely tolerates
	// browsers' inability to set custom headers on EventSource.
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	return ""
}

// writeAuthError writes a JSON error response with request_id for tracing (§4.3).
func writeAuthError(w http.ResponseWriter, message string) {
	requestID := w.Header().Get("X-Request-Id")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	resp := map[string]string{
		"code":    "AUTH_REQUIRED",
		"message": message,
	}
	if requestID != "" {
		resp["request_id"] = requestID
	}
	_ = json.NewEncoder(w).Encode(resp)
}

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

// RequireJWT returns chi middleware that validates JWT Bearer tokens.
// Accepts Hydra RSA JWTs (CLI) and HMAC session JWTs (dashboard).
// Uses pkg/jwt — single source of truth for JWT validation.
func RequireJWT(rsaValidator *loopzerojwt.Validator, sessValidator *loopzerojwt.SessionTokenIssuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				slog.Warn("auth: rejected — no Bearer token", "method", r.Method, "path", r.URL.Path)
				writeAuthError(w, "Not authenticated. Run: loopzero login")
				return
			}
			token := authHeader[7:]

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
						// MEET-2026-04-07-039 B7: never fall back to "default" tenant.
						// Missing tenant_id means provisioning failed or hasn't happened yet.
						slog.Warn("auth: JWT has no tenant_id", "sub", claims.RegisteredClaims.Subject, "path", r.URL.Path)
						writeAuthError(w, "Account setup required. Please try logging in again.")
						return
					}
					ctx := context.WithValue(r.Context(), userCtxKey, &UserContext{
						Subject:  claims.RegisteredClaims.Subject,
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
						slog.Warn("auth: session JWT has no tenant_id", "sub", claims.RegisteredClaims.Subject, "path", r.URL.Path)
						writeAuthError(w, "Account setup required. Please try logging in again.")
						return
					}
					ctx := context.WithValue(r.Context(), userCtxKey, &UserContext{
						Subject:  claims.RegisteredClaims.Subject,
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
	json.NewEncoder(w).Encode(resp)
}

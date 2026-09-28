// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package auth establishes who is calling, before any handler runs.
//
// It recognises two credentials — the user Bearer token, and the
// service-to-service secret the cloud proxy attaches to every forwarded
// request — resolves the caller's tenant membership from the store rather than
// from headers, and gates the admin surface behind a key of its own. The
// settled identity travels to handlers as a *UserContext in the request
// context, which GetUserContext reads back.
//
// The bounded cache that keeps the token validator off the hot path lives in
// token_cache.go; splitting it out is what holds this file under the 500-line
// budget the B-059 plan set for the package (NFR-006).
//
// This package is a leaf of the middleware tree: audit, policy and ratelimit
// import it to learn the caller, and it imports none of them.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ErrInvalidToken is returned when token validation fails.
var ErrInvalidToken = errors.New("invalid or expired token")

// TokenValidationResult holds the result of validating an auth token.
type TokenValidationResult struct {
	UserID string
	Email  string
}

// TokenValidator validates authentication tokens (DIP — abstracts GitHub API).
type TokenValidator interface {
	ValidateToken(ctx context.Context, token string) (*TokenValidationResult, error)
}

// TenantMembership represents a user's membership in a tenant. The type is
// owned by ports so the store that reads it does not import this middleware.
type TenantMembership = ports.TenantMembership

// MembershipStore retrieves tenant memberships for a user (DIP — abstracts DB).
type MembershipStore interface {
	GetMemberships(ctx context.Context, userID string) ([]TenantMembership, error)
}

// UserContext holds authenticated user information, injected into request context.
type UserContext struct {
	UserID   string
	Email    string
	TenantID string
	Role     string
}

type contextKey string

const userContextKey contextKey = "userContext"

// UserContextKeyPublic returns the context key for UserContext.
// Exported for use in tests that inject UserContext directly.
func UserContextKeyPublic() contextKey {
	return userContextKey
}

// GetUserContext extracts UserContext from the request context.
func GetUserContext(ctx context.Context) *UserContext {
	uc, ok := ctx.Value(userContextKey).(*UserContext)
	if !ok {
		return nil
	}
	return uc
}

// MiddlewareWithTTL is Middleware with a configurable token-cache TTL.
// Also accepts internal service calls with X-Service-Auth header (cloud→engine proxy).
func (a *Authenticator) MiddlewareWithTTL(validator TokenValidator, store MembershipStore, cacheTTL time.Duration) func(http.Handler) http.Handler {
	cache := &sync.Map{}
	var cacheSize atomic.Int64

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Internal service auth: cloud proxy sends X-Service-Auth + identity headers.
			// S2.3: Engine resolves role from DB, not from headers. Proxy sends only
			// identity (user ID, tenant, email). Role is NEVER trusted from headers.
			//
			// 2026-05-13 (ADR `decisions/2026-05-13-service-auth-ed25519-adoption.md`):
			// dual-control via Ed25519 signature is layered on top of the shared
			// secret. Phase 1 (accept-both): unsigned requests still proceed via
			// the legacy shared-secret path; signed requests get the additional
			// signature verification. Phase 2 (require-signed) flips the
			// RequireSignature flag and unsigned requests are rejected.
			if serviceAuth := r.Header.Get("X-Service-Auth"); serviceAuth != "" {
				if a.validateServiceAuth(serviceAuth) {
					userID := r.Header.Get("X-LoopZero-User-ID")
					tenantID := r.Header.Get("X-LoopZero-Tenant")
					email := r.Header.Get("X-LoopZero-Email")
					if userID != "" && tenantID != "" {
						// Dual-control: Ed25519 signature gate (Phase 1 + Phase 2 logic).
						sigPresent := hasServiceAuthSignatureHeaders(r)
						if sigPresent {
							serviceAuthSignaturePresent.WithLabelValues("present").Inc()
							ok, reason := a.verifyServiceAuthSignature(r, userID, tenantID)
							if !ok {
								serviceAuthSignatureRejected.WithLabelValues(string(reason)).Inc()
								a.emitFailure(r, AuthFailureServiceMismatch,
									"service-auth signature rejected: "+string(reason),
									map[string]any{"requested_tenant": tenantID, "user_id": userID, "reject_reason": string(reason)})
								writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid service authentication")
								return
							}
						} else {
							serviceAuthSignaturePresent.WithLabelValues("absent").Inc()
							if a.cfg.RequireSignature {
								// Phase 2+ — unsigned no longer accepted.
								a.emitFailure(r, AuthFailureServiceMismatch,
									"service-auth signature required (Phase 2)",
									map[string]any{"requested_tenant": tenantID, "user_id": userID, "reject_reason": "signature_missing"})
								writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid service authentication")
								return
							}
							// Phase 1 — fall through to legacy single-control path.
						}

						// Resolve role from DB — same authority as Bearer path
						memberships, mErr := store.GetMemberships(r.Context(), userID)
						if mErr != nil {
							writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to resolve team memberships")
							return
						}
						role := ""
						for _, m := range memberships {
							if m.TenantID == tenantID {
								role = m.Role
								break
							}
						}
						if role == "" {
							a.emitFailure(r, AuthFailureTenantNotFound,
								"service-auth user not a member of requested tenant",
								map[string]any{"requested_tenant": tenantID, "user_id": userID})
							writeError(w, http.StatusForbidden, "TENANT_NOT_FOUND", "You are not a member of the requested team")
							return
						}
						ctx := context.WithValue(r.Context(), userContextKey, &UserContext{
							UserID:   userID,
							Email:    email,
							TenantID: tenantID,
							Role:     role,
						})
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}
				// Invalid service auth — reject immediately, do not fall through
				a.emitFailure(r, AuthFailureServiceMismatch, "invalid X-Service-Auth on Bearer endpoint", nil)
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid service authentication")
				return
			}

			// Extract token
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				a.emitFailure(r, AuthFailureTokenInvalid, "missing or malformed Authorization header", nil)
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing or invalid Authorization header. Use: Authorization: Bearer <token>")
				return
			}
			token := strings.TrimPrefix(authHeader, "Bearer ")

			// Validate token (with bounded cache)
			ctx, span := otel.Tracer("loopzero-api").Start(r.Context(), "loopzero.api.auth.validate_token",
				trace.WithAttributes(
					attribute.String("loopzero.auth.result", "pending"),
				))

			result, err := validateWithCache(ctx, validator, cache, &cacheSize, token, cacheTTL)
			if err != nil {
				span.SetAttributes(attribute.String("loopzero.auth.result", "failure"))
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				span.End()
				if errors.Is(err, ErrInvalidToken) {
					// Emit audit: token rejected by validator. Reason includes the
					// validator's error string (sanitized — pkg/jwt returns
					// structured "expired" vs "invalid signature" etc).
					a.emitFailure(r, AuthFailureTokenInvalid, err.Error(), nil)
					writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired token")
				} else {
					// Transient infra failure (JWKS down etc) — NOT a security
					// signal. Skip audit emission to avoid alert fatigue.
					writeError(w, http.StatusServiceUnavailable, "AUTH_SERVICE_UNAVAILABLE", "Authentication service is temporarily unavailable")
				}
				return
			}
			span.SetAttributes(attribute.String("loopzero.auth.result", "success"))
			span.End()

			// Resolve tenant
			memberships, err := store.GetMemberships(r.Context(), result.UserID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to resolve tenant memberships")
				return
			}

			uc := &UserContext{
				UserID: result.UserID,
				Email:  result.Email,
			}

			switch len(memberships) {
			case 0:
				a.emitFailure(r, AuthFailureTenantNotFound,
					"authenticated user has zero tenant memberships",
					map[string]any{"user_id": result.UserID})
				writeError(w, http.StatusForbidden, "NO_TENANT", "No team associated with this account")
				return
			case 1:
				uc.TenantID = memberships[0].TenantID
				uc.Role = memberships[0].Role
			default:
				requestedTenant := r.Header.Get("X-LoopZero-Tenant")
				if requestedTenant == "" {
					writeError(w, http.StatusConflict, "TENANT_REQUIRED", "Multiple teams available. Set the X-LoopZero-Tenant header")
					return
				}
				found := false
				for _, m := range memberships {
					if m.TenantID == requestedTenant {
						uc.TenantID = m.TenantID
						uc.Role = m.Role
						found = true
						break
					}
				}
				if !found {
					a.emitFailure(r, AuthFailureTenantNotFound,
						"Bearer user not a member of X-LoopZero-Tenant",
						map[string]any{"requested_tenant": requestedTenant, "user_id": result.UserID})
					writeError(w, http.StatusForbidden, "TENANT_NOT_FOUND", "You are not a member of the requested team")
					return
				}
			}

			reqCtx := context.WithValue(r.Context(), userContextKey, uc)
			next.ServeHTTP(w, r.WithContext(reqCtx))
		})
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": message,
	})
}

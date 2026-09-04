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
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
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

// TenantMembership represents a user's membership in a tenant.
type TenantMembership struct {
	TenantID string
	Role     string
}

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

// Auth returns middleware that validates Bearer tokens and resolves tenant membership.
// Token validation results are cached for 5 minutes.
func Auth(validator TokenValidator, store MembershipStore) func(http.Handler) http.Handler {
	return AuthWithTTL(validator, store, 5*time.Minute)
}

// ServiceAuthSecret is the shared secret for internal service-to-service calls.
// Set from LOOPZERO_SERVICE_AUTH_SECRET env var. When empty, service auth is disabled.
var ServiceAuthSecret string

// ServiceAuthSecretPrevious is the previous secret for zero-downtime rotation (S1.2).
// Set from LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS env var. When empty, single-key mode.
var ServiceAuthSecretPrevious string

// AdminAPIKey is the platform-admin API key for cross-tenant operations.
// Set from LOOPZERO_ADMIN_API_KEY env var. When empty, admin endpoints are disabled.
// MEET-052 Security VETO: Required for force-delete, bulk-delete, and admin operations.
var AdminAPIKey string

// AdminAPIKeyPrevious is the previous admin API key for zero-downtime rotation.
// Set from LOOPZERO_ADMIN_API_KEY_PREVIOUS env var. When empty, single-key mode.
var AdminAPIKeyPrevious string

// validateServiceAuth checks the X-Service-Auth header against current and previous secrets.
// S1.2: Dual-key rotation — accepts current OR previous during rotation window.
//
// T2.2 of deep-review-remediation-plan (closes find_sec_002 HIGH): uses
// subtle.ConstantTimeCompare to prevent timing-oracle byte-by-byte brute-force.
// Empty AdminAPIKey/ServiceAuthSecret returns false BEFORE constant-time compare
// so attacker cannot probe for "service has no key configured".
func validateServiceAuth(provided string) bool {
	if ServiceAuthSecret == "" {
		return false
	}
	current := subtle.ConstantTimeCompare([]byte(provided), []byte(ServiceAuthSecret)) == 1
	prev := ServiceAuthSecretPrevious != "" &&
		subtle.ConstantTimeCompare([]byte(provided), []byte(ServiceAuthSecretPrevious)) == 1
	return current || prev
}

// ValidateAdminAPIKey checks the provided key against current and previous admin API keys.
// MEET-052: Dual-key rotation for zero-downtime key rotation.
//
// T0.2 of deep-review-remediation-plan (closes find_sec_001 CRITICAL): exported
// (was validateAdminAPIKey) so handlers serving dual-mode self-service + admin
// (e.g., tenants.Delete) can validate the X-Admin-Key value instead of just
// checking presence. Bypass attack: previous code used header-presence check
// (`r.Header.Get("X-Admin-Key") != ""`) which any authenticated user could
// trivially spoof.
//
// T2.2 (closes find_sec_002 HIGH): uses subtle.ConstantTimeCompare to prevent
// timing-oracle byte-by-byte brute-force attack against AdminAPIKey.
func ValidateAdminAPIKey(provided string) bool {
	if AdminAPIKey == "" {
		return false
	}
	current := subtle.ConstantTimeCompare([]byte(provided), []byte(AdminAPIKey)) == 1
	prev := AdminAPIKeyPrevious != "" &&
		subtle.ConstantTimeCompare([]byte(provided), []byte(AdminAPIKeyPrevious)) == 1
	return current || prev
}

// RequireAdminService is a middleware that requires a valid service-auth
// secret in the X-Service-Auth header for ALL /api/v1/admin/* operations.
//
// T2.1 of deep-review-2026-05-10-remediation-plan (find_sec_admin_secrets_divergent HIGH):
// pre-T2.1 admin_pin_cell.go + admin_evict.go each inlined their own
// subtle.ConstantTimeCompare reading TWO different env vars
// (LOOPZERO_ADMIN_SERVICE_AUTH_SECRET vs LOOPZERO_SERVICE_AUTH_SECRET). The
// divergence made the secret impossible to audit/rotate consistently —
// this middleware enforces a single auth gate.
//
// Secret lookup (qa-sprint2-cleanup-2026-06-24 T1.0 narrow — closes loopzero-cloud#16):
//  1. LOOPZERO_ADMIN_SERVICE_AUTH_SECRET (canonical, per ADR D3)
//  2. LOOPZERO_ADMIN_SERVICE_AUTH_SECRET_PREVIOUS (canonical rotation sibling)
//
// The legacy LOOPZERO_SERVICE_AUTH_SECRET (+ its _PREVIOUS sibling) is the secret the
// cloud /engine/* proxy adds to EVERY forwarded user-JWT request. Accepting it here
// would let any owner-JWT request reach admin endpoints — exactly the #16 bypass.
// The legacy env REMAINS valid for the user-JWT proxy on NON-admin routes
// (consumed by AuthWithTTL below), so the proxy keeps working; this middleware
// just refuses to honor it for admin authorization.
//
// Uses subtle.ConstantTimeCompare for timing-attack resistance.
func RequireAdminService(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-Service-Auth")
		if provided == "" {
			emitAuthFailure(r, AuthFailureServiceMismatch, "missing X-Service-Auth header on admin endpoint", nil)
			writeError(w, http.StatusForbidden, "FORBIDDEN",
				"Admin service authentication required. Set X-Service-Auth header.")
			return
		}
		canonical := os.Getenv("LOOPZERO_ADMIN_SERVICE_AUTH_SECRET")
		canonicalPrev := os.Getenv("LOOPZERO_ADMIN_SERVICE_AUTH_SECRET_PREVIOUS")

		match := func(expected string) bool {
			return expected != "" &&
				subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
		}
		if match(canonical) || match(canonicalPrev) {
			next.ServeHTTP(w, r)
			return
		}
		emitAuthFailure(r, AuthFailureServiceMismatch, "invalid X-Service-Auth secret on admin endpoint", nil)
		writeError(w, http.StatusForbidden, "FORBIDDEN",
			"Invalid admin service-auth secret.")
	})
}

// RequireAdmin is a middleware that requires a valid admin API key in the
// X-Admin-Key header. Used for cross-tenant operations (force-delete, bulk-delete).
// MEET-052 Security VETO: No destructive admin endpoint may be exposed without this.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-Admin-Key")
		if key == "" {
			emitAuthFailure(r, AuthFailureAdminMismatch, "missing X-Admin-Key header", nil)
			writeError(w, http.StatusForbidden, "FORBIDDEN", "Admin API key required. Set X-Admin-Key header.")
			return
		}
		if !ValidateAdminAPIKey(key) {
			emitAuthFailure(r, AuthFailureAdminMismatch, "invalid X-Admin-Key", nil)
			writeError(w, http.StatusForbidden, "FORBIDDEN", "Invalid admin API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AuthWithTTL is like Auth but with a configurable cache TTL.
// Also accepts internal service calls with X-Service-Auth header (cloud→engine proxy).
func AuthWithTTL(validator TokenValidator, store MembershipStore, cacheTTL time.Duration) func(http.Handler) http.Handler {
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
			// ServiceAuthRequireSignature flag and unsigned requests are rejected.
			if serviceAuth := r.Header.Get("X-Service-Auth"); serviceAuth != "" {
				if validateServiceAuth(serviceAuth) {
					userID := r.Header.Get("X-LoopZero-User-ID")
					tenantID := r.Header.Get("X-LoopZero-Tenant")
					email := r.Header.Get("X-LoopZero-Email")
					if userID != "" && tenantID != "" {
						// Dual-control: Ed25519 signature gate (Phase 1 + Phase 2 logic).
						sigPresent := hasServiceAuthSignatureHeaders(r)
						if sigPresent {
							serviceAuthSignaturePresent.WithLabelValues("present").Inc()
							ok, reason := verifyServiceAuthSignature(r, userID, tenantID)
							if !ok {
								serviceAuthSignatureRejected.WithLabelValues(string(reason)).Inc()
								emitAuthFailure(r, AuthFailureServiceMismatch,
									"service-auth signature rejected: "+string(reason),
									map[string]any{"requested_tenant": tenantID, "user_id": userID, "reject_reason": string(reason)})
								writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid service authentication")
								return
							}
						} else {
							serviceAuthSignaturePresent.WithLabelValues("absent").Inc()
							if ServiceAuthRequireSignature {
								// Phase 2+ — unsigned no longer accepted.
								emitAuthFailure(r, AuthFailureServiceMismatch,
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
							emitAuthFailure(r, AuthFailureTenantNotFound,
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
				emitAuthFailure(r, AuthFailureServiceMismatch, "invalid X-Service-Auth on Bearer endpoint", nil)
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid service authentication")
				return
			}

			// Extract token
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				emitAuthFailure(r, AuthFailureTokenInvalid, "missing or malformed Authorization header", nil)
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
					emitAuthFailure(r, AuthFailureTokenInvalid, err.Error(), nil)
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
				emitAuthFailure(r, AuthFailureTenantNotFound,
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
					emitAuthFailure(r, AuthFailureTenantNotFound,
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

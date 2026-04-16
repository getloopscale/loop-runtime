// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

// cachedValidation holds a cached token validation result.
type cachedValidation struct {
	result    *TokenValidationResult
	createdAt time.Time
	expiresAt time.Time
}

// Auth returns middleware that validates Bearer tokens and resolves tenant membership.
// Token validation results are cached for 5 minutes.
func Auth(validator TokenValidator, store MembershipStore) func(http.Handler) http.Handler {
	return AuthWithTTL(validator, store, 5*time.Minute)
}

// maxTokenCacheSize is the maximum number of entries in the token validation cache.
// When exceeded, the cache is cleared to prevent unbounded memory growth.
// The TTL ensures entries expire naturally; this limit is a safety net.
const maxTokenCacheSize = 10000

// ServiceAuthSecret is the shared secret for internal service-to-service calls.
// Set from LOOPZERO_SERVICE_AUTH_SECRET env var. When empty, service auth is disabled.
var ServiceAuthSecret string

// ServiceAuthSecretPrevious is the previous secret for zero-downtime rotation (S1.2).
// Set from LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS env var. When empty, single-key mode.
var ServiceAuthSecretPrevious string

// validateServiceAuth checks the X-Service-Auth header against current and previous secrets.
// S1.2: Dual-key rotation — accepts current OR previous during rotation window.
func validateServiceAuth(provided string) bool {
	if ServiceAuthSecret == "" {
		return false
	}
	if provided == ServiceAuthSecret {
		return true
	}
	if ServiceAuthSecretPrevious != "" && provided == ServiceAuthSecretPrevious {
		return true
	}
	return false
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
			if serviceAuth := r.Header.Get("X-Service-Auth"); serviceAuth != "" {
				if validateServiceAuth(serviceAuth) {
					userID := r.Header.Get("X-LoopZero-User-ID")
					tenantID := r.Header.Get("X-LoopZero-Tenant")
					email := r.Header.Get("X-LoopZero-Email")
					if userID != "" && tenantID != "" {
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
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid service authentication")
				return
			}

			// Extract token
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing or invalid Authorization header. Use: Authorization: Bearer <token>")
				return
			}
			token := strings.TrimPrefix(authHeader, "Bearer ")

			// Validate token (with bounded cache)
			result, err := validateWithCache(r.Context(), validator, cache, &cacheSize, token, cacheTTL)
			if err != nil {
				if errors.Is(err, ErrInvalidToken) {
					writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired token")
				} else {
					writeError(w, http.StatusServiceUnavailable, "AUTH_SERVICE_UNAVAILABLE", "Authentication service is temporarily unavailable")
				}
				return
			}

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
					writeError(w, http.StatusForbidden, "TENANT_NOT_FOUND", "You are not a member of the requested team")
					return
				}
			}

			ctx := context.WithValue(r.Context(), userContextKey, uc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// tokenCacheKey returns a SHA256 hash of the token for use as cache key.
// SEC-001 fix: raw tokens as map keys means a heap dump exposes all active sessions.
// Using a hash prevents token recovery from memory dumps.
func tokenCacheKey(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func validateWithCache(ctx context.Context, validator TokenValidator, cache *sync.Map, cacheSize *atomic.Int64, token string, ttl time.Duration) (*TokenValidationResult, error) {
	cacheKey := tokenCacheKey(token)

	// Check cache
	if cached, ok := cache.Load(cacheKey); ok {
		cv := cached.(*cachedValidation)
		if time.Now().Before(cv.expiresAt) {
			return cv.result, nil
		}
		cache.Delete(cacheKey)
		cacheSize.Add(-1)
	}

	result, err := validator.ValidateToken(ctx, token)
	if err != nil {
		return nil, err
	}

	// Bound cache size: evict entries when the cache exceeds the limit.
	// First pass: delete expired entries. Second pass (if still over 80%):
	// delete oldest entries until under the target.
	now := time.Now()
	if cacheSize.Add(1) > maxTokenCacheSize {
		evictCache(cache, cacheSize, now)
	}

	cache.Store(cacheKey, &cachedValidation{
		result:    result,
		createdAt: now,
		expiresAt: now.Add(ttl),
	})

	return result, nil
}

// evictCache removes entries from the cache to bring it under the size limit.
// First pass: remove expired entries. If still over 80% capacity, remove the
// oldest entries until the target is reached. All operations use sync.Map
// methods — the struct is never replaced.
func evictCache(cache *sync.Map, cacheSize *atomic.Int64, now time.Time) {
	target := int64(float64(maxTokenCacheSize) * 0.8)

	// First pass: delete expired entries.
	// Use LoadAndDelete to avoid double-decrement when concurrent evictions
	// race on the same key — Delete is idempotent but cacheSize.Add(-1) is not.
	cache.Range(func(key, value any) bool {
		cv := value.(*cachedValidation)
		if now.After(cv.expiresAt) {
			if _, loaded := cache.LoadAndDelete(key); loaded {
				cacheSize.Add(-1)
			}
		}
		return true
	})

	if cacheSize.Load() <= target {
		return
	}

	// Second pass: find the oldest entries and delete them until under target.
	// Collect keys with their creation times, then delete the oldest.
	type entry struct {
		key       any
		createdAt time.Time
	}
	var entries []entry
	cache.Range(func(key, value any) bool {
		cv := value.(*cachedValidation)
		entries = append(entries, entry{key: key, createdAt: cv.createdAt})
		return true
	})

	// Sort oldest first.
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].createdAt.Before(entries[i].createdAt) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Delete oldest until under target.
	// Use LoadAndDelete to prevent double-decrement from concurrent evictions.
	for _, e := range entries {
		if cacheSize.Load() <= target {
			break
		}
		if _, loaded := cache.LoadAndDelete(e.key); loaded {
			cacheSize.Add(-1)
		}
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

package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrInvalidToken is returned when token validation fails.
var ErrInvalidToken = errors.New("invalid or expired token")

// TokenValidationResult holds the result of validating a GitHub token.
type TokenValidationResult struct {
	UserID   string
	Email    string
	GitHubID int64
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
	GitHubID int64
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
	expiresAt time.Time
}

// Auth returns middleware that validates Bearer tokens and resolves tenant membership.
// Token validation results are cached for 5 minutes.
func Auth(validator TokenValidator, store MembershipStore) func(http.Handler) http.Handler {
	return AuthWithTTL(validator, store, 5*time.Minute)
}

// AuthWithTTL is like Auth but with a configurable cache TTL.
func AuthWithTTL(validator TokenValidator, store MembershipStore, cacheTTL time.Duration) func(http.Handler) http.Handler {
	cache := &sync.Map{}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing or invalid Authorization header. Use: Authorization: Bearer <token>")
				return
			}
			token := strings.TrimPrefix(authHeader, "Bearer ")

			// Validate token (with cache)
			result, err := validateWithCache(r.Context(), validator, cache, token, cacheTTL)
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
				UserID:   result.UserID,
				Email:    result.Email,
				GitHubID: result.GitHubID,
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

func validateWithCache(ctx context.Context, validator TokenValidator, cache *sync.Map, token string, ttl time.Duration) (*TokenValidationResult, error) {
	// Check cache
	if cached, ok := cache.Load(token); ok {
		cv := cached.(*cachedValidation)
		if time.Now().Before(cv.expiresAt) {
			return cv.result, nil
		}
		cache.Delete(token)
	}

	result, err := validator.ValidateToken(ctx, token)
	if err != nil {
		return nil, err
	}

	cache.Store(token, &cachedValidation{
		result:    result,
		expiresAt: time.Now().Add(ttl),
	})

	return result, nil
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": message,
	})
}

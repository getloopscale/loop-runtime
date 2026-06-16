// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	loopzerojwt "github.com/getloopscale/loopzero-contracts/jwt"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/ratelimit"
)

// SessionTokenHandler exchanges Kratos session cookies for JWT tokens.
type SessionTokenHandler struct {
	issuer            *loopzerojwt.SessionTokenIssuer
	tenantProvisioner *auth.TenantProvisioner
	kratosInternalURL string
	kratosClient      *http.Client
	limiter           ratelimit.Limiter
}

const (
	rateLimitWindow   = 1 * time.Minute
	rateLimitMaxCalls = 10
)

// NewSessionTokenHandler creates a session token exchange handler.
func NewSessionTokenHandler(issuer *loopzerojwt.SessionTokenIssuer, tenantProv *auth.TenantProvisioner, kratosInternalURL string) *SessionTokenHandler {
	return &SessionTokenHandler{
		issuer:            issuer,
		tenantProvisioner: tenantProv,
		kratosInternalURL: kratosInternalURL,
		kratosClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		limiter:           ratelimit.NewInMemoryLimiter(rateLimitMaxCalls, rateLimitWindow),
	}
}

// Exchange validates the Kratos session cookie and returns a JWT.
// POST /v1/auth/session/token
func (h *SessionTokenHandler) Exchange(w http.ResponseWriter, r *http.Request) {
	// §2.3: Rate limit by identity+IP with standard headers
	key := ratelimit.Key("", r.RemoteAddr) // Pre-auth: IP only
	allowed, remaining, resetAt := h.limiter.Allow(key)
	ratelimit.SetHeaders(w, rateLimitMaxCalls, remaining, resetAt)
	if !allowed {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests. Try again in a moment.")
		return
	}

	// Validate Kratos session by forwarding cookies to whoami
	whoamiURL := fmt.Sprintf("%s/sessions/whoami", h.kratosInternalURL)
	req, err := http.NewRequestWithContext(r.Context(), "GET", whoamiURL, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Authentication service error.")
		return
	}
	for _, cookie := range r.Cookies() {
		req.AddCookie(cookie)
	}

	resp, err := h.kratosClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "AUTH_SERVICE_UNAVAILABLE", "Authentication service unavailable. Try again.")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Please sign in.")
		return
	}

	var session struct {
		Active   bool `json:"active"`
		Identity struct {
			ID     string `json:"id"`
			Traits struct {
				Email string `json:"email"`
				Name  string `json:"name"`
			} `json:"traits"`
		} `json:"identity"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil || !session.Active {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Session expired. Please sign in again.")
		return
	}

	// Resolve or create tenant for this user.
	// This MUST succeed — never issue a JWT with "default" tenant.
	tenantID := ""
	if h.tenantProvisioner != nil {
		tid, _, tErr := h.tenantProvisioner.EnsureTenant(r.Context(), session.Identity.ID, session.Identity.Traits.Email)
		if tErr != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_ERROR",
				"Failed to set up your account. Try again or contact support.")
			return
		}
		tenantID = tid
	}
	if tenantID == "" {
		tenantID = "personal" // Only in dev without DB
	}

	// Issue JWT
	token, expiresIn, err := h.issuer.Issue(
		session.Identity.ID,
		session.Identity.Traits.Email,
		session.Identity.Traits.Name,
		tenantID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TOKEN_ERROR", "Failed to create access token.")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   expiresIn,
	})
}

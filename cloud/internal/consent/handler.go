// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package consent implements the login/consent handlers that Ory Hydra
// calls during OAuth2 authorization flows.
//
// For the CLI (first-party, trusted client), consent is auto-approved.
// Login redirects to Kratos self-service login flow.
package consent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

// Handler manages Hydra's login and consent callbacks.
type Handler struct {
	hydra             *auth.HydraClient
	tenantProvisioner *auth.TenantProvisioner
	kratosPublicURL   string // Browser-facing URL (for redirects)
	kratosInternalURL string // Cluster-internal URL (for server-to-server calls)
	kratosClient      *http.Client
	logger            *slog.Logger
}

// NewHandler creates a consent handler.
// kratosPublicURL is used for browser redirects, kratosInternalURL for server-to-server (whoami).
func NewHandler(hydra *auth.HydraClient, tenantProv *auth.TenantProvisioner, kratosPublicURL, kratosInternalURL string, logger *slog.Logger) *Handler {
	if kratosInternalURL == "" {
		kratosInternalURL = kratosPublicURL
	}
	return &Handler{
		hydra:             hydra,
		tenantProvisioner: tenantProv,
		kratosPublicURL:   kratosPublicURL,
		kratosInternalURL: kratosInternalURL,
		kratosClient:     &http.Client{Timeout: 5 * time.Second},
		logger:           logger,
	}
}

// Login handles the login challenge from Hydra.
// Hydra redirects here when a user needs to authenticate.
// We redirect to Kratos self-service login, passing the challenge.
// GET /auth/login?login_challenge=...
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("login_challenge")
	if challenge == "" {
		http.Error(w, "Missing login_challenge", http.StatusBadRequest)
		return
	}

	loginReq, err := h.hydra.GetLoginRequest(challenge)
	if err != nil {
		h.logger.Error("failed to get login request", "error", err)
		http.Error(w, "Authentication service error", http.StatusBadGateway)
		return
	}

	// If the user has already authenticated (skip=true), accept immediately.
	if loginReq.Skip {
		redirectTo, err := h.hydra.AcceptLogin(challenge, loginReq.Subject)
		if err != nil {
			h.logger.Error("failed to accept login", "error", err)
			http.Error(w, "Authentication service error", http.StatusBadGateway)
			return
		}
		http.Redirect(w, r, redirectTo, http.StatusFound)
		return
	}

	// Redirect to Kratos self-service login flow.
	// After Kratos authenticates the user, it redirects back here
	// with the identity session, and we accept the Hydra login.
	kratosLoginURL := fmt.Sprintf("%s/self-service/login/browser?return_to=%s",
		h.kratosPublicURL,
		url.QueryEscape(fmt.Sprintf("/auth/login/complete?login_challenge=%s", challenge)),
	)
	http.Redirect(w, r, kratosLoginURL, http.StatusFound)
}

// LoginComplete is called after Kratos authenticates the user.
// It validates the Kratos session cookie, then accepts the Hydra login challenge.
// GET /auth/login/complete?login_challenge=...
func (h *Handler) LoginComplete(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("login_challenge")
	if challenge == "" {
		http.Error(w, "Missing login_challenge", http.StatusBadRequest)
		return
	}

	// Validate Kratos session — the user must have an active session cookie.
	// Without this check, an attacker could call /auth/login/complete directly.
	subject, err := h.getKratosSessionSubject(r)
	if err != nil {
		h.logger.Error("kratos session validation failed", "error", err, "challenge", challenge)
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	redirectTo, err := h.hydra.AcceptLogin(challenge, subject)
	if err != nil {
		h.logger.Error("failed to accept login", "error", err)
		http.Error(w, "Authentication service error", http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// getKratosSessionSubject validates the Kratos session cookie and returns the subject.
func (h *Handler) getKratosSessionSubject(r *http.Request) (string, error) {
	// Forward the cookies from the original request to Kratos /sessions/whoami.
	// Uses internal URL to avoid going through external proxy/TLS.
	whoamiURL := fmt.Sprintf("%s/sessions/whoami", h.kratosInternalURL)
	req, err := http.NewRequest("GET", whoamiURL, nil)
	if err != nil {
		return "", fmt.Errorf("create whoami request: %w", err)
	}

	// Forward ALL cookies — Kratos uses dynamic cookie names (e.g., csrf_token_{hash}).
	for _, cookie := range r.Cookies() {
		req.AddCookie(cookie)
	}

	resp, err := h.kratosClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("kratos whoami request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("no active kratos session (status %d)", resp.StatusCode)
	}

	var session struct {
		Identity struct {
			ID string `json:"id"`
		} `json:"identity"`
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return "", fmt.Errorf("decode kratos session: %w", err)
	}

	if !session.Active || session.Identity.ID == "" {
		return "", fmt.Errorf("kratos session is not active")
	}

	return session.Identity.ID, nil
}

// fetchIdentityClaims fetches Kratos identity traits (email, name) for JWT claims.
func (h *Handler) fetchIdentityClaims(subject string) map[string]interface{} {
	claims := map[string]interface{}{}

	// Fetch identity from Kratos admin API (server-to-server).
	// Admin API has /admin/identities endpoint; public API doesn't.
	kratosAdmin := h.kratosInternalURL // Same cluster, admin is on separate port
	// Try admin URL pattern: replace "public" with "admin" in service name
	kratosAdmin = strings.Replace(kratosAdmin, "kratos-public", "kratos-admin", 1)
	identityURL := fmt.Sprintf("%s/admin/identities/%s", kratosAdmin, subject)
	resp, err := h.kratosClient.Get(identityURL)
	if err != nil {
		h.logger.Warn("failed to fetch identity for claims", "subject", subject, "error", err)
		return claims
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		h.logger.Warn("identity fetch returned non-200", "subject", subject, "status", resp.StatusCode)
		return claims
	}

	var identity struct {
		Traits struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"traits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		h.logger.Warn("failed to decode identity", "error", err)
		return claims
	}

	if identity.Traits.Email != "" {
		claims["email"] = identity.Traits.Email
	}
	if identity.Traits.Name != "" {
		claims["name"] = identity.Traits.Name
	}

	// Resolve tenant — injects tenant_id into Hydra JWT for CLI auth.
	// MEET-2026-04-07-039 B5: EnsureTenant is non-fatal in consent flow.
	// If provisioning fails, JWT is issued WITHOUT tenant_id. The auth middleware
	// (B7 fix) will reject API calls with "Account setup required" instead of
	// silently falling back to a "default" tenant.
	// This decouples authentication availability from DB availability.
	if h.tenantProvisioner != nil && identity.Traits.Email != "" {
		ctx := context.Background()
		tenantID, _, err := h.tenantProvisioner.EnsureTenant(ctx, subject, identity.Traits.Email)
		if err == nil && tenantID != "" {
			claims["tenant_id"] = tenantID
		} else if err != nil {
			h.logger.Error("tenant provisioning failed during consent — JWT will lack tenant_id",
				"subject", subject,
				"email", identity.Traits.Email,
				"error", err,
			)
			// Do NOT fail the consent flow. Auth succeeds; tenant resolution retries on next login.
		}
	}

	return claims
}

// Consent handles the consent challenge from Hydra.
// For the CLI (first-party client), auto-approve all requested scopes.
// GET /auth/consent?consent_challenge=...
func (h *Handler) Consent(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("consent_challenge")
	if challenge == "" {
		http.Error(w, "Missing consent_challenge", http.StatusBadRequest)
		return
	}

	consentReq, err := h.hydra.GetConsentRequest(challenge)
	if err != nil {
		h.logger.Error("failed to get consent request", "error", err)
		http.Error(w, "Authentication service error", http.StatusBadGateway)
		return
	}

	// Fetch user identity from Kratos to include email/name in JWT.
	tokenClaims := h.fetchIdentityClaims(consentReq.Subject)

	// Auto-approve — LoopZero CLI is a first-party trusted client.
	// Forward requested audience so Hydra sets aud in the token.
	redirectTo, err := h.hydra.AcceptConsent(challenge, consentReq.RequestedScope, consentReq.RequestedAccessTokenAud, tokenClaims)
	if err != nil {
		h.logger.Error("failed to accept consent", "error", err)
		http.Error(w, "Authentication service error", http.StatusBadGateway)
		return
	}

	h.logger.Info("consent auto-approved",
		"client", consentReq.ClientID,
		"subject", consentReq.Subject,
		"scopes", consentReq.RequestedScope,
		"claims", tokenClaims,
	)
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// DeviceVerify handles the device authorization challenge from Hydra.
// When a user visits the device verification URL, Hydra redirects here with a challenge.
// This handler validates the Kratos session, then accepts the device challenge.
// GET /auth/device?device_challenge=...&user_code=...
func (h *Handler) DeviceVerify(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("device_challenge")
	if challenge == "" {
		http.Error(w, "Missing device_challenge", http.StatusBadRequest)
		return
	}

	// Validate Kratos session — user must be authenticated
	subject, err := h.getKratosSessionSubject(r)
	if err != nil {
		h.logger.Warn("device verify: no session, redirecting to login", "error", err)
		// Redirect to Kratos login with return_to back to this device verify URL
		returnTo := r.URL.String()
		loginURL := fmt.Sprintf("%s/self-service/login/browser?return_to=%s",
			h.kratosPublicURL, url.QueryEscape(returnTo))
		http.Redirect(w, r, loginURL, http.StatusFound)
		return
	}

	// Accept the device challenge via Hydra admin
	userCode := r.URL.Query().Get("user_code")
	redirectTo, err := h.hydra.AcceptDeviceChallenge(challenge, userCode)
	if err != nil {
		h.logger.Error("device verify: failed to accept device challenge", "error", err, "subject", subject)
		http.Error(w, "Failed to authorize device. Please try again.", http.StatusBadGateway)
		return
	}
	h.logger.Info("device authorized", "subject", subject, "user_code", userCode)

	if redirectTo != "" {
		http.Redirect(w, r, redirectTo, http.StatusFound)
		return
	}

	// Success — show confirmation
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html><html><body style="font-family:system-ui;display:flex;align-items:center;justify-content:center;height:100vh;background:#0a0a0a;color:#fff;margin:0"><div style="text-align:center"><h2>CLI authorized</h2><p style="color:#888">You can close this tab and return to your terminal.</p></div></body></html>`)
}

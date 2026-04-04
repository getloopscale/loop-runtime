// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package consent implements the login/consent handlers that Ory Hydra
// calls during OAuth2 authorization flows.
//
// For the CLI (first-party, trusted client), consent is auto-approved.
// Login redirects to Kratos self-service login flow.
package consent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

// Handler manages Hydra's login and consent callbacks.
type Handler struct {
	hydra           *auth.HydraClient
	kratosPublicURL string
	kratosClient    *http.Client
	logger          *slog.Logger
}

// NewHandler creates a consent handler.
func NewHandler(hydra *auth.HydraClient, kratosPublicURL string, logger *slog.Logger) *Handler {
	return &Handler{
		hydra:           hydra,
		kratosPublicURL: kratosPublicURL,
		kratosClient:    &http.Client{Timeout: 5 * time.Second},
		logger:          logger,
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
	// Forward the cookies from the original request to Kratos /sessions/whoami
	whoamiURL := fmt.Sprintf("%s/sessions/whoami", h.kratosPublicURL)
	req, err := http.NewRequest("GET", whoamiURL, nil)
	if err != nil {
		return "", fmt.Errorf("create whoami request: %w", err)
	}

	// Forward session cookies
	for _, cookie := range r.Cookies() {
		if cookie.Name == "ory_kratos_session" || cookie.Name == "csrf_token" {
			req.AddCookie(cookie)
		}
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

	// Auto-approve — LoopZero CLI is a first-party trusted client.
	redirectTo, err := h.hydra.AcceptConsent(challenge, consentReq.RequestedScope)
	if err != nil {
		h.logger.Error("failed to accept consent", "error", err)
		http.Error(w, "Authentication service error", http.StatusBadGateway)
		return
	}

	h.logger.Info("consent auto-approved",
		"client", consentReq.ClientID,
		"subject", consentReq.Subject,
		"scopes", consentReq.RequestedScope,
	)
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

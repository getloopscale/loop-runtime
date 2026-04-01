// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package consent implements the login/consent handlers that Ory Hydra
// calls during OAuth2 authorization flows.
//
// For the CLI (first-party, trusted client), consent is auto-approved.
// Login redirects to Kratos self-service login flow.
package consent

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

// Handler manages Hydra's login and consent callbacks.
type Handler struct {
	hydra           *auth.HydraClient
	kratosPublicURL string
	logger          *slog.Logger
}

// NewHandler creates a consent handler.
func NewHandler(hydra *auth.HydraClient, kratosPublicURL string, logger *slog.Logger) *Handler {
	return &Handler{
		hydra:           hydra,
		kratosPublicURL: kratosPublicURL,
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
// It accepts the Hydra login challenge with the authenticated subject.
// GET /auth/login/complete?login_challenge=...
func (h *Handler) LoginComplete(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("login_challenge")
	if challenge == "" {
		http.Error(w, "Missing login_challenge", http.StatusBadRequest)
		return
	}

	// Extract the Kratos session to get the subject (user identity).
	// In a full implementation, this reads the Kratos session cookie.
	// For now, we accept with the subject from the login request
	// (Kratos session validation will be added in Phase 3-4).
	loginReq, err := h.hydra.GetLoginRequest(challenge)
	if err != nil {
		h.logger.Error("failed to get login request for completion", "error", err)
		http.Error(w, "Authentication service error", http.StatusBadGateway)
		return
	}

	redirectTo, err := h.hydra.AcceptLogin(challenge, loginReq.Subject)
	if err != nil {
		h.logger.Error("failed to accept login", "error", err)
		http.Error(w, "Authentication service error", http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
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

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

// SessionTokenHandler exchanges Kratos session cookies for JWT tokens.
// This is the BFF (Backend For Frontend) token exchange endpoint.
// The dashboard calls this after Kratos login to get a JWT for all API calls.
type SessionTokenHandler struct {
	issuer            *auth.SessionTokenIssuer
	kratosInternalURL string
}

// NewSessionTokenHandler creates a session token exchange handler.
func NewSessionTokenHandler(issuer *auth.SessionTokenIssuer, kratosInternalURL string) *SessionTokenHandler {
	return &SessionTokenHandler{issuer: issuer, kratosInternalURL: kratosInternalURL}
}

// Exchange validates the Kratos session cookie and returns a JWT.
// POST /v1/auth/session/token
func (h *SessionTokenHandler) Exchange(w http.ResponseWriter, r *http.Request) {
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

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "AUTH_SERVICE_UNAVAILABLE", "Authentication service unavailable. Try again.")
		return
	}
	defer resp.Body.Close()

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

	// Issue JWT
	token, expiresIn, err := h.issuer.Issue(
		session.Identity.ID,
		session.Identity.Traits.Email,
		session.Identity.Traits.Name,
		"default", // Tenant resolved later via DB lookup
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

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package routes implements HTTP handlers for the cloud auth service.
package routes

import (
	"encoding/json"
	"net/http"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

// DeviceHandler handles Device Code Flow endpoints.
type DeviceHandler struct {
	hydra         *auth.HydraClient
	dashboardURL  string // Public dashboard URL for verification redirect
}

// NewDeviceHandler creates a new DeviceHandler.
func NewDeviceHandler(hydra *auth.HydraClient, dashboardURL string) *DeviceHandler {
	return &DeviceHandler{hydra: hydra, dashboardURL: dashboardURL}
}

// Authorize starts the Device Code Flow (RFC 8628).
// POST /v1/auth/device/authorize
func (h *DeviceHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	resp, err := h.hydra.DeviceAuthorize("openid offline_access")
	if err != nil {
		writeError(w, http.StatusBadGateway, "AUTH_SERVICE_ERROR",
			"Authentication service is temporarily unavailable. Try again in a moment.")
		return
	}

	// Override verification_uri to use the branded dashboard URL.
	// The dashboard routes /oauth2/* to Hydra via Traefik gateway.
	verificationURI := resp.VerificationURI
	if h.dashboardURL != "" {
		verificationURI = h.dashboardURL + "/oauth2/device/verify"
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user_code":                 resp.UserCode,
		"verification_uri":          verificationURI,
		"verification_uri_complete": verificationURI + "?user_code=" + resp.UserCode,
		"expires_in":                resp.ExpiresIn,
		"interval":                  resp.Interval,
		"device_code":               resp.DeviceCode,
	})
}

// Token exchanges a device code for an access token.
// POST /v1/auth/device/token
func (h *DeviceHandler) Token(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Missing device_code in request body.")
		return
	}

	token, err := h.hydra.DeviceToken(req.DeviceCode)
	if err != nil {
		writeError(w, http.StatusBadGateway, "AUTH_SERVICE_ERROR",
			"Authentication service error. Try again.")
		return
	}

	if token == nil {
		// Authorization pending — client should keep polling
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "pending",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"token_type":    token.TokenType,
		"expires_in":    token.ExpiresIn,
	})
}

// Refresh exchanges a refresh token for a new access token.
// POST /v1/auth/token/refresh
func (h *DeviceHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Missing refresh_token in request body.")
		return
	}

	token, err := h.hydra.RefreshToken(req.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "REFRESH_FAILED",
			"Session expired. Run `loopzero login` to authenticate again.")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"token_type":    token.TokenType,
		"expires_in":    token.ExpiresIn,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]interface{}{
		"code":  code,
		"error": message,
	})
}

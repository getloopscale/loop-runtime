// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package routes implements HTTP handlers for the cloud auth service.
package routes

import (
	"encoding/json"
	"log/slog"
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

	// Use Hydra's native verification URI — CSRF cookies are domain-bound.
	// Routing through cloud gateway breaks CSRF because cookies are set for auth.{domain}.
	verificationURI := resp.VerificationURI

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user_code":                 resp.UserCode,
		"verification_uri":          verificationURI,
		"verification_uri_complete": verificationURI + "?user_code=" + resp.UserCode,
		"expires_in":                resp.ExpiresIn,
		"interval":                  resp.Interval,
		"device_code":               resp.DeviceCode,
	})
}

// Accept handles the dashboard's device-verify form submission. The browser
// page collected the human-readable user_code and forwards it together with
// the device_challenge Hydra emits. We delegate to Hydra admin
// AcceptDeviceChallenge (which validates the user_code server-side and
// returns redirect_to to continue the OAuth login + consent dance).
// POST /v1/auth/device/accept
//
// Plan J-05 amendment 2026-06-05 — Hydra v2.6 requires the operator to host
// the device-verify UI. The page lives at /device-verify in the dashboard;
// this handler is the only backend hop it needs.
func (h *DeviceHandler) Accept(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceChallenge string `json:"device_challenge"`
		UserCode        string `json:"user_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.DeviceChallenge == "" || req.UserCode == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST",
			"Missing device_challenge or user_code.")
		return
	}

	redirectTo, err := h.hydra.AcceptDeviceChallenge(req.DeviceChallenge, req.UserCode)
	if err != nil {
		// T6.2 LIVE catch #18 visibility patch — surface Hydra error before mask
		slog.Error("device accept: hydra AcceptDeviceChallenge failed",
			"err", err.Error(),
			"user_code", req.UserCode,
			"challenge_prefix", func() string {
				if len(req.DeviceChallenge) > 12 {
					return req.DeviceChallenge[:12] + "..."
				}
				return req.DeviceChallenge
			}(),
		)
		writeError(w, http.StatusBadGateway, "AUTH_SERVICE_ERROR",
			"Could not authorize this device. Double-check the code or run `loopzero login --device-flow` again.")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"redirect_to": redirectTo,
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
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes a JSON error response with request_id for tracing (§4.3).
func writeError(w http.ResponseWriter, status int, code, message string) {
	resp := map[string]interface{}{
		"code":    code,
		"message": message,
	}
	if rid := w.Header().Get("X-Request-Id"); rid != "" {
		resp["request_id"] = rid
	}
	writeJSON(w, status, resp)
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package argocdhttp serves /internal/argocd-callback: the sync notification the
// GitOps controller posts back when an application reaches a new state.
//
// Twenty-fifth and last family to leave the flat `routes` package (B-080,
// slice-40). Internal-only, no user context — a sibling of the webhook in how the
// router registers it.
package argocdhttp

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ArgoCallbackPayload is the webhook payload sent by ArgoCD Notifications.
// ArgoCD sends this when an Application reaches a configured trigger state.
type ArgoCallbackPayload struct {
	// AppName is the ArgoCD Application name (e.g., "tn-loopruntime-production")
	AppName string `json:"app_name"`
	// SyncStatus is "Synced" or "OutOfSync"
	SyncStatus string `json:"sync_status"`
	// HealthStatus is "Healthy", "Progressing", "Degraded", etc.
	HealthStatus string `json:"health_status"`
	// Revision is the git commit SHA that was synced
	Revision string `json:"revision"`
}

// ReleaseActivator activates releases for a given ArgoCD application that
// has reached Synced + Healthy state.
type ReleaseActivator interface {
	OnSyncCompleted(appName, revision string) error
}

// ArgoCallbackHandler handles POST /internal/argocd-callback from ArgoCD Notifications.
// This replaces the Phase D synchronous polling pattern with a push-based callback.
// MEET-2026-03-27-012: ArgoCD notifies the API when sync completes, instead of
// the API polling ArgoCD every 10 seconds for up to 600 seconds.
type ArgoCallbackHandler struct {
	activator ReleaseActivator
	logger    *slog.Logger
	secret    string // SEC-004: shared secret for webhook authentication
}

// NewArgoCallbackHandler creates a new ArgoCallbackHandler.
// When secret is non-empty, requests must include Authorization: Bearer <secret>.
func NewArgoCallbackHandler(activator ReleaseActivator, logger *slog.Logger, secret string) *ArgoCallbackHandler {
	return &ArgoCallbackHandler{activator: activator, logger: logger, secret: secret}
}

// Handle processes the ArgoCD webhook callback.
// POST /internal/argocd-callback
// SEC-004: Validates shared secret when configured.
func (h *ArgoCallbackHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// SEC-004: Validate callback secret when configured.
	// B3: Use constant-time comparison to prevent timing attacks on secret values.
	if h.secret != "" {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(h.secret)) != 1 {
			h.logger.Warn("argocd callback: unauthorized request")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}

	var payload ArgoCallbackPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Warn("argocd callback: invalid payload", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	_, span := otel.Tracer("loopruntime-api").Start(r.Context(), "loopruntime.api.argocd.callback_received",
		trace.WithAttributes(
			attribute.String("loopruntime.argocd.app_name", payload.AppName),
			attribute.String("loopruntime.argocd.sync_status", payload.SyncStatus),
			attribute.String("loopruntime.argocd.health_status", payload.HealthStatus),
		))
	defer span.End()

	h.logger.Info("argocd callback received",
		"app", payload.AppName,
		"sync", payload.SyncStatus,
		"health", payload.HealthStatus,
		"revision", payload.Revision)

	if payload.SyncStatus != "Synced" || payload.HealthStatus != "Healthy" {
		// Not a completion event — ignore (ArgoCD may send intermediate states)
		w.WriteHeader(http.StatusOK)
		return
	}

	if h.activator != nil {
		if err := h.activator.OnSyncCompleted(payload.AppName, payload.Revision); err != nil {
			h.logger.Error("argocd callback: activation failed",
				"app", payload.AppName, "revision", payload.Revision, "error", err)
			// 200 on purpose: the callback is a fast path, not the only one.
			// ReleaseConvergenceLoop (services/release) polls every PENDING
			// release and activates it once its app is Synced + Healthy, so a
			// failed activation here is retried by the loop, not by the sender.
		}
	}

	w.WriteHeader(http.StatusOK)
}

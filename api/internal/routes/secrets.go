// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/domain"
	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// Type aliases for backward compatibility -- canonical definitions are in domain/.
type (
	SecretMetadata = domain.SecretMetadata
	SecretStore    = domain.SecretStore
	SecretK8sSync  = domain.SecretK8sSync
	AuditLogger    = domain.AuditLogger
)

// SecretHandler provides HTTP handlers for secret operations.
type SecretHandler struct {
	store   SecretStore
	k8sSync SecretK8sSync // optional — nil disables K8s sync
	audit   AuditLogger   // optional — nil disables audit logging
}

// NewSecretHandler creates a new SecretHandler.
func NewSecretHandler(store SecretStore) *SecretHandler {
	return &SecretHandler{store: store}
}

// SetAuditLogger configures audit logging for sensitive operations.
func (h *SecretHandler) SetAuditLogger(audit AuditLogger) {
	h.audit = audit
}

// SetK8sSync configures the K8s sync for creating Secrets and ExternalSecret CRs.
func (h *SecretHandler) SetK8sSync(sync SecretK8sSync) {
	h.k8sSync = sync
}

// secretListResponse is the JSON response for GET /api/v1/secrets.
type secretListResponse struct {
	Secrets []SecretMetadata `json:"secrets"`
}

// secretSetRequest is the JSON body for POST /api/v1/secrets.
type secretSetRequest struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Environment string `json:"environment"`
	App         string `json:"app"`
}

// secretSetResponse is the JSON response for POST /api/v1/secrets.
// Value is NEVER included in the response.
type secretSetResponse struct {
	Name        string `json:"name"`
	Environment string `json:"environment"`
	App         string `json:"app"`
	Created     bool   `json:"created"`
}

// secretDeleteResponse is the JSON response for DELETE /api/v1/secrets/{name}.
type secretDeleteResponse struct {
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

// secretNameRegex validates secret names: uppercase letters, digits, underscores.
var secretNameRegex = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// List handles GET /api/v1/secrets?env={env}&project={project}.
// Returns secret names only — NEVER values.
func (h *SecretHandler) List(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	env := r.URL.Query().Get("env")
	project := r.URL.Query().Get("project")

	secrets, err := h.store.ListNames(r.Context(), uc.TenantID, project, env)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	if secrets == nil {
		secrets = []SecretMetadata{}
	}

	writeJSON(w, http.StatusOK, secretListResponse{Secrets: secrets})
}

// Set handles POST /api/v1/secrets.
// Creates or updates a secret. Value is NEVER returned in the response.
func (h *SecretHandler) Set(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req secretSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("invalid request body"))
		return
	}

	// Validate name format
	if !secretNameRegex.MatchString(req.Name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "INVALID_SECRET_NAME",
			"message": "Secret name must be uppercase with underscores (e.g., DATABASE_URL). Got: '" + req.Name + "'.",
		})
		return
	}

	// Validate required fields
	if req.Value == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("value is required"))
		return
	}
	if req.Environment == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("environment is required"))
		return
	}
	if req.App == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("app is required"))
		return
	}

	// Use project name from query or default
	project := r.URL.Query().Get("project")

	created, err := h.store.Set(ctx, uc.TenantID, project, req.Environment, req.App, req.Name, []byte(req.Value))
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// MEET-2026-03-31-024: Audit log for secret operations
	if h.audit != nil {
		action := "SECRET_UPDATED"
		if created {
			action = "SECRET_CREATED"
		}
		if auditErr := h.audit.LogAudit(ctx, uc.TenantID, action, uc.UserID,
			"secret", req.Name, r.Header.Get("Correlation-ID"),
			map[string]any{"environment": req.Environment, "app": req.App, "project": project}); auditErr != nil {
			slog.Error("failed to write audit log for secret set", "name", req.Name, "error", auditErr)
		}
	}

	// Create K8s Secret on mgmt + ExternalSecret CR on workload for ESO sync
	if h.k8sSync != nil {
		if syncErr := h.k8sSync.CreateSecret(ctx, uc.TenantID, project, req.Environment, req.App, req.Name, req.Value); syncErr != nil {
			// Log but don't fail — PG is source of truth, K8s sync is eventual
			slog.Error("failed to sync secret to K8s", "name", req.Name, "error", syncErr)
		}
	}

	writeJSON(w, http.StatusOK, secretSetResponse{
		Name:        req.Name,
		Environment: req.Environment,
		App:         req.App,
		Created:     created,
	})
}

// DeleteSecret handles DELETE /api/v1/secrets/{name}?environment={env}&app={app}.
func (h *SecretHandler) DeleteSecret(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("secret name is required"))
		return
	}

	env := r.URL.Query().Get("environment")
	app := r.URL.Query().Get("app")
	project := r.URL.Query().Get("project")

	deleted, err := h.store.Delete(ctx, uc.TenantID, project, env, app, name)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	if !deleted {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"code":    "SECRET_NOT_FOUND",
			"message": "Secret '" + name + "' not found.",
		})
		return
	}

	// MEET-2026-03-31-024: Audit log for secret deletion
	if h.audit != nil {
		if auditErr := h.audit.LogAudit(ctx, uc.TenantID, "SECRET_DELETED", uc.UserID,
			"secret", name, r.Header.Get("Correlation-ID"),
			map[string]any{"environment": env, "app": app, "project": project}); auditErr != nil {
			slog.Error("failed to write audit log for secret delete", "name", name, "error", auditErr)
		}
	}

	// Remove K8s Secret from mgmt + ExternalSecret CR from workload
	if h.k8sSync != nil {
		if syncErr := h.k8sSync.DeleteSecret(ctx, uc.TenantID, env, app, name); syncErr != nil {
			slog.Error("failed to delete secret from K8s", "name", name, "error", syncErr)
		}
	}

	writeJSON(w, http.StatusOK, secretDeleteResponse{
		Name:    name,
		Deleted: true,
	})
}

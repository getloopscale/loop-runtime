// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/domain"
	"github.com/loopzero/loopzero/api/internal/metrics"
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

// SecretEnvLocker is the narrow interface SecretHandler needs to wrap
// mutations in `pg_advisory_xact_lock` (T1.1 of deep-review-2026-05-10-
// remediation-plan). Defined locally per ISP — accepts any locker the
// services layer exposes, including `services.EnvironmentLocker` and
// `adapters.PoolEnvironmentLocker`.
//
// Nil-safe: when handler.locker is nil, Set/DeleteSecret fall back to
// the legacy non-locked path. Production wiring must inject the real
// locker; this fallback keeps unit-test fakes simple.
type SecretEnvLocker interface {
	WithLock(ctx context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error
}

// SecretHandler provides HTTP handlers for secret operations.
type SecretHandler struct {
	store   SecretStore
	k8sSync SecretK8sSync   // optional — nil disables K8s sync
	audit   AuditLogger     // optional — nil disables audit logging
	locker  SecretEnvLocker // optional — nil disables environment lock (legacy path)
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

// SetLocker configures the environment lock for secret mutations. When set,
// Set/DeleteSecret acquire a pg_advisory_xact_lock on the (tenant, project,
// env) key BEFORE invoking the store mutation, serializing with concurrent
// `loopzero deploy` flows that hold the same lock.
//
// Sprint A T1.1 (closes comp-001 CRITICAL): the locker was added to the
// handler struct in deep-review-2026-05-10-remediation-plan but never
// wired from server.Config. This setter mirrors SetK8sSync exactly so
// server/router.go can inject the locker post-construction.
//
// Lock-domain parity (EC-2): inject *lock.PoolLocker (via
// adapters.PoolEnvironmentLocker) — same advisory key as
// lock.WithEnvironmentLock used by build handlers. Any other locker
// implementation breaks cross-feature serialization.
//
// K8s sync OUTSIDE lock (EC-3, intentional design): h.k8sSync.CreateSecret
// runs AFTER mutate returns AND AFTER the lock releases — PG is canonical,
// K8s is eventual, and the lock has 5s timeout. Moving K8s sync inside
// mutate is a REGRESSION VECTOR (would kill secret Sets every time K8s
// API server is slow).
func (h *SecretHandler) SetLocker(locker SecretEnvLocker) {
	h.locker = locker
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

	// T1.1 of deep-review-2026-05-10-remediation-plan: serialize mutations
	// per (tenant, project, env) so concurrent `loopzero secret set` + a deploy
	// capturing config cannot race. Falls back to non-locked path when
	// locker is nil (tests + boot path).
	var created bool
	mutate := func(_ db.DBTX) error {
		var setErr error
		created, setErr = h.store.Set(ctx, uc.TenantID, project, req.Environment, req.App, req.Name, []byte(req.Value))
		return setErr
	}
	var err error
	if h.locker != nil {
		err = h.locker.WithLock(ctx, uc.TenantID, project, req.Environment, mutate)
	} else {
		err = mutate(nil)
	}
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrLockTimeout) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"code":    "DEPLOY_IN_PROGRESS",
				"message": "Another operation is in progress for this environment. Retry shortly.",
			})
			return
		}
		// V9 L18: mirror mapBuildError + promote.go pattern — log unexpected
		// errors for diagnosis. The silent-swallow pre-L18 made every
		// secret-set failure indistinguishable from any other 5xx.
		slog.Error("unexpected error in secret set handler",
			"error", err.Error(),
			"type", fmt.Sprintf("%T", err),
			"tenant_id", uc.TenantID,
			"project", project,
			"environment", req.Environment,
			"app", req.App,
			"secret_name", req.Name,
		)
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

	// Create K8s Secret on mgmt + ExternalSecret CR on workload for ESO sync.
	// dogfood-2026-05-08-fixes T2.3 (F-2): emit "skip" metric when k8sSync is
	// nil — that path was the silent-failure cause, formerly invisible. Now
	// `rate(loopzero_secret_externalsecret_total{result="skip"}[5m]) > 0` alerts
	// that the API is silently no-opping `loopzero secret set`.
	if h.k8sSync != nil {
		if syncErr := h.k8sSync.CreateSecret(ctx, uc.TenantID, project, req.Environment, req.App, req.Name, req.Value); syncErr != nil {
			// Log but don't fail — PG is source of truth, K8s sync is eventual
			slog.Error("failed to sync secret to K8s", "name", req.Name, "error", syncErr)
		}
	} else {
		metrics.SecretExternalSecretTotal.WithLabelValues("skip").Inc()
		slog.Warn("ExternalSecret sync skipped — k8sSync nil at boot",
			"tenant", uc.TenantID,
			"app", req.App,
			"environment", req.Environment,
			"secret_name", req.Name)
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

	// Sprint A T1.1 EC-1: validate environment query param. Without this,
	// with the locker wired, empty-env DELETEs produce lock key
	// "{tenant}:{project}:" (trailing colon) — two concurrent empty-env
	// deletes contend on that single bucket → bogus DEPLOY_IN_PROGRESS
	// 409s, and the store.Delete matches L19 PK rows that include the
	// empty-env slot. Mirror the Set handler validation pattern.
	if env == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("environment is required"))
		return
	}

	// T1.1 of deep-review-2026-05-10-remediation-plan: same lock as Set.
	var deleted bool
	mutate := func(_ db.DBTX) error {
		var delErr error
		deleted, delErr = h.store.Delete(ctx, uc.TenantID, project, env, app, name)
		return delErr
	}
	var err error
	if h.locker != nil {
		err = h.locker.WithLock(ctx, uc.TenantID, project, env, mutate)
	} else {
		err = mutate(nil)
	}
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrLockTimeout) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"code":    "DEPLOY_IN_PROGRESS",
				"message": "Another operation is in progress for this environment. Retry shortly.",
			})
			return
		}
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

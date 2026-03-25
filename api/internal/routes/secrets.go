package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// SecretMetadata holds the name and context of a secret — never its value.
type SecretMetadata struct {
	Name        string `json:"name"`
	Environment string `json:"environment"`
	App         string `json:"app"`
}

// SecretStore abstracts secret persistence (DIP).
// Implementations MUST NOT expose secret values in list operations.
type SecretStore interface {
	ListNames(ctx context.Context, tenantID, projectID, env string) ([]SecretMetadata, error)
	Set(ctx context.Context, tenantID, projectID, env, app, name string, value []byte) (created bool, err error)
	Delete(ctx context.Context, tenantID, projectID, env, app, name string) (deleted bool, err error)
}

// SecretHandler provides HTTP handlers for secret operations.
type SecretHandler struct {
	store SecretStore
}

// NewSecretHandler creates a new SecretHandler.
func NewSecretHandler(store SecretStore) *SecretHandler {
	return &SecretHandler{store: store}
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
	uc := middleware.GetUserContext(r.Context())
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

	created, err := h.store.Set(r.Context(), uc.TenantID, project, req.Environment, req.App, req.Name, []byte(req.Value))
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
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
	uc := middleware.GetUserContext(r.Context())
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

	deleted, err := h.store.Delete(r.Context(), uc.TenantID, project, env, app, name)
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

	writeJSON(w, http.StatusOK, secretDeleteResponse{
		Name:    name,
		Deleted: true,
	})
}

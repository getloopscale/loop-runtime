package routes

import (
	"context"
	"net/http"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// SecretMetadata holds the name and context of a secret — never its value.
type SecretMetadata struct {
	Name        string `json:"name"`
	Environment string `json:"environment"`
	App         string `json:"app"`
}

// SecretStore abstracts secret metadata retrieval (DIP).
// Implementations MUST return only names, never values.
type SecretStore interface {
	ListNames(ctx context.Context, tenantID, projectID, env string) ([]SecretMetadata, error)
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

	// Ensure we always return an array, never null
	if secrets == nil {
		secrets = []SecretMetadata{}
	}

	writeJSON(w, http.StatusOK, secretListResponse{Secrets: secrets})
}

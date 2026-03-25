package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// RollbackService abstracts the rollback orchestrator for the handler (DIP).
type RollbackService interface {
	Rollback(ctx context.Context, req RollbackInput) (*RollbackOutput, error)
}

// RollbackInput is the input for the rollback service from the handler.
type RollbackInput struct {
	TenantID      string
	ProjectID     string
	ProjectName   string
	Environment   string
	ToReleaseID   string
	CorrelationID string
	UserID        string
	BaseDomain    string
}

// RollbackOutput is the result from the rollback service.
type RollbackOutput struct {
	RolledBackReleaseID  string
	ReactivatedReleaseID string
	Environment          string
	URL                  string
}

// RollbackProjectStore abstracts project lookup for rollback (DIP).
type RollbackProjectStore interface {
	GetByName(ctx context.Context, tenantID, name string) (*ProjectResult, error)
}

// rollbackRequest is the JSON body for POST /api/v1/projects/{name}/rollback.
type rollbackRequest struct {
	Environment string `json:"environment"`
	ToReleaseID string `json:"to_release_id"`
}

// rollbackResponse is the JSON response for a successful rollback.
type rollbackResponse struct {
	Status              string `json:"status"`
	RolledBackRelease   string `json:"rolled_back_release"`
	ReactivatedRelease  string `json:"reactivated_release"`
	Environment         string `json:"environment"`
	URL                 string `json:"url"`
}

// RollbackHandler provides HTTP handlers for rollback operations.
type RollbackHandler struct {
	projects   RollbackProjectStore
	service    RollbackService
	baseDomain string
}

// NewRollbackHandler creates a new RollbackHandler.
func NewRollbackHandler(projects RollbackProjectStore, service RollbackService, baseDomain string) *RollbackHandler {
	return &RollbackHandler{
		projects:   projects,
		service:    service,
		baseDomain: baseDomain,
	}
}

// Create handles POST /api/v1/projects/{name}/rollback.
func (h *RollbackHandler) Create(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	// Parse request body
	var req rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("invalid request body"))
		return
	}

	env := req.Environment
	if env == "" {
		env = "production"
	}

	// Look up project
	project, err := h.projects.GetByName(r.Context(), uc.TenantID, name)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		writeLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	correlationID := r.Header.Get("X-Correlation-ID")

	// Execute rollback
	result, err := h.service.Rollback(r.Context(), RollbackInput{
		TenantID:      uc.TenantID,
		ProjectID:     project.ID,
		ProjectName:   name,
		Environment:   env,
		ToReleaseID:   req.ToReleaseID,
		CorrelationID: correlationID,
		UserID:        uc.UserID,
		BaseDomain:    h.baseDomain,
	})
	if err != nil {
		var loopzeroErr *loopzeroerrors.LoopZeroError
		if errors.As(err, &loopzeroErr) {
			writeLoopZeroError(w, loopzeroErr)
			return
		}
		// Lock timeout → 409
		if isLockTimeout(err) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"code":    "DEPLOY_IN_PROGRESS",
				"message": "Another operation is in progress. Try again in a moment.",
			})
			return
		}
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	resp := rollbackResponse{
		Status:             "completed",
		RolledBackRelease:  result.RolledBackReleaseID,
		ReactivatedRelease: result.ReactivatedReleaseID,
		Environment:        result.Environment,
		URL:                result.URL,
	}

	writeJSON(w, http.StatusOK, resp)
}

// isLockTimeout checks if the error is a lock timeout (advisory lock busy).
func isLockTimeout(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, loopzeroerrors.ErrLockTimeout) || strings.Contains(err.Error(), "lock timeout")
}

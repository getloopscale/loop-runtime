package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

var (
	// ErrProjectExists is returned when a project with the same name already exists in the tenant.
	ErrProjectExists = errors.New("project already exists")

	// rfc1123 validates Kubernetes-compatible names: lowercase, alphanumeric, hyphens, max 63 chars.
	rfc1123 = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// CreateProjectInput is the input for creating a project.
type CreateProjectInput struct {
	TenantID  string
	Name      string
	CreatedBy string
}

// ProjectResult is the result of creating a project.
type ProjectResult struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"team"`
}

// ProjectStore abstracts project persistence (DIP).
type ProjectStore interface {
	CreateProject(ctx context.Context, input CreateProjectInput) (ProjectResult, error)
}

// TenantCRSpec holds the spec for creating/updating a Tenant CR.
type TenantCRSpec struct {
	Plan string
}

// TenantCRManager abstracts Kubernetes Tenant CR operations (DIP).
type TenantCRManager interface {
	EnsureTenant(ctx context.Context, tenantID string, spec TenantCRSpec) error
}

// ProjectHandler provides HTTP handlers for project operations.
type ProjectHandler struct {
	store   ProjectStore
	crMgr   TenantCRManager
}

// NewProjectHandler creates a new ProjectHandler.
func NewProjectHandler(store ProjectStore, crMgr TenantCRManager) *ProjectHandler {
	return &ProjectHandler{store: store, crMgr: crMgr}
}

// createProjectRequest is the JSON request body for POST /api/v1/projects.
type createProjectRequest struct {
	Name string                 `json:"name"`
	Apps []map[string]interface{} `json:"apps,omitempty"`
}

// Create handles POST /api/v1/projects.
func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}

	// Validate name (RFC 1123)
	if !rfc1123.MatchString(req.Name) {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidName.Wrap("project name must be lowercase, alphanumeric, and may contain hyphens"))
		return
	}

	// Create project in DB
	result, err := h.store.CreateProject(r.Context(), CreateProjectInput{
		TenantID:  uc.TenantID,
		Name:      req.Name,
		CreatedBy: uc.UserID,
	})
	if err != nil {
		if errors.Is(err, ErrProjectExists) {
			writeLoopZeroError(w, loopzeroerrors.ErrProjectExists)
			return
		}
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// Create/update Tenant CR in Kubernetes — this triggers the TenantController.
	// Best-effort: project is already persisted, Tenant CR can be reconciled later.
	_ = h.crMgr.EnsureTenant(r.Context(), uc.TenantID, TenantCRSpec{Plan: "starter"})

	writeJSON(w, http.StatusCreated, result)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeLoopZeroError writes a LoopZeroError as a JSON response with the correct HTTP status.
func writeLoopZeroError(w http.ResponseWriter, err *loopzeroerrors.LoopZeroError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.HTTPStatus)
	_ = json.NewEncoder(w).Encode(err.ToJSON())
}

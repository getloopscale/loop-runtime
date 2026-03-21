package routes

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

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
	ID     string `json:"id"`
	Name   string `json:"name"`
	TeamID string `json:"team"`
}

// ProjectStore abstracts project persistence (DIP).
type ProjectStore interface {
	CreateProject(ctx context.Context, input CreateProjectInput) (ProjectResult, error)
	GetByName(ctx context.Context, tenantID, name string) (*ProjectResult, error)
	DeleteProject(ctx context.Context, tenantID, projectID string) error
}

// TenantCRSpec holds the spec for creating/updating a Tenant CR.
type TenantCRSpec struct {
	Plan string
}

// TenantCRManager abstracts Kubernetes Tenant CR operations (DIP).
type TenantCRManager interface {
	EnsureTenant(ctx context.Context, tenantID string, spec TenantCRSpec) error
}

// ProjectDeleteInput is the input for deleting a project.
type ProjectDeleteInput struct {
	TenantID      string
	ProjectID     string
	ProjectName   string
	Environment   string // empty = all environments
	DeletedBy     string
	CorrelationID string
}

// ProjectDeleteOutput is the result of project deletion.
type ProjectDeleteOutput struct {
	ProjectName         string   `json:"project"`
	DeletedEnvironments []string `json:"deleted_environments"`
	CancelledBuilds     int      `json:"cancelled_builds"`
}

// ProjectDeleter abstracts project deletion orchestration (DIP — routes don't import services).
type ProjectDeleter interface {
	Delete(ctx context.Context, req ProjectDeleteInput) (*ProjectDeleteOutput, error)
}

// ProjectHandler provides HTTP handlers for project operations.
type ProjectHandler struct {
	store   ProjectStore
	crMgr   TenantCRManager
	deleter ProjectDeleter
}

// NewProjectHandler creates a new ProjectHandler.
func NewProjectHandler(store ProjectStore, crMgr TenantCRManager) *ProjectHandler {
	return &ProjectHandler{store: store, crMgr: crMgr}
}

// SetDeleter configures the project deleter. Called from main.go after all deps are ready.
func (h *ProjectHandler) SetDeleter(d ProjectDeleter) {
	h.deleter = d
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
	if err := h.crMgr.EnsureTenant(r.Context(), uc.TenantID, TenantCRSpec{Plan: "starter"}); err != nil {
		slog.Error("failed to ensure tenant CR — deploy will fail until resolved",
			"tenant_id", uc.TenantID,
			"error", err.Error(),
		)
	}

	writeJSON(w, http.StatusCreated, result)
}

// Delete handles DELETE /api/v1/projects/{name}.
// Deletes a project from all environments.
func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	if h.deleter == nil {
		slog.Error("project delete handler called but deleter is not configured")
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	// Look up project by name
	project, err := h.store.GetByName(r.Context(), uc.TenantID, name)
	if err != nil {
		slog.Error("project lookup failed", "project", name, "error", err)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		writeLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	correlationID := r.Header.Get("Correlation-ID")

	result, err := h.deleter.Delete(r.Context(), ProjectDeleteInput{
		TenantID:      uc.TenantID,
		ProjectID:     project.ID,
		ProjectName:   name,
		Environment:   "", // all environments
		DeletedBy:     uc.UserID,
		CorrelationID: correlationID,
	})
	if err != nil {
		slog.Error("project delete failed", "project", name, "error", err)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// DeleteEnvironment handles DELETE /api/v1/projects/{name}/environments/{env}.
// Deletes a project from a specific environment.
func (h *ProjectHandler) DeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	if h.deleter == nil {
		slog.Error("project delete environment handler called but deleter is not configured")
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	name := chi.URLParam(r, "name")
	env := chi.URLParam(r, "env")
	if name == "" || env == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name and environment are required"))
		return
	}

	// Look up project by name
	project, err := h.store.GetByName(r.Context(), uc.TenantID, name)
	if err != nil {
		slog.Error("project lookup failed", "project", name, "error", err)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		writeLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	correlationID := r.Header.Get("Correlation-ID")

	result, err := h.deleter.Delete(r.Context(), ProjectDeleteInput{
		TenantID:      uc.TenantID,
		ProjectID:     project.ID,
		ProjectName:   name,
		Environment:   env,
		DeletedBy:     uc.UserID,
		CorrelationID: correlationID,
	})
	if err != nil {
		slog.Error("project delete failed", "project", name, "env", env, "error", err)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusOK, result)
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

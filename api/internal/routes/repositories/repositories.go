// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package repositories serves the project↔repository link: connecting a GitHub
// installation to a project and answering what a project is connected to.
//
// Every collaborator it needs is declared here as an interface it owns
// (RepositoryStore, and the narrow structs beside it) rather than imported from the
// persistence layer — the DIP boundary that keeps a route family from depending on
// how the link happens to be stored.
package repositories

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/getloopscale/loopzero/api/internal/ports"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/getloopscale/loopzero/api/internal/github"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/routes/httpio"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// ProjectRepository represents a project↔repo link (DIP — avoids db import).
type ProjectRepository struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	ProjectName    string    `json:"project_name"`
	Provider       string    `json:"provider"`
	RepoFullName   string    `json:"repo_full_name"`
	InstallationID int64     `json:"installation_id"`
	DefaultBranch  string    `json:"default_branch"`
	AutoDeploy     bool      `json:"auto_deploy"`
	CreatedAt      time.Time `json:"created_at"`
}

// RepositoryStore abstracts project↔repository persistence (DIP).
type RepositoryStore interface {
	// Plan J-09 T3.2 (2026-06-05): autoDeploy parameter added; was hardcoded true.
	LinkRepository(ctx context.Context, tenantID, projectName, repoFullName string, installationID int64, defaultBranch string, autoDeploy bool) error
	UnlinkRepository(ctx context.Context, tenantID, projectName string) error
	GetByProject(ctx context.Context, tenantID, projectName string) (*ProjectRepository, error)
	GetByRepo(ctx context.Context, repoFullName string) (*ProjectRepository, error)
	SaveInstallation(ctx context.Context, id int64, accountLogin, accountType string, tenantID *string) error
	// InstallationTenant returns the tenant a GitHub installation is bound
	// to, or "" when it is unclaimed or unknown.
	InstallationTenant(ctx context.Context, installationID int64) (string, error)
}

// RepoIntrospector abstracts the GitHub API calls the Connect handler makes
// for loopzero.yaml validation. Plan J-09 T5.1 (2026-06-05) — interface (not
// *AppClient direct) so tests can stub without minting JWTs.
type RepoIntrospector interface {
	BranchExists(ctx context.Context, installationID int64, owner, repo, ref string) error
	GetFileContent(ctx context.Context, installationID int64, owner, repo, ref, path string) ([]byte, error)
	// connect-then-deploy 2026-06-09 — sponsor walkthrough catch: Connect
	// must deploy HEAD immediately (Vercel-canonical UX). GetHeadCommit
	// returns SHA + message + author needed by autoDeployer.HandlePush.
	GetHeadCommit(ctx context.Context, installationID int64, owner, repo, branch string) (*github.HeadCommitInfo, error)
}

// Autodeployer abstracts the auto-deploy entrypoint so the Connect handler
// can trigger a build on link without depending on the concrete
// *github.AutoDeployer (DIP). Mirrors AutoDeployer.HandlePush signature
// verbatim — see api/internal/github/autodeploy.go.
type Autodeployer interface {
	HandlePush(ctx context.Context, req github.PushRequest) error
}

// RepositoryHandler handles project↔GitHub repository linking.
type RepositoryHandler struct {
	store        RepositoryStore
	introspector RepoIntrospector // optional — nil disables loopzero.yaml validation
	autoDeployer Autodeployer     // optional — nil disables connect-then-deploy
	serverCtx    context.Context  // lifetime of the deploys Connect starts; set with autoDeployer
}

// NewRepositoryHandler creates a new RepositoryHandler.
func NewRepositoryHandler(store RepositoryStore) *RepositoryHandler {
	return &RepositoryHandler{store: store}
}

// WithIntrospector wires a RepoIntrospector for loopzero.yaml validation.
// Plan J-09 T5.1 — production wiring passes a *github.AppClient via main.
func (h *RepositoryHandler) WithIntrospector(i RepoIntrospector) *RepositoryHandler {
	h.introspector = i
	return h
}

// WithAutoDeployer wires an Autodeployer so Connect triggers an immediate
// HEAD deploy after a successful link (Vercel-canonical UX). When nil,
// Connect behaves as pre-2026-06-09 (link only — first deploy waits for
// the next push event). Sponsor walkthrough 2026-06-09 catch.
//
// serverCtx is the server's lifetime (the shutdown context): the deploy runs
// after the response, and binding it there lets a graceful stop cancel it —
// the same bound the push webhook's deploys have.
func (h *RepositoryHandler) WithAutoDeployer(d Autodeployer, serverCtx context.Context) *RepositoryHandler {
	if serverCtx == nil {
		panic("repositories: WithAutoDeployer requires the server context")
	}
	h.autoDeployer = d
	h.serverCtx = serverCtx
	return h
}

type connectRequest struct {
	Repo           string `json:"repo"` // "owner/repo"
	InstallationID int64  `json:"installation_id"`
	Branch         string `json:"branch,omitempty"` // default: "main"
	// Plan J-09 T3.2 (2026-06-05) — explicit auto_deploy toggle (column
	// already exists in project_repositories per T0.1 recon). Pointer to
	// preserve "unset" vs "false" semantics — nil → default true, false → off.
	AutoDeploy *bool `json:"auto_deploy,omitempty"`
}

type connectResponse struct {
	OK         bool   `json:"ok"`
	Project    string `json:"project"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	AutoDeploy bool   `json:"auto_deploy"`
}

// Connect links a GitHub repository to a LoopZero project (POST /api/v1/projects/{name}/connect).
//
// Plan 2026-06-15-code-review-49-findings-remediation T5.4 (R2-F3 HIGH):
// CC 18 → ≤ 11 via 2 helper extracts (parseConnectRequest + checkRepoLinkConflict).
func (h *RepositoryHandler) Connect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	projectName := chi.URLParam(r, "name")

	req, branch, ok := parseConnectRequest(w, r)
	if !ok {
		return // parseConnectRequest wrote the response
	}

	if !h.checkRepoLinkConflict(ctx, w, req.Repo, uc.TenantID, projectName) {
		return // checkRepoLinkConflict wrote the response
	}

	// An installation bound to another tenant is not rebound: the upsert
	// below would COALESCE this caller's tenant over the existing binding.
	// 403, as the repository picker answers for the same installation.
	owner, err := h.store.InstallationTenant(ctx, req.InstallationID)
	if err != nil {
		slog.ErrorContext(ctx, "installation lookup failed", "error", err, "installation_id", req.InstallationID)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "internal error"})
		return
	}
	if owner != "" && owner != uc.TenantID {
		httpio.WriteJSON(w, http.StatusForbidden, map[string]string{
			"code":    "FORBIDDEN",
			"message": "You do not have access to this installation.",
		})
		return
	}

	// Bind the installation to this tenant (first connect) or keep it.
	tenantID := uc.TenantID
	if err := h.store.SaveInstallation(ctx, req.InstallationID, "", "User", &tenantID); err != nil {
		slog.ErrorContext(ctx, "save installation failed", "error", err)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "internal error"})
		return
	}

	// Plan J-09 T3.2: default auto_deploy=true when caller omits field (back-compat).
	autoDeploy := true
	if req.AutoDeploy != nil {
		autoDeploy = *req.AutoDeploy
	}

	// Plan J-09 T5.1 (2026-06-05) — pre-link validation: branch must exist,
	// loopzero.yaml must be present at that ref, and the file must parse as YAML.
	// Three distinct error codes so the UI can render actionable copy
	// (EC-10 + EC-4). Validation is skipped only when no introspector is
	// wired (test/local without GitHub credentials).
	if h.introspector != nil {
		if !h.validateLoopZeroYamlOnRef(ctx, w, req, branch) {
			return // validateLoopZeroYamlOnRef wrote the response
		}
	}

	// Link (idempotent — if already linked to same project, no-op via unique constraint)
	if err := h.store.LinkRepository(ctx, uc.TenantID, projectName, req.Repo, req.InstallationID, branch, autoDeploy); err != nil {
		slog.ErrorContext(ctx, "link repository failed", "error", err, "repo", req.Repo)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "failed to connect repository"})
		return
	}

	slog.InfoContext(ctx, "repository connected", "project", projectName, "repo", req.Repo, "branch", branch, "auto_deploy", autoDeploy)

	if autoDeploy {
		h.triggerConnectThenDeploy(ctx, uc.TenantID, projectName, req, branch)
	}

	httpio.WriteJSON(w, http.StatusOK, connectResponse{
		OK:         true,
		Project:    projectName,
		Repo:       req.Repo,
		Branch:     branch,
		AutoDeploy: autoDeploy,
	})
}

// parseConnectRequest decodes + validates the POST body. Returns the
// parsed request, default-applied branch, and a bool indicating whether
// to continue (true) or stop (false — response already written).
func parseConnectRequest(w http.ResponseWriter, r *http.Request) (connectRequest, string, bool) {
	var req connectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{"code": "INTERNAL_ERROR", "message": "invalid request body"})
		return req, "", false
	}
	if req.Repo == "" || req.InstallationID == 0 {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "repo and installation_id are required",
		})
		return req, "", false
	}
	branch := req.Branch
	if branch == "" {
		branch = "main"
	}
	return req, branch, true
}

// checkRepoLinkConflict returns true when the repo is OK to link (either
// unlinked OR already linked to this same project). Returns false +
// writes a 409 response when the repo is linked to a different
// (tenant, project) tuple.
func (h *RepositoryHandler) checkRepoLinkConflict(
	ctx context.Context,
	w http.ResponseWriter,
	repo, tenantID, projectName string,
) bool {
	existing, err := h.store.GetByRepo(ctx, repo)
	if err != nil {
		slog.ErrorContext(ctx, "repository lookup failed", "error", err)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "internal error"})
		return false
	}
	if existing != nil && (existing.TenantID != tenantID || existing.ProjectName != projectName) {
		httpio.WriteJSON(w, http.StatusConflict, map[string]string{
			"code":    "REPO_ALREADY_LINKED",
			"message": "Repository is already connected to another project.",
		})
		return false
	}
	return true
}

// Disconnect unlinks a GitHub repository from a LoopZero project (DELETE /api/v1/projects/{name}/connect).
func (h *RepositoryHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	projectName := chi.URLParam(r, "name")

	if err := h.store.UnlinkRepository(ctx, uc.TenantID, projectName); err != nil {
		// AUDIT-21: map "not linked" to 404 instead of silent success.
		if errors.Is(err, ports.ErrRepositoryNotLinked) {
			httpio.WriteLoopZeroError(w, loopzeroerrors.NewRepositoryNotLinked(projectName))
			return
		}
		slog.ErrorContext(ctx, "unlink repository failed", "error", err)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "failed to disconnect"})
		return
	}

	slog.InfoContext(ctx, "repository disconnected", "project", projectName)

	httpio.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"project": projectName,
	})
}

// GetConnection returns the linked repository for a project (GET /api/v1/projects/{name}/connect).
func (h *RepositoryHandler) GetConnection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	projectName := chi.URLParam(r, "name")

	repo, err := h.store.GetByProject(ctx, uc.TenantID, projectName)
	if err != nil {
		slog.ErrorContext(ctx, "get connection failed", "error", err)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "internal error"})
		return
	}
	if repo == nil {
		httpio.WriteJSON(w, http.StatusNotFound, map[string]string{
			"code":    "NOT_CONNECTED",
			"message": "No repository connected to this project.",
		})
		return
	}

	// Plan J-09 dogfood 2026-06-06: dashboard derives the "connected"
	// state from the `connected` boolean (see project-connect.tsx FSM).
	// Without this flag the dashboard had a repo row in the DB but the
	// UI silently kept showing the picker -- "no feedback" symptom.
	// installation_id is included so effectiveConn does not need to merge
	// the URL query param post-connect.
	httpio.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"connected":       true,
		"project":         repo.ProjectName,
		"repo":            repo.RepoFullName,
		"provider":        repo.Provider,
		"branch":          repo.DefaultBranch,
		"auto_deploy":     repo.AutoDeploy,
		"installation_id": repo.InstallationID,
		"connected_at":    repo.CreatedAt,
	})
}

// validateLoopZeroYamlOnRef runs the pre-link introspector chain: branch
// existence + loopzero.yaml presence + loopzero.yaml parses as non-empty YAML
// mapping. Returns true when validation passed; false (and writes the
// HTTP response) on any failure. Extracted from Connect for T4.2 of
// plan 2026-06-11-code-review-remediation v1.2 + ADR D6 — keeps the
// Connect orchestrator focused on link-creation logic.
//
// Plan J-09 T5.1 (2026-06-05) — three distinct error codes so the UI
// can render actionable copy (EC-10 + EC-4):
//   - INVALID_REPO            (400) — repo not "owner/name" format
//   - BRANCH_NOT_FOUND        (400) — branch missing on GitHub
//   - MISSING_LOOPZERO_YAML       (400) — file absent at that ref
//   - INVALID_LOOPZERO_YAML       (400) — empty, malformed, or zero fields
//   - GITHUB_UNAVAILABLE      (502) — GitHub API transport failure
func (h *RepositoryHandler) validateLoopZeroYamlOnRef(ctx context.Context, w http.ResponseWriter, req connectRequest, branch string) bool {
	parts := strings.SplitN(req.Repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "INVALID_REPO",
			"message": "Repository must be in 'owner/name' format.",
		})
		return false
	}
	owner, name := parts[0], parts[1]

	if err := h.introspector.BranchExists(ctx, req.InstallationID, owner, name, branch); err != nil {
		var bnf *github.BranchNotFoundError
		if errors.As(err, &bnf) {
			httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
				"code":    "BRANCH_NOT_FOUND",
				"message": "Branch \"" + branch + "\" was not found on " + req.Repo + ".",
			})
			return false
		}
		slog.ErrorContext(ctx, "branch check failed", "error", err, "repo", req.Repo, "branch", branch)
		httpio.WriteJSON(w, http.StatusBadGateway, map[string]string{
			"code":    "GITHUB_UNAVAILABLE",
			"message": "Could not verify the branch right now. Try again in a moment.",
		})
		return false
	}

	content, err := h.introspector.GetFileContent(ctx, req.InstallationID, owner, name, branch, "loopzero.yaml")
	if err != nil {
		var fnf *github.FileNotFoundError
		if errors.As(err, &fnf) {
			httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
				"code":    "MISSING_LOOPZERO_YAML",
				"message": "loopzero.yaml not found on branch \"" + branch + "\". Run `loopzero init` locally and commit the file. Docs: https://docs.loopzero.example/config/loopzero-yaml",
			})
			return false
		}
		slog.ErrorContext(ctx, "loopzero.yaml fetch failed", "error", err, "repo", req.Repo)
		httpio.WriteJSON(w, http.StatusBadGateway, map[string]string{
			"code":    "GITHUB_UNAVAILABLE",
			"message": "Could not fetch loopzero.yaml right now. Try again in a moment.",
		})
		return false
	}

	// EC-4 / EC-19: loopzero.yaml MUST parse as a non-empty YAML mapping.
	if len(strings.TrimSpace(string(content))) == 0 {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "INVALID_LOOPZERO_YAML",
			"message": "loopzero.yaml is empty. Add at least a `name:` field. Docs: https://docs.loopzero.example/config/loopzero-yaml",
		})
		return false
	}
	var parsed map[string]interface{}
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "INVALID_LOOPZERO_YAML",
			"message": "loopzero.yaml is not valid YAML: " + err.Error(),
		})
		return false
	}
	if len(parsed) == 0 {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "INVALID_LOOPZERO_YAML",
			"message": "loopzero.yaml has no fields. Add at least a `name:` entry.",
		})
		return false
	}
	return true
}

// triggerConnectThenDeploy dispatches the Vercel-canonical immediate
// deploy of HEAD after a successful link. Extracted from Connect for
// T4.2 of plan 2026-06-11-code-review-remediation v1.2 + ADR D6 —
// keeps the synchronous link path separate from the async deploy
// dispatch.
//
// connect-then-deploy 2026-06-09 (sponsor walkthrough catch): without
// this, the dashboard shows "No deployments yet" and forces the user
// to push an extra commit to see the first build. We dispatch the
// same code path the push-webhook uses (autoDeployer.HandlePush)
// asynchronously so the HTTP response is not delayed by the source
// download.
//
// Honest-failure contract: GetHeadCommit error does NOT fail the HTTP
// response — the link is in place and the next push will still trigger
// a normal webhook autodeploy. We surface a WARN so operators see this
// in logs.
func (h *RepositoryHandler) triggerConnectThenDeploy(ctx context.Context, tenantID, projectName string, req connectRequest, branch string) {
	if h.autoDeployer == nil || h.introspector == nil {
		return
	}
	parts := strings.SplitN(req.Repo, "/", 2)
	if len(parts) != 2 {
		return
	}
	head, hcErr := h.introspector.GetHeadCommit(ctx, req.InstallationID, parts[0], parts[1], branch)
	if hcErr != nil {
		slog.WarnContext(ctx, "connect-then-deploy: GetHeadCommit failed — first deploy will wait for next push",
			"error", hcErr, "project", projectName, "repo", req.Repo, "branch", branch)
		return
	}
	if head == nil || head.SHA == "" {
		return
	}
	repoFull := req.Repo
	instID := req.InstallationID
	deployer := h.autoDeployer
	go func() {
		// Outlives the request (Connect answers before the source download)
		// but not the server: cancelled on graceful stop.
		bgCtx, cancel := context.WithTimeout(h.serverCtx, 5*time.Minute)
		defer cancel()
		if depErr := deployer.HandlePush(bgCtx, github.PushRequest{
			RepoFullName: repoFull, Branch: branch, CommitSHA: head.SHA, InstallationID: instID,
			TenantID: tenantID, ProjectName: projectName,
			CommitMessage: head.CommitMessage, SenderLogin: head.AuthorLogin,
		}); depErr != nil {
			slog.Error("connect-then-deploy: HandlePush failed",
				"error", depErr, "project", projectName, "sha", head.SHA)
		}
	}()
	slog.InfoContext(ctx, "connect-then-deploy: triggered",
		"project", projectName, "sha", head.SHA, "branch", branch)
}

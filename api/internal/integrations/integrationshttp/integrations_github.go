// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package integrationshttp serves /api/v1/integrations/github: the dashboard's view
// of which GitHub App installations a tenant has, and the revocation state of
// each.
//
// Eleventh family to leave the flat `routes` package (B-080, slice-33), extracted
// alongside `bootstrap`.
package integrationshttp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/getloopscale/loopzero/api/internal/httpio"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// InstallationRepository is the sanitised slice of a GitHub installation
// repository the dashboard picker renders. Lives here (not in the github
// package) so the routes package does not pull in github and we avoid the
// test-side import cycle (internal/github/preview_deploy_test.go ->
// internal/routes). Adapters in cmd/ translate the github package shape
// into this one.
type InstallationRepository struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Language      string `json:"language,omitempty"`
}

// InstallationRecord is the minimal persistence-side shape needed for the
// tenant boundary check. See InstallationRepository comment for the same
// rationale (no github import in routes).
type InstallationRecord struct {
	ID       int64
	TenantID *string
}

// ErrInstallationRevoked is returned by InstallationRepoLister when GitHub
// reports the installation as revoked/uninstalled (HTTP 404/403/401).
// The handler maps this to HTTP 410 GONE so the dashboard can prompt a
// reinstall. errors.Is(err, ErrInstallationRevoked) for the comparison.
var ErrInstallationRevoked = errors.New("github installation revoked")

// InstallationRepoLister is the narrow interface IntegrationsGitHubHandler
// uses to list repositories per installation. Implementations live in
// cmd/ adapters and translate the github package shape to ours so the
// routes package never imports internal/github (avoids test-side import
// cycle). When the upstream signals revocation the adapter MUST wrap
// ErrInstallationRevoked so errors.Is(err, ErrInstallationRevoked) is true.
//
// Plan J-09 T2.1.
type InstallationRepoLister interface {
	ListInstallationRepositories(ctx context.Context, installationID int64, page int) ([]InstallationRepository, error)
}

// InstallationLookup is the slice of RepositoryStore the handler needs --
// just installation ownership lookup. Avoids dragging the full
// RepositoryStore surface into a route that only reads installation rows.
type InstallationLookup interface {
	GetInstallation(ctx context.Context, installationID int64) (*InstallationRecord, error)
}

// IntegrationsGitHubHandler serves the dashboard repo picker
// (GET /api/v1/integrations/github/installations/{id}/repositories).
type IntegrationsGitHubHandler struct {
	lister InstallationRepoLister
	store  InstallationLookup
	clock  func() time.Time

	cacheMu sync.Mutex
	cache   map[int64]repoListCacheEntry
}

type repoListCacheEntry struct {
	expiresAt time.Time
	repos     []InstallationRepository
}

// repoListCacheTTL bounds GitHub upstream traffic. 60s covers a single
// dashboard session (user picks repo within seconds of install) and
// limits us to <100 calls/hr per installation -- well under the 5000/hr
// installation token quota.
const repoListCacheTTL = 60 * time.Second

// NewIntegrationsGitHubHandler wires the handler.
func NewIntegrationsGitHubHandler(lister InstallationRepoLister, store InstallationLookup) *IntegrationsGitHubHandler {
	return &IntegrationsGitHubHandler{
		lister: lister,
		store:  store,
		clock:  time.Now,
		cache:  make(map[int64]repoListCacheEntry),
	}
}

// withClock is used by tests to control time.
func (h *IntegrationsGitHubHandler) withClock(now func() time.Time) *IntegrationsGitHubHandler {
	h.clock = now
	return h
}

// ListRepositories handles GET /api/v1/integrations/github/installations/{id}/repositories.
func (h *IntegrationsGitHubHandler) ListRepositories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	installationID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || installationID <= 0 {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "INVALID_INSTALLATION_ID",
			"message": "installation_id must be a positive integer",
		})
		return
	}

	// Tenant boundary: a tenant may only list repositories of installations
	// that have already been linked to their tenant (or whose ownership
	// row is still NULL, i.e. fresh-from-callback before the first
	// connect). Cross-tenant requests get 403, not 404, so we don't
	// leak the existence of someone else's installation.
	inst, err := h.store.GetInstallation(ctx, installationID)
	if err != nil {
		slog.ErrorContext(ctx, "installation lookup failed", "error", err, "installation_id", installationID)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL_ERROR",
			"message": "internal error",
		})
		return
	}
	if inst != nil && inst.TenantID != nil && *inst.TenantID != uc.TenantID {
		httpio.WriteJSON(w, http.StatusForbidden, map[string]string{
			"code":    "FORBIDDEN",
			"message": "You do not have access to this installation.",
		})
		return
	}

	// Cache lookup (60s TTL). The cache key includes only installationID
	// because the upstream response is identical for any tenant -- the
	// tenant gate above already filtered cross-tenant calls.
	if repos, ok := h.cacheGet(installationID); ok {
		httpio.WriteJSON(w, http.StatusOK, map[string]any{"repositories": repos})
		return
	}

	// Upstream: GitHub /installation/repositories. We currently fetch only
	// page 1 -- 100 repos covers the >99% installation case. Pagination
	// can land in T2.2 when the picker UI asks for it.
	repos, err := h.lister.ListInstallationRepositories(ctx, installationID, 1)
	if err != nil {
		if errors.Is(err, ErrInstallationRevoked) {
			httpio.WriteJSON(w, http.StatusGone, map[string]string{
				"code":    "INSTALLATION_REVOKED",
				"message": "GitHub App access has been removed. Reinstall to continue.",
			})
			return
		}
		slog.ErrorContext(ctx, "list installation repositories failed", "error", err, "installation_id", installationID)
		httpio.WriteJSON(w, http.StatusBadGateway, map[string]string{
			"code":    "GITHUB_UNAVAILABLE",
			"message": "Could not reach GitHub. Try again in a moment.",
		})
		return
	}

	h.cachePut(installationID, repos)
	httpio.WriteJSON(w, http.StatusOK, map[string]any{"repositories": repos})
}

func (h *IntegrationsGitHubHandler) cacheGet(id int64) ([]InstallationRepository, bool) {
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()
	entry, ok := h.cache[id]
	if !ok {
		return nil, false
	}
	if h.clock().After(entry.expiresAt) {
		delete(h.cache, id)
		return nil, false
	}
	return entry.repos, true
}

func (h *IntegrationsGitHubHandler) cachePut(id int64, repos []InstallationRepository) {
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()
	h.cache[id] = repoListCacheEntry{
		expiresAt: h.clock().Add(repoListCacheTTL),
		repos:     repos,
	}
}

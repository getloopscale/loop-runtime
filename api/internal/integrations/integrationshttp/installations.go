// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integrationshttp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/httpio"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// Installation is a GitHub App installation as the console lists it.
type Installation struct {
	ID           int64  `json:"id"`
	AccountLogin string `json:"account_login"`
	AccountType  string `json:"account_type"`
}

// ErrOAuthCodeRejected is returned by UserInstallationSource when GitHub
// refuses the authorization code: wrong, expired or exchanged already.
var ErrOAuthCodeRejected = errors.New("github rejected the authorization code")

// UserInstallationSource asks GitHub which installations of this App a user
// can access. The user is the one who authorized on GitHub and received code.
// An adapter wraps ErrOAuthCodeRejected when GitHub refuses the code; any
// other error means GitHub could not be asked.
type UserInstallationSource interface {
	InstallationsForCode(ctx context.Context, code string) ([]Installation, error)
}

// InstallationClaimStore holds which tenant an installation belongs to.
type InstallationClaimStore interface {
	// ClaimInstallation binds inst to tenantID, recording that GitHub
	// confirmed userID can access it. It reports false and writes nothing
	// when the installation belongs to another tenant.
	ClaimInstallation(ctx context.Context, inst Installation, tenantID, userID string) (bool, error)
	// ListVerifiedInstallations returns the installations bound to tenantID
	// with the binding confirmed on GitHub.
	ListVerifiedInstallations(ctx context.Context, tenantID string) ([]Installation, error)
}

// WithInstallationClaims wires the claim and list endpoints. access may be nil
// when the App's OAuth client credentials are not configured: the claim then
// answers 503 and no installation can be bound.
func (h *IntegrationsGitHubHandler) WithInstallationClaims(access UserInstallationSource, claims InstallationClaimStore) *IntegrationsGitHubHandler {
	h.access = access
	h.claims = claims
	return h
}

// claimRequest is the body of the claim: the one-time code GitHub handed to
// the console's callback after the user authorized the App.
type claimRequest struct {
	Code string `json:"code"`
}

// maxClaimBodyBytes bounds the claim body, which carries one short code.
const maxClaimBodyBytes = 4 << 10

// ClaimInstallations handles POST /api/v1/integrations/github/installations/claim.
//
// It binds to the caller's tenant the installations of this App that GitHub
// says the user who authorized can access, and answers the installations now
// verified for the tenant. GitHub is the only source of the installation ids:
// the request carries none. An installation that belongs to another tenant is
// left alone. Nothing is bound unless GitHub answered both of its calls, and
// the user's token is used for those calls only.
func (h *IntegrationsGitHubHandler) ClaimInstallations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req claimRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxClaimBodyBytes)).Decode(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "INVALID_REQUEST",
			"message": "code is required: the code GitHub returned after the user authorized the App.",
		})
		return
	}

	if h.access == nil || h.claims == nil {
		httpio.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"code":    "GITHUB_OAUTH_NOT_CONFIGURED",
			"message": "GitHub authorization is not configured on this server.",
		})
		return
	}

	accessible, err := h.access.InstallationsForCode(ctx, strings.TrimSpace(req.Code))
	if errors.Is(err, ErrOAuthCodeRejected) {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "GITHUB_CODE_REJECTED",
			"message": "GitHub did not accept the authorization code. It expired or was already used. Authorize on GitHub again.",
		})
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "github installation claim: GitHub could not be asked", "error", err, "tenant_id", uc.TenantID)
		httpio.WriteJSON(w, http.StatusBadGateway, map[string]string{
			"code":    "GITHUB_UNAVAILABLE",
			"message": "Could not reach GitHub. Try again in a moment.",
		})
		return
	}

	bound, skipped := 0, 0
	for _, inst := range accessible {
		ok, err := h.claims.ClaimInstallation(ctx, inst, uc.TenantID, uc.UserID)
		if err != nil {
			slog.ErrorContext(ctx, "github installation claim: bind failed", "error", err, "installation_id", inst.ID, "tenant_id", uc.TenantID)
			httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "internal error"})
			return
		}
		if ok {
			bound++
		} else {
			skipped++
		}
	}
	slog.InfoContext(ctx, "github installations claimed",
		"tenant_id", uc.TenantID, "user_id", uc.UserID, "bound", bound, "bound_to_another_tenant", skipped)

	h.writeVerifiedInstallations(w, r, uc.TenantID)
}

// ListInstallations handles GET /api/v1/integrations/github/installations: the
// installations verified for the caller's tenant.
func (h *IntegrationsGitHubHandler) ListInstallations(w http.ResponseWriter, r *http.Request) {
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}
	if h.claims == nil {
		httpio.WriteJSON(w, http.StatusOK, map[string]any{"installations": []Installation{}})
		return
	}
	h.writeVerifiedInstallations(w, r, uc.TenantID)
}

func (h *IntegrationsGitHubHandler) writeVerifiedInstallations(w http.ResponseWriter, r *http.Request, tenantID string) {
	ctx := r.Context()
	installations, err := h.claims.ListVerifiedInstallations(ctx, tenantID)
	if err != nil {
		slog.ErrorContext(ctx, "list verified installations failed", "error", err, "tenant_id", tenantID)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "internal error"})
		return
	}
	if installations == nil {
		installations = []Installation{}
	}
	httpio.WriteJSON(w, http.StatusOK, map[string]any{"installations": installations})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Plan dashboard-memory-bff-server-side-key-vault v1.2 § T2.1.
//
// DashboardMemoryHandler is the BFF (Backend-For-Frontend) handler family
// for /v1/dashboard/memory/*. JWT-authenticated browser sessions hit these
// endpoints; the handler resolves a server-side loopzero_live_* API key via
// BFFKeyVault (the key NEVER touches the browser per ADR D1), then delegates
// the actual memory operation to the existing MemoryHandler.
//
// SECURITY INVARIANT (per Plan Goal): the secret returned by MintOrFetch
// is DISCARDED immediately. It is NEVER written to w, NEVER logged, NEVER
// set in a cookie. The mint is for AUDIT/REVOKE bookkeeping only.

package routes

import (
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

const dashboardMemoryTracer = "github.com/loopzero/loopzero/cloud/internal/routes/dashboard_memory"

// DashboardMemoryHandler delegates browser-trust calls through the BFF vault
// to the existing MemoryHandler infrastructure.
type DashboardMemoryHandler struct {
	vault         *auth.BFFKeyVault
	memoryHandler *MemoryHandler
	logger        *slog.Logger
}

// NewDashboardMemoryHandler constructs a DashboardMemoryHandler. Both vault
// and memoryHandler MUST be non-nil; logger MUST be non-nil per
// .claude/rules/loopzero-design.md § Error Philosophy (fail-loud at construction).
func NewDashboardMemoryHandler(v *auth.BFFKeyVault, m *MemoryHandler, logger *slog.Logger) *DashboardMemoryHandler {
	if v == nil || m == nil || logger == nil {
		panic("dashboard_memory: vault, memoryHandler, and logger required")
	}
	return &DashboardMemoryHandler{vault: v, memoryHandler: m, logger: logger}
}

// mintOrFail wraps the vault MintOrFetch call shared by every BFF method.
// On error, writes the appropriate status + diagnostic + returns false.
// On success, sets the OTel span attribute and returns true.
//
// SECURITY: the returned secret is DISCARDED here (not returned). The mint
// is for audit-trail bookkeeping only — the actual upstream call to TheMemory
// uses the master THEMORY_API_KEY (per GO-TO CTO Invariant 3).
func (h *DashboardMemoryHandler) mintOrFail(w http.ResponseWriter, r *http.Request, spanSuffix string) bool {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated.")
		return false
	}
	// MintOrFetch returns secret; we discard it immediately (security invariant).
	_, err := h.vault.MintOrFetch(r.Context(), user.Subject, user.TenantID)
	if err != nil {
		// EC-14: error never contains the secret (BFFVaultError.Error()).
		h.logger.Error("BFF mint failed", "error", err, "subject", user.Subject, "endpoint", spanSuffix)
		writeError(w, http.StatusInternalServerError, "VAULT_ERROR", "Internal error.")
		return false
	}
	// Audit trail per Q5 resolution: span name bff.memory.<verb>
	span := otel.Tracer(dashboardMemoryTracer).Start
	_, sp := span(r.Context(), "bff.memory."+spanSuffix)
	sp.SetAttributes(attribute.String("bff.minted", "true"))
	sp.End()
	return true
}

// List proxies /v1/dashboard/memory/list — browser-trust path. After mint,
// delegates to MemoryHandler.List which performs the upstream TheMemory call.
func (h *DashboardMemoryHandler) List(w http.ResponseWriter, r *http.Request) {
	if !h.mintOrFail(w, r, "list") {
		return
	}
	h.memoryHandler.List(w, r)
}

// Metrics proxies /v1/dashboard/memory/metrics.
func (h *DashboardMemoryHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	if !h.mintOrFail(w, r, "metrics") {
		return
	}
	h.memoryHandler.Metrics(w, r)
}

// Forget proxies /v1/dashboard/memory/forget/{memoryID}.
func (h *DashboardMemoryHandler) Forget(w http.ResponseWriter, r *http.Request) {
	if !h.mintOrFail(w, r, "forget") {
		return
	}
	h.memoryHandler.Forget(w, r)
}

// Entities proxies /v1/dashboard/memory/entities.
func (h *DashboardMemoryHandler) Entities(w http.ResponseWriter, r *http.Request) {
	if !h.mintOrFail(w, r, "entities") {
		return
	}
	h.memoryHandler.Entities(w, r)
}

// RevokeOnLogout proxies POST /v1/dashboard/memory/revoke-on-logout — clean
// shutdown path called by Dashboard logout flow. Idempotent.
func (h *DashboardMemoryHandler) RevokeOnLogout(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated.")
		return
	}
	if err := h.vault.Revoke(r.Context(), user.Subject, user.TenantID); err != nil {
		h.logger.Error("BFF revoke failed", "error", err, "subject", user.Subject)
		writeError(w, http.StatusInternalServerError, "VAULT_ERROR", "Internal error.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

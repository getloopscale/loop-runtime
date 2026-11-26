// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/clientip"
	"github.com/getloopscale/loop-runtime/api/internal/middleware/slidingwindow"
)

// T1.3 — Auth failure persistence in audit_log.
// Per MEET-2026-05-12 plan: failed Bearer/service-auth/admin-key auth attempts
// MUST persist to audit_log table so forensics can reconstruct attack timelines.
// Closes audit M10 G6.
//
// Per EC-NEW-12: emission MUST fail open. If audit_log INSERT fails, the auth
// rejection still happens, but slog records the audit failure + Prometheus
// counter increments. Audit emission failure must NEVER block sign or reject.
//
// Rate limit (per EC-NEW from plan v1.1): max 100 audit_log entries per IP
// per minute. Prevents storage bloat from bot scanning.
//
// Decoupled from db package to avoid import cycle (db.idempotency_store
// imports middleware). Caller injects AuthAuditWriter function.

// AuthFailureAction enum (compatible with existing audit_log.action varchar)
type AuthFailureAction string

const (
	AuthFailureTokenInvalid    AuthFailureAction = "auth.token_invalid" //nolint:gosec // G101 false positive: audit action NAME, not credential value
	AuthFailureTokenExpired    AuthFailureAction = "auth.token_expired" //nolint:gosec // G101 false positive: audit action NAME, not credential value
	AuthFailureServiceMismatch AuthFailureAction = "auth.service_auth_mismatch"
	AuthFailureAdminMismatch   AuthFailureAction = "auth.admin_key_mismatch"
	AuthFailureTenantNotFound  AuthFailureAction = "auth.tenant_not_found"
)

// AuthAuditWriter persists an audit-log entry. Caller injects this (typically
// wraps store.WriteAuditLog). Decoupled to avoid import cycle.
// Returns error if write fails — caller logs (per EC-NEW-12 fail-open).
type AuthAuditWriter func(ctx context.Context, tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) error

// AuthFailureAuditor persists auth-rejection events to audit_log + slog.
// Constructed once at API startup with the writer function.
type AuthFailureAuditor struct {
	Write    AuthAuditWriter
	Logger   *slog.Logger
	throttle *slidingwindow.Counter
	clientIP clientip.Resolver
}

// NewAuthFailureAuditor returns an auditor throttled to 100 rows/min per client
// IP. Per EC-NEW v1.1: prevents bot-scanning floods from filling audit_log.
// clientIP decides which address a request is throttled (and recorded) under;
// it must carry the same trusted-proxy policy as the edge IP rate limiter, or a
// client picks its own bucket by writing X-Forwarded-For.
func NewAuthFailureAuditor(writer AuthAuditWriter, logger *slog.Logger, clientIP clientip.Resolver) *AuthFailureAuditor {
	return &AuthFailureAuditor{
		Write:    writer,
		Logger:   logger,
		throttle: slidingwindow.New(100, time.Minute, nil),
		clientIP: clientIP,
	}
}

// Emit records an auth failure to audit_log (best-effort; never blocks caller).
// Per EC-NEW-12: failure to write audit_log emits slog.Warn + Prometheus
// counter; caller continues with rejection regardless.
//
// The tenantID is "" for pre-auth failures (no tenant context yet). Resource
// type is "session". Actor is empty when the failure is at the auth layer.
func (a *AuthFailureAuditor) Emit(
	ctx context.Context,
	r *http.Request,
	action AuthFailureAction,
	reason string,
	metadata map[string]any,
) {
	if a == nil || a.Write == nil {
		return
	}

	clientAddr := a.clientIP.IP(r)
	ip := r.RemoteAddr // unparseable peer: record it verbatim for forensics
	if clientAddr != nil {
		ip = clientAddr.String()
	}
	if allowed, _ := a.throttle.Allow(clientip.BucketKey(clientAddr)); !allowed {
		// Rate limited: log debug, skip DB write
		a.Logger.Debug("auth audit emission rate limited", "ip", ip, "action", string(action))
		return
	}

	// Build metadata (always includes IP + user_agent + route for forensics)
	enriched := map[string]any{
		"ip":             ip,
		"user_agent":     r.UserAgent(),
		"route":          r.URL.Path,
		"method":         r.Method,
		"reason":         reason,
		"correlation_id": r.Header.Get("Correlation-ID"),
	}
	for k, v := range metadata {
		enriched[k] = v
	}

	// Detached context: caller's request may be cancelling; auth audit should
	// still complete on a short independent timeout.
	auditCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := a.Write(
		auditCtx,
		"", // tenantID — pre-auth
		string(action),
		"", // actor — auth layer
		"session",
		r.Header.Get("X-Request-Id"),
		r.Header.Get("Correlation-ID"),
		enriched,
	)
	if err != nil {
		// EC-NEW-12: fail open. Log + (TODO) metric; do not block caller.
		a.Logger.Warn("auth audit_log emission failed",
			"action", string(action),
			"ip", ip,
			"error", err,
		)
		return
	}

	a.Logger.Debug("auth failure audit emitted",
		"action", string(action),
		"ip", ip,
		"reason", reason,
	)
}

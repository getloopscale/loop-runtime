// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
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
	AuthFailureTokenInvalid    AuthFailureAction = "auth.token_invalid"
	AuthFailureTokenExpired    AuthFailureAction = "auth.token_expired"
	AuthFailureServiceMismatch AuthFailureAction = "auth.service_auth_mismatch"
	AuthFailureAdminMismatch   AuthFailureAction = "auth.admin_key_mismatch"
	AuthFailureTenantNotFound  AuthFailureAction = "auth.tenant_not_found"
)

// AuthAuditWriter persists an audit-log entry. Caller injects this (typically
// wraps db.WriteAuditLog). Decoupled to avoid import cycle.
// Returns error if write fails — caller logs (per EC-NEW-12 fail-open).
type AuthAuditWriter func(ctx context.Context, tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) error

// AuthFailureAuditor persists auth-rejection events to audit_log + slog.
// Constructed once at API startup with the writer function.
type AuthFailureAuditor struct {
	Write       AuthAuditWriter
	Logger      *slog.Logger
	rateLimiter *ipTokenBucket
}

// DefaultAuthAuditor is the package-level singleton wired by the boot
// process (`api/cmd/setup_auth.go`). All failure paths in this package
// call `emitAuthFailure(...)` which nil-safe dispatches to this auditor.
//
// Pattern matches existing `ServiceAuthSecret` / `AdminAPIKey` package-
// level vars — assign-once at boot, read-only thereafter (no races in
// production). Tests leave it nil; the helper short-circuits.
//
// Wave 1 Phase A.4 of MEET-2026-05-12 wires this from setup_auth.go.
var DefaultAuthAuditor *AuthFailureAuditor

// emitAuthFailure is the nil-safe dispatch helper used by failure paths
// in this package. Reads `DefaultAuthAuditor` once; if nil (test path
// or pre-wire boot), is a no-op.
//
// The auditor itself is also nil-safe in Emit, but reading the var here
// makes the call sites uniform and grep-friendly: every reject path
// has one `emitAuthFailure(r, action, reason, ...)` line.
func emitAuthFailure(r *http.Request, action AuthFailureAction, reason string, metadata map[string]any) {
	auditor := DefaultAuthAuditor
	if auditor == nil {
		return
	}
	auditor.Emit(r.Context(), r, action, reason, metadata)
}

// NewAuthFailureAuditor returns an auditor with default 100/min/IP rate limit.
// Per EC-NEW v1.1: prevents bot-scanning floods from filling audit_log.
func NewAuthFailureAuditor(writer AuthAuditWriter, logger *slog.Logger) *AuthFailureAuditor {
	return &AuthFailureAuditor{
		Write:       writer,
		Logger:      logger,
		rateLimiter: newIPTokenBucket(100, time.Minute),
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

	ip := clientIP(r)
	if !a.rateLimiter.allow(ip) {
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

// clientIP extracts the best-effort client IP from request, honoring
// X-Forwarded-For when running behind a load balancer.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ipTokenBucket is a simple per-IP rate limiter: N tokens per window, refills
// at end of window. Sufficient for audit emission throttling — not for
// general request rate-limiting (use iprate.go for that).
type ipTokenBucket struct {
	mu        sync.Mutex
	buckets   map[string]*bucketState
	maxTokens int
	window    time.Duration
}

type bucketState struct {
	tokens      int
	windowStart time.Time
}

func newIPTokenBucket(maxTokens int, window time.Duration) *ipTokenBucket {
	return &ipTokenBucket{
		buckets:   make(map[string]*bucketState),
		maxTokens: maxTokens,
		window:    window,
	}
}

func (b *ipTokenBucket) allow(ip string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	state, exists := b.buckets[ip]
	if !exists || now.Sub(state.windowStart) >= b.window {
		// New window: reset
		b.buckets[ip] = &bucketState{
			tokens:      b.maxTokens - 1,
			windowStart: now,
		}
		return true
	}

	if state.tokens > 0 {
		state.tokens--
		return true
	}
	return false
}

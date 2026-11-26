// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secretshttp

// internal_secret_fetch_audit_adapter.go — T4.4 of plan
// 2026-06-02-openbao-adoption v1.3.
//
// SecretReadAuditAdapter implements SecretReadAuditWriter (T4.1 interface)
// over the AuditLogger port (ports.AuditLogger — store.AuditLogger in
// production, injected by cmd). It maps T4.1's SecretReadAuditEvent onto the
// canonical audit row:
//
//   secret.read.broker    → Action=ActionSecretReadBroker
//   cross_tenant_attempt  → Action=ActionCrossTenantAttempt
//
// ResourceType + ResourceID are set so audit_log queries can filter by
// (tenant_id, secret_name) cleanly. Metadata embeds cell + actor for
// forensic trace.
//
// Located in routes/ (not services/) because routes/ owns the
// SecretReadAuditWriter interface — placing the adapter beside the
// interface avoids the routes ↔ services import cycle.

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

// Canonical action strings — exported so tests don't drift.
const (
	ActionSecretReadBroker   = "secret.read.broker"
	ActionCrossTenantAttempt = "security.cross_tenant_attempt"
)

// SecretReadAuditAdapter writes broker endpoint events to the audit log in
// canonical shape, through the AuditLogger port.
type SecretReadAuditAdapter struct {
	Logger AuditLogger
}

// NewSecretReadAuditAdapter constructs the adapter.
func NewSecretReadAuditAdapter(logger AuditLogger) *SecretReadAuditAdapter {
	return &SecretReadAuditAdapter{Logger: logger}
}

// WriteAudit satisfies SecretReadAuditWriter.
//
// T2.3 of plan 2026-06-10-j22-layer2-code-review-fixes (closes HIGH #16
// silent audit error): on LogAudit failure we now (a) increment the
// canonical Prometheus counter `loop_runtime_secret_audit_write_failed_total`
// AND (b) log via slog.Error with structured fields. Callers may keep
// the legacy `_ = WriteAudit(...)` posture (audit is best-effort and
// MUST NOT fail the request); telemetry is observed via the metric +
// log here, NOT at every call site. Single source of truth for the
// audit failure signal.
func (a *SecretReadAuditAdapter) WriteAudit(ctx context.Context, e SecretReadAuditEvent) error {
	if a == nil || a.Logger == nil {
		err := fmt.Errorf("secret-read audit: logger not configured")
		metrics.SecretAuditWriteFailedTotal.WithLabelValues(metricEventLabel(e.EventType)).Inc()
		slog.Error("secret-read audit failed",
			"event_type", e.EventType,
			"tenant_id", e.TenantID,
			"secret_name", e.SecretName,
			"project", e.Project,
			"environment", e.Environment,
			"actor", e.Actor,
			"error", "logger_not_configured")
		return err
	}

	action := e.EventType
	switch e.EventType {
	case "secret.read.broker":
		action = ActionSecretReadBroker
	case "cross_tenant_attempt":
		action = ActionCrossTenantAttempt
	}

	metadata := map[string]any{
		"cell_id":    e.CellID,
		"event_type": e.EventType,
		"actor_kind": "eso-broker", // distinguishes from operator-side audit entries
	}
	// T2.3: thread project + environment into the audit metadata so the
	// forensic trace of cross_tenant_attempt records exactly which env
	// the offending request targeted (T2.1 URL contract carries them).
	if e.Project != "" {
		metadata["project"] = e.Project
	}
	if e.Environment != "" {
		metadata["environment"] = e.Environment
	}
	if e.Reason != "" {
		metadata["reason"] = e.Reason
	}
	// LogAudit signature: ctx, tenantID, action, actor, resourceType, resourceID, correlationID, metadata
	err := a.Logger.LogAudit(ctx, e.TenantID, action, e.Actor, "secret", e.SecretName, "", metadata)
	if err != nil {
		metrics.SecretAuditWriteFailedTotal.WithLabelValues(metricEventLabel(e.EventType)).Inc()
		slog.Error("secret-read audit failed",
			"event_type", e.EventType,
			"tenant_id", e.TenantID,
			"secret_name", e.SecretName,
			"project", e.Project,
			"environment", e.Environment,
			"actor", e.Actor,
			"error", err.Error())
	}
	return err
}

// metricEventLabel maps the human event_type to a Prometheus-safe label.
// Underscore-only — never a free-form caller value to avoid label
// cardinality explosion.
func metricEventLabel(eventType string) string {
	switch eventType {
	case "secret.read.broker":
		return "secret_read_broker"
	case "cross_tenant_attempt":
		return "cross_tenant_attempt"
	default:
		return "unknown"
	}
}

// Static interface check — fails compile if the adapter drifts from
// the SecretReadAuditWriter contract.
var _ SecretReadAuditWriter = (*SecretReadAuditAdapter)(nil)

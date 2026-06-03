// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

// internal_secret_fetch_audit_adapter.go — T4.4 of plan
// 2026-06-02-openbao-adoption v1.3.
//
// SecretReadAuditAdapter implements SecretReadAuditWriter (T4.1 interface)
// by wrapping the existing db.AuditLogger / WriteAuditLog infrastructure.
// Maps T4.1's SecretReadAuditEvent into the canonical db.AuditEntry shape:
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

	"github.com/loopzero/loopzero/api/internal/db"
)

// Canonical action strings — exported so tests don't drift.
const (
	ActionSecretReadBroker   = "secret.read.broker"
	ActionCrossTenantAttempt = "security.cross_tenant_attempt"
)

// SecretReadAuditAdapter wraps a db.AuditLogger so that broker endpoint
// events flow into audit_log with canonical shape.
type SecretReadAuditAdapter struct {
	Logger *db.AuditLogger
}

// NewSecretReadAuditAdapter constructs the adapter.
func NewSecretReadAuditAdapter(logger *db.AuditLogger) *SecretReadAuditAdapter {
	return &SecretReadAuditAdapter{Logger: logger}
}

// WriteAudit satisfies SecretReadAuditWriter.
func (a *SecretReadAuditAdapter) WriteAudit(ctx context.Context, e SecretReadAuditEvent) error {
	if a == nil || a.Logger == nil {
		return fmt.Errorf("secret-read audit: logger not configured")
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
	if e.Reason != "" {
		metadata["reason"] = e.Reason
	}
	// LogAudit signature: ctx, tenantID, action, actor, resourceType, resourceID, correlationID, metadata
	return a.Logger.LogAudit(ctx, e.TenantID, action, e.Actor, "secret", e.SecretName, "", metadata)
}

// _ = db.AuditEntry{} // type-keep no-op (LogAudit takes flat args; we still
// reference db.AuditLogger so the import remains canonical).
var _ = (*db.AuditLogger)(nil)

// Static interface check — fails compile if the adapter drifts from
// the SecretReadAuditWriter contract.
var _ SecretReadAuditWriter = (*SecretReadAuditAdapter)(nil)

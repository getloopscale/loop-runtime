// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package audit is the audit-log feature: the audit-log handlers live in
// audithttp and the nightly retention pruner is AuditLogPruner.
package audit

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// AuditStore abstracts audit log persistence.
type AuditStore interface {
	ListAuditEntries(ctx context.Context, tenantID string, filter ports.AuditFilter) ([]ports.AuditEntryResult, int, error)
}

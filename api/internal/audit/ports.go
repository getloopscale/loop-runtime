// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package audit

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/ports"
)

// AuditStore abstracts audit log persistence.
type AuditStore interface {
	ListAuditEntries(ctx context.Context, tenantID string, filter ports.AuditFilter) ([]ports.AuditEntryResult, int, error)
}

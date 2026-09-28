// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environmentsadapter

import (
	"context"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/environments/environmentshttp"
)

var _ environmentshttp.PreviewSlotLookup = (*PreviewSlotLookup)(nil)

type previewSlotsByTenant interface {
	ListByTenant(ctx context.Context, tenantID string) ([]store.PreviewSlot, error)
}

// PreviewSlotLookup lists a tenant's preview slots, one per project that has
// ever had a preview (migration 000028). Empty slice, never nil, when the
// tenant has none.
type PreviewSlotLookup struct {
	Store previewSlotsByTenant
}

// ListPreviewSlots returns every preview slot of the tenant.
func (a *PreviewSlotLookup) ListPreviewSlots(ctx context.Context, tenantID string) ([]environmentshttp.PreviewSlotInfo, error) {
	slots, err := a.Store.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]environmentshttp.PreviewSlotInfo, 0, len(slots))
	for _, slot := range slots {
		out = append(out, environmentshttp.PreviewSlotInfo{
			ProjectID:       slot.ProjectID,
			ActivePRNumber:  slot.ActivePRNumber,
			ActiveBranch:    slot.ActiveBranch,
			ActiveReleaseID: slot.ActiveReleaseID,
			URL:             slot.URL,
			UpdatedAt:       slot.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

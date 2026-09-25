// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placementadapter

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/placement"
	"github.com/getloopscale/loopzero/api/internal/placement/placementhttp"
)

// TenantEvictorRouteAdapter exposes *placement.TenantEvictor via the
// routes.TenantEvictor interface. The concrete type's pointer-receiver
// methods already satisfy the interface — this wrapper only exists to make
// that relationship explicit (and would allow future per-route concerns
// like logging/tracing to be layered in without touching services).
type TenantEvictorRouteAdapter struct {
	Inner *placement.TenantEvictor
}

func (a *TenantEvictorRouteAdapter) MatchTenants(ctx context.Context, pattern string, ids []string) ([]string, error) {
	return a.Inner.MatchTenants(ctx, pattern, ids)
}

// EnqueueEviction satisfies the route's port. The eviction is synchronous and
// no job table exists, so there is no job id to return; the route never read
// the one the service used to fabricate ("evict-N-tenants", shared by every
// eviction of N tenants).
func (a *TenantEvictorRouteAdapter) EnqueueEviction(ctx context.Context, tenantIDs []string) (string, error) {
	_, err := a.Inner.EvictTenants(ctx, tenantIDs)
	return "", err
}

// The port this adapter satisfies, asserted at compile time.
var _ placementhttp.TenantEvictor = (*TenantEvictorRouteAdapter)(nil)

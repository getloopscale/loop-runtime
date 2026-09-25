// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/services/placement"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/k8s/tenancy"
)

// TenantDBDeleterAdapter implements placement.TenantDBDeleter by translating
// store.TenantDeletionCounts (infrastructure shape) to placement.TenantDeletionCounts
// (domain shape). Fields are identical; separate types exist to prevent a
// cyclic import between services and db.
//
// T1.4 (plan 2026-05-29-sqlc-migration v1.3): Store field widened from
// concrete *db.PostgresTenantStore to store.AdminTenantStore interface so
// the sqlc-backed TenantStoreSqlc drops in without churn. Only
// DeleteTenantsFKOrdered is consumed at this seam — interface stays
// minimal.
type TenantDBDeleterAdapter struct {
	Store store.AdminTenantStore
}

func (a *TenantDBDeleterAdapter) DeleteTenantsFKOrdered(ctx context.Context, tenantIDs []string) (placement.TenantDeletionCounts, error) {
	c, err := a.Store.DeleteTenantsFKOrdered(ctx, tenantIDs)
	if err != nil {
		return placement.TenantDeletionCounts{}, err
	}
	return placement.TenantDeletionCounts{
		EnvironmentTargets: c.EnvironmentTargets,
		Releases:           c.Releases,
		BuildApps:          c.BuildApps,
		Builds:             c.Builds,
		Projects:           c.Projects,
		Subscriptions:      c.Subscriptions,
		TenantMembers:      c.TenantMembers,
		Tenants:            c.Tenants,
		MeteringCursors:    c.MeteringCursors,
	}, nil
}

// TenantK8sEvictorAdapter implements placement.TenantK8sEvictor by wrapping
// tenancy.TenantK8sEvictor and translating K8sEvictionCounts across the package
// boundary. Mirrors the TenantDBDeleterAdapter pattern.
type TenantK8sEvictorAdapter struct {
	Evictor *tenancy.TenantK8sEvictor
}

func (a *TenantK8sEvictorAdapter) EvictTenantsFromClusters(ctx context.Context, tenantIDs []string) (placement.K8sEvictionCounts, error) {
	c, err := a.Evictor.EvictTenantsFromClusters(ctx, tenantIDs)
	if err != nil {
		return placement.K8sEvictionCounts{}, err
	}
	return placement.K8sEvictionCounts{
		ArgoWorkflowsDeleted: c.ArgoWorkflowsDeleted,
		MgmtCRsDeleted:       c.MgmtCRsDeleted,
		FinalizersCleared:    c.FinalizersCleared,
		NamespacesFinalized:  c.NamespacesFinalized,
	}, nil
}

// The port this adapter satisfies, asserted at compile time.
//
// B-058: the package is called `adapters` and its admission criterion was "it is an adapter" —
// which refuses nothing. Measured on 2026-08-18: of the nine adapters, five NAMED their port in
// prose and only ONE verified it. Prose does not compile; this line does, and it is what lets
// the criterion say no.
var _ placement.TenantDBDeleter = (*TenantDBDeleterAdapter)(nil)

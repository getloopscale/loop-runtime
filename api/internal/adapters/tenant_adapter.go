// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/k8s"
	"github.com/loopzero/loopzero/api/internal/services"
)

// TenantDBDeleterAdapter implements services.TenantDBDeleter by translating
// db.TenantDeletionCounts (infrastructure shape) to services.TenantDeletionCounts
// (domain shape). Fields are identical; separate types exist to prevent a
// cyclic import between services and db.
type TenantDBDeleterAdapter struct {
	Store *db.PostgresTenantStore
}

func (a *TenantDBDeleterAdapter) DeleteTenantsFKOrdered(ctx context.Context, tenantIDs []string) (services.TenantDeletionCounts, error) {
	c, err := a.Store.DeleteTenantsFKOrdered(ctx, tenantIDs)
	if err != nil {
		return services.TenantDeletionCounts{}, err
	}
	return services.TenantDeletionCounts{
		EnvironmentTargets: c.EnvironmentTargets,
		Releases:           c.Releases,
		BuildApps:          c.BuildApps,
		Builds:             c.Builds,
		Projects:           c.Projects,
		Subscriptions:      c.Subscriptions,
		TenantMembers:      c.TenantMembers,
		Tenants:            c.Tenants,
	}, nil
}

// TenantK8sEvictorAdapter implements services.TenantK8sEvictor by wrapping
// k8s.TenantK8sEvictor and translating K8sEvictionCounts across the package
// boundary. Mirrors the TenantDBDeleterAdapter pattern.
type TenantK8sEvictorAdapter struct {
	Evictor *k8s.TenantK8sEvictor
}

func (a *TenantK8sEvictorAdapter) EvictTenantsFromClusters(ctx context.Context, tenantIDs []string) (services.K8sEvictionCounts, error) {
	c, err := a.Evictor.EvictTenantsFromClusters(ctx, tenantIDs)
	if err != nil {
		return services.K8sEvictionCounts{}, err
	}
	return services.K8sEvictionCounts{
		ArgoWorkflowsDeleted: c.ArgoWorkflowsDeleted,
		MgmtCRsDeleted:       c.MgmtCRsDeleted,
		FinalizersCleared:    c.FinalizersCleared,
		NamespacesFinalized:  c.NamespacesFinalized,
	}, nil
}

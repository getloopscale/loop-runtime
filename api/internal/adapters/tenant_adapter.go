// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/services/placement"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/k8s"
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
	}, nil
}

// TenantK8sEvictorAdapter implements placement.TenantK8sEvictor by wrapping
// k8s.TenantK8sEvictor and translating K8sEvictionCounts across the package
// boundary. Mirrors the TenantDBDeleterAdapter pattern.
type TenantK8sEvictorAdapter struct {
	Evictor *k8s.TenantK8sEvictor
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

// A porta que este adaptador satisfaz, afirmada em tempo de compilacao.
//
// B-058: o pacote se chama `adapters` e o criterio de admissao dele era "e um adapter" —
// que nao recusa nada. Medido em 2026-08-18: dos nove adaptadores, cinco NOMEAVAM a porta em
// prosa e apenas UM a verificava. Prosa nao compila; esta linha sim, e e ela que faz o
// criterio conseguir dizer nao.
var _ placement.TenantDBDeleter = (*TenantDBDeleterAdapter)(nil)

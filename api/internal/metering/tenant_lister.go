// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

// tenant_lister.go — the metering enumeration source (plan metering-compute-loop
// T1.3 / T3.1). The worker meters every ACTIVE tenant each tick; this is where
// "active" is defined: the observed Tenant CR status.phase == Active. Kept behind
// a narrow TenantLister interface (DIP) so the worker is unit-testable without a
// live management cluster.

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/getloopscale/loop-runtime/api/internal/k8s"
	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// tenantCRNamespace is the canonical namespace holding every Tenant CR. Mirrors
// k8s.tenantNamespace (ADR-0018 v2 / C1): a hard-coded const, NOT env-overridable
// — the metering enumeration MUST NOT be re-pointable at another namespace, which
// was the misplacement vector the ADR closed.
const tenantCRNamespace = k8s.ControlPlaneNamespace

// TenantLister enumerates the tenant IDs the metering loop must meter this tick.
// The DIP boundary (the worker depends on this narrow interface, not on a concrete
// controller-runtime client) so a fake can drive the loop's unit tests.
type TenantLister interface {
	ListActiveTenants(ctx context.Context) ([]string, error)
}

// CRTenantLister implements TenantLister over the management-cluster
// controller-runtime client.
type CRTenantLister struct {
	Client client.Client
}

// ListActiveTenants returns the IDs of Tenant CRs whose observed status.phase is
// Active, scoped to the canonical loopzero-system namespace (C2) and filtered on the
// TenantPhaseActive CONSTANT (T1.3 — never a hardcoded string). Provisioning /
// Suspended / PendingDeletion / Deleting are skipped: a non-Active tenant reserves
// no billable compute the loop should meter (ADR-5), and metering a draining
// tenant is the intentional C3 under-count in the plan's Drawbacks. The cell_id is
// resolved separately from canonical PG (MeteringStore.GetCellForTenant, INV I2) —
// NEVER the CR spec.cellID mirror.
func (l *CRTenantLister) ListActiveTenants(ctx context.Context) ([]string, error) {
	var list apiv1.TenantList
	if err := l.Client.List(ctx, &list, client.InNamespace(tenantCRNamespace)); err != nil {
		return nil, fmt.Errorf("metering: list tenant CRs for enumeration: %w", err)
	}
	active := make([]string, 0, len(list.Items))
	for i := range list.Items {
		if list.Items[i].Status.Phase == apiv1.TenantPhaseActive {
			active = append(active, list.Items[i].Name)
		}
	}
	return active, nil
}

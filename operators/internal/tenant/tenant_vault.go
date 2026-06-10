// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

// tenant_vault.go — T3.3 of openbao-adoption v1.3.
//
// reconcileTransitKey is the optional reconcile step that ensures a
// per-tenant Vault Transit key exists before the Tenant CR transitions
// to ACTIVE. When TenantReconciler.VaultClient == nil, this step is a
// no-op (vault disabled mode — preserves backward compatibility).
//
// EC-15 amendment: quota errors set Condition TransitKeyAvailable=False
// with Reason=VaultQuotaExceeded so PrometheusRule can alarm operators.

import (
	"context"
	"errors"
	"fmt"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/operators/internal/vault"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// reconcileTransitKey ensures transit/keys/tenant-<id> exists in Vault.
// No-op when VaultClient is nil (vault disabled). On success, sets
// Condition TransitKeyAvailable=True (Reason=TransitKeyProvisioned).
// On quota error, sets False (Reason=VaultQuotaExceeded). On other
// errors, returns the error so runSteps records it generically.
func (r *TenantReconciler) reconcileTransitKey(ctx context.Context, tenant *loopzerov1.Tenant) error {
	if r.VaultClient == nil {
		// Vault disabled — skip step. Do NOT set condition (avoids
		// permanent "Unknown" state on tenants that may later opt-in).
		return nil
	}

	tenantID := tenant.Name
	if err := r.VaultClient.EnsureTenantKey(ctx, tenantID); err != nil {
		// EC-15: distinguish quota errors for alarm wiring
		if errors.Is(err, vault.ErrVaultQuotaExceeded) {
			conditions.SetCondition(
				&tenant.Status.Conditions,
				conditions.TypeTransitKeyAvailable,
				metav1.ConditionFalse,
				conditions.ReasonVaultQuotaExceeded,
				fmt.Sprintf("Vault transit key create rate-limited for tenant %s: %v", tenantID, err),
				tenant.Generation,
			)
			// Return error so reconcile retries with exp-backoff
			return err
		}
		// Other errors (network/unreachable/5xx)
		conditions.SetCondition(
			&tenant.Status.Conditions,
			conditions.TypeTransitKeyAvailable,
			metav1.ConditionFalse,
			conditions.ReasonVaultUnreachable,
			fmt.Sprintf("Vault unreachable for tenant %s: %v", tenantID, err),
			tenant.Generation,
		)
		return err
	}

	conditions.SetCondition(
		&tenant.Status.Conditions,
		conditions.TypeTransitKeyAvailable,
		metav1.ConditionTrue,
		conditions.ReasonTransitKeyProvisioned,
		fmt.Sprintf("Per-tenant Vault Transit key %s ensured", vault.TenantKeyPath(tenantID)),
		tenant.Generation,
	)
	return nil
}

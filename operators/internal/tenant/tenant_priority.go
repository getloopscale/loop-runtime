// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"fmt"

	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/planentitlements"
)

// reconcilePriorityClass ensures the PriorityClass for the tenant's plan exists.
// PriorityClasses are cluster-scoped and shared across tenants of the same plan,
// so they must NOT be deleted when a tenant is deleted.
//
// ARCH-010: PriorityClasses are created on the runtime cluster of the tenant's cell
// only, not the build cluster.
// Build cluster priority is intentionally flat — all builds are equal regardless of plan tier.
// Rationale: build pods are short-lived and managed by Argo Workflows with its own queue
// and parallelism controls. Plan-based priority would create starvation for starter tenants
// during peak build times, which is worse than FIFO ordering.
func (r *TenantReconciler) reconcilePriorityClass(ctx context.Context, tenant *loopzerov1.Tenant) error {
	entitled := planentitlements.For(tenant.Spec.Plan)
	pcName, pcValue, preemptionPolicy := entitled.PriorityClassName, entitled.PriorityValue, entitled.Preemption

	runtimeClient, err := r.runtimeClient(ctx, tenant)
	if err != nil {
		return err
	}

	pc := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: pcName,
		},
	}
	result, err := controllerutil.CreateOrUpdate(ctx, runtimeClient, pc, func() error {
		pc.Value = pcValue
		pc.PreemptionPolicy = &preemptionPolicy
		pc.GlobalDefault = false
		pc.Description = fmt.Sprintf("PriorityClass for LoopZero %s plan tenants", string(tenant.Spec.Plan))
		if pc.Labels == nil {
			pc.Labels = make(map[string]string)
		}
		pc.Labels[constants.LabelManagedBy] = constants.ManagedByTenantController
		return nil
	})
	if err != nil {
		return fmt.Errorf("priorityclass %s: %w", pcName, err)
	}

	log.FromContext(ctx).Info("priorityclass reconciled", "name", pcName, "result", result)
	conditions.SetCondition(&tenant.Status.Conditions, conditions.TypePriorityClassReady,
		metav1.ConditionTrue, "PriorityClassReady", "priority class provisioned", tenant.Generation)
	return nil
}

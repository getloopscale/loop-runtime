package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
)

// priorityClassForPlan returns the PriorityClass name, value, and preemption policy for a given plan.
func priorityClassForPlan(plan loopzerov1.PlanTier) (name string, value int32, preemptionPolicy corev1.PreemptionPolicy) {
	switch plan {
	case loopzerov1.PlanGrowth:
		return "loopzero-tenant-growth", 500, corev1.PreemptLowerPriority
	case loopzerov1.PlanEnterprise:
		return "loopzero-tenant-enterprise", 1000, corev1.PreemptLowerPriority
	default:
		return "loopzero-tenant-starter", 100, corev1.PreemptNever
	}
}

// reconcilePriorityClass ensures the PriorityClass for the tenant's plan exists.
// PriorityClasses are cluster-scoped and shared across tenants of the same plan,
// so they must NOT be deleted when a tenant is deleted.
func (r *TenantReconciler) reconcilePriorityClass(ctx context.Context, tenant *loopzerov1.Tenant) error {
	pcName, pcValue, preemptionPolicy := priorityClassForPlan(tenant.Spec.Plan)

	pc := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: pcName,
		},
	}
	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, pc, func() error {
		pc.Value = pcValue
		pc.PreemptionPolicy = &preemptionPolicy
		pc.GlobalDefault = false
		pc.Description = fmt.Sprintf("PriorityClass for LoopZero %s plan tenants", string(tenant.Spec.Plan))
		if pc.Labels == nil {
			pc.Labels = make(map[string]string)
		}
		pc.Labels["loopzero.io/managed-by"] = "tenant-controller"
		return nil
	})
	if err != nil {
		return fmt.Errorf("priorityclass %s: %w", pcName, err)
	}

	log.FromContext(ctx).Info("priorityclass reconciled", "name", pcName, "result", result)
	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionPriorityClassReady,
		metav1.ConditionTrue, "PriorityClassReady", "priority class provisioned", tenant.Generation)
	return nil
}

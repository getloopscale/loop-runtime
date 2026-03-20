package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/naming"
)

// reconcileBuildNamespace ensures the build-{tenantID} namespace exists with ResourceQuota.
func (r *TenantReconciler) reconcileBuildNamespace(ctx context.Context, tenant *loopzerov1.Tenant) error {
	buildNs, err := naming.BuildNamespaceName(tenant.Name)
	if err != nil {
		return fmt.Errorf("invalid build namespace name for tenant %s: %w", tenant.Name, err)
	}

	// Create or update build namespace (NO ownerReference — cross-namespace limitation)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: buildNs,
		},
	}
	var createErr error
	_, createErr = controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels["loopzero.io/tenant"] = tenant.Name
		ns.Labels["loopzero.io/component"] = "build"
		ns.Labels["loopzero.io/managed-by"] = "tenant-controller"
		return nil
	})
	if createErr != nil {
		return fmt.Errorf("build namespace %s: %w", buildNs, createErr)
	}

	// ResourceQuota for build namespace
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "build-quota",
			Namespace: buildNs,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, quota, func() error {
		quota.Spec.Hard = corev1.ResourceList{
			corev1.ResourceRequestsCPU:    resource.MustParse("4"),
			corev1.ResourceRequestsMemory: resource.MustParse("8Gi"),
			corev1.ResourceLimitsCPU:      resource.MustParse("8"),
			corev1.ResourceLimitsMemory:   resource.MustParse("16Gi"),
		}
		// Enforce maxBuildConcurrency via workflow count quota
		if tenant.Spec.Limits.MaxBuildConcurrency > 0 {
			quota.Spec.Hard[corev1.ResourceName("count/workflows.argoproj.io")] =
				resource.MustParse(fmt.Sprintf("%d", tenant.Spec.Limits.MaxBuildConcurrency))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("build resourcequota %s/%s: %w", buildNs, "build-quota", err)
	}

	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionBuildNamespaceReady,
		metav1.ConditionTrue, "BuildNamespaceReady", "build namespace provisioned", tenant.Generation)

	// QuotaEnforced = True when ALL quotas (workload + build) are applied
	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionQuotaEnforced,
		metav1.ConditionTrue, "AllQuotasEnforced", "all resource quotas applied", tenant.Generation)

	return nil
}

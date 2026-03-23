package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/pkg/naming"
)

// reconcileNamespaces ensures each environment has a namespace with ResourceQuota and LimitRange.
func (r *TenantReconciler) reconcileNamespaces(ctx context.Context, tenant *loopzerov1.Tenant) error {
	for _, env := range tenant.Spec.Environments {
		nsName, err := naming.EnvironmentNamespace(tenant.Name, env.Name)
		if err != nil {
			return fmt.Errorf("invalid namespace name for env %s: %w", env.Name, err)
		}

		// Create or update namespace (NO ownerReference — cross-namespace limitation)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
			},
		}
		result, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, ns, func() error {
			if ns.Labels == nil {
				ns.Labels = make(map[string]string)
			}
			ns.Labels["loopzero.io/tenant"] = tenant.Name
			ns.Labels["loopzero.io/environment"] = env.Name
			ns.Labels["loopzero.io/managed-by"] = "tenant-controller"
			return nil
		})
		if err != nil {
			return fmt.Errorf("namespace %s: %w", nsName, err)
		}
		log.FromContext(ctx).Info("namespace reconciled", "namespace", nsName, "result", result)

		// Create or update ResourceQuota
		if err := r.reconcileResourceQuota(ctx, nsName, tenant); err != nil {
			return err
		}

		// Create or update LimitRange
		if err := r.reconcileLimitRange(ctx, nsName); err != nil {
			return err
		}
	}

	conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeNamespacesReady,
		metav1.ConditionTrue, "AllNamespacesReady", "all environment namespaces provisioned", tenant.Generation)
	return nil
}

// reconcileResourceQuota ensures a ResourceQuota exists in the given namespace.
func (r *TenantReconciler) reconcileResourceQuota(ctx context.Context, nsName string, tenant *loopzerov1.Tenant) error {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tenant-quota",
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, quota, func() error {
		quota.Spec.Hard = corev1.ResourceList{
			corev1.ResourceRequestsCPU:    tenant.Spec.Limits.MaxCPU,
			corev1.ResourceRequestsMemory: tenant.Spec.Limits.MaxMemory,
			corev1.ResourcePersistentVolumeClaims: resource.MustParse("10"),
			"services.loadbalancers":              resource.MustParse("0"),
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("resourcequota %s/%s: %w", nsName, "tenant-quota", err)
	}
	return nil
}

// reconcileLimitRange ensures a LimitRange exists in the given namespace.
func (r *TenantReconciler) reconcileLimitRange(ctx context.Context, nsName string) error {
	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tenant-limits",
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, lr, func() error {
		lr.Spec.Limits = []corev1.LimitRangeItem{
			{
				Type: corev1.LimitTypeContainer,
				Default: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
				DefaultRequest: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("limitrange %s/%s: %w", nsName, "tenant-limits", err)
	}
	return nil
}

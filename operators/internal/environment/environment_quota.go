// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environment

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/reconcileloop"
	"github.com/getloopscale/loopzero/operators/internal/tenantpolicy"
)

// reconcileNamespace ensures the namespace exists via CreateOrUpdate.
// fix-all-platform-issues-plan T1.1 — writes through the cell client Reconcile resolved.
func (r *EnvironmentReconciler) reconcileNamespace(ctx context.Context, runtimeClient client.Client, env *loopzerov1.Environment, nsName, tenantName string) error {
	ctx, span := reconcileloop.StartChildSpan(ctx, "environment.namespace.ensure")
	defer span.End()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	result, err := controllerutil.CreateOrUpdate(ctx, runtimeClient, ns, func() error {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels[constants.LabelTenant] = tenantName
		ns.Labels[constants.LabelEnvironment] = env.Spec.Environment
		ns.Labels[constants.LabelManagedBy] = constants.ManagedByEnvironmentController
		// Task 7.2: PodSecurity Admission — enforce restricted profile on all tenant namespaces.
		ns.Labels["pod-security.kubernetes.io/enforce"] = "restricted"
		ns.Labels["pod-security.kubernetes.io/warn"] = "restricted"
		ns.Labels["pod-security.kubernetes.io/audit"] = "restricted"
		return nil
	})
	if err != nil {
		return fmt.Errorf("namespace %s: %w", nsName, err)
	}
	log.FromContext(ctx).Info("namespace reconciled", "namespace", nsName, "result", result)
	return nil
}

// reconcileQuota ensures ResourceQuota and LimitRange exist if configured.
// fix-all-platform-issues-plan T1.1 — writes through the cell client Reconcile resolved.
func (r *EnvironmentReconciler) reconcileQuota(ctx context.Context, runtimeClient client.Client, env *loopzerov1.Environment, nsName string) error {
	// ResourceQuota (only if spec.resourceQuota is set)
	if env.Spec.ResourceQuota != nil {
		cpuQty, err := resource.ParseQuantity(env.Spec.ResourceQuota.CPU)
		if err != nil {
			return fmt.Errorf("invalid CPU quota %q: %w", env.Spec.ResourceQuota.CPU, err)
		}
		memQty, err := resource.ParseQuantity(env.Spec.ResourceQuota.Memory)
		if err != nil {
			return fmt.Errorf("invalid memory quota %q: %w", env.Spec.ResourceQuota.Memory, err)
		}

		quota := &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.QuotaTenant,
				Namespace: nsName,
			},
		}
		_, err = controllerutil.CreateOrUpdate(ctx, runtimeClient, quota, func() error {
			quota.Spec.Hard = corev1.ResourceList{
				corev1.ResourceRequestsCPU:            cpuQty,
				corev1.ResourceRequestsMemory:         memQty,
				corev1.ResourcePersistentVolumeClaims: resource.MustParse(constants.QuotaPVCLimit),
				"services.loadbalancers":              resource.MustParse(constants.QuotaLoadBalancerLimit),
			}
			if env.Spec.ResourceQuota.Storage != "" {
				storageQty, parseErr := resource.ParseQuantity(env.Spec.ResourceQuota.Storage)
				if parseErr != nil {
					return fmt.Errorf("invalid storage quota %q: %w", env.Spec.ResourceQuota.Storage, parseErr)
				}
				quota.Spec.Hard[corev1.ResourceRequestsStorage] = storageQty
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("resourcequota %s/%s: %w", nsName, constants.QuotaTenant, err)
		}
	}

	// LimitRange (always created)
	return tenantpolicy.ApplyTenantLimitRange(ctx, runtimeClient, nsName, "limitrange")
}

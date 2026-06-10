// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package preview

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/operators/internal/shared"
)

// Sprint 5 / T1.2 — preview namespace hardening.
//
// Each preview namespace gets the same isolation guarantees as a normal
// environment namespace:
//   - DenyAll NetworkPolicy (default-deny ingress/egress)
//   - AllowIngress NetworkPolicy (ingress controller only)
//   - AllowEgress NetworkPolicy (DNS + intra-ns + API server)
//   - ResourceQuota (caps CPU/memory/storage to bounded values)
//   - LimitRange (per-container default request/limit)
//
// These limits are intentionally tight — previews are short-lived and
// should never out-noise neighboring environments on the workload cluster.
// (ADR D1 — namespace isolation in workload cluster, not dedicated cluster.)

// previewQuotaCPU is the CPU cap for a preview namespace.
const (
	previewQuotaCPU     = "2"
	previewQuotaMemory  = "4Gi"
	previewQuotaStorage = "5Gi"
)

// reconcilePreviewNetworkPolicies ensures the deny-all + allow-ingress +
// allow-egress NP triplet exists in the preview namespace.
//
// Identical defense-in-depth posture as EnvironmentReconciler — uses the
// same constants so platform-wide audits don't have to special-case
// previews. Phase 3 v37: delegates to shared.ApplyTenantNetworkPolicies
// to eliminate the 3-function cross-package duplication with environment_network.go.
//
// drift-baseline.md port #3: r.ingressNS() resolves the Traefik namespace
// (configurable via INGRESS_NAMESPACE env, defaults to constants.NamespaceIngress).
func (r *PreviewReconciler) reconcilePreviewNetworkPolicies(ctx context.Context, nsName string) error {
	return shared.ApplyTenantNetworkPolicies(ctx, r.RuntimeClient, nsName, r.ingressNS(), "preview networkpolicy")
}

// reconcilePreviewQuota ensures a ResourceQuota and LimitRange exist in
// the preview namespace. Quotas are tight on purpose — previews compete
// with normal workloads on the same cluster.
func (r *PreviewReconciler) reconcilePreviewQuota(ctx context.Context, nsName string) error {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.QuotaTenant,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, quota, func() error {
		quota.Spec.Hard = corev1.ResourceList{
			corev1.ResourceRequestsCPU:            resource.MustParse(previewQuotaCPU),
			corev1.ResourceRequestsMemory:         resource.MustParse(previewQuotaMemory),
			corev1.ResourceRequestsStorage:        resource.MustParse(previewQuotaStorage),
			corev1.ResourcePersistentVolumeClaims: resource.MustParse(constants.QuotaPVCLimit),
			"services.loadbalancers":              resource.MustParse(constants.QuotaLoadBalancerLimit),
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("preview resourcequota %s/%s: %w", nsName, constants.QuotaTenant, err)
	}

	return shared.ApplyTenantLimitRange(ctx, r.RuntimeClient, nsName, "preview limitrange")
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package preview

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/tenantpolicy"
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
// previews. Phase 3 v37: delegates to tenantpolicy.ApplyTenantNetworkPolicies
// to eliminate the 3-function cross-package duplication with environment_network.go.
//
// drift-baseline.md port #3: r.ingressNS() resolves the Traefik namespace
// (configurable via INGRESS_NAMESPACE env, defaults to constants.NamespaceIngress).
func (r *PreviewReconciler) reconcilePreviewNetworkPolicies(ctx context.Context, rtClient client.Client, nsName string) error {
	return tenantpolicy.ApplyTenantNetworkPolicies(ctx, rtClient, nsName, r.ingressNS(), "preview networkpolicy")
}

// reconcilePreviewServiceAccount ensures the `loop-runtime-app` ServiceAccount
// exists inside the preview namespace.
//
// J-23 LIVE catch 2026-06-07: pods rendered by the API set
// `pod.spec.serviceAccountName: loop-runtime-app` (renderer_deployment.go:146 +
// renderer_rollout.go:400). The environment_controller creates this SA
// for production/staging namespaces (environment_rbac.go:129-157) but the
// preview controller historically didn't — every first preview deploy
// got a "serviceaccount loop-runtime-app not found" CrashLoop. The temporary
// LIVE workaround was `kubectl create sa loop-runtime-app -n <preview-ns>`
// (codified here so cleanroom self-heals).
//
// Shape mirrors environment_rbac.go's appSA: AutomountServiceAccountToken=
// false (defense-in-depth — preview pods don't need API access by default),
// labeled with constants.LabelManagedBy. We do NOT copy the pull-secret-
// from-source dance because (a) previews target the workload cluster
// where image pulls go through the cluster-default config, and (b) local
// k3d previews use the k3d-loop-runtime-registry (HTTP, no pull secret needed).
// If a future env requires private-registry previews, mirror the
// environment_rbac.go ensurePullSecret path here as a separate concern.
func (r *PreviewReconciler) reconcilePreviewServiceAccount(ctx context.Context, rtClient client.Client, nsName string) error {
	appSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.SAApp,
			Namespace: nsName,
		},
	}
	falseVal := false
	_, err := controllerutil.CreateOrUpdate(ctx, rtClient, appSA, func() error {
		if appSA.Labels == nil {
			appSA.Labels = make(map[string]string)
		}
		appSA.Labels[constants.LabelManagedBy] = constants.ManagedByPreviewController
		appSA.AutomountServiceAccountToken = &falseVal
		return nil
	})
	if err != nil {
		return fmt.Errorf("preview serviceaccount %s/loop-runtime-app: %w", nsName, err)
	}
	return nil
}

// reconcilePreviewQuota ensures a ResourceQuota and LimitRange exist in
// the preview namespace. Quotas are tight on purpose — previews compete
// with normal workloads on the same cluster.
func (r *PreviewReconciler) reconcilePreviewQuota(ctx context.Context, rtClient client.Client, nsName string) error {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.QuotaTenant,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, rtClient, quota, func() error {
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

	return tenantpolicy.ApplyTenantLimitRange(ctx, rtClient, nsName, "preview limitrange")
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/loopzero/loopzero/operators/internal/constants"
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
// previews.
func (r *PreviewReconciler) reconcilePreviewNetworkPolicies(ctx context.Context, nsName string) error {
	if err := r.reconcilePreviewDenyAllPolicy(ctx, nsName); err != nil {
		return err
	}
	if err := r.reconcilePreviewAllowIngressPolicy(ctx, nsName); err != nil {
		return err
	}
	return r.reconcilePreviewAllowEgressPolicy(ctx, nsName)
}

func (r *PreviewReconciler) reconcilePreviewDenyAllPolicy(ctx context.Context, nsName string) error {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.NetworkPolicyDenyAll,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, np, func() error {
		np.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("preview networkpolicy %s/%s: %w", nsName, constants.NetworkPolicyDenyAll, err)
	}
	return nil
}

func (r *PreviewReconciler) reconcilePreviewAllowIngressPolicy(ctx context.Context, nsName string) error {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.NetworkPolicyAllowIngress,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, np, func() error {
		np.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									// CHG-2026-04-29-DOGFOOD-FB: must be the
									// ingress (Traefik) namespace, not the
									// platform system namespace. Resolved via
									// helper to honor INGRESS_NAMESPACE override.
									constants.KubeMetadataNameLabel: r.ingressNS(),
								},
							},
						},
					},
				},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("preview networkpolicy %s/%s: %w", nsName, constants.NetworkPolicyAllowIngress, err)
	}
	return nil
}

func (r *PreviewReconciler) reconcilePreviewAllowEgressPolicy(ctx context.Context, nsName string) error {
	dnsPort := intstr.FromInt32(constants.PortDNS)
	apiPort := intstr.FromInt32(constants.PortHTTPS)
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.NetworkPolicyAllowEgress,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, np, func() error {
		np.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// DNS: UDP 53 to kube-system
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									constants.KubeMetadataNameLabel: constants.NamespaceKubeSystem,
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &udp, Port: &dnsPort},
					},
				},
				// Intra-namespace
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{},
						},
					},
				},
				// K8s API server (default ns): TCP 443
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									constants.KubeMetadataNameLabel: constants.NamespaceDefault,
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &apiPort},
					},
				},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("preview networkpolicy %s/%s: %w", nsName, constants.NetworkPolicyAllowEgress, err)
	}
	return nil
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

	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.LimitRangeTenant,
			Namespace: nsName,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, lr, func() error {
		lr.Spec.Limits = []corev1.LimitRangeItem{
			{
				Type: corev1.LimitTypeContainer,
				Default: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(constants.TenantLimitDefaultCPU),
					corev1.ResourceMemory: resource.MustParse(constants.TenantLimitDefaultMemory),
				},
				DefaultRequest: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(constants.TenantLimitDefaultRequestCPU),
					corev1.ResourceMemory: resource.MustParse(constants.TenantLimitDefaultRequestMemory),
				},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("preview limitrange %s/%s: %w", nsName, constants.LimitRangeTenant, err)
	}

	return nil
}

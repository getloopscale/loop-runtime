// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenantpolicy

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/getloopscale/loop-runtime/operators/internal/constants"
)

// ApplyTenantNetworkPolicies creates or updates the canonical defense-in-depth
// NetworkPolicy triplet (deny-all + allow-ingress + allow-egress) in the given
// tenant or preview namespace. Phase 3 cross-package helper: previously
// inlined 3 separate reconcile*Policy functions duplicated in 2 sites:
//   - internal/tenant/environment_network.go    (runtime tenant namespace)
//   - internal/preview/preview_resources.go     (preview namespace)
//
// ingressNamespace is the namespace where the Traefik ingress controller runs
// (resolved by each caller via its own ingressNS() method — typically
// constants.NamespaceIngress or overridden by IngressNamespace).
//
// errCtx is the human-readable error context (e.g., "networkpolicy" or
// "preview networkpolicy") so each call site preserves its existing
// error-wrap message format used by audit/log scans.
func ApplyTenantNetworkPolicies(ctx context.Context, c client.Client, nsName, ingressNamespace, errCtx string) error {
	if err := applyDenyAllNetworkPolicy(ctx, c, nsName, errCtx); err != nil {
		return err
	}
	if err := applyAllowIngressNetworkPolicy(ctx, c, nsName, ingressNamespace, errCtx); err != nil {
		return err
	}
	return applyAllowEgressNetworkPolicy(ctx, c, nsName, errCtx)
}

func applyDenyAllNetworkPolicy(ctx context.Context, c client.Client, nsName, errCtx string) error {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.NetworkPolicyDenyAll,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, np, func() error {
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
		return fmt.Errorf("%s %s/%s: %w", errCtx, nsName, constants.NetworkPolicyDenyAll, err)
	}
	return nil
}

func applyAllowIngressNetworkPolicy(ctx context.Context, c client.Client, nsName, ingressNamespace, errCtx string) error {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.NetworkPolicyAllowIngress,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, np, func() error {
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
									constants.KubeMetadataNameLabel: ingressNamespace,
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
		return fmt.Errorf("%s %s/%s: %w", errCtx, nsName, constants.NetworkPolicyAllowIngress, err)
	}
	return nil
}

func applyAllowEgressNetworkPolicy(ctx context.Context, c client.Client, nsName, errCtx string) error {
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
	_, err := controllerutil.CreateOrUpdate(ctx, c, np, func() error {
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
		return fmt.Errorf("%s %s/%s: %w", errCtx, nsName, constants.NetworkPolicyAllowEgress, err)
	}
	return nil
}

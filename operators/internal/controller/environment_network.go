// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/constants"
)

// reconcileNetworkPolicies ensures deny-all + allow-ingress + allow-egress policies exist.
// cell-2-activation-plan iter 4 BUG-FIX: cell-aware via env.Spec.TenantRef → cellID → factory.
func (r *EnvironmentReconciler) reconcileNetworkPolicies(ctx context.Context, env *loopzerov1.Environment, nsName string) error {
	ctx, span := startChildSpan(ctx, "environment.network_policies.ensure")
	defer span.End()
	runtimeClient, err := r.runtimeClientForEnv(ctx, env)
	if err != nil {
		return fmt.Errorf("network policies: cell-aware client: %w", err)
	}
	if err := r.reconcileEnvDenyAllPolicy(ctx, runtimeClient, nsName); err != nil {
		return err
	}
	if err := r.reconcileEnvAllowIngressPolicy(ctx, runtimeClient, nsName); err != nil {
		return err
	}
	if err := r.reconcileEnvAllowEgressPolicy(ctx, runtimeClient, nsName); err != nil {
		return err
	}
	return nil
}

func (r *EnvironmentReconciler) reconcileEnvDenyAllPolicy(ctx context.Context, runtimeClient client.Client, nsName string) error {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.NetworkPolicyDenyAll,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, runtimeClient, np, func() error {
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
		return fmt.Errorf("networkpolicy %s/%s: %w", nsName, constants.NetworkPolicyDenyAll, err)
	}
	return nil
}

func (r *EnvironmentReconciler) reconcileEnvAllowIngressPolicy(ctx context.Context, runtimeClient client.Client, nsName string) error {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.NetworkPolicyAllowIngress,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, runtimeClient, np, func() error {
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
		return fmt.Errorf("networkpolicy %s/%s: %w", nsName, constants.NetworkPolicyAllowIngress, err)
	}
	return nil
}

func (r *EnvironmentReconciler) reconcileEnvAllowEgressPolicy(ctx context.Context, runtimeClient client.Client, nsName string) error {
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
	_, err := controllerutil.CreateOrUpdate(ctx, runtimeClient, np, func() error {
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
				// API server: TCP 443
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
		return fmt.Errorf("networkpolicy %s/%s: %w", nsName, constants.NetworkPolicyAllowEgress, err)
	}
	return nil
}

// ingressNS returns the namespace where the ingress controller runs.
//
// Default is `traefik` (constants.NamespaceIngress) because LoopZero's
// helmfile installs Traefik there for ALL environments (dev, prod,
// standalone-multi). The previous default `loopzero-system` was wrong and
// caused the `allow-ingress-traefik` NetworkPolicy to block the real
// Traefik namespace, surfacing as silent HTTP 502s downstream. The env
// var override (`INGRESS_NAMESPACE`) remains for clusters where
// operators install Traefik elsewhere.
func (r *EnvironmentReconciler) ingressNS() string {
	if r.IngressNamespace != "" {
		return r.IngressNamespace
	}
	return constants.NamespaceIngress
}

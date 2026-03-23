package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
)

// reconcileNetworkPolicies ensures deny-all + allow-ingress + allow-egress policies exist per namespace.
func (r *TenantReconciler) reconcileNetworkPolicies(ctx context.Context, tenant *loopzerov1.Tenant) error {
	for _, env := range tenant.Spec.Environments {
		nsName := fmt.Sprintf("loopzero-%s-%s", tenant.Name, env.Name)

		if err := r.reconcileDenyAllPolicy(ctx, nsName); err != nil {
			return err
		}
		if err := r.reconcileAllowIngressPolicy(ctx, nsName); err != nil {
			return err
		}
		if err := r.reconcileAllowEgressPolicy(ctx, nsName); err != nil {
			return err
		}
	}

	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionNetworkPolicyReady,
		metav1.ConditionTrue, "NetworkPoliciesConfigured", "network policies provisioned", tenant.Generation)
	return nil
}

// reconcileDenyAllPolicy creates a deny-all NetworkPolicy (blocks all ingress and egress by default).
func (r *TenantReconciler) reconcileDenyAllPolicy(ctx context.Context, nsName string) error {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deny-all",
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, np, func() error {
		np.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{}, // selects all pods
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			// Empty Ingress and Egress = deny all
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("networkpolicy %s/%s: %w", nsName, "deny-all", err)
	}
	return nil
}

// reconcileAllowIngressPolicy allows ingress from Traefik (ingress controller namespace).
func (r *TenantReconciler) reconcileAllowIngressPolicy(ctx context.Context, nsName string) error {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-ingress-traefik",
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, np, func() error {
		np.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{}, // applies to all pods
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "traefik",
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
		return fmt.Errorf("networkpolicy %s/%s: %w", nsName, "allow-ingress-traefik", err)
	}
	return nil
}

// reconcileAllowEgressPolicy allows egress to DNS (kube-system:53), intra-namespace, and API server (TCP 443).
func (r *TenantReconciler) reconcileAllowEgressPolicy(ctx context.Context, nsName string) error {
	dnsPort := intstr.FromInt32(53)
	apiPort := intstr.FromInt32(443)
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-egress",
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
									"kubernetes.io/metadata.name": "kube-system",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &udp, Port: &dnsPort},
					},
				},
				// Intra-namespace: pods within the same namespace
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{},
						},
					},
				},
				// API server: TCP 443 to default namespace (covers kubernetes.default.svc)
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "default",
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
		return fmt.Errorf("networkpolicy %s/%s: %w", nsName, "allow-egress", err)
	}
	return nil
}

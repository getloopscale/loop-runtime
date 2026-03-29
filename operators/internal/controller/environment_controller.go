// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

const environmentFinalizer = constants.FinalizerEnvironment

// environmentSM is the Environment lifecycle state machine (spec §5).
// Phase transitions go through this machine — direct assignment is forbidden (Rule 8).
var environmentSM = statemachine.NewEnvironmentMachine()

// EnvironmentReconciler reconciles an Environment object.
//
// Multi-cluster design: the Environment CR lives in the mgmt cluster,
// but namespace/RBAC/NetworkPolicy resources are created on the runtime cluster.
//   - r.Client        -> mgmt cluster (for reading/updating Environment CR)
//   - r.RuntimeClient -> runtime cluster (for creating namespaces, RBAC, etc.)
type EnvironmentReconciler struct {
	client.Client
	RuntimeClient client.Client
	Scheme        *runtime.Scheme
}

// Reconcile handles the reconciliation loop for Environment resources.
func (r *EnvironmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var env loopzerov1.Environment
	if err := r.Get(ctx, req.NamespacedName, &env); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion with verify-then-remove finalizer pattern
	if !env.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &env)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&env, environmentFinalizer) {
		controllerutil.AddFinalizer(&env, environmentFinalizer)
		if err := r.Update(ctx, &env); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, req.NamespacedName, &env); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Transition from empty to Pending via state machine (Rule 8).
	if env.Status.Phase == "" {
		env.Status.Phase = loopzerov1.EnvironmentPhase(
			environmentSM.Transition(statemachine.EnvironmentEmpty, statemachine.EnvironmentCreate),
		)
	}

	nsName := env.Spec.Namespace.Name
	tenantName := env.Spec.TenantRef.Name

	// envStep pairs a condition type with its reconcile function and success message.
	type envStep struct {
		condType       string
		successReason  string
		successMessage string
		fn             func() error
	}

	steps := []envStep{
		{conditions.TypeEnvNamespaceReady, "NamespaceReady", "namespace provisioned",
			func() error { return r.reconcileNamespace(ctx, &env, nsName, tenantName) }},
		{conditions.TypeEnvRBACReady, "RBACConfigured", "RBAC resources provisioned",
			func() error { return r.reconcileRBAC(ctx, &env, nsName) }},
		{conditions.TypeEnvNetworkPolicyReady, "NetworkPoliciesConfigured", "network policies provisioned",
			func() error {
				if env.Spec.NetworkPolicy.Isolate {
					return r.reconcileNetworkPolicies(ctx, nsName)
				}
				return nil
			}},
		{conditions.TypeEnvQuotaReady, "QuotaReady", "resource quota applied",
			func() error { return r.reconcileQuota(ctx, &env, nsName) }},
	}

	for _, step := range steps {
		if err := step.fn(); err != nil {
			conditions.SetCondition(&env.Status.Conditions, step.condType,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), env.Generation)
			// Re-fetch to avoid stale resourceVersion before status update.
			savedStatus := env.Status
			if refetchErr := r.Get(ctx, req.NamespacedName, &env); refetchErr != nil {
				logger.Error(refetchErr, "failed to re-fetch environment for status update")
				return ctrl.Result{}, err
			}
			env.Status = savedStatus
			if statusErr := r.Status().Update(ctx, &env); statusErr != nil {
				logger.Error(statusErr, "failed to update environment status")
			}
			return ctrl.Result{}, err
		}
		conditions.SetCondition(&env.Status.Conditions, step.condType,
			metav1.ConditionTrue, step.successReason, step.successMessage, env.Generation)
	}

	// All conditions True -> transition to Active via state machine (Rule 8).
	// Only transition if not already Active (idempotent reconcile).
	if env.Status.Phase != loopzerov1.EnvironmentPhaseActive {
		env.Status.Phase = loopzerov1.EnvironmentPhase(
			environmentSM.Transition(statemachine.State(env.Status.Phase), statemachine.EnvironmentProvisioned),
		)
	}
	env.Status.Namespace = nsName
	env.Status.ObservedGeneration = env.Generation

	// Re-fetch to get latest resourceVersion before final status update.
	// Concurrent reconciles (e.g., TenantController's CreateOrUpdate on this CR)
	// may have bumped the resourceVersion since our initial Get.
	savedStatus := env.Status
	if err := r.Get(ctx, req.NamespacedName, &env); err != nil {
		return ctrl.Result{}, err
	}
	env.Status = savedStatus

	if err := r.Status().Update(ctx, &env); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("reconciled environment", "environment", env.Name, "phase", env.Status.Phase, "namespace", nsName)
	// Requeue periodically to detect and correct drift on cross-cluster resources
	// (NetworkPolicy, ResourceQuota, RBAC). Since these live on RuntimeClient,
	// we cannot use Owns() to watch them — periodic reconcile is the safety net.
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// reconcileDelete handles the verify-then-remove finalizer pattern for Environment deletion.
func (r *EnvironmentReconciler) reconcileDelete(ctx context.Context, env *loopzerov1.Environment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(env, environmentFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("environment is being deleted", "environment", env.Name)

	// Transition to Deleting via state machine (Rule 8).
	if env.Status.Phase != loopzerov1.EnvironmentPhaseDeleting {
		env.Status.Phase = loopzerov1.EnvironmentPhase(
			environmentSM.Transition(statemachine.State(env.Status.Phase), statemachine.EnvironmentDeleteRequested),
		)
		if err := r.Status().Update(ctx, env); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Delete namespace on runtime cluster
	nsName := env.Spec.Namespace.Name
	ns := &corev1.Namespace{}
	if err := r.RuntimeClient.Get(ctx, client.ObjectKey{Name: nsName}, ns); err == nil {
		// Check if namespace still belongs to us (label match)
		if ns.Labels != nil && ns.Labels[constants.LabelTenant] == env.Spec.TenantRef.Name {
			if ns.DeletionTimestamp.IsZero() {
				if err := r.RuntimeClient.Delete(ctx, ns); err != nil && !errors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
			}
			// Namespace still exists and belongs to us — requeue to verify deletion
			logger.Info("namespace still exists, requeuing", "namespace", nsName)
			return ctrl.Result{RequeueAfter: constants.RequeueNamespaceDeletionWait}, nil
		}
		// Namespace exists but no longer has our label — consider it cleaned up
	}

	// Namespace gone — remove finalizer
	controllerutil.RemoveFinalizer(env, environmentFinalizer)
	if err := r.Update(ctx, env); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("environment deletion complete", "environment", env.Name)
	return ctrl.Result{}, nil
}

// reconcileNamespace ensures the namespace exists via CreateOrUpdate.
func (r *EnvironmentReconciler) reconcileNamespace(ctx context.Context, env *loopzerov1.Environment, nsName, tenantName string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	result, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, ns, func() error {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels[constants.LabelTenant] = tenantName
		ns.Labels[constants.LabelEnvironment] = env.Spec.Environment
		ns.Labels[constants.LabelManagedBy] = constants.ManagedByEnvironmentController
		return nil
	})
	if err != nil {
		return fmt.Errorf("namespace %s: %w", nsName, err)
	}
	log.FromContext(ctx).Info("namespace reconciled", "namespace", nsName, "result", result)
	return nil
}

// reconcileRBAC ensures ServiceAccount, Role, and RoleBinding exist in the namespace.
func (r *EnvironmentReconciler) reconcileRBAC(ctx context.Context, env *loopzerov1.Environment, nsName string) error {
	// ServiceAccount
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.SADeployer,
			Namespace: nsName,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, sa, func() error {
		if sa.Labels == nil {
			sa.Labels = make(map[string]string)
		}
		sa.Labels[constants.LabelManagedBy] = constants.ManagedByEnvironmentController
		return nil
	}); err != nil {
		return fmt.Errorf("serviceaccount %s/%s: %w", nsName, constants.SADeployer, err)
	}

	// Role (permissions vary by plan tier)
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.RoleDeployer,
			Namespace: nsName,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, role, func() error {
		role.Rules = rbacRulesForPlan(env.Spec.Plan)
		return nil
	}); err != nil {
		return fmt.Errorf("role %s/%s: %w", nsName, constants.RoleDeployer, err)
	}

	// RoleBinding
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.RoleBindingDeployer,
			Namespace: nsName,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, rb, func() error {
		rb.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     constants.RoleDeployer,
		}
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      constants.SADeployer,
				Namespace: nsName,
			},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("rolebinding %s/%s: %w", nsName, constants.RoleBindingDeployer, err)
	}

	return nil
}

// reconcileNetworkPolicies ensures deny-all + allow-ingress + allow-egress policies exist.
func (r *EnvironmentReconciler) reconcileNetworkPolicies(ctx context.Context, nsName string) error {
	if err := r.reconcileEnvDenyAllPolicy(ctx, nsName); err != nil {
		return err
	}
	if err := r.reconcileEnvAllowIngressPolicy(ctx, nsName); err != nil {
		return err
	}
	if err := r.reconcileEnvAllowEgressPolicy(ctx, nsName); err != nil {
		return err
	}
	return nil
}

func (r *EnvironmentReconciler) reconcileEnvDenyAllPolicy(ctx context.Context, nsName string) error {
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
		return fmt.Errorf("networkpolicy %s/%s: %w", nsName, constants.NetworkPolicyDenyAll, err)
	}
	return nil
}

func (r *EnvironmentReconciler) reconcileEnvAllowIngressPolicy(ctx context.Context, nsName string) error {
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
									constants.KubeMetadataNameLabel: constants.NamespacePlatformSystem,
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

func (r *EnvironmentReconciler) reconcileEnvAllowEgressPolicy(ctx context.Context, nsName string) error {
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

// reconcileQuota ensures ResourceQuota and LimitRange exist if configured.
func (r *EnvironmentReconciler) reconcileQuota(ctx context.Context, env *loopzerov1.Environment, nsName string) error {
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
		_, err = controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, quota, func() error {
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
	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.LimitRangeTenant,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, lr, func() error {
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
		return fmt.Errorf("limitrange %s/%s: %w", nsName, constants.LimitRangeTenant, err)
	}

	return nil
}

// rbacRulesForPlan returns the RBAC rules for a given plan tier.
// Rules are split by API group to avoid RBAC escalation issues:
// Kubernetes expands combined APIGroups × Resources as a cartesian product,
// requiring the controller to hold ALL expanded combinations.
func rbacRulesForPlan(plan loopzerov1.PlanTier) []rbacv1.PolicyRule {
	// Starter: basic deploy permissions (split by API group)
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"services", "configmaps"},
			Verbs:     []string{"get", "list", "create", "update", "patch"},
		},
		{
			APIGroups: []string{"apps"},
			Resources: []string{"deployments"},
			Verbs:     []string{"get", "list", "create", "update", "patch"},
		},
	}

	if plan == loopzerov1.PlanGrowth || plan == loopzerov1.PlanEnterprise {
		// Growth: starter + secrets
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     []string{"get", "list", "create", "update", "patch"},
		})
	}

	if plan == loopzerov1.PlanEnterprise {
		// Enterprise: growth + ingress/networkpolicy management
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{"networking.k8s.io"},
			Resources: []string{"ingresses", "networkpolicies"},
			Verbs:     []string{"get", "list", "create", "update", "patch", "delete"},
		})
	}

	return rules
}

// SetupEnvironmentController registers the EnvironmentController with the manager.
func SetupEnvironmentController(mgr ctrl.Manager, runtimeClient client.Client) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.Environment{}).
		Named("environment").
		Complete(&EnvironmentReconciler{
			Client:        mgr.GetClient(),
			RuntimeClient: runtimeClient,
			Scheme:        mgr.GetScheme(),
		})
}

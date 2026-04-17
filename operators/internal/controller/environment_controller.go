// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"fmt"
	"os"
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
	RuntimeClient    client.Client
	Scheme           *runtime.Scheme
	IngressNamespace string // Namespace where ingress controller (Traefik) runs. Default: platform-system.
}

// Reconcile handles the reconciliation loop for Environment resources.
func (r *EnvironmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := startReconcileSpan(ctx, "environment", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			recordError(span, retErr)
		} else {
			setSuccess(span)
		}
		reconcileOutcome("environment", start, retErr)
	}()

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
				// Task 7.3: NetworkPolicy is ALWAYS applied, regardless of Isolate flag.
				// Every tenant namespace must have baseline network policies.
				return r.reconcileNetworkPolicies(ctx, nsName)
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
	// Uses TryTransition because env.Status.Phase comes from CRD status (external source).
	if env.Status.Phase != loopzerov1.EnvironmentPhaseActive {
		next, err := environmentSM.TryTransition(statemachine.State(env.Status.Phase), statemachine.EnvironmentProvisioned)
		if err != nil {
			logger.Error(err, "invalid state transition", "phase", env.Status.Phase)
			return ctrl.Result{}, err
		}
		env.Status.Phase = loopzerov1.EnvironmentPhase(next)
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
	// Uses TryTransition because env.Status.Phase comes from CRD status (external source).
	if env.Status.Phase != loopzerov1.EnvironmentPhaseDeleting {
		next, err := environmentSM.TryTransition(statemachine.State(env.Status.Phase), statemachine.EnvironmentDeleteRequested)
		if err != nil {
			logger.Error(err, "invalid state transition for delete", "phase", env.Status.Phase)
			return ctrl.Result{}, err
		}
		env.Status.Phase = loopzerov1.EnvironmentPhase(next)
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
	ctx, span := startChildSpan(ctx, "environment.namespace.ensure")
	defer span.End()
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
		// Task 7.2: PodSecurity Admission — enforce restricted profile on all tenant namespaces.
		// This prevents pods from running as root, using hostPath, etc.
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

	// Task 7.4: Dedicated ServiceAccount for app workloads (separate from deployer SA).
	// Pods use this SA with automountServiceAccountToken=false.
	//
	// imagePullSecrets provisioning (bug #24 fix — L2 race condition):
	//   1. Copy the docker config from loopzero-infra-registry-source in platform-system
	//      (seeded once at platform bootstrap) into this namespace as loopzero-infra-registry.
	//   2. Attach it to loopzero-app SA.
	//
	// Previously we relied on doctl kubernetes cluster registry add propagating a
	// secret to the default SA, then copied it to loopzero-app. That mechanism was not
	// guaranteed on fresh namespaces under load — pods could be created by ArgoCD
	// before the default SA had the pull secret, causing ImagePullBackOff that
	// kubelet cached until manual pod deletion.
	if err := r.ensurePullSecret(ctx, nsName); err != nil {
		// Non-fatal: pod may still pull if another mechanism (doctl integration,
		// namespace-level auto-propagation) provides the secret later.
		// Log but do not block Environment readiness.
		log.FromContext(ctx).Info("pull secret ensure failed (non-fatal)", "error", err.Error(), "namespace", nsName)
	}

	// Bug #24 complement: if a pod was already Pending with ImagePullBackOff
	// (because ArgoCD sync created it BEFORE this reconcile ran and provisioned
	// the pull secret), kubelet caches the 401 and won't retry with the new
	// secret. Delete stuck pods so their ReplicaSet recreates them with the
	// now-valid imagePullSecrets on loopzero-app SA.
	//
	// Non-fatal: if pod list/delete fails, the pod may self-recover later
	// (kubelet does eventually retry) or manual intervention will fix it.
	if err := r.kickStuckPullBackoffPods(ctx, nsName); err != nil {
		log.FromContext(ctx).Info("kick stuck pull-backoff pods failed (non-fatal)", "error", err.Error(), "namespace", nsName)
	}

	appSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "loopzero-app",
			Namespace: nsName,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, appSA, func() error {
		if appSA.Labels == nil {
			appSA.Labels = make(map[string]string)
		}
		appSA.Labels[constants.LabelManagedBy] = constants.ManagedByEnvironmentController
		falseVal := false
		appSA.AutomountServiceAccountToken = &falseVal
		// Prefer loopzero-infra-registry (provisioned by ensurePullSecret above).
		// Fall back to copying from default SA (legacy doctl integration path).
		appSA.ImagePullSecrets = []corev1.LocalObjectReference{
			{Name: "loopzero-infra-registry"},
		}
		var defaultSA corev1.ServiceAccount
		if err := r.RuntimeClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "default"}, &defaultSA); err == nil {
			// Merge: keep loopzero-infra-registry + add any other refs from default.
			for _, ref := range defaultSA.ImagePullSecrets {
				if ref.Name == "loopzero-infra-registry" {
					continue
				}
				appSA.ImagePullSecrets = append(appSA.ImagePullSecrets, ref)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("serviceaccount %s/loopzero-app: %w", nsName, err)
	}

	return nil
}

// ensurePullSecret copies the registry pull secret from the control plane
// namespace (platform-system) into the tenant runtime namespace. Called
// per-reconcile; idempotent via CreateOrUpdate.
//
// Source secret: loopzero-infra-registry in platform-system, seeded at bootstrap.
// Destination: loopzero-infra-registry in nsName.
func (r *EnvironmentReconciler) ensurePullSecret(ctx context.Context, nsName string) error {
	var src corev1.Secret
	srcKey := client.ObjectKey{Namespace: "platform-system", Name: "loopzero-infra-registry"}
	// r.Client is the mgmt cluster client (embedded); r.RuntimeClient is runtime.
	if err := r.Client.Get(ctx, srcKey, &src); err != nil {
		return fmt.Errorf("source pull secret platform-system/loopzero-infra-registry: %w", err)
	}

	dst := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "loopzero-infra-registry",
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, dst, func() error {
		dst.Type = corev1.SecretTypeDockerConfigJson
		dst.Data = src.Data
		if dst.Labels == nil {
			dst.Labels = make(map[string]string)
		}
		dst.Labels[constants.LabelManagedBy] = constants.ManagedByEnvironmentController
		return nil
	})
	if err != nil {
		return fmt.Errorf("destination pull secret %s/loopzero-infra-registry: %w", nsName, err)
	}
	return nil
}

// kickStuckPullBackoffPods deletes pods in the namespace that are stuck in
// ImagePullBackOff or ErrImagePull. When ArgoCD sync creates a pod BEFORE
// this controller has provisioned the imagePullSecret, kubelet caches the 401
// failure and won't retry with the new secret. Deleting the pod forces the
// ReplicaSet to recreate it, which picks up the now-valid imagePullSecrets
// from the loopzero-app ServiceAccount.
//
// Bug #24 complement (scale-50k L2). Idempotent — no-op if no stuck pods.
func (r *EnvironmentReconciler) kickStuckPullBackoffPods(ctx context.Context, nsName string) error {
	var pods corev1.PodList
	if err := r.RuntimeClient.List(ctx, &pods, client.InNamespace(nsName)); err != nil {
		return fmt.Errorf("list pods in %s: %w", nsName, err)
	}
	var kicked int
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp != nil {
			continue // already being deleted
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil &&
				(cs.State.Waiting.Reason == "ImagePullBackOff" || cs.State.Waiting.Reason == "ErrImagePull") {
				if err := r.RuntimeClient.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
					log.FromContext(ctx).Info("kick pod failed",
						"pod", pod.Name, "namespace", nsName, "error", err.Error())
					continue
				}
				kicked++
				break
			}
		}
	}
	if kicked > 0 {
		log.FromContext(ctx).Info("kicked stuck pull-backoff pods",
			"namespace", nsName, "count", kicked)
	}
	return nil
}

// reconcileNetworkPolicies ensures deny-all + allow-ingress + allow-egress policies exist.
func (r *EnvironmentReconciler) reconcileNetworkPolicies(ctx context.Context, nsName string) error {
	ctx, span := startChildSpan(ctx, "environment.network_policies.ensure")
	defer span.End()
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

// ingressNS returns the namespace where the ingress controller runs.
func (r *EnvironmentReconciler) ingressNS() string {
	if r.IngressNamespace != "" {
		return r.IngressNamespace
	}
	return constants.NamespacePlatformSystem
}

// SetupEnvironmentController registers the EnvironmentController with the manager.
func SetupEnvironmentController(mgr ctrl.Manager, runtimeClient client.Client) error {
	ingressNS := os.Getenv("INGRESS_NAMESPACE")
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.Environment{}).
		Named("environment").
		Complete(&EnvironmentReconciler{
			Client:           mgr.GetClient(),
			RuntimeClient:    runtimeClient,
			Scheme:           mgr.GetScheme(),
			IngressNamespace: ingressNS,
		})
}

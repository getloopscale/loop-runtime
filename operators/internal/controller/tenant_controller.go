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
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/gitea"
	"github.com/loopzero/loopzero/operators/internal/statemachine"
)

const tenantFinalizer = "loopzero.io/tenant-cleanup"

// tenantSM is the Tenant lifecycle state machine (spec §5).
// Phase transitions go through this machine — direct assignment is forbidden.
var tenantSM = statemachine.NewTenantMachine()

// TenantReconciler reconciles a Tenant object.
type TenantReconciler struct {
	client.Client
	GitProvisioner gitea.GitRepoProvisioner
}

// Reconcile handles the reconciliation loop for Tenant resources.
func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var tenant loopzerov1.Tenant
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion with verify-then-remove finalizer pattern
	if !tenant.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &tenant)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&tenant, tenantFinalizer) {
		controllerutil.AddFinalizer(&tenant, tenantFinalizer)
		if err := r.Update(ctx, &tenant); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Set Phase to Provisioning if empty (via state machine)
	if tenant.Status.Phase == "" {
		tenant.Status.Phase = loopzerov1.TenantPhase(
			tenantSM.Transition(statemachine.TenantEmpty, statemachine.TenantCreate),
		)
	}

	// Detect spec change: if generation advanced while Phase is Active, transition back to Provisioning
	// so all reconcile steps re-execute (condition-gated checks compare ObservedGeneration).
	if tenant.Status.Phase == loopzerov1.TenantPhaseActive &&
		tenant.Generation != tenant.Status.ObservedGeneration {
		tenant.Status.Phase = loopzerov1.TenantPhase(
			tenantSM.Transition(statemachine.TenantActive, statemachine.TenantSpecChanged),
		)
	}

	// S1.2: Reconcile namespaces + ResourceQuota + LimitRange
	// Skip if NamespacesReady is already True at current generation (crash recovery optimization)
	if !conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionNamespacesReady, tenant.Generation) {
		if err := r.reconcileNamespaces(ctx, &tenant); err != nil {
			loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionNamespacesReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			_ = r.Status().Update(ctx, &tenant)
			return ctrl.Result{}, err
		}
	}

	// S1.3: Reconcile RBAC (ServiceAccount + Role + RoleBinding)
	// Skip if RBACReady is already True at current generation
	if !conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionRBACReady, tenant.Generation) {
		if err := r.reconcileRBAC(ctx, &tenant); err != nil {
			loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionRBACReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			_ = r.Status().Update(ctx, &tenant)
			return ctrl.Result{}, err
		}
	}

	// S1.4: Reconcile NetworkPolicies (deny-all + allow-ingress + allow-egress)
	// Skip if NetworkPolicyReady is already True at current generation
	if !conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionNetworkPolicyReady, tenant.Generation) {
		if err := r.reconcileNetworkPolicies(ctx, &tenant); err != nil {
			loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionNetworkPolicyReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			_ = r.Status().Update(ctx, &tenant)
			return ctrl.Result{}, err
		}
	}

	// S1.6: Reconcile Gitea repo
	// Skip if GitRepoReady is already True at current generation
	if !conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionGitRepoReady, tenant.Generation) {
		if err := r.reconcileGitRepo(ctx, &tenant); err != nil {
			loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionGitRepoReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			_ = r.Status().Update(ctx, &tenant)
			return ctrl.Result{}, err
		}
	}

	// S1.5: Reconcile build namespace + QuotaEnforced
	// Skip if BOTH BuildNamespaceReady AND QuotaEnforced are True at current generation
	// (both conditions are set inside reconcileBuildNamespace)
	if !conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionBuildNamespaceReady, tenant.Generation) ||
		!conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionQuotaEnforced, tenant.Generation) {
		if err := r.reconcileBuildNamespace(ctx, &tenant); err != nil {
			loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionBuildNamespaceReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			_ = r.Status().Update(ctx, &tenant)
			return ctrl.Result{}, err
		}
	}

	// S1.7: Phase transition — Active when all conditions are True (via state machine)
	if allConditionsTrue(tenant.Status.Conditions) && tenant.Status.Phase == loopzerov1.TenantPhaseProvisioning {
		tenant.Status.Phase = loopzerov1.TenantPhase(
			tenantSM.Transition(statemachine.TenantProvisioning, statemachine.TenantAllReady),
		)
	}
	tenant.Status.ObservedGeneration = tenant.Generation

	// Update status
	if err := r.Status().Update(ctx, &tenant); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("reconciled tenant", "tenant", tenant.Name, "phase", tenant.Status.Phase)
	return ctrl.Result{}, nil
}

// conditionIsTrue checks if a specific condition is True AND its ObservedGeneration
// matches the current resource generation. This enables condition-gated reconciliation:
// if a step already succeeded at the current generation, we skip re-executing it.
// When the spec changes (generation bumps), all steps are re-executed.
func conditionIsTrue(conditions []metav1.Condition, conditionType string, generation int64) bool {
	for _, c := range conditions {
		if c.Type == conditionType {
			return c.Status == metav1.ConditionTrue && c.ObservedGeneration == generation
		}
	}
	return false
}

// allConditionsTrue checks if all required conditions are True.
func allConditionsTrue(conditions []metav1.Condition) bool {
	required := []string{
		loopzerov1.ConditionGitRepoReady,
		loopzerov1.ConditionNamespacesReady,
		loopzerov1.ConditionRBACReady,
		loopzerov1.ConditionNetworkPolicyReady,
		loopzerov1.ConditionBuildNamespaceReady,
		loopzerov1.ConditionQuotaEnforced,
	}
	for _, req := range required {
		found := false
		for _, c := range conditions {
			if c.Type == req && c.Status == metav1.ConditionTrue {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// reconcileDelete handles the verify-then-remove finalizer pattern for Tenant deletion.
func (r *TenantReconciler) reconcileDelete(ctx context.Context, tenant *loopzerov1.Tenant) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(tenant, tenantFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("tenant is being deleted", "tenant", tenant.Name)
	currentPhase := statemachine.State(tenant.Status.Phase)
	if currentPhase == "" {
		currentPhase = statemachine.TenantEmpty
	}
	// Only transition if not already Deleting (idempotent)
	if tenant.Status.Phase != loopzerov1.TenantPhaseDeleting {
		tenant.Status.Phase = loopzerov1.TenantPhase(
			tenantSM.Transition(currentPhase, statemachine.TenantDelete),
		)
	}
	if err := r.Status().Update(ctx, tenant); err != nil {
		return ctrl.Result{}, err
	}

	// Phase 1: Issue deletes (idempotent)
	// Delete all namespaces with tenant label
	var nsList corev1.NamespaceList
	if err := r.List(ctx, &nsList, client.MatchingLabels{"loopzero.io/tenant": tenant.Name}); err != nil {
		return ctrl.Result{}, err
	}
	for i := range nsList.Items {
		if nsList.Items[i].DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, &nsList.Items[i]); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
	}

	// Archive Gitea repo (soft delete, idempotent)
	if err := r.GitProvisioner.ArchiveRepo(ctx, tenant.Spec.GitOps.RepoName); err != nil {
		logger.Error(err, "failed to archive gitea repo", "repo", tenant.Spec.GitOps.RepoName)
		// Non-fatal: continue with cleanup
	}

	// Phase 2: Verify all namespaces are gone
	var remaining corev1.NamespaceList
	if err := r.List(ctx, &remaining, client.MatchingLabels{"loopzero.io/tenant": tenant.Name}); err != nil {
		return ctrl.Result{}, err
	}
	if len(remaining.Items) > 0 {
		logger.Info("namespaces still exist, requeuing", "count", len(remaining.Items))
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// All clean — remove finalizer
	controllerutil.RemoveFinalizer(tenant, tenantFinalizer)
	if err := r.Update(ctx, tenant); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("tenant deletion complete", "tenant", tenant.Name)
	return ctrl.Result{}, nil
}

// reconcileNamespaces ensures each environment has a namespace with ResourceQuota and LimitRange.
func (r *TenantReconciler) reconcileNamespaces(ctx context.Context, tenant *loopzerov1.Tenant) error {
	for _, env := range tenant.Spec.Environments {
		nsName := fmt.Sprintf("%s-%s", tenant.Name, env.Name)

		// Create or update namespace (NO ownerReference — cross-namespace limitation)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
			},
		}
		result, err := controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
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

	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionNamespacesReady,
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
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, quota, func() error {
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
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, lr, func() error {
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

// reconcileRBAC ensures RBAC resources (ServiceAccount, Role, RoleBinding) exist per namespace.
func (r *TenantReconciler) reconcileRBAC(ctx context.Context, tenant *loopzerov1.Tenant) error {
	for _, env := range tenant.Spec.Environments {
		nsName := fmt.Sprintf("%s-%s", tenant.Name, env.Name)

		// ServiceAccount
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "loopzero-deployer",
				Namespace: nsName,
			},
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
			return nil
		}); err != nil {
			return fmt.Errorf("serviceaccount %s/%s: %w", nsName, "loopzero-deployer", err)
		}

		// Role (permissions vary by plan tier)
		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "loopzero-deployer",
				Namespace: nsName,
			},
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, role, func() error {
			role.Rules = rbacRulesForPlan(tenant.Spec.Plan)
			return nil
		}); err != nil {
			return fmt.Errorf("role %s/%s: %w", nsName, "loopzero-deployer", err)
		}

		// RoleBinding
		rb := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "loopzero-deployer",
				Namespace: nsName,
			},
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, rb, func() error {
			rb.RoleRef = rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "Role",
				Name:     "loopzero-deployer",
			}
			rb.Subjects = []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "loopzero-deployer",
					Namespace: nsName,
				},
			}
			return nil
		}); err != nil {
			return fmt.Errorf("rolebinding %s/%s: %w", nsName, "loopzero-deployer", err)
		}
	}

	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionRBACReady,
		metav1.ConditionTrue, "RBACConfigured", "RBAC resources provisioned", tenant.Generation)
	return nil
}

// reconcileGitRepo ensures the GitOps repository exists via the GitRepoProvisioner.
func (r *TenantReconciler) reconcileGitRepo(ctx context.Context, tenant *loopzerov1.Tenant) error {
	repoURL, err := r.GitProvisioner.EnsureRepo(ctx, tenant.Spec.GitOps.RepoName, tenant.Spec.GitOps.Branch)
	if err != nil {
		return fmt.Errorf("gitea repo %s: %w", tenant.Spec.GitOps.RepoName, err)
	}

	tenant.Status.GitOpsRepo = repoURL
	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionGitRepoReady,
		metav1.ConditionTrue, "RepoReady", "git repository provisioned", tenant.Generation)
	return nil
}

// reconcileBuildNamespace ensures the build-{tenantID} namespace exists with ResourceQuota.
func (r *TenantReconciler) reconcileBuildNamespace(ctx context.Context, tenant *loopzerov1.Tenant) error {
	buildNs := fmt.Sprintf("build-%s", tenant.Name)

	// Create or update build namespace (NO ownerReference — cross-namespace limitation)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: buildNs,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels["loopzero.io/tenant"] = tenant.Name
		ns.Labels["loopzero.io/component"] = "build"
		ns.Labels["loopzero.io/managed-by"] = "tenant-controller"
		return nil
	})
	if err != nil {
		return fmt.Errorf("build namespace %s: %w", buildNs, err)
	}

	// ResourceQuota for build namespace
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "build-quota",
			Namespace: buildNs,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, quota, func() error {
		quota.Spec.Hard = corev1.ResourceList{
			corev1.ResourceRequestsCPU:    resource.MustParse("4"),
			corev1.ResourceRequestsMemory: resource.MustParse("8Gi"),
			corev1.ResourceLimitsCPU:      resource.MustParse("8"),
			corev1.ResourceLimitsMemory:   resource.MustParse("16Gi"),
		}
		// Enforce maxBuildConcurrency via workflow count quota
		if tenant.Spec.Limits.MaxBuildConcurrency > 0 {
			quota.Spec.Hard[corev1.ResourceName("count/workflows.argoproj.io")] =
				resource.MustParse(fmt.Sprintf("%d", tenant.Spec.Limits.MaxBuildConcurrency))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("build resourcequota %s/%s: %w", buildNs, "build-quota", err)
	}

	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionBuildNamespaceReady,
		metav1.ConditionTrue, "BuildNamespaceReady", "build namespace provisioned", tenant.Generation)

	// QuotaEnforced = True when ALL quotas (workload + build) are applied
	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionQuotaEnforced,
		metav1.ConditionTrue, "AllQuotasEnforced", "all resource quotas applied", tenant.Generation)

	return nil
}

// reconcileNetworkPolicies ensures deny-all + allow-ingress + allow-egress policies exist per namespace.
func (r *TenantReconciler) reconcileNetworkPolicies(ctx context.Context, tenant *loopzerov1.Tenant) error {
	for _, env := range tenant.Spec.Environments {
		nsName := fmt.Sprintf("%s-%s", tenant.Name, env.Name)

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
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, np, func() error {
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
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, np, func() error {
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
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, np, func() error {
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

// rbacRulesForPlan returns the RBAC rules for a given plan tier.
func rbacRulesForPlan(plan loopzerov1.PlanTier) []rbacv1.PolicyRule {
	// Starter: basic deploy permissions
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{"", "apps"},
			Resources: []string{"deployments", "services", "configmaps"},
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

// SetupTenantController registers the TenantController with the manager.
func SetupTenantController(mgr ctrl.Manager, gitProvisioner gitea.GitRepoProvisioner) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.Tenant{}).
		Named("tenant").
		Complete(&TenantReconciler{
			Client:         mgr.GetClient(),
			GitProvisioner: gitProvisioner,
		})
}

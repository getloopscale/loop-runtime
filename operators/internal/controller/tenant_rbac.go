package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/pkg/naming"
)

// reconcileRBAC ensures RBAC resources (ServiceAccount, Role, RoleBinding) exist per namespace.
func (r *TenantReconciler) reconcileRBAC(ctx context.Context, tenant *loopzerov1.Tenant) error {
	for _, env := range tenant.Spec.Environments {
		nsName, err := naming.EnvironmentNamespace(tenant.Name, env.Name)
		if err != nil {
			return fmt.Errorf("invalid namespace name for env %s: %w", env.Name, err)
		}

		// ServiceAccount
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "loopzero-deployer",
				Namespace: nsName,
			},
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, sa, func() error {
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
		if _, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, role, func() error {
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
		if _, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, rb, func() error {
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

	conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeRBACReady,
		metav1.ConditionTrue, "RBACConfigured", "RBAC resources provisioned", tenant.Generation)
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

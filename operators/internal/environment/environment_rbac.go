// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environment

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/planentitlements"
	"github.com/getloopscale/loopzero/operators/internal/tenantpolicy"
)

// reconcileRBAC ensures ServiceAccount, Role, and RoleBinding exist in the namespace.
// fix-all-platform-issues-plan T1.1 — writes through the cell client Reconcile resolved.
func (r *EnvironmentReconciler) reconcileRBAC(ctx context.Context, runtimeClient client.Client, env *loopzerov1.Environment, nsName string) error {
	// ServiceAccount
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.SADeployer,
			Namespace: nsName,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, runtimeClient, sa, func() error {
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
	if _, err := controllerutil.CreateOrUpdate(ctx, runtimeClient, role, func() error {
		role.Rules = rbacRulesForPlan(env.Spec.Plan)
		return nil
	}); err != nil {
		return fmt.Errorf("role %s/%s: %w", nsName, constants.RoleDeployer, err)
	}

	// RoleBinding — bind loopzero-deployer SA to loopzero-deployer Role.
	if err := tenantpolicy.ApplySingleSARoleBinding(ctx, runtimeClient, nsName, constants.RoleBindingDeployer, constants.RoleDeployer, constants.SADeployer); err != nil {
		return fmt.Errorf("rolebinding %s/%s: %w", nsName, constants.RoleBindingDeployer, err)
	}

	// Task 7.4: Dedicated ServiceAccount for app workloads (separate from deployer SA).
	// Pods use this SA with automountServiceAccountToken=false.
	//
	// imagePullSecrets provisioning (bug #24 fix — L2 race condition):
	//   1. Copy the docker config from loopzero-system-source in loopzero-system
	//      (seeded once at platform bootstrap) into this namespace as loopzero-system.
	//   2. Attach it to loopzero-app SA.
	// F-09 (CHG-2026-04-29-DOGFOOD T4.1 / drift-baseline.md port #2):
	// when the source pull secret does not exist (standalone-multi, local k3d
	// with a public registry), this path used to log "pull secret ensure
	// failed (non-fatal)" on every reconcile. NotFound (expected in some
	// profiles) is now told apart from a real failure: NotFound becomes
	// TypePullSecretReady=False/SourceMissing with NO log (idempotent, no
	// noise); other failures are still logged.
	switch err := r.ensurePullSecret(ctx, runtimeClient, nsName); {
	case err == nil:
		conditions.SetCondition(&env.Status.Conditions, conditions.TypePullSecretReady,
			metav1.ConditionTrue, conditions.ReasonReady, "Pull secret provisioned", env.Generation)
	case errors.IsNotFound(err):
		// Source absent: a profile with no private registry. Not a runtime failure.
		conditions.SetCondition(&env.Status.Conditions, conditions.TypePullSecretReady,
			metav1.ConditionFalse, "SourceMissing",
			"Source registry pull secret not present in loopzero-system; pods will rely on public images or external auto-mount.",
			env.Generation)
	default:
		log.FromContext(ctx).Info("pull secret ensure failed (non-fatal)", "error", err.Error(), "namespace", nsName)
		conditions.SetCondition(&env.Status.Conditions, conditions.TypePullSecretReady,
			metav1.ConditionFalse, conditions.ReasonFailed, err.Error(), env.Generation)
	}

	// Bug #24 complement: delete stuck ImagePullBackOff pods so ReplicaSet recreates them.
	if err := r.kickStuckPullBackoffPods(ctx, runtimeClient, nsName); err != nil {
		log.FromContext(ctx).Info("kick stuck pull-backoff pods failed (non-fatal)", "error", err.Error(), "namespace", nsName)
	}

	appSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.SAApp,
			Namespace: nsName,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, runtimeClient, appSA, func() error {
		if appSA.Labels == nil {
			appSA.Labels = make(map[string]string)
		}
		appSA.Labels[constants.LabelManagedBy] = constants.ManagedByEnvironmentController
		falseVal := false
		appSA.AutomountServiceAccountToken = &falseVal
		appSA.ImagePullSecrets = []corev1.LocalObjectReference{
			{Name: constants.SecretRegistryPull},
		}
		var defaultSA corev1.ServiceAccount
		if err := runtimeClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "default"}, &defaultSA); err == nil {
			for _, ref := range defaultSA.ImagePullSecrets {
				if ref.Name == constants.SecretRegistryPull {
					continue
				}
				appSA.ImagePullSecrets = append(appSA.ImagePullSecrets, ref)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("serviceaccount %s/%s: %w", nsName, constants.SAApp, err)
	}

	return nil
}

// ensurePullSecret copies the registry pull secret from loopzero-system into the tenant namespace.
// fix-all-platform-issues-plan T1.1 — writes through the cell client Reconcile resolved.
func (r *EnvironmentReconciler) ensurePullSecret(ctx context.Context, runtimeClient client.Client, nsName string) error {
	var src corev1.Secret
	srcKey := client.ObjectKey{Namespace: constants.NamespacePlatformSystem, Name: constants.SecretRegistryPull}
	if err := r.Get(ctx, srcKey, &src); err != nil {
		return fmt.Errorf("source pull secret loopzero-system/loopzero-system: %w", err)
	}

	dst := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.SecretRegistryPull,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, runtimeClient, dst, func() error {
		dst.Type = corev1.SecretTypeDockerConfigJson
		dst.Data = src.Data
		if dst.Labels == nil {
			dst.Labels = make(map[string]string)
		}
		dst.Labels[constants.LabelManagedBy] = constants.ManagedByEnvironmentController
		return nil
	})
	if err != nil {
		return fmt.Errorf("destination pull secret %s/loopzero-system: %w", nsName, err)
	}
	return nil
}

// kickStuckPullBackoffPods deletes pods stuck in ImagePullBackOff or ErrImagePull.
// Bug #24 complement (scale-50k L2). Idempotent — no-op if no stuck pods.
// fix-all-platform-issues-plan T1.1 — writes through the cell client Reconcile resolved.
func (r *EnvironmentReconciler) kickStuckPullBackoffPods(ctx context.Context, runtimeClient client.Client, nsName string) error {
	var pods corev1.PodList
	if err := runtimeClient.List(ctx, &pods, client.InNamespace(nsName)); err != nil {
		return fmt.Errorf("list pods in %s: %w", nsName, err)
	}
	var kicked int
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil &&
				(cs.State.Waiting.Reason == "ImagePullBackOff" || cs.State.Waiting.Reason == "ErrImagePull") {
				if err := runtimeClient.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
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

// rbacRulesForPlan returns the RBAC rules for a given plan tier.
func rbacRulesForPlan(plan loopzerov1.PlanTier) []rbacv1.PolicyRule {
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

	entitled := planentitlements.For(plan)
	if entitled.ManagesSecrets {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     []string{"get", "list", "create", "update", "patch"},
		})
	}

	if entitled.ManagesNetworking {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{"networking.k8s.io"},
			Resources: []string{"ingresses", "networkpolicies"},
			Verbs:     []string{"get", "list", "create", "update", "patch", "delete"},
		})
	}

	return rules
}

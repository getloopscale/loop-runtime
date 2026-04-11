// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	k "github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/operators/internal/harbor"
	"github.com/loopzero/loopzero/pkg/naming"
)

// ensureHarborProject creates a Harbor project for the tenant if it doesn't exist.
// Sets condition HarborProjectReady on the Tenant CR.
// No-op when HarborClient is nil (backward compatibility with DOCR mode).
func (r *TenantReconciler) ensureHarborProject(ctx context.Context, tenant *loopzerov1.Tenant) error {
	if r.HarborClient == nil {
		return nil
	}

	quota := harborQuotaForPlan(tenant.Spec.Plan)

	if err := r.HarborClient.EnsureProject(ctx, tenant.Name, harbor.ProjectOpts{
		StorageQuota: quota,
		Immutable:    true,
	}); err != nil {
		conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeHarborProjectReady,
			metav1.ConditionFalse, "EnsureProjectFailed", err.Error(), tenant.Generation)
		return fmt.Errorf("harbor project %s: %w", tenant.Name, err)
	}

	slog.Info("harbor project ensured", "tenant", tenant.Name, "quotaBytes", quota)
	conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeHarborProjectReady,
		metav1.ConditionTrue, "HarborProjectReady", "harbor project provisioned", tenant.Generation)
	return nil
}

// ensureHarborRobotAccounts creates/rotates robot accounts for the tenant:
//   - build robot (push+pull) stored as buildkit-registry-config in build namespace
//   - pull robot (pull-only) stored as imagePullSecret in workload namespaces
//
// No-op when HarborClient is nil.
func (r *TenantReconciler) ensureHarborRobotAccounts(ctx context.Context, tenant *loopzerov1.Tenant) error {
	if r.HarborClient == nil {
		return nil
	}

	buildNs, err := naming.BuildNamespace(tenant.Name)
	if err != nil {
		return fmt.Errorf("invalid build namespace for tenant %s: %w", tenant.Name, err)
	}

	// Build robot — push+pull access for BuildKit.
	buildCreds, err := r.HarborClient.EnsureRobotAccount(ctx, tenant.Name, harbor.RobotAccountOpts{
		Name:       "build-" + tenant.Name,
		TTL:        k.HarborRobotAccountTTL,
		PushAccess: true,
	})
	if err != nil {
		return fmt.Errorf("harbor build robot for %s: %w", tenant.Name, err)
	}

	// Only write the secret when we have a fresh secret (non-empty).
	// Harbor does not return the secret for existing robots that are still valid.
	if buildCreds.Secret != "" {
		if err := r.ensureHarborRegistrySecret(ctx, buildNs, tenant.Name, buildCreds); err != nil {
			return fmt.Errorf("harbor build registry secret for %s: %w", tenant.Name, err)
		}
		slog.Info("harbor build robot credentials stored",
			"tenant", tenant.Name, "namespace", buildNs, "robot", buildCreds.Username)
	}

	// Pull robot — read-only access for workload pods.
	pullCreds, err := r.HarborClient.EnsureRobotAccount(ctx, tenant.Name, harbor.RobotAccountOpts{
		Name:       "pull-" + tenant.Name,
		TTL:        k.HarborRobotAccountTTL,
		PushAccess: false,
	})
	if err != nil {
		return fmt.Errorf("harbor pull robot for %s: %w", tenant.Name, err)
	}

	// Store pull credentials in each workload namespace.
	if pullCreds.Secret != "" {
		for _, env := range tenant.Spec.Environments {
			envNs, nsErr := naming.EnvironmentNamespace(tenant.Name, env.Name)
			if nsErr != nil {
				return fmt.Errorf("invalid namespace for env %s: %w", env.Name, nsErr)
			}
			if err := r.ensureHarborPullSecret(ctx, envNs, tenant.Name, pullCreds); err != nil {
				return fmt.Errorf("harbor pull secret for %s/%s: %w", tenant.Name, env.Name, err)
			}
		}
		slog.Info("harbor pull robot credentials stored",
			"tenant", tenant.Name, "environments", len(tenant.Spec.Environments), "robot", pullCreds.Username)
	}

	return nil
}

// deleteHarborProject removes the Harbor project during tenant deletion.
// No-op when HarborClient is nil.
func (r *TenantReconciler) deleteHarborProject(ctx context.Context, tenant *loopzerov1.Tenant) error {
	if r.HarborClient == nil {
		return nil
	}

	if err := r.HarborClient.DeleteProject(ctx, tenant.Name); err != nil {
		return fmt.Errorf("harbor delete project %s: %w", tenant.Name, err)
	}

	slog.Info("harbor project deleted", "tenant", tenant.Name)
	return nil
}

// ensureHarborRegistrySecret creates or updates the BuildKit registry config Secret
// with Harbor robot credentials in Docker config.json format.
func (r *TenantReconciler) ensureHarborRegistrySecret(ctx context.Context, namespace, tenantName string, creds *harbor.RobotCredentials) error {
	dockerConfigJSON := buildDockerConfigJSON(r.HarborRegistryHost, creds.Username, creds.Secret)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: k.SecretHarborRegistryConfig, Namespace: namespace},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, secret, func() error {
		secret.Labels = map[string]string{
			k.LabelTenant:    tenantName,
			k.LabelComponent: k.ComponentBuild,
			k.LabelManagedBy: k.ManagedByTenantController,
		}
		if secret.Annotations == nil {
			secret.Annotations = make(map[string]string)
		}
		secret.Annotations[k.AnnotationTokenExpiresAt] = creds.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
		secret.Type = corev1.SecretTypeDockerConfigJson
		secret.Data = map[string][]byte{
			corev1.DockerConfigJsonKey: []byte(dockerConfigJSON),
		}
		return nil
	})
	return err
}

// ensureHarborPullSecret creates or updates the imagePullSecret in a workload namespace
// with Harbor pull-only robot credentials.
func (r *TenantReconciler) ensureHarborPullSecret(ctx context.Context, namespace, tenantName string, creds *harbor.RobotCredentials) error {
	dockerConfigJSON := buildDockerConfigJSON(r.HarborRegistryHost, creds.Username, creds.Secret)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: k.SecretHarborPullCredentials, Namespace: namespace},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, secret, func() error {
		secret.Labels = map[string]string{
			k.LabelTenant:    tenantName,
			k.LabelManagedBy: k.ManagedByTenantController,
		}
		if secret.Annotations == nil {
			secret.Annotations = make(map[string]string)
		}
		secret.Annotations[k.AnnotationTokenExpiresAt] = creds.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
		secret.Type = corev1.SecretTypeDockerConfigJson
		secret.Data = map[string][]byte{
			corev1.DockerConfigJsonKey: []byte(dockerConfigJSON),
		}
		return nil
	})
	return err
}

// buildDockerConfigJSON builds a Docker config.json string for registry authentication.
func buildDockerConfigJSON(registryHost, username, password string) string {
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	// Use explicit quoting to avoid JSON injection from special characters in credentials.
	return fmt.Sprintf(`{"auths":{%s:{"username":%s,"password":%s,"auth":%s}}}`,
		quote(registryHost), quote(username), quote(password), quote(auth))
}

// quote wraps a string in JSON-safe double quotes.
func quote(s string) string {
	// Use strings.NewReplacer for JSON string escaping of the most critical characters.
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(s) + `"`
}

// harborQuotaForPlan returns the Harbor storage quota in bytes for a given plan tier.
func harborQuotaForPlan(plan loopzerov1.PlanTier) int64 {
	const gb = 1024 * 1024 * 1024
	switch plan {
	case loopzerov1.PlanStarter:
		return 5 * gb
	case loopzerov1.PlanGrowth:
		return 50 * gb
	case loopzerov1.PlanEnterprise:
		return -1 // unlimited
	default:
		// Conservative default: same as starter.
		return 5 * gb
	}
}

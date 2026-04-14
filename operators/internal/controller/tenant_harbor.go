// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
// Self-healing: if a K8s Secret was deleted but the robot still has valid TTL,
// the robot is force-recreated to obtain a new secret. This prevents a 22-hour
// blind window where builds fail because the secret is missing.
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

	buildRobotName := "build-" + tenant.Name

	// Self-healing: if the K8s Secret is missing, force-recreate the robot to get a new secret.
	if !r.secretExists(ctx, r.BuildClient, buildNs, k.SecretHarborRegistryConfig) {
		slog.Info("harbor build secret missing, forcing robot recreation",
			"tenant", tenant.Name, "namespace", buildNs, "secret", k.SecretHarborRegistryConfig)
		if delErr := r.HarborClient.DeleteRobotAccount(ctx, tenant.Name, buildRobotName); delErr != nil {
			slog.Warn("harbor build robot delete failed (will attempt recreate anyway)",
				"tenant", tenant.Name, "robot", buildRobotName, "error", delErr)
		}
	}

	// Build robot — push+pull access for BuildKit.
	buildCreds, err := r.HarborClient.EnsureRobotAccount(ctx, tenant.Name, harbor.RobotAccountOpts{
		Name:       buildRobotName,
		TTL:        k.HarborRobotAccountTTL,
		PushAccess: true,
	})
	if err != nil {
		return fmt.Errorf("harbor build robot for %s: %w", tenant.Name, err)
	}

	// Write the secret when we have a fresh secret (non-empty).
	// Harbor does not return the secret for existing robots that are still valid.
	if buildCreds.Secret != "" {
		if err := r.ensureHarborRegistrySecret(ctx, buildNs, tenant.Name, buildCreds); err != nil {
			return fmt.Errorf("harbor build registry secret for %s: %w", tenant.Name, err)
		}
		slog.Info("harbor build robot credentials stored",
			"tenant", tenant.Name, "namespace", buildNs, "robot", buildCreds.Username)
	}

	pullRobotName := "pull-" + tenant.Name

	// Self-healing for pull secrets: check each workload namespace.
	pullSecretMissing := false
	for _, env := range tenant.Spec.Environments {
		envNs, nsErr := naming.EnvironmentNamespace(tenant.Name, env.Name)
		if nsErr != nil {
			return fmt.Errorf("invalid namespace for env %s: %w", env.Name, nsErr)
		}
		if !r.secretExists(ctx, r.RuntimeClient, envNs, k.SecretHarborPullCredentials) {
			slog.Info("harbor pull secret missing, forcing robot recreation",
				"tenant", tenant.Name, "namespace", envNs, "secret", k.SecretHarborPullCredentials)
			pullSecretMissing = true
			break
		}
	}
	if pullSecretMissing {
		if delErr := r.HarborClient.DeleteRobotAccount(ctx, tenant.Name, pullRobotName); delErr != nil {
			slog.Warn("harbor pull robot delete failed (will attempt recreate anyway)",
				"tenant", tenant.Name, "robot", pullRobotName, "error", delErr)
		}
	}

	// Pull robot — read-only access for workload pods.
	pullCreds, err := r.HarborClient.EnsureRobotAccount(ctx, tenant.Name, harbor.RobotAccountOpts{
		Name:       pullRobotName,
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

// secretExists checks if a K8s Secret exists in the given namespace using the specified client.
func (r *TenantReconciler) secretExists(ctx context.Context, cl client.Client, namespace, name string) bool {
	var secret corev1.Secret
	err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret)
	if err != nil {
		if errors.IsNotFound(err) {
			return false
		}
		// On transient errors, assume the secret exists to avoid unnecessary churn.
		slog.Warn("harbor secret check failed, assuming exists", "namespace", namespace, "name", name, "error", err)
		return true
	}
	return true
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

// harborDockerConfig represents a Docker config.json for Harbor registry authentication.
// This is distinct from dockerConfigJSON in tenant_credentials.go because Harbor
// requires username+password fields in addition to the base64 auth token.
type harborDockerConfig struct {
	Auths map[string]harborDockerAuth `json:"auths"`
}

// harborDockerAuth holds Harbor registry authentication with full credentials.
type harborDockerAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Auth     string `json:"auth"`
}

// buildDockerConfigJSON builds a Docker config.json string for registry authentication.
// Uses json.Marshal for correct escaping of all JSON special characters.
func buildDockerConfigJSON(registryHost, username, password string) string {
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	cfg := harborDockerConfig{
		Auths: map[string]harborDockerAuth{
			registryHost: {
				Username: username,
				Password: password,
				Auth:     auth,
			},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		// This should never happen with string fields, but fail loud if it does.
		panic(fmt.Sprintf("harbor: marshal docker config: %v", err))
	}
	return string(data)
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

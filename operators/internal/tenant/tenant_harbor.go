// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

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

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	k "github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/harbor"
	"github.com/getloopscale/loopzero/operators/internal/metrics"
	"github.com/getloopscale/loopzero/operators/internal/planentitlements"
	"github.com/getloopscale/loopzero/operators/internal/reconcileloop"
	"github.com/getloopscale/loopzero/pkg/naming"
)

// ensureHarborProject creates a Harbor project for the tenant if it doesn't exist.
// Sets condition HarborProjectReady on the Tenant CR.
// No-op when HarborClient is nil (backward compatibility with DOCR mode).
func (r *TenantReconciler) ensureHarborProject(ctx context.Context, tenant *loopzerov1.Tenant) error {
	if r.HarborClient == nil {
		return nil
	}

	ctx, span := reconcileloop.StartChildSpan(ctx, "tenant.harbor.ensure")
	defer span.End()

	quota := planentitlements.For(tenant.Spec.Plan).HarborQuotaBytes

	if err := r.HarborClient.EnsureProject(ctx, tenant.Name, harbor.ProjectOpts{
		StorageQuota: quota,
		Immutable:    true,
	}); err != nil {
		reconcileloop.RecordError(span, err)
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

	// Self-healing: force-recreate the build robot whenever the K8s Secret
	// is missing OR exists but does not yet contain a Harbor auth entry for
	// `HarborRegistryHost`. The second case appears post-CHG-002b host-scoped
	// merge: the DOCR writer creates the Secret with only `registry.digitalocean.com`,
	// then this function would otherwise skip rotation (because the Secret
	// "exists") and Harbor wouldn't return the robot password (because the
	// robot already exists with valid TTL), leaving the merged Secret
	// permanently DOCR-only. Detecting the missing host entry forces a fresh
	// secret on next EnsureRobotAccount call.
	needsBuildRotation := !r.secretHasHostAuth(ctx, r.BuildClient, buildNs, k.SecretHarborRegistryConfig, r.HarborRegistryHost)
	if needsBuildRotation {
		slog.Info("harbor build secret missing or host entry absent, forcing robot recreation",
			"tenant", tenant.Name, "namespace", buildNs, "secret", k.SecretHarborRegistryConfig, "host", r.HarborRegistryHost)
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

	runtimeClient, err := r.runtimeClient(ctx, tenant)
	if err != nil {
		return err
	}

	// Self-healing for pull secrets: check each workload namespace.
	pullSecretMissing := false
	for _, env := range tenant.Spec.Environments {
		envNs, nsErr := naming.Namespace(tenant.Name, env.Project, env.Name)
		if nsErr != nil {
			return fmt.Errorf("invalid namespace for env %s: %w", env.Name, nsErr)
		}
		if !r.secretExists(ctx, runtimeClient, envNs, k.SecretHarborPullCredentials) {
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
	//
	// CHG-002b 2026-04-28: the workload namespace is created by the
	// EnvironmentController, and the Environment CRs are emitted by a later
	// step of this very reconcile, so on the first pass the namespace does
	// not exist yet. That NotFound is tolerated only while the Environment
	// has not reported the namespace as provisioned; once it has, NotFound
	// means a wrong cluster or a deleted namespace and fails the reconcile.
	if pullCreds.Secret != "" {
		ensured := 0
		for _, env := range tenant.Spec.Environments {
			envNs, nsErr := naming.Namespace(tenant.Name, env.Project, env.Name)
			if nsErr != nil {
				return fmt.Errorf("invalid namespace for env %s: %w", env.Name, nsErr)
			}
			err := r.ensureHarborPullSecret(ctx, runtimeClient, envNs, tenant.Name, pullCreds)
			if err == nil {
				ensured++
				continue
			}
			if !errors.IsNotFound(err) {
				return fmt.Errorf("harbor pull secret for %s/%s: %w", tenant.Name, env.Name, err)
			}
			pending, pendErr := r.workloadNamespacePending(ctx, tenant, env)
			if pendErr != nil {
				return pendErr
			}
			if !pending {
				return fmt.Errorf("harbor pull secret for %s/%s: namespace %s is reported provisioned but not found in cell %q: %w",
					tenant.Name, env.Name, envNs, tenant.Spec.CellID, err)
			}
			slog.Info("harbor pull secret deferred — workload namespace not provisioned yet",
				"tenant", tenant.Name, "namespace", envNs)
			metrics.ReconcileErrors.WithLabelValues("tenant", "pull_secret_namespace_not_ready").Inc()
		}
		slog.Info("harbor pull robot credentials stored",
			"tenant", tenant.Name, "ensured", ensured, "total", len(tenant.Spec.Environments), "robot", pullCreds.Username)
	}

	return nil
}

// workloadNamespacePending reports whether env's workload namespace is still
// being provisioned: its Environment CR does not exist yet, or has not reported
// NamespaceReady=True.
func (r *TenantReconciler) workloadNamespacePending(ctx context.Context, tenant *loopzerov1.Tenant, env loopzerov1.TenantEnvironmentEntry) (bool, error) {
	name, err := naming.EnvironmentCRName(tenant.Name, env.Project, env.Name)
	if err != nil {
		return false, fmt.Errorf("environment CR name for %s/%s: %w", env.Project, env.Name, err)
	}
	var envCR loopzerov1.Environment
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: tenant.Namespace}, &envCR); err != nil {
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("read environment %s: %w", name, err)
	}
	return !conditions.IsConditionTrue(envCR.Status.Conditions, conditions.TypeEnvNamespaceReady), nil
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

// secretHasHostAuth returns true when `<namespace>/<name>` exists and its
// `config.json` data contains an `auths` entry for `host`. Returns false on
// missing Secret, parse failure, or absent host entry — all of which warrant
// a robot rotation. On non-NotFound transient errors the function returns
// true (skip rotation, avoid retry storms).
func (r *TenantReconciler) secretHasHostAuth(ctx context.Context, cl client.Client, namespace, name, host string) bool {
	var secret corev1.Secret
	err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret)
	if err != nil {
		if errors.IsNotFound(err) {
			return false
		}
		slog.Warn("harbor host-auth check failed, assuming present", "namespace", namespace, "name", name, "error", err)
		return true
	}
	if secret.Data == nil {
		return false
	}
	cfg, parseErr := parseDockerConfigJSON(secret.Data[k.KeyDockerConfigJSON])
	if parseErr != nil {
		return false
	}
	_, ok := cfg.Auths[host]
	return ok
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

// ensureHarborRegistrySecret upserts the Harbor auth entry into the unified
// `buildkit-registry-config` Secret while preserving any DOCR auth that may
// already be there. The Secret is always written as `Opaque` with key
// `config.json` (the path BuildKit, crane, and cosign mount under
// `/home/user/.docker/config.json` and `$DOCKER_CONFIG/config.json`).
//
// CHG-002b 2026-04-28 — root cause: previously, `ensureBuildkitRegistrySecret`
// (DOCR) and `ensureHarborRegistrySecret` (Harbor) were two writers that fully
// overwrote the same Secret on every reconcile, oscillating its content
// between DOCR-only and Harbor-only auths and changing its Type. The
// per-pipeline-step result was non-deterministic: cosign sometimes got DOCR,
// mirror-harbor sometimes got Harbor, and frequently one of them got 401.
// The fix is host-scoped merge: each writer only edits its own host entry.
func (r *TenantReconciler) ensureHarborRegistrySecret(ctx context.Context, namespace, tenantName string, creds *harbor.RobotCredentials) error {
	if r.HarborRegistryHost == "" {
		return fmt.Errorf("ensureHarborRegistrySecret: HarborRegistryHost is empty")
	}

	desiredType := corev1.SecretTypeOpaque

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: k.SecretHarborRegistryConfig, Namespace: namespace},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, secret, func() error {
		// Type migration: if the Secret exists with a legacy type, delete
		// inside the mutator and return sentinel error for requeue.
		if secret.Type != "" && secret.Type != desiredType {
			slog.Info("registry secret type mismatch, deleting for recreate",
				"namespace", namespace, "old_type", secret.Type, "new_type", desiredType)
			if delErr := r.BuildClient.Delete(ctx, secret); delErr != nil && !errors.IsNotFound(delErr) {
				return fmt.Errorf("delete legacy registry secret: %w", delErr)
			}
			return errSecretTypeMismatch
		}

		secret.Labels = map[string]string{
			k.LabelTenant:    tenantName,
			k.LabelComponent: k.ComponentBuild,
			k.LabelManagedBy: k.ManagedByTenantController,
		}
		if secret.Annotations == nil {
			secret.Annotations = make(map[string]string)
		}
		// Use a Harbor-scoped annotation so we do NOT clobber the DOCR
		// token expiry that ensureBuildkitRegistrySecret depends on for its
		// cache check (isRegistryTokenFresh). The two annotations are
		// independent: each writer owns its own.
		secret.Annotations[k.AnnotationHarborTokenExpiresAt] = creds.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
		secret.Type = desiredType
		// Merge inside the mutator using whatever the live Secret holds
		// right now — controllerutil.CreateOrUpdate may re-invoke this
		// closure with refreshed state. Re-running the merge keeps it
		// idempotent and tolerant of concurrent writes from the DOCR side.
		live := []byte(nil)
		if secret.Data != nil {
			live = secret.Data[k.KeyDockerConfigJSON]
		}
		nextJSON, mergeErr := mergeAuthsByHost(live, map[string]mergedAuthEntry{
			r.HarborRegistryHost: authEntryFromBasic(creds.Username, creds.Secret),
		})
		if mergeErr != nil {
			return mergeErr
		}
		secret.Data = map[string][]byte{
			k.KeyDockerConfigJSON: nextJSON,
		}
		return nil
	})
	return err
}

// ensureHarborPullSecret creates or updates the imagePullSecret in a workload namespace
// with Harbor pull-only robot credentials.
func (r *TenantReconciler) ensureHarborPullSecret(ctx context.Context, runtimeClient client.Client, namespace, tenantName string, creds *harbor.RobotCredentials) error {
	dockerConfigJSON, err := buildDockerConfigJSON(r.HarborRegistryHost, creds.Username, creds.Secret)
	if err != nil {
		return fmt.Errorf("ensureHarborPullSecret: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: k.SecretHarborPullCredentials, Namespace: namespace},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, runtimeClient, secret, func() error {
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
//
// CODE-002 / T2.3: previously panicked on json.Marshal failure (which amplified
// TC-1 toxic combination — panic in reconcile hot path with cluster-wide RBAC
// during recovery). Now returns the marshal error so the reconciler can requeue
// with backoff instead of crashing the goroutine.
func buildDockerConfigJSON(registryHost, username, password string) (string, error) {
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
		return "", fmt.Errorf("harbor: marshal docker config: %w", err)
	}
	return string(data), nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	k "github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/reconcileloop"
	"github.com/getloopscale/loopzero/operators/internal/tenantpolicy"
	"github.com/getloopscale/loopzero/pkg/naming"
)

// errSecretTypeMismatch is returned when a Secret exists with the wrong type
// (e.g., DockerConfigJson instead of Opaque). The Secret is deleted inside the
// mutator and this error triggers a requeue so the next reconcile creates it
// fresh. This eliminates the TOCTOU race of doing Delete + CreateOrUpdate as
// two separate operations.
var errSecretTypeMismatch = fmt.Errorf("secret type mismatch: deleted for recreation on next reconcile")

// reconcileBuildNamespace orchestrates the build-{tenantID} namespace provisioning.
// Each step is idempotent via CreateOrUpdate. Steps are extracted into focused functions.
func (r *TenantReconciler) reconcileBuildNamespace(ctx context.Context, tenant *loopzerov1.Tenant) (err error) {
	ctx, span := reconcileloop.StartChildSpan(ctx, "tenant.build.namespace")
	defer span.End()
	defer func() {
		if err != nil {
			reconcileloop.RecordError(span, err)
		}
	}()
	buildNs, err := naming.BuildNamespace(tenant.Name)
	if err != nil {
		return fmt.Errorf("invalid build namespace name for tenant %s: %w", tenant.Name, err)
	}

	// COMP-006: Initialize QuotaEnforced to False so that if the function fails partway,
	// the condition is present (as False) providing diagnostic info. It will be set to True
	// only after all steps succeed below.
	conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeQuotaEnforced,
		metav1.ConditionFalse, "Pending", "quota enforcement in progress", tenant.Generation)

	steps := []struct {
		name string
		fn   func() error
	}{
		{"namespace", func() error { return r.ensureBuildNs(ctx, buildNs, tenant.Name) }},
		{"resourcequota", func() error { return r.ensureBuildQuota(ctx, buildNs, tenant) }},
		{"networkpolicy", func() error { return r.reconcileBuildNetworkPolicy(ctx, buildNs) }},
		{"limitrange", func() error { return r.ensureBuildLimitRange(ctx, buildNs) }},
		{"serviceaccount", func() error { return r.ensureBuildServiceAccount(ctx, buildNs, tenant.Name) }},
		{"storage-credentials", func() error { return r.ensureBuildStorageCredentials(ctx, buildNs, tenant.Name) }},
		{"registry-credentials", func() error { return r.ensureBuildRegistryCredentials(ctx, buildNs, tenant.Name) }},
		{"executor-rbac", func() error { return r.ensureBuildExecutorRBAC(ctx, buildNs) }},
		{"submitter-rbac", func() error { return r.ensureBuildSubmitterRBAC(ctx, buildNs) }},
		{"minio-per-tenant", func() error { return r.ensureBuildMinIOPerTenant(ctx, buildNs, tenant.Name) }},
	}

	for _, step := range steps {
		if err := step.fn(); err != nil {
			return fmt.Errorf("build %s %s: %w", step.name, buildNs, err)
		}
	}

	conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeBuildNamespaceReady,
		metav1.ConditionTrue, "BuildNamespaceReady", "build namespace provisioned", tenant.Generation)
	conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeQuotaEnforced,
		metav1.ConditionTrue, "AllQuotasEnforced", "all resource quotas applied", tenant.Generation)

	return nil
}

// ensureBuildNs creates or updates the build namespace (NO ownerReference — cross-cluster limitation).
func (r *TenantReconciler) ensureBuildNs(ctx context.Context, buildNs, tenantName string) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: buildNs}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, ns, func() error {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels[k.LabelTenant] = tenantName
		ns.Labels[k.LabelComponent] = k.ComponentBuild
		ns.Labels[k.LabelManagedBy] = k.ManagedByTenantController
		return nil
	})
	return err
}

// ensureBuildQuota creates or updates the ResourceQuota for the build namespace.
func (r *TenantReconciler) ensureBuildQuota(ctx context.Context, buildNs string, tenant *loopzerov1.Tenant) error {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: k.QuotaBuild, Namespace: buildNs},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, quota, func() error {
		quota.Spec.Hard = corev1.ResourceList{
			corev1.ResourceRequestsCPU:    resource.MustParse(k.BuildQuotaRequestsCPU),
			corev1.ResourceRequestsMemory: resource.MustParse(k.BuildQuotaRequestsMemory),
			corev1.ResourceLimitsCPU:      resource.MustParse(k.BuildQuotaLimitsCPU),
			corev1.ResourceLimitsMemory:   resource.MustParse(k.BuildQuotaLimitsMemory),
		}
		if tenant.Spec.Limits.MaxBuildConcurrency > 0 {
			quota.Spec.Hard[corev1.ResourceName("count/workflows.argoproj.io")] =
				resource.MustParse(fmt.Sprintf("%d", tenant.Spec.Limits.MaxBuildConcurrency))
		}
		return nil
	})
	return err
}

// ensureBuildLimitRange creates or updates the LimitRange for default resource requests/limits.
// Phase 3 v40: delegates to tenantpolicy.ApplyContainerLimitRange so the build
// LimitRange shares the canonical creation path with tenant + preview
// LimitRanges. Only the constants differ (build vs tenant defaults).
func (r *TenantReconciler) ensureBuildLimitRange(ctx context.Context, buildNs string) error {
	return tenantpolicy.ApplyContainerLimitRange(ctx, r.BuildClient, buildNs, k.LimitRangeBuild, "build limitrange",
		tenantpolicy.LimitRangeContainerDefaults{
			CPU:           k.BuildLimitDefaultCPU,
			Memory:        k.BuildLimitDefaultMemory,
			RequestCPU:    k.BuildLimitDefaultRequestCPU,
			RequestMemory: k.BuildLimitDefaultRequestMemory,
		})
}

// ensureBuildServiceAccount creates or updates the ServiceAccount for Argo Workflow pods.
func (r *TenantReconciler) ensureBuildServiceAccount(ctx context.Context, buildNs, tenantName string) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: k.SABuildWorkflow, Namespace: buildNs},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, sa, func() error {
		if sa.Labels == nil {
			sa.Labels = make(map[string]string)
		}
		sa.Labels[k.LabelTenant] = tenantName
		sa.Labels[k.LabelManagedBy] = k.ManagedByTenantController
		return nil
	})
	return err
}

// ensureBuildStorageCredentials creates global MinIO/Spaces credentials when per-tenant
// provisioner is disabled. CWTs reference Secret "loopzero-minio-credentials" via envFrom.
func (r *TenantReconciler) ensureBuildStorageCredentials(ctx context.Context, buildNs, tenantName string) error {
	if r.MinIOProvisioner != nil || r.Credentials.MinIOEndpoint == "" {
		return nil
	}
	storageData := r.Credentials.StorageSecretData()
	if storageData == nil {
		return fmt.Errorf("SPACES_ACCESS_KEY_ID and SPACES_SECRET_ACCESS_KEY are required when MinIOProvisioner is disabled")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: k.SecretMinIOCredentials, Namespace: buildNs},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, secret, func() error {
		secret.Labels = map[string]string{
			k.LabelTenant:    tenantName,
			k.LabelComponent: k.ComponentBuild,
			k.LabelManagedBy: k.ManagedByTenantController,
		}
		secret.Type = corev1.SecretTypeOpaque
		secret.StringData = storageData
		return nil
	})
	return err
}

// ensureBuildRegistryCredentials creates BuildKit registry config Secret.
// DO Container Registry requires credentials generated via the DO API.
// For local k3d development, RegistryDockerConfig is set at init time.
//
// Caching (ARCH_OP_005): For DO deployments, the token is cached in the Secret's
// AnnotationTokenExpiresAt annotation. The DO API is only called when the token
// has less than DORegistryTokenRefreshBuffer (1 day) of remaining validity,
// reducing ~20+ API calls/min to ~1 call per 6 days per tenant.
func (r *TenantReconciler) ensureBuildRegistryCredentials(ctx context.Context, buildNs, tenantName string) error {
	// For DO deployments, check if the cached token is still fresh.
	if r.Credentials.DOToken != "" {
		if r.isRegistryTokenFresh(ctx, buildNs) {
			return nil
		}
	}

	dockerConfig, err := r.Credentials.FetchRegistryDockerConfig(ctx)
	if err != nil {
		return fmt.Errorf("fetch registry credentials: %w", err)
	}
	if dockerConfig == "" {
		return nil
	}
	return r.ensureBuildkitRegistrySecret(ctx, buildNs, tenantName, dockerConfig)
}

// isRegistryTokenFresh checks if the existing buildkit-registry-config Secret
// has a cached DOCR token with more than DORegistryTokenRefreshBuffer remaining
// validity AND that the Secret currently contains a DOCR auth entry. The
// auth-presence check is necessary because, post-CHG-002b host-scoped merge,
// the Secret may exist (with valid annotation) but still be missing the DOCR
// `auths` entry — for instance if the Harbor writer raced ahead and this
// function previously short-circuited the DOCR write. Returning false in that
// case forces a re-merge and restores the missing entry.
func (r *TenantReconciler) isRegistryTokenFresh(ctx context.Context, namespace string) bool {
	var secret corev1.Secret
	key := types.NamespacedName{Name: k.SecretBuildKitRegistryConfig, Namespace: namespace}
	if err := r.BuildClient.Get(ctx, key, &secret); err != nil {
		if errors.IsNotFound(err) {
			return false
		}
		// On transient errors, fall through to refresh — the DO API call is the safe default.
		return false
	}

	expiresAtStr, ok := secret.Annotations[k.AnnotationTokenExpiresAt]
	if !ok {
		return false
	}

	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		return false
	}

	if time.Until(expiresAt) <= k.DORegistryTokenRefreshBuffer {
		return false
	}

	// Auth-presence check: the cached token annotation only matters if the
	// DOCR auth is actually in the Secret. A Secret with only Harbor auths
	// must not short-circuit DOCR's writer.
	if secret.Data == nil {
		return false
	}
	cfg, parseErr := parseDockerConfigJSON(secret.Data[k.KeyDockerConfigJSON])
	if parseErr != nil || len(cfg.Auths) == 0 {
		return false
	}
	for host := range cfg.Auths {
		// DOCR returns auths under registry.digitalocean.com[/...]; we accept
		// any host containing "digitalocean" rather than hard-coding because
		// the API may return host variants (region-suffixed paths in DOCR v2).
		if containsDOCRHost(host) {
			return true
		}
	}
	return false
}

// containsDOCRHost is a tiny helper to keep the host check in one place.
// Kept package-private and minimal to avoid a heavier abstraction layer.
// Requires exact match or a sub-path (e.g., registry.digitalocean.com/v2)
// to prevent prefix-spoofing (registry.digitalocean.com.evil.com).
func containsDOCRHost(host string) bool {
	return host == "registry.digitalocean.com" ||
		strings.HasPrefix(host, "registry.digitalocean.com/")
}

// ensureBuildExecutorRBAC creates the Role and RoleBinding for Argo Workflow executor pods.
func (r *TenantReconciler) ensureBuildExecutorRBAC(ctx context.Context, buildNs string) error {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: k.RoleArgoExecutor, Namespace: buildNs},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, role, func() error {
		role.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{"argoproj.io"},
				Resources: []string{"workflowtaskresults"},
				Verbs:     []string{"create", "patch", "get", "list"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "pods/log"},
				Verbs:     []string{"get", "watch", "patch", "list"},
			},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("role: %w", err)
	}

	if err := tenantpolicy.ApplySingleSARoleBinding(ctx, r.BuildClient, buildNs, k.RoleBindingArgoExecutor, k.RoleArgoExecutor, k.SABuildWorkflow); err != nil {
		return fmt.Errorf("rolebinding: %w", err)
	}
	return nil
}

// ensureBuildSubmitterRBAC creates Role and RoleBinding allowing the workflow-submitter SA
// (which lives in the "argo" namespace) to create/manage workflows in this tenant's build namespace.
// Without this, the API cannot submit build workflows to build-{tenant}.
func (r *TenantReconciler) ensureBuildSubmitterRBAC(ctx context.Context, buildNs string) error {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: k.RoleWorkflowSubmitter, Namespace: buildNs},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, role, func() error {
		role.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{"argoproj.io"},
				Resources: []string{"workflows"},
				Verbs:     []string{"create", "get", "list", "watch", "patch", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "pods/log"},
				Verbs:     []string{"get", "list", "watch"},
			},
		}
		return nil
	}); err != nil {
		return fmt.Errorf("role: %w", err)
	}

	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: k.RoleBindingWorkflowSubmitter, Namespace: buildNs},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, binding, func() error {
		binding.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     k.RoleWorkflowSubmitter,
		}
		binding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      k.SAWorkflowSubmitter,
				Namespace: k.NSArgoWorkflows,
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("rolebinding: %w", err)
	}
	return nil
}

// ensureBuildMinIOPerTenant provisions per-tenant MinIO credentials scoped to tenant prefix.
// When MinIOProvisioner is nil, this is a no-op (graceful degradation).
func (r *TenantReconciler) ensureBuildMinIOPerTenant(ctx context.Context, buildNs, tenantName string) error {
	if r.MinIOProvisioner == nil {
		return nil
	}
	return r.reconcileBuildMinIOCredentials(ctx, buildNs, tenantName)
}

// reconcileBuildMinIOCredentials provisions per-tenant MinIO credentials and stores
// them as a K8s Secret in the build namespace.
// When the MinIO user already exists, secretKey is empty and the Secret is only
// created (not overwritten), preserving existing credentials for in-flight builds.
func (r *TenantReconciler) reconcileBuildMinIOCredentials(ctx context.Context, namespace, tenantID string) error {
	accessKey, secretKey, err := r.MinIOProvisioner.EnsureTenantCredentials(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("provision minio credentials: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: k.SecretMinIOCredentials, Namespace: namespace},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.BuildClient, secret, func() error {
		secret.Labels = map[string]string{
			k.LabelTenant:    tenantID,
			k.LabelComponent: k.ComponentBuild,
			k.LabelManagedBy: k.ManagedByTenantController,
		}
		secret.Type = corev1.SecretTypeOpaque
		// Only overwrite credentials when we have a new secret key (first creation).
		// When secretKey is empty, the user already existed — preserve existing credentials.
		if secretKey != "" {
			secret.StringData = map[string]string{
				k.KeyAWSAccessKeyID:     accessKey,
				k.KeyAWSSecretAccessKey: secretKey,
				k.KeyMinIOEndpoint:      r.Credentials.MinIOEndpoint,
			}
		} else if secret.CreationTimestamp.IsZero() {
			// Auto-recover: delete and reprovision MinIO user to regenerate credentials.
			// This handles the case where the K8s Secret was deleted but the MinIO user still exists.
			if err := r.MinIOProvisioner.DeleteTenantCredentials(ctx, tenantID); err != nil {
				return fmt.Errorf("force-rotate: delete minio user %s: %w", accessKey, err)
			}
			newAccessKey, newSecretKey, reproErr := r.MinIOProvisioner.EnsureTenantCredentials(ctx, tenantID)
			if reproErr != nil {
				return fmt.Errorf("force-rotate: reprovision minio for %s: %w", tenantID, reproErr)
			}
			secret.Labels = map[string]string{
				k.LabelTenant:    tenantID,
				k.LabelComponent: k.ComponentBuild,
				k.LabelManagedBy: k.ManagedByTenantController,
			}
			secret.Type = corev1.SecretTypeOpaque
			secret.StringData = map[string]string{
				k.KeyAWSAccessKeyID:     newAccessKey,
				k.KeyAWSSecretAccessKey: newSecretKey,
				k.KeyMinIOEndpoint:      r.Credentials.MinIOEndpoint,
			}
		}
		// Existing secret with existing user — labels updated, data preserved.
		return nil
	})
	return err
}

// ensureBuildkitRegistrySecret upserts the DOCR auth entries from `dockerConfig`
// into the unified `buildkit-registry-config` Secret while preserving any
// auths from other writers (notably Harbor — see `ensureHarborRegistrySecret`).
//
// CHG-002b 2026-04-28 — root cause: this used to fully overwrite the Secret
// with DOCR-only auths, racing the Harbor writer and producing 401s on
// `mirror-harbor-*` steps. Host-scoped merge eliminates the oscillation.
//
// `dockerConfig` is expected to be a Docker config.json document for DOCR.
// All `auths` entries from it are upserted onto the merged Secret; this
// covers cases where the DO API returns multiple registry hosts.
func (r *TenantReconciler) ensureBuildkitRegistrySecret(ctx context.Context, namespace, tenantName, dockerConfig string) error {
	docrCfg, err := parseDockerConfigJSON([]byte(dockerConfig))
	if err != nil {
		return fmt.Errorf("ensureBuildkitRegistrySecret: parse dockerConfig: %w", err)
	}
	docrAuths := map[string]mergedAuthEntry{}
	for host, entry := range docrCfg.Auths {
		docrAuths[host] = entry
	}

	desiredType := corev1.SecretTypeOpaque

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: k.SecretBuildKitRegistryConfig, Namespace: namespace},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.BuildClient, secret, func() error {
		// Type migration: if the Secret exists with a legacy type, delete
		// inside the mutator and return sentinel error for requeue.
		if secret.Type != "" && secret.Type != desiredType {
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
		if r.Credentials.DOToken != "" {
			if secret.Annotations == nil {
				secret.Annotations = make(map[string]string)
			}
			expiresAt := time.Now().Add(time.Duration(k.DORegistryTokenExpirySeconds) * time.Second)
			secret.Annotations[k.AnnotationTokenExpiresAt] = expiresAt.Format(time.RFC3339)
		}
		secret.Type = desiredType

		live := []byte(nil)
		if secret.Data != nil {
			live = secret.Data[k.KeyDockerConfigJSON]
		}
		nextJSON, mergeErr := mergeAuthsByHost(live, docrAuths)
		if mergeErr != nil {
			return mergeErr
		}
		secret.Data = map[string][]byte{
			k.KeyDockerConfigJSON: nextJSON,
		}
		// StringData and Data are mutually exclusive at apply time — clear
		// any leftover StringData from prior controller versions.
		secret.StringData = nil
		return nil
	})
	return err
}

// reconcileBuildNetworkPolicy creates restrictive egress policies in build namespaces.
// Prevents source code exfiltration via malicious Dockerfiles (spec §17.1).
// RISK-007: Port-443 egress is restricted to RegistryCIDRs when configured.
func (r *TenantReconciler) reconcileBuildNetworkPolicy(ctx context.Context, namespace string) error {
	dnsPort := intstr.FromInt32(k.PortDNS)
	minioPort := intstr.FromInt32(k.PortMinIO)
	registryPort := intstr.FromInt32(k.PortRegistryDev)
	registryTLSPort := intstr.FromInt32(k.PortHTTPS)
	k8sAPIPort := intstr.FromInt32(k.PortKubeAPI)
	protoUDP := corev1.ProtocolUDP
	protoTCP := corev1.ProtocolTCP

	// RISK-007: Build registry egress rule — restrict port-443 to known CIDRs
	// when configured, preventing exfiltration to arbitrary HTTPS endpoints.
	registryRule := networkingv1.NetworkPolicyEgressRule{
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protoTCP, Port: &registryPort},
			{Protocol: &protoTCP, Port: &registryTLSPort},
		},
	}
	if len(r.RegistryCIDRs) > 0 {
		peers := make([]networkingv1.NetworkPolicyPeer, 0, len(r.RegistryCIDRs))
		for _, cidr := range r.RegistryCIDRs {
			peers = append(peers, networkingv1.NetworkPolicyPeer{
				IPBlock: &networkingv1.IPBlock{CIDR: cidr},
			})
		}
		registryRule.To = peers
	}

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: k.NetworkPolicyBuildEgress, Namespace: namespace},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, np, func() error {
		np.Labels = map[string]string{
			k.LabelComponent: k.ComponentBuild,
			k.LabelManagedBy: k.ManagedByTenantController,
		}
		// T2.9 / COMP-005: MinIO and K8s API egress rules now carry destination
		// selectors. Previously they had only Ports{}, which let build pods
		// reach any external host on those ports — a confirmed exfiltration
		// vector. The DNS rule (kube-system) and registry rule (RegistryCIDRs
		// IPBlock) remain unchanged.
		minioEgress := networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"kubernetes.io/metadata.name": k.NamespacePlatformSystem},
				},
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "minio"},
				},
			}},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &minioPort},
			},
		}
		k8sAPIEgress := networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"kubernetes.io/metadata.name": "default"},
				},
			}},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &protoTCP, Port: &k8sAPIPort},
			},
		}

		np.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					To: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"},
						},
					}},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &protoUDP, Port: &dnsPort},
						{Protocol: &protoTCP, Port: &dnsPort},
					},
				},
				minioEgress,
				registryRule,
				k8sAPIEgress,
			},
		}
		return nil
	})
	return err
}

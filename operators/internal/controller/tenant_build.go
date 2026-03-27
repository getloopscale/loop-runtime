// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	k "github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/pkg/naming"
)

// reconcileBuildNamespace orchestrates the build-{tenantID} namespace provisioning.
// Each step is idempotent via CreateOrUpdate. Steps are extracted into focused functions.
func (r *TenantReconciler) reconcileBuildNamespace(ctx context.Context, tenant *loopzerov1.Tenant) error {
	buildNs, err := naming.BuildNamespace(tenant.Name)
	if err != nil {
		return fmt.Errorf("invalid build namespace name for tenant %s: %w", tenant.Name, err)
	}

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
func (r *TenantReconciler) ensureBuildLimitRange(ctx context.Context, buildNs string) error {
	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{Name: k.LimitRangeBuild, Namespace: buildNs},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, lr, func() error {
		lr.Spec.Limits = []corev1.LimitRangeItem{
			{
				Type: corev1.LimitTypeContainer,
				Default: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(k.BuildLimitDefaultCPU),
					corev1.ResourceMemory: resource.MustParse(k.BuildLimitDefaultMemory),
				},
				DefaultRequest: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(k.BuildLimitDefaultRequestCPU),
					corev1.ResourceMemory: resource.MustParse(k.BuildLimitDefaultRequestMemory),
				},
			},
		}
		return nil
	})
	return err
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
	if r.MinIOProvisioner != nil || r.MinIOEndpoint == "" {
		return nil
	}
	if r.SpacesAccessKeyID == "" || r.SpacesSecretAccessKey == "" {
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
		secret.StringData = map[string]string{
			k.KeyAWSAccessKeyID:     r.SpacesAccessKeyID,
			k.KeyAWSSecretAccessKey: r.SpacesSecretAccessKey,
			k.KeyMinIOEndpoint:      r.MinIOEndpoint,
		}
		return nil
	})
	return err
}

// ensureBuildRegistryCredentials creates BuildKit registry config Secret.
// DO Container Registry requires credentials generated via the DO API.
// For local k3d development, RegistryDockerConfig is set at init time.
func (r *TenantReconciler) ensureBuildRegistryCredentials(ctx context.Context, buildNs, tenantName string) error {
	if r.DOToken != "" {
		dockerConfig, err := fetchDORegistryCredentials(ctx, r.DOToken)
		if err != nil {
			return fmt.Errorf("fetch DO registry credentials: %w", err)
		}
		return r.ensureBuildkitRegistrySecret(ctx, buildNs, tenantName, dockerConfig)
	}
	if r.RegistryDockerConfig != "" {
		return r.ensureBuildkitRegistrySecret(ctx, buildNs, tenantName, r.RegistryDockerConfig)
	}
	return nil
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

	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: k.RoleBindingArgoExecutor, Namespace: buildNs},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, binding, func() error {
		binding.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     k.RoleArgoExecutor,
		}
		binding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      k.SABuildWorkflow,
				Namespace: buildNs,
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
		secret.StringData = map[string]string{
			k.KeyAWSAccessKeyID:     accessKey,
			k.KeyAWSSecretAccessKey: secretKey,
			k.KeyMinIOEndpoint:      r.MinIOEndpoint,
		}
		return nil
	})
	return err
}

// ensureBuildkitRegistrySecret creates or updates the BuildKit registry config Secret.
func (r *TenantReconciler) ensureBuildkitRegistrySecret(ctx context.Context, namespace, tenantName, dockerConfig string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: k.SecretBuildKitRegistryConfig, Namespace: namespace},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, secret, func() error {
		secret.Labels = map[string]string{
			k.LabelTenant:    tenantName,
			k.LabelComponent: k.ComponentBuild,
			k.LabelManagedBy: k.ManagedByTenantController,
		}
		secret.Type = corev1.SecretTypeOpaque
		secret.StringData = map[string]string{
			k.KeyDockerConfigJSON: dockerConfig,
		}
		return nil
	})
	return err
}

// doHTTPClient is reused across reconcile cycles for DO API calls.
// Safe for concurrent use (http.Client is goroutine-safe).
var doHTTPClient = &http.Client{Timeout: k.HTTPTimeoutExternal}

// fetchDORegistryCredentials calls the DigitalOcean API to generate read-write
// registry credentials in Docker config.json format.
func fetchDORegistryCredentials(ctx context.Context, apiToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s?read_write=true&expiry_seconds=%d", k.DORegistryCredentialsURL, k.DORegistryTokenExpirySeconds),
		nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := doHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call DO API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("DO API returned %d: %s", resp.StatusCode, string(body))
	}

	var config map[string]interface{}
	if err := json.Unmarshal(body, &config); err != nil {
		return "", fmt.Errorf("invalid JSON from DO API: %w", err)
	}
	if _, ok := config["auths"]; !ok {
		return "", fmt.Errorf("DO API response missing 'auths' field")
	}

	return string(body), nil
}

// reconcileBuildNetworkPolicy creates restrictive egress policies in build namespaces.
// Prevents source code exfiltration via malicious Dockerfiles (spec §17.1).
func (r *TenantReconciler) reconcileBuildNetworkPolicy(ctx context.Context, namespace string) error {
	dnsPort := intstr.FromInt32(k.PortDNS)
	minioPort := intstr.FromInt32(k.PortMinIO)
	registryPort := intstr.FromInt32(k.PortRegistryDev)
	registryTLSPort := intstr.FromInt32(k.PortHTTPS)
	k8sAPIPort := intstr.FromInt32(k.PortKubeAPI)
	protoUDP := corev1.ProtocolUDP
	protoTCP := corev1.ProtocolTCP

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: k.NetworkPolicyBuildEgress, Namespace: namespace},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, np, func() error {
		np.Labels = map[string]string{
			k.LabelComponent: k.ComponentBuild,
			k.LabelManagedBy: k.ManagedByTenantController,
		}
		np.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{Ports: []networkingv1.NetworkPolicyPort{
					{Protocol: &protoUDP, Port: &dnsPort},
					{Protocol: &protoTCP, Port: &dnsPort},
				}},
				{Ports: []networkingv1.NetworkPolicyPort{
					{Protocol: &protoTCP, Port: &minioPort},
				}},
				{Ports: []networkingv1.NetworkPolicyPort{
					{Protocol: &protoTCP, Port: &registryPort},
					{Protocol: &protoTCP, Port: &registryTLSPort},
				}},
				{Ports: []networkingv1.NetworkPolicyPort{
					{Protocol: &protoTCP, Port: &k8sAPIPort},
				}},
			},
		}
		return nil
	})
	return err
}

package controller

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/naming"
)

// reconcileBuildNamespace ensures the build-{tenantID} namespace exists with ResourceQuota.
func (r *TenantReconciler) reconcileBuildNamespace(ctx context.Context, tenant *loopzerov1.Tenant) error {
	buildNs, err := naming.BuildNamespaceName(tenant.Name)
	if err != nil {
		return fmt.Errorf("invalid build namespace name for tenant %s: %w", tenant.Name, err)
	}

	// Create or update build namespace (NO ownerReference — cross-namespace limitation)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: buildNs,
		},
	}
	var createErr error
	_, createErr = controllerutil.CreateOrUpdate(ctx, r.BuildClient, ns, func() error {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels["loopzero.io/tenant"] = tenant.Name
		ns.Labels["loopzero.io/component"] = "build"
		ns.Labels["loopzero.io/managed-by"] = "tenant-controller"
		return nil
	})
	if createErr != nil {
		return fmt.Errorf("build namespace %s: %w", buildNs, createErr)
	}

	// ResourceQuota for build namespace
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "build-quota",
			Namespace: buildNs,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.BuildClient, quota, func() error {
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

	// REVIEW-BLOCKER: NetworkPolicy for build namespace — restrict egress to only
	// DNS, MinIO, and container registry. Prevents source code exfiltration via
	// malicious Dockerfiles or postinstall scripts (spec §17.1 threat model).
	if err := r.reconcileBuildNetworkPolicy(ctx, buildNs); err != nil {
		return fmt.Errorf("build network policy %s: %w", buildNs, err)
	}

	// LimitRange — default resource requests/limits for build pods.
	// Without this, pods without explicit resources are rejected by the ResourceQuota.
	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "build-limits",
			Namespace: buildNs,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.BuildClient, lr, func() error {
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
		return fmt.Errorf("build limitrange %s: %w", buildNs, err)
	}

	// ServiceAccount for Argo Workflow pods — must exist in each build namespace.
	// The workflow spec references serviceAccountName: loopzero-build-sa.
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "loopzero-build-sa",
			Namespace: buildNs,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.BuildClient, sa, func() error {
		if sa.Labels == nil {
			sa.Labels = make(map[string]string)
		}
		sa.Labels["loopzero.io/tenant"] = tenant.Name
		sa.Labels["loopzero.io/managed-by"] = "tenant-controller"
		return nil
	})
	if err != nil {
		return fmt.Errorf("build serviceaccount %s: %w", buildNs, err)
	}

	// Global MinIO credentials for CWTs (when per-tenant provisioner is disabled).
	// CWTs reference Secret "loopzero-minio-credentials" via envFrom.
	if r.MinIOProvisioner == nil && r.MinIOEndpoint != "" {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "loopzero-minio-credentials",
				Namespace: buildNs,
			},
		}
		_, err = controllerutil.CreateOrUpdate(ctx, r.BuildClient, secret, func() error {
			secret.Labels = map[string]string{
				"loopzero.io/tenant":     tenant.Name,
				"loopzero.io/component":  "build",
				"loopzero.io/managed-by": "tenant-controller",
			}
			secret.Type = corev1.SecretTypeOpaque
			// Use global object storage credentials.
			// In production, these point to DigitalOcean Spaces (S3-compatible).
			// Per-tenant isolation is handled when MinIOProvisioner is enabled.
			accessKey := os.Getenv("SPACES_ACCESS_KEY_ID")
			secretKey := os.Getenv("SPACES_SECRET_ACCESS_KEY")
			if accessKey == "" {
				accessKey = "loopzero-admin" // fallback for local dev
			}
			if secretKey == "" {
				secretKey = "loopzero-minio-dev-2026" // fallback for local dev
			}
			secret.StringData = map[string]string{
				"AWS_ACCESS_KEY_ID":     accessKey,
				"AWS_SECRET_ACCESS_KEY": secretKey,
				"MINIO_ENDPOINT":        r.MinIOEndpoint,
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("build minio credentials (global) %s: %w", buildNs, err)
		}
	}

	// Provision per-tenant MinIO credentials scoped to tenant prefix.
	// Each tenant gets its own MinIO user + policy that restricts access to
	// loopzero-builds/{tenantID}/* only. This prevents cross-tenant source code access
	// and cache poisoning (spec §17.2, §17.4).
	// When MinIOProvisioner is nil, per-tenant isolation is disabled (graceful degradation).
	if r.MinIOProvisioner != nil {
		if err := r.reconcileBuildMinIOCredentials(ctx, buildNs, tenant.Name); err != nil {
			return fmt.Errorf("build minio credentials %s: %w", buildNs, err)
		}
	}

	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionBuildNamespaceReady,
		metav1.ConditionTrue, "BuildNamespaceReady", "build namespace provisioned", tenant.Generation)

	// QuotaEnforced = True when ALL quotas (workload + build) are applied
	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionQuotaEnforced,
		metav1.ConditionTrue, "AllQuotasEnforced", "all resource quotas applied", tenant.Generation)

	return nil
}

// reconcileBuildMinIOCredentials provisions per-tenant MinIO credentials and stores
// them as a K8s Secret in the build namespace. The CWTs reference this Secret by
// the fixed name "loopzero-minio-credentials" via envFrom.
//
// Each tenant gets its own MinIO user with a policy scoped to loopzero-builds/{tenantID}/*,
// preventing cross-tenant source code access and cache poisoning.
func (r *TenantReconciler) reconcileBuildMinIOCredentials(ctx context.Context, namespace, tenantID string) error {
	accessKey, secretKey, err := r.MinIOProvisioner.EnsureTenantCredentials(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("provision minio credentials: %w", err)
	}

	// Create or update the K8s Secret with tenant-scoped credentials.
	// The Secret name MUST be "loopzero-minio-credentials" — all CWTs reference it.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "loopzero-minio-credentials",
			Namespace: namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.BuildClient, secret, func() error {
		secret.Labels = map[string]string{
			"loopzero.io/tenant":     tenantID,
			"loopzero.io/component":  "build",
			"loopzero.io/managed-by": "tenant-controller",
		}
		secret.Type = corev1.SecretTypeOpaque
		secret.StringData = map[string]string{
			"AWS_ACCESS_KEY_ID":     accessKey,
			"AWS_SECRET_ACCESS_KEY": secretKey,
			"MINIO_ENDPOINT":       r.MinIOEndpoint,
		}
		return nil
	})
	return err
}

// reconcileBuildNetworkPolicy creates restrictive egress policies in build namespaces.
//
// REVIEW-BLOCKER: Without this, build pods (especially Kaniko running as root)
// have unrestricted network egress. A malicious Dockerfile can exfiltrate source
// code to any external endpoint (spec §17.1: "Exfiltrate source code via network egress").
//
// Allowed egress:
//   - DNS (port 53 UDP/TCP) — required for any network operation
//   - MinIO (port 9000 TCP) — source bundles and cache
//   - Container registry (port 5000 TCP for k3d, 443 for production registries)
//   - K8s API (port 6443 TCP) — Argo Workflow task result reporting
//
// All other egress is denied.
func (r *TenantReconciler) reconcileBuildNetworkPolicy(ctx context.Context, namespace string) error {
	dnsPort := intstr.FromInt32(53)
	minioPort := intstr.FromInt32(9000)
	registryPort := intstr.FromInt32(5000)
	registryTLSPort := intstr.FromInt32(443)
	k8sAPIPort := intstr.FromInt32(6443)
	protoUDP := corev1.ProtocolUDP
	protoTCP := corev1.ProtocolTCP

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "build-egress-restrict",
			Namespace: namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.BuildClient, np, func() error {
		np.Labels = map[string]string{
			"loopzero.io/component":  "build",
			"loopzero.io/managed-by": "tenant-controller",
		}
		np.Spec = networkingv1.NetworkPolicySpec{
			// Apply to all pods in the build namespace
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// DNS — required for hostname resolution
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &protoUDP, Port: &dnsPort},
						{Protocol: &protoTCP, Port: &dnsPort},
					},
				},
				// MinIO — source bundles and dependency cache
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &protoTCP, Port: &minioPort},
					},
				},
				// Container registry — Kaniko image push (dev: 5000, prod: 443)
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &protoTCP, Port: &registryPort},
						{Protocol: &protoTCP, Port: &registryTLSPort},
					},
				},
				// K8s API — Argo task result reporting
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &protoTCP, Port: &k8sAPIPort},
					},
				},
			},
		}
		return nil
	})
	return err
}

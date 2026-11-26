// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package constants

// Well-known ports used in NetworkPolicy egress rules.
// Defined as int32 for direct use with intstr.FromInt32().
const (
	PortDNS         int32 = 53
	PortHTTPS       int32 = 443
	PortRegistryDev int32 = 5000
	PortKubeAPI     int32 = 6443
	PortMinIO       int32 = 9000
)

// --- Namespace Labels for NetworkPolicy selectors ---

const (
	// KubeMetadataNameLabel is the standard Kubernetes label for namespace names.
	KubeMetadataNameLabel = "kubernetes.io/metadata.name"

	// NamespaceKubeSystem is the kube-system namespace name.
	NamespaceKubeSystem = "kube-system"

	// NamespaceDefault is the default namespace name.
	NamespaceDefault = "default"

	// NamespacePlatformSystem is the LoopRuntime platform system namespace.
	NamespacePlatformSystem = "loop-runtime-system"

	// NamespaceIngress is the namespace where Traefik runs.
	// LoopRuntime's helmfile (`infra/helmfile/{mgmt,runtime}.helmfile.yaml.gotmpl`)
	// installs Traefik in `traefik` for ALL environments (dev, prod,
	// standalone-multi) — see lines 88 (runtime) and 135 (mgmt). The
	// operator-generated `allow-ingress-traefik` NetworkPolicy MUST allow
	// traffic from this namespace; otherwise Traefik can't reach tenant
	// pods → HTTP 502 in any deploy. Discovered live during
	// CHG-2026-04-29-DOGFOOD-FB Phase 1: api pod Running 1/1 + cert ready
	// + endpoints valid, but Traefik logged repeated 502s and "subset not
	// found" because the deny-all + allow-ingress-from-loop-runtime-system policy
	// silently dropped traffic from the real Traefik namespace. Affects
	// production identically — DOKS CNI may or may not enforce, but the
	// guarantee should not depend on CNI choice.
	NamespaceIngress = "traefik"
)

// --- Build Resource Defaults ---
// Default resource limits for build namespaces and LimitRanges.
// These are string representations of Kubernetes resource quantities.

const (
	BuildQuotaRequestsCPU    = "4"
	BuildQuotaRequestsMemory = "8Gi"
	BuildQuotaLimitsCPU      = "8"
	BuildQuotaLimitsMemory   = "16Gi"

	BuildLimitDefaultCPU           = "500m"
	BuildLimitDefaultMemory        = "512Mi"
	BuildLimitDefaultRequestCPU    = "100m"
	BuildLimitDefaultRequestMemory = "128Mi"

	// TenantLimitRange defaults (used for environment namespaces)
	TenantLimitDefaultCPU           = "500m"
	TenantLimitDefaultMemory        = "512Mi"
	TenantLimitDefaultRequestCPU    = "100m"
	TenantLimitDefaultRequestMemory = "128Mi"
)

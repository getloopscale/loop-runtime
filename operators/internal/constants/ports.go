// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

	// NamespacePlatformSystem is the LoopZero platform system namespace.
	NamespacePlatformSystem = "platform-system"
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

// --- DO Registry Credential Expiry ---

const (
	// DORegistryTokenExpirySeconds is the TTL for DigitalOcean registry tokens (7 days).
	DORegistryTokenExpirySeconds = 604800
)

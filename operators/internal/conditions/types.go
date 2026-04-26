// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package conditions provides condition helpers for all LoopZero controllers.
// Conditions follow the Kubernetes API conventions for status conditions
// (metav1.Condition) with typed constants for condition types and reasons.
//
// This package lives in operators/internal/ because it depends on K8s types
// (metav1.Condition) and should not be exposed via pkg/.
package conditions

// --- Tenant conditions ---
const (
	TypeGitRepoReady        = "GitRepoReady"
	TypeQuotaEnforced       = "QuotaEnforced"
	TypeBuildNamespaceReady = "BuildNamespaceReady"
	TypePriorityClassReady  = "PriorityClassReady"
	TypeArgoCDAppReady      = "ArgoCDAppReady"
	TypeHarborProjectReady  = "HarborProjectReady"
)

// --- PlatformBootstrap conditions ---
const (
	TypeGiteaReady        = "GiteaReady"
	TypeArgoCDReady       = "ArgoCDReady"
	TypeDatabaseSeeded    = "DatabaseSeeded"
	TypeBootstrapComplete = "BootstrapComplete"
)

// --- GitRepositoryBinding conditions ---
const (
	TypeCredentialsResolved  = "CredentialsResolved"
	TypeRepositoryRegistered = "RepositoryRegistered"
	TypeRepositoryReachable  = "RepositoryReachable"
	TypeConsumerHealthy      = "ConsumerHealthy"
)

// --- Environment conditions ---
const (
	TypeEnvNamespaceReady     = "NamespaceReady"
	TypeEnvRBACReady          = "RBACReady"
	TypeEnvNetworkPolicyReady = "NetworkPolicyReady"
	TypeEnvQuotaReady         = "QuotaReady"
	TypePullSecretReady       = "PullSecretReady" // Informational only — does not block Active transition
)

// --- Application conditions ---
const (
	TypeServiceResolved    = "ServiceResolved"
	TypeIngressReady       = "IngressReady"
	TypeCertificateIssued  = "CertificateIssued"
	TypeRouteReachable     = "RouteReachable"
	TypeApplicationHealthy = "ApplicationHealthy"
)

// --- ClusterCredential conditions ---
const (
	TypeSecretNotFound = "SecretNotFound"
	TypeConnected      = "Connected"
)

// --- Preview conditions ---
// T4.1 / ARCH-003: previously freeform "NamespaceReady" string in
// preview_controller.go:119,127. Defined as a separate constant from
// TypeEnvNamespaceReady because Preview's NamespaceReady has different
// semantics (per-PR namespace lifecycle).
//
// Sprint 5 / T1.2 (preview-environments-plan): adds the workload-readiness
// conditions the controller will set as it reconciles namespace + workload
// deployment. The deny-all + allow-ingress + allow-egress NetworkPolicy
// triplet, the ResourceQuota, and the LimitRange each get their own
// condition for finer-grained operator UX (`kubectl describe pe` shows
// exactly which resource failed).
const (
	TypePreviewNamespaceReady       = "NamespaceReady"
	TypePreviewNetworkPolicyReady   = "NetworkPolicyReady"
	TypePreviewResourceQuotaReady   = "ResourceQuotaReady"
	TypePreviewLimitRangeReady      = "LimitRangeReady"
	TypePreviewBuildSubmitted       = "BuildSubmitted"
	TypePreviewBuildCompleted       = "BuildCompleted"
	TypePreviewWorkloadHealthy      = "WorkloadHealthy"
	TypePreviewApplicationDeleted   = "ApplicationDeleted"
	TypePreviewNamespaceDeleted     = "NamespaceDeleted"
)

// --- Tenant deletion conditions ---
// T4.1 / ARCH-003: previously freeform "EnvironmentCleanup" string in
// tenant_deletion.go:88. Used by Phase 1 of the deletion lifecycle to
// surface per-environment Delete failures.
const (
	TypeTenantEnvironmentCleanup = "EnvironmentCleanup"
)

// Reason constants are enumerated, not freeform. All condition reasons
// must use one of these constants to ensure consistency across controllers.
const (
	ReasonReconciling = "Reconciling"
	ReasonReady       = "Ready"
	ReasonFailed      = "Failed"
	ReasonNotFound    = "NotFound"
	ReasonUnreachable = "Unreachable"
	ReasonWaiting     = "Waiting"
)

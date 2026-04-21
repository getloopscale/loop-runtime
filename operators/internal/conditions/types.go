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

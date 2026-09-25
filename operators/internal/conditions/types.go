// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package conditions provides condition helpers for all LoopZero controllers.
// Conditions follow the Kubernetes API conventions for status conditions
// (metav1.Condition) with typed constants for condition types and reasons.
//
// M6 (extract-crd-types-contract-module): the CR-status condition TYPE
// vocabulary + the generic SetCondition/GetCondition helpers were relocated to
// the versioned CRD contract module `operators/api/v1` (they are part of the CR
// status contract). This package re-exports them as aliases/delegations so its
// 40 importers are unaffected, and keeps the operator-internal reason vocabulary
// + kstatus summary/retry helpers here.
package conditions

import apiv1 "github.com/getloopscale/loopzero/operators/api/v1"

// --- CR-status condition TYPE vocabulary (re-exported from operators/api/v1) ---
// The canonical definitions live in operators/api/v1/conditions.go (M6). These
// aliases keep `conditions.TypeX` call sites compiling unchanged.
const (
	// kstatus universal summary conditions (ADR-0025 D3)
	TypeReady       = apiv1.TypeReady
	TypeReconciling = apiv1.TypeReconciling
	TypeStalled     = apiv1.TypeStalled

	// Tenant conditions
	TypeGitRepoReady        = apiv1.TypeGitRepoReady
	TypeQuotaEnforced       = apiv1.TypeQuotaEnforced
	TypeBuildNamespaceReady = apiv1.TypeBuildNamespaceReady
	TypePriorityClassReady  = apiv1.TypePriorityClassReady
	TypeArgoCDAppReady      = apiv1.TypeArgoCDAppReady
	TypeHarborProjectReady  = apiv1.TypeHarborProjectReady
	TypeTransitKeyAvailable = apiv1.TypeTransitKeyAvailable

	// PlatformBootstrap conditions
	TypeGiteaReady        = apiv1.TypeGiteaReady
	TypeArgoCDReady       = apiv1.TypeArgoCDReady
	TypeDatabaseSeeded    = apiv1.TypeDatabaseSeeded
	TypeBootstrapComplete = apiv1.TypeBootstrapComplete

	// GitRepositoryBinding conditions
	TypeCredentialsResolved  = apiv1.TypeCredentialsResolved
	TypeRepositoryRegistered = apiv1.TypeRepositoryRegistered
	TypeRepositoryReachable  = apiv1.TypeRepositoryReachable
	TypeConsumerHealthy      = apiv1.TypeConsumerHealthy

	// Environment conditions
	TypeEnvNamespaceReady     = apiv1.TypeEnvNamespaceReady
	TypeEnvRBACReady          = apiv1.TypeEnvRBACReady
	TypeEnvNetworkPolicyReady = apiv1.TypeEnvNetworkPolicyReady
	TypeEnvQuotaReady         = apiv1.TypeEnvQuotaReady
	TypePullSecretReady       = apiv1.TypePullSecretReady

	// Application conditions
	TypeServiceResolved    = apiv1.TypeServiceResolved
	TypeIngressReady       = apiv1.TypeIngressReady
	TypeCertificateIssued  = apiv1.TypeCertificateIssued
	TypeRouteReachable     = apiv1.TypeRouteReachable
	TypeApplicationHealthy = apiv1.TypeApplicationHealthy

	// ClusterCredential conditions
	TypeSecretNotFound     = apiv1.TypeSecretNotFound
	TypeConnected          = apiv1.TypeConnected
	TypeArgoCDSecretSynced = apiv1.TypeArgoCDSecretSynced

	// Preview conditions
	TypePreviewNamespaceReady      = apiv1.TypePreviewNamespaceReady
	TypePreviewNetworkPolicyReady  = apiv1.TypePreviewNetworkPolicyReady
	TypePreviewResourceQuotaReady  = apiv1.TypePreviewResourceQuotaReady
	TypePreviewLimitRangeReady     = apiv1.TypePreviewLimitRangeReady
	TypePreviewServiceAccountReady = apiv1.TypePreviewServiceAccountReady
	TypePreviewBuildSubmitted      = apiv1.TypePreviewBuildSubmitted
	TypePreviewBuildCompleted      = apiv1.TypePreviewBuildCompleted
	TypePreviewWorkloadHealthy     = apiv1.TypePreviewWorkloadHealthy
	TypePreviewApplicationDeleted  = apiv1.TypePreviewApplicationDeleted
	TypePreviewNamespaceDeleted    = apiv1.TypePreviewNamespaceDeleted

	// Tenant deletion conditions
	TypeTenantEnvironmentCleanup = apiv1.TypeTenantEnvironmentCleanup
)

// --- Tenant condition reasons (T3.3 EC-15) ---
const (
	ReasonVaultQuotaExceeded    = "VaultQuotaExceeded"    // Vault rejected key create due to quota
	ReasonVaultUnreachable      = "VaultUnreachable"      // network/timeout/5xx
	ReasonTransitKeyProvisioned = "TransitKeyProvisioned" // success state
)

// Reason constants are enumerated, not freeform. All condition reasons must use
// one of these constants to ensure consistency across controllers.
//
// kstatus convention (ADR-0025): the helper SetReadinessSummary classifies each
// detail condition's Reason as either {Reconciling, Progressing, Waiting} (→
// summary Reconciling=True) or {Failed, Stalled} (→ summary Stalled=True;
// Stalled wins over Reconciling).
const (
	ReasonReconciling = "Reconciling"
	ReasonReady       = "Ready"
	ReasonFailed      = "Failed"
	ReasonNotFound    = "NotFound"
	ReasonUnreachable = "Unreachable"
	ReasonWaiting     = "Waiting"
	// ReasonDegraded marks a partial-failure state where the resource is
	// observable but not fully healthy.
	ReasonDegraded = "Degraded"
	// ReasonProgressing — generic in-progress reason for kstatus consumers.
	ReasonProgressing = "Progressing"
	// ReasonStalled — generic stalled reason; kstatus.Compute treats it like
	// ReasonFailed when classifying the summary.
	ReasonStalled = "Stalled"
)

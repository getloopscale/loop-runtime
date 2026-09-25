// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package v1

// This file is the home of the CRD status-condition CONTRACT: the condition
// type-name vocabulary every LoopZero CRD exposes in its `status.conditions`, plus
// the generic metav1 condition helpers. It lives in the `operators/api` module
// (the versioned CRD contract artifact, M6) and imports only k8s.io/apimachinery
// so the module remains a clean base (acyclic-graph gate enforces this).
//
// The operator-side `operators/internal/conditions` package re-exports these
// symbols (const aliases + delegating helpers) so its 40 importers are
// unaffected by the move. Reason vocabulary + kstatus summary helpers stay
// operator-internal (they are reconcile concerns, not the CR contract).

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// --- kstatus universal summary conditions (ADR-0025 D3) ---
const (
	TypeReady       = "Ready"
	TypeReconciling = "Reconciling"
	TypeStalled     = "Stalled"
)

// --- Tenant conditions ---
const (
	TypeGitRepoReady        = "GitRepoReady"
	TypeQuotaEnforced       = "QuotaEnforced"
	TypeBuildNamespaceReady = "BuildNamespaceReady"
	TypePriorityClassReady  = "PriorityClassReady"
	TypeArgoCDAppReady      = "ArgoCDAppReady"
	TypeHarborProjectReady  = "HarborProjectReady"
	TypeTransitKeyAvailable = "TransitKeyAvailable"
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
	// TypeArgoCDSecretSynced reports whether the ArgoCD cluster Secret mirrors
	// the credential. Only set when Spec.Scope.ArgoCD is true.
	TypeArgoCDSecretSynced = "ArgoCDSecretSynced"
)

// --- Preview conditions ---
// Preview's NamespaceReady is a distinct constant from TypeEnvNamespaceReady
// because the per-PR namespace lifecycle has different semantics (same string
// value is intentional).
const (
	TypePreviewNamespaceReady      = "NamespaceReady"
	TypePreviewNetworkPolicyReady  = "NetworkPolicyReady"
	TypePreviewResourceQuotaReady  = "ResourceQuotaReady"
	TypePreviewLimitRangeReady     = "LimitRangeReady"
	TypePreviewServiceAccountReady = "ServiceAccountReady"
	TypePreviewBuildSubmitted      = "BuildSubmitted"
	TypePreviewBuildCompleted      = "BuildCompleted"
	TypePreviewWorkloadHealthy     = "WorkloadHealthy"
	TypePreviewApplicationDeleted  = "ApplicationDeleted"
	TypePreviewNamespaceDeleted    = "NamespaceDeleted"
)

// --- Tenant deletion conditions ---
const (
	TypeTenantEnvironmentCleanup = "EnvironmentCleanup"
)

// SetCondition sets or updates a condition on a slice of conditions.
// ObservedGeneration is mandatory and tracks which generation of the resource
// the condition relates to. If the condition already exists with the same
// status, only reason, message, and observedGeneration are updated (preserving
// LastTransitionTime). If the status changes, LastTransitionTime is updated.
func SetCondition(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string, observedGeneration int64) {
	now := metav1.Now()

	for i, c := range *conditions {
		if c.Type == conditionType {
			if c.Status == status {
				// Status unchanged: update reason/message/generation but preserve transition time.
				(*conditions)[i].Reason = reason
				(*conditions)[i].Message = message
				(*conditions)[i].ObservedGeneration = observedGeneration
				return
			}
			// Status changed: update everything including transition time.
			(*conditions)[i] = metav1.Condition{
				Type:               conditionType,
				Status:             status,
				LastTransitionTime: now,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: observedGeneration,
			}
			return
		}
	}

	// Condition not found: append new.
	*conditions = append(*conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: observedGeneration,
	})
}

// GetCondition returns the condition with the given type, or nil if not found.
func GetCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

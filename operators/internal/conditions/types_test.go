// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package conditions

import "testing"

// TestKstatusTypes_AreCorrectStrings — ADR-0025 D3 / plan T2.2: pin the
// kpt-convention condition type strings so a typo doesn't silently break
// kstatus.Compute consumers (Argo Rollouts, Flux Helm releases, ArgoCD
// health checks, kubectl wait).
func TestKstatusTypes_AreCorrectStrings(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"TypeReady", TypeReady, "Ready"},
		{"TypeReconciling", TypeReconciling, "Reconciling"},
		{"TypeStalled", TypeStalled, "Stalled"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q (kpt CRD Status Convention is normative)", tc.name, tc.got, tc.want)
		}
	}
}

// TestKstatusReasons_AreCorrectStrings — pin the reason strings used by
// SetReadinessSummary's classifier (Reconciling/Progressing/Waiting →
// summary Reconciling=True; Failed/Stalled → summary Stalled=True).
func TestKstatusReasons_AreCorrectStrings(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"ReasonReady", ReasonReady, "Ready"},
		{"ReasonReconciling", ReasonReconciling, "Reconciling"},
		{"ReasonProgressing", ReasonProgressing, "Progressing"},
		{"ReasonWaiting", ReasonWaiting, "Waiting"},
		{"ReasonFailed", ReasonFailed, "Failed"},
		{"ReasonStalled", ReasonStalled, "Stalled"},
		{"ReasonDegraded", ReasonDegraded, "Degraded"},
		{"ReasonNotFound", ReasonNotFound, "NotFound"},
		{"ReasonUnreachable", ReasonUnreachable, "Unreachable"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// TestKstatusTypes_NoCollisionWithDetailTypes — guard against accidentally
// reusing one of the summary type names ("Ready"/"Reconciling"/"Stalled")
// as a detail condition type. SetReadinessSummary is the LAST status
// mutation per ADR-0025; if a detail type collides, the helper would
// overwrite it.
func TestKstatusTypes_NoCollisionWithDetailTypes(t *testing.T) {
	summary := map[string]bool{
		TypeReady: true, TypeReconciling: true, TypeStalled: true,
	}
	detail := []string{
		TypeGitRepoReady, TypeQuotaEnforced, TypeBuildNamespaceReady,
		TypePriorityClassReady, TypeArgoCDAppReady,
		TypeGiteaReady, TypeArgoCDReady, TypeDatabaseSeeded, TypeBootstrapComplete,
		TypeCredentialsResolved, TypeRepositoryRegistered, TypeRepositoryReachable, TypeConsumerHealthy,
		TypeEnvNamespaceReady, TypeEnvRBACReady, TypeEnvNetworkPolicyReady, TypeEnvQuotaReady, TypePullSecretReady,
		TypeServiceResolved, TypeIngressReady, TypeCertificateIssued, TypeRouteReachable, TypeApplicationHealthy,
		TypeSecretNotFound, TypeConnected,
		TypePreviewNamespaceReady, TypePreviewNetworkPolicyReady, TypePreviewResourceQuotaReady,
		TypePreviewLimitRangeReady, TypePreviewServiceAccountReady,
		TypePreviewBuildSubmitted, TypePreviewBuildCompleted,
		TypePreviewWorkloadHealthy, TypePreviewApplicationDeleted, TypePreviewNamespaceDeleted,
		TypeTenantEnvironmentCleanup,
	}
	for _, d := range detail {
		if summary[d] {
			t.Errorf("detail condition type %q collides with kstatus summary — rename one of them", d)
		}
	}
}

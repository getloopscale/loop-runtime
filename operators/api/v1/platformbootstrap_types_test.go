// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPlatformBootstrapPhase_Values(t *testing.T) {
	phases := []PlatformBootstrapPhase{
		PlatformBootstrapPhasePending,
		PlatformBootstrapPhaseConfiguringGitea,
		PlatformBootstrapPhaseConfiguringArgoCD,
		PlatformBootstrapPhaseWaitingForAPI,
		PlatformBootstrapPhaseComplete,
		PlatformBootstrapPhaseFailed,
	}
	expected := []string{"Pending", "ConfiguringGitea", "ConfiguringArgoCD", "WaitingForAPI", "BootstrapComplete", "Failed"}

	for i, p := range phases {
		if string(p) != expected[i] {
			t.Errorf("expected phase %q, got %q", expected[i], p)
		}
	}
}

func TestPlatformBootstrapPhase_AreDistinct(t *testing.T) {
	phases := []PlatformBootstrapPhase{
		PlatformBootstrapPhasePending,
		PlatformBootstrapPhaseConfiguringGitea,
		PlatformBootstrapPhaseConfiguringArgoCD,
		PlatformBootstrapPhaseWaitingForAPI,
		PlatformBootstrapPhaseComplete,
		PlatformBootstrapPhaseFailed,
	}
	seen := make(map[PlatformBootstrapPhase]bool)
	for _, p := range phases {
		if seen[p] {
			t.Errorf("duplicate platform bootstrap phase: %q", p)
		}
		seen[p] = true
	}
}

func TestPlatformBootstrap_SpecGitea(t *testing.T) {
	pbs := PlatformBootstrap{
		Spec: PlatformBootstrapSpec{
			Gitea: GiteaBootstrapSpec{
				AdminSecretRef: SecretKeyRef{Name: "gitea-admin-secret"},
				Organizations:  []string{"loopruntime", "acme"},
				TokenName:      "platform-bootstrap-token",
				TokenScopes:    []string{"write:organization", "write:admin"},
				InternalURL:    "http://gitea-http.loopruntime-system.svc.cluster.local:3000",
			},
		},
	}

	if pbs.Spec.Gitea.AdminSecretRef.Name != "gitea-admin-secret" {
		t.Errorf("expected AdminSecretRef.Name %q, got %q", "gitea-admin-secret", pbs.Spec.Gitea.AdminSecretRef.Name)
	}
	if len(pbs.Spec.Gitea.Organizations) != 2 {
		t.Fatalf("expected 2 organizations, got %d", len(pbs.Spec.Gitea.Organizations))
	}
	if pbs.Spec.Gitea.Organizations[0] != "loopruntime" {
		t.Errorf("expected first org %q, got %q", "loopruntime", pbs.Spec.Gitea.Organizations[0])
	}
	if pbs.Spec.Gitea.TokenName != "platform-bootstrap-token" {
		t.Errorf("expected TokenName %q, got %q", "platform-bootstrap-token", pbs.Spec.Gitea.TokenName)
	}
	if len(pbs.Spec.Gitea.TokenScopes) != 2 {
		t.Fatalf("expected 2 token scopes, got %d", len(pbs.Spec.Gitea.TokenScopes))
	}
	if pbs.Spec.Gitea.InternalURL != "http://gitea-http.loopruntime-system.svc.cluster.local:3000" {
		t.Errorf("expected InternalURL to match, got %q", pbs.Spec.Gitea.InternalURL)
	}
}

func TestPlatformBootstrap_SpecArgoCD(t *testing.T) {
	pbs := PlatformBootstrap{
		Spec: PlatformBootstrapSpec{
			ArgoCD: ArgoCDBootstrapSpec{
				Namespace:       "argocd",
				AdminSecretName: "argocd-initial-admin-secret",
				RepoCredentials: []ArgoCDRepoCredential{
					{Name: "gitea-creds", URL: "http://gitea-http.loopruntime-system.svc.cluster.local:3000"},
				},
				InternalURL: "https://argocd-server.argocd.svc.cluster.local",
			},
		},
	}

	if pbs.Spec.ArgoCD.Namespace != "argocd" {
		t.Errorf("expected Namespace %q, got %q", "argocd", pbs.Spec.ArgoCD.Namespace)
	}
	if pbs.Spec.ArgoCD.AdminSecretName != "argocd-initial-admin-secret" {
		t.Errorf("expected AdminSecretName %q, got %q", "argocd-initial-admin-secret", pbs.Spec.ArgoCD.AdminSecretName)
	}
	if len(pbs.Spec.ArgoCD.RepoCredentials) != 1 {
		t.Fatalf("expected 1 repo credential, got %d", len(pbs.Spec.ArgoCD.RepoCredentials))
	}
	if pbs.Spec.ArgoCD.RepoCredentials[0].Name != "gitea-creds" {
		t.Errorf("expected repo credential name %q, got %q", "gitea-creds", pbs.Spec.ArgoCD.RepoCredentials[0].Name)
	}
}

func TestPlatformBootstrap_SpecAPI(t *testing.T) {
	pbs := PlatformBootstrap{
		Spec: PlatformBootstrapSpec{
			API: APIBootstrapSpec{
				BootstrapEndpoint: "http://loopruntime-api.loopruntime-system.svc.cluster.local:8080/internal/bootstrap",
				HealthEndpoint:    "http://loopruntime-api.loopruntime-system.svc.cluster.local:8080/api/v1/health",
				BootstrapSecretRef: &SecretKeyRef{
					Name: "bootstrap-secret",
				},
			},
		},
	}

	if pbs.Spec.API.BootstrapEndpoint != "http://loopruntime-api.loopruntime-system.svc.cluster.local:8080/internal/bootstrap" {
		t.Errorf("expected BootstrapEndpoint to match, got %q", pbs.Spec.API.BootstrapEndpoint)
	}
	if pbs.Spec.API.HealthEndpoint != "http://loopruntime-api.loopruntime-system.svc.cluster.local:8080/api/v1/health" {
		t.Errorf("expected HealthEndpoint to match, got %q", pbs.Spec.API.HealthEndpoint)
	}
	if pbs.Spec.API.BootstrapSecretRef == nil {
		t.Fatal("expected BootstrapSecretRef to be set")
	}
	if pbs.Spec.API.BootstrapSecretRef.Name != "bootstrap-secret" {
		t.Errorf("expected BootstrapSecretRef.Name %q, got %q", "bootstrap-secret", pbs.Spec.API.BootstrapSecretRef.Name)
	}
}

func TestPlatformBootstrap_SpecAPIWithoutSecret(t *testing.T) {
	pbs := PlatformBootstrap{
		Spec: PlatformBootstrapSpec{
			API: APIBootstrapSpec{
				BootstrapEndpoint: "http://loopruntime-api:8080/internal/bootstrap",
				HealthEndpoint:    "http://loopruntime-api:8080/api/v1/health",
			},
		},
	}

	if pbs.Spec.API.BootstrapSecretRef != nil {
		t.Error("expected BootstrapSecretRef to be nil when not set")
	}
}

func TestPlatformBootstrap_StatusFields(t *testing.T) {
	transitionTime := metav1.Now()

	pbs := PlatformBootstrap{
		Status: PlatformBootstrapStatus{
			Phase:              PlatformBootstrapPhaseConfiguringGitea,
			RetryCount:         2,
			ObservedGeneration: 1,
			LastTransitionTime: &transitionTime,
		},
	}

	if pbs.Status.Phase != PlatformBootstrapPhaseConfiguringGitea {
		t.Errorf("expected Phase %q, got %q", PlatformBootstrapPhaseConfiguringGitea, pbs.Status.Phase)
	}
	if pbs.Status.RetryCount != 2 {
		t.Errorf("expected RetryCount %d, got %d", 2, pbs.Status.RetryCount)
	}
	if pbs.Status.ObservedGeneration != 1 {
		t.Errorf("expected ObservedGeneration %d, got %d", 1, pbs.Status.ObservedGeneration)
	}
	if pbs.Status.LastTransitionTime == nil {
		t.Fatal("expected LastTransitionTime to be set")
	}
}

func TestPlatformBootstrap_SetCondition(t *testing.T) {
	pbs := PlatformBootstrap{}

	SetCondition(&pbs.Status.Conditions, TypeGiteaReady, metav1.ConditionTrue, "Ready", "Gitea is reachable", 1)
	SetCondition(&pbs.Status.Conditions, TypeArgoCDReady, metav1.ConditionFalse, "Unreachable", "waiting for ArgoCD", 1)
	SetCondition(&pbs.Status.Conditions, TypeDatabaseSeeded, metav1.ConditionFalse, "Pending", "not yet seeded", 1)

	if len(pbs.Status.Conditions) != 3 {
		t.Fatalf("expected 3 conditions, got %d", len(pbs.Status.Conditions))
	}

	c := GetCondition(pbs.Status.Conditions, TypeGiteaReady)
	if c == nil {
		t.Fatal("expected to find GiteaReady condition")
	}
	if c.Status != metav1.ConditionTrue {
		t.Errorf("expected condition status True, got %v", c.Status)
	}
}

func TestPlatformBootstrap_ConditionTypes_AreDistinct(t *testing.T) {
	types := []string{
		TypeGiteaReady,
		TypeArgoCDReady,
		TypeDatabaseSeeded,
		TypeBootstrapComplete,
	}
	seen := make(map[string]bool)
	for _, ct := range types {
		if seen[ct] {
			t.Errorf("duplicate condition type: %q", ct)
		}
		seen[ct] = true
	}
}

func TestPlatformBootstrapList_ContainsItems(t *testing.T) {
	list := PlatformBootstrapList{
		Items: []PlatformBootstrap{
			{ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-dev"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-prod"}},
		},
	}

	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list.Items))
	}
	if list.Items[0].Name != "bootstrap-dev" {
		t.Errorf("expected first item name %q, got %q", "bootstrap-dev", list.Items[0].Name)
	}
	if list.Items[1].Name != "bootstrap-prod" {
		t.Errorf("expected second item name %q, got %q", "bootstrap-prod", list.Items[1].Name)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGitRepositoryBinding_SpecFields(t *testing.T) {
	grb := GitRepositoryBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "acme-gitops-binding",
			Namespace: "loopzero-system",
		},
		Spec: GitRepositoryBindingSpec{
			RepoURL: "http://gitea-http.loopzero-system.svc.cluster.local:3000/loopzero/acme-gitops.git",
			CredentialSecretRef: CredentialSecretRef{
				Name:      "acme-git-token",
				Key:       "token",
				Namespace: "loopzero-system",
			},
			Consumer: ConsumerSpec{
				Type:      "argocd",
				Namespace: "argocd",
			},
		},
	}

	if grb.Spec.RepoURL != "http://gitea-http.loopzero-system.svc.cluster.local:3000/loopzero/acme-gitops.git" {
		t.Errorf("expected RepoURL to match, got %q", grb.Spec.RepoURL)
	}
	if grb.Spec.CredentialSecretRef.Name != "acme-git-token" {
		t.Errorf("expected CredentialSecretRef.Name %q, got %q", "acme-git-token", grb.Spec.CredentialSecretRef.Name)
	}
	if grb.Spec.CredentialSecretRef.Key != "token" {
		t.Errorf("expected CredentialSecretRef.Key %q, got %q", "token", grb.Spec.CredentialSecretRef.Key)
	}
	if grb.Spec.CredentialSecretRef.Namespace != "loopzero-system" {
		t.Errorf("expected CredentialSecretRef.Namespace %q, got %q", "loopzero-system", grb.Spec.CredentialSecretRef.Namespace)
	}
	if grb.Spec.Consumer.Type != "argocd" {
		t.Errorf("expected Consumer.Type %q, got %q", "argocd", grb.Spec.Consumer.Type)
	}
	if grb.Spec.Consumer.Namespace != "argocd" {
		t.Errorf("expected Consumer.Namespace %q, got %q", "argocd", grb.Spec.Consumer.Namespace)
	}
}

func TestConsumerSpec_TypeArgoCD(t *testing.T) {
	consumer := ConsumerSpec{
		Type:      "argocd",
		Namespace: "argocd",
	}

	if consumer.Type != "argocd" {
		t.Errorf("expected Consumer.Type %q, got %q", "argocd", consumer.Type)
	}
}

func TestCredentialSecretRef_AllFields(t *testing.T) {
	ref := CredentialSecretRef{
		Name:      "my-secret",
		Key:       "password",
		Namespace: "default",
	}

	if ref.Name != "my-secret" {
		t.Errorf("expected Name %q, got %q", "my-secret", ref.Name)
	}
	if ref.Key != "password" {
		t.Errorf("expected Key %q, got %q", "password", ref.Key)
	}
	if ref.Namespace != "default" {
		t.Errorf("expected Namespace %q, got %q", "default", ref.Namespace)
	}
}

func TestGitRepositoryBinding_StatusFields(t *testing.T) {
	grb := GitRepositoryBinding{
		Status: GitRepositoryBindingStatus{
			CredentialHash:     "abc123def456",
			ObservedGeneration: 7,
		},
	}

	if grb.Status.CredentialHash != "abc123def456" {
		t.Errorf("expected CredentialHash %q, got %q", "abc123def456", grb.Status.CredentialHash)
	}
	if grb.Status.ObservedGeneration != 7 {
		t.Errorf("expected ObservedGeneration %d, got %d", 7, grb.Status.ObservedGeneration)
	}
}

func TestGitRepositoryBinding_StatusDefaults(t *testing.T) {
	grb := GitRepositoryBinding{}

	if grb.Status.CredentialHash != "" {
		t.Errorf("expected empty CredentialHash by default, got %q", grb.Status.CredentialHash)
	}
	if grb.Status.ObservedGeneration != 0 {
		t.Errorf("expected ObservedGeneration 0 by default, got %d", grb.Status.ObservedGeneration)
	}
	if len(grb.Status.Conditions) != 0 {
		t.Errorf("expected 0 conditions by default, got %d", len(grb.Status.Conditions))
	}
}

func TestGitRepositoryBinding_SetCondition(t *testing.T) {
	grb := GitRepositoryBinding{}

	SetCondition(&grb.Status.Conditions, TypeCredentialsResolved, metav1.ConditionTrue, "Resolved", "credentials found", 1)
	SetCondition(&grb.Status.Conditions, TypeRepositoryRegistered, metav1.ConditionTrue, "Registered", "repo registered in ArgoCD", 1)

	if len(grb.Status.Conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(grb.Status.Conditions))
	}

	c := GetCondition(grb.Status.Conditions, TypeCredentialsResolved)
	if c == nil {
		t.Fatal("expected to find CredentialsResolved condition")
	}
	if c.Status != metav1.ConditionTrue {
		t.Errorf("expected condition status True, got %v", c.Status)
	}
}

func TestGitRepositoryBinding_ConditionTypes_AreDistinct(t *testing.T) {
	types := []string{
		TypeCredentialsResolved,
		TypeRepositoryRegistered,
		TypeRepositoryReachable,
		TypeConsumerHealthy,
	}
	seen := make(map[string]bool)
	for _, ct := range types {
		if seen[ct] {
			t.Errorf("duplicate condition type: %q", ct)
		}
		seen[ct] = true
	}
}

func TestGitRepositoryBindingList_ContainsItems(t *testing.T) {
	list := GitRepositoryBindingList{
		Items: []GitRepositoryBinding{
			{ObjectMeta: metav1.ObjectMeta{Name: "binding-1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "binding-2"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "binding-3"}},
		},
	}

	if len(list.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list.Items))
	}
	if list.Items[0].Name != "binding-1" {
		t.Errorf("expected first item name %q, got %q", "binding-1", list.Items[0].Name)
	}
	if list.Items[2].Name != "binding-3" {
		t.Errorf("expected third item name %q, got %q", "binding-3", list.Items[2].Name)
	}
}

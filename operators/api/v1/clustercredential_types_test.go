// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterCredential_SpecFields(t *testing.T) {
	cc := ClusterCredential{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cell-0-cluster-01",
			Namespace: "loop-runtime-system",
		},
		Spec: ClusterCredentialSpec{
			ClusterName: "workload-01",
			APIServer:   "https://10.0.0.1:6443",
			SecretRef: SecretReference{
				Name:      "workload-01-kubeconfig",
				Namespace: "loop-runtime-system",
			},
			Scope: CredentialScope{
				ArgoCD:    true,
				Operators: true,
			},
		},
	}

	if cc.Spec.ClusterName != "workload-01" {
		t.Errorf("expected ClusterName %q, got %q", "workload-01", cc.Spec.ClusterName)
	}
	if cc.Spec.APIServer != "https://10.0.0.1:6443" {
		t.Errorf("expected APIServer %q, got %q", "https://10.0.0.1:6443", cc.Spec.APIServer)
	}
	if cc.Spec.SecretRef.Name != "workload-01-kubeconfig" {
		t.Errorf("expected SecretRef.Name %q, got %q", "workload-01-kubeconfig", cc.Spec.SecretRef.Name)
	}
	if cc.Spec.SecretRef.Namespace != "loop-runtime-system" {
		t.Errorf("expected SecretRef.Namespace %q, got %q", "loop-runtime-system", cc.Spec.SecretRef.Namespace)
	}
}

func TestCredentialScope_BothEnabled(t *testing.T) {
	scope := CredentialScope{
		ArgoCD:    true,
		Operators: true,
	}

	if !scope.ArgoCD {
		t.Error("expected ArgoCD to be true")
	}
	if !scope.Operators {
		t.Error("expected Operators to be true")
	}
}

func TestCredentialScope_ArgoCDOnly(t *testing.T) {
	scope := CredentialScope{
		ArgoCD:    true,
		Operators: false,
	}

	if !scope.ArgoCD {
		t.Error("expected ArgoCD to be true")
	}
	if scope.Operators {
		t.Error("expected Operators to be false")
	}
}

func TestCredentialScope_OperatorsOnly(t *testing.T) {
	scope := CredentialScope{
		ArgoCD:    false,
		Operators: true,
	}

	if scope.ArgoCD {
		t.Error("expected ArgoCD to be false")
	}
	if !scope.Operators {
		t.Error("expected Operators to be true")
	}
}

func TestCredentialScope_Defaults(t *testing.T) {
	scope := CredentialScope{}

	if scope.ArgoCD {
		t.Error("expected ArgoCD to default to false")
	}
	if scope.Operators {
		t.Error("expected Operators to default to false")
	}
}

func TestClusterCredential_StatusFields(t *testing.T) {
	lastChecked := metav1.Now()

	cc := ClusterCredential{
		Status: ClusterCredentialStatus{
			Connected:     true,
			LastCheckedAt: &lastChecked,
		},
	}

	if !cc.Status.Connected {
		t.Error("expected Connected to be true")
	}
	if cc.Status.LastCheckedAt == nil {
		t.Fatal("expected LastCheckedAt to be set")
	}
}

func TestClusterCredential_StatusDisconnected(t *testing.T) {
	cc := ClusterCredential{
		Status: ClusterCredentialStatus{
			Connected: false,
		},
	}

	if cc.Status.Connected {
		t.Error("expected Connected to be false")
	}
}

func TestClusterCredential_StatusConditions(t *testing.T) {
	cc := ClusterCredential{}

	SetCondition(&cc.Status.Conditions, "Reachable", metav1.ConditionTrue, "Connected", "cluster API server is reachable", 1)

	if len(cc.Status.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(cc.Status.Conditions))
	}
	if cc.Status.Conditions[0].Type != "Reachable" {
		t.Errorf("expected condition type %q, got %q", "Reachable", cc.Status.Conditions[0].Type)
	}
	if cc.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("expected condition status True, got %v", cc.Status.Conditions[0].Status)
	}
}

func TestClusterCredentialList_ContainsItems(t *testing.T) {
	list := ClusterCredentialList{
		Items: []ClusterCredential{
			{ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"}},
		},
	}

	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list.Items))
	}
	if list.Items[0].Name != "cluster-1" {
		t.Errorf("expected first item name %q, got %q", "cluster-1", list.Items[0].Name)
	}
	if list.Items[1].Name != "cluster-2" {
		t.Errorf("expected second item name %q, got %q", "cluster-2", list.Items[1].Name)
	}
}

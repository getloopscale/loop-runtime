// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAppSecret_SpecFields(t *testing.T) {
	as := AppSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-api-key",
			Namespace: "loopzero-system",
		},
		Spec: AppSecretSpec{
			TenantID:    "tenant-abc",
			ProjectID:   "project-xyz",
			Environment: "production",
			App:         "api-server",
			SecretRef: SecretReference{
				Name:      "api-key-secret",
				Namespace: "mgmt-secrets",
			},
			Rotation: RotationConfig{
				Enabled:           true,
				MaxAgeHours:       720,
				NotifyBeforeHours: 48,
			},
		},
	}

	if as.Spec.TenantID != "tenant-abc" {
		t.Errorf("expected TenantID %q, got %q", "tenant-abc", as.Spec.TenantID)
	}
	if as.Spec.ProjectID != "project-xyz" {
		t.Errorf("expected ProjectID %q, got %q", "project-xyz", as.Spec.ProjectID)
	}
	if as.Spec.Environment != "production" {
		t.Errorf("expected Environment %q, got %q", "production", as.Spec.Environment)
	}
	if as.Spec.App != "api-server" {
		t.Errorf("expected App %q, got %q", "api-server", as.Spec.App)
	}
}

func TestAppSecret_SecretReference(t *testing.T) {
	ref := SecretReference{
		Name:      "db-password",
		Namespace: "mgmt-secrets",
	}

	if ref.Name != "db-password" {
		t.Errorf("expected Name %q, got %q", "db-password", ref.Name)
	}
	if ref.Namespace != "mgmt-secrets" {
		t.Errorf("expected Namespace %q, got %q", "mgmt-secrets", ref.Namespace)
	}
}

func TestAppSecret_RotationConfig(t *testing.T) {
	rc := RotationConfig{
		Enabled:           true,
		MaxAgeHours:       720,
		NotifyBeforeHours: 48,
	}

	if !rc.Enabled {
		t.Error("expected Enabled to be true")
	}
	if rc.MaxAgeHours != 720 {
		t.Errorf("expected MaxAgeHours %d, got %d", 720, rc.MaxAgeHours)
	}
	if rc.NotifyBeforeHours != 48 {
		t.Errorf("expected NotifyBeforeHours %d, got %d", 48, rc.NotifyBeforeHours)
	}
}

func TestAppSecret_RotationConfig_Defaults(t *testing.T) {
	rc := RotationConfig{}

	if rc.Enabled {
		t.Error("expected Enabled to default to false")
	}
	if rc.MaxAgeHours != 0 {
		t.Errorf("expected MaxAgeHours to default to 0, got %d", rc.MaxAgeHours)
	}
	if rc.NotifyBeforeHours != 0 {
		t.Errorf("expected NotifyBeforeHours to default to 0, got %d", rc.NotifyBeforeHours)
	}
}

func TestAppSecret_StatusFields(t *testing.T) {
	syncedAt := metav1.Now()
	rotationDue := metav1.Now()

	as := AppSecret{
		Status: AppSecretStatus{
			SyncedAt:        &syncedAt,
			TargetNamespace: "workload-tenant-abc-production",
			Hash:            "sha256:abc123",
			WorkloadHash:    "sha256:abc123",
			HashMatch:       true,
			RotationDueAt:   &rotationDue,
		},
	}

	if as.Status.SyncedAt == nil {
		t.Fatal("expected SyncedAt to be set")
	}
	if as.Status.TargetNamespace != "workload-tenant-abc-production" {
		t.Errorf("expected TargetNamespace %q, got %q", "workload-tenant-abc-production", as.Status.TargetNamespace)
	}
	if as.Status.Hash != "sha256:abc123" {
		t.Errorf("expected Hash %q, got %q", "sha256:abc123", as.Status.Hash)
	}
	if as.Status.WorkloadHash != "sha256:abc123" {
		t.Errorf("expected WorkloadHash %q, got %q", "sha256:abc123", as.Status.WorkloadHash)
	}
	if !as.Status.HashMatch {
		t.Error("expected HashMatch to be true")
	}
	if as.Status.RotationDueAt == nil {
		t.Fatal("expected RotationDueAt to be set")
	}
}

func TestAppSecret_HashMismatch(t *testing.T) {
	as := AppSecret{
		Status: AppSecretStatus{
			Hash:         "sha256:abc123",
			WorkloadHash: "sha256:def456",
			HashMatch:    false,
		},
	}

	if as.Status.HashMatch {
		t.Error("expected HashMatch to be false when hashes differ")
	}
	if as.Status.Hash == as.Status.WorkloadHash {
		t.Error("expected Hash and WorkloadHash to differ")
	}
}

func TestAppSecret_StatusConditions(t *testing.T) {
	as := AppSecret{}

	SetCondition(&as.Status.Conditions, "Synced", metav1.ConditionTrue, "Synchronized", "secret synced to workload cluster", 1)
	SetCondition(&as.Status.Conditions, "RotationCurrent", metav1.ConditionTrue, "WithinPolicy", "rotation not yet due", 1)

	if len(as.Status.Conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(as.Status.Conditions))
	}
	if as.Status.Conditions[0].Type != "Synced" {
		t.Errorf("expected first condition type %q, got %q", "Synced", as.Status.Conditions[0].Type)
	}
	if as.Status.Conditions[1].Type != "RotationCurrent" {
		t.Errorf("expected second condition type %q, got %q", "RotationCurrent", as.Status.Conditions[1].Type)
	}
}

func TestAppSecretList_ContainsItems(t *testing.T) {
	list := AppSecretList{
		Items: []AppSecret{
			{ObjectMeta: metav1.ObjectMeta{Name: "secret-1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "secret-2"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "secret-3"}},
		},
	}

	if len(list.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list.Items))
	}
	if list.Items[2].Name != "secret-3" {
		t.Errorf("expected third item name %q, got %q", "secret-3", list.Items[2].Name)
	}
}

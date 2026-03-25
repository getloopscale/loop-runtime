// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package v1

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterSyncState_Values(t *testing.T) {
	states := []ClusterSyncState{
		ClusterStateSynced,
		ClusterStateDrifted,
		ClusterStateChecking,
		ClusterStateError,
	}
	expected := []string{"Synced", "Drifted", "Checking", "Error"}

	for i, s := range states {
		if string(s) != expected[i] {
			t.Errorf("expected state %q, got %q", expected[i], s)
		}
	}
}

func TestClusterSyncState_AreDistinct(t *testing.T) {
	states := []ClusterSyncState{
		ClusterStateSynced,
		ClusterStateDrifted,
		ClusterStateChecking,
		ClusterStateError,
	}
	seen := make(map[ClusterSyncState]bool)
	for _, s := range states {
		if seen[s] {
			t.Errorf("duplicate cluster sync state: %q", s)
		}
		seen[s] = true
	}
}

func TestDriftResolution_Values(t *testing.T) {
	resolutions := []DriftResolution{
		DriftResolutionAutoRepaired,
		DriftResolutionManualRequired,
	}
	expected := []string{"AutoRepaired", "ManualRequired"}

	for i, r := range resolutions {
		if string(r) != expected[i] {
			t.Errorf("expected resolution %q, got %q", expected[i], r)
		}
	}
}

func TestDriftResolution_AreDistinct(t *testing.T) {
	resolutions := []DriftResolution{
		DriftResolutionAutoRepaired,
		DriftResolutionManualRequired,
	}
	seen := make(map[DriftResolution]bool)
	for _, r := range resolutions {
		if seen[r] {
			t.Errorf("duplicate drift resolution: %q", r)
		}
		seen[r] = true
	}
}

func TestReconciliationReport_SpecFields(t *testing.T) {
	rr := ReconciliationReport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "report-tenant-abc-prod",
			Namespace: "loopzero-system",
		},
		Spec: ReconciliationReportSpec{
			TenantID:       "tenant-abc",
			ProjectID:      "project-xyz",
			Environment:    "production",
			CheckInterval:  metav1.Duration{Duration: 60 * time.Second},
			DriftThreshold: metav1.Duration{Duration: 300 * time.Second},
			ExcludeFields:  []string{"metadata.resourceVersion", "metadata.uid"},
			ExcludeAnnotations: []string{
				"kubectl.kubernetes.io/",
			},
			ExcludeLabels: []string{
				"app.kubernetes.io/managed-by",
			},
			AuthorizedControllers: []AuthorizedController{
				{
					Name:   "HPA",
					Fields: []string{"spec.replicas"},
				},
			},
		},
	}

	if rr.Spec.TenantID != "tenant-abc" {
		t.Errorf("expected TenantID %q, got %q", "tenant-abc", rr.Spec.TenantID)
	}
	if rr.Spec.ProjectID != "project-xyz" {
		t.Errorf("expected ProjectID %q, got %q", "project-xyz", rr.Spec.ProjectID)
	}
	if rr.Spec.Environment != "production" {
		t.Errorf("expected Environment %q, got %q", "production", rr.Spec.Environment)
	}
	if rr.Spec.CheckInterval.Duration != 60*time.Second {
		t.Errorf("expected CheckInterval 60s, got %v", rr.Spec.CheckInterval.Duration)
	}
	if rr.Spec.DriftThreshold.Duration != 300*time.Second {
		t.Errorf("expected DriftThreshold 300s, got %v", rr.Spec.DriftThreshold.Duration)
	}
}

func TestReconciliationReport_ExcludeFields(t *testing.T) {
	rr := ReconciliationReport{
		Spec: ReconciliationReportSpec{
			ExcludeFields:      []string{"metadata.resourceVersion", "metadata.uid"},
			ExcludeAnnotations: []string{"kubectl.kubernetes.io/"},
			ExcludeLabels:      []string{"app.kubernetes.io/managed-by"},
		},
	}

	if len(rr.Spec.ExcludeFields) != 2 {
		t.Fatalf("expected 2 exclude fields, got %d", len(rr.Spec.ExcludeFields))
	}
	if len(rr.Spec.ExcludeAnnotations) != 1 {
		t.Fatalf("expected 1 exclude annotation, got %d", len(rr.Spec.ExcludeAnnotations))
	}
	if len(rr.Spec.ExcludeLabels) != 1 {
		t.Fatalf("expected 1 exclude label, got %d", len(rr.Spec.ExcludeLabels))
	}
}

func TestAuthorizedController_Fields(t *testing.T) {
	ac := AuthorizedController{
		Name:   "HPA",
		Fields: []string{"spec.replicas"},
	}

	if ac.Name != "HPA" {
		t.Errorf("expected Name %q, got %q", "HPA", ac.Name)
	}
	if len(ac.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(ac.Fields))
	}
	if ac.Fields[0] != "spec.replicas" {
		t.Errorf("expected field %q, got %q", "spec.replicas", ac.Fields[0])
	}
}

func TestAuthorizedController_MultipleFields(t *testing.T) {
	ac := AuthorizedController{
		Name:   "cert-manager",
		Fields: []string{"spec.tls", "metadata.annotations"},
	}

	if len(ac.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(ac.Fields))
	}
}

func TestReconciliationReport_StatusFields(t *testing.T) {
	lastChecked := metav1.Now()
	driftDetected := metav1.Now()
	resolution := DriftResolutionAutoRepaired

	rr := ReconciliationReport{
		Status: ReconciliationReportStatus{
			LastCheckedAt:   &lastChecked,
			GitHeadCommit:   "abc123def456",
			ArgoSyncCommit:  "abc123def456",
			ClusterState:    ClusterStateSynced,
			DriftDetectedAt: &driftDetected,
			DriftResolution: &resolution,
		},
	}

	if rr.Status.LastCheckedAt == nil {
		t.Fatal("expected LastCheckedAt to be set")
	}
	if rr.Status.GitHeadCommit != "abc123def456" {
		t.Errorf("expected GitHeadCommit %q, got %q", "abc123def456", rr.Status.GitHeadCommit)
	}
	if rr.Status.ArgoSyncCommit != "abc123def456" {
		t.Errorf("expected ArgoSyncCommit %q, got %q", "abc123def456", rr.Status.ArgoSyncCommit)
	}
	if rr.Status.ClusterState != ClusterStateSynced {
		t.Errorf("expected ClusterState %q, got %q", ClusterStateSynced, rr.Status.ClusterState)
	}
	if rr.Status.DriftDetectedAt == nil {
		t.Fatal("expected DriftDetectedAt to be set")
	}
	if rr.Status.DriftResolution == nil {
		t.Fatal("expected DriftResolution to be set")
	}
	if *rr.Status.DriftResolution != DriftResolutionAutoRepaired {
		t.Errorf("expected DriftResolution %q, got %q", DriftResolutionAutoRepaired, *rr.Status.DriftResolution)
	}
}

func TestReconciliationReport_DriftDetails(t *testing.T) {
	detail := DriftDetail{
		Resource:     "apps/v1/Deployment/api",
		Field:        "spec.replicas",
		GitValue:     "3",
		ClusterValue: "5",
		Cause:        "HPA scaled up",
	}

	if detail.Resource != "apps/v1/Deployment/api" {
		t.Errorf("expected Resource %q, got %q", "apps/v1/Deployment/api", detail.Resource)
	}
	if detail.Field != "spec.replicas" {
		t.Errorf("expected Field %q, got %q", "spec.replicas", detail.Field)
	}
	if detail.GitValue != "3" {
		t.Errorf("expected GitValue %q, got %q", "3", detail.GitValue)
	}
	if detail.ClusterValue != "5" {
		t.Errorf("expected ClusterValue %q, got %q", "5", detail.ClusterValue)
	}
	if detail.Cause != "HPA scaled up" {
		t.Errorf("expected Cause %q, got %q", "HPA scaled up", detail.Cause)
	}
}

func TestReconciliationReport_DriftDetails_WithoutCause(t *testing.T) {
	detail := DriftDetail{
		Resource:     "v1/ConfigMap/settings",
		Field:        "data.LOG_LEVEL",
		GitValue:     "info",
		ClusterValue: "debug",
	}

	if detail.Cause != "" {
		t.Errorf("expected empty Cause, got %q", detail.Cause)
	}
}

func TestReconciliationReport_StatusConditions(t *testing.T) {
	rr := ReconciliationReport{}

	SetCondition(&rr.Status.Conditions, "InSync", metav1.ConditionTrue, "AllSynced", "git and cluster are in sync", 1)

	if len(rr.Status.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(rr.Status.Conditions))
	}
	if rr.Status.Conditions[0].Type != "InSync" {
		t.Errorf("expected condition type %q, got %q", "InSync", rr.Status.Conditions[0].Type)
	}
	if rr.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("expected condition status True, got %v", rr.Status.Conditions[0].Status)
	}
}

func TestReconciliationReport_MultipleDriftDetails(t *testing.T) {
	rr := ReconciliationReport{
		Status: ReconciliationReportStatus{
			ClusterState: ClusterStateDrifted,
			DriftDetails: []DriftDetail{
				{
					Resource:     "apps/v1/Deployment/api",
					Field:        "spec.replicas",
					GitValue:     "3",
					ClusterValue: "5",
					Cause:        "HPA scaled up",
				},
				{
					Resource:     "v1/ConfigMap/settings",
					Field:        "data.LOG_LEVEL",
					GitValue:     "info",
					ClusterValue: "debug",
				},
			},
		},
	}

	if len(rr.Status.DriftDetails) != 2 {
		t.Fatalf("expected 2 drift details, got %d", len(rr.Status.DriftDetails))
	}
	if rr.Status.ClusterState != ClusterStateDrifted {
		t.Errorf("expected ClusterState %q, got %q", ClusterStateDrifted, rr.Status.ClusterState)
	}
}

func TestReconciliationReportList_ContainsItems(t *testing.T) {
	list := ReconciliationReportList{
		Items: []ReconciliationReport{
			{ObjectMeta: metav1.ObjectMeta{Name: "report-1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "report-2"}},
		},
	}

	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list.Items))
	}
	if list.Items[0].Name != "report-1" {
		t.Errorf("expected first item name %q, got %q", "report-1", list.Items[0].Name)
	}
}

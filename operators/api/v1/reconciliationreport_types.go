// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterSyncState represents the synchronization state between Git and the cluster.
type ClusterSyncState string

const (
	ClusterStateSynced   ClusterSyncState = "Synced"
	ClusterStateDrifted  ClusterSyncState = "Drifted"
	ClusterStateChecking ClusterSyncState = "Checking"
	ClusterStateError    ClusterSyncState = "Error"
)

// DriftResolution describes how a drift was resolved.
type DriftResolution string

const (
	DriftResolutionAutoRepaired    DriftResolution = "AutoRepaired"
	DriftResolutionManualRequired  DriftResolution = "ManualRequired"
)

// ReconciliationReport is the Schema for the reconciliationreports API.
// It reports drift between desired state (Git) and actual state (cluster).
// The StateReconciler is observational only and never modifies Git or cluster state.
// See spec section 13.
//
// NOTE: This CRD is currently dead code — no controller implements reconciliation
// for this type and no sprint is assigned for its implementation. It is retained
// as a placeholder for future drift-detection work. Do not depend on its schema
// being stable.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.clusterState`
// +kubebuilder:printcolumn:name="Environment",type=string,JSONPath=`.spec.environment`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ReconciliationReport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReconciliationReportSpec   `json:"spec,omitempty"`
	Status ReconciliationReportStatus `json:"status,omitempty"`
}

// ReconciliationReportSpec defines the desired reconciliation checking behavior.
type ReconciliationReportSpec struct {
	// TenantID is the tenant being monitored.
	TenantID string `json:"tenantId"`

	// ProjectID is the project being monitored.
	ProjectID string `json:"projectId"`

	// Environment is the environment being monitored.
	Environment string `json:"environment"`

	// CheckInterval is how often to check for drift (e.g., "60s").
	CheckInterval metav1.Duration `json:"checkInterval"`

	// DriftThreshold is how long drift must persist before alerting (e.g., "300s").
	DriftThreshold metav1.Duration `json:"driftThreshold"`

	// ExcludeFields lists field paths to ignore during drift detection.
	ExcludeFields []string `json:"excludeFields,omitempty"`

	// ExcludeAnnotations lists annotation prefixes to ignore during drift detection.
	ExcludeAnnotations []string `json:"excludeAnnotations,omitempty"`

	// ExcludeLabels lists label prefixes to ignore during drift detection.
	ExcludeLabels []string `json:"excludeLabels,omitempty"`

	// AuthorizedControllers lists controllers whose mutations are considered expected.
	AuthorizedControllers []AuthorizedController `json:"authorizedControllers,omitempty"`
}

// AuthorizedController identifies a controller whose field mutations are expected.
type AuthorizedController struct {
	// Name is the human-readable name of the controller (e.g., "HPA", "cert-manager").
	Name string `json:"name"`

	// Fields lists the fields this controller is authorized to modify.
	Fields []string `json:"fields"`
}

// ReconciliationReportStatus defines the observed reconciliation state.
type ReconciliationReportStatus struct {
	// LastCheckedAt is the last time a drift check was performed.
	LastCheckedAt *metav1.Time `json:"lastCheckedAt,omitempty"`

	// GitHeadCommit is the latest Git commit SHA observed.
	GitHeadCommit string `json:"gitHeadCommit,omitempty"`

	// ArgoSyncCommit is the commit SHA that ArgoCD last synced.
	ArgoSyncCommit string `json:"argoSyncCommit,omitempty"`

	// ClusterState is the current synchronization state.
	ClusterState ClusterSyncState `json:"clusterState,omitempty"`

	// DriftDetectedAt is when drift was first detected (nil if in sync).
	DriftDetectedAt *metav1.Time `json:"driftDetectedAt,omitempty"`

	// DriftResolution describes how the drift was resolved (nil if no drift).
	DriftResolution *DriftResolution `json:"driftResolution,omitempty"`

	// DriftDetails lists specific field-level drift information.
	DriftDetails []DriftDetail `json:"driftDetails,omitempty"`

	// Conditions represent the latest available observations.
	// Condition types: InSync.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// DriftDetail describes a specific field-level divergence between Git and cluster.
type DriftDetail struct {
	// Resource identifies the drifted resource (e.g., "apps/v1/Deployment/api").
	Resource string `json:"resource"`

	// Field is the specific field that drifted (e.g., "spec.replicas").
	Field string `json:"field"`

	// GitValue is the value in Git.
	GitValue string `json:"gitValue"`

	// ClusterValue is the value in the cluster.
	ClusterValue string `json:"clusterValue"`

	// Cause describes why the drift occurred (e.g., "HPA scaled up").
	Cause string `json:"cause,omitempty"`
}

// ReconciliationReportList contains a list of ReconciliationReport resources.
//
// +kubebuilder:object:root=true
type ReconciliationReportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ReconciliationReport `json:"items"`
}

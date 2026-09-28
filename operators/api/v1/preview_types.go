// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PreviewPhase represents the lifecycle phase of a PreviewEnvironment.
// See state machine spec section 5.3.
type PreviewPhase string

const (
	PreviewPhaseCreating      PreviewPhase = "Creating"
	PreviewPhaseActive        PreviewPhase = "Active"
	PreviewPhaseExpiring      PreviewPhase = "Expiring"
	PreviewPhaseDeleted       PreviewPhase = "Deleted"
	PreviewPhaseFailed        PreviewPhase = "Failed"
	PreviewPhaseDeleting      PreviewPhase = "Deleting"
	PreviewPhaseCleanupFailed PreviewPhase = "CleanupFailed"
)

// PreviewEnvironment is the Schema for the previewenvironments API.
// It represents a temporary environment created for a pull request.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Branch",type=string,JSONPath=`.spec.branch`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type PreviewEnvironment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PreviewEnvironmentSpec   `json:"spec,omitempty"`
	Status PreviewEnvironmentStatus `json:"status,omitempty"`
}

// PreviewEnvironmentSpec defines the desired state of a PreviewEnvironment
// as described in spec section 9.2.
type PreviewEnvironmentSpec struct {
	// TenantID is the owning tenant identifier.
	// +kubebuilder:validation:MinLength=1
	TenantID string `json:"tenantId"`

	// ProjectID is the project this preview belongs to.
	// +kubebuilder:validation:MinLength=1
	ProjectID string `json:"projectId"`

	// CellID is the multi-cell routing target for this preview environment.
	// T1.1 of deep-review-remediation-plan (closes find_arch_002 HIGH):
	// previously absent, causing PreviewController to route ALL preview
	// workloads to the static CELL_0_KUBECONFIG cell, ignoring the
	// tenant's pinned cell. Cell-2 tenants' previews silently landed on
	// cell-0 — multi-cell isolation breach.
	//
	// When CellID is empty (legacy CRs created pre-T1.1), the controller
	// falls back to tenant.Spec.CellID via tenant lookup. Going forward,
	// the API submitting the CR sets CellID at creation time.
	//
	// +kubebuilder:validation:Optional
	CellID string `json:"cellId,omitempty"`

	// PRNumber is the pull request number that triggered this preview.
	// +kubebuilder:validation:Minimum=1
	PRNumber int `json:"prNumber"`

	// Branch is the source branch for the preview.
	// +kubebuilder:validation:MinLength=1
	Branch string `json:"branch"`

	// ExpiresAt is the time at which this preview should be automatically cleaned up.
	ExpiresAt *metav1.Time `json:"expiresAt"`

	// ReleaseID is the release associated with this preview.
	ReleaseID string `json:"releaseId"`

	// PublicURL is the user-facing URL of this preview, computed once by the
	// API with pkg/naming.PreviewURL (tenant, project, entry app and the
	// public preview domain). The controller reports it in Status.URL and
	// never derives a URL of its own: the formula has one owner. Empty when
	// the API has no public preview domain configured.
	//
	// +kubebuilder:validation:Optional
	PublicURL string `json:"publicURL,omitempty"`
}

// PreviewEnvironmentStatus defines the observed state of a PreviewEnvironment.
type PreviewEnvironmentStatus struct {
	// Phase represents the current lifecycle phase. See state machine 5.3.
	Phase PreviewPhase `json:"phase,omitempty"`

	// URL is the publicly accessible URL for this preview environment.
	URL string `json:"url,omitempty"`

	// CreatedAt is the time the preview environment was actually created.
	CreatedAt *metav1.Time `json:"createdAt,omitempty"`

	// Conditions represent the latest available observations of the preview's state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// PreviewEnvironmentList contains a list of PreviewEnvironment resources.
//
// +kubebuilder:object:root=true
type PreviewEnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PreviewEnvironment `json:"items"`
}

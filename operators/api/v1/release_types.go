package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReleasePhase represents the lifecycle phase of a LoopZeroRelease.
// +kubebuilder:validation:Enum=Ready;Pending;Active;GitMirrorFailed;SyncFailed;DriftAlert;Superseded;RollingBack;RolledBack;RollbackFailed;Cancelled
type ReleasePhase string

const (
	ReleasePhaseReady           ReleasePhase = "Ready"
	ReleasePhasePending         ReleasePhase = "Pending"
	ReleasePhaseActive          ReleasePhase = "Active"
	ReleasePhaseGitMirrorFailed ReleasePhase = "GitMirrorFailed"
	ReleasePhaseSyncFailed      ReleasePhase = "SyncFailed"
	ReleasePhaseDriftAlert      ReleasePhase = "DriftAlert"
	ReleasePhaseSuperseded      ReleasePhase = "Superseded"
	ReleasePhaseRollingBack     ReleasePhase = "RollingBack"
	ReleasePhaseRolledBack      ReleasePhase = "RolledBack"
	ReleasePhaseRollbackFailed  ReleasePhase = "RollbackFailed"
	ReleasePhaseCancelled       ReleasePhase = "Cancelled"
)

// LoopZeroRelease is the Schema for the loopzeroreleases API.
// A LoopZeroRelease represents a single release tracked by the release controller.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="ReleaseID",type=string,JSONPath=`.spec.releaseId`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type LoopZeroRelease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LoopZeroReleaseSpec   `json:"spec,omitempty"`
	Status LoopZeroReleaseStatus `json:"status,omitempty"`
}

// LoopZeroReleaseSpec defines the desired state of a LoopZeroRelease.
type LoopZeroReleaseSpec struct {
	// ReleaseID is the unique identifier for this release.
	// +kubebuilder:validation:MinLength=1
	ReleaseID string `json:"releaseId"`

	// TenantID is the tenant that owns this release.
	// +kubebuilder:validation:MinLength=1
	TenantID string `json:"tenantId"`

	// ProjectSlug is the project this release belongs to.
	// +kubebuilder:validation:MinLength=1
	ProjectSlug string `json:"projectSlug"`

	// EnvironmentSlug is the target environment for this release.
	// +kubebuilder:validation:MinLength=1
	EnvironmentSlug string `json:"environmentSlug"`

	// ImageRef is the full image reference including digest.
	// +kubebuilder:validation:MinLength=1
	ImageRef string `json:"imageRef"`

	// Replicas is the desired number of replicas.
	// +optional
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`

	// EnvVars are environment variables to set on the deployment.
	// +optional
	EnvVars map[string]string `json:"envVars,omitempty"`

	// PreviousReleaseID is the release this one supersedes.
	// +optional
	PreviousReleaseID string `json:"previousReleaseId,omitempty"`
}

// LoopZeroReleaseStatus defines the observed state of a LoopZeroRelease.
type LoopZeroReleaseStatus struct {
	// Phase represents the current release lifecycle phase.
	Phase ReleasePhase `json:"phase,omitempty"`

	// GiteaCommit is the commit SHA of the GitOps push in Gitea.
	GiteaCommit string `json:"giteaCommit,omitempty"`

	// ArgoSyncStatus is the last known ArgoCD sync status.
	ArgoSyncStatus string `json:"argoSyncStatus,omitempty"`

	// ArgoHealthStatus is the last known ArgoCD health status.
	ArgoHealthStatus string `json:"argoHealthStatus,omitempty"`

	// CompletedAt is the time the release reached a terminal or active state.
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// Message contains a human-readable message about the current state.
	Message string `json:"message,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the release's state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// LoopZeroReleaseList contains a list of LoopZeroRelease resources.
//
// +kubebuilder:object:root=true
type LoopZeroReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LoopZeroRelease `json:"items"`
}

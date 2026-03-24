package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildPhase represents the lifecycle phase of a LoopZeroBuild.
// +kubebuilder:validation:Enum=Queued;Running;Succeeded;Failed;Cancelling;Cancelled
type BuildPhase string

const (
	BuildPhaseQueued     BuildPhase = "Queued"
	BuildPhaseRunning    BuildPhase = "Running"
	BuildPhaseSucceeded  BuildPhase = "Succeeded"
	BuildPhaseFailed     BuildPhase = "Failed"
	BuildPhaseCancelling BuildPhase = "Cancelling"
	BuildPhaseCancelled  BuildPhase = "Cancelled"
)

// LoopZeroBuild is the Schema for the loopzerobuilds API.
// A LoopZeroBuild represents a single build execution tracked by the build controller.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="BuildID",type=string,JSONPath=`.spec.buildId`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type LoopZeroBuild struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LoopZeroBuildSpec   `json:"spec,omitempty"`
	Status LoopZeroBuildStatus `json:"status,omitempty"`
}

// LoopZeroBuildSpec defines the desired state of a LoopZeroBuild.
type LoopZeroBuildSpec struct {
	// BuildID is the unique identifier for this build.
	// +kubebuilder:validation:MinLength=1
	BuildID string `json:"buildId"`

	// TenantID is the tenant that owns this build.
	// +kubebuilder:validation:MinLength=1
	TenantID string `json:"tenantId"`

	// ProjectSlug is the project this build belongs to.
	// +kubebuilder:validation:MinLength=1
	ProjectSlug string `json:"projectSlug"`

	// EnvironmentSlug is the target environment for this build.
	// +kubebuilder:validation:MinLength=1
	EnvironmentSlug string `json:"environmentSlug"`

	// SourceRef is the MinIO object key containing the build source.
	// +kubebuilder:validation:MinLength=1
	SourceRef string `json:"sourceRef"`

	// Dockerfile is the path to the Dockerfile within the source archive.
	// +optional
	Dockerfile string `json:"dockerfile,omitempty"`

	// BuildArgs are additional build arguments passed to the build engine.
	// +optional
	BuildArgs map[string]string `json:"buildArgs,omitempty"`

	// ImageTag is the expected tag for the built image.
	// +kubebuilder:validation:MinLength=1
	ImageTag string `json:"imageTag"`
}

// LoopZeroBuildStatus defines the observed state of a LoopZeroBuild.
type LoopZeroBuildStatus struct {
	// Phase represents the current build lifecycle phase.
	Phase BuildPhase `json:"phase,omitempty"`

	// WorkflowName is the name of the Argo Workflow executing this build.
	WorkflowName string `json:"workflowName,omitempty"`

	// ImageDigest is the sha256 digest of the built image (set on success).
	ImageDigest string `json:"imageDigest,omitempty"`

	// StartedAt is the time the build started running.
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletedAt is the time the build reached a terminal state.
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// Message contains a human-readable message about the current state.
	Message string `json:"message,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the build's state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// LoopZeroBuildList contains a list of LoopZeroBuild resources.
//
// +kubebuilder:object:root=true
type LoopZeroBuildList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LoopZeroBuild `json:"items"`
}

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ApplicationPhase represents the lifecycle phase of a LoopZeroApplication.
// +kubebuilder:validation:Enum=Progressing;Serving;Reachable;Degraded;Down
type ApplicationPhase string

const (
	ApplicationPhaseProgressing ApplicationPhase = "Progressing"
	ApplicationPhaseServing     ApplicationPhase = "Serving"
	ApplicationPhaseReachable   ApplicationPhase = "Reachable"
	ApplicationPhaseDegraded    ApplicationPhase = "Degraded"
	ApplicationPhaseDown        ApplicationPhase = "Down"
)

// LoopZeroApplication is the Schema for the loopzeroapplications API.
// A LoopZeroApplication represents a tenant's deployed application, managing
// hostname assignment, ExposedRoute creation, and health tracking.
//
// Named LoopZeroApplication (not Application) to avoid collision with ArgoCD's
// Application type.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type LoopZeroApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LoopZeroApplicationSpec   `json:"spec,omitempty"`
	Status LoopZeroApplicationStatus `json:"status,omitempty"`
}

// LoopZeroApplicationSpec defines the desired state of a LoopZeroApplication.
type LoopZeroApplicationSpec struct {
	// TenantRef is the name of the parent Tenant CR.
	// +kubebuilder:validation:MinLength=1
	TenantRef string `json:"tenantRef"`

	// Project is the project name.
	// +kubebuilder:validation:MinLength=1
	Project string `json:"project"`

	// Environment is the environment name (e.g., "production", "staging").
	// +kubebuilder:validation:MinLength=1
	Environment string `json:"environment"`

	// ServiceName is the Kubernetes Service name backing this application.
	// +kubebuilder:validation:MinLength=1
	ServiceName string `json:"serviceName"`

	// Hostname is the external hostname for this application.
	// If empty, the controller computes it via naming.Hostname().
	// +optional
	Hostname string `json:"hostname,omitempty"`

	// Replicas is the desired number of replicas.
	// +optional
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`

	// ServicePort is the port on the backing service.
	// +optional
	// +kubebuilder:default=80
	ServicePort int32 `json:"servicePort,omitempty"`
}

// LoopZeroApplicationStatus defines the observed state of a LoopZeroApplication.
type LoopZeroApplicationStatus struct {
	// Phase represents the overall application lifecycle phase.
	Phase ApplicationPhase `json:"phase,omitempty"`

	// PodReady is the number of ready pods.
	PodReady int32 `json:"podReady,omitempty"`

	// PodDesired is the desired number of pods.
	PodDesired int32 `json:"podDesired,omitempty"`

	// EndpointsReady indicates whether endpoints are available.
	EndpointsReady bool `json:"endpointsReady,omitempty"`

	// Reachable indicates whether the route probe succeeded.
	Reachable bool `json:"reachable,omitempty"`

	// ArgoSyncStatus reflects the ArgoCD sync status (if available).
	ArgoSyncStatus string `json:"argoSyncStatus,omitempty"`

	// URL is the full URL where the application is accessible.
	URL string `json:"url,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the application's state.
	// Condition types: ServiceResolved, IngressReady, CertificateIssued, RouteReachable, ApplicationHealthy
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// LoopZeroApplicationList contains a list of LoopZeroApplication resources.
//
// +kubebuilder:object:root=true
type LoopZeroApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LoopZeroApplication `json:"items"`
}

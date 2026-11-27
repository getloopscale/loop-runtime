// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ApplicationPhase represents the lifecycle phase of a LoopRuntimeApplication.
// +kubebuilder:validation:Enum=Progressing;Serving;Reachable;Degraded;Down;Cleaned
type ApplicationPhase string

const (
	ApplicationPhaseProgressing ApplicationPhase = "Progressing"
	ApplicationPhaseServing     ApplicationPhase = "Serving"
	ApplicationPhaseReachable   ApplicationPhase = "Reachable"
	ApplicationPhaseDegraded    ApplicationPhase = "Degraded"
	ApplicationPhaseDown        ApplicationPhase = "Down"
	// ApplicationPhaseCleaned is a terminal state set by the reaper when the
	// CR has been Progressing for > grace period AND the GitOps source no
	// longer contains its manifest. CR record is preserved for audit trail.
	// fix-all-platform-issues-plan T2.2.
	ApplicationPhaseCleaned ApplicationPhase = "Cleaned"
)

// LoopRuntimeApplication is the Schema for the loopruntimeapplications API.
// A LoopRuntimeApplication represents a tenant's deployed application, managing
// hostname assignment, ExposedRoute creation, and health tracking.
//
// Named LoopRuntimeApplication (not Application) to avoid collision with ArgoCD's
// Application type.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type LoopRuntimeApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LoopRuntimeApplicationSpec   `json:"spec,omitempty"`
	Status LoopRuntimeApplicationStatus `json:"status,omitempty"`
}

// LoopRuntimeApplicationSpec defines the desired state of a LoopRuntimeApplication.
type LoopRuntimeApplicationSpec struct {
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

	// AppName is the application's name inside its project (the service name
	// in loopzero.yaml). Together with TenantRef, Project and Environment it
	// composes the routed hostname through pkg/naming.AppHostname, the formula
	// the IngressRoute renderer uses. Optional so CRs written before the field
	// existed stay valid: for those the controller reads the runtime.getloopscale.com/app label
	// the api has always set on the CR.
	// +optional
	AppName string `json:"appName,omitempty"`

	// Hostname is the external hostname for this application.
	// If empty, the controller computes it via naming.AppHostname.
	// +optional
	Hostname string `json:"hostname,omitempty"`

	// Replicas is the desired number of replicas.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas,omitempty"`

	// ServicePort is the port on the backing service.
	// +optional
	// +kubebuilder:default=80
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	ServicePort int32 `json:"servicePort,omitempty"`
}

// LoopRuntimeApplicationStatus defines the observed state of a LoopRuntimeApplication.
type LoopRuntimeApplicationStatus struct {
	// Phase represents the overall application lifecycle phase.
	Phase ApplicationPhase `json:"phase,omitempty"`

	// EndpointsReady indicates whether endpoints are available.
	EndpointsReady bool `json:"endpointsReady,omitempty"`

	// Reachable indicates whether the route probe succeeded.
	Reachable bool `json:"reachable,omitempty"`

	// URL is the full URL where the application is accessible.
	URL string `json:"url,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the application's state.
	// Condition types: ServiceResolved, IngressReady, CertificateIssued, RouteReachable, ApplicationHealthy
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// LoopRuntimeApplicationList contains a list of LoopRuntimeApplication resources.
//
// +kubebuilder:object:root=true
type LoopRuntimeApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LoopRuntimeApplication `json:"items"`
}

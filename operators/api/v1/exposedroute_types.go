// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExposedRoutePhase represents the lifecycle phase of an ExposedRoute.
// +kubebuilder:validation:Enum=Pending;CertIssued;Reachable;Failed
type ExposedRoutePhase string

const (
	ExposedRoutePhasePending    ExposedRoutePhase = "Pending"
	ExposedRoutePhaseCertIssued ExposedRoutePhase = "CertIssued"
	ExposedRoutePhaseReachable  ExposedRoutePhase = "Reachable"
	ExposedRoutePhaseFailed     ExposedRoutePhase = "Failed"
)

// ExposedRoute is the Schema for the exposedroutes API.
// An ExposedRoute represents DNS/TLS exposure for a LoopRuntimeApplication,
// managing certificate status and reachability probes.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Hostname",type=string,JSONPath=`.spec.hostname`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ExposedRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExposedRouteSpec   `json:"spec,omitempty"`
	Status ExposedRouteStatus `json:"status,omitempty"`
}

// ExposedRouteSpec defines the desired state of an ExposedRoute.
type ExposedRouteSpec struct {
	// ApplicationRef is the name of the parent LoopRuntimeApplication CR.
	// +kubebuilder:validation:MinLength=1
	ApplicationRef string `json:"applicationRef"`

	// Hostname is the external hostname for this route.
	// +kubebuilder:validation:MinLength=1
	Hostname string `json:"hostname"`

	// ServiceRef references the backing Kubernetes Service.
	ServiceRef ServiceRef `json:"serviceRef"`

	// TLS configures TLS termination for this route.
	// +optional
	TLS TLSConfig `json:"tls,omitempty"`
}

// ServiceRef references a Kubernetes Service by name and port.
type ServiceRef struct {
	// Name is the Service name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Port is the Service port.
	// +kubebuilder:default=80
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
}

// TLSConfig configures TLS for an ExposedRoute.
type TLSConfig struct {
	// Enabled indicates whether TLS is enabled.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// IssuerRef is the cert-manager issuer name.
	// +optional
	IssuerRef string `json:"issuerRef,omitempty"`
}

// ExposedRouteStatus defines the observed state of an ExposedRoute.
type ExposedRouteStatus struct {
	// Phase represents the overall route lifecycle phase.
	Phase ExposedRoutePhase `json:"phase,omitempty"`

	// CertificateReady indicates whether the TLS certificate is ready.
	CertificateReady bool `json:"certificateReady,omitempty"`

	// LastProbeTime is the time of the last reachability probe.
	// +optional
	LastProbeTime *metav1.Time `json:"lastProbeTime,omitempty"`

	// LastProbeResult is the HTTP status code of the last probe (0 if no probe yet).
	LastProbeResult int32 `json:"lastProbeResult,omitempty"`

	// ConsecutiveFailures tracks the number of consecutive failed probes.
	ConsecutiveFailures int32 `json:"consecutiveFailures,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the route's state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ExposedRouteList contains a list of ExposedRoute resources.
//
// +kubebuilder:object:root=true
type ExposedRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExposedRoute `json:"items"`
}

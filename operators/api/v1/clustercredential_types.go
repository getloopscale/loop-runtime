// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterCredential is the Schema for the clustercredentials API.
// It manages workload cluster kubeconfigs and distributes minimum-scope
// credentials to ArgoCD. See spec section 7.3.3.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterName`
// +kubebuilder:printcolumn:name="Connected",type=boolean,JSONPath=`.status.connected`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ClusterCredential struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterCredentialSpec   `json:"spec,omitempty"`
	Status ClusterCredentialStatus `json:"status,omitempty"`
}

// ClusterCredentialSpec defines the desired state of a ClusterCredential.
type ClusterCredentialSpec struct {
	// ClusterName is the name of the target cluster.
	// +kubebuilder:validation:MinLength=1
	ClusterName string `json:"clusterName"`

	// APIServer is the URL of the target cluster's API server.
	// Must be a valid HTTPS URL (e.g., "https://cluster.example.com:6443").
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^https?://`
	APIServer string `json:"apiServer"`

	// SecretRef references the Kubernetes Secret containing the kubeconfig.
	SecretRef SecretReference `json:"secretRef"`

	// Scope defines what components are allowed to use these credentials.
	Scope CredentialScope `json:"scope"`

	// TLS configures TLS verification for the target cluster API server.
	// +optional
	TLS ClusterTLSConfig `json:"tls,omitempty"`

	// CellID identifies which Cell this cluster belongs to.
	// Used by CellRouter to discover available Cells and their kubeconfigs.
	// ADR-0008 Phase 1.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^cell-[a-z0-9-]+$`
	// +kubebuilder:default="cell-0"
	CellID string `json:"cellID,omitempty"`
}

// CredentialScope defines which components can use a ClusterCredential.
type CredentialScope struct {
	// ArgoCD indicates whether ArgoCD should receive these credentials.
	ArgoCD bool `json:"argocd"`

	// Operators indicates whether operators should receive these credentials.
	Operators bool `json:"operators"`
}

// ClusterTLSConfig configures TLS verification for a cluster connection.
type ClusterTLSConfig struct {
	// Insecure disables TLS certificate verification for the ArgoCD cluster secret.
	// Only enable for development clusters with self-signed certificates.
	// +kubebuilder:default=false
	// +optional
	Insecure bool `json:"insecure,omitempty"`
}

// ClusterCredentialStatus defines the observed state of a ClusterCredential.
type ClusterCredentialStatus struct {
	// Connected indicates whether the target cluster is reachable.
	//
	// +optional deliberately, and the JSON tag deliberately keeps no omitempty.
	//
	// For a plain bool, absent and false are the SAME value — requiring it bought
	// no distinction (that would need *bool), while it made every partial write
	// invalid: a merge patch omits a field equal in base and object, and applying
	// such a patch to an object whose status was never written produces a status
	// without `connected`, which the apiserver rejects. Measured against envtest
	// while converting this controller to the merge-patch contract (B-085).
	//
	// The default is what keeps the `Connected` printcolumn from going blank, and
	// the first version of this change got that wrong. Dropping `required` alone
	// is not enough: a merge patch sends the DIFF, not the marshalled status, so
	// `connected` is omitted whenever it did not change — and an object whose
	// status was never written then stores no key at all. Measured against a real
	// apiserver, `kubectl get` printed an empty cell for every ClusterCredential
	// that had not yet connected. The default makes the server materialise the
	// field on write, so the column reads false again.
	//
	// Decision: MEET-ASYNC-AMENDMENT-2026-08-13-clustercredential-connected-optional
	// +optional
	// +kubebuilder:default=false
	Connected bool `json:"connected"`

	// LastCheckedAt is the last time connectivity was verified.
	LastCheckedAt *metav1.Time `json:"lastCheckedAt,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// MEET-053 D-03: Enables consumers to check "has the controller processed my latest spec change?"
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ClusterCredentialList contains a list of ClusterCredential resources.
//
// +kubebuilder:object:root=true
type ClusterCredentialList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterCredential `json:"items"`
}

// SecretReference identifies a Kubernetes Secret by name and namespace.
// Originally lived in appsecret_types.go; moved here after the AppSecret CRD
// deletion (ADR 2026-05-13) because ClusterCredentialSpec.SecretRef is the
// only remaining consumer.
type SecretReference struct {
	// Name is the name of the source Secret.
	Name string `json:"name"`

	// Namespace is the namespace of the source Secret.
	Namespace string `json:"namespace"`
}

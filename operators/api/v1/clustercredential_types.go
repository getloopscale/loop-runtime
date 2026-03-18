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
	// +kubebuilder:validation:MinLength=1
	APIServer string `json:"apiServer"`

	// SecretRef references the Kubernetes Secret containing the kubeconfig.
	SecretRef SecretReference `json:"secretRef"`

	// Scope defines what components are allowed to use these credentials.
	Scope CredentialScope `json:"scope"`
}

// CredentialScope defines which components can use a ClusterCredential.
type CredentialScope struct {
	// ArgoCD indicates whether ArgoCD should receive these credentials.
	ArgoCD bool `json:"argocd"`

	// Operators indicates whether operators should receive these credentials.
	Operators bool `json:"operators"`
}

// ClusterCredentialStatus defines the observed state of a ClusterCredential.
type ClusterCredentialStatus struct {
	// Connected indicates whether the target cluster is reachable.
	Connected bool `json:"connected"`

	// LastCheckedAt is the last time connectivity was verified.
	LastCheckedAt *metav1.Time `json:"lastCheckedAt,omitempty"`

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

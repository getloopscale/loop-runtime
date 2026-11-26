// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GitRepositoryBinding binds a Git repository to a consumer (e.g., ArgoCD)
// by creating the appropriate credential Secret in the consumer's namespace.
// One binding per (repo, consumer) pair. TenantController creates these CRs
// with ownerRef to Tenant.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=grb
// +kubebuilder:printcolumn:name="RepoURL",type=string,JSONPath=`.spec.repoURL`
// +kubebuilder:printcolumn:name="Consumer",type=string,JSONPath=`.spec.consumer.type`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type GitRepositoryBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitRepositoryBindingSpec   `json:"spec,omitempty"`
	Status GitRepositoryBindingStatus `json:"status,omitempty"`
}

// GitRepositoryBindingSpec defines the desired state of a GitRepositoryBinding.
type GitRepositoryBindingSpec struct {
	// RepoURL is the Git repository URL to register with the consumer.
	// SEC-003 / T3.5: must be an HTTP(S) URL or SSH git URL. Prevents SSRF
	// via arbitrary URL schemes (file://, gopher://, javascript:, etc.) reaching
	// the HTTP client used by GitRepositoryBindingController. Plain http://
	// is allowed for in-cluster Gitea services (e.g., http://gitea.loopruntime-system.svc:3000).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:Pattern=`^(https?://[^\s]+|ssh://git@[^\s]+|git@[^\s:]+:[^\s]+)$`
	RepoURL string `json:"repoURL"`

	// CredentialSecretRef references the Secret containing the Git credential.
	CredentialSecretRef CredentialSecretRef `json:"credentialSecretRef"`

	// Consumer defines the target system that will receive the repository credential.
	Consumer ConsumerSpec `json:"consumer"`
}

// CredentialSecretRef references a specific key within a Kubernetes Secret.
type CredentialSecretRef struct {
	// Name is the Secret name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key is the data key within the Secret that holds the credential token.
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`

	// Namespace is the namespace of the Secret.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

// ConsumerSpec defines the target consumer for the repository binding.
type ConsumerSpec struct {
	// Type is the consumer type. Currently only "argocd" is supported.
	// +kubebuilder:validation:Enum=argocd
	Type string `json:"type"`

	// Namespace is the namespace where the consumer expects its secrets.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

// GitRepositoryBindingStatus defines the observed state of a GitRepositoryBinding.
type GitRepositoryBindingStatus struct {
	// CredentialHash is the sha256 hash prefix of the current credential token.
	// Used to detect rotation: if the hash changes, the ArgoCD Secret is updated.
	// +optional
	CredentialHash string `json:"credentialHash,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the binding's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GitRepositoryBindingList contains a list of GitRepositoryBinding resources.
//
// +kubebuilder:object:root=true
type GitRepositoryBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitRepositoryBinding `json:"items"`
}

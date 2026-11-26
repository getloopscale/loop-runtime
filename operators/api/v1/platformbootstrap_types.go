// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PlatformBootstrapPhase represents the current phase of the bootstrap process.
type PlatformBootstrapPhase string

const (
	// PlatformBootstrapPhasePending is the initial phase before reconciliation starts.
	PlatformBootstrapPhasePending PlatformBootstrapPhase = "Pending"

	// PlatformBootstrapPhaseConfiguringGitea indicates the controller is setting up Gitea
	// (validating admin token, creating organizations, storing credentials).
	PlatformBootstrapPhaseConfiguringGitea PlatformBootstrapPhase = "ConfiguringGitea"

	// PlatformBootstrapPhaseConfiguringArgoCD indicates the controller is setting up ArgoCD
	// (generating JWT, registering repo credentials).
	PlatformBootstrapPhaseConfiguringArgoCD PlatformBootstrapPhase = "ConfiguringArgoCD"

	// PlatformBootstrapPhaseWaitingForAPI indicates the controller is waiting for the API
	// to be healthy and then calling the bootstrap endpoint for DB seed.
	PlatformBootstrapPhaseWaitingForAPI PlatformBootstrapPhase = "WaitingForAPI"

	// PlatformBootstrapPhaseComplete indicates bootstrap finished successfully.
	// This is a terminal phase — the controller will not re-bootstrap automatically.
	PlatformBootstrapPhaseComplete PlatformBootstrapPhase = "BootstrapComplete"

	// PlatformBootstrapPhaseFailed indicates bootstrap failed after exhausting retries.
	// Requires manual intervention: delete and recreate the CR, or set annotation
	// platform.runtime.getloopscale.com/force-retry: "true".
	PlatformBootstrapPhaseFailed PlatformBootstrapPhase = "Failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=pbs
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PlatformBootstrap describes the desired state of the platform's bootstrap process.
// It replaces the imperative shell scripts in the CI/CD pipeline with a declarative,
// reconciled, idempotent controller.
type PlatformBootstrap struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlatformBootstrapSpec   `json:"spec,omitempty"`
	Status PlatformBootstrapStatus `json:"status,omitempty"`
}

// PlatformBootstrapSpec defines the desired bootstrap configuration.
type PlatformBootstrapSpec struct {
	// Gitea configures the Gitea git server bootstrap.
	Gitea GiteaBootstrapSpec `json:"gitea"`

	// ArgoCD configures the ArgoCD bootstrap.
	ArgoCD ArgoCDBootstrapSpec `json:"argocd"`

	// API configures the API bootstrap endpoint for DB seeding.
	API APIBootstrapSpec `json:"api"`

	// MaxRetries is the maximum number of retries per phase before transitioning to Failed.
	// +kubebuilder:default=10
	// +optional
	MaxRetries int `json:"maxRetries,omitempty"`
}

// GiteaBootstrapSpec configures Gitea bootstrap.
type GiteaBootstrapSpec struct {
	// AdminSecretRef references the Secret containing Gitea admin credentials.
	// The Secret must contain 'username' and 'password' keys, OR a 'token' key
	// with a pre-existing admin API token.
	AdminSecretRef SecretKeyRef `json:"adminSecretRef"`

	// Organizations is the list of Gitea organizations to create.
	// +optional
	Organizations []string `json:"organizations,omitempty"`

	// TokenName is the name for the admin API token to create.
	// Used for idempotent token creation (check by name before creating).
	// +kubebuilder:default="platform-bootstrap-token"
	// +optional
	TokenName string `json:"tokenName,omitempty"`

	// TokenScopes defines the minimum required scopes for the admin token.
	// +optional
	TokenScopes []string `json:"tokenScopes,omitempty"`

	// InternalURL is the cluster-internal URL for Gitea HTTP API.
	// SEC-004: Validated to prevent SSRF via arbitrary URL redirection.
	// +kubebuilder:default="http://gitea-http.loopruntime-system.svc.cluster.local:3000"
	// +kubebuilder:validation:Pattern=`^https?://[a-zA-Z0-9][a-zA-Z0-9.\-]+(:[0-9]+)?(/.*)?$`
	// +optional
	InternalURL string `json:"internalURL,omitempty"`
}

// ArgoCDBootstrapSpec configures ArgoCD bootstrap.
type ArgoCDBootstrapSpec struct {
	// Namespace is where ArgoCD is installed.
	// +kubebuilder:default="argocd"
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// AdminSecretName is the name of the Secret containing ArgoCD admin password.
	// Created automatically by the ArgoCD Helm chart.
	// +kubebuilder:default="argocd-initial-admin-secret"
	// +optional
	AdminSecretName string `json:"adminSecretName,omitempty"`

	// RepoCredentials defines the repo credential secrets to create for ArgoCD.
	// +optional
	RepoCredentials []ArgoCDRepoCredential `json:"repoCredentials,omitempty"`

	// InternalURL is the cluster-internal URL for ArgoCD API.
	// SEC-004: Validated to prevent SSRF via arbitrary URL redirection.
	// +kubebuilder:default="https://argocd-server.argocd.svc.cluster.local"
	// +kubebuilder:validation:Pattern=`^https?://[a-zA-Z0-9][a-zA-Z0-9.\-]+(:[0-9]+)?(/.*)?$`
	// +optional
	InternalURL string `json:"internalURL,omitempty"`
}

// ArgoCDRepoCredential defines a repo credential to register in ArgoCD.
type ArgoCDRepoCredential struct {
	// Name is the Secret name in the ArgoCD namespace.
	Name string `json:"name"`

	// URL is the Git server URL pattern.
	URL string `json:"url"`
}

// APIBootstrapSpec configures the API bootstrap endpoint.
type APIBootstrapSpec struct {
	// BootstrapEndpoint is the internal URL for the bootstrap endpoint.
	// SEC-004: Validated to prevent SSRF via arbitrary URL redirection.
	// +kubebuilder:default="http://loopruntime-api.loopruntime-system.svc.cluster.local:8080/internal/bootstrap"
	// +kubebuilder:validation:Pattern=`^https?://[a-zA-Z0-9][a-zA-Z0-9.\-]+(:[0-9]+)?(/.*)?$`
	// +optional
	BootstrapEndpoint string `json:"bootstrapEndpoint,omitempty"`

	// HealthEndpoint is the internal URL for the API health check.
	// SEC-004: Validated to prevent SSRF via arbitrary URL redirection.
	// +kubebuilder:default="http://loopruntime-api.loopruntime-system.svc.cluster.local:8080/api/v1/health"
	// +kubebuilder:validation:Pattern=`^https?://[a-zA-Z0-9][a-zA-Z0-9.\-]+(:[0-9]+)?(/.*)?$`
	// +optional
	HealthEndpoint string `json:"healthEndpoint,omitempty"`

	// BootstrapSecretRef references the Secret containing the bootstrap auth token.
	// The Secret must contain a key "bootstrap-secret" with the Bearer token value
	// matching LOOPRUNTIME_BOOTSTRAP_SECRET on the API side (SEC-003).
	// B1: Without this, callBootstrapEndpoint fails 401 when SEC-003 is enabled.
	// +optional
	BootstrapSecretRef *SecretKeyRef `json:"bootstrapSecretRef,omitempty"`
}

// SecretKeyRef references a Kubernetes Secret.
type SecretKeyRef struct {
	// Name is the Secret name.
	Name string `json:"name"`
}

// PlatformBootstrapStatus defines the observed state of PlatformBootstrap.
type PlatformBootstrapStatus struct {
	// Phase is the current phase of the bootstrap process.
	// +optional
	Phase PlatformBootstrapPhase `json:"phase,omitempty"`

	// RetryCount tracks how many retries have been attempted in the current phase.
	// +optional
	RetryCount int `json:"retryCount,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the bootstrap's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastTransitionTime is when the phase last changed.
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// +kubebuilder:object:root=true

// PlatformBootstrapList contains a list of PlatformBootstrap.
type PlatformBootstrapList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PlatformBootstrap `json:"items"`
}

// DeepCopy methods for PlatformBootstrap types are generated by controller-gen
// in zz_generated.deepcopy.go. Run `make generate` to regenerate.

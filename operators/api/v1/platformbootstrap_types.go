package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	// platform.loopzero.io/force-retry: "true".
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

	// Timeouts configures per-phase timeouts in seconds.
	// +optional
	Timeouts BootstrapTimeouts `json:"timeouts,omitempty"`

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
	// +kubebuilder:default="http://gitea-http.platform-system.svc.cluster.local:3000"
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

	// TokenTTLSeconds is the TTL for the generated ArgoCD JWT.
	// +kubebuilder:default=7776000
	// +optional
	TokenTTLSeconds int64 `json:"tokenTTLSeconds,omitempty"`

	// RepoCredentials defines the repo credential secrets to create for ArgoCD.
	// +optional
	RepoCredentials []ArgoCDRepoCredential `json:"repoCredentials,omitempty"`

	// InternalURL is the cluster-internal URL for ArgoCD API.
	// +kubebuilder:default="https://argocd-server.argocd.svc.cluster.local"
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
	// +kubebuilder:default="http://loopzero-api.platform-system.svc.cluster.local:8080/internal/bootstrap"
	// +optional
	BootstrapEndpoint string `json:"bootstrapEndpoint,omitempty"`

	// HealthEndpoint is the internal URL for the API health check.
	// +kubebuilder:default="http://loopzero-api.platform-system.svc.cluster.local:8080/api/v1/health"
	// +optional
	HealthEndpoint string `json:"healthEndpoint,omitempty"`
}

// BootstrapTimeouts configures per-phase timeouts.
type BootstrapTimeouts struct {
	// GiteaReadySeconds is how long to wait for Gitea to be reachable.
	// +kubebuilder:default=120
	// +optional
	GiteaReadySeconds int `json:"giteaReadySeconds,omitempty"`

	// ArgoCDReadySeconds is how long to wait for ArgoCD to be reachable.
	// +kubebuilder:default=120
	// +optional
	ArgoCDReadySeconds int `json:"argoCDReadySeconds,omitempty"`

	// APIReadySeconds is how long to wait for the API to be healthy.
	// +kubebuilder:default=120
	// +optional
	APIReadySeconds int `json:"apiReadySeconds,omitempty"`

	// TotalBootstrapSeconds is the overall timeout for the entire bootstrap process.
	// +kubebuilder:default=600
	// +optional
	TotalBootstrapSeconds int `json:"totalBootstrapSeconds,omitempty"`
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

	// Message provides human-readable detail about the current phase.
	// +optional
	Message string `json:"message,omitempty"`

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

// DeepCopyObject implements runtime.Object for PlatformBootstrap.
func (in *PlatformBootstrap) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(PlatformBootstrap)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into another PlatformBootstrap.
func (in *PlatformBootstrap) DeepCopyInto(out *PlatformBootstrap) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopyObject implements runtime.Object for PlatformBootstrapList.
func (in *PlatformBootstrapList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(PlatformBootstrapList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into another PlatformBootstrapList.
func (in *PlatformBootstrapList) DeepCopyInto(out *PlatformBootstrapList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]PlatformBootstrap, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopyInto copies PlatformBootstrapSpec fields.
func (in *PlatformBootstrapSpec) DeepCopyInto(out *PlatformBootstrapSpec) {
	*out = *in
	in.Gitea.DeepCopyInto(&out.Gitea)
	in.ArgoCD.DeepCopyInto(&out.ArgoCD)
	out.API = in.API
	out.Timeouts = in.Timeouts
}

// DeepCopyInto copies GiteaBootstrapSpec fields.
func (in *GiteaBootstrapSpec) DeepCopyInto(out *GiteaBootstrapSpec) {
	*out = *in
	out.AdminSecretRef = in.AdminSecretRef
	if in.Organizations != nil {
		out.Organizations = make([]string, len(in.Organizations))
		copy(out.Organizations, in.Organizations)
	}
	if in.TokenScopes != nil {
		out.TokenScopes = make([]string, len(in.TokenScopes))
		copy(out.TokenScopes, in.TokenScopes)
	}
}

// DeepCopyInto copies ArgoCDBootstrapSpec fields.
func (in *ArgoCDBootstrapSpec) DeepCopyInto(out *ArgoCDBootstrapSpec) {
	*out = *in
	if in.RepoCredentials != nil {
		out.RepoCredentials = make([]ArgoCDRepoCredential, len(in.RepoCredentials))
		copy(out.RepoCredentials, in.RepoCredentials)
	}
}

// DeepCopyInto copies PlatformBootstrapStatus fields.
func (in *PlatformBootstrapStatus) DeepCopyInto(out *PlatformBootstrapStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
	if in.LastTransitionTime != nil {
		out.LastTransitionTime = in.LastTransitionTime.DeepCopy()
	}
}

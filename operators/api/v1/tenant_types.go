// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package v1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PlanTier represents a tenant's subscription plan.
// +kubebuilder:validation:Enum=starter;growth;enterprise
type PlanTier string

const (
	PlanStarter    PlanTier = "starter"
	PlanGrowth     PlanTier = "growth"
	PlanEnterprise PlanTier = "enterprise"
)

// TenantPhase represents the overall lifecycle phase of a Tenant.
// +kubebuilder:validation:Enum=Provisioning;Active;Suspended;Deleting
type TenantPhase string

const (
	TenantPhaseProvisioning TenantPhase = "Provisioning"
	TenantPhaseActive       TenantPhase = "Active"
	TenantPhaseSuspended    TenantPhase = "Suspended"
	TenantPhaseDeleting     TenantPhase = "Deleting"
)

// Tenant is the Schema for the tenants API.
// A Tenant represents a team/account in LoopZero, with its own namespaces,
// RBAC, quotas, and GitOps repository.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Plan",type=string,JSONPath=`.spec.plan`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Tenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantSpec   `json:"spec,omitempty"`
	Status TenantStatus `json:"status,omitempty"`
}

// TenantSpec defines the desired state of a Tenant as described in spec section 9.2.
type TenantSpec struct {
	// DisplayName is the human-readable name of the tenant.
	// SEC-011: Pattern constraint prevents injection of control characters or markup.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9][a-zA-Z0-9 _.\-]*$`
	DisplayName string `json:"displayName"`

	// Plan is the subscription tier: starter, growth, or enterprise.
	Plan PlanTier `json:"plan"`

	// Environments lists the environments available to this tenant.
	// +kubebuilder:validation:MinItems=1
	Environments []TenantEnvironmentEntry `json:"environments"`

	// GitOps configures the tenant's GitOps repository.
	GitOps GitOpsSpec `json:"gitops"`

	// Preview configures preview environment limits.
	Preview PreviewSpec `json:"preview"`

	// Limits defines resource limits for the tenant.
	Limits ResourceLimits `json:"limits"`

	// CellID identifies which Cell this tenant's workloads run in.
	// ADR-0008: Phase 0 defaults to "cell-default" (single Cell).
	// Phase 1+ PlacementEngine assigns tenants to specific Cells.
	// +kubebuilder:default="cell-default"
	// +kubebuilder:validation:MinLength=1
	CellID string `json:"cellID,omitempty"`
}

// TenantEnvironmentEntry defines an environment reference within a Tenant spec.
// ARCH-004: Renamed from EnvironmentSpec to resolve naming collision with EnvironmentCRSpec.
type TenantEnvironmentEntry struct {
	// Name is the environment name (e.g., "production", "staging").
	Name string `json:"name"`

	// Cluster is the target cluster for this environment.
	Cluster string `json:"cluster"`
}

// GitOpsSpec configures the tenant's GitOps repository in Gitea.
type GitOpsSpec struct {
	// RepoName is the name of the GitOps repository in Gitea.
	// +kubebuilder:validation:MinLength=1
	RepoName string `json:"repoName"`

	// Branch is the branch used for GitOps (default: "main").
	// +kubebuilder:validation:MinLength=1
	Branch string `json:"branch"`
}

// PreviewSpec configures preview environment limits.
type PreviewSpec struct {
	// MaxEnvironments is the maximum number of concurrent preview environments.
	MaxEnvironments int `json:"maxEnvironments"`

	// TTLHours is the default time-to-live for preview environments in hours.
	TTLHours int `json:"ttlHours"`
}

// ResourceLimits defines resource limits for a tenant as described in spec section 9.2.
type ResourceLimits struct {
	// MaxCPU is the maximum CPU allowed across all tenant namespaces.
	MaxCPU resource.Quantity `json:"maxCPU"`

	// MaxMemory is the maximum memory allowed across all tenant namespaces.
	MaxMemory resource.Quantity `json:"maxMemory"`

	// MaxStorage is the maximum storage allowed across all tenant namespaces.
	MaxStorage resource.Quantity `json:"maxStorage"`

	// MaxBuildConcurrency is the maximum number of concurrent builds.
	// +kubebuilder:validation:Minimum=1
	MaxBuildConcurrency int `json:"maxBuildConcurrency"`
}

// TenantStatus defines the observed state of a Tenant.
type TenantStatus struct {
	// Phase represents the overall tenant lifecycle phase.
	Phase TenantPhase `json:"phase,omitempty"`

	// GitOpsRepo is the URL of the provisioned GitOps repository.
	GitOpsRepo string `json:"gitopsRepo,omitempty"`

	// Conditions represent the latest available observations of the tenant's state.
	// Condition types: GitRepoReady, NamespacesReady, RBACReady, QuotaEnforced,
	// NetworkPolicyReady, BuildNamespaceReady.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// TenantList contains a list of Tenant resources.
//
// +kubebuilder:object:root=true
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tenant `json:"items"`
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PlanTier represents a tenant's subscription plan, in the canonical
// vocabulary the api writes (pkg/domain.PlanTier): starter, pro, team,
// enterprise. "growth" is the pre-2026-05-12 name for pro and stays valid so
// stored CRs carrying it still pass validation on update.
// +kubebuilder:validation:Enum=starter;pro;team;enterprise;growth
type PlanTier string

const (
	PlanStarter    PlanTier = "starter"
	PlanPro        PlanTier = "pro"
	PlanTeam       PlanTier = "team"
	PlanEnterprise PlanTier = "enterprise"
	// PlanGrowth is the legacy name for PlanPro.
	PlanGrowth PlanTier = "growth"
)

// TenantPhase represents the overall lifecycle phase of a Tenant.
// +kubebuilder:validation:Enum=Provisioning;Active;Suspended;PendingDeletion;Deleting
type TenantPhase string

const (
	TenantPhaseProvisioning    TenantPhase = "Provisioning"
	TenantPhaseActive          TenantPhase = "Active"
	TenantPhaseSuspended       TenantPhase = "Suspended"
	TenantPhasePendingDeletion TenantPhase = "PendingDeletion" // MEET-052: soft delete grace period
	TenantPhaseDeleting        TenantPhase = "Deleting"
)

// Tenant is the Schema for the tenants API.
// A Tenant represents a team/account in LoopRuntime, with its own namespaces,
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

	// Plan is the subscription tier: starter, pro, team or enterprise
	// (growth is accepted as the legacy name for pro).
	Plan PlanTier `json:"plan"`

	// Environments lists the environments available to this tenant.
	//
	// Shape-only constraint (MinItems=0): the "tenant has production from
	// day 1" business invariant lives in the API layer (T4.1 of plan
	// 2026-06-09-m2-operator-canonical-fix — EnvironmentEnsurer auto-creates
	// the production env immediately after CreateProject + EnsureTenant).
	// Forcing MinItems=1 here previously required factories to pre-populate
	// a hardcoded environments[] entry; combined with the T1.1 Project-required
	// constraint this caused admission failures in the EnsureTenant call
	// path (LIVE-caught 2026-06-09 during T6.1, per
	// feedback_no_hardcode_gate_2026_06_09). The CRD validates SHAPE, the
	// API enforces BUSINESS INVARIANT — separating those concerns lets
	// EnsureTenant create an empty CR while the canonical T4.1 chain
	// remains the single source of truth for env entries.
	// +kubebuilder:validation:MinItems=0
	Environments []TenantEnvironmentEntry `json:"environments"`

	// GitOps configures the tenant's GitOps repository.
	GitOps GitOpsSpec `json:"gitops"`

	// Preview configures preview environment limits.
	Preview PreviewSpec `json:"preview"`

	// Limits defines resource limits for the tenant.
	Limits ResourceLimits `json:"limits"`

	// CellID identifies which Cell this tenant's workloads run in.
	// ADR-0008: Phase 0 defaults to "cell-0" (single Cell).
	// Phase 1+ PlacementEngine assigns tenants to specific Cells.
	// +kubebuilder:default="cell-0"
	// +kubebuilder:validation:MinLength=1
	CellID string `json:"cellID,omitempty"`
}

// TenantEnvironmentEntry defines an environment reference within a Tenant spec.
// ARCH-004: Renamed from EnvironmentSpec to resolve naming collision with EnvironmentCRSpec.
type TenantEnvironmentEntry struct {
	// Name is the environment name (e.g., "production", "staging").
	// SEC-002 / T3.5: RFC 1123 label — alphanumeric + hyphens, must start
	// with alphanumeric, length 1-63. Prevents injection vectors via
	// environment names propagated to namespace and label values.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// Cluster is the target cluster for this environment.
	// SEC-002 / T3.5: RFC 1123 label, same constraints as Name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Cluster string `json:"cluster"`

	// Project is the project segment of the canonical namespace formula
	// `loop-runtime-<tenant>-<project>-<env>`. J-23 LIVE catch 2026-06-07
	// (#69, sponsor verbatim "Atacar #69 fix tenant_gitops + CRD"): the
	// renderer canonical refactor (commit a567cdbb5) emits manifests with
	// namespace `loop-runtime-<tenant>-<project>-<env>` for ALL envs. The API
	// populates it when creating / updating the Tenant CR
	// (api/internal/k8s/tenant_cr_manager.go), the operator reads it in
	// reconcileArgoCDApp (tenant_gitops.go) to compute the canonical
	// destination, and ArgoCD sync applies workloads into the correct
	// namespace.
	//
	// T1.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4: required +
	// RFC 1123 Pattern at the CRD level. The Optional-with-fallback path
	// (pre-#69 legacy two-segment naming) was eliminated by Phase 5
	// migration (T5.1 of the same plan) running BEFORE this validation
	// tightens per ADR D5. EC-2 absorption: Pattern catches non-RFC-1123
	// slugs at the request boundary instead of leaving the Environment
	// stuck Pending with a cryptic error surfaced by pkg/naming.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Project string `json:"project"`
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

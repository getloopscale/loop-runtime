// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnvironmentPhase represents the lifecycle phase of an Environment.
// +kubebuilder:validation:Enum=Pending;Active;Deleting;Failed
type EnvironmentPhase string

const (
	EnvironmentPhasePending  EnvironmentPhase = "Pending"
	EnvironmentPhaseActive   EnvironmentPhase = "Active"
	EnvironmentPhaseDeleting EnvironmentPhase = "Deleting"
	EnvironmentPhaseFailed   EnvironmentPhase = "Failed"
)

// Environment is the Schema for the environments API.
// An Environment represents a single deployment target (e.g., production, staging)
// within a Tenant. It manages the namespace, RBAC, NetworkPolicy, and ResourceQuota
// for that environment.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.namespace`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Environment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EnvironmentCRSpec   `json:"spec,omitempty"`
	Status EnvironmentCRStatus `json:"status,omitempty"`
}

// EnvironmentCRSpec defines the desired state of an Environment.
// ARCH-004: The naming collision is resolved — EnvironmentSpec was renamed to
// TenantEnvironmentEntry in tenant_types.go. This type can be renamed to
// EnvironmentSpec in a future cleanup if desired.
type EnvironmentCRSpec struct {
	// TenantRef references the parent Tenant CR by name.
	TenantRef TenantRef `json:"tenantRef"`

	// Environment is the environment name (e.g., "production", "staging").
	// +kubebuilder:validation:MinLength=1
	Environment string `json:"environment"`

	// Project is the project segment of the canonical namespace formula
	// `loopzero-<tenant>-<project>-<env>`. T3.1 of plan
	// 2026-06-09-m2-operator-canonical-fix v1.4: when populated by the
	// TenantController (from TenantEnvironmentEntry.Project — which T1.1
	// makes required at the CRD level), EnvironmentController computes the
	// canonical M2 namespace from this field + Environment instead of
	// trusting the Namespace.Name value. Optional at the EnvironmentCRSpec
	// level for backward-compatibility with pre-T3.1 CRs whose namespace
	// was hand-set via Spec.Namespace.Name; new CRs created by the
	// post-T3.1 TenantController populate it unconditionally.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$`
	Project string `json:"project,omitempty"`

	// Namespace defines the target namespace for this environment.
	Namespace NamespaceSpec `json:"namespace"`

	// ResourceQuota defines optional resource limits for the namespace.
	// +optional
	ResourceQuota *ResourceQuotaSpec `json:"resourceQuota,omitempty"`

	// NetworkPolicy configures network isolation for the namespace.
	NetworkPolicy NetworkPolicyConfig `json:"networkPolicy"`

	// Plan is the tenant's subscription tier, used to determine RBAC rules.
	Plan PlanTier `json:"plan"`
}

// TenantRef references a Tenant CR by name.
type TenantRef struct {
	// Name is the name of the Tenant CR.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// NamespaceSpec defines the target namespace name.
type NamespaceSpec struct {
	// Name is the explicit namespace name (e.g., "loopzero-loopzero-production").
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ResourceQuotaSpec defines resource limits for the environment namespace.
type ResourceQuotaSpec struct {
	// CPU is the maximum CPU allowed (e.g., "8").
	CPU string `json:"cpu"`

	// Memory is the maximum memory allowed (e.g., "16Gi").
	Memory string `json:"memory"`

	// Storage is the maximum storage allowed (e.g., "50Gi").
	Storage string `json:"storage"`
}

// NetworkPolicyConfig configures network isolation for the environment.
type NetworkPolicyConfig struct {
	// Isolate enables network isolation (deny-all + allow-ingress + allow-egress).
	// +kubebuilder:default=true
	Isolate bool `json:"isolate"`
}

// EnvironmentCRStatus defines the observed state of an Environment.
type EnvironmentCRStatus struct {
	// Phase represents the overall environment lifecycle phase.
	Phase EnvironmentPhase `json:"phase,omitempty"`

	// Namespace is the actual namespace name managed by this Environment.
	Namespace string `json:"namespace,omitempty"`

	// CellID is the cell whose runtime cluster holds Namespace, recorded when
	// the namespace is provisioned. Deletion reads it so the namespace is
	// removed where it was created, even after the Tenant was re-pinned to
	// another cell or is already gone.
	CellID string `json:"cellID,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the environment's state.
	// Condition types: NamespaceReady, RBACReady, NetworkPolicyReady, QuotaReady
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EnvironmentList contains a list of Environment resources.
//
// +kubebuilder:object:root=true
type EnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Environment `json:"items"`
}

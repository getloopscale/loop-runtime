// Package v1 contains API types for the loopzero.io API group version v1.
//
// These types define the Custom Resource Definitions (CRDs) used by
// LoopZero's Kubernetes operators: Tenant, PreviewEnvironment, AppSecret,
// ClusterCredential, and ReconciliationReport.
//
// +kubebuilder:object:generate=true
// +groupName=loopzero.io
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/loopzero/loopzero/operators/internal/conditions"
)

// GroupVersion is the API group and version for LoopZero CRDs.
var GroupVersion = schema.GroupVersion{Group: "loopzero.io", Version: "v1"}

// SchemeBuilder is used to add types to the scheme.
var SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

// AddToScheme adds the types in this group-version to the given scheme.
var AddToScheme = SchemeBuilder.AddToScheme

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&Tenant{},
		&TenantList{},
		&PreviewEnvironment{},
		&PreviewEnvironmentList{},
		&AppSecret{},
		&AppSecretList{},
		&ClusterCredential{},
		&ClusterCredentialList{},
		&ReconciliationReport{},
		&ReconciliationReportList{},
		&PlatformBootstrap{},
		&PlatformBootstrapList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}

// Resource takes an unqualified resource name and returns a GroupResource.
func Resource(resource string) schema.GroupResource {
	return GroupVersion.WithResource(resource).GroupResource()
}

// ConditionType constants for Tenant status conditions.
// Re-exported from internal/conditions for backward compatibility.
const (
	ConditionGitRepoReady        = conditions.TypeGitRepoReady
	ConditionNamespacesReady     = conditions.TypeNamespacesReady
	ConditionRBACReady           = conditions.TypeRBACReady
	ConditionQuotaEnforced       = conditions.TypeQuotaEnforced
	ConditionNetworkPolicyReady  = conditions.TypeNetworkPolicyReady
	ConditionBuildNamespaceReady = conditions.TypeBuildNamespaceReady
	ConditionPriorityClassReady  = conditions.TypePriorityClassReady
	ConditionArgoCDAppReady      = conditions.TypeArgoCDAppReady
)

// SetCondition delegates to the conditions package.
// Kept for backward compatibility with test code and external consumers.
var SetCondition = conditions.SetCondition

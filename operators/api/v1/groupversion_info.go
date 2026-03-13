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
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}

// Resource takes an unqualified resource name and returns a GroupResource.
func Resource(resource string) schema.GroupResource {
	return GroupVersion.WithResource(resource).GroupResource()
}

// ConditionType constants for Tenant status conditions.
const (
	ConditionGitRepoReady       string = "GitRepoReady"
	ConditionNamespacesReady    string = "NamespacesReady"
	ConditionRBACReady          string = "RBACReady"
	ConditionQuotaEnforced      string = "QuotaEnforced"
	ConditionNetworkPolicyReady string = "NetworkPolicyReady"
	ConditionBuildNamespaceReady string = "BuildNamespaceReady"
)

// SetCondition sets or updates a condition on a slice of conditions.
// If the condition already exists with the same status, it is not updated
// (preserving LastTransitionTime). If the status changes, LastTransitionTime
// is updated to now. The observedGeneration parameter tracks which generation
// of the resource the condition relates to.
func SetCondition(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string, observedGeneration int64) {
	now := metav1.Now()

	for i, c := range *conditions {
		if c.Type == conditionType {
			if c.Status == status {
				// Status unchanged, update reason/message but keep transition time.
				(*conditions)[i].Reason = reason
				(*conditions)[i].Message = message
				(*conditions)[i].ObservedGeneration = observedGeneration
				return
			}
			// Status changed, update everything.
			(*conditions)[i] = metav1.Condition{
				Type:               conditionType,
				Status:             status,
				LastTransitionTime: now,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: observedGeneration,
			}
			return
		}
	}

	// Condition not found, append it.
	*conditions = append(*conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: observedGeneration,
	})
}

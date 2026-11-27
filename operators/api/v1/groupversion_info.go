// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package v1 contains API types for the runtime.getloopscale.com API group version v1.
//
// These types define the Custom Resource Definitions (CRDs) used by
// LoopRuntime's Kubernetes operators: Tenant, PreviewEnvironment, ClusterCredential,
// PlatformBootstrap, GitRepositoryBinding, Environment, LoopRuntimeApplication,
// and ExposedRoute (8 CRDs, all wired). AppSecret and ReconciliationReport
// were deleted 2026-05-13 (ADR 2026-05-13-orphan-crd-deletion).
//
// +kubebuilder:object:generate=true
// +groupName=runtime.getloopscale.com
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion is the API group and version for LoopRuntime CRDs.
var GroupVersion = schema.GroupVersion{Group: "runtime.getloopscale.com", Version: "v1"}

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
		&ClusterCredential{},
		&ClusterCredentialList{},
		&PlatformBootstrap{},
		&PlatformBootstrapList{},
		&GitRepositoryBinding{},
		&GitRepositoryBindingList{},
		&Environment{},
		&EnvironmentList{},
		&LoopRuntimeApplication{},
		&LoopRuntimeApplicationList{},
		&ExposedRoute{},
		&ExposedRouteList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}

// Resource takes an unqualified resource name and returns a GroupResource.
func Resource(resource string) schema.GroupResource {
	return GroupVersion.WithResource(resource).GroupResource()
}

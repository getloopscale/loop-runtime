// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package v1 contains API types for the loopzero.io API group version v1.
//
// These types define the Custom Resource Definitions (CRDs) used by
// LoopZero's Kubernetes operators: Tenant, PreviewEnvironment, AppSecret,
// ClusterCredential, ReconciliationReport, LoopZeroApplication, and ExposedRoute.
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
		&PlatformBootstrap{},
		&PlatformBootstrapList{},
		&GitRepositoryBinding{},
		&GitRepositoryBindingList{},
		&Environment{},
		&EnvironmentList{},
		&LoopZeroApplication{},
		&LoopZeroApplicationList{},
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


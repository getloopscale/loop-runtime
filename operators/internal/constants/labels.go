// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package constants centralizes all string identifiers used across LoopZero operators.
//
// Every Kubernetes label key, annotation key, finalizer name, resource name, and
// well-known value used in controllers MUST be defined here. No string literal
// identifiers should appear in controller code — use these constants instead.
//
// This eliminates the "scattered strings" anti-pattern where the same identifier
// (e.g., "loopzero.io/tenant") appears in 15+ locations with no single source of truth.
package constants

// --- Kubernetes Label Keys ---

const (
	// LabelTenant identifies the owning tenant on any LoopZero-managed resource.
	LabelTenant = "loopzero.io/tenant"

	// LabelEnvironment identifies the environment on a resource.
	LabelEnvironment = "loopzero.io/environment"

	// LabelComponent identifies the LoopZero subsystem (build, runtime, etc.).
	LabelComponent = "loopzero.io/component"

	// LabelManagedBy identifies which controller created/manages the resource.
	LabelManagedBy = "loopzero.io/managed-by"

	// LabelClusterCredential links an ArgoCD secret to its ClusterCredential CR.
	LabelClusterCredential = "loopzero.io/cluster-credential"
)

// --- Label Values for LabelComponent ---

const (
	ComponentBuild = "build"
)

// --- Label Values for LabelManagedBy ---

const (
	ManagedByTenantController              = "tenant-controller"
	ManagedByEnvironmentController         = "environment-controller"
	ManagedByClusterCredentialController   = "clustercredential-controller"
	ManagedByGitRepoBindingController      = "gitrepositorybinding-controller"
	ManagedByPlatformBootstrapController   = "platformbootstrap-controller"
)

// --- ArgoCD Label Keys and Values ---

const (
	// ArgoCDSecretTypeLabel is the label key ArgoCD uses to identify secret types.
	ArgoCDSecretTypeLabel = "argocd.argoproj.io/secret-type"

	// ArgoCDSecretTypeRepository marks a secret as a repository credential.
	ArgoCDSecretTypeRepository = "repository"

	// ArgoCDSecretTypeRepoCreds marks a secret as repo-creds template.
	ArgoCDSecretTypeRepoCreds = "repo-creds"

	// ArgoCDSecretTypeCluster marks a secret as a cluster credential.
	ArgoCDSecretTypeCluster = "cluster"

	// ArgoCDResourcesFinalizer is the ArgoCD finalizer for Application CRs.
	ArgoCDResourcesFinalizer = "resources-finalizer.argocd.argoproj.io"
)

// --- Annotation Keys ---

const (
	// AnnotationCredentialHash stores the SHA256 hash of credentials for rotation detection.
	AnnotationCredentialHash = "platform.loopzero.io/credential-hash"

	// AnnotationProbeURLOverride allows tests to inject a probe URL.
	AnnotationProbeURLOverride = "loopzero.io/probe-url-override"
)

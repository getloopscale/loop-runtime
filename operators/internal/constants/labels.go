// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package constants centralizes all string identifiers used across LoopRuntime operators.
//
// Every Kubernetes label key, annotation key, finalizer name, resource name, and
// well-known value shared across controllers is defined here, and controller
// code uses these constants rather than string literals. A value only one
// package uses stays in that package.
//
// This eliminates the "scattered strings" anti-pattern where the same identifier
// (e.g., "runtime.getloopscale.com/tenant") appears in 15+ locations with no single source of truth.
package constants

// --- Kubernetes Label Keys ---

const (
	// LabelTenant identifies the owning tenant on any LoopRuntime-managed resource.
	LabelTenant = "runtime.getloopscale.com/tenant"

	// LabelEnvironment identifies the environment on a resource.
	LabelEnvironment = "runtime.getloopscale.com/environment"

	// LabelProject identifies the project on a resource.
	LabelProject = "runtime.getloopscale.com/project"

	// LabelComponent identifies the LoopRuntime subsystem (build, runtime, etc.).
	LabelComponent = "runtime.getloopscale.com/component"

	// LabelManagedBy identifies which controller created/manages the resource.
	LabelManagedBy = "runtime.getloopscale.com/managed-by"

	// LabelClusterCredential links an ArgoCD secret to its ClusterCredential CR.
	LabelClusterCredential = "runtime.getloopscale.com/cluster-credential"
)

// --- Label Values for LabelComponent ---

const (
	ComponentBuild = "build"
)

// --- Label Values for LabelManagedBy ---

const (
	ManagedByTenantController            = "tenant-controller"
	ManagedByEnvironmentController       = "environment-controller"
	ManagedByClusterCredentialController = "clustercredential-controller"
	ManagedByGitRepoBindingController    = "gitrepositorybinding-controller"
	ManagedByPlatformBootstrapController = "platformbootstrap-controller"
	ManagedByPreviewController           = "preview-controller"
)

// --- ArgoCD Label Keys and Values ---

const (
	// ArgoCDSecretTypeLabel is the label key ArgoCD uses to identify secret types.
	ArgoCDSecretTypeLabel = "argocd.argoproj.io/secret-type" //nolint:gosec // G101 false positive: label KEY, not credential value

	// ArgoCDSecretTypeRepository marks a secret as a repository credential.
	ArgoCDSecretTypeRepository = "repository"

	// ArgoCDSecretTypeRepoCreds marks a secret as repo-creds template.
	ArgoCDSecretTypeRepoCreds = "repo-creds" //nolint:gosec // G101 false positive: secret-TYPE marker string, not credential value

	// ArgoCDSecretTypeCluster marks a secret as a cluster credential.
	ArgoCDSecretTypeCluster = "cluster"

	// ArgoCDResourcesFinalizer is the ArgoCD finalizer for Application CRs.
	ArgoCDResourcesFinalizer = "resources-finalizer.argocd.argoproj.io"
)

// --- Annotation Keys ---

const (
	// AnnotationCredentialHash stores the SHA256 hash of credentials for rotation detection.
	AnnotationCredentialHash = "platform.runtime.getloopscale.com/credential-hash"

	// AnnotationProbeURLOverride allows tests to inject a probe URL.
	AnnotationProbeURLOverride = "runtime.getloopscale.com/probe-url-override"

	// AnnotationTokenExpiresAt stores the RFC3339 expiry timestamp for cached DO registry tokens.
	// Used to skip the DO API call when the token has more than DORegistryTokenRefreshBuffer remaining.
	AnnotationTokenExpiresAt = "runtime.getloopscale.com/token-expires-at" //nolint:gosec // G101 false positive: annotation KEY, not credential value

	// AnnotationParentGeneration propagates the parent Tenant's generation to child
	// Environment CRs. When the Tenant reconciles, it bumps this annotation, which
	// triggers the EnvironmentController to re-reconcile and correct drift on
	// cross-cluster resources (NetworkPolicy, ResourceQuota, RBAC).
	AnnotationParentGeneration = "runtime.getloopscale.com/parent-generation"

	// AnnotationDeletionRetryCount tracks the number of failed cleanup attempts
	// during tenant deletion. Used for exponential backoff calculation. MEET-052.
	AnnotationDeletionRetryCount = "runtime.getloopscale.com/deletion-retry-count"
)

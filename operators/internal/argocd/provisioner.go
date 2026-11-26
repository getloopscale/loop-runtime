// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package argocd provides the operator-side client wrapper for
// ArgoCD's REST API plus a circuit-breaker decorator. Used by
// PlatformBootstrap + Tenant controllers to create/sync/delete
// AppProjects and Applications without coupling the controllers to
// raw HTTP error handling.
package argocd

import "context"

// ApplicationSpec defines the desired state of an ArgoCD Application.
type ApplicationSpec struct {
	// Name is the ArgoCD Application name (e.g., "acme-production").
	Name string

	// Namespace is the namespace where the ArgoCD Application CR lives (typically "argocd").
	Namespace string

	// TenantID identifies the owning tenant.
	TenantID string

	// RepoURL is the Git repository URL (e.g., Gitea repo URL).
	RepoURL string

	// TargetRevision is the Git branch to track (e.g., "main").
	TargetRevision string

	// Path is the directory within the repo containing manifests (e.g., "environments/production/").
	Path string

	// DestinationNS is the target namespace in the workload cluster.
	DestinationNS string

	// DestinationURL is the Kubernetes API server URL for the destination cluster.
	DestinationURL string

	// CellID identifies which Cell this Application targets.
	// ADR-0008 Phase 1: The provisioner resolves CellID to the correct ArgoCD instance.
	// Phase 0: empty or "cell-0" — uses the single ArgoCD instance.
	CellID string

	// Environment is the env name (e.g., "production", "staging", "j-rollout-a").
	// Sprint B B3 prep (code-005 architectural follow-up): populating this enables
	// adding the `runtime.getloopscale.com/environment` label to the rendered ArgoCD Application.
	// Future activator iteration can switch from ParseArgoAppName(LastIndex("-"))
	// to label-based lookup, closing the hyphen-ambiguity finding without breaking
	// existing fixtures (MEET-ASYNC-AMENDMENT-2026-06-02-002 documented why the
	// regex tightening path was reverted). Empty string is tolerated for backward
	// compat — the label is only emitted when this field is non-empty.
	Environment string
}

// AppProjectSpec defines the desired state of an ArgoCD AppProject for tenant RBAC isolation.
type AppProjectSpec struct {
	// Name is the AppProject name (typically the tenant name).
	Name string

	// Namespace is the namespace where the AppProject CR lives (typically "argocd").
	Namespace string

	// TenantID identifies the owning tenant.
	TenantID string

	// SourceRepos lists allowed Git repository URLs.
	SourceRepos []string

	// DestinationNamespaces restricts which namespaces ArgoCD can deploy to.
	// If empty, defaults to "loop-runtime-{tenantID}-*" wildcard pattern.
	DestinationNamespaces []string

	// DestinationServer is the Kubernetes API server URL for allowed destinations.
	// SEC-007: Replaces wildcard `server: *` with explicit server URL to prevent
	// cross-cluster deployment via ArgoCD when multiple clusters are registered.
	// If empty, defaults to "*" for backward compatibility.
	// CHG-2026-04-30-DELETE-SINGLE-CELL: ignored when CellID is set to a
	// non-default cell (eg. "cell-1") — destinations switch to `name: cellID`.
	DestinationServer string

	// CellID is the cell identifier (matches ClusterCredential CR name +
	// ArgoCD cluster Secret label). When non-empty AND not "cell-0",
	// AppProject destinations use `name: <CellID>` instead of `server: URL`,
	// so ArgoCD resolves via its cluster registry (handles k3d IP rotation
	// transparently and is the same idiomatic mechanism Argo recommends
	// for multi-cluster).
	CellID string
}

// Provisioner manages ArgoCD Application and AppProject CRs.
type Provisioner interface {
	// EnsureAppProject creates or updates an ArgoCD AppProject CR for tenant RBAC isolation.
	// This operation is idempotent: calling it multiple times with the same spec produces no side effects.
	EnsureAppProject(ctx context.Context, spec AppProjectSpec) error

	// EnsureApplication creates or updates an ArgoCD Application CR to match the given spec.
	// This operation is idempotent: calling it multiple times with the same spec produces no side effects.
	EnsureApplication(ctx context.Context, spec ApplicationSpec) error

	// DeleteApplications removes all ArgoCD Application CRs associated with a tenant.
	// This operation is idempotent: calling it when no applications exist is a no-op.
	DeleteApplications(ctx context.Context, tenantID string) error

	// DeleteAppProject removes the ArgoCD AppProject CR associated with a tenant.
	// This operation is idempotent: calling it when no project exists is a no-op.
	DeleteAppProject(ctx context.Context, tenantID string) error

	// ListApplicationNames returns the metadata.name of every ArgoCD Application
	// CR labeled with the given tenantID. Used by tenant_gitops to compute the
	// set of Apps to prune after the active PreviewEnvironment set shrinks
	// (close-PR cleanup). #71 LIVE catch 2026-06-07 — preview_controller.go:609
	// comment claimed "Application CR deletion is owned by the API" but the
	// API never wired it; the canonical fix is here at the tenant operator,
	// not in the API.
	ListApplicationNames(ctx context.Context, tenantID string) ([]string, error)

	// DeleteApplication removes a SINGLE ArgoCD Application CR by name.
	// Idempotent: NotFound is not an error. #71 LIVE catch 2026-06-07.
	DeleteApplication(ctx context.Context, appName string) error
}

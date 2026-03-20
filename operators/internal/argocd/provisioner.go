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
}

package gitea

import "context"

// GitRepoProvisioner manages Git repository lifecycle for tenant GitOps.
type GitRepoProvisioner interface {
	EnsureRepo(ctx context.Context, name string, branch string) (repoURL string, err error)
	RepoExists(ctx context.Context, name string) (bool, error)
	ArchiveRepo(ctx context.Context, name string) error
}

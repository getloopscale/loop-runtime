// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitea

import "context"

// GitRepoProvisioner manages Git repository lifecycle for tenant GitOps.
type GitRepoProvisioner interface {
	EnsureRepo(ctx context.Context, name string, branch string) (repoURL string, err error)
	RepoExists(ctx context.Context, name string) (bool, error)
	ArchiveRepo(ctx context.Context, name string) error
	// UnarchiveRepo flips a repo's archived flag back to false (idempotent).
	// CHG-002b 2026-04-28 — required so EnsureRepo can recover from
	// the tenant-delete-then-recreate cycle.
	UnarchiveRepo(ctx context.Context, name string) error
}

// BootstrapClient defines the Gitea operations required by PlatformBootstrap.
// The APIClient satisfies this interface. Controllers should depend on this
// interface (DIP) rather than calling HTTP endpoints inline.
type BootstrapClient interface {
	CheckVersion(ctx context.Context) error
	ValidateToken(ctx context.Context, token string) error
	ListTokens(ctx context.Context, username, password string) ([]TokenInfo, error)
	DeleteToken(ctx context.Context, username, password string, tokenID int) error
	CreateToken(ctx context.Context, username, password, tokenName string) (string, error)
	EnsureOrg(ctx context.Context, token, orgName string) error
}

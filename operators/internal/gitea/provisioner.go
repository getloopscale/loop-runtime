// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package gitea provides the operator-side client wrapper for
// Gitea's REST API plus a circuit-breaker decorator. Used by
// PlatformBootstrap + Tenant controllers to create/manage the
// per-tenant Gitea organization, repositories, and access tokens
// that back the GitOps source-of-truth.
package gitea

import "context"

// GitRepoProvisioner manages Git repository lifecycle for tenant GitOps: the
// two operations its only consumer, the TenantReconciler, performs. EnsureRepo
// unarchives a repo archived by an earlier tenant deletion (CHG-002b), so the
// delete-then-recreate cycle needs no separate operation here.
type GitRepoProvisioner interface {
	EnsureRepo(ctx context.Context, name string, branch string) (repoURL string, err error)
	ArchiveRepo(ctx context.Context, name string) error
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

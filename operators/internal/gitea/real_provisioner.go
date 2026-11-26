// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitea

import (
	"context"
	"fmt"

	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/httpclient"
	"github.com/getloopscale/loop-runtime/pkg/gitearepo"
)

// Compile-time interface compliance check.
var _ GitRepoProvisioner = (*RealProvisioner)(nil)

// RealProvisioner manages tenant GitOps repositories through the shared
// repository client, pkg/gitearepo — the same one the api uses.
type RealProvisioner struct {
	repos *gitearepo.Client
}

// NewRealProvisioner creates a provisioner that manages real Gitea repositories.
func NewRealProvisioner(baseURL, token, orgName string) *RealProvisioner {
	httpClient := httpclient.NewWithTimeout(constants.HTTPTimeoutDefault)
	return &RealProvisioner{repos: gitearepo.New(baseURL, token, orgName, httpClient)}
}

// EnsureRepo makes sure the tenant's GitOps repository exists and accepts
// pushes, and returns its in-cluster clone URL. A repository archived by an
// earlier deletion of the same tenant is unarchived (CHG-002b): otherwise
// every push returns 403 and the tenant's releases fail. The repository is
// auto-initialised because ArgoCD clones it before anything is pushed.
func (p *RealProvisioner) EnsureRepo(ctx context.Context, name string, branch string) (string, error) {
	if err := p.repos.EnsureRepo(ctx, name, gitearepo.EnsureOptions{AutoInit: true, DefaultBranch: branch}); err != nil {
		return "", fmt.Errorf("gitea: %w", err)
	}
	return p.repos.RepoURL(name), nil
}

// ArchiveRepo archives (soft-deletes) a repository.
func (p *RealProvisioner) ArchiveRepo(ctx context.Context, name string) error {
	if err := p.repos.ArchiveRepo(ctx, name); err != nil {
		return fmt.Errorf("gitea: %w", err)
	}
	return nil
}

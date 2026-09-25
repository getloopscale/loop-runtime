// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitea

import (
	"context"
	"fmt"
	"strings"

	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/httpclient"
)

// Compile-time interface compliance check.
var _ GitRepoProvisioner = (*RealProvisioner)(nil)

// RealProvisioner manages Git repositories in Gitea via the typed APIClient.
type RealProvisioner struct {
	api     *APIClient
	baseURL string
	token   string
	orgName string
}

// NewRealProvisioner creates a provisioner that manages real Gitea repositories.
func NewRealProvisioner(baseURL, token, orgName string) *RealProvisioner {
	httpClient := httpclient.NewWithTimeout(constants.HTTPTimeoutDefault)
	return &RealProvisioner{
		api:     NewAPIClient(baseURL, httpClient),
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		orgName: orgName,
	}
}

// repoURL builds a clone URL using the provisioner's configured baseURL.
//
// We do NOT return Gitea's `clone_url` field because that uses Gitea's
// configured ROOT_URL, which is the public/external hostname (e.g.,
// https://git.test.local). ArgoCD's repo-server runs in-cluster and has
// no external DNS, so it would fail to resolve the public hostname.
// Building the URL from baseURL (the in-cluster service address) keeps
// Gitea reachable from ArgoCD without requiring hairpin DNS.
func (p *RealProvisioner) repoURL(name string) string {
	return fmt.Sprintf("%s/%s/%s.git", p.baseURL, p.orgName, name)
}

// EnsureRepo creates a Git repository if it doesn't exist. Idempotent.
//
// CHG-002b 2026-04-28 — when the repo already exists in archived state
// (e.g., the tenant was previously deleted, which calls ArchiveRepo on the
// gitops repo, and the same tenant name is recreated), unarchive it before
// returning success. Without this, every subsequent `git-receive-pack`
// returns 403 Forbidden, gitops push fails, releases are marked
// GIT_MIRROR_FAILED, and builds are silently failed by the reconcile loop.
func (p *RealProvisioner) EnsureRepo(ctx context.Context, name string, branch string) (string, error) {
	repo, err := p.api.GetRepo(ctx, p.token, p.orgName, name)
	if err != nil {
		return "", fmt.Errorf("gitea: check repo %s: %w", name, err)
	}
	if repo != nil {
		if repo.Archived {
			if uerr := p.api.UnarchiveRepo(ctx, p.token, p.orgName, name); uerr != nil {
				return "", fmt.Errorf("gitea: unarchive repo %s: %w", name, uerr)
			}
		}
		return p.repoURL(name), nil
	}

	if branch == "" {
		branch = "main"
	}
	created, err := p.api.CreateRepo(ctx, p.token, p.orgName, CreateRepoRequest{
		Name:          name,
		AutoInit:      true,
		DefaultBranch: branch,
		Private:       true,
	})
	if err != nil {
		return "", err
	}
	if created == nil {
		return "", fmt.Errorf("gitea: create repo %s: no response", name)
	}
	return p.repoURL(name), nil
}

// ArchiveRepo archives (soft-deletes) a repository.
func (p *RealProvisioner) ArchiveRepo(ctx context.Context, name string) error {
	return p.api.ArchiveRepo(ctx, p.token, p.orgName, name)
}

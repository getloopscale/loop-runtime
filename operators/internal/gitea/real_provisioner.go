// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitea

import (
	"context"
	"fmt"
	"net/http"

	"github.com/loopzero/loopzero/operators/internal/constants"
)

// Compile-time interface compliance check.
var _ GitRepoProvisioner = (*RealProvisioner)(nil)

// RealProvisioner manages Git repositories in Gitea via the typed APIClient.
type RealProvisioner struct {
	api     *APIClient
	token   string
	orgName string
}

// NewRealProvisioner creates a provisioner that manages real Gitea repositories.
func NewRealProvisioner(baseURL, token, orgName string) *RealProvisioner {
	httpClient := &http.Client{Timeout: constants.HTTPTimeoutDefault}
	return &RealProvisioner{
		api:     NewAPIClient(baseURL, httpClient),
		token:   token,
		orgName: orgName,
	}
}

// EnsureRepo creates a Git repository if it doesn't exist. Idempotent.
func (p *RealProvisioner) EnsureRepo(ctx context.Context, name string, branch string) (string, error) {
	// Check if repo exists
	repo, err := p.api.GetRepo(ctx, p.token, p.orgName, name)
	if err != nil {
		return "", fmt.Errorf("gitea: check repo %s: %w", name, err)
	}
	if repo != nil {
		return repo.CloneURL, nil
	}

	// Create repo
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
	return created.CloneURL, nil
}

// RepoExists checks if a repository exists.
func (p *RealProvisioner) RepoExists(ctx context.Context, name string) (bool, error) {
	repo, err := p.api.GetRepo(ctx, p.token, p.orgName, name)
	if err != nil {
		return false, err
	}
	return repo != nil, nil
}

// ArchiveRepo archives (soft-deletes) a repository.
func (p *RealProvisioner) ArchiveRepo(ctx context.Context, name string) error {
	return p.api.ArchiveRepo(ctx, p.token, p.orgName, name)
}

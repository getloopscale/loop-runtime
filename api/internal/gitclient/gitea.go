// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// GiteaRepoManager creates and queries repos on a Gitea instance.
// Previously this logic was duplicated across source/gitea_store.go,
// gitops/gitea_pusher.go, and gitops/gogit_pusher.go.
type GiteaRepoManager struct {
	baseURL string
	token   string
	orgName string
	client  *http.Client
}

// NewGiteaRepoManager creates a GiteaRepoManager.
func NewGiteaRepoManager(baseURL, token, orgName string, client *http.Client) *GiteaRepoManager {
	return &GiteaRepoManager{
		baseURL: baseURL,
		token:   token,
		orgName: orgName,
		client:  client,
	}
}

// createRepoRequest is the JSON body for Gitea's create-repo API.
// SEC-4: Using json.Marshal prevents JSON injection via repo names.
type createRepoRequest struct {
	Name          string `json:"name"`
	AutoInit      bool   `json:"auto_init"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

// EnsureRepo creates the repo under the org via Gitea API if it doesn't exist.
// autoInit controls whether Gitea initializes the repo with a default branch.
// Source repos use false (pushed from CLI), gitops repos use true (need branch for ArgoCD).
func (m *GiteaRepoManager) EnsureRepo(ctx context.Context, repoName string, autoInit bool) error {
	checkURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", m.baseURL, m.orgName, repoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+m.token)

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("check repo: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil // repo exists
	}

	// Create repo under org
	createURL := fmt.Sprintf("%s/api/v1/orgs/%s/repos", m.baseURL, m.orgName)
	bodyBytes, err := json.Marshal(createRepoRequest{
		Name:          repoName,
		AutoInit:      autoInit,
		DefaultBranch: "main",
		Private:       true,
	})
	if err != nil {
		return fmt.Errorf("marshal create repo body: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+m.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = m.client.Do(req)
	if err != nil {
		return fmt.Errorf("create repo: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		return fmt.Errorf("create repo: status %d", resp.StatusCode)
	}

	return nil
}

// RepoURL returns the plain Gitea repo URL without embedded credentials.
// SEC-1: Credentials are injected via http.extraHeader in GitRunner.RunWithAuth.
func (m *GiteaRepoManager) RepoURL(repoName string) string {
	return fmt.Sprintf("%s/%s/%s.git", m.baseURL, m.orgName, repoName)
}

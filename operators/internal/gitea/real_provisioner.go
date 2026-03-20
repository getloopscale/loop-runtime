package gitea

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// RealProvisioner manages Git repositories in Gitea via REST API.
type RealProvisioner struct {
	baseURL string
	token   string
	orgName string
	client  *http.Client
}

// NewRealProvisioner creates a provisioner that manages real Gitea repositories.
func NewRealProvisioner(baseURL, token, orgName string) *RealProvisioner {
	return &RealProvisioner{
		baseURL: baseURL,
		token:   token,
		orgName: orgName,
		client:  &http.Client{},
	}
}

// EnsureRepo creates a Git repository if it doesn't exist. Idempotent.
func (p *RealProvisioner) EnsureRepo(ctx context.Context, name string, branch string) (string, error) {
	// Check if repo exists
	checkURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", p.baseURL, p.orgName, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gitea: check repo %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		var repo struct {
			CloneURL string `json:"clone_url"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
			return "", fmt.Errorf("gitea: decode repo response: %w", err)
		}
		return repo.CloneURL, nil
	}

	// Create repo
	if branch == "" {
		branch = "main"
	}
	createURL := fmt.Sprintf("%s/api/v1/orgs/%s/repos", p.baseURL, p.orgName)
	body, _ := json.Marshal(map[string]interface{}{
		"name":           name,
		"auto_init":      true,
		"default_branch": branch,
		"private":        true,
	})

	req, err = http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gitea: create repo %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusConflict {
		// Repo already exists (race condition) — get it
		return p.EnsureRepo(ctx, name, branch)
	}

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gitea: create repo %s: status %d: %s", name, resp.StatusCode, string(respBody))
	}

	var created struct {
		CloneURL string `json:"clone_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("gitea: decode created repo: %w", err)
	}

	return created.CloneURL, nil
}

// RepoExists checks if a repository exists.
func (p *RealProvisioner) RepoExists(ctx context.Context, name string) (bool, error) {
	checkURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", p.baseURL, p.orgName, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "token "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("gitea: check repo %s: %w", name, err)
	}
	_ = resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// ArchiveRepo archives (soft-deletes) a repository.
func (p *RealProvisioner) ArchiveRepo(ctx context.Context, name string) error {
	archiveURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", p.baseURL, p.orgName, name)
	body, _ := json.Marshal(map[string]interface{}{
		"archived": true,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, archiveURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("gitea: archive repo %s: %w", name, err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("gitea: archive repo %s: status %d", name, resp.StatusCode)
	}

	return nil
}

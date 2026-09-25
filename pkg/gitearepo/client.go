// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package gitearepo is the one client for the repository lifecycle on the
// platform's Gitea: ensure a repository exists and is writable, archive it,
// and build its in-cluster clone URL.
//
// The operator (tenant GitOps repos) and the api (source and per-environment
// GitOps repos) used to carry one hand-rolled copy each, and the copies had
// drifted: only the operator unarchived a repository archived by an earlier
// tenant deletion, and only the operator honoured a caller-chosen default
// branch. Both now behave as this package does.
package gitearepo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBranch is the branch a repository is created with when the caller
// names none.
const DefaultBranch = "main"

// defaultTimeout bounds one API call when the caller supplies no client:
// http.DefaultClient has no timeout, and a hung server would block forever.
const defaultTimeout = 30 * time.Second

// StatusError is an API call answered with an unexpected HTTP status. The
// status stays a number so callers classify it with errors.As.
type StatusError struct {
	Op         string
	Repo       string
	StatusCode int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s repository %s: status %d", e.Op, e.Repo, e.StatusCode)
}

// Client manages repositories of one organization.
type Client struct {
	baseURL string
	token   string
	org     string
	http    *http.Client
}

// New returns a Client for the organization org on the server at baseURL,
// authenticating with token. A nil httpClient gets one with a timeout.
func New(baseURL, token, org string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, org: org, http: httpClient}
}

// EnsureOptions shapes a repository EnsureRepo has to create. They do not
// apply to a repository that already exists.
type EnsureOptions struct {
	// AutoInit makes the server create the default branch with an initial
	// commit. A repository something clones before pushing to needs it.
	AutoInit bool
	// DefaultBranch is the initial branch; empty means DefaultBranch.
	DefaultBranch string
}

// EnsureRepo makes sure the repository exists and accepts pushes. It is
// idempotent:
//   - an existing, active repository is left alone;
//   - an archived one is unarchived — archiving is how a deleted tenant's
//     repository is retired, the server refuses pushes to it, and a tenant
//     recreated under the same name must be able to push again;
//   - a missing one is created private, with opts;
//   - a create answered 409 means a concurrent caller created it.
//
// A lookup answered with anything but 200 or 404 is an error: it says
// nothing about whether the repository exists.
func (c *Client) EnsureRepo(ctx context.Context, name string, opts EnsureOptions) error {
	archived, found, err := c.lookup(ctx, name)
	if err != nil {
		return err
	}
	if found {
		if archived {
			return c.setArchived(ctx, name, false, "unarchive")
		}
		return nil
	}
	return c.create(ctx, name, opts)
}

// ArchiveRepo archives (soft-deletes) the repository. Archiving one that does
// not exist is not an error.
func (c *Client) ArchiveRepo(ctx context.Context, name string) error {
	return c.setArchived(ctx, name, true, "archive")
}

// RepoURL is the clone URL built from the configured base URL, without
// credentials.
//
// It is deliberately not the server's own clone_url: that one is built from
// the server's public ROOT_URL, which in-cluster clients (ArgoCD's
// repo-server, the api) cannot resolve.
func (c *Client) RepoURL(name string) string {
	return fmt.Sprintf("%s/%s/%s.git", c.baseURL, c.org, name)
}

func (c *Client) repoEndpoint(name string) string {
	return fmt.Sprintf("%s/api/v1/repos/%s/%s", c.baseURL, url.PathEscape(c.org), url.PathEscape(name))
}

func (c *Client) lookup(ctx context.Context, name string) (archived, found bool, err error) {
	resp, err := c.do(ctx, http.MethodGet, c.repoEndpoint(name), nil)
	if err != nil {
		return false, false, fmt.Errorf("look up repository %s: %w", c.qualified(name), err)
	}
	defer closeBody(resp)
	switch resp.StatusCode {
	case http.StatusOK:
		var repo struct {
			Archived bool `json:"archived"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
			return false, false, fmt.Errorf("decode repository %s: %w", c.qualified(name), err)
		}
		return repo.Archived, true, nil
	case http.StatusNotFound:
		return false, false, nil
	default:
		return false, false, &StatusError{Op: "look up", Repo: c.qualified(name), StatusCode: resp.StatusCode}
	}
}

func (c *Client) create(ctx context.Context, name string, opts EnsureOptions) error {
	branch := opts.DefaultBranch
	if branch == "" {
		branch = DefaultBranch
	}
	body := struct {
		Name          string `json:"name"`
		AutoInit      bool   `json:"auto_init"`
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
	}{Name: name, AutoInit: opts.AutoInit, DefaultBranch: branch, Private: true}
	endpoint := fmt.Sprintf("%s/api/v1/orgs/%s/repos", c.baseURL, url.PathEscape(c.org))
	resp, err := c.do(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("create repository %s: %w", c.qualified(name), err)
	}
	defer closeBody(resp)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		return &StatusError{Op: "create", Repo: c.qualified(name), StatusCode: resp.StatusCode}
	}
	return nil
}

func (c *Client) setArchived(ctx context.Context, name string, archived bool, op string) error {
	resp, err := c.do(ctx, http.MethodPatch, c.repoEndpoint(name), map[string]bool{"archived": archived})
	if err != nil {
		return fmt.Errorf("%s repository %s: %w", op, c.qualified(name), err)
	}
	defer closeBody(resp)
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusNotFound && archived:
		return nil
	default:
		return &StatusError{Op: op, Repo: c.qualified(name), StatusCode: resp.StatusCode}
	}
}

func (c *Client) do(ctx context.Context, method, endpoint string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

func (c *Client) qualified(name string) string {
	return c.org + "/" + name
}

// closeBody drains the body so the connection is reused.
func closeBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package harbor provides a typed HTTP client for the Harbor v2.0 REST API.
// It manages projects and robot accounts for per-tenant container image isolation.
//
// All methods are idempotent and safe to retry. Authentication uses HTTP Basic Auth
// with Harbor admin credentials. No external dependencies — standard library only.
package harbor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/operators/internal/httpclient"
)

// renewalThreshold is the minimum remaining TTL before a robot account is
// considered "near expiry" and will be recreated. If a robot has less than
// this duration remaining, EnsureRobotAccount deletes and recreates it.
const renewalThreshold = 2 * time.Hour

// Client manages Harbor resources (projects, robot accounts) for the LoopZero platform.
type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

// ProjectOpts controls project creation settings.
type ProjectOpts struct {
	StorageQuota int64 // bytes, -1 for unlimited
	// RequireContentTrust turns on Harbor content trust: only signed images
	// may be pulled from the project. It does not configure tag immutability.
	RequireContentTrust bool
}

// RobotAccountOpts controls robot account creation.
type RobotAccountOpts struct {
	Name       string        // e.g. "build-acme" or "pull-acme"
	TTL        time.Duration // max 24h
	PushAccess bool          // true for build accounts, false for pull-only
}

// RobotCredentials holds the credentials returned by Harbor for a robot account.
type RobotCredentials struct {
	Username  string    // e.g. "robot$build-acme"
	Secret    string    // the token/password
	ExpiresAt time.Time // when the robot account expires
}

// NewClient creates a Harbor API client.
// The baseURL should be the root URL of the Harbor instance (e.g. "https://harbor.example.com").
//
// T4.2 / CODE-003: uses the shared httpclient package so Harbor inherits
// the secure-by-default TLS configuration controlled by
// OPERATOR_INSECURE_SKIP_TLS (T2.5 / SEC-001).
// Quality-Gate Phase 2: NewClientWithHTTP variant removed (no callers).
// Restore alongside a real custom-transport use case if needed.
func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		username:   username,
		password:   password,
		httpClient: httpclient.NewWithTimeout(30 * time.Second),
	}
}

// --- Projects ---

// harborProject is the Harbor API response for a project.
type harborProject struct {
	ProjectID int    `json:"project_id"`
	Name      string `json:"name"`
}

// EnsureProject creates a project if it doesn't exist, or updates it if it does.
// Idempotent: safe to call repeatedly.
func (c *Client) EnsureProject(ctx context.Context, name string, opts ProjectOpts) error {
	existing, err := c.getProjectByName(ctx, name)
	if err != nil {
		return fmt.Errorf("harbor: check project %q: %w", name, err)
	}

	if existing != nil {
		return c.updateProject(ctx, existing.ProjectID, name, opts)
	}

	return c.createProject(ctx, name, opts)
}

// DeleteProject deletes a project and all its contents.
// Idempotent: returns nil if the project does not exist.
func (c *Client) DeleteProject(ctx context.Context, name string) error {
	existing, err := c.getProjectByName(ctx, name)
	if err != nil {
		return fmt.Errorf("harbor: check project %q for deletion: %w", name, err)
	}
	if existing == nil {
		slog.Info("harbor: project not found, nothing to delete", "project", name)
		return nil
	}

	reqURL := fmt.Sprintf("%s/api/v2.0/projects/%d", c.baseURL, existing.ProjectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("harbor: create delete project request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("harbor: delete project %q: %w", name, err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("harbor: delete project %q: status %d: %s", name, resp.StatusCode, string(body))
}

// --- Robot Accounts ---

// harborRobot is the Harbor API response for a robot account.
type harborRobot struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Secret    string `json:"secret"`
	ExpiresAt int64  `json:"expires_at"`
}

// EnsureRobotAccount creates or refreshes a robot account for a project.
// Returns the robot account credentials (username + secret).
//
// If the account exists and has sufficient TTL remaining, returns existing credentials
// (without the secret, since Harbor does not return secrets for existing robots).
// If the account exists and is near expiry, deletes and recreates it.
// If the account does not exist, creates it.
func (c *Client) EnsureRobotAccount(ctx context.Context, projectName string, opts RobotAccountOpts) (*RobotCredentials, error) {
	// CHG-002b 2026-04-28 — search project-scoped first (the path
	// /projects/<name>/robots is the only endpoint that lists project-level
	// robots reliably; the global /robots?q=name= filter silently omits
	// them in Harbor 2.x). Falls back to the global search for legacy
	// system-level robots.
	existing, err := c.findRobotInProject(ctx, projectName, opts.Name)
	if err != nil {
		return nil, fmt.Errorf("harbor: find robot %q in project %q: %w", opts.Name, projectName, err)
	}
	if existing == nil {
		existing, err = c.findRobot(ctx, opts.Name)
		if err != nil {
			return nil, fmt.Errorf("harbor: find robot %q: %w", opts.Name, err)
		}
	}

	if existing != nil {
		expiresAt := time.Unix(existing.ExpiresAt, 0)
		remaining := time.Until(expiresAt)

		if remaining > renewalThreshold {
			// Robot is still valid — return existing credentials.
			return &RobotCredentials{
				Username:  existing.Name,
				Secret:    existing.Secret,
				ExpiresAt: expiresAt,
			}, nil
		}

		// Near expiry — delete and recreate.
		slog.Info("harbor: robot near expiry, recreating",
			"robot", opts.Name, "remaining", remaining.String())
		if err := c.deleteRobotByID(ctx, existing.ID); err != nil {
			return nil, fmt.Errorf("harbor: delete expired robot %q: %w", opts.Name, err)
		}
	}

	return c.createRobot(ctx, projectName, opts)
}

// DeleteRobotAccount deletes a robot account by name.
// Idempotent: returns nil if the robot does not exist.
//
// CHG-002b 2026-04-28 — must search project-scoped first, mirroring
// EnsureRobotAccount. Project-level robots in Harbor 2.x have names like
// `robot$<project>+<name>` and the global /robots?q=name= filter does not
// match them reliably, so an exclusively-global lookup silently returns
// "not found" and leaves the robot in place. The result was that the
// self-heal path believed the robot was deleted, then EnsureRobotAccount
// found it via project scope, decided the TTL was still valid, and returned
// a credentials object with an empty Secret — preventing the merged
// `buildkit-registry-config` from ever receiving the Harbor auth entry.
func (c *Client) DeleteRobotAccount(ctx context.Context, projectName string, robotName string) error {
	existing, err := c.findRobotInProject(ctx, projectName, robotName)
	if err != nil {
		return fmt.Errorf("harbor: find robot %q in project %q for deletion: %w", robotName, projectName, err)
	}
	if existing == nil {
		existing, err = c.findRobot(ctx, robotName)
		if err != nil {
			return fmt.Errorf("harbor: find robot %q for deletion: %w", robotName, err)
		}
	}
	if existing == nil {
		slog.Info("harbor: robot not found, nothing to delete", "robot", robotName, "project", projectName)
		return nil
	}

	return c.deleteRobotByID(ctx, existing.ID)
}

// --- Health ---

// Healthy returns nil if Harbor is responding.
func (c *Client) Healthy(ctx context.Context) error {
	reqURL := fmt.Sprintf("%s/api/v2.0/health", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("harbor: create health request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("harbor: health check failed: %w", err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("harbor: health check returned status %d", resp.StatusCode)
	}
	return nil
}

// --- Internal helpers ---

func (c *Client) getProjectByName(ctx context.Context, name string) (*harborProject, error) {
	reqURL := fmt.Sprintf("%s/api/v2.0/projects?name=%s", c.baseURL, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting projects: %w", err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list projects returned status %d", resp.StatusCode)
	}

	var projects []harborProject
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return nil, fmt.Errorf("decoding projects: %w", err)
	}

	// Harbor's name filter is a prefix match, so verify exact match.
	for i := range projects {
		if projects[i].Name == name {
			return &projects[i], nil
		}
	}
	return nil, nil
}

func (c *Client) createProject(ctx context.Context, name string, opts ProjectOpts) error {
	payload := map[string]interface{}{
		"project_name": name,
		"metadata": map[string]string{
			"public": "false",
		},
	}
	if opts.StorageQuota != 0 {
		payload["storage_limit"] = opts.StorageQuota
	}
	if opts.RequireContentTrust {
		payload["metadata"].(map[string]string)["enable_content_trust"] = "true"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("harbor: marshal create project: %w", err)
	}

	reqURL := fmt.Sprintf("%s/api/v2.0/projects", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("harbor: create project request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("harbor: create project %q: %w", name, err)
	}
	defer drainAndClose(resp)

	// 201 = created, 409 = already exists (race condition — idempotent).
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("harbor: create project %q: status %d: %s", name, resp.StatusCode, string(respBody))
}

func (c *Client) updateProject(ctx context.Context, projectID int, name string, opts ProjectOpts) error {
	payload := map[string]interface{}{
		"metadata": map[string]string{
			"public": "false",
		},
	}
	if opts.StorageQuota != 0 {
		payload["storage_limit"] = opts.StorageQuota
	}
	if opts.RequireContentTrust {
		payload["metadata"].(map[string]string)["enable_content_trust"] = "true"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("harbor: marshal update project: %w", err)
	}

	reqURL := fmt.Sprintf("%s/api/v2.0/projects/%d", c.baseURL, projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("harbor: create update project request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("harbor: update project %q: %w", name, err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("harbor: update project %q: status %d: %s", name, resp.StatusCode, string(respBody))
}

// findRobotInProject lists robots scoped to a Harbor project and matches the
// caller's logical name against the wide variety of Harbor name shapes:
//
//	robot$<project>+<name>     ← project-level (Harbor 2.x default)
//	robot$<name>               ← system-level
//	<project>+<name>           ← project-level without "robot$" prefix
//	<name>                     ← bare
//
// Project-scoped listing returns project robots reliably, where the global
// /robots?q=name= filter silently omits them.
func (c *Client) findRobotInProject(ctx context.Context, projectName, name string) (*harborRobot, error) {
	reqURL := fmt.Sprintf("%s/api/v2.0/projects/%s/robots?page_size=100", c.baseURL, projectName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing project robots: %w", err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list project robots returned status %d", resp.StatusCode)
	}

	var robots []harborRobot
	if err := json.NewDecoder(resp.Body).Decode(&robots); err != nil {
		return nil, fmt.Errorf("decoding project robots: %w", err)
	}
	for i := range robots {
		fullName := robots[i].Name
		stripped := strings.TrimPrefix(fullName, "robot$")
		// strip "<project>+" prefix if present
		if idx := strings.Index(stripped, "+"); idx >= 0 {
			stripped = stripped[idx+1:]
		}
		if stripped == name {
			return &robots[i], nil
		}
	}
	return nil, nil
}

func (c *Client) findRobot(ctx context.Context, name string) (*harborRobot, error) {
	// CHG-002b 2026-04-28 — match BOTH name shapes Harbor 2.x emits:
	//   - System-level robots:  "robot$<name>"
	//   - Project-level robots: "<project>+<name>"  (e.g. tn-foo+build-tn-foo)
	//
	// The previous matcher only handled `name` and `robot$<name>`, so a
	// project-level robot created by an earlier reconcile was never
	// detected by EnsureRobotAccount → CreateRobot was called again →
	// Harbor returned 409 CONFLICT → tenant reconcile loop failed forever.
	// All builds depending on that tenant's Harbor robot then 401'd at
	// mirror-harbor — silent supply-chain breakage, masked by
	// ContinueOn{Failed:true}.
	reqURL := fmt.Sprintf("%s/api/v2.0/robots?q=name%%3D%s&page_size=100", c.baseURL, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing robots: %w", err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list robots returned status %d", resp.StatusCode)
	}

	var robots []harborRobot
	if err := json.NewDecoder(resp.Body).Decode(&robots); err != nil {
		return nil, fmt.Errorf("decoding robots: %w", err)
	}

	for i := range robots {
		fullName := robots[i].Name
		if fullName == name {
			return &robots[i], nil
		}
		if strings.TrimPrefix(fullName, "robot$") == name {
			return &robots[i], nil
		}
		// Strip "<project>+" prefix.
		if idx := strings.Index(fullName, "+"); idx >= 0 && fullName[idx+1:] == name {
			return &robots[i], nil
		}
	}
	return nil, nil
}

func (c *Client) createRobot(ctx context.Context, projectName string, opts RobotAccountOpts) (*RobotCredentials, error) {
	access := []map[string]string{
		{"resource": "repository", "action": "pull"},
		{"resource": "artifact", "action": "read"},
		{"resource": "tag", "action": "list"},
	}
	if opts.PushAccess {
		access = append([]map[string]string{
			{"resource": "repository", "action": "push"},
		}, access...)
	}

	payload := map[string]interface{}{
		"name":     opts.Name,
		"duration": int64(opts.TTL.Seconds()),
		"level":    "project",
		"permissions": []map[string]interface{}{
			{
				"kind":      "project",
				"namespace": projectName,
				"access":    access,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("harbor: marshal create robot: %w", err)
	}

	reqURL := fmt.Sprintf("%s/api/v2.0/robots", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("harbor: create robot request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("harbor: create robot %q: %w", opts.Name, err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("harbor: create robot %q: status %d: %s", opts.Name, resp.StatusCode, string(respBody))
	}

	var robot harborRobot
	if err := json.NewDecoder(resp.Body).Decode(&robot); err != nil {
		return nil, fmt.Errorf("harbor: decode robot response: %w", err)
	}

	expiresAt := time.Unix(robot.ExpiresAt, 0)
	// If the API returned 0 for expires_at, calculate from TTL.
	if robot.ExpiresAt == 0 {
		expiresAt = time.Now().Add(opts.TTL)
	}

	return &RobotCredentials{
		Username:  robot.Name,
		Secret:    robot.Secret,
		ExpiresAt: expiresAt,
	}, nil
}

func (c *Client) deleteRobotByID(ctx context.Context, id int) error {
	reqURL := fmt.Sprintf("%s/api/v2.0/robots/%d", c.baseURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("harbor: create delete robot request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("harbor: delete robot %d: %w", id, err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("harbor: delete robot %d: status %d: %s", id, resp.StatusCode, string(body))
}

// drainAndClose reads and discards the remaining body to enable connection reuse,
// then closes the body. This is the standard pattern used across all LoopZero API clients.
func drainAndClose(resp *http.Response) {
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
}

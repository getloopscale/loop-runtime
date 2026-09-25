// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// harbor-init is an idempotent post-deploy configuration script for Harbor.
//
// It waits for Harbor health, changes the admin password, creates the
// loopzero-platform project, sets system configuration, and creates a CI
// robot account for pipeline image pushes.
//
// Usage:
//
//	HARBOR_URL=https://harbor.dev.loopzero.example \
//	HARBOR_ADMIN_PASSWORD_NEW=secret \
//	go run infra/scripts/harbor-init/main.go
//
// Exit codes:
//
//	0 — all steps completed successfully
//	1 — Harbor not healthy within timeout
//	2 — configuration failed
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	timeout, err := time.ParseDuration(cfg.healthTimeout)
	if err != nil {
		slog.Error("invalid health timeout", "value", cfg.healthTimeout, "error", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout+2*time.Minute)
	defer cancel()

	client := &http.Client{Timeout: 15 * time.Second}
	h := &harborInit{
		client:   client,
		baseURL:  cfg.harborURL,
		oldPass:  cfg.adminPasswordOld,
		newPass:  cfg.adminPasswordNew,
		logger:   logger,
		timeout:  timeout,
		platform: "loopzero-platform",
	}

	// Step 1: Wait for Harbor health.
	slog.Info("step 1/6: waiting for Harbor health", "url", cfg.harborURL, "timeout", timeout)
	if err := h.waitHealthy(ctx); err != nil {
		slog.Error("Harbor not healthy within timeout", "error", err)
		os.Exit(1)
	}
	slog.Info("Harbor is healthy")

	// Step 2: Change admin password.
	slog.Info("step 2/6: changing admin password")
	if err := h.changeAdminPassword(ctx); err != nil {
		slog.Error("failed to change admin password", "error", err)
		os.Exit(2)
	}

	// Step 3: Create loopzero-platform project.
	slog.Info("step 3/6: creating loopzero-platform project")
	if err := h.createProject(ctx); err != nil {
		slog.Error("failed to create project", "project", h.platform, "error", err)
		os.Exit(2)
	}

	// Step 4: Verify storage backend.
	slog.Info("step 4/6: verifying storage backend")
	if err := h.verifyStorage(ctx); err != nil {
		slog.Error("storage backend unhealthy", "error", err)
		os.Exit(1)
	}
	slog.Info("storage backend healthy")

	// Step 5: Set system configuration.
	slog.Info("step 5/6: setting system configuration")
	if err := h.setSystemConfig(ctx); err != nil {
		slog.Error("failed to set system configuration", "error", err)
		os.Exit(2)
	}

	// Step 6: Create CI robot account.
	slog.Info("step 6/6: creating CI robot account")
	if err := h.createCIRobot(ctx); err != nil {
		slog.Error("failed to create CI robot", "error", err)
		os.Exit(2)
	}

	slog.Info("harbor-init completed successfully")
}

// harborInitialAdminPassword is the password a fresh Harbor install starts with.
const harborInitialAdminPassword = "Harbor12345"

// config holds environment-driven configuration.
type config struct {
	harborURL        string
	adminPasswordOld string
	adminPasswordNew string
	healthTimeout    string
}

func loadConfig() (config, error) {
	c := config{
		harborURL:        os.Getenv("HARBOR_URL"),
		adminPasswordOld: os.Getenv("HARBOR_ADMIN_PASSWORD_OLD"),
		adminPasswordNew: os.Getenv("HARBOR_ADMIN_PASSWORD_NEW"),
		healthTimeout:    os.Getenv("HARBOR_HEALTH_TIMEOUT"),
	}
	if c.harborURL == "" {
		return c, fmt.Errorf("HARBOR_URL is required")
	}
	if c.adminPasswordNew == "" {
		return c, fmt.Errorf("HARBOR_ADMIN_PASSWORD_NEW is required")
	}
	if c.adminPasswordOld == "" {
		// Harbor's documented initial admin password. The harbor chart sets no
		// harborAdminPassword and its post-install hook passes no
		// HARBOR_ADMIN_PASSWORD_OLD, so a fresh install starts with this one;
		// it is used once, to log in and replace it with the new password.
		c.adminPasswordOld = harborInitialAdminPassword
	}
	if c.healthTimeout == "" {
		c.healthTimeout = "120s"
	}
	return c, nil
}

// harborInit encapsulates Harbor API interactions.
type harborInit struct {
	client   *http.Client
	baseURL  string
	oldPass  string
	newPass  string
	logger   *slog.Logger
	timeout  time.Duration
	platform string
	// pollInterval is the pause between health checks. Zero means 3s.
	pollInterval time.Duration
}

// healthResponse represents the Harbor /api/v2.0/health response.
type healthResponse struct {
	Status     string            `json:"status"`
	Components []healthComponent `json:"components"`
}

type healthComponent struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// waitHealthy polls Harbor health until it reports healthy or the timeout
// passes. A failed request, a non-200 status, an unreadable or unparseable
// body and an unhealthy report are all retried.
func (h *harborInit) waitHealthy(ctx context.Context) error {
	deadline := time.Now().Add(h.timeout)
	interval := h.pollInterval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		hr, err := h.fetchHealth(ctx)
		if err == nil && hr.Status == "healthy" {
			for _, c := range hr.Components {
				h.logger.Info("component healthy", "name", c.Name, "status", c.Status)
			}
			return nil
		}
		h.logger.Info("Harbor not yet healthy, retrying", "error", err, "status", hr.Status)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return fmt.Errorf("timeout after %s waiting for Harbor health", h.timeout)
}

// fetchHealth reads Harbor's health report. Every failure is returned: a
// transport error, a non-200 status, a body that cannot be read or parsed.
func (h *harborInit) fetchHealth(ctx context.Context) (healthResponse, error) {
	var hr healthResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/api/v2.0/health", nil)
	if err != nil {
		return hr, fmt.Errorf("creating health request: %w", err)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return hr, fmt.Errorf("health request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return hr, fmt.Errorf("reading health response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return hr, fmt.Errorf("health endpoint returned %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &hr); err != nil {
		return hr, fmt.Errorf("parsing health response: %w", err)
	}
	return hr, nil
}

// changeAdminPassword changes the admin password. Idempotent: if the new
// password already works, the step is skipped.
func (h *harborInit) changeAdminPassword(ctx context.Context) error {
	// First, check if new password already works.
	if h.canLogin(ctx, h.newPass) {
		h.logger.Info("admin password already changed, skipping")
		return nil
	}

	// Verify old password works.
	if !h.canLogin(ctx, h.oldPass) {
		return fmt.Errorf("neither old nor new admin password works — manual intervention required")
	}

	payload := map[string]string{
		"old_password": h.oldPass,
		"new_password": h.newPass,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, h.baseURL+"/api/v2.0/users/1/password", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating password change request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", h.oldPass)

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("password change request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("password change returned %d: %s", resp.StatusCode, string(respBody))
	}

	h.logger.Info("admin password changed successfully")
	return nil
}

// canLogin checks if the given password authenticates as admin.
func (h *harborInit) canLogin(ctx context.Context, password string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/api/v2.0/users/current", nil)
	if err != nil {
		return false
	}
	req.SetBasicAuth("admin", password)

	resp, err := h.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// createProject creates the loopzero-platform project. Idempotent: 409 Conflict
// means the project already exists.
func (h *harborInit) createProject(ctx context.Context) error {
	payload := map[string]any{
		"project_name":  h.platform,
		"metadata":      map[string]string{"auto_scan": "false"},
		"storage_limit": -1,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/api/v2.0/projects", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating project request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", h.newPass)

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("project creation request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		h.logger.Info("project created", "project", h.platform)
	case http.StatusConflict:
		h.logger.Info("project already exists, skipping", "project", h.platform)
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("project creation returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// verifyStorage checks that the storage component is healthy in the Harbor
// health response.
func (h *harborInit) verifyStorage(ctx context.Context) error {
	hr, err := h.fetchHealth(ctx)
	if err != nil {
		return err
	}

	for _, c := range hr.Components {
		if c.Name == "registryctl" || c.Name == "registry" {
			if c.Status != "healthy" {
				return fmt.Errorf("storage component %q is %s (expected healthy)", c.Name, c.Status)
			}
			h.logger.Info("storage component verified", "name", c.Name, "status", c.Status)
		}
	}
	return nil
}

// setSystemConfig applies system-level Harbor configuration. This call is
// idempotent (PUT overwrites).
func (h *harborInit) setSystemConfig(ctx context.Context) error {
	payload := map[string]any{
		"auth_mode":                    "db_auth",
		"project_creation_restriction": "adminonly",
		"robot_name_prefix":            "robot$",
		"robot_token_duration":         1440,
		"self_registration":            false,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, h.baseURL+"/api/v2.0/configurations", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating config request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", h.newPass)

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("config request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("system config returned %d: %s", resp.StatusCode, string(respBody))
	}

	h.logger.Info("system configuration applied",
		"auth_mode", "db_auth",
		"project_creation_restriction", "adminonly",
		"robot_token_duration", 1440,
		"self_registration", false,
	)
	return nil
}

// createCIRobot creates a system-level robot account for CI/CD pipeline
// image pushes. Idempotent: if a robot with the same name already exists
// (409 Conflict), the step is skipped.
func (h *harborInit) createCIRobot(ctx context.Context) error {
	robotName := "ci-pipeline"

	payload := map[string]any{
		"name":        robotName,
		"description": "CI/CD pipeline robot for pushing platform images to loopzero-platform",
		"duration":    -1,
		"level":       "system",
		"disable":     false,
		"permissions": []map[string]any{
			{
				"kind":      "project",
				"namespace": h.platform,
				"access": []map[string]string{
					{"resource": "repository", "action": "push"},
					{"resource": "repository", "action": "pull"},
					{"resource": "tag", "action": "create"},
					{"resource": "tag", "action": "list"},
					{"resource": "artifact", "action": "read"},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/api/v2.0/robots", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating robot request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", h.newPass)

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("robot creation request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusCreated:
		var result struct {
			ID     int    `json:"id"`
			Name   string `json:"name"`
			Secret string `json:"secret"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return fmt.Errorf("parsing robot response: %w", err)
		}
		h.logger.Info("CI robot created",
			"id", result.ID,
			"name", result.Name,
		)
		// Print the secret to stderr, apart from the JSON log stream on
		// stdout, so the caller can capture it (e.g. into a Kubernetes Secret
		// or vault) without it landing in collected logs.
		fmt.Fprintf(os.Stderr, "\n=== CI ROBOT CREDENTIALS ===\n")
		fmt.Fprintf(os.Stderr, "Username: %s\n", result.Name)
		fmt.Fprintf(os.Stderr, "Secret:   %s\n", result.Secret)
		fmt.Fprintf(os.Stderr, "============================\n\n")
		h.logger.Warn("robot secret printed to stderr — store it securely, it cannot be retrieved again")

	case http.StatusConflict:
		h.logger.Info("CI robot already exists, skipping", "name", robotName)

	default:
		return fmt.Errorf("robot creation returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

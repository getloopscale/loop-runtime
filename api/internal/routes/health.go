// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DBHealthChecker is an interface for checking database connectivity.
// This follows DIP — the handler depends on an abstraction, not a concrete DB connection.
type DBHealthChecker interface {
	Ping() error
}

// ServiceHealthChecker probes an external service for health.
// Each dependency (ArgoCD, Gitea, Argo Workflows) implements this.
type ServiceHealthChecker interface {
	Name() string
	Check(ctx context.Context) error
}

// HealthHandler provides HTTP handlers for health checks.
type HealthHandler struct {
	db       DBHealthChecker
	services []ServiceHealthChecker
}

// NewHealthHandler creates a new HealthHandler.
// The db parameter can be nil for basic health checks (GET /health).
func NewHealthHandler(db DBHealthChecker, services ...ServiceHealthChecker) *HealthHandler {
	return &HealthHandler{db: db, services: services}
}

// Health responds with a simple 200 OK.
// GET /api/v1/health — lightweight liveness probe.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// DeepHealth checks all dependencies and responds accordingly.
// GET /api/v1/health/deep — readiness probe.
//
// Checks: database + all registered services (ArgoCD, Gitea, Argo Workflows).
// Returns 200 if all healthy, 503 if any degraded.
// MEET-2026-03-27-012 Phase 1.3: verify ALL critical dependencies.
func (h *HealthHandler) DeepHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	result := make(map[string]string)
	degraded := false

	// Database check
	if h.db == nil || h.db.Ping() != nil {
		result["database"] = "disconnected"
		degraded = true
	} else {
		result["database"] = "connected"
	}

	// Service dependency checks
	for _, svc := range h.services {
		if err := svc.Check(ctx); err != nil {
			if strings.Contains(err.Error(), "auth_failed") {
				result[svc.Name()] = "auth_failed"
			} else {
				result[svc.Name()] = "unreachable"
			}
			degraded = true
		} else {
			result[svc.Name()] = "connected"
		}
	}

	if degraded {
		result["status"] = "degraded"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		result["status"] = "ok"
		w.WriteHeader(http.StatusOK)
	}
	_ = json.NewEncoder(w).Encode(result)
}

// HTTPServiceChecker checks an HTTP service by calling a health endpoint.
// Used for ArgoCD, Gitea, and any HTTP-based dependency.
type HTTPServiceChecker struct {
	name   string
	url    string
	token  string
	client *http.Client
}

// NewHTTPServiceChecker creates a checker that probes the given URL.
// For ArgoCD: url = "http://argocd-server:80/api/v1/applications", token = apiKey.
// For Gitea: url = "http://gitea-http:3000/api/v1/repos/search?limit=1", token = access_token.
func NewHTTPServiceChecker(name, url, token string, client *http.Client) *HTTPServiceChecker {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &HTTPServiceChecker{name: name, url: url, token: token, client: client}
}

func (c *HTTPServiceChecker) Name() string { return c.name }

func (c *HTTPServiceChecker) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return fmt.Errorf("%s: create request: %w", c.name, err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: unreachable: %w", c.name, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("%s: auth_failed", c.name)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s: status %d", c.name, resp.StatusCode)
	}
	return nil
}

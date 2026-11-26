// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package healthhttp serves /api/v1/health and the readiness probes behind it: the
// database check, the per-service HTTP checks, and cell discovery.
//
// Ninth family to leave the flat `routes` package (B-080, slice-32), extracted
// alongside `audit`.
//
// Depends on no sibling family and, unusually, not even on `httpio`: it writes
// its own responses because a health endpoint that fails to render is worse than
// one that renders plainly.
package healthhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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

// CellDiscoveryChecker reports how many cells the router currently knows.
// MEET-053 A-04: Used by DeepHealth to detect "0 cells" degraded state.
type CellDiscoveryChecker interface {
	// DiscoveredCellCount returns the number of cells the router currently has.
	DiscoveredCellCount() int
	// IsDynamic returns true if cell discovery is active (DynamicRouter).
	IsDynamic() bool
}

// HealthHandler provides HTTP handlers for health checks.
type HealthHandler struct {
	db       DBHealthChecker
	services []ServiceHealthChecker
	cells    CellDiscoveryChecker
}

// NewHealthHandler creates a new HealthHandler.
// The db parameter can be nil for basic health checks (GET /health).
func NewHealthHandler(db DBHealthChecker, services ...ServiceHealthChecker) *HealthHandler {
	return &HealthHandler{db: db, services: services}
}

// WithCellDiscovery attaches cell discovery checking to the health handler.
// MEET-053 A-04: DeepHealth returns 503 when DynamicRouter has 0 cells.
func (h *HealthHandler) WithCellDiscovery(checker CellDiscoveryChecker) *HealthHandler {
	h.cells = checker
	return h
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

	_, span := otel.Tracer("loop-runtime-api").Start(ctx, "loop-runtime.api.health.deep_check",
		trace.WithAttributes(
			attribute.String("loop-runtime.health.status", "pending"),
		))
	defer span.End()

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
			if errors.Is(err, ErrServiceAuthFailed) {
				result[svc.Name()] = "auth_failed"
			} else {
				result[svc.Name()] = "unreachable"
			}
			degraded = true
		} else {
			result[svc.Name()] = "connected"
		}
	}

	// MEET-053 A-04: Cell discovery check.
	// DynamicRouter with 0 cells = platform cannot serve any deploy operation.
	if h.cells != nil {
		count := h.cells.DiscoveredCellCount()
		if h.cells.IsDynamic() {
			if count == 0 {
				result["cells"] = "0 discovered (DEGRADED)"
				degraded = true
			} else {
				result["cells"] = fmt.Sprintf("%d discovered", count)
			}
		} else {
			result["cells"] = fmt.Sprintf("static (%d)", count)
		}
	}

	if degraded {
		result["status"] = "degraded"
		span.SetAttributes(attribute.String("loop-runtime.health.status", "degraded"))
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		result["status"] = "ok"
		span.SetAttributes(attribute.String("loop-runtime.health.status", "healthy"))
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

// ErrServiceAuthFailed is returned by a ServiceChecker whose dependency
// rejected its credentials (401/403) — reported as "auth_failed" rather than
// "unreachable", because the fix is a credential, not the network.
var ErrServiceAuthFailed = errors.New("auth_failed")

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
		return fmt.Errorf("%s: %w", c.name, ErrServiceAuthFailed)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s: status %d", c.name, resp.StatusCode)
	}
	return nil
}

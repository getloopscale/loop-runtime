// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/server"
)

// fakeTokenValidator implements auth.TokenValidator for server tests.
type fakeServerTokenValidator struct {
	result *auth.TokenValidationResult
	err    error
}

func (f *fakeServerTokenValidator) ValidateToken(ctx context.Context, token string) (*auth.TokenValidationResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// fakeServerMembershipStore implements auth.MembershipStore for server tests.
type fakeServerMembershipStore struct {
	memberships []auth.TenantMembership
	err         error
}

func (f *fakeServerMembershipStore) GetMemberships(ctx context.Context, userID string) ([]auth.TenantMembership, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.memberships, nil
}

func TestServerRouting(t *testing.T) {
	validator := &fakeServerTokenValidator{
		result: &auth.TokenValidationResult{
			UserID: "github-lucas",
			Email:  "lucas@acme.dev",
		},
	}
	memberStore := &fakeServerMembershipStore{
		memberships: []auth.TenantMembership{
			{TenantID: "acme", Role: "owner"},
		},
	}
	projectStore := &fakeProjectStore{projects: make(map[string]bool)}
	crMgr := &fakeTenantCRManager{}

	cfg := server.Config{
		Port:           "0",
		Logger:         nil,
		DB:             nil,
		TokenValidator: validator,
		MemberStore:    memberStore,
		ProjectStore:   projectStore,
		TenantCRMgr:    crMgr,
	}
	router := server.NewRouter(cfg)

	t.Run("GET /api/v1/health is public — no auth required", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/health/deep is public — no auth required", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		// 503 because DB is nil, but it should NOT be 401
		if rec.Code == http.StatusUnauthorized {
			t.Fatal("health/deep should be public, got 401")
		}
	})

	t.Run("GET /api/v1/auth/whoami returns 401 without token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/auth/whoami returns 200 with valid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["team"] != "acme" {
			t.Fatalf("expected team=acme, got %q", body["team"])
		}
		if body["email"] != "lucas@acme.dev" {
			t.Fatalf("expected email=lucas@acme.dev, got %q", body["email"])
		}
	})

	t.Run("POST /api/v1/projects returns 401 without token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("all responses have Correlation-ID header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		corrID := rec.Header().Get("Correlation-ID")
		if corrID == "" {
			t.Fatal("expected Correlation-ID header in response")
		}
	})

	t.Run("Correlation-ID is preserved when client provides one", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.Header.Set("Correlation-ID", "client-corr-123")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		corrID := rec.Header().Get("Correlation-ID")
		if corrID != "client-corr-123" {
			t.Fatalf("expected Correlation-ID=client-corr-123, got %q", corrID)
		}
	})
}

func TestServer404Returns_JSON(t *testing.T) {
	cfg := server.Config{
		Port:   "0",
		Logger: nil,
		DB:     nil,
	}
	router := server.NewRouter(cfg)

	t.Run("GET unknown route returns JSON 404 with LoopRuntimeError format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}

		ct := rec.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Fatalf("expected Content-Type application/json, got %q", ct)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("expected JSON body, got decode error: %v (body: %s)", err, rec.Body.String())
		}
		if body["code"] != "NOT_FOUND" {
			t.Errorf("expected code NOT_FOUND, got %v", body["code"])
		}
	})

	t.Run("POST to GET-only route returns JSON 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/health", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}

		ct := rec.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Fatalf("expected Content-Type application/json, got %q", ct)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("expected JSON body, got decode error: %v", err)
		}
		if body["code"] != "METHOD_NOT_ALLOWED" {
			t.Errorf("expected code METHOD_NOT_ALLOWED, got %v", body["code"])
		}
	})
}

func TestServerNilLogger(t *testing.T) {
	t.Run("NewRouter does not panic when Logger is nil", func(t *testing.T) {
		cfg := server.Config{
			Port:   "0",
			Logger: nil,
			DB:     nil,
		}
		router := server.NewRouter(cfg)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}

func TestServerMetricsNotOnMainRouter(t *testing.T) {
	t.Run("GET /metrics returns 404 on main router", func(t *testing.T) {
		cfg := server.Config{
			Port:   "0",
			Logger: nil,
			DB:     nil,
		}
		router := server.NewRouter(cfg)

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for /metrics on main router, got %d", rec.Code)
		}
	})
}

func TestValidateProductionEnv(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	t.Run("returns nil when all required vars are set", func(t *testing.T) {
		vars := map[string]string{
			"DATABASE_URL":                      "postgres://localhost:5432/loop_runtime",
			"LOOP_RUNTIME_ARGO_CALLBACK_SECRET": "some-secret",
			"GITEA_TOKEN":                       "tok-123",
			"ARGOCD_TOKEN":                      "tok-456",
		}
		err := server.ValidateProductionEnv(logger, vars)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("returns error when a required var is empty", func(t *testing.T) {
		vars := map[string]string{
			"DATABASE_URL":                      "postgres://localhost:5432/loop_runtime",
			"LOOP_RUNTIME_ARGO_CALLBACK_SECRET": "",
			"GITEA_TOKEN":                       "tok-123",
			"ARGOCD_TOKEN":                      "tok-456",
		}
		err := server.ValidateProductionEnv(logger, vars)
		if err == nil {
			t.Fatal("expected error for missing LOOP_RUNTIME_ARGO_CALLBACK_SECRET, got nil")
		}
	})

	t.Run("returns error listing all missing vars", func(t *testing.T) {
		vars := map[string]string{
			"DATABASE_URL":                      "",
			"LOOP_RUNTIME_ARGO_CALLBACK_SECRET": "",
			"GITEA_TOKEN":                       "",
			"ARGOCD_TOKEN":                      "",
		}
		err := server.ValidateProductionEnv(logger, vars)
		if err == nil {
			t.Fatal("expected error for all missing vars, got nil")
		}
	})
}

func TestServerConfigHasMetricsPort(t *testing.T) {
	t.Run("Config struct accepts MetricsPort", func(t *testing.T) {
		cfg := server.Config{
			Port:        "3001",
			MetricsPort: "9090",
		}
		if cfg.MetricsPort != "9090" {
			t.Fatalf("expected MetricsPort=9090, got %q", cfg.MetricsPort)
		}
	})
}

func TestMetricsServerStartsOnSeparatePort(t *testing.T) {
	t.Run("metrics server serves /metrics on separate port", func(t *testing.T) {
		cfg := server.Config{
			Port:        "0",
			MetricsPort: "0", // Let OS assign a port
			Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
		}

		// Use NewMetricsServer to get a testable server
		metricsSrv := server.NewMetricsServer(cfg.MetricsPort)

		// Use httptest to verify the handler responds to /metrics
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		metricsSrv.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 from metrics handler, got %d", rec.Code)
		}

		ct := rec.Header().Get("Content-Type")
		if ct == "" {
			t.Fatal("expected Content-Type header from metrics endpoint")
		}
	})
}

// Ensure unused imports don't cause build errors.
var _ = fmt.Sprintf

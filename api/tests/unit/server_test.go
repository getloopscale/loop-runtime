package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/server"
)

// fakeTokenValidator implements middleware.TokenValidator for server tests.
type fakeServerTokenValidator struct {
	result *middleware.TokenValidationResult
	err    error
}

func (f *fakeServerTokenValidator) ValidateToken(ctx context.Context, token string) (*middleware.TokenValidationResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// fakeServerMembershipStore implements middleware.MembershipStore for server tests.
type fakeServerMembershipStore struct {
	memberships []middleware.TenantMembership
	err         error
}

func (f *fakeServerMembershipStore) GetMemberships(ctx context.Context, userID string) ([]middleware.TenantMembership, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.memberships, nil
}

func TestServerRouting(t *testing.T) {
	validator := &fakeServerTokenValidator{
		result: &middleware.TokenValidationResult{
			UserID:   "github-lucas",
			Email:    "lucas@acme.dev",
			GitHubID: 12345,
		},
	}
	memberStore := &fakeServerMembershipStore{
		memberships: []middleware.TenantMembership{
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

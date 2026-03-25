package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/loopzero/loopzero/api/internal/routes"
)

// fakeSecretStore implements routes.SecretStore for testing.
type fakeSecretStore struct {
	secrets []routes.SecretMetadata
	err     error
}

func (f *fakeSecretStore) ListNames(ctx context.Context, tenantID, projectID, env string) ([]routes.SecretMetadata, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.secrets, nil
}

func (f *fakeSecretStore) Set(_ context.Context, _, _, _, _, _ string, _ []byte) (bool, error) {
	return true, nil
}

func (f *fakeSecretStore) Delete(_ context.Context, _, _, _, _, _ string) (bool, error) {
	return true, nil
}

func TestSecretsRoute(t *testing.T) {
	t.Run("GET /api/v1/secrets → 200 with secrets list", func(t *testing.T) {
		store := &fakeSecretStore{
			secrets: []routes.SecretMetadata{
				{Name: "DATABASE_URL", Environment: "production", App: "api"},
				{Name: "REDIS_URL", Environment: "production", App: "api"},
			},
		}
		handler := routes.NewSecretHandler(store)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets?env=production&project=proj-123", nil).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string][]routes.SecretMetadata
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		secrets := result["secrets"]
		if len(secrets) != 2 {
			t.Fatalf("expected 2 secrets, got %d", len(secrets))
		}
		if secrets[0].Name != "DATABASE_URL" {
			t.Errorf("expected first secret name 'DATABASE_URL', got '%s'", secrets[0].Name)
		}
		if secrets[1].Name != "REDIS_URL" {
			t.Errorf("expected second secret name 'REDIS_URL', got '%s'", secrets[1].Name)
		}
	})

	t.Run("GET /api/v1/secrets → 200 with empty list when no secrets", func(t *testing.T) {
		store := &fakeSecretStore{
			secrets: []routes.SecretMetadata{},
		}
		handler := routes.NewSecretHandler(store)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets?env=production&project=proj-123", nil).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string][]routes.SecretMetadata
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		secrets := result["secrets"]
		if len(secrets) != 0 {
			t.Fatalf("expected 0 secrets, got %d", len(secrets))
		}
	})

	t.Run("GET /api/v1/secrets → 401 without auth", func(t *testing.T) {
		store := &fakeSecretStore{
			secrets: []routes.SecretMetadata{},
		}
		handler := routes.NewSecretHandler(store)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets?env=production&project=proj-123", nil)
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/secrets → response never contains value field", func(t *testing.T) {
		store := &fakeSecretStore{
			secrets: []routes.SecretMetadata{
				{Name: "DATABASE_URL", Environment: "production", App: "api"},
			},
		}
		handler := routes.NewSecretHandler(store)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets?env=production&project=proj-123", nil).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Decode as raw JSON to check for unexpected fields
		var raw map[string][]map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		for _, secret := range raw["secrets"] {
			if _, hasValue := secret["value"]; hasValue {
				t.Fatal("response must NEVER contain a 'value' field")
			}
			// Verify expected fields exist
			if _, hasName := secret["name"]; !hasName {
				t.Error("expected 'name' field in secret")
			}
			if _, hasEnv := secret["environment"]; !hasEnv {
				t.Error("expected 'environment' field in secret")
			}
			if _, hasApp := secret["app"]; !hasApp {
				t.Error("expected 'app' field in secret")
			}
		}
	})

	t.Run("GET /api/v1/secrets → passes tenant ID to store for isolation", func(t *testing.T) {
		var capturedTenantID string
		store := &capturingSecretStore{
			onListNames: func(ctx context.Context, tenantID, projectID, env string) ([]routes.SecretMetadata, error) {
				capturedTenantID = tenantID
				return []routes.SecretMetadata{}, nil
			},
		}
		handler := routes.NewSecretHandler(store)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets?env=production&project=proj-123", nil).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if capturedTenantID != "acme" {
			t.Errorf("expected tenant ID 'acme' passed to store, got '%s'", capturedTenantID)
		}
	})
}

// capturingSecretStore captures call arguments for verification.
type capturingSecretStore struct {
	onListNames func(ctx context.Context, tenantID, projectID, env string) ([]routes.SecretMetadata, error)
}

func (c *capturingSecretStore) ListNames(ctx context.Context, tenantID, projectID, env string) ([]routes.SecretMetadata, error) {
	return c.onListNames(ctx, tenantID, projectID, env)
}

func (c *capturingSecretStore) Set(_ context.Context, _, _, _, _, _ string, _ []byte) (bool, error) {
	return true, nil
}

func (c *capturingSecretStore) Delete(_ context.Context, _, _, _, _, _ string) (bool, error) {
	return true, nil
}

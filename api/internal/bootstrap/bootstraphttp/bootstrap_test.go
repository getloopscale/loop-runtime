// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package bootstraphttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/bootstrap/bootstraphttp"
	"github.com/getloopscale/loop-runtime/api/internal/bootstrap/bootstraphttp/bootstraptest"
)

// --- Fake bootstrap.CellPlacementEngine ---

type fakeCellPlacement struct {
	cellID string
	err    error
}

func (f *fakeCellPlacement) AssignCell(_ context.Context) (string, error) {
	return f.cellID, f.err
}

func (f *fakeCellPlacement) AssignAndExecute(_ context.Context, fn func(ctx context.Context, cellID string) error) error {
	if f.err != nil {
		return f.err
	}
	return fn(context.Background(), f.cellID)
}

// --- Tests ---

func TestBootstrap_HappyPath(t *testing.T) {
	store := bootstraptest.NewInMemoryStore()
	placement := &fakeCellPlacement{cellID: "cell-0"}
	h := bootstraphttp.NewBootstrapHandler(store, "", placement)

	body := `{"admin_github_id":"12345","admin_login":"testuser","tenant_slug":"my-team"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Bootstrap(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp bootstraphttp.BootstrapResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("want status ok, got %s", resp.Status)
	}
	if !resp.Created {
		t.Fatal("want created=true for first bootstrap")
	}
	if resp.UserID == "" {
		t.Fatal("want non-empty user_id")
	}
	if resp.TenantID == "" {
		t.Fatal("want non-empty tenant_id")
	}
}

func TestBootstrap_Idempotent(t *testing.T) {
	store := bootstraptest.NewInMemoryStore()
	placement := &fakeCellPlacement{cellID: "cell-0"}
	h := bootstraphttp.NewBootstrapHandler(store, "", placement)

	body := `{"admin_github_id":"12345","admin_login":"testuser","tenant_slug":"my-team"}`

	// First call
	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Bootstrap(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first: want 200, got %d", w.Code)
	}

	// Second call (idempotent)
	req = httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	h.Bootstrap(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second: want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp bootstraphttp.BootstrapResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Created {
		t.Fatal("want created=false for idempotent re-call")
	}
}

func TestBootstrap_MissingGitHubID(t *testing.T) {
	store := bootstraptest.NewInMemoryStore()
	placement := &fakeCellPlacement{cellID: "cell-0"}
	h := bootstraphttp.NewBootstrapHandler(store, "", placement)

	body := `{"admin_login":"testuser","tenant_slug":"my-team"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Bootstrap(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBootstrap_MissingAdminLogin(t *testing.T) {
	store := bootstraptest.NewInMemoryStore()
	placement := &fakeCellPlacement{cellID: "cell-0"}
	h := bootstraphttp.NewBootstrapHandler(store, "", placement)

	body := `{"admin_github_id":"12345","tenant_slug":"my-team"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Bootstrap(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBootstrap_MissingTenantSlug(t *testing.T) {
	store := bootstraptest.NewInMemoryStore()
	placement := &fakeCellPlacement{cellID: "cell-0"}
	h := bootstraphttp.NewBootstrapHandler(store, "", placement)

	body := `{"admin_github_id":"12345","admin_login":"testuser"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Bootstrap(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBootstrap_InvalidJSON(t *testing.T) {
	store := bootstraptest.NewInMemoryStore()
	placement := &fakeCellPlacement{cellID: "cell-0"}
	h := bootstraphttp.NewBootstrapHandler(store, "", placement)

	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewBufferString("not-json"))
	w := httptest.NewRecorder()

	h.Bootstrap(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBootstrap_SecretRequired_Unauthorized(t *testing.T) {
	store := bootstraptest.NewInMemoryStore()
	placement := &fakeCellPlacement{cellID: "cell-0"}
	h := bootstraphttp.NewBootstrapHandler(store, "my-secret", placement)

	body := `{"admin_github_id":"12345","admin_login":"testuser","tenant_slug":"my-team"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewBufferString(body))
	// No Authorization header
	w := httptest.NewRecorder()

	h.Bootstrap(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBootstrap_SecretRequired_CorrectSecret(t *testing.T) {
	store := bootstraptest.NewInMemoryStore()
	placement := &fakeCellPlacement{cellID: "cell-0"}
	h := bootstraphttp.NewBootstrapHandler(store, "my-secret", placement)

	body := `{"admin_github_id":"12345","admin_login":"testuser","tenant_slug":"my-team"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer my-secret")
	w := httptest.NewRecorder()

	h.Bootstrap(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBootstrap_DefaultPlan(t *testing.T) {
	store := bootstraptest.NewInMemoryStore()
	placement := &fakeCellPlacement{cellID: "cell-0"}
	h := bootstraphttp.NewBootstrapHandler(store, "", placement)

	// No "plan" field — should default to "starter"
	body := `{"admin_github_id":"12345","admin_login":"testuser","tenant_slug":"my-team"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Bootstrap(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

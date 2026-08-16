// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Error-path coverage for the environment handler, moved out of the parent's
// coverage_boost_test.go when the family was extracted (B-080, slice-36).
package environments

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routes/routestest"
)

// =============================================================================
// EnvironmentHandler — additional error paths
// =============================================================================

func TestEnvs_List_ServiceError(t *testing.T) {
	mgr := &fakeEnvMgr{listErr: errors.New("list failed")}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestEnvs_Create_Unauthenticated(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	body := `{"name":"staging"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestEnvs_Create_InvalidJSON(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString("not-json"))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestEnvs_Create_ServiceError(t *testing.T) {
	mgr := &fakeEnvMgr{addErr: errors.New("add failed")}
	h := NewEnvironmentHandler(mgr)

	// M2 v1.2: project field required (T4.1).
	body := `{"name":"staging","project":"web"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

func TestEnvs_Get_Unauthenticated(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/prod", nil)
	req = routestest.WithChiParam(req, "name", "prod")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestEnvs_Get_MissingName(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestEnvs_Delete_Unauthenticated(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/staging", nil)
	req = routestest.WithChiParam(req, "name", "staging")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestEnvs_Delete_MissingName(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestEnvs_Delete_ServiceError(t *testing.T) {
	mgr := &fakeEnvMgr{removeErr: errors.New("remove failed")}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/staging", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "staging")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

// =============================================================================
// StubEnvironmentManager
// =============================================================================

func TestStubEnvMgr_ListEnvironments(t *testing.T) {
	stub := &StubEnvironmentManager{}
	envs, err := stub.ListEnvironments(context.Background(), "tn-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(envs) != 1 || envs[0].Name != "production" {
		t.Errorf("expected [production], got %v", envs)
	}
}

func TestStubEnvMgr_AddEnvironment(t *testing.T) {
	stub := &StubEnvironmentManager{}
	// M2 v1.2: signature carries `project`.
	if err := stub.AddEnvironment(context.Background(), "tn-test", "web", "staging"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStubEnvMgr_RemoveEnvironment(t *testing.T) {
	stub := &StubEnvironmentManager{}
	if err := stub.RemoveEnvironment(context.Background(), "tn-test", "web", "staging"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStubEnvMgr_GetEnvironmentDetail_Found(t *testing.T) {
	stub := &StubEnvironmentManager{}
	detail, err := stub.GetEnvironmentDetail(context.Background(), "tn-test", "", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail == nil {
		t.Fatal("expected non-nil detail for production")
	}
	if detail.Name != "production" {
		t.Errorf("expected 'production', got %q", detail.Name)
	}
}

func TestStubEnvMgr_GetEnvironmentDetail_NotFound(t *testing.T) {
	stub := &StubEnvironmentManager{}
	detail, err := stub.GetEnvironmentDetail(context.Background(), "tn-test", "web", "staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail != nil {
		t.Errorf("expected nil for non-production env, got %v", detail)
	}
}

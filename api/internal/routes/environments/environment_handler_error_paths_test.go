// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// The environment handler's refusal and failure paths: unauthenticated
// callers, malformed bodies, missing names and store failures.
package environments

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
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

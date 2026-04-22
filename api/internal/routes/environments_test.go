// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Fake EnvironmentManager ---

type fakeEnvMgr struct {
	envs      []EnvironmentEntry
	detail    *EnvironmentDetail
	addErr    error
	removeErr error
	listErr   error
}

func (f *fakeEnvMgr) ListEnvironments(_ context.Context, _ string) ([]EnvironmentEntry, error) {
	return f.envs, f.listErr
}

func (f *fakeEnvMgr) AddEnvironment(_ context.Context, _, _ string) error {
	return f.addErr
}

func (f *fakeEnvMgr) RemoveEnvironment(_ context.Context, _, _ string) error {
	return f.removeErr
}

func (f *fakeEnvMgr) GetEnvironmentDetail(_ context.Context, _, envName string) (*EnvironmentDetail, error) {
	if f.detail != nil && f.detail.Name == envName {
		return f.detail, nil
	}
	return nil, nil
}

// --- Tests ---

func TestEnvs_List_HappyPath(t *testing.T) {
	mgr := &fakeEnvMgr{
		envs: []EnvironmentEntry{
			{Name: "production", Status: "active"},
			{Name: "staging", Status: "active"},
		},
	}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string][]EnvironmentEntry
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp["environments"]) != 2 {
		t.Fatalf("want 2 environments, got %d", len(resp["environments"]))
	}
}

func TestEnvs_List_Unauthenticated(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestEnvs_Create_HappyPath(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	body := `{"name":"staging"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Create_InvalidName(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	body := `{"name":"BAD NAME"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Create_ReservedName(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	body := `{"name":"kube-system"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Get_Found(t *testing.T) {
	mgr := &fakeEnvMgr{
		detail: &EnvironmentDetail{
			Name:   "production",
			Status: "active",
		},
	}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/production", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "production")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Get_NotFound(t *testing.T) {
	mgr := &fakeEnvMgr{} // nil detail
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/nonexistent", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Delete_HappyPath(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/staging", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "staging")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Delete_CannotDeleteProduction(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/production", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "production")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

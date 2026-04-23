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
	"time"
)

// --- Fake ProjectStore ---

type fakeProjectStore struct {
	projects     []ProjectResult
	project      *ProjectResult
	createResult ProjectResult
	createErr    error
	deleteErr    error
}

func (f *fakeProjectStore) CreateProject(_ context.Context, input CreateProjectInput) (ProjectResult, error) {
	if f.createErr != nil {
		return ProjectResult{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeProjectStore) GetByName(_ context.Context, _, name string) (*ProjectResult, error) {
	if f.project != nil && f.project.Name == name {
		return f.project, nil
	}
	return nil, nil
}

func (f *fakeProjectStore) ListByTenant(_ context.Context, _ string) ([]ProjectResult, error) {
	return f.projects, nil
}

func (f *fakeProjectStore) DeleteProject(_ context.Context, _, _ string) error {
	return f.deleteErr
}

// --- Fake TenantCRManager ---

type fakeTenantCRMgr struct {
	err error
}

func (f *fakeTenantCRMgr) EnsureTenant(_ context.Context, _ string, _ TenantCRSpec) error {
	return f.err
}

// --- Fake ProjectDeleter ---

type fakeProjectDeleter struct {
	result *ProjectDeleteOutput
	err    error
}

func (f *fakeProjectDeleter) Delete(_ context.Context, _ ProjectDeleteInput) (*ProjectDeleteOutput, error) {
	return f.result, f.err
}

// --- Tests ---

func TestProjects_List_HappyPath(t *testing.T) {
	store := &fakeProjectStore{
		projects: []ProjectResult{
			{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
			{ID: "p2", Name: "my-web", TeamID: "tn-test-123"},
		},
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string][]ProjectResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp["projects"]) != 2 {
		t.Fatalf("want 2 projects, got %d", len(resp["projects"]))
	}
}

func TestProjects_List_Unauthenticated(t *testing.T) {
	h := NewProjectHandler(&fakeProjectStore{}, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestProjects_Get_Found(t *testing.T) {
	store := &fakeProjectStore{
		project: &ProjectResult{
			ID:        "p1",
			Name:      "my-api",
			TeamID:    "tn-test-123",
			CreatedAt: time.Now(),
		},
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp ProjectResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Name != "my-api" {
		t.Fatalf("want name my-api, got %s", resp.Name)
	}
}

func TestProjects_Get_NotFound(t *testing.T) {
	store := &fakeProjectStore{} // nil project
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nonexistent", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProjects_Create_HappyPath(t *testing.T) {
	store := &fakeProjectStore{
		createResult: ProjectResult{
			ID:     "p1",
			Name:   "my-api",
			TeamID: "tn-test-123",
		},
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	body := `{"name":"my-api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}

	var resp ProjectResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Name != "my-api" {
		t.Fatalf("want name my-api, got %s", resp.Name)
	}
}

func TestProjects_Create_AlreadyExists(t *testing.T) {
	store := &fakeProjectStore{
		createErr: ErrProjectExists,
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	body := `{"name":"my-api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProjects_Create_InvalidName(t *testing.T) {
	h := NewProjectHandler(&fakeProjectStore{}, &fakeTenantCRMgr{})

	body := `{"name":"UPPERCASE-BAD"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProjects_Create_InvalidJSON(t *testing.T) {
	h := NewProjectHandler(&fakeProjectStore{}, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString("not-json"))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProjects_Delete_Success(t *testing.T) {
	store := &fakeProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	deleter := &fakeProjectDeleter{
		result: &ProjectDeleteOutput{
			ProjectName:         "my-api",
			DeletedEnvironments: []string{"production"},
			CancelledBuilds:     0,
		},
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})
	h.SetDeleter(deleter)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/my-api", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProjects_Delete_NotFound(t *testing.T) {
	store := &fakeProjectStore{} // nil project
	h := NewProjectHandler(store, &fakeTenantCRMgr{})
	h.SetDeleter(&fakeProjectDeleter{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/nonexistent", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProjects_Delete_NoDeleter(t *testing.T) {
	store := &fakeProjectStore{}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})
	// deleter not set

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/my-api", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
	}
}

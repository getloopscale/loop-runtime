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

// --- Fake RepositoryStore ---

type fakeRepoStore struct {
	repo          *ProjectRepository
	repoByName    *ProjectRepository
	linkErr       error
	unlinkErr     error
	saveInstErr   error
	getByRepoErr  error
	getByProjErr  error
}

func (f *fakeRepoStore) LinkRepository(_ context.Context, _, _, _ string, _ int64, _ string) error {
	return f.linkErr
}

func (f *fakeRepoStore) UnlinkRepository(_ context.Context, _, _ string) error {
	return f.unlinkErr
}

func (f *fakeRepoStore) GetByProject(_ context.Context, _, _ string) (*ProjectRepository, error) {
	return f.repoByName, f.getByProjErr
}

func (f *fakeRepoStore) GetByRepo(_ context.Context, _ string) (*ProjectRepository, error) {
	return f.repo, f.getByRepoErr
}

func (f *fakeRepoStore) SaveInstallation(_ context.Context, _ int64, _, _ string, _ *string) error {
	return f.saveInstErr
}

// --- Tests ---

func TestRepositories_Connect_HappyPath(t *testing.T) {
	store := &fakeRepoStore{}
	h := NewRepositoryHandler(store)

	body := `{"repo":"owner/my-repo","installation_id":12345}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp connectResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Fatal("want ok=true")
	}
	if resp.Repo != "owner/my-repo" {
		t.Fatalf("want repo owner/my-repo, got %s", resp.Repo)
	}
	if resp.Branch != "main" {
		t.Fatalf("want default branch main, got %s", resp.Branch)
	}
}

func TestRepositories_Connect_CustomBranch(t *testing.T) {
	store := &fakeRepoStore{}
	h := NewRepositoryHandler(store)

	body := `{"repo":"owner/my-repo","installation_id":12345,"branch":"develop"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp connectResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Branch != "develop" {
		t.Fatalf("want branch develop, got %s", resp.Branch)
	}
}

func TestRepositories_Connect_MissingRepo(t *testing.T) {
	h := NewRepositoryHandler(&fakeRepoStore{})

	body := `{"installation_id":12345}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_Connect_AlreadyLinkedToOtherProject(t *testing.T) {
	store := &fakeRepoStore{
		repo: &ProjectRepository{
			TenantID:    "tn-other-tenant",
			ProjectName: "other-project",
		},
	}
	h := NewRepositoryHandler(store)

	body := `{"repo":"owner/my-repo","installation_id":12345}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_Connect_MemberForbidden(t *testing.T) {
	h := NewRepositoryHandler(&fakeRepoStore{})

	body := `{"repo":"owner/my-repo","installation_id":12345}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = withAuth(req, memberUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_Connect_Unauthenticated(t *testing.T) {
	h := NewRepositoryHandler(&fakeRepoStore{})

	body := `{"repo":"owner/my-repo","installation_id":12345}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_Disconnect_HappyPath(t *testing.T) {
	store := &fakeRepoStore{}
	h := NewRepositoryHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/my-api/connect", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Disconnect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_Disconnect_MemberForbidden(t *testing.T) {
	h := NewRepositoryHandler(&fakeRepoStore{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/my-api/connect", nil)
	req = withAuth(req, memberUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Disconnect(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_GetConnection_Found(t *testing.T) {
	store := &fakeRepoStore{
		repoByName: &ProjectRepository{
			ProjectName:  "my-api",
			RepoFullName: "owner/my-repo",
			DefaultBranch: "main",
			AutoDeploy:    true,
			Provider:      "github",
		},
	}
	h := NewRepositoryHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/connect", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.GetConnection(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_GetConnection_NotFound(t *testing.T) {
	store := &fakeRepoStore{repoByName: nil}
	h := NewRepositoryHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/connect", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.GetConnection(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_GetConnection_Unauthenticated(t *testing.T) {
	h := NewRepositoryHandler(&fakeRepoStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/connect", nil)
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.GetConnection(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package rollbackhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// --- Fake RollbackService ---

type fakeRollbackService struct {
	result *RollbackOutput
	err    error
}

func (f *fakeRollbackService) Rollback(_ context.Context, _ RollbackInput) (*RollbackOutput, error) {
	return f.result, f.err
}

// --- Fake RollbackProjectStore ---

type fakeRollbackProjectStore struct {
	project *ports.ProjectResult
}

func (f *fakeRollbackProjectStore) GetByName(_ context.Context, _, name string) (*ports.ProjectResult, error) {
	if f.project != nil && f.project.Name == name {
		return f.project, nil
	}
	return nil, nil
}

// --- Tests ---

func TestRollback_HappyPath(t *testing.T) {
	projects := &fakeRollbackProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakeRollbackService{
		result: &RollbackOutput{
			RolledBackReleaseID:  "rel-old",
			ReactivatedReleaseID: "rel-prev",
			Environment:          "production",
			URL:                  "https://my-api-production.dev.getloopscale.com",
		},
	}
	h := NewRollbackHandler(projects, svc, "dev.getloopscale.com")

	body := `{"environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/rollback", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	// 202: the manifests are pushed and the sync is triggered, and the cells
	// are still rolling. The release state settles after the answer (#118).
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}

	var resp rollbackResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "in_progress" {
		t.Fatalf("want status in_progress, got %s", resp.Status)
	}
	if resp.RolledBackRelease == "" || resp.ReactivatedRelease == "" || resp.Environment == "" {
		t.Errorf("body = %s, want the two releases and the environment", w.Body.String())
	}
}

func TestRollback_ProjectNotFound(t *testing.T) {
	projects := &fakeRollbackProjectStore{} // nil project
	svc := &fakeRollbackService{}
	h := NewRollbackHandler(projects, svc, "dev.getloopscale.com")

	body := `{"environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/nonexistent/rollback", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRollback_MissingEnvironment(t *testing.T) {
	projects := &fakeRollbackProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakeRollbackService{}
	h := NewRollbackHandler(projects, svc, "dev.getloopscale.com")

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/rollback", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRollback_NoActiveRelease(t *testing.T) {
	projects := &fakeRollbackProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakeRollbackService{
		err: loopruntimeerrors.ErrNoActiveRelease,
	}
	h := NewRollbackHandler(projects, svc, "dev.getloopscale.com")

	body := `{"environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/rollback", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRollback_Unauthenticated(t *testing.T) {
	h := NewRollbackHandler(&fakeRollbackProjectStore{}, &fakeRollbackService{}, "dev.getloopscale.com")

	body := `{"environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/rollback", bytes.NewBufferString(body))
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRollback_LockTimeout(t *testing.T) {
	projects := &fakeRollbackProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakeRollbackService{
		err: loopruntimeerrors.ErrLockTimeout,
	}
	h := NewRollbackHandler(projects, svc, "dev.getloopscale.com")

	body := `{"environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/rollback", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

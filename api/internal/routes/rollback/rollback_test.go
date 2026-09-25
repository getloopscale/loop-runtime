// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package rollback

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/routestest"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
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
			URL:                  "https://my-api-production.dev.loopzero.example",
		},
	}
	h := NewRollbackHandler(projects, svc, "dev.loopzero.example")

	body := `{"environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/rollback", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp rollbackResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "completed" {
		t.Fatalf("want status completed, got %s", resp.Status)
	}
}

func TestRollback_ProjectNotFound(t *testing.T) {
	projects := &fakeRollbackProjectStore{} // nil project
	svc := &fakeRollbackService{}
	h := NewRollbackHandler(projects, svc, "dev.loopzero.example")

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
	h := NewRollbackHandler(projects, svc, "dev.loopzero.example")

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
		err: loopzeroerrors.ErrNoActiveRelease,
	}
	h := NewRollbackHandler(projects, svc, "dev.loopzero.example")

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
	h := NewRollbackHandler(&fakeRollbackProjectStore{}, &fakeRollbackService{}, "dev.loopzero.example")

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
		err: loopzeroerrors.ErrLockTimeout,
	}
	h := NewRollbackHandler(projects, svc, "dev.loopzero.example")

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

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// --- Fake BuildOrchestratorService ---

type fakeBuildOrch struct {
	submitResult *SubmitBuildResult
	submitErr    error

	statusResult *BuildStatusResponse
	statusErr    error

	cancelErr error

	tenantID    string
	tenantIDErr error
}

func (f *fakeBuildOrch) SubmitBuild(_ context.Context, _ SubmitBuildInput) (*SubmitBuildResult, error) {
	return f.submitResult, f.submitErr
}

func (f *fakeBuildOrch) GetBuildStatus(_ context.Context, _ string) (*BuildStatusResponse, error) {
	return f.statusResult, f.statusErr
}

func (f *fakeBuildOrch) OnBuildCompleted(_ context.Context, _ string, _ BuildCallbackInput) error {
	return nil
}

func (f *fakeBuildOrch) IsBuildTerminal(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (f *fakeBuildOrch) CancelBuild(_ context.Context, _ string) error {
	return f.cancelErr
}

func (f *fakeBuildOrch) GetBuildTenantID(_ context.Context, _ string) (string, error) {
	return f.tenantID, f.tenantIDErr
}

// --- Tests ---

func TestBuilds_Create_HappyPath(t *testing.T) {
	orch := &fakeBuildOrch{
		submitResult: &SubmitBuildResult{
			BuildID:   "bld-abc123",
			StreamURL: "/api/v1/builds/bld-abc123/stream",
		},
	}
	h := NewBuildHandler(orch, slog.Default())

	body := `{"project_id":"my-project","environment":"production","commit_sha":"abc123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}

	var resp SubmitBuildResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BuildID != "bld-abc123" {
		t.Fatalf("want build_id bld-abc123, got %s", resp.BuildID)
	}
}

func TestBuilds_Create_MissingProject(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())

	body := `{"environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Create_MissingEnvironment(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())

	body := `{"project_id":"my-project"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Create_InvalidEnvironmentName(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())

	body := `{"project_id":"my-project","environment":"UPPERCASE"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Create_Unauthenticated(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())

	body := `{"project_id":"my-project","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	// No auth context
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Create_InvalidJSON(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString("not-json"))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Create_DeployInProgress(t *testing.T) {
	orch := &fakeBuildOrch{
		submitErr: loopzeroerrors.ErrDeployInProgress,
	}
	h := NewBuildHandler(orch, slog.Default())

	body := `{"project_id":"my-project","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Create_UnsupportedFramework(t *testing.T) {
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())

	body := `{"project_id":"my-project","environment":"production","apps":[{"name":"web","framework":"madeup-framework","build":"buildpack"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Get_Found(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantID: "tn-test-123",
		statusResult: &BuildStatusResponse{
			BuildID:     "bld-abc123",
			ProjectID:   "my-project",
			Environment: "production",
			Status:      "SUCCEEDED",
		},
	}
	h := NewBuildHandler(orch, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp BuildStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BuildID != "bld-abc123" {
		t.Fatalf("want build_id bld-abc123, got %s", resp.BuildID)
	}
}

func TestBuilds_Get_NotFound(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantIDErr: loopzeroerrors.ErrBuildNotFound,
	}
	h := NewBuildHandler(orch, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-nonexistent", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "bld-nonexistent")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Get_CrossTenantDenied(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantID: "tn-other-tenant", // different tenant owns the build
	}
	h := NewBuildHandler(orch, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123", nil)
	req = withAuth(req, defaultUserContext()) // tn-test-123
	req = withChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	// SEC-4: returns 404 not 403 to avoid leaking build existence
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for cross-tenant, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Cancel_Success(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantID: "tn-test-123",
	}
	h := NewBuildHandler(orch, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/bld-abc123/cancel", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Cancel(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Cancel_NotFound(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantIDErr: loopzeroerrors.ErrBuildNotFound,
	}
	h := NewBuildHandler(orch, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/bld-nonexistent/cancel", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "bld-nonexistent")
	w := httptest.NewRecorder()

	h.Cancel(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuilds_Cancel_AlreadyCompleted(t *testing.T) {
	orch := &fakeBuildOrch{
		tenantID:  "tn-test-123",
		cancelErr: loopzeroerrors.ErrBuildAlreadyCompleted,
	}
	h := NewBuildHandler(orch, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/bld-abc123/cancel", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Cancel(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

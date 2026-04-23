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

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// --- Fake PromoteService ---

type fakePromoteService struct {
	result *PromoteOutput
	err    error
}

func (f *fakePromoteService) Promote(_ context.Context, _ PromoteInput) (*PromoteOutput, error) {
	return f.result, f.err
}

// --- Fake PromoteProjectStore ---

type fakePromoteProjectStore struct {
	project *ProjectResult
}

func (f *fakePromoteProjectStore) GetByName(_ context.Context, _, name string) (*ProjectResult, error) {
	if f.project != nil && f.project.Name == name {
		return f.project, nil
	}
	return nil, nil
}

// --- Tests ---

func TestPromote_HappyPath(t *testing.T) {
	// Arrange
	projects := &fakePromoteProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{
		result: &PromoteOutput{
			PromotionID:    "promo-abc",
			SourceReleaseID: "rel-src-1",
			NewReleaseID:   "rel-new-1",
			FromEnv:        "staging",
			ToEnv:          "production",
			URL:            "https://my-api-production.dev.loopzero.example",
		},
	}
	h := NewPromoteHandler(projects, svc, "dev.loopzero.example")

	body := `{"from":"staging","to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	// Act
	h.Create(w, req)

	// Assert
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}

	var resp promoteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "promoting" {
		t.Errorf("want status promoting, got %s", resp.Status)
	}
	if resp.PromotionID != "promo-abc" {
		t.Errorf("want promotion_id promo-abc, got %s", resp.PromotionID)
	}
	if resp.FromEnv != "staging" {
		t.Errorf("want from staging, got %s", resp.FromEnv)
	}
	if resp.ToEnv != "production" {
		t.Errorf("want to production, got %s", resp.ToEnv)
	}
}

func TestPromote_ProjectNotFound(t *testing.T) {
	projects := &fakePromoteProjectStore{} // nil project
	svc := &fakePromoteService{}
	h := NewPromoteHandler(projects, svc, "dev.loopzero.example")

	body := `{"from":"staging","to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/nonexistent/promote", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPromote_MissingFromEnv(t *testing.T) {
	projects := &fakePromoteProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{}
	h := NewPromoteHandler(projects, svc, "dev.loopzero.example")

	body := `{"to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPromote_MissingToEnv(t *testing.T) {
	projects := &fakePromoteProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{}
	h := NewPromoteHandler(projects, svc, "dev.loopzero.example")

	body := `{"from":"staging"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPromote_SameEnvironment(t *testing.T) {
	projects := &fakePromoteProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{
		err: loopzeroerrors.ErrSameEnvironment,
	}
	h := NewPromoteHandler(projects, svc, "dev.loopzero.example")

	body := `{"from":"staging","to":"staging"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPromote_NoActiveRelease(t *testing.T) {
	projects := &fakePromoteProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{
		err: loopzeroerrors.ErrNoActiveRelease,
	}
	h := NewPromoteHandler(projects, svc, "dev.loopzero.example")

	body := `{"from":"staging","to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPromote_Unauthenticated(t *testing.T) {
	h := NewPromoteHandler(&fakePromoteProjectStore{}, &fakePromoteService{}, "dev.loopzero.example")

	body := `{"from":"staging","to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPromote_LockTimeout(t *testing.T) {
	projects := &fakePromoteProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{
		err: loopzeroerrors.ErrLockTimeout,
	}
	h := NewPromoteHandler(projects, svc, "dev.loopzero.example")

	body := `{"from":"staging","to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

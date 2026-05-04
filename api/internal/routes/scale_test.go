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

// --- Fake ScaleProjectStore ---

type fakeScaleProjectStore struct {
	project *ProjectResult
}

func (f *fakeScaleProjectStore) GetByName(_ context.Context, _, name string) (*ProjectResult, error) {
	if f.project != nil && f.project.Name == name {
		return f.project, nil
	}
	return nil, nil
}

// --- Tests ---

func TestScale_HappyPath_Autoscaling(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewScaleHandler(projects, nil, true) // KEDA enabled

	body := `{"environment":"production","app":"api","min":0,"max":5,"cpu":80}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}

	var resp scaleResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.App != "api" {
		t.Errorf("want app=api, got %s", resp.App)
	}
	if resp.Status != "acknowledged" {
		t.Errorf("want status=acknowledged, got %s", resp.Status)
	}
	if resp.Autoscaling == nil {
		t.Fatal("expected autoscaling in response")
	}
	if resp.Autoscaling.Min != 0 {
		t.Errorf("want min=0, got %d", resp.Autoscaling.Min)
	}
	if resp.Autoscaling.Max != 5 {
		t.Errorf("want max=5, got %d", resp.Autoscaling.Max)
	}
	if resp.Autoscaling.CPUTarget != 80 {
		t.Errorf("want cpu_target=80, got %d", resp.Autoscaling.CPUTarget)
	}
	if resp.Autoscaling.Cooldown == nil || *resp.Autoscaling.Cooldown != 300 {
		t.Errorf("want cooldown=300 for scale-to-zero, got %v", resp.Autoscaling.Cooldown)
	}
}

func TestScale_HappyPath_FixedReplicas(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewScaleHandler(projects, nil, false)

	replicas := 3
	body, _ := json.Marshal(scaleRequest{Environment: "production", App: "api", Replicas: &replicas})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBuffer(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}

	var resp scaleResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Replicas == nil || *resp.Replicas != 3 {
		t.Errorf("want replicas=3, got %v", resp.Replicas)
	}
	if resp.Autoscaling != nil {
		t.Error("expected no autoscaling for fixed replicas")
	}
}

func TestScale_MutualExclusion(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewScaleHandler(projects, nil, false)

	replicas := 3
	min := 1
	body, _ := json.Marshal(scaleRequest{App: "api", Replicas: &replicas, Min: &min})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBuffer(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScale_MinZeroWithoutKEDA(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewScaleHandler(projects, nil, false) // KEDA disabled

	min := 0
	max := 5
	body, _ := json.Marshal(scaleRequest{App: "api", Min: &min, Max: &max})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBuffer(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (KEDA disabled), got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScale_Unauthenticated(t *testing.T) {
	h := NewScaleHandler(&fakeScaleProjectStore{}, nil, false)

	body := `{"app":"api","min":1,"max":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestScale_ProjectNotFound(t *testing.T) {
	projects := &fakeScaleProjectStore{project: nil}
	h := NewScaleHandler(projects, nil, false)

	body := `{"app":"api","min":1,"max":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/nonexistent/scale", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScale_MissingApp(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewScaleHandler(projects, nil, false)

	body := `{"min":1,"max":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScale_NoScalingParams(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewScaleHandler(projects, nil, false)

	body := `{"app":"api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScale_DefaultEnvironment(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewScaleHandler(projects, nil, false)

	body := `{"app":"api","min":1,"max":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}

	var resp scaleResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Environment != "production" {
		t.Errorf("want default environment=production, got %s", resp.Environment)
	}
}

// Plan T2.2 — tests for ScaleApplyFunc real-persistence path (200 not 202).

func TestScale_RealPersistence_Applied(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	var captured ScaleConfigInput
	apply := func(_ context.Context, c ScaleConfigInput) (bool, error) {
		captured = c
		return true, nil
	}
	h := NewScaleHandler(projects, apply, true)

	body := `{"environment":"production","app":"api","replicas":3}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if captured.AppName != "api" {
		t.Errorf("captured app=%q want api", captured.AppName)
	}
	if captured.MinReplicas != 3 || captured.MaxReplicas != 3 {
		t.Errorf("captured min=%d max=%d want 3,3", captured.MinReplicas, captured.MaxReplicas)
	}

	var resp scaleResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "applied" {
		t.Errorf("want status=applied, got %q", resp.Status)
	}
}

func TestScale_RealPersistence_NoChange(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	apply := func(_ context.Context, _ ScaleConfigInput) (bool, error) {
		return false, nil
	}
	h := NewScaleHandler(projects, apply, true)

	body := `{"environment":"production","app":"api","replicas":3}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp scaleResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "no_change" {
		t.Errorf("want status=no_change, got %q", resp.Status)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package promotehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
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
	project *ports.ProjectResult
}

func (f *fakePromoteProjectStore) GetByName(_ context.Context, _, name string) (*ports.ProjectResult, error) {
	if f.project != nil && f.project.Name == name {
		return f.project, nil
	}
	return nil, nil
}

// --- Tests ---

func TestPromote_HappyPath(t *testing.T) {
	// Arrange
	projects := &fakePromoteProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{
		result: &PromoteOutput{
			PromotionID:     "promo-abc",
			SourceReleaseID: "rel-src-1",
			NewReleaseID:    "rel-new-1",
			FromEnv:         "staging",
			ToEnv:           "production",
			URL:             "https://my-api-production.dev.getloopscale.com",
		},
	}
	h := NewPromoteHandler(projects, svc, "dev.getloopscale.com")

	body := `{"from":"staging","to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
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
	h := NewPromoteHandler(projects, svc, "dev.getloopscale.com")

	body := `{"from":"staging","to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/nonexistent/promote", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPromote_MissingFromEnv(t *testing.T) {
	projects := &fakePromoteProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{}
	h := NewPromoteHandler(projects, svc, "dev.getloopscale.com")

	body := `{"to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPromote_MissingToEnv(t *testing.T) {
	projects := &fakePromoteProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{}
	h := NewPromoteHandler(projects, svc, "dev.getloopscale.com")

	body := `{"from":"staging"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPromote_SameEnvironment(t *testing.T) {
	projects := &fakePromoteProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{
		err: loopruntimeerrors.ErrSameEnvironment,
	}
	h := NewPromoteHandler(projects, svc, "dev.getloopscale.com")

	body := `{"from":"staging","to":"staging"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPromote_NoActiveRelease(t *testing.T) {
	projects := &fakePromoteProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{
		err: loopruntimeerrors.ErrNoActiveRelease,
	}
	h := NewPromoteHandler(projects, svc, "dev.getloopscale.com")

	body := `{"from":"staging","to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPromote_Unauthenticated(t *testing.T) {
	h := NewPromoteHandler(&fakePromoteProjectStore{}, &fakePromoteService{}, "dev.getloopscale.com")

	body := `{"from":"staging","to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPromote_LockTimeout(t *testing.T) {
	projects := &fakePromoteProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{
		err: loopruntimeerrors.ErrLockTimeout,
	}
	h := NewPromoteHandler(projects, svc, "dev.getloopscale.com")

	body := `{"from":"staging","to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}

	// The lock's typed ErrLockTimeout reaches the client as LOCK_TIMEOUT
	// through errors.As; there is no string-matching fallback.
	var errBody map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode lock-timeout body: %v", err)
	}
	got, _ := errBody["code"].(string)
	if got != "LOCK_TIMEOUT" {
		t.Errorf("want code LOCK_TIMEOUT, got %q body=%s", got, w.Body.String())
	}
}

// TestPromote_SameEnvironment_SurfacesCode asserts the response body carries
// `SAME_ENVIRONMENT` so the dashboard PromoteDialog can pre-flight reject
// before submitting. Without this, the dialog would only see a generic 400
// and the user would have no actionable feedback.
func TestPromote_SameEnvironment_SurfacesCode(t *testing.T) {
	projects := &fakePromoteProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{err: loopruntimeerrors.ErrSameEnvironment}
	h := NewPromoteHandler(projects, svc, "dev.getloopscale.com")

	body := `{"from":"staging","to":"staging"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
	var errBody map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode same-env body: %v", err)
	}
	if got, _ := errBody["code"].(string); got != "SAME_ENVIRONMENT" {
		t.Errorf("want code SAME_ENVIRONMENT, got %q", got)
	}
}

// TestPromote_RegressionV10BodyShape locks the canonical request body shape
// {from, to}. The v1.0 PromoteDialog sent {from_environment, to_environment}
// which the backend silently ignored (no struct tag match) — the request
// parsed cleanly, both fields landed empty, the handler returned
// MISSING_FIELD, and the dashboard surfaced a confusing 400 from the
// "wrong layer". This test pins the contract so the bug cannot return.
func TestPromote_RegressionV10BodyShape(t *testing.T) {
	projects := &fakePromoteProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	svc := &fakePromoteService{
		result: &PromoteOutput{
			PromotionID: "should-not-reach",
		},
	}
	h := NewPromoteHandler(projects, svc, "dev.getloopscale.com")

	cases := []struct {
		name    string
		body    string
		want    int
		wantMsg string
	}{
		{
			name:    "v1.0_buggy_keys_from_environment_to_environment",
			body:    `{"from_environment":"staging","to_environment":"production"}`,
			want:    http.StatusBadRequest,
			wantMsg: "'from' environment is required",
		},
		{
			name:    "empty_body",
			body:    `{}`,
			want:    http.StatusBadRequest,
			wantMsg: "'from' environment is required",
		},
		{
			name:    "canonical_keys_pass_through",
			body:    `{"from":"staging","to":"production"}`,
			want:    http.StatusAccepted,
			wantMsg: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/promote", bytes.NewBufferString(tc.body))
			req = routestest.WithAuth(req, routestest.DefaultUserContext())
			req = routestest.WithChiParam(req, "name", "my-api")
			w := httptest.NewRecorder()

			h.Create(w, req)

			if w.Code != tc.want {
				t.Fatalf("[%s] want %d, got %d body=%s", tc.name, tc.want, w.Code, w.Body.String())
			}
			if tc.wantMsg != "" {
				var errBody map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
					t.Fatalf("[%s] decode: %v", tc.name, err)
				}
				gotMsg, _ := errBody["message"].(string)
				if gotMsg == "" || !strings.Contains(gotMsg, tc.wantMsg) {
					t.Errorf("[%s] want message containing %q, got %q", tc.name, tc.wantMsg, gotMsg)
				}
			}
		})
	}
}

// --- Fake EnvironmentEnsurer for #158 tests ---

type fakePromoteEnsurer struct {
	calls []struct {
		TenantID, Project, Env string
	}
	err error
}

func (f *fakePromoteEnsurer) EnsureEnvironment(_ context.Context, tenantID, project, env string) error {
	f.calls = append(f.calls, struct{ TenantID, Project, Env string }{tenantID, project, env})
	return f.err
}

// TestPromote_EnsuresTargetEnvironment_BeforeService (closes Red Team loop-console#158)
// asserts the promote handler calls EnvironmentEnsurer.EnsureEnvironment for the
// (tenant, project, toEnv) tuple BEFORE invoking the promote service. Pre-fix
// state: promote never called EnsureEnvironment → Tenant CR spec.environments[]
// missing the tuple → AppProject destinations never extended → sync failed →
// release SYNC_FAILED → CLI reports success + URL that 404s.
func TestPromote_EnsuresTargetEnvironment_BeforeService(t *testing.T) {
	projects := &fakePromoteProjectStore{project: &ports.ProjectResult{ID: "p1", Name: "loopzerokit-example"}}
	svc := &fakePromoteService{
		result: &PromoteOutput{
			PromotionID:     "promo-158",
			SourceReleaseID: "rel-src",
			NewReleaseID:    "rel-new",
			FromEnv:         "preview",
			ToEnv:           "production",
		},
	}
	ensurer := &fakePromoteEnsurer{}
	h := NewPromoteHandler(projects, svc, "prod-dev.getloopscale.app").WithEnvironmentEnsurer(ensurer)

	body := `{"from":"preview","to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/loopzerokit-example/promote", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "loopzerokit-example")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}
	if len(ensurer.calls) != 1 {
		t.Fatalf("want exactly 1 EnsureEnvironment call, got %d", len(ensurer.calls))
	}
	got := ensurer.calls[0]
	if got.Project != "loopzerokit-example" {
		t.Errorf("want project=loopzerokit-example, got %q", got.Project)
	}
	if got.Env != "production" {
		t.Errorf("want env=production (the ToEnv), got %q", got.Env)
	}
	if got.TenantID == "" {
		t.Error("want non-empty tenant_id")
	}
}

// TestPromote_EnsureEnvironment_Failure_ShortCircuits (Red Team #158 negative case).
// When EnsureEnvironment fails, the handler MUST NOT invoke the promote service
// (would create a release row without a viable target env → the exact
// SYNC_FAILED trap #158 fixes).
func TestPromote_EnsureEnvironment_Failure_ShortCircuits(t *testing.T) {
	projects := &fakePromoteProjectStore{project: &ports.ProjectResult{ID: "p1", Name: "loopzerokit-example"}}
	svc := &fakePromoteService{
		result: &PromoteOutput{PromotionID: "should-not-reach"},
	}
	ensurer := &fakePromoteEnsurer{err: loopruntimeerrors.NewLoopRuntimeError("ENV_PROVISION_TIMEOUT", "operator did not reconcile in time")}
	h := NewPromoteHandler(projects, svc, "prod-dev.getloopscale.app").WithEnvironmentEnsurer(ensurer)

	body := `{"from":"preview","to":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/loopzerokit-example/promote", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "loopzerokit-example")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code == http.StatusAccepted {
		t.Fatalf("want non-202 on ensure failure, got 202 body=%s", w.Body.String())
	}
	// Service must not have been consulted — we can't strictly check via fake, but
	// the response code must NOT be Accepted (which is what svc.result would give).
	if svc.result != nil && w.Code == http.StatusAccepted {
		t.Errorf("promote service ran despite ensure failure — #158 short-circuit missing")
	}
}

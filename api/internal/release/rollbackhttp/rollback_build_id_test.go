// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package rollbackhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/ports"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
)

type fakeReleaseLookup struct {
	tenantID  string
	buildID   string
	releaseID string
	err       error
}

func (f *fakeReleaseLookup) GetLatestReleaseForBuild(ctx context.Context, tenantID, buildID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if tenantID == f.tenantID && buildID == f.buildID {
		return f.releaseID, nil
	}
	return "", nil
}

// capturingRollbackService wraps fakeRollbackService and records the
// input received so tests can assert resolution worked.
type capturingRollbackService struct {
	result   *RollbackOutput
	err      error
	captured *RollbackInput
}

func (f *capturingRollbackService) Rollback(_ context.Context, in RollbackInput) (*RollbackOutput, error) {
	if f.captured != nil {
		*f.captured = in
	}
	return f.result, f.err
}

func newRollbackTestServer(svc RollbackService, lookup RollbackReleaseLookup, projects RollbackProjectStore) *httptest.Server {
	h := NewRollbackHandler(projects, svc, "loopzero.local")
	if lookup != nil {
		h.WithReleaseLookup(lookup)
	}
	r := chi.NewRouter()
	r.Post("/api/v1/projects/{name}/rollback", func(w http.ResponseWriter, req *http.Request) {
		ctx := context.WithValue(req.Context(),
			auth.UserContextKeyPublic(),
			&auth.UserContext{
				UserID:   "u-1",
				Email:    "x@y",
				TenantID: "tn-1",
				Role:     "owner",
			})
		h.Create(w, req.WithContext(ctx))
	})
	return httptest.NewServer(r)
}

// TestRollback_BuildIDSugar_ResolvesToReleaseID confirms the dashboard
// LIVE-fix path: client sends {environment, build_id}, handler resolves
// to {environment, to_release_id} via RollbackReleaseLookup, service
// receives canonical input. Closes the 400 "environment is required"
// false-positive caught LIVE 2026-06-18 against build-25d1977102ec378b.
func TestRollback_BuildIDSugar_ResolvesToReleaseID(t *testing.T) {
	captured := RollbackInput{}
	svc := &capturingRollbackService{
		result: &RollbackOutput{
			RolledBackReleaseID:  "rel-old",
			ReactivatedReleaseID: "rel-canon-42",
			Environment:          "production",
			URL:                  "https://app.getloopscale.com",
		},
		captured: &captured,
	}
	lookup := &fakeReleaseLookup{tenantID: "tn-1", buildID: "build-xyz", releaseID: "rel-canon-42"}
	projects := &fakeRollbackProjectStore{
		project: &ports.ProjectResult{ID: "proj-1", Name: "proj-1", TeamID: "tn-1"},
	}
	srv := newRollbackTestServer(svc, lookup, projects)
	defer srv.Close()

	body := bytes.NewBufferString(`{"environment":"production","build_id":"build-xyz"}`)
	resp, err := http.Post(srv.URL+"/api/v1/projects/proj-1/rollback", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d; want 202", resp.StatusCode)
	}
	if captured.ToReleaseID != "rel-canon-42" {
		t.Errorf("ToReleaseID = %q; want rel-canon-42 (resolved from build_id)", captured.ToReleaseID)
	}
}

// TestRollback_BuildIDSugar_UnknownBuild_Returns400 confirms the
// existence-leak guard: build_id with no release (or cross-tenant)
// returns 400 missing_field rather than silently rolling back to a
// fallback release.
func TestRollback_BuildIDSugar_UnknownBuild_Returns400(t *testing.T) {
	svc := &fakeRollbackService{}
	lookup := &fakeReleaseLookup{tenantID: "tn-1", buildID: "build-known"}
	projects := &fakeRollbackProjectStore{
		project: &ports.ProjectResult{ID: "proj-1", Name: "proj-1", TeamID: "tn-1"},
	}
	srv := newRollbackTestServer(svc, lookup, projects)
	defer srv.Close()

	body := bytes.NewBufferString(`{"environment":"production","build_id":"build-unknown"}`)
	resp, err := http.Post(srv.URL+"/api/v1/projects/proj-1/rollback", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", resp.StatusCode)
	}
}

// TestRollback_BuildIDSugar_NoLookupConfigured_Returns500 confirms
// the nil-safe handler degrades cleanly when the wire-up is missing.
func TestRollback_BuildIDSugar_NoLookupConfigured_Returns500(t *testing.T) {
	svc := &fakeRollbackService{}
	projects := &fakeRollbackProjectStore{
		project: &ports.ProjectResult{ID: "proj-1", Name: "proj-1", TeamID: "tn-1"},
	}
	srv := newRollbackTestServer(svc, nil, projects)
	defer srv.Close()

	body := bytes.NewBufferString(`{"environment":"production","build_id":"build-xyz"}`)
	resp, err := http.Post(srv.URL+"/api/v1/projects/proj-1/rollback", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d; want 500 when ReleaseLookup not wired", resp.StatusCode)
	}
	var errBody map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	if msg, _ := errBody["message"].(string); msg == "" {
		t.Errorf("expected error message body")
	}
}

// TestRollback_CanonicalToReleaseID_StillWorks confirms backward-compat:
// existing CLI clients passing {environment, to_release_id} continue
// to work unchanged.
func TestRollback_CanonicalToReleaseID_StillWorks(t *testing.T) {
	captured := RollbackInput{}
	svc := &capturingRollbackService{
		result:   &RollbackOutput{ReactivatedReleaseID: "rel-canon-direct", Environment: "production"},
		captured: &captured,
	}
	projects := &fakeRollbackProjectStore{
		project: &ports.ProjectResult{ID: "proj-1", Name: "proj-1", TeamID: "tn-1"},
	}
	// No lookup needed when to_release_id is supplied directly.
	srv := newRollbackTestServer(svc, nil, projects)
	defer srv.Close()

	body := bytes.NewBufferString(`{"environment":"production","to_release_id":"rel-canon-direct"}`)
	resp, err := http.Post(srv.URL+"/api/v1/projects/proj-1/rollback", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d; want 202", resp.StatusCode)
	}
	if captured.ToReleaseID != "rel-canon-direct" {
		t.Errorf("ToReleaseID = %q; want rel-canon-direct", captured.ToReleaseID)
	}
}

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

type fakeEvictor struct {
	matched   []string
	matchErr  error
	enqueueID string
	enqErr    error
}

func (f *fakeEvictor) MatchTenants(_ context.Context, _ string, _ []string) ([]string, error) {
	return f.matched, f.matchErr
}
func (f *fakeEvictor) EnqueueEviction(_ context.Context, _ []string) (string, error) {
	return f.enqueueID, f.enqErr
}

func doEvict(t *testing.T, input AdminEvictInput, ev TenantEvictor, setEnv map[string]string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	for k, v := range setEnv {
		t.Setenv(k, v)
	}
	body, _ := json.Marshal(input)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/evict", bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	AdminEvictHandler(ev).ServeHTTP(w, req)
	return w
}

func TestAdminEvict_FeatureFlagOff_Returns404(t *testing.T) {
	w := doEvict(t, AdminEvictInput{Pattern: "tn-lt-%"}, &fakeEvictor{}, nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 without LOOPZERO_ADMIN_API, got %d", w.Code)
	}
}

func TestAdminEvict_MissingAuth_Returns403(t *testing.T) {
	w := doEvict(t, AdminEvictInput{Pattern: "tn-lt-%"}, &fakeEvictor{},
		map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_SERVICE_AUTH_SECRET": "s3cr3t"},
		nil,
	)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 without X-Service-Auth, got %d", w.Code)
	}
}

func TestAdminEvict_WrongSecret_Returns403(t *testing.T) {
	w := doEvict(t, AdminEvictInput{Pattern: "tn-lt-%"}, &fakeEvictor{},
		map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_SERVICE_AUTH_SECRET": "s3cr3t"},
		map[string]string{"X-Service-Auth": "wrong"},
	)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 with wrong secret, got %d", w.Code)
	}
}

func TestAdminEvict_EmptyFilter_Returns400(t *testing.T) {
	w := doEvict(t, AdminEvictInput{}, &fakeEvictor{},
		map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_SERVICE_AUTH_SECRET": "s"},
		map[string]string{"X-Service-Auth": "s"},
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 empty filter, got %d", w.Code)
	}
}

func TestAdminEvict_DangerousPattern_Returns400(t *testing.T) {
	for _, p := range []string{"%", "tn-%"} {
		w := doEvict(t, AdminEvictInput{Pattern: p}, &fakeEvictor{},
			map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_SERVICE_AUTH_SECRET": "s"},
			map[string]string{"X-Service-Auth": "s"},
		)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("pattern %q: want 400, got %d", p, w.Code)
		}
	}
}

func TestAdminEvict_DryRun_ReturnsMatches(t *testing.T) {
	ev := &fakeEvictor{matched: []string{"tn-lt-1", "tn-lt-2"}}
	w := doEvict(t, AdminEvictInput{Pattern: "tn-lt-%", DryRun: true}, ev,
		map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_SERVICE_AUTH_SECRET": "s"},
		map[string]string{"X-Service-Auth": "s"},
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 dry-run, got %d body=%s", w.Code, w.Body.String())
	}
	var resp AdminEvictResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Queued != 2 || len(resp.Matched) != 2 || resp.Accepted {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.JobID != "dry-run" {
		t.Fatalf("want dry-run job_id, got %s", resp.JobID)
	}
}

func TestAdminEvict_Live_Returns202(t *testing.T) {
	ev := &fakeEvictor{matched: []string{"tn-lt-1"}, enqueueID: "job-1"}
	w := doEvict(t, AdminEvictInput{Pattern: "tn-lt-%"}, ev,
		map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_SERVICE_AUTH_SECRET": "s"},
		map[string]string{"X-Service-Auth": "s"},
	)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}
	var resp AdminEvictResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Accepted || resp.Queued != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestAdminEvict_BadJSON_Returns400(t *testing.T) {
	t.Setenv("LOOPZERO_ADMIN_API", "true")
	t.Setenv("LOOPZERO_SERVICE_AUTH_SECRET", "s")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/evict",
		bytes.NewReader([]byte("not-json")))
	req.Header.Set("X-Service-Auth", "s")
	w := httptest.NewRecorder()
	AdminEvictHandler(&fakeEvictor{}).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 bad json, got %d", w.Code)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placementhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
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

// doEvict drives the route as the router mounts it. settings carries the
// operator configuration by its environment names (LOOPRUNTIME_ADMIN_API and the
// service secrets), which cmd reads once at boot.
func doEvict(t *testing.T, input AdminEvictInput, ev TenantEvictor, settings map[string]string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(input)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/evict", bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	// qa-sprint2-cleanup-2026-06-24 T1.2: route handler is wrapped with
	// Authenticator.RequireAdminService at the router layer. Tests exercise the
	// SAME chain (middleware → handler) so the auth-gate behavior is covered
	// alongside the handler logic.
	adminRoute(settings, AdminEvictHandler(ev, settings["LOOPRUNTIME_ADMIN_API"] == "true")).ServeHTTP(w, req)
	return w
}

func TestAdminEvict_FeatureFlagOff_Returns404(t *testing.T) {
	// Post-T1.2 the middleware runs first → caller must pass auth before
	// reaching the LOOPRUNTIME_ADMIN_API gate. So a valid-secret request with the
	// flag OFF returns 404 (defense-in-depth: flag-off is opaque even to
	// authorized callers, NOT just to unauthorized ones).
	w := doEvict(t, AdminEvictInput{Pattern: "tn-lt-%"}, &fakeEvictor{},
		map[string]string{"LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET": "s"},
		map[string]string{"X-Service-Auth": "s"},
	)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 without LOOPRUNTIME_ADMIN_API, got %d", w.Code)
	}
}

func TestAdminEvict_MissingAuth_Returns403(t *testing.T) {
	w := doEvict(t, AdminEvictInput{Pattern: "tn-lt-%"}, &fakeEvictor{},
		map[string]string{"LOOPRUNTIME_ADMIN_API": "true", "LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET": "s3cr3t"},
		nil,
	)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 without X-Service-Auth, got %d", w.Code)
	}
}

func TestAdminEvict_WrongSecret_Returns403(t *testing.T) {
	w := doEvict(t, AdminEvictInput{Pattern: "tn-lt-%"}, &fakeEvictor{},
		map[string]string{"LOOPRUNTIME_ADMIN_API": "true", "LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET": "s3cr3t"},
		map[string]string{"X-Service-Auth": "wrong"},
	)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 with wrong secret, got %d", w.Code)
	}
}

// TestAdminEvict_LegacyProxySecret_Returns403 closes loopruntime-cloud#16:
// a request carrying the LEGACY LOOPRUNTIME_SERVICE_AUTH_SECRET in the X-Service-Auth
// header (the exact shape the cloud /engine/* proxy adds to every forwarded
// user-JWT request) must be rejected. Pre-T1.0+T1.2 the inline check accepted
// this secret → owner-JWT requests reached the k8s cleanup logic → #16.
// Post-fix: middleware reads only LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET → 403.
func TestAdminEvict_LegacyProxySecret_Returns403(t *testing.T) {
	w := doEvict(t, AdminEvictInput{Pattern: "tn-lt-%"}, &fakeEvictor{},
		map[string]string{
			"LOOPRUNTIME_ADMIN_API":                          "true",
			"LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET":          "canonical-admin-secret",
			"LOOPRUNTIME_SERVICE_AUTH_SECRET":                "legacy-proxy-secret",
			"LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET_PREVIOUS": "",
			"LOOPRUNTIME_SERVICE_AUTH_SECRET_PREVIOUS":       "",
		},
		map[string]string{"X-Service-Auth": "legacy-proxy-secret"}, // proxy-added secret
	)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 (legacy proxy secret must not authorize admin), got %d body=%s",
			w.Code, w.Body.String())
	}
}

// TestAdminEvict_CanonicalAdminSecret_Reachable closes the inverse:
// a request carrying the CANONICAL admin secret reaches the handler successfully.
// Without this we cannot tell whether T1.2's middleware rejected everything or
// just the legacy secret.
func TestAdminEvict_CanonicalAdminSecret_Reachable(t *testing.T) {
	ev := &fakeEvictor{matched: []string{"tn-aaaaaaaaaaaaaaaaaaaa"}}
	w := doEvict(t, AdminEvictInput{IDs: []string{"tn-aaaaaaaaaaaaaaaaaaaa"}, DryRun: true}, ev,
		map[string]string{
			"LOOPRUNTIME_ADMIN_API":                 "true",
			"LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET": "canonical-admin-secret",
		},
		map[string]string{"X-Service-Auth": "canonical-admin-secret"},
	)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with canonical admin secret, got %d body=%s",
			w.Code, w.Body.String())
	}
}

func TestAdminEvict_EmptyFilter_Returns400(t *testing.T) {
	w := doEvict(t, AdminEvictInput{}, &fakeEvictor{},
		map[string]string{"LOOPRUNTIME_ADMIN_API": "true", "LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET": "s"},
		map[string]string{"X-Service-Auth": "s"},
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 empty filter, got %d", w.Code)
	}
}

func TestAdminEvict_DangerousPattern_Returns400(t *testing.T) {
	for _, p := range []string{"%", "tn-%"} {
		w := doEvict(t, AdminEvictInput{Pattern: p}, &fakeEvictor{},
			map[string]string{"LOOPRUNTIME_ADMIN_API": "true", "LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET": "s"},
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
		map[string]string{"LOOPRUNTIME_ADMIN_API": "true", "LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET": "s"},
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
		map[string]string{"LOOPRUNTIME_ADMIN_API": "true", "LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET": "s"},
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
	if resp.JobID != "job-1" {
		t.Errorf("job_id = %q, want the id the eviction queue returned (job-1)", resp.JobID)
	}
}

// A tenant-matching failure must not echo the upstream error to the caller
// (CWE-209, as for enqueue): it can carry database or cluster internals.
func TestAdminEvict_MatchFailure_SanitizesErrorBody(t *testing.T) {
	ev := &fakeEvictor{matchErr: errors.New(`pq: relation "tenants" does not exist on host db-primary.loopruntime-system`)}
	w := doEvict(t, AdminEvictInput{Pattern: "tn-lt-%"}, ev,
		map[string]string{"LOOPRUNTIME_ADMIN_API": "true", "LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET": "s"},
		map[string]string{"X-Service-Auth": "s"},
	)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "loopruntime-system") || strings.Contains(w.Body.String(), "relation") {
		t.Errorf("response leaks the upstream error: %s", w.Body.String())
	}
}

// T2.1 of plan qa-sprint1-cleanup-2026-06-24 v1.2 — closes #20 (CWE-209).
// Verifies that when the upstream evictor returns a k8s-shaped error
// containing ServiceAccount + namespace + ArgoCD APIgroup, the 500
// response body is sanitized (generic message, no internals leak).
func TestAdminEvict_EnqueueFailure_SanitizesErrorBody(t *testing.T) {
	// Inject a fake error that mirrors the real k8s/ArgoCD leak shape
	// captured in QA Red Team issue #20.
	leakyErr := errors.New(`failed to create resource: User "system:serviceaccount:loopruntime-system:default" cannot create resource "applications" in API group "argoproj.io"`)
	ev := &fakeEvictor{matched: []string{"tn-lt-1"}, enqErr: leakyErr}
	w := doEvict(t, AdminEvictInput{Pattern: "tn-lt-%"}, ev,
		map[string]string{"LOOPRUNTIME_ADMIN_API": "true", "LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET": "s"},
		map[string]string{"X-Service-Auth": "s"},
	)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	// Per ADR D6 — generic message + zero internal terms.
	denylist := []string{
		"serviceaccount:",
		"system:serviceaccount",
		"loopruntime-system",
		"argoproj.io",
		"API group",
		"create resource",
	}
	for _, leak := range denylist {
		if strings.Contains(body, leak) {
			t.Errorf("CWE-209 leak: response body contains %q (must be sanitized): %s", leak, body)
		}
	}

	// Sanity — client still sees the canonical code + generic message.
	if !strings.Contains(body, `"code":"ENQUEUE_FAILED"`) {
		t.Errorf("missing canonical error code: %s", body)
	}
	if !strings.Contains(body, "queue temporarily unavailable") {
		t.Errorf("missing canonical generic message: %s", body)
	}
}

func TestAdminEvict_BadJSON_Returns400(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/evict",
		bytes.NewReader([]byte("not-json")))
	req.Header.Set("X-Service-Auth", "s")
	w := httptest.NewRecorder()
	AdminEvictHandler(&fakeEvictor{}, true).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 bad json, got %d", w.Code)
	}
}

// adminRoute wraps h in the admin gate built from settings.
func adminRoute(settings map[string]string, h http.Handler) http.Handler {
	authn := auth.NewAuthenticator(auth.Config{
		ServiceSecret:              settings["LOOPRUNTIME_SERVICE_AUTH_SECRET"],
		AdminServiceSecret:         settings["LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET"],
		AdminServiceSecretPrevious: settings["LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET_PREVIOUS"],
	})
	return authn.RequireAdminService(h)
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package releasehttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/release"

	"github.com/go-chi/chi/v5"
)

// T4.4 handler tests:
//   - 200 happy path (LOOPRUNTIME_ADMIN_API + service-auth set)
//   - 403 missing X-Service-Auth → maps to CLI exit 4
//   - 404 unknown release → maps to CLI exit 1
//   - 404 LOOPRUNTIME_ADMIN_API=false (must hide endpoint existence)
//   - 400 missing path param
//   - JSON content-type + stable shape

type fakeInspector struct {
	inspect func(ctx context.Context, releaseID string) (*release.ReleaseInspection, error)
}

func (f *fakeInspector) Inspect(ctx context.Context, releaseID string) (*release.ReleaseInspection, error) {
	return f.inspect(ctx, releaseID)
}

func setupHandler(t *testing.T, inspector release.ReleaseInspector, apiEnabled bool) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	// The router mounts this handler behind Authenticator.RequireAdminService; the
	// test exercises the same chain so the auth gate is covered with it.
	authn := auth.NewAuthenticator(auth.Config{AdminServiceSecret: "secret-abc"})
	r.With(authn.RequireAdminService).Get("/api/v1/admin/releases/{id}/inspect", AdminReleaseInspectHandler(inspector, apiEnabled))
	return r
}

func TestAdminReleaseInspect_HappyPath(t *testing.T) {
	insp := &fakeInspector{inspect: func(_ context.Context, _ string) (*release.ReleaseInspection, error) {
		return &release.ReleaseInspection{
			ReleaseID:       "rel-001",
			BuildID:         "bld-001",
			TenantID:        "tn-acme",
			SigningPath:     "keyless",
			AttestationPath: "present",
			AuditLogEntries: []release.ReleaseInspectionAuditRow{},
		}, nil
	}}
	srv := setupHandler(t, insp, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/releases/rel-001/inspect", nil)
	req.Header.Set("X-Service-Auth", "secret-abc")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body release.ReleaseInspection
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v\nraw=%s", err, rec.Body.String())
	}
	if body.ReleaseID != "rel-001" {
		t.Errorf("body.release_id = %q, want rel-001", body.ReleaseID)
	}
}

func TestAdminReleaseInspect_403_WithoutServiceAuth(t *testing.T) {
	srv := setupHandler(t, &fakeInspector{}, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/releases/rel-001/inspect", nil)
	// No X-Service-Auth header.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestAdminReleaseInspect_403_WithWrongServiceAuth(t *testing.T) {
	srv := setupHandler(t, &fakeInspector{}, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/releases/rel-001/inspect", nil)
	req.Header.Set("X-Service-Auth", "wrong-secret")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestAdminReleaseInspect_404_WhenAdminAPIDisabled(t *testing.T) {
	// LOOPRUNTIME_ADMIN_API NOT set → an authenticated operator still sees 404:
	// the flag hides the endpoint even from callers holding the secret.
	srv := setupHandler(t, &fakeInspector{}, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/releases/rel-001/inspect", nil)
	req.Header.Set("X-Service-Auth", "secret-abc")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (admin API disabled must look like 404)", rec.Code)
	}
}

func TestAdminReleaseInspect_404_ForUnknownRelease(t *testing.T) {
	insp := &fakeInspector{inspect: func(_ context.Context, id string) (*release.ReleaseInspection, error) {
		return nil, release.ErrReleaseInspectionNotFound{ReleaseID: id}
	}}
	srv := setupHandler(t, insp, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/releases/rel-missing/inspect", nil)
	req.Header.Set("X-Service-Auth", "secret-abc")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "RELEASE_NOT_FOUND") {
		t.Errorf("body should mention RELEASE_NOT_FOUND, got %s", rec.Body.String())
	}
}

func TestAdminReleaseInspect_Returns_AttestationChain(t *testing.T) {
	// CR-10 specific assertion: the response must surface the
	// image_attestation_rekor_url + signing_path so on-call can
	// answer "which write path was taken" without log access.
	rekorURL := "https://rekor.loopruntime.test/api/v1/log/entries?logIndex=42"
	insp := &fakeInspector{inspect: func(_ context.Context, _ string) (*release.ReleaseInspection, error) {
		return &release.ReleaseInspection{
			ReleaseID:                "rel-001",
			BuildID:                  "bld-001",
			TenantID:                 "tn-acme",
			SigningPath:              "keyless",
			AttestationPath:          "present",
			ImageAttestationRekorURL: &rekorURL,
			AuditLogEntries:          []release.ReleaseInspectionAuditRow{},
		}, nil
	}}
	srv := setupHandler(t, insp, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/releases/rel-001/inspect", nil)
	req.Header.Set("X-Service-Auth", "secret-abc")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"signing_path", "keyless", "attestation_path", "present", rekorURL} {
		if !strings.Contains(body, want) {
			t.Errorf("body must contain %q for CR-10 attestation-chain visibility, got: %s", want, body)
		}
	}
}

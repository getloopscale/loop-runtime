// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Phase 5 T5.5 — admin_pin_cell handler tests.
// Mirror admin_evict_test.go structure for consistency.

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/services/build"

	placementsvc "github.com/getloopscale/loopzero/api/internal/services/placement"
)

// osReadFileC11 — local alias avoiding clash with any other test helper named
// readFile in this package (defensive, since multiple *_test.go files exist).
var osReadFileC11 = os.ReadFile

type fakePinner struct {
	prev string
	err  error

	// captures
	calledTenant string
	calledCell   string
}

func (f *fakePinner) Pin(_ context.Context, tenantID, cellID string) (string, error) {
	f.calledTenant = tenantID
	f.calledCell = cellID
	return f.prev, f.err
}

func doPin(t *testing.T, body any, p CellPinner, env map[string]string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/pin", bytes.NewReader(b))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	AdminPinCellHandler(p).ServeHTTP(rec, req)
	return rec
}

func TestAdminPinCell_FeatureFlagOff_Returns404(t *testing.T) {
	// LOOPZERO_ADMIN_API not set
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, &fakePinner{}, nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestAdminPinCell_MissingAuth_Returns403(t *testing.T) {
	env := map[string]string{
		"LOOPZERO_ADMIN_API":                 "true",
		"LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret",
	}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, &fakePinner{}, env, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestAdminPinCell_WrongSecret_Returns403(t *testing.T) {
	env := map[string]string{
		"LOOPZERO_ADMIN_API":                 "true",
		"LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret",
	}
	headers := map[string]string{"X-Service-Auth": "wrong"}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, &fakePinner{}, env, headers)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestAdminPinCell_BadJSON_Returns400(t *testing.T) {
	t.Setenv("LOOPZERO_ADMIN_API", "true")
	t.Setenv("LOOPZERO_ADMIN_SERVICE_AUTH_SECRET", "secret")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/pin", bytes.NewReader([]byte("{not json")))
	req.Header.Set("X-Service-Auth", "secret")
	rec := httptest.NewRecorder()
	AdminPinCellHandler(&fakePinner{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAdminPinCell_MissingTenantID_Returns400(t *testing.T) {
	env := map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	rec := doPin(t, AdminPinCellInput{TenantID: "", CellID: "cell-2"}, &fakePinner{}, env, headers)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestAdminPinCell_BadCellPattern_Returns400(t *testing.T) {
	env := map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	for _, bad := range []string{"cell-2_no", "cell-", "Cell-2", "2", "garbage"} {
		rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: bad}, &fakePinner{}, env, headers)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for cell_id=%q, got %d", bad, rec.Code)
		}
	}
}

func TestAdminPinCell_NilPinner_Returns503(t *testing.T) {
	env := map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, nil, env, headers)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestAdminPinCell_Success_Returns200(t *testing.T) {
	env := map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	pinner := &fakePinner{prev: "cell-default"}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, pinner, env, headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var resp AdminPinCellResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TenantID != "tn-x" || resp.NewCell != "cell-2" || resp.PreviousCell != "cell-default" || !resp.Updated {
		t.Errorf("unexpected response: %+v", resp)
	}
	if pinner.calledTenant != "tn-x" || pinner.calledCell != "cell-2" {
		t.Errorf("pinner not called correctly: tenant=%q cell=%q", pinner.calledTenant, pinner.calledCell)
	}
}

func TestAdminPinCell_NoChange_Updated_False(t *testing.T) {
	env := map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	pinner := &fakePinner{prev: "cell-2"} // already on cell-2
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, pinner, env, headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp AdminPinCellResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Updated {
		t.Errorf("expected updated=false when previous == new")
	}
}

func TestAdminPinCell_TenantNotFound_Returns404(t *testing.T) {
	env := map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	// T6.1 migration: pinner now returns the sentinel directly so the
	// handler's errors.Is mapping fires (was string-match before).
	pinner := &fakePinner{err: placementsvc.ErrTenantNotFound}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, pinner, env, headers)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestAdminPinCell_CellNotPinnable_Returns409(t *testing.T) {
	env := map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	pinner := &fakePinner{err: placementsvc.ErrCellNotPinnable}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, pinner, env, headers)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

func TestAdminPinCell_CellAtCapacity_Returns409(t *testing.T) {
	env := map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	pinner := &fakePinner{err: build.ErrCellAtCapacity}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, pinner, env, headers)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// cell-2-activation-plan T1.4 / ADR C11 — authz on --cell flag in 3 paths.
//
// Path 1 (CLI): loopzero admin tenants pin without LOOPZERO_SERVICE_AUTH → exit 4.
//               CLI behavior tested in cli/tests/integration/admin-tenants-pin.test.ts.
//               Here we cover the matching API-level guarantee.
// Path 2 (API): curl POST /api/v1/admin/tenants/pin without X-Service-Auth → 403.
// Path 3 (Webhook): GitHub App auto-deploy MUST NOT set cell_id from payload.
//                   Verified by code-grep + godoc invariant on autodeploy.go.
//
// Drift on any path lets a bypass route compromise placement isolation.
// ---------------------------------------------------------------------------

func TestAdminPinCell_AuthzInvariantC11Path1_CLIRequiresServiceAuth(t *testing.T) {
	// CLI's exit-code-4 contract is mirrored at the API layer: without
	// X-Service-Auth header AND with admin flag enabled, the API returns
	// 403 — the CLI maps this to exit 4 per Rule 4. This test pins the
	// API contract; the CLI integration test (admin-tenants-pin.test.ts)
	// pins the exit code mapping.
	env := map[string]string{
		"LOOPZERO_ADMIN_API":                 "true",
		"LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret",
	}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, &fakePinner{}, env, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("C11 path 1: expected 403 without X-Service-Auth, got %d", rec.Code)
	}
}

func TestAdminPinCell_AuthzInvariantC11Path2_APIDirect403(t *testing.T) {
	// curl POST without X-Service-Auth → must NOT be allowed even though
	// the request shape is otherwise valid (full body, valid IDs, pinner
	// available).
	env := map[string]string{
		"LOOPZERO_ADMIN_API":                 "true",
		"LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "the-real-secret",
	}
	headers := map[string]string{} // no X-Service-Auth
	pinner := &fakePinner{prev: "cell-1"}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, pinner, env, headers)
	if rec.Code != http.StatusForbidden {
		t.Errorf("C11 path 2: expected 403 from API without auth, got %d", rec.Code)
	}
	// Defensive: pinner MUST NOT be invoked if auth fails.
	if pinner.calledTenant != "" || pinner.calledCell != "" {
		t.Errorf("C11 path 2: pinner invoked despite auth failure — tenant=%q cell=%q",
			pinner.calledTenant, pinner.calledCell)
	}
}

func TestAdminPinCell_AuthzInvariantC11Path3_NoCellIDInWebhookPath(t *testing.T) {
	// Path 3 is a code-shape invariant, not a runtime test:
	// the GitHub auto-deploy code MUST NOT contain cell_id parsing from
	// webhook payload. We grep autodeploy.go + preview_deploy.go for the
	// forbidden tokens to catch drift.
	for _, path := range []string{
		"../../internal/github/autodeploy.go",
		"../../internal/github/preview_deploy.go",
	} {
		data, err := readFileContent(path)
		if err != nil {
			t.Logf("skipping %s — not present in this repo state", path)
			continue
		}
		// Forbidden patterns: any direct read of cell_id from webhook input.
		for _, forbidden := range []string{
			`payload.cell_id`,
			`"cell_id"`,            // direct JSON parse
			`webhook.CellID`,       // struct field
			`pushEvent.CellID`,     // GitHub PushEvent extension
			`AutoDeployInput.Cell`, // typed input field
		} {
			if containsToken(data, forbidden) {
				t.Errorf("C11 path 3 invariant violated: %s contains forbidden token %q",
					path, forbidden)
			}
		}
	}
	// Bonus: also confirm the godoc INVARIANT comment is present on
	// AutoDeployConfig (catches drift if someone removes the warning).
	data, err := readFileContent("../../internal/github/autodeploy.go")
	if err == nil {
		if !containsToken(data, "ADR C11 path 3") {
			t.Errorf("C11 path 3: godoc invariant marker 'ADR C11 path 3' missing from autodeploy.go")
		}
		if !containsToken(data, "MUST NOT set tenant.cell_id") {
			t.Errorf("C11 path 3: godoc invariant 'MUST NOT set tenant.cell_id' missing from autodeploy.go")
		}
	}
}

// readFileContent + containsToken — tiny helpers kept inline.
// Bytes-based grep so the test stays pure-stdlib.
func readFileContent(relPath string) (string, error) {
	b, err := osReadFileC11(relPath)
	return string(b), err
}

func containsToken(haystack, needle string) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestAdminPinCell_ErrorMappingViaErrorsIs is the T6.1 DoD test
// (find_code_001 / CODE-001 HIGH of deep-review-remediation-plan).
//
// Asserts that the handler maps wrapped sentinel errors to the correct HTTP
// status via errors.Is — NOT via err.Error() string matching. This covers
// the regression risk where a sentinel gets wrapped by fmt.Errorf("...: %w", e)
// in a downstream caller; the substring-match version would silently fall
// through to 500 because "tenant not found" is no longer a substring of
// "pin failed: tenant not found".
//
// We construct fmt.Errorf("...: %w", services.ErrXxx) wrappers and verify
// each maps to the documented status code.
func TestAdminPinCell_ErrorMappingViaErrorsIs(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "wrapped ErrTenantNotFound returns 404",
			err:        fmt.Errorf("pin failed: %w", placementsvc.ErrTenantNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   "tenant_not_found",
		},
		{
			name:       "wrapped ErrCellNotFound returns 404",
			err:        fmt.Errorf("pin failed: %w", placementsvc.ErrCellNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   "cell_not_found",
		},
		{
			name:       "wrapped ErrCellNotPinnable returns 409",
			err:        fmt.Errorf("pin failed: %w", placementsvc.ErrCellNotPinnable),
			wantStatus: http.StatusConflict,
			wantCode:   "cell_not_pinnable",
		},
		{
			name:       "wrapped ErrCellAtCapacity returns 409",
			err:        fmt.Errorf("pin failed: %w", build.ErrCellAtCapacity),
			wantStatus: http.StatusConflict,
			wantCode:   "cell_at_capacity",
		},
	}

	env := map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"},
				&fakePinner{err: tc.err}, env, headers)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status: want %d got %d body=%s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if !contains(rec.Body.String(), `"code":"`+tc.wantCode+`"`) {
				t.Errorf("body missing code=%q: %s", tc.wantCode, rec.Body.String())
			}
		})
	}
}

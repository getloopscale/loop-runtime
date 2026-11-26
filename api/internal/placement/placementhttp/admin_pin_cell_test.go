// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Phase 5 T5.5 — admin_pin_cell handler tests.
// Mirror admin_evict_test.go structure for consistency.

package placementhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/placement"
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

func doPin(t *testing.T, body any, p CellPinner, settings map[string]string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/pin", bytes.NewReader(b))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	// The router mounts this handler behind Authenticator.RequireAdminService; the
	// test exercises the same chain so the auth gate is covered with it.
	adminRoute(settings, AdminPinCellHandler(p, nil, settings["LOOP_RUNTIME_ADMIN_API"] == "true")).ServeHTTP(rec, req)
	return rec
}

func TestAdminPinCell_FeatureFlagOff_Returns404(t *testing.T) {
	// LOOP_RUNTIME_ADMIN_API not set: even an authenticated operator sees 404.
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"}, &fakePinner{},
		map[string]string{"LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "s"},
		map[string]string{"X-Service-Auth": "s"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestAdminPinCell_MissingAuth_Returns403(t *testing.T) {
	env := map[string]string{
		"LOOP_RUNTIME_ADMIN_API":                 "true",
		"LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "secret",
	}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"}, &fakePinner{}, env, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestAdminPinCell_WrongSecret_Returns403(t *testing.T) {
	env := map[string]string{
		"LOOP_RUNTIME_ADMIN_API":                 "true",
		"LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "secret",
	}
	headers := map[string]string{"X-Service-Auth": "wrong"}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"}, &fakePinner{}, env, headers)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestAdminPinCell_BadJSON_Returns400(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/pin", bytes.NewReader([]byte("{not json")))
	req.Header.Set("X-Service-Auth", "secret")
	rec := httptest.NewRecorder()
	AdminPinCellHandler(&fakePinner{}, nil, true).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestAdminPinCell_Success_WritesAuditEntry (vap-pinpath-exemption T4 / security
// review Condition 2): a successful cell MOVE writes exactly one TENANT_CELL_PINNED
// audit entry with the safe fixed actor and from/to metadata. Proves the pin path
// is now forensically traceable (was NOT before — the gap the review surfaced).
func TestAdminPinCell_Success_WritesAuditEntry(t *testing.T) {

	var calls int
	var got struct {
		tenantID, action, actor, resourceType, resourceID string
		metadata                                          map[string]any
	}
	audit := func(tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) {
		calls++
		got.tenantID, got.action, got.actor = tenantID, action, actor
		got.resourceType, got.resourceID, got.metadata = resourceType, resourceID, metadata
	}

	b, _ := json.Marshal(AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/pin", bytes.NewReader(b))
	req.Header.Set("X-Service-Auth", "secret")
	rec := httptest.NewRecorder()
	AdminPinCellHandler(&fakePinner{prev: "cell-0"}, audit, true).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 audit write on a move, got %d", calls)
	}
	if got.action != "TENANT_CELL_PINNED" {
		t.Errorf("action: want TENANT_CELL_PINNED, got %q", got.action)
	}
	if got.actor != "admin:service-auth" {
		t.Errorf("actor: want admin:service-auth (safe fixed string, no secret), got %q", got.actor)
	}
	if got.resourceType != "tenant" || got.resourceID != "tn-x" {
		t.Errorf("resource: want tenant/tn-x, got %s/%s", got.resourceType, got.resourceID)
	}
	if got.metadata["from"] != "cell-0" || got.metadata["to"] != "cell-1" {
		t.Errorf("metadata: want from=cell-0 to=cell-1, got %+v", got.metadata)
	}
}

// TestAdminPinCell_NoOpPin_WritesNoAudit: re-pinning to the same cell is not a
// move — it must NOT emit an audit entry (keeps the audit log meaningful).
func TestAdminPinCell_NoOpPin_WritesNoAudit(t *testing.T) {

	var calls int
	audit := func(_, _, _, _, _, _ string, _ map[string]any) { calls++ }

	b, _ := json.Marshal(AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/pin", bytes.NewReader(b))
	req.Header.Set("X-Service-Auth", "secret")
	rec := httptest.NewRecorder()
	AdminPinCellHandler(&fakePinner{prev: "cell-1"}, audit, true).ServeHTTP(rec, req) // already on cell-1

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if calls != 0 {
		t.Errorf("expected 0 audit writes on a no-op re-pin, got %d", calls)
	}
}

func TestAdminPinCell_MissingTenantID_Returns400(t *testing.T) {
	env := map[string]string{"LOOP_RUNTIME_ADMIN_API": "true", "LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	rec := doPin(t, AdminPinCellInput{TenantID: "", CellID: "cell-1"}, &fakePinner{}, env, headers)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestAdminPinCell_BadCellPattern_Returns400(t *testing.T) {
	env := map[string]string{"LOOP_RUNTIME_ADMIN_API": "true", "LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	for _, bad := range []string{"cell-2_no", "cell-", "Cell-2", "2", "garbage"} {
		rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: bad}, &fakePinner{}, env, headers)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for cell_id=%q, got %d", bad, rec.Code)
		}
	}
}

func TestAdminPinCell_NilPinner_Returns503(t *testing.T) {
	env := map[string]string{"LOOP_RUNTIME_ADMIN_API": "true", "LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"}, nil, env, headers)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestAdminPinCell_Success_Returns200(t *testing.T) {
	env := map[string]string{"LOOP_RUNTIME_ADMIN_API": "true", "LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	pinner := &fakePinner{prev: "cell-0"}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"}, pinner, env, headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var resp AdminPinCellResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TenantID != "tn-x" || resp.NewCell != "cell-1" || resp.PreviousCell != "cell-0" || !resp.Updated {
		t.Errorf("unexpected response: %+v", resp)
	}
	if pinner.calledTenant != "tn-x" || pinner.calledCell != "cell-1" {
		t.Errorf("pinner not called correctly: tenant=%q cell=%q", pinner.calledTenant, pinner.calledCell)
	}
}

func TestAdminPinCell_NoChange_Updated_False(t *testing.T) {
	env := map[string]string{"LOOP_RUNTIME_ADMIN_API": "true", "LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	pinner := &fakePinner{prev: "cell-1"} // already on cell-1
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"}, pinner, env, headers)
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
	env := map[string]string{"LOOP_RUNTIME_ADMIN_API": "true", "LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	// T6.1 migration: pinner now returns the sentinel directly so the
	// handler's errors.Is mapping fires (was string-match before).
	pinner := &fakePinner{err: placement.ErrTenantNotFound}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"}, pinner, env, headers)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestAdminPinCell_CellNotPinnable_Returns409(t *testing.T) {
	env := map[string]string{"LOOP_RUNTIME_ADMIN_API": "true", "LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	pinner := &fakePinner{err: placement.ErrCellNotPinnable}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"}, pinner, env, headers)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

// The pinner refuses a full cell with placement.ErrCellAtTenantCapacity (the
// store's sentinel). The handler used to match build.ErrCellAtCapacity, an
// error the pin path never returns, so a full cell answered 500.
func TestAdminPinCell_CellAtCapacity_Returns409(t *testing.T) {
	env := map[string]string{"LOOP_RUNTIME_ADMIN_API": "true", "LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	pinner := &fakePinner{err: placement.ErrCellAtTenantCapacity}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"}, pinner, env, headers)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// cell-1-activation-plan T1.4 / ADR C11 — authz on --cell flag in 3 paths.
//
// Path 1 (CLI): loop-runtime admin tenants pin without LOOP_RUNTIME_SERVICE_AUTH → exit 4.
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
		"LOOP_RUNTIME_ADMIN_API":                 "true",
		"LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "secret",
	}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"}, &fakePinner{}, env, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("C11 path 1: expected 403 without X-Service-Auth, got %d", rec.Code)
	}
}

func TestAdminPinCell_AuthzInvariantC11Path2_APIDirect403(t *testing.T) {
	// curl POST without X-Service-Auth → must NOT be allowed even though
	// the request shape is otherwise valid (full body, valid IDs, pinner
	// available).
	env := map[string]string{
		"LOOP_RUNTIME_ADMIN_API":                 "true",
		"LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "the-real-secret",
	}
	headers := map[string]string{} // no X-Service-Auth
	pinner := &fakePinner{prev: "cell-0"}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"}, pinner, env, headers)
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
			err:        fmt.Errorf("pin failed: %w", placement.ErrTenantNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   "tenant_not_found",
		},
		{
			name:       "wrapped ErrCellNotFound returns 404",
			err:        fmt.Errorf("pin failed: %w", placement.ErrCellNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   "cell_not_found",
		},
		{
			name:       "wrapped ErrCellNotPinnable returns 409",
			err:        fmt.Errorf("pin failed: %w", placement.ErrCellNotPinnable),
			wantStatus: http.StatusConflict,
			wantCode:   "cell_not_pinnable",
		},
		{
			name:       "wrapped ErrCellAtCapacity returns 409",
			err:        fmt.Errorf("pin failed: %w", placement.ErrCellAtTenantCapacity),
			wantStatus: http.StatusConflict,
			wantCode:   "cell_at_capacity",
		},
	}

	env := map[string]string{"LOOP_RUNTIME_ADMIN_API": "true", "LOOP_RUNTIME_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-1"},
				&fakePinner{err: tc.err}, env, headers)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status: want %d got %d body=%s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"`+tc.wantCode+`"`) {
				t.Errorf("body missing code=%q: %s", tc.wantCode, rec.Body.String())
			}
		})
	}
}

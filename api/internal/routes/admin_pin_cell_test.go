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
	"net/http"
	"net/http/httptest"
	"testing"
)

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
		"LOOPZERO_ADMIN_API":                "true",
		"LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret",
	}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, &fakePinner{}, env, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestAdminPinCell_WrongSecret_Returns403(t *testing.T) {
	env := map[string]string{
		"LOOPZERO_ADMIN_API":                "true",
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
	pinner := &fakePinner{err: errString("tenant not found: tn-x")}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, pinner, env, headers)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestAdminPinCell_CellNotPinnable_Returns409(t *testing.T) {
	env := map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	pinner := &fakePinner{err: errString("cell not pinnable: status=draining")}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, pinner, env, headers)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

func TestAdminPinCell_CellAtCapacity_Returns409(t *testing.T) {
	env := map[string]string{"LOOPZERO_ADMIN_API": "true", "LOOPZERO_ADMIN_SERVICE_AUTH_SECRET": "secret"}
	headers := map[string]string{"X-Service-Auth": "secret"}
	pinner := &fakePinner{err: errString("cell at capacity: 500/500")}
	rec := doPin(t, AdminPinCellInput{TenantID: "tn-x", CellID: "cell-2"}, pinner, env, headers)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

// errString is a tiny helper because we don't want to depend on a
// project-internal errors package for these tests.
type errString string

func (e errString) Error() string { return string(e) }

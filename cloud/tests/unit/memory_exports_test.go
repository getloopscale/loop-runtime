// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Plan 3 of dashboard-memory-section-roadmap.md, T3.3.
// Tests for the 3 memory export handlers (Create / Status / Download).
// Focused on D1 (EC-16): server-generated UUID filename — client body
// `filename` is IGNORED. Path traversal attempts on the {id} URL param
// rejected with 400 BEFORE the DB is touched.

package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/routes"
	"github.com/loopzero/loopzero/cloud/internal/themory"
)

func newExportsHandler(t *testing.T) (*routes.MemoryExportsHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fake := &fakeTheMemory{
		listResp: &themory.ListResult{Memories: []themory.MemoryRow{
			{ID: "m1", Content: "fact"},
		}},
	}
	// Worker delay 0 → completes synchronously in tests (no goroutine wait).
	// Export dir is a t.TempDir so files clean up automatically.
	return routes.NewMemoryExportsHandler(db, fake, nil, t.TempDir(), 0), mock
}

func TestMemoryExports_CreateRejectsUnauthenticated(t *testing.T) {
	h, _ := newExportsHandler(t)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest("POST", "/v1/memory/exports", strings.NewReader(`{}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// T3.3 — D1 / EC-16 absorption: client-supplied `filename` is IGNORED.
// The persisted id is server-generated; the on-disk filename derives from
// the id, never from the request body. A `filename: "../../etc/passwd"`
// attempt MUST not appear in any persisted state.
func TestMemoryExports_CreateIgnoresClientFilename(t *testing.T) {
	h, mock := newExportsHandler(t)
	mock.ExpectExec("INSERT INTO memory_exports").
		WithArgs(
			sqlmock.AnyArg(), // id — MUST start with "export-" + uuid form, NOT the client filename
			"user-A", "tenant-A",
			sqlmock.AnyArg(), // display_name (label only)
			"pending",
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body := `{"filename":"../../etc/passwd","label":"june-backup"}`
	req := httptest.NewRequest("POST", "/v1/memory/exports", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// D1 invariant: id is the server-generated form, NEVER contains `..` / `/`
	if !strings.HasPrefix(resp.ID, "export-") {
		t.Errorf("id must start with 'export-', got %q", resp.ID)
	}
	if strings.ContainsAny(resp.ID, "/.\\") {
		t.Errorf("id contains traversal chars — D1 violation: %q", resp.ID)
	}
	if resp.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", resp.Status)
	}
}

// T3.3 — Download path: malformed {id} rejected with 400 BEFORE the DB
// is touched. Prevents path-traversal probes from polluting the download
// flow / slow logs.
func TestMemoryExports_DownloadMalformedIDRejectedBeforeDB(t *testing.T) {
	cases := []string{
		"../etc/passwd",
		"'; DROP",
		"not-a-uuid",
		"",
		"export-../etc",
	}
	for _, malformed := range cases {
		t.Run(malformed, func(t *testing.T) {
			h, mock := newExportsHandler(t)
			req := httptest.NewRequest("GET", "/v1/memory/exports/x/download", nil)
			req = req.WithContext(auth.WithUser(req.Context(), &auth.UserContext{Subject: "u", TenantID: "t"}))
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", malformed)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			rec := httptest.NewRecorder()
			h.Download(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 INVALID_ID, got %d", rec.Code)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("DB called before validation: %v", err)
			}
		})
	}
}

func TestMemoryExports_StatusReturnsRowForOwnExport(t *testing.T) {
	h, mock := newExportsHandler(t)
	mock.ExpectQuery("SELECT id, status, error_message, file_path FROM memory_exports").
		WithArgs("export-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "user-A", "tenant-A").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "error_message", "file_path"}).
			AddRow("export-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "succeeded", nil, "/tmp/x.json"))

	req := httptest.NewRequest("GET", "/v1/memory/exports/x/status", nil)
	req = req.WithContext(auth.WithUser(req.Context(), &auth.UserContext{Subject: "user-A", TenantID: "tenant-A"}))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "export-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Status(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"succeeded"`) {
		t.Errorf("expected succeeded status, got %s", rec.Body.String())
	}
}

// Cross-tenant Status → 404 (D5 inheritance). The WHERE clause includes
// (subject, tenant_id), so a wrong tenant produces 0 rows.
func TestMemoryExports_CrossTenantStatusReturns404(t *testing.T) {
	h, mock := newExportsHandler(t)
	mock.ExpectQuery("SELECT id, status, error_message, file_path FROM memory_exports").
		WithArgs("export-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "user-B", "tenant-B").
		WillReturnError(sqlNoRowsErr())

	req := httptest.NewRequest("GET", "/v1/memory/exports/x/status", nil)
	req = req.WithContext(auth.WithUser(req.Context(), &auth.UserContext{Subject: "user-B", TenantID: "tenant-B"}))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "export-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Status(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatal("CRITICAL: cross-tenant returned 403 — D5 violation")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// sqlNoRowsErr returns the sentinel sqlmock surfaces when a QueryRow Scan
// finds zero rows. Mirrors sqlmockErrNoRows() from the scope_middleware_test.go.
func sqlNoRowsErr() error { return sqlmockErrNoRows() }

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package audithttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// --- Fake AuditStore ---

type fakeAuditStore struct {
	entries []AuditEntryResult
	total   int
	err     error
}

func (f *fakeAuditStore) ListAuditEntries(_ context.Context, _ string, _ AuditFilter) ([]AuditEntryResult, int, error) {
	return f.entries, f.total, f.err
}

// --- Tests ---

func TestAudit_List_HappyPath(t *testing.T) {
	store := &fakeAuditStore{
		entries: []AuditEntryResult{
			{
				Action:       "project.created",
				ResourceType: "project",
				ResourceID:   "p1",
				Actor:        "test@example.com",
				CreatedAt:    time.Now(),
			},
			{
				Action:       "build.submitted",
				ResourceType: "build",
				ResourceID:   "bld-1",
				Actor:        "test@example.com",
				CreatedAt:    time.Now(),
			},
		},
		total: 2,
	}
	h := NewAuditHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext()) // owner role
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	entries, ok := resp["entries"].([]interface{})
	if !ok {
		t.Fatal("want entries array in response")
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if resp["total"].(float64) != 2 {
		t.Fatalf("want total=2, got %v", resp["total"])
	}
}

func TestAudit_List_Empty(t *testing.T) {
	store := &fakeAuditStore{
		entries: []AuditEntryResult{},
		total:   0,
	}
	h := NewAuditHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["total"].(float64) != 0 {
		t.Fatalf("want total=0, got %v", resp["total"])
	}
}

func TestAudit_List_Unauthenticated(t *testing.T) {
	h := NewAuditHandler(&fakeAuditStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAudit_List_WithFilters(t *testing.T) {
	store := &fakeAuditStore{
		entries: []AuditEntryResult{
			{Action: "project.created", ResourceType: "project", ResourceID: "p1", Actor: "test@example.com", CreatedAt: time.Now()},
		},
		total: 1,
	}
	h := NewAuditHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?action=project.created&resource_type=project&limit=10&offset=5", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["limit"].(float64) != 10 {
		t.Fatalf("want limit=10, got %v", resp["limit"])
	}
	if resp["offset"].(float64) != 5 {
		t.Fatalf("want offset=5, got %v", resp["offset"])
	}
}

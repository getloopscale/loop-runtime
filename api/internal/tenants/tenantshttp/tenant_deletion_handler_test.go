// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenantshttp_test

import (
	"context"
	"encoding/json"
	"github.com/getloopscale/loop-runtime/api/internal/tenants/tenantshttp"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
)

// fakeTenantDeletionStore is a test double for TenantDeletionStore.
type fakeTenantDeletionStore struct {
	records    map[string]*tenantshttp.DeletionRecord
	tombstones map[string]*tenantshttp.TombstoneRecord
}

func newFakeDeletionStore() *fakeTenantDeletionStore {
	return &fakeTenantDeletionStore{
		records:    make(map[string]*tenantshttp.DeletionRecord),
		tombstones: make(map[string]*tenantshttp.TombstoneRecord),
	}
}

func (s *fakeTenantDeletionStore) RequestDeletion(_ context.Context, input tenantshttp.DeletionRequest) (*tenantshttp.DeletionRecord, error) {
	if existing, ok := s.records[input.TenantID]; ok {
		return existing, nil // idempotent
	}
	record := &tenantshttp.DeletionRecord{
		TenantID:    input.TenantID,
		RequestedAt: time.Now(),
		RequestedBy: input.RequestedBy,
		Reason:      input.Reason,
		GraceUntil:  input.GraceUntil,
		Force:       input.Force,
	}
	s.records[input.TenantID] = record
	return record, nil
}

func (s *fakeTenantDeletionStore) CancelDeletion(_ context.Context, tenantID string) error {
	delete(s.records, tenantID)
	return nil
}

func (s *fakeTenantDeletionStore) GetPendingDeletion(_ context.Context, tenantID string) (*tenantshttp.DeletionRecord, error) {
	return s.records[tenantID], nil
}

func (s *fakeTenantDeletionStore) WriteTombstone(_ context.Context, tombstone tenantshttp.TombstoneRecord) error {
	s.tombstones[tombstone.TenantID] = &tombstone
	delete(s.records, tombstone.TenantID)
	return nil
}

func setupTenantRouter(store tenantshttp.TenantDeletionStore) *chi.Mux {
	handler := tenantshttp.NewTenantHandler(store, slog.Default(), func(string) bool { return false })
	r := chi.NewRouter()
	r.Delete("/api/v1/tenants/{id}", handler.Delete)
	r.Post("/api/v1/tenants/{id}/restore", handler.Restore)
	return r
}

func withUserContext(r *http.Request, uc *auth.UserContext) *http.Request {
	ctx := context.WithValue(r.Context(), auth.UserContextKeyPublic(), uc)
	return r.WithContext(ctx)
}

func TestTenantDelete_SoftDelete_Returns202(t *testing.T) {
	store := newFakeDeletionStore()
	router := setupTenantRouter(store)

	req := httptest.NewRequest("DELETE", "/api/v1/tenants/tn-test123", nil)
	req = withUserContext(req, &auth.UserContext{
		UserID:   "user-1",
		Email:    "test@example.com",
		TenantID: "tn-test123",
		Role:     "owner",
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp tenantshttp.TenantDeletionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.TenantID != "tn-test123" {
		t.Errorf("expected tenant_id=tn-test123, got %s", resp.TenantID)
	}
	if resp.Status != "pending_deletion" {
		t.Errorf("expected status=pending_deletion, got %s", resp.Status)
	}
	if resp.GraceUntil == nil {
		t.Error("expected grace_until to be set for self-service delete")
	}
}

func TestTenantDelete_Idempotent(t *testing.T) {
	store := newFakeDeletionStore()
	router := setupTenantRouter(store)

	uc := &auth.UserContext{
		UserID: "user-1", Email: "test@example.com",
		TenantID: "tn-test123", Role: "owner",
	}

	// First call
	req1 := httptest.NewRequest("DELETE", "/api/v1/tenants/tn-test123", nil)
	req1 = withUserContext(req1, uc)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)

	// Second call — should return same record
	req2 := httptest.NewRequest("DELETE", "/api/v1/tenants/tn-test123", nil)
	req2 = withUserContext(req2, uc)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusAccepted {
		t.Fatalf("second call should also return 202, got %d", rec2.Code)
	}
}

func TestTenantDelete_IDOR_Blocked(t *testing.T) {
	store := newFakeDeletionStore()
	router := setupTenantRouter(store)

	// User belongs to tn-alice but tries to delete tn-bob
	req := httptest.NewRequest("DELETE", "/api/v1/tenants/tn-bob", nil)
	req = withUserContext(req, &auth.UserContext{
		UserID: "user-1", Email: "alice@example.com",
		TenantID: "tn-alice", Role: "owner",
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("IDOR should be blocked with 403, got %d", rec.Code)
	}
}

func TestTenantDelete_NonOwner_Blocked(t *testing.T) {
	store := newFakeDeletionStore()
	router := setupTenantRouter(store)

	req := httptest.NewRequest("DELETE", "/api/v1/tenants/tn-test123", nil)
	req = withUserContext(req, &auth.UserContext{
		UserID: "user-1", Email: "member@example.com",
		TenantID: "tn-test123", Role: "member",
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner should be blocked with 403, got %d", rec.Code)
	}
}

func TestTenantDelete_ForceRequiresAdmin(t *testing.T) {
	store := newFakeDeletionStore()
	router := setupTenantRouter(store)

	req := httptest.NewRequest("DELETE", "/api/v1/tenants/tn-test123?force=true", nil)
	req = withUserContext(req, &auth.UserContext{
		UserID: "user-1", Email: "owner@example.com",
		TenantID: "tn-test123", Role: "owner",
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("force without admin key should be 403, got %d", rec.Code)
	}
}

func TestTenantRestore_CancelsPendingDeletion(t *testing.T) {
	store := newFakeDeletionStore()
	graceUntil := time.Now().Add(24 * time.Hour)
	store.records["tn-test123"] = &tenantshttp.DeletionRecord{
		TenantID:    "tn-test123",
		RequestedAt: time.Now(),
		RequestedBy: "test@example.com",
		Reason:      "user_request",
		GraceUntil:  &graceUntil,
	}

	router := setupTenantRouter(store)

	req := httptest.NewRequest("POST", "/api/v1/tenants/tn-test123/restore", nil)
	req = withUserContext(req, &auth.UserContext{
		UserID: "user-1", Email: "test@example.com",
		TenantID: "tn-test123", Role: "owner",
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("restore should return 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, exists := store.records["tn-test123"]; exists {
		t.Error("pending deletion should be removed after restore")
	}
}

func TestTenantRestore_ExpiredGrace_Returns410(t *testing.T) {
	store := newFakeDeletionStore()
	expired := time.Now().Add(-1 * time.Hour) // expired 1h ago
	store.records["tn-test123"] = &tenantshttp.DeletionRecord{
		TenantID:    "tn-test123",
		RequestedAt: time.Now(),
		RequestedBy: "test@example.com",
		Reason:      "user_request",
		GraceUntil:  &expired,
	}

	router := setupTenantRouter(store)

	req := httptest.NewRequest("POST", "/api/v1/tenants/tn-test123/restore", nil)
	req = withUserContext(req, &auth.UserContext{
		UserID: "user-1", Email: "test@example.com",
		TenantID: "tn-test123", Role: "owner",
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("expired grace should return 410, got %d", rec.Code)
	}
}

func TestTenantRestore_NoPending_Returns404(t *testing.T) {
	store := newFakeDeletionStore()
	router := setupTenantRouter(store)

	req := httptest.NewRequest("POST", "/api/v1/tenants/tn-test123/restore", nil)
	req = withUserContext(req, &auth.UserContext{
		UserID: "user-1", Email: "test@example.com",
		TenantID: "tn-test123", Role: "owner",
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("no pending deletion should return 404, got %d", rec.Code)
	}
}

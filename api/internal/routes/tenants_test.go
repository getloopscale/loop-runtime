// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"log/slog"
)

// --- Fake TenantDeletionStore ---

type fakeTenantDeletionStore struct {
	record    *DeletionRecord
	createErr error
	cancelErr error
	getErr    error
}

func (f *fakeTenantDeletionStore) RequestDeletion(_ context.Context, input DeletionRequest) (*DeletionRecord, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &DeletionRecord{
		TenantID:    input.TenantID,
		RequestedAt: time.Now(),
		RequestedBy: input.RequestedBy,
		Reason:      input.Reason,
		GraceUntil:  input.GraceUntil,
		Force:       input.Force,
	}, nil
}

func (f *fakeTenantDeletionStore) CancelDeletion(_ context.Context, _ string) error {
	return f.cancelErr
}

func (f *fakeTenantDeletionStore) GetPendingDeletion(_ context.Context, _ string) (*DeletionRecord, error) {
	return f.record, f.getErr
}

func (f *fakeTenantDeletionStore) WriteTombstone(_ context.Context, _ TombstoneRecord) error {
	return nil
}

// --- Tests ---

func TestTenants_Delete_HappyPath(t *testing.T) {
	store := &fakeTenantDeletionStore{}
	h := NewTenantHandler(store, slog.Default())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tn-test-123", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "tn-test-123")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}

	var resp TenantDeletionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "pending_deletion" {
		t.Fatalf("want status pending_deletion, got %s", resp.Status)
	}
	if resp.GraceUntil == nil {
		t.Fatal("want grace_until to be set for non-force deletion")
	}
}

func TestTenants_Delete_Unauthenticated(t *testing.T) {
	h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tn-test-123", nil)
	req = withChiParam(req, "id", "tn-test-123")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTenants_Delete_CrossTenantDenied(t *testing.T) {
	h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tn-other-tenant", nil)
	req = withAuth(req, defaultUserContext()) // tn-test-123
	req = withChiParam(req, "id", "tn-other-tenant")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTenants_Delete_MemberForbidden(t *testing.T) {
	h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tn-test-123", nil)
	req = withAuth(req, memberUserContext()) // member, not owner
	req = withChiParam(req, "id", "tn-test-123")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTenants_Delete_MissingID(t *testing.T) {
	h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTenants_Delete_ForceRequiresAdmin(t *testing.T) {
	h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tn-test-123?force=true", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "tn-test-123")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 for force without admin, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTenants_Restore_HappyPath(t *testing.T) {
	grace := time.Now().Add(24 * time.Hour)
	store := &fakeTenantDeletionStore{
		record: &DeletionRecord{
			TenantID:   "tn-test-123",
			GraceUntil: &grace,
		},
	}
	h := NewTenantHandler(store, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tn-test-123/restore", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "tn-test-123")
	w := httptest.NewRecorder()

	h.Restore(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTenants_Restore_NoPendingDeletion(t *testing.T) {
	store := &fakeTenantDeletionStore{record: nil}
	h := NewTenantHandler(store, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tn-test-123/restore", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "tn-test-123")
	w := httptest.NewRecorder()

	h.Restore(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTenants_Restore_GraceExpired(t *testing.T) {
	expired := time.Now().Add(-1 * time.Hour) // already expired
	store := &fakeTenantDeletionStore{
		record: &DeletionRecord{
			TenantID:   "tn-test-123",
			GraceUntil: &expired,
		},
	}
	h := NewTenantHandler(store, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tn-test-123/restore", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "tn-test-123")
	w := httptest.NewRecorder()

	h.Restore(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("want 410, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTenants_Restore_Unauthenticated(t *testing.T) {
	h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tn-test-123/restore", nil)
	req = withChiParam(req, "id", "tn-test-123")
	w := httptest.NewRecorder()

	h.Restore(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTenants_Restore_CrossTenantDenied(t *testing.T) {
	h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tn-other-tenant/restore", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "tn-other-tenant")
	w := httptest.NewRecorder()

	h.Restore(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

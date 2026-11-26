// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenantshttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"

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
	h := NewTenantHandler(store, slog.Default(), noAdminKey)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tn-test-123", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "tn-test-123")
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

// TestTenants_Delete_AuthorizationMatrix consolidates 4 near-clone tests
// for TenantHandler.Delete authorization paths. Phase 3 helper: each case
// previously was a 13-line test function with the same Setup-Act-Assert
// scaffold differing only in (tenantID, auth context, expected code).
func TestTenants_Delete_AuthorizationMatrix(t *testing.T) {
	cases := []struct {
		name      string
		tenantID  string
		setupAuth func(req *http.Request) *http.Request // nil for unauthenticated
		wantCode  int
	}{
		{
			name: "Unauthenticated", tenantID: "tn-test-123",
			setupAuth: nil,
			wantCode:  http.StatusUnauthorized,
		},
		{
			name: "CrossTenantDenied", tenantID: "tn-other-tenant",
			setupAuth: func(req *http.Request) *http.Request {
				return routestest.WithAuth(req, routestest.DefaultUserContext())
			},
			wantCode: http.StatusForbidden,
		},
		{
			name: "MemberForbidden", tenantID: "tn-test-123",
			setupAuth: func(req *http.Request) *http.Request { return routestest.WithAuth(req, routestest.MemberUserContext()) },
			wantCode:  http.StatusForbidden,
		},
		{
			name: "MissingID", tenantID: "",
			setupAuth: func(req *http.Request) *http.Request {
				return routestest.WithAuth(req, routestest.DefaultUserContext())
			},
			wantCode: http.StatusBadRequest,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default(), noAdminKey)
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/"+c.tenantID, nil)
			if c.setupAuth != nil {
				req = c.setupAuth(req)
			}
			req = routestest.WithChiParam(req, "id", c.tenantID)
			w := httptest.NewRecorder()
			h.Delete(w, req)
			if w.Code != c.wantCode {
				t.Fatalf("want %d, got %d body=%s", c.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestTenants_Delete_ForceRequiresAdmin(t *testing.T) {
	h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default(), noAdminKey)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tn-test-123?force=true", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "tn-test-123")
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
	h := NewTenantHandler(store, slog.Default(), noAdminKey)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tn-test-123/restore", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "tn-test-123")
	w := httptest.NewRecorder()

	h.Restore(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTenants_Restore_NoPendingDeletion(t *testing.T) {
	store := &fakeTenantDeletionStore{record: nil}
	h := NewTenantHandler(store, slog.Default(), noAdminKey)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tn-test-123/restore", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "tn-test-123")
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
	h := NewTenantHandler(store, slog.Default(), noAdminKey)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tn-test-123/restore", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "tn-test-123")
	w := httptest.NewRecorder()

	h.Restore(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("want 410, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTenants_Restore_Unauthenticated(t *testing.T) {
	h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default(), noAdminKey)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tn-test-123/restore", nil)
	req = routestest.WithChiParam(req, "id", "tn-test-123")
	w := httptest.NewRecorder()

	h.Restore(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTenants_Restore_CrossTenantDenied(t *testing.T) {
	h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default(), noAdminKey)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tn-other-tenant/restore", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "tn-other-tenant")
	w := httptest.NewRecorder()

	h.Restore(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// T0.2 of deep-review-remediation-plan (closes find_sec_001 CRITICAL).
// Attacker scenario: authenticated member of tenantA sends DELETE for tenantB
// with a garbage X-Admin-Key header. Pre-fix: handler treated header presence
// as admin grant → cross-tenant DELETE succeeded. Post-fix: the admin-key check
// compares the key value against the configured key in constant time.
func TestTenants_Delete_RejectsUnvalidatedAdminKey(t *testing.T) {
	store := &fakeTenantDeletionStore{}
	h := NewTenantHandler(store, slog.Default(), noAdminKey)

	// No admin key is configured, so the admin-key check MUST refuse any
	// input, including the attacker's garbage value.
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tn-victim-tenant", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext()) // authenticated as tn-test-123 (different tenant)
	req = routestest.WithChiParam(req, "id", "tn-victim-tenant")
	req.Header.Set("X-Admin-Key", "garbage-attacker-payload")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("attacker scenario: garbage X-Admin-Key MUST be rejected (403), got %d body=%s", w.Code, w.Body.String())
	}
}

// T0.2 of deep-review-remediation-plan — same attacker scenario for Restore.
func TestTenants_Restore_RejectsUnvalidatedAdminKey(t *testing.T) {
	store := &fakeTenantDeletionStore{
		record: &DeletionRecord{TenantID: "tn-victim-tenant", RequestedAt: time.Now()},
	}
	h := NewTenantHandler(store, slog.Default(), noAdminKey)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tn-victim-tenant/restore", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "tn-victim-tenant")
	req.Header.Set("X-Admin-Key", "garbage-attacker-payload")
	w := httptest.NewRecorder()

	h.Restore(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("attacker scenario: garbage X-Admin-Key MUST be rejected (403), got %d body=%s", w.Code, w.Body.String())
	}
}

// noAdminKey is the admin-key check of an Authenticator with no key configured.
var noAdminKey = auth.NewAuthenticator(auth.Config{}).ValidateAdminAPIKey

// The configured platform-admin key does grant the cross-tenant path — the
// check refuses garbage, not the operator.
func TestTenants_Delete_AcceptsTheConfiguredAdminKeyAcrossTenants(t *testing.T) {
	adminKey := auth.NewAuthenticator(auth.Config{AdminAPIKey: "the-admin-key"}).ValidateAdminAPIKey
	h := NewTenantHandler(&fakeTenantDeletionStore{}, slog.Default(), adminKey)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tn-other-tenant", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "tn-other-tenant")
	req.Header.Set("X-Admin-Key", "the-admin-key")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code == http.StatusForbidden {
		t.Fatalf("the configured admin key was refused for a cross-tenant delete: %s", w.Body.String())
	}
}

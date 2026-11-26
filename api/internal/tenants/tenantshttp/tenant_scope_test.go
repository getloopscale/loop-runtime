// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenantshttp

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// scopeRecordingDeletionStore records the database scope each call ran under.
type scopeRecordingDeletionStore struct {
	fakeTenantDeletionStore
	scopes []string
}

func (s *scopeRecordingDeletionStore) record(ctx context.Context) {
	scope, _ := dbtx.TenantScope(ctx)
	s.scopes = append(s.scopes, scope)
}

func (s *scopeRecordingDeletionStore) RequestDeletion(ctx context.Context, in DeletionRequest) (*DeletionRecord, error) {
	s.record(ctx)
	return s.fakeTenantDeletionStore.RequestDeletion(ctx, in)
}

func (s *scopeRecordingDeletionStore) GetPendingDeletion(ctx context.Context, id string) (*DeletionRecord, error) {
	s.record(ctx)
	grace := time.Now().Add(time.Hour)
	return &DeletionRecord{TenantID: id, GraceUntil: &grace}, nil
}

func (s *scopeRecordingDeletionStore) CancelDeletion(ctx context.Context, id string) error {
	s.record(ctx)
	return nil
}

// An operator holding the admin key acts on a tenant other than the one it
// authenticated for. The deletion store runs on the request pool, whose tenant
// tables are fail-closed under Row Level Security and scoped to the caller —
// so the admin branch must act in the TARGET tenant's scope, or the pending
// deletion it writes is refused and the one it restores is never found.
func adminDeletionRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "tn-target")
	req.Header.Set("X-Admin-Key", "k")
	return req.WithContext(dbtx.WithTenantScope(req.Context(), routestest.DefaultUserContext().TenantID))
}

func TestAnAdminDeletesAnotherTenantInThatTenantsScope(t *testing.T) {
	store := &scopeRecordingDeletionStore{}
	h := NewTenantHandler(store, slog.Default(), func(k string) bool { return k == "k" })
	w := httptest.NewRecorder()

	h.Delete(w, adminDeletionRequest(http.MethodDelete, "/api/v1/tenants/tn-target"))

	if w.Code != http.StatusAccepted {
		t.Fatalf("admin delete = %d, want 202 (body %s)", w.Code, w.Body.String())
	}
	if len(store.scopes) != 1 || store.scopes[0] != "tn-target" {
		t.Fatalf("deletion store scopes = %q; want [tn-target]", store.scopes)
	}
}

func TestAnAdminRestoresAnotherTenantInThatTenantsScope(t *testing.T) {
	store := &scopeRecordingDeletionStore{}
	h := NewTenantHandler(store, slog.Default(), func(k string) bool { return k == "k" })
	w := httptest.NewRecorder()

	h.Restore(w, adminDeletionRequest(http.MethodPost, "/api/v1/tenants/tn-target/restore"))

	if w.Code != http.StatusOK {
		t.Fatalf("admin restore = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if len(store.scopes) != 2 || store.scopes[0] != "tn-target" || store.scopes[1] != "tn-target" {
		t.Fatalf("deletion store scopes = %q; want tn-target for the lookup and the cancel", store.scopes)
	}
}

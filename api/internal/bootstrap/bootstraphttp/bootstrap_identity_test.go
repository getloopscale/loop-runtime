// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package bootstraphttp_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/bootstrap/bootstraphttp"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// recordingStore answers every Bootstrap with err and keeps the request it saw.
type recordingStore struct {
	got *ports.BootstrapRequest
	err error
}

func (s *recordingStore) Bootstrap(_ context.Context, req ports.BootstrapRequest) (string, string, bool, error) {
	s.got = &req
	if s.err != nil {
		return "", "", false, s.err
	}
	return req.AdminUserID, "tn-" + req.TenantSlug, true, nil
}

func postBootstrapBody(t *testing.T, store ports.BootstrapStore, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := bootstraphttp.NewBootstrapHandler(store, "", &fakeCellPlacement{cellID: "cell-0"})
	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.Bootstrap(rec, req)
	return rec
}

func TestBootstrapAcceptsAnExplicitIdentityWithoutAGitHubID(t *testing.T) {
	store := &recordingStore{}

	rec := postBootstrapBody(t, store,
		`{"admin_user_id":"kratos-uuid","admin_login":"admin","admin_email":"admin@example.test","tenant_slug":"admin"}`)

	if rec.Code != http.StatusOK || store.got == nil || store.got.AdminUserID != "kratos-uuid" || store.got.AdminEmail != "admin@example.test" {
		t.Fatalf("bootstrap = %d %s, store saw %+v", rec.Code, rec.Body.String(), store.got)
	}
}

func TestBootstrapRequiresAnIdentity(t *testing.T) {
	store := &recordingStore{}

	rec := postBootstrapBody(t, store, `{"admin_login":"admin","tenant_slug":"admin"}`)

	if rec.Code != http.StatusBadRequest || store.got != nil || !strings.Contains(rec.Body.String(), "admin_user_id or admin_github_id") {
		t.Fatalf("bootstrap without an identity = %d %s", rec.Code, rec.Body.String())
	}
}

func TestBootstrapAnswersConflictWhenTheSlugBelongsToAnotherIdentity(t *testing.T) {
	store := &recordingStore{err: fmt.Errorf("bootstrap: tenant slug %q: %w", "admin", ports.ErrTenantSlugTaken)}

	rec := postBootstrapBody(t, store, `{"admin_user_id":"kratos-uuid","admin_login":"admin","tenant_slug":"admin"}`)

	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "TENANT_SLUG_TAKEN") {
		t.Fatalf("bootstrap onto a taken slug = %d %s", rec.Code, rec.Body.String())
	}
}

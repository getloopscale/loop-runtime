// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// --- fakes ---

type fakeRepoLister struct {
	calls atomic.Int64
	reply []InstallationRepository
	err   error
}

func (f *fakeRepoLister) ListInstallationRepositories(_ context.Context, _ int64, _ int) ([]InstallationRepository, error) {
	f.calls.Add(1)
	return f.reply, f.err
}

type fakeInstallationStore struct {
	calls   atomic.Int64
	record  *InstallationRecord
	err     error
	missing bool // when true, return (nil, nil) regardless of record
}

func (f *fakeInstallationStore) GetInstallation(_ context.Context, _ int64) (*InstallationRecord, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	if f.missing {
		return nil, nil
	}
	return f.record, nil
}

func tenantPtr(s string) *string { return &s }

func defaultRepoReply() []InstallationRepository {
	return []InstallationRepository{
		{FullName: "owner/api", DefaultBranch: "main", Private: false, Language: "Go"},
		{FullName: "owner/web", DefaultBranch: "develop", Private: true, Language: "TypeScript"},
	}
}

func newListRequest(t *testing.T, installationID string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/github/installations/"+installationID+"/repositories", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", installationID)
	return req
}

// --- tests ---

func TestListRepositories_HappyPath_200(t *testing.T) {
	lister := &fakeRepoLister{reply: defaultRepoReply()}
	store := &fakeInstallationStore{record: &InstallationRecord{ID: 12345, TenantID: tenantPtr("tn-test-123")}}
	h := NewIntegrationsGitHubHandler(lister, store)

	w := httptest.NewRecorder()
	h.ListRepositories(w, newListRequest(t, "12345"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Repositories []InstallationRepository `json:"repositories"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Repositories) != 2 {
		t.Fatalf("len = %d, want 2", len(resp.Repositories))
	}
	if resp.Repositories[0].FullName != "owner/api" {
		t.Fatalf("first repo = %q, want owner/api", resp.Repositories[0].FullName)
	}
}

func TestListRepositories_Unauthenticated_401(t *testing.T) {
	lister := &fakeRepoLister{reply: defaultRepoReply()}
	store := &fakeInstallationStore{}
	h := NewIntegrationsGitHubHandler(lister, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/github/installations/1/repositories", nil)
	req = routestest.WithChiParam(req, "id", "1")
	w := httptest.NewRecorder()
	h.ListRepositories(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestListRepositories_InvalidID_400(t *testing.T) {
	lister := &fakeRepoLister{}
	store := &fakeInstallationStore{}
	h := NewIntegrationsGitHubHandler(lister, store)

	w := httptest.NewRecorder()
	h.ListRepositories(w, newListRequest(t, "not-a-number"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestListRepositories_Forbidden_DifferentTenant(t *testing.T) {
	lister := &fakeRepoLister{reply: defaultRepoReply()}
	other := tenantPtr("tn-other-456")
	store := &fakeInstallationStore{record: &InstallationRecord{ID: 12345, TenantID: other}}
	h := NewIntegrationsGitHubHandler(lister, store)

	w := httptest.NewRecorder()
	h.ListRepositories(w, newListRequest(t, "12345"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (cross-tenant)", w.Code)
	}
	if lister.calls.Load() != 0 {
		t.Fatalf("lister called %d times; must skip upstream when tenant fails", lister.calls.Load())
	}
}

func TestListRepositories_UnclaimedInstallation_AllowsAccess(t *testing.T) {
	// Fresh-from-callback: installation row not yet linked to any tenant
	// (TenantID == nil). Any authenticated user may still list repos
	// from it -- the next POST /connect will write the tenant binding.
	lister := &fakeRepoLister{reply: defaultRepoReply()}
	store := &fakeInstallationStore{record: &InstallationRecord{ID: 12345, TenantID: nil}}
	h := NewIntegrationsGitHubHandler(lister, store)

	w := httptest.NewRecorder()
	h.ListRepositories(w, newListRequest(t, "12345"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (unclaimed installation)", w.Code)
	}
}

func TestListRepositories_InstallationMissing_AllowsAccess(t *testing.T) {
	// Cleanroom path: the install callback hasn't persisted the row yet
	// but the dashboard is already polling. We let the GitHub upstream
	// be the ultimate arbiter of access (it returns 404 if revoked).
	lister := &fakeRepoLister{reply: defaultRepoReply()}
	store := &fakeInstallationStore{missing: true}
	h := NewIntegrationsGitHubHandler(lister, store)

	w := httptest.NewRecorder()
	h.ListRepositories(w, newListRequest(t, "12345"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (missing installation row)", w.Code)
	}
}

func TestListRepositories_CacheHit_NoUpstreamCall(t *testing.T) {
	lister := &fakeRepoLister{reply: defaultRepoReply()}
	store := &fakeInstallationStore{record: &InstallationRecord{ID: 12345, TenantID: tenantPtr("tn-test-123")}}
	h := NewIntegrationsGitHubHandler(lister, store)

	// First call populates the cache.
	w1 := httptest.NewRecorder()
	h.ListRepositories(w1, newListRequest(t, "12345"))
	if w1.Code != http.StatusOK {
		t.Fatalf("first call status = %d, want 200", w1.Code)
	}
	if lister.calls.Load() != 1 {
		t.Fatalf("first call: lister called %d times, want 1", lister.calls.Load())
	}

	// Second call within the TTL must NOT hit upstream.
	w2 := httptest.NewRecorder()
	h.ListRepositories(w2, newListRequest(t, "12345"))
	if w2.Code != http.StatusOK {
		t.Fatalf("second call status = %d", w2.Code)
	}
	if lister.calls.Load() != 1 {
		t.Fatalf("second call: lister called %d times, want still 1 (cache hit)", lister.calls.Load())
	}
}

func TestListRepositories_CacheExpiry_RefetchesUpstream(t *testing.T) {
	lister := &fakeRepoLister{reply: defaultRepoReply()}
	store := &fakeInstallationStore{record: &InstallationRecord{ID: 12345, TenantID: tenantPtr("tn-test-123")}}
	h := NewIntegrationsGitHubHandler(lister, store)

	var nowAtomic atomic.Int64
	nowAtomic.Store(time.Unix(1_700_000_000, 0).UnixNano())
	h.withClock(func() time.Time { return time.Unix(0, nowAtomic.Load()) })

	h.ListRepositories(httptest.NewRecorder(), newListRequest(t, "12345"))
	if lister.calls.Load() != 1 {
		t.Fatalf("after first call: %d, want 1", lister.calls.Load())
	}

	nowAtomic.Add(int64(repoListCacheTTL + time.Second))
	h.ListRepositories(httptest.NewRecorder(), newListRequest(t, "12345"))
	if lister.calls.Load() != 2 {
		t.Fatalf("after expiry: %d, want 2 (fresh mint)", lister.calls.Load())
	}
}

func TestListRepositories_InstallationRevoked_410(t *testing.T) {
	// Adapter wraps the underlying github.InstallationRevokedError into
	// ErrInstallationRevoked via fmt.Errorf("...: %w", ErrInstallationRevoked).
	lister := &fakeRepoLister{err: fmt.Errorf("installation 12345 revoked: %w", ErrInstallationRevoked)}
	store := &fakeInstallationStore{record: &InstallationRecord{ID: 12345, TenantID: tenantPtr("tn-test-123")}}
	h := NewIntegrationsGitHubHandler(lister, store)

	w := httptest.NewRecorder()
	h.ListRepositories(w, newListRequest(t, "12345"))
	if w.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410 (revoked)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INSTALLATION_REVOKED") {
		t.Fatalf("body = %s, want INSTALLATION_REVOKED code", w.Body.String())
	}
}

func TestListRepositories_UpstreamError_502(t *testing.T) {
	lister := &fakeRepoLister{err: context.DeadlineExceeded}
	store := &fakeInstallationStore{record: &InstallationRecord{ID: 12345, TenantID: tenantPtr("tn-test-123")}}
	h := NewIntegrationsGitHubHandler(lister, store)

	w := httptest.NewRecorder()
	h.ListRepositories(w, newListRequest(t, "12345"))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (upstream unavailable)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "GITHUB_UNAVAILABLE") {
		t.Fatalf("body = %s, want GITHUB_UNAVAILABLE", w.Body.String())
	}
}

func TestListRepositories_Sanitizes_OnlyAllowedFields(t *testing.T) {
	// The handler only marshals InstallationRepository which has
	// {full_name, default_branch, private, language}. Confirm nothing
	// else leaks even when upstream provides richer fields.
	lister := &fakeRepoLister{reply: defaultRepoReply()}
	store := &fakeInstallationStore{record: &InstallationRecord{ID: 12345, TenantID: tenantPtr("tn-test-123")}}
	h := NewIntegrationsGitHubHandler(lister, store)

	w := httptest.NewRecorder()
	h.ListRepositories(w, newListRequest(t, "12345"))

	var raw map[string][]map[string]any
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, r := range raw["repositories"] {
		for key := range r {
			switch key {
			case "full_name", "default_branch", "private", "language":
				// allowed
			default:
				t.Fatalf("unexpected field %q in response (leaks beyond sanitised contract)", key)
			}
		}
	}
}

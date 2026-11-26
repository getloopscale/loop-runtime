// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integrationshttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// The caller of every request in this file: routestest.DefaultUserContext.
const callerTenant = "tn-test-123"

const forbiddenBody = `{"code":"FORBIDDEN","message":"You do not have access to this installation."}`

// A tenant lists the repositories of an installation only when GitHub
// confirmed that one of its users can access it. Every other state of the
// binding is refused with the same answer, before GitHub is asked.
func TestListRepositories_RefusesAnInstallationNotVerifiedForTheCaller(t *testing.T) {
	cases := map[string]*fakeInstallationStore{
		"no row":                           {missing: true},
		"row the webhook created":          {record: &InstallationRecord{ID: 12345}},
		"bound to the caller, unverified":  {record: &InstallationRecord{ID: 12345, TenantID: tenantPtr(callerTenant)}},
		"verified for another tenant":      {record: &InstallationRecord{ID: 12345, TenantID: tenantPtr("tn-other-456"), Verified: true}},
		"bound to another, unverified":     {record: &InstallationRecord{ID: 12345, TenantID: tenantPtr("tn-other-456")}},
		"verified flag without any tenant": {record: &InstallationRecord{ID: 12345, Verified: true}},
	}
	for name, store := range cases {
		t.Run(name, func(t *testing.T) {
			lister := &fakeRepoLister{reply: defaultRepoReply()}
			h := NewIntegrationsGitHubHandler(lister, store)

			w := httptest.NewRecorder()
			h.ListRepositories(w, newListRequest(t, "12345"))

			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
			}
			if got := strings.TrimSpace(w.Body.String()); got != forbiddenBody {
				t.Errorf("body = %s\n want = %s", got, forbiddenBody)
			}
			if lister.calls.Load() != 0 {
				t.Errorf("GitHub was asked %d time(s) for an installation the caller may not use", lister.calls.Load())
			}
		})
	}
}

// A cached answer is not a way around the check: the cache is read after it.
func TestListRepositories_ChecksOwnershipBeforeTheCache(t *testing.T) {
	lister := &fakeRepoLister{reply: defaultRepoReply()}
	store := &fakeInstallationStore{record: &InstallationRecord{ID: 12345, TenantID: tenantPtr(callerTenant), Verified: true}}
	h := NewIntegrationsGitHubHandler(lister, store)
	h.ListRepositories(httptest.NewRecorder(), newListRequest(t, "12345"))

	store.record = &InstallationRecord{ID: 12345, TenantID: tenantPtr("tn-other-456"), Verified: true}
	w := httptest.NewRecorder()
	h.ListRepositories(w, newListRequest(t, "12345"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 once the installation is another tenant's", w.Code)
	}
}

// --- claim ---

// fakeUserAccess stands for GitHub: the installations of this App the user
// who authorized can access.
type fakeUserAccess struct {
	installations []Installation
	err           error
	codes         []string
}

func (f *fakeUserAccess) InstallationsForCode(_ context.Context, code string) ([]Installation, error) {
	f.codes = append(f.codes, code)
	if f.err != nil {
		return nil, f.err
	}
	return f.installations, nil
}

// fakeClaims is the binding table: installation id to the tenant it is bound
// to, as verified.
type fakeClaims struct {
	bound      map[int64]string
	verifiedBy map[int64]string
	accounts   map[int64]Installation
	claimErr   error
	claimCalls int
}

func newFakeClaims() *fakeClaims {
	return &fakeClaims{bound: map[int64]string{}, verifiedBy: map[int64]string{}, accounts: map[int64]Installation{}}
}

func (f *fakeClaims) ClaimInstallation(_ context.Context, inst Installation, tenantID, userID string) (bool, error) {
	f.claimCalls++
	if f.claimErr != nil {
		return false, f.claimErr
	}
	if owner, ok := f.bound[inst.ID]; ok && owner != tenantID {
		return false, nil
	}
	f.bound[inst.ID], f.verifiedBy[inst.ID], f.accounts[inst.ID] = tenantID, userID, inst
	return true, nil
}

func (f *fakeClaims) ListVerifiedInstallations(_ context.Context, tenantID string) ([]Installation, error) {
	list := []Installation{}
	for id := int64(0); id < 1000; id++ {
		if f.bound[id] == tenantID {
			list = append(list, f.accounts[id])
		}
	}
	return list, nil
}

func claimHandler(access UserInstallationSource, claims InstallationClaimStore) *IntegrationsGitHubHandler {
	return NewIntegrationsGitHubHandler(&fakeRepoLister{}, &fakeInstallationStore{}).WithInstallationClaims(access, claims)
}

func claim(h *IntegrationsGitHubHandler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/github/installations/claim", strings.NewReader(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.ClaimInstallations(w, req)
	return w
}

type installationsBody struct {
	Installations []Installation `json:"installations"`
}

func decodeInstallations(t *testing.T, w *httptest.ResponseRecorder) []Installation {
	t.Helper()
	var body installationsBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	if body.Installations == nil {
		t.Fatalf("installations is null in %s, want an array", w.Body.String())
	}
	return body.Installations
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return body["code"]
}

func TestClaim_BindsTheInstallationsGitHubReturnedForTheUser(t *testing.T) {
	access := &fakeUserAccess{installations: []Installation{
		{ID: 101, AccountLogin: "acme", AccountType: "Organization"},
		{ID: 303, AccountLogin: "alice", AccountType: "User"},
	}}
	claims := newFakeClaims()

	w := claim(claimHandler(access, claims), `{"code":"code-1"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	got := decodeInstallations(t, w)
	if len(got) != 2 || got[0] != access.installations[0] || got[1] != access.installations[1] {
		t.Errorf("installations = %+v, want the two GitHub returned", got)
	}
	if len(claims.bound) != 2 || claims.bound[101] != callerTenant || claims.bound[303] != callerTenant {
		t.Errorf("bound = %v, want only 101 and 303 bound to the caller's tenant", claims.bound)
	}
	if claims.verifiedBy[101] != routestest.DefaultUserContext().UserID {
		t.Errorf("verified_by = %q, want the caller", claims.verifiedBy[101])
	}
	if len(access.codes) != 1 || access.codes[0] != "code-1" {
		t.Errorf("codes exchanged = %v, want code-1 once", access.codes)
	}
	if !strings.Contains(w.Body.String(), `"account_login":"acme"`) || !strings.Contains(w.Body.String(), `"account_type":"Organization"`) {
		t.Errorf("body = %s, want id, account_login and account_type per installation", w.Body.String())
	}
}

func TestClaim_SkipsAnInstallationBoundToAnotherTenant(t *testing.T) {
	access := &fakeUserAccess{installations: []Installation{
		{ID: 101, AccountLogin: "acme", AccountType: "Organization"},
		{ID: 202, AccountLogin: "globex", AccountType: "Organization"},
	}}
	claims := newFakeClaims()
	claims.bound[202] = "tn-other-456"

	w := claim(claimHandler(access, claims), `{"code":"code-1"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	got := decodeInstallations(t, w)
	if len(got) != 1 || got[0].ID != 101 {
		t.Errorf("installations = %+v, want only 101", got)
	}
	if claims.bound[202] != "tn-other-456" {
		t.Errorf("installation 202 was rebound to %q", claims.bound[202])
	}
}

func TestClaim_AnswersAnEmptyListWhenTheUserHasNoInstallation(t *testing.T) {
	claims := newFakeClaims()

	w := claim(claimHandler(&fakeUserAccess{}, claims), `{"code":"code-1"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if got := decodeInstallations(t, w); len(got) != 0 {
		t.Errorf("installations = %+v, want none", got)
	}
	if len(claims.bound) != 0 {
		t.Errorf("bound = %v, want nothing bound", claims.bound)
	}
}

func TestClaim_ARefusedCodeIsA400AndBindsNothing(t *testing.T) {
	access := &fakeUserAccess{err: ErrOAuthCodeRejected}
	claims := newFakeClaims()

	w := claim(claimHandler(access, claims), `{"code":"used-code"}`)

	if w.Code != http.StatusBadRequest || errorCode(t, w) != "GITHUB_CODE_REJECTED" {
		t.Fatalf("status = %d code = %q, want 400 GITHUB_CODE_REJECTED; body = %s", w.Code, errorCode(t, w), w.Body.String())
	}
	if claims.claimCalls != 0 {
		t.Errorf("%d installation(s) claimed with a refused code", claims.claimCalls)
	}
}

func TestClaim_AGitHubOutageIsA502AndBindsNothing(t *testing.T) {
	access := &fakeUserAccess{
		installations: []Installation{{ID: 101, AccountLogin: "acme", AccountType: "Organization"}},
		err:           errors.New("github user access: list installations answered status 503"),
	}
	claims := newFakeClaims()

	w := claim(claimHandler(access, claims), `{"code":"code-1"}`)

	if w.Code != http.StatusBadGateway || errorCode(t, w) != "GITHUB_UNAVAILABLE" {
		t.Fatalf("status = %d code = %q, want 502 GITHUB_UNAVAILABLE; body = %s", w.Code, errorCode(t, w), w.Body.String())
	}
	if claims.claimCalls != 0 || len(claims.bound) != 0 {
		t.Errorf("claimed %d installation(s) during a GitHub outage", claims.claimCalls)
	}
	if strings.Contains(w.Body.String(), "503") {
		t.Errorf("the upstream error leaked into the answer: %s", w.Body.String())
	}
}

func TestClaim_RefusesARequestWithoutACode(t *testing.T) {
	for name, body := range map[string]string{
		"empty object": `{}`, "blank code": `{"code":"  "}`, "not JSON": `code=abc`,
	} {
		t.Run(name, func(t *testing.T) {
			access := &fakeUserAccess{}

			w := claim(claimHandler(access, newFakeClaims()), body)

			if w.Code != http.StatusBadRequest || errorCode(t, w) != "INVALID_REQUEST" {
				t.Fatalf("status = %d code = %q, want 400 INVALID_REQUEST", w.Code, errorCode(t, w))
			}
			if len(access.codes) != 0 {
				t.Errorf("GitHub was asked without a code")
			}
		})
	}
}

func TestClaim_Unauthenticated_401(t *testing.T) {
	h := claimHandler(&fakeUserAccess{}, newFakeClaims())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/github/installations/claim", strings.NewReader(`{"code":"c"}`))
	w := httptest.NewRecorder()

	h.ClaimInstallations(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestClaim_WithoutOAuthCredentialsIsA503(t *testing.T) {
	claims := newFakeClaims()

	w := claim(claimHandler(nil, claims), `{"code":"code-1"}`)

	if w.Code != http.StatusServiceUnavailable || errorCode(t, w) != "GITHUB_OAUTH_NOT_CONFIGURED" {
		t.Fatalf("status = %d code = %q, want 503 GITHUB_OAUTH_NOT_CONFIGURED", w.Code, errorCode(t, w))
	}
	if claims.claimCalls != 0 {
		t.Errorf("claimed without asking GitHub")
	}
}

func TestClaim_AStoreFailureIsA500(t *testing.T) {
	access := &fakeUserAccess{installations: []Installation{{ID: 101, AccountLogin: "acme", AccountType: "Organization"}}}
	claims := newFakeClaims()
	claims.claimErr = errors.New("db down")

	w := claim(claimHandler(access, claims), `{"code":"code-1"}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

// --- list ---

func TestListInstallations_ReturnsTheCallersVerifiedInstallations(t *testing.T) {
	claims := newFakeClaims()
	claims.bound[101], claims.accounts[101] = callerTenant, Installation{ID: 101, AccountLogin: "acme", AccountType: "Organization"}
	claims.bound[202], claims.accounts[202] = "tn-other-456", Installation{ID: 202, AccountLogin: "globex", AccountType: "Organization"}
	h := claimHandler(&fakeUserAccess{}, claims)
	req := routestest.WithAuth(httptest.NewRequest(http.MethodGet, "/api/v1/integrations/github/installations", nil), routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.ListInstallations(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	got := decodeInstallations(t, w)
	if len(got) != 1 || got[0].ID != 101 {
		t.Errorf("installations = %+v, want only the caller's 101", got)
	}
}

func TestListInstallations_Unauthenticated_401(t *testing.T) {
	h := claimHandler(&fakeUserAccess{}, newFakeClaims())
	w := httptest.NewRecorder()

	h.ListInstallations(w, httptest.NewRequest(http.MethodGet, "/api/v1/integrations/github/installations", nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

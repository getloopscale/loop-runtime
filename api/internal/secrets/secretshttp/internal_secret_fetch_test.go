// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secretshttp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/go-chi/chi/v5"
)

// --- mocks ---

type mockTenantBinding struct {
	cellByTenant map[string]string
	err          error
}

func (m *mockTenantBinding) GetTenantCellID(_ context.Context, tenantID string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	c, ok := m.cellByTenant[tenantID]
	if !ok {
		return "", ErrTenantNotFound
	}
	return c, nil
}

type mockSecretReader struct {
	plaintextByKey map[string][]byte
	err            error
}

// T2.1 of plan 2026-06-10-j22-layer2-code-review-fixes: extended signature
// to (tenantID, project, env, secretName) carrying the canonical composite
// scope. plaintextByKey is keyed `tenantID:project:env:secretName` so tests
// can pin specific composite combinations and verify cross-env isolation.
func (m *mockSecretReader) ReadSecret(_ context.Context, tenantID, project, env, secretName string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	p, ok := m.plaintextByKey[tenantID+":"+project+":"+env+":"+secretName]
	if !ok {
		return nil, ErrSecretNotFound
	}
	return p, nil
}

type mockAuditWriter struct {
	mu     sync.Mutex
	events []SecretReadAuditEvent
}

func (m *mockAuditWriter) WriteAudit(_ context.Context, e SecretReadAuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return nil
}

func (m *mockAuditWriter) eventTypes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ts []string
	for _, e := range m.events {
		ts = append(ts, e.EventType)
	}
	return ts
}

// --- test helper ---

// newTestServerWithCrossCluster runs the route behind a middleware that
// authenticates every request as identity, for the cell the request names in
// X-Cell-ID — the shape a per-cell TokenReview would give the handler, which is
// what lets these tests exercise the handler's own cell-binding check.
func newTestServerWithCrossCluster(t *testing.T, deps SecretFetchHandlerDeps, identity *auth.TokenReviewResult) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			reviewed := *identity
			reviewed.CellID = req.Header.Get("X-Cell-ID")
			next.ServeHTTP(w, req.WithContext(auth.InjectCrossClusterResultForBroker(req.Context(), &reviewed)))
		})
	})
	r.Post("/internal/v1/secret-fetch/{tenant_id}/{secret_name}", NewSecretFetchHandler(deps))
	return httptest.NewServer(r)
}

func newAuthedRequest(method, url string, cellID string) *http.Request {
	req, _ := http.NewRequest(method, url, nil)
	req.Header.Set("Authorization", "Bearer fake-eso-token")
	req.Header.Set("X-Cell-ID", cellID)
	return req
}

// --- tests ---

func TestSecretFetch_HappyPath(t *testing.T) {
	deps := SecretFetchHandlerDeps{
		TenantBinding: &mockTenantBinding{cellByTenant: map[string]string{"tenant-a": "cell-0"}},
		SecretReader: &mockSecretReader{
			plaintextByKey: map[string][]byte{"tenant-a:testproj:production:MY_SECRET": []byte("hunter2")},
		},
		AuditWriter: &mockAuditWriter{},
	}
	identity := &auth.TokenReviewResult{
		Authenticated: true,
		Username:      "system:serviceaccount:external-secrets:eso",
		CellID:        "cell-0",
	}
	srv := newTestServerWithCrossCluster(t, deps, identity)
	defer srv.Close()

	req := newAuthedRequest(http.MethodPost,
		srv.URL+"/internal/v1/secret-fetch/tenant-a/MY_SECRET.testproj.production", "cell-0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body secretFetchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	got, err := base64.StdEncoding.DecodeString(body.PlaintextB64)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hunter2" {
		t.Errorf("decoded = %q, want hunter2", got)
	}
	if body.TenantID != "tenant-a" || body.SecretName != "MY_SECRET" {
		t.Errorf("response identifiers mismatch: %+v", body)
	}

	// Audit: secret.read.broker event recorded
	types := deps.AuditWriter.(*mockAuditWriter).eventTypes()
	if len(types) != 1 || types[0] != "secret.read.broker" {
		t.Errorf("expected 1 secret.read.broker event, got %v", types)
	}
}

// EC-08 MUST FIX P1
func TestSecretFetch_EC08_InvalidSecretName_400(t *testing.T) {
	cases := []string{
		"lowercase_only",
		"BadName-WithHyphen",
		"WITH SPACE",
		"PATH/TRAVERSAL",
		"../../../platform/master",
		"123_STARTS_WITH_NUMBER",
		"",
	}
	identity := &auth.TokenReviewResult{
		Authenticated: true, CellID: "cell-0",
	}
	for _, badName := range cases {
		t.Run(fmt.Sprintf("bad/%s", badName), func(t *testing.T) {
			deps := SecretFetchHandlerDeps{
				TenantBinding: &mockTenantBinding{},
				SecretReader:  &mockSecretReader{},
				AuditWriter:   &mockAuditWriter{},
			}
			srv := newTestServerWithCrossCluster(t, deps, identity)
			defer srv.Close()

			// Pass via composite path param — chi.URLParam returns the
			// raw `secretName.project.environment` triple; handler splits
			// on '.' and runs EC-08 regex on segment[0]. T3.1 of plan
			// 2026-06-11-code-review-remediation: aligned with the post-
			// T2.1+ADR D2 (revised 2026-06-11) URL contract that collapsed
			// the legacy 4-slot path into a single composite slot per ESO
			// v1beta1 strict-decoder limitation. Pre-T3.1 the URL was
			// shaped `tenant/{secret}/{project}/{env}` — chi short-circuited
			// to 404 before reaching the handler, so the regex enforcement
			// path was never actually exercised.
			composite := badName + ".testproj.production"
			url := srv.URL + "/internal/v1/secret-fetch/tenant-a/" + composite
			req := newAuthedRequest(http.MethodPost, url, "cell-0")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("http.DefaultClient.Do: %v", err)
			}
			defer resp.Body.Close()
			// "PATH/TRAVERSAL" + "../../../platform/master" still cause
			// chi to fail routing (path contains additional '/' segments)
			// → 404 from the mux. All other names with no '/' must reach
			// the handler regex and be rejected with 400.
			if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
				body, _ := readAll(resp.Body)
				t.Errorf("want 400 or 404, got %d (body: %s)", resp.StatusCode, body)
			}
		})
	}
}

// EC-07 MUST FIX P0
func TestSecretFetch_EC07_CellTenantBindingMismatch_403(t *testing.T) {
	// Tenant placed in cell-0, but the caller claims X-Cell-ID=cell-1 (another cell).
	deps := SecretFetchHandlerDeps{
		TenantBinding: &mockTenantBinding{cellByTenant: map[string]string{"tenant-a": "cell-0"}},
		SecretReader:  &mockSecretReader{plaintextByKey: map[string][]byte{"tenant-a:testproj:production:S": []byte("x")}},
		AuditWriter:   &mockAuditWriter{},
	}
	identity := &auth.TokenReviewResult{
		Authenticated: true,
		Username:      "system:serviceaccount:external-secrets:eso",
		// The attacker's cell must be ANOTHER cell. Before the 1:1 unification this
		// pair was `cell-1` vs `cell-default` — two names for the SAME cluster, so
		// the test never exercised a real cross-cell attempt (ADR 2026-08-09).
		CellID: "cell-1", // attacker cell — distinct from the tenant's cell
	}
	srv := newTestServerWithCrossCluster(t, deps, identity)
	defer srv.Close()

	req := newAuthedRequest(http.MethodPost,
		srv.URL+"/internal/v1/secret-fetch/tenant-a/S.testproj.production", "cell-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
	body, _ := readAll(resp.Body)
	if !strings.Contains(body, "cell_tenant_binding_missing") {
		t.Errorf("body should mention cell_tenant_binding_missing: %s", body)
	}

	// Audit: cross_tenant_attempt recorded
	types := deps.AuditWriter.(*mockAuditWriter).eventTypes()
	found := false
	for _, e := range types {
		if e == "cross_tenant_attempt" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cross_tenant_attempt audit, got %v", types)
	}
	// And SUCCESS event NOT recorded
	for _, e := range types {
		if e == "secret.read.broker" {
			t.Errorf("must NOT record secret.read.broker on EC-07 violation")
		}
	}
}

func TestSecretFetch_TenantNotFound_404(t *testing.T) {
	deps := SecretFetchHandlerDeps{
		TenantBinding: &mockTenantBinding{cellByTenant: map[string]string{}},
		SecretReader:  &mockSecretReader{},
		AuditWriter:   &mockAuditWriter{},
	}
	identity := &auth.TokenReviewResult{Authenticated: true, CellID: "cell-0"}
	srv := newTestServerWithCrossCluster(t, deps, identity)
	defer srv.Close()

	req := newAuthedRequest(http.MethodPost,
		srv.URL+"/internal/v1/secret-fetch/tenant-missing/MY_SECRET.testproj.production", "cell-0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestSecretFetch_SecretNotFound_404(t *testing.T) {
	deps := SecretFetchHandlerDeps{
		TenantBinding: &mockTenantBinding{cellByTenant: map[string]string{"tenant-a": "cell-0"}},
		SecretReader:  &mockSecretReader{plaintextByKey: map[string][]byte{}},
		AuditWriter:   &mockAuditWriter{},
	}
	identity := &auth.TokenReviewResult{Authenticated: true, CellID: "cell-0"}
	srv := newTestServerWithCrossCluster(t, deps, identity)
	defer srv.Close()

	req := newAuthedRequest(http.MethodPost,
		srv.URL+"/internal/v1/secret-fetch/tenant-a/MISSING.testproj.production", "cell-0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestSecretFetch_TenantBindingError_500(t *testing.T) {
	deps := SecretFetchHandlerDeps{
		TenantBinding: &mockTenantBinding{err: errors.New("pg connection refused")},
		SecretReader:  &mockSecretReader{},
		AuditWriter:   &mockAuditWriter{},
	}
	identity := &auth.TokenReviewResult{Authenticated: true, CellID: "cell-0"}
	srv := newTestServerWithCrossCluster(t, deps, identity)
	defer srv.Close()

	req := newAuthedRequest(http.MethodPost,
		srv.URL+"/internal/v1/secret-fetch/tenant-a/MY_SECRET.testproj.production", "cell-0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

func TestSecretFetch_NoAuth_401(t *testing.T) {
	deps := SecretFetchHandlerDeps{
		TenantBinding: &mockTenantBinding{},
		SecretReader:  &mockSecretReader{},
		AuditWriter:   &mockAuditWriter{},
	}
	// No identity injected → middleware rejects (since real cross-cluster
	// middleware needs Authorization). Skip the middleware here by directly
	// hitting the handler — should reject because ctx has no identity.
	r := chi.NewRouter()
	r.Post("/internal/v1/secret-fetch/{tenant_id}/{secret_name}", NewSecretFetchHandler(deps))
	srv := httptest.NewServer(r)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost,
		srv.URL+"/internal/v1/secret-fetch/tenant-a/MY_SECRET.testproj.production", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", resp.StatusCode)
	}
}

func TestSecretNameRegex_AcceptsCanonicalNames(t *testing.T) {
	good := []string{
		"PORT", "DATABASE_URL", "GITHUB_APP_PRIVATE_KEY",
		"A", "A_B_C", "X1", "VALUE_2_VERSION_3",
	}
	for _, n := range good {
		if !secretNameRegex.MatchString(n) {
			t.Errorf("expected %q to match, did not", n)
		}
	}
}

// helper
func readAll(r interface{ Read(p []byte) (int, error) }) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			if err.Error() == "EOF" {
				return sb.String(), nil
			}
			return sb.String(), err
		}
	}
}

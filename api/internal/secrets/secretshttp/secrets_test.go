// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T0.1 of plan 2026-06-01-p1-security-hardening (Plano A) — regression tests
// for comp-001 (CRITICAL): SecretHandler.locker MUST be non-nil at runtime
// so secret mutations serialize under pg_advisory_xact_lock with concurrent
// deploys. The wiring was done in Sprint A T1.1 (setup_server.go:82); these
// tests lock the invariant so a future refactor cannot silently re-introduce
// the unlocked path.

package secretshttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
	loopzerovault "github.com/getloopscale/loopzero/pkg/vault"

	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/pkg/enforcement"
)

// --- Fakes ---

// lockCountingFake is a recording SecretEnvLocker that captures every
// WithLock invocation. After a request that should acquire the lock, the
// test asserts calls length == 1 + key matches expected tenant:project:env.
type lockCountingFake struct {
	mu    sync.Mutex
	calls []string
}

func (l *lockCountingFake) WithLock(_ context.Context, tenant, project, env string, fn func(tx dbtx.DBTX) error) error {
	l.mu.Lock()
	l.calls = append(l.calls, tenant+":"+project+":"+env)
	l.mu.Unlock()
	return fn(nil)
}

// fakeSecretStoreT01 stubs ports.SecretStore for routing tests.
type fakeSecretStoreT01 struct {
	setCalls    int
	deleteCalls int
}

func (f *fakeSecretStoreT01) ListNames(_ context.Context, _, _, _ string) ([]ports.SecretMetadata, error) {
	return nil, nil
}

func (f *fakeSecretStoreT01) Set(_ context.Context, _, _, _, _, _ string, _ []byte) (bool, error) {
	f.setCalls++
	return true, nil
}

func (f *fakeSecretStoreT01) Delete(_ context.Context, _, _, _, _, _ string) (bool, error) {
	f.deleteCalls++
	return true, nil
}

// --- Tests ---

// TestSecretSet_AcquiresEnvLock proves SecretHandler.Set invokes WithLock
// exactly once for the canonical (tenant, project, env) key when a locker
// is wired. Failure => comp-001 has regressed (locker is nil OR Set bypasses
// it). This is the canonical regression for the Sprint A T1.1 fix.
func TestSecretSet_AcquiresEnvLock(t *testing.T) {
	store := &fakeSecretStoreT01{}
	locker := &lockCountingFake{}
	h := NewSecretHandler(store, locker)

	body, _ := json.Marshal(map[string]string{
		"environment": "production",
		"app":         "api",
		"name":        "DB_PASS",
		"value":       "s3cret",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets?project=proj-x", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Set(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("want 200/201, got %d body=%s", w.Code, w.Body.String())
	}
	if got := len(locker.calls); got != 1 {
		t.Fatalf("expected exactly 1 WithLock invocation; got %d (calls=%v)", got, locker.calls)
	}
	want := "tn-test-123:proj-x:production"
	if locker.calls[0] != want {
		t.Fatalf("lock key mismatch: got %q want %q", locker.calls[0], want)
	}
	if store.setCalls != 1 {
		t.Fatalf("store.Set should be called exactly 1x inside lock; got %d", store.setCalls)
	}
}

// TestNewSecretHandler_RefusesANilLocker: a secret mutation outside the
// (tenant, project, env) lock races a deploy capturing config, so an unlocked
// handler is a wiring bug refused at construction — there is no unlocked path.
func TestNewSecretHandler_RefusesANilLocker(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewSecretHandler accepted a nil locker")
		}
	}()
	NewSecretHandler(&fakeSecretStoreT01{}, nil)
}

// TestSecretDelete_AcquiresEnvLock mirrors the Set test for Delete handler
// — both mutate the secret store and both MUST serialize via the env lock.
func TestSecretDelete_AcquiresEnvLock(t *testing.T) {
	store := &fakeSecretStoreT01{}
	locker := &lockCountingFake{}
	h := NewSecretHandler(store, locker)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/secrets/DB_PASS?project=proj-x&environment=production&app=api", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "DB_PASS")
	w := httptest.NewRecorder()

	h.DeleteSecret(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("want 200/204, got %d body=%s", w.Code, w.Body.String())
	}
	if got := len(locker.calls); got != 1 {
		t.Fatalf("expected exactly 1 WithLock invocation on Delete; got %d", got)
	}
	want := "tn-test-123:proj-x:production"
	if locker.calls[0] != want {
		t.Fatalf("lock key mismatch on Delete: got %q want %q", locker.calls[0], want)
	}
}

// TestSecretDelete_RejectsEmptyEnvParam — EC-1 of plan 2026-06-01-sprint-a-p1-hotfixes.
// DELETE without ?environment= must 400 BEFORE the locker is consulted.
// Rationale: with the locker wired (A1), an empty env produces lock key
// "{tenant}:{project}:" (trailing colon) — two concurrent empty-env deletes
// would contend on that bogus bucket → spurious DEPLOY_IN_PROGRESS 409s, and
// store.Delete would match L19 PK rows that include the empty-env slot. The
// guard at secrets.go:300-303 mirrors Set handler validation; this test
// proves the guard fires AND that the locker is never invoked when env is
// missing (fail-loud, fail-early).
func TestSecretDelete_RejectsEmptyEnvParam(t *testing.T) {
	store := &fakeSecretStoreT01{}
	locker := &lockCountingFake{}
	h := NewSecretHandler(store, locker)

	// NO environment query param — only project + app + name.
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/secrets/DB_PASS?project=proj-x&app=api", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "DB_PASS")
	w := httptest.NewRecorder()

	h.DeleteSecret(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 MISSING_FIELD for empty env; got %d body=%s", w.Code, w.Body.String())
	}
	if got := len(locker.calls); got != 0 {
		t.Fatalf("locker MUST NOT be invoked when env validation fails; got %d calls=%v",
			got, locker.calls)
	}
	if store.deleteCalls != 0 {
		t.Fatalf("store.Delete MUST NOT be called when env validation fails; got %d", store.deleteCalls)
	}
	// Body should contain "environment" so the operator knows what to fix.
	if !bytes.Contains(w.Body.Bytes(), []byte("environment")) {
		t.Errorf("error body should mention 'environment'; got %s", w.Body.String())
	}
}

// --- J-22 T1.1: bulk endpoint shape invariant ---
//
// TestSecretBulk_ShapeInvariant proves SecretHandler.Bulk returns exactly
// len(request.Secrets) entries in results, regardless of per-key success
// or failure. This is the canonical TDD invariant declared in plan
// 2026-06-06-dashboard-journey-j22-secrets-crud § T1.1.
//
// EC absorbed:
//   - EC-16 (input validation via existing secretNameRegex) — INVALID-KEY
//     results in status="failed" without aborting the batch
//   - EC-18 (size cap 100) — TestSecretBulk_SizeCap below
//
// EC absorptions deferred to subsequent iterations:
//   - EC-9 (per-key savepoint atomicity) — currently best-effort sequential Sets
//   - EC-10 (pg_advisory_xact_lock race-safe limit gate) — reuses existing
//     SecretEnvLocker pattern in a follow-up iteration
//   - EC-11 (ESO sync_status polling) — currently always "pending"
//   - EC-14 (duplicate-key detection) — relies on store semantics
//   - EC-15 (whitespace preservation in values) — verified via Set already
func TestSecretBulk_ShapeInvariant(t *testing.T) {
	store := &fakeSecretStoreT01{}
	h := NewSecretHandler(store, &lockCountingFake{})

	body, _ := json.Marshal(map[string]any{
		"project": "proj-x",
		"secrets": []map[string]any{
			{"name": "DATABASE_URL", "value": "postgres://...", "envs": []string{"production"}},
			{"name": "API_KEY", "value": "secret123", "envs": []string{"production", "staging"}},
			{"name": "INVALID-KEY", "value": "x", "envs": []string{"production"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Bulk(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Results []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Error      string `json:"error,omitempty"`
			SyncStatus string `json:"sync_status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}

	if got, want := len(resp.Results), 3; got != want {
		t.Fatalf("results length: got %d want %d (T1.1 invariant violated)", got, want)
	}

	if resp.Results[0].Status != "created" {
		t.Errorf("DATABASE_URL: status=%q want=created (error=%q)", resp.Results[0].Status, resp.Results[0].Error)
	}
	if resp.Results[1].Status != "created" {
		t.Errorf("API_KEY: status=%q want=created (error=%q)", resp.Results[1].Status, resp.Results[1].Error)
	}
	if resp.Results[2].Status != "failed" {
		t.Errorf("INVALID-KEY: status=%q want=failed (regex violation)", resp.Results[2].Status)
	}

	if got, want := store.setCalls, 3; got != want {
		t.Errorf("store.Set calls: got %d want %d (DATABASE_URL×1 + API_KEY×2)", got, want)
	}
}

// fakeAuditLoggerT12 captures every LogAudit invocation for assertions.
type fakeAuditLoggerT12 struct {
	mu    sync.Mutex
	calls []string // formatted as "action:resourceType:resourceID"
}

func (f *fakeAuditLoggerT12) LogAudit(_ context.Context, _, action, _, resourceType, resourceID, _ string, _ map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, action+":"+resourceType+":"+resourceID)
	return nil
}

// TestSecretBulk_AuditLogPerKey — J-22 T1.2 absorbed: every successfully
// created secret in a bulk request MUST produce exactly ONE audit_log row
// via LogAudit. Invalid keys (status=failed) MUST NOT produce audit rows.
//
// EC-12 (fail-loud transactionality — audit failure rolls back the mutation)
// remains DEFERRED to a follow-up iteration; current iteration only locks
// the per-key audit-call count invariant.
func TestSecretBulk_AuditLogPerKey(t *testing.T) {
	store := &fakeSecretStoreT01{}
	audit := &fakeAuditLoggerT12{}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetAuditLogger(audit)

	body, _ := json.Marshal(map[string]any{
		"project": "proj-x",
		"secrets": []map[string]any{
			{"name": "DATABASE_URL", "value": "postgres://...", "envs": []string{"production"}},
			{"name": "INVALID-KEY", "value": "x", "envs": []string{"production"}},
			{"name": "API_KEY", "value": "secret123", "envs": []string{"production", "staging"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Bulk(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	// 2 valid keys → 2 audit calls; INVALID-KEY produces NO audit call.
	if got, want := len(audit.calls), 2; got != want {
		t.Errorf("audit calls: got %d want %d (DATABASE_URL + API_KEY, INVALID-KEY excluded). calls=%v",
			got, want, audit.calls)
	}
	// Action format check: SECRET_CREATED:secret:DATABASE_URL
	wantCall := "SECRET_CREATED:secret:DATABASE_URL"
	if len(audit.calls) > 0 && audit.calls[0] != wantCall {
		t.Errorf("audit call 0: got %q want %q", audit.calls[0], wantCall)
	}
}

// fakePlanLimiterDeny is a SecretPlanLimiter that always denies.
type fakePlanLimiterDeny struct{}

func (fakePlanLimiterDeny) CheckValue(_ context.Context, _, metric string, requested int) (enforcement.LimitResult, error) {
	return enforcement.LimitResult{Allowed: false, Plan: "free", Metric: metric, Current: requested, Limit: 10}, nil
}

// TestSecretBulk_PlanLimit_402 — J-22 T1.3 absorbed: when the plan limit
// gate denies the bulk creation request, response MUST be HTTP 402
// PLAN_LIMIT_EXCEEDED with body {current, limit:"secrets", max, plan}.
// store.Set MUST NOT be invoked. Audit log MUST NOT receive any rows.
func TestSecretBulk_PlanLimit_402(t *testing.T) {
	store := &fakeSecretStoreT01{}
	audit := &fakeAuditLoggerT12{}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetAuditLogger(audit)
	h.SetPlanLimiter(fakePlanLimiterDeny{})

	body, _ := json.Marshal(map[string]any{
		"project": "proj-x",
		"secrets": []map[string]any{
			{"name": "DATABASE_URL", "value": "x", "envs": []string{"production"}},
			{"name": "API_KEY", "value": "y", "envs": []string{"production"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Bulk(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("want 402, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("PLAN_LIMIT_EXCEEDED")) {
		t.Errorf("body should include PLAN_LIMIT_EXCEEDED code: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"plan":"free"`)) {
		t.Errorf("body should include plan=free: %s", w.Body.String())
	}
	// The person reads this in the console and in the CLI: it has to say what
	// was refused and what to do about it.
	var refusal struct {
		Message    string `json:"message"`
		UpgradeURL string `json:"upgrade_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &refusal); err != nil {
		t.Fatalf("decode the 402 body: %v", err)
	}
	for _, want := range []string{"secrets per environment", "production", "Upgrade your plan"} {
		if !strings.Contains(refusal.Message, want) {
			t.Errorf("message %q does not say %q", refusal.Message, want)
		}
	}
	if strings.Contains(refusal.Message, `"`) {
		t.Errorf("message %q quotes identifiers like a log line", refusal.Message)
	}
	if refusal.UpgradeURL != "/billing" {
		t.Errorf("upgrade_url = %q, want /billing", refusal.UpgradeURL)
	}
	if store.setCalls != 0 {
		t.Errorf("store.Set MUST NOT be invoked when plan denies; got %d", store.setCalls)
	}
	if len(audit.calls) != 0 {
		t.Errorf("audit MUST NOT receive rows when plan denies; got %d", len(audit.calls))
	}
}

// TestSecretBulk_DuplicateKeyInBatch_400 — EC-14 (backend side): when a
// bulk request contains the SAME key more than once in the secrets array,
// the entire request returns 400 DUPLICATE_KEY_IN_BATCH without invoking
// the store. The browser-side parseDotenv (T3.5) also catches this
// pre-flight, but the backend MUST enforce it independently for defense
// in depth + API consumer correctness.
func TestSecretBulk_DuplicateKeyInBatch_400(t *testing.T) {
	store := &fakeSecretStoreT01{}
	audit := &fakeAuditLoggerT12{}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetAuditLogger(audit)

	body, _ := json.Marshal(map[string]any{
		"project": "proj-x",
		"secrets": []map[string]any{
			{"name": "DATABASE_URL", "value": "x", "envs": []string{"production"}},
			{"name": "API_KEY", "value": "y", "envs": []string{"production"}},
			{"name": "DATABASE_URL", "value": "z", "envs": []string{"production"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Bulk(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("DUPLICATE_KEY_IN_BATCH")) {
		t.Errorf("body should include DUPLICATE_KEY_IN_BATCH: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("DATABASE_URL")) {
		t.Errorf("body should name the duplicated key DATABASE_URL: %s", w.Body.String())
	}
	if store.setCalls != 0 {
		t.Errorf("store.Set MUST NOT be invoked on duplicate-key reject; got %d", store.setCalls)
	}
	if len(audit.calls) != 0 {
		t.Errorf("audit MUST NOT receive rows on duplicate-key reject; got %d", len(audit.calls))
	}
}

// TestSecretBulk_SizeCap proves EC-18: requests over 100 entries return 413
// PAYLOAD_TOO_LARGE without invoking the store.
func TestSecretBulk_SizeCap(t *testing.T) {
	store := &fakeSecretStoreT01{}
	h := NewSecretHandler(store, &lockCountingFake{})

	secrets := make([]map[string]any, 0, 101)
	for i := 0; i < 101; i++ {
		secrets = append(secrets, map[string]any{
			"name":  "KEY",
			"value": "v",
			"envs":  []string{"production"},
		})
	}
	body, _ := json.Marshal(map[string]any{"project": "proj-x", "secrets": secrets})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Bulk(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("PAYLOAD_TOO_LARGE")) {
		t.Errorf("body should include PAYLOAD_TOO_LARGE code: %s", w.Body.String())
	}
	if store.setCalls != 0 {
		t.Errorf("store.Set should NOT be invoked when payload too large; got %d calls", store.setCalls)
	}
}

// ============================================================================
// T1.1 of plan 2026-06-10-j22-openbao-transit-eso-sync-plan v1.2 —
// Bulk handler wires k8sSync per-key, per-env with EC-01 (continue-best-effort)
// + EC-02 (sync_status_per_env granularity) absorptions.
// ============================================================================

// fakeK8sSyncT11 stubs ports.SecretK8sSync for routing tests. Records every
// CreateSecret invocation and supports per-env failure injection via failOnEnv.
type fakeK8sSyncT11 struct {
	mu          sync.Mutex
	createCalls []string // formatted as "tenant:project:env:name"
	deleteCalls []string
	failOnEnv   map[string]error // env → error to return on CreateSecret
}

func (f *fakeK8sSyncT11) CreateSecret(_ context.Context, ref ports.SecretRef, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls = append(f.createCalls, ref.TenantID+":"+ref.Project+":"+ref.Environment+":"+ref.Name)
	if err, ok := f.failOnEnv[ref.Environment]; ok {
		return err
	}
	return nil
}

func (f *fakeK8sSyncT11) DeleteSecret(_ context.Context, ref ports.SecretRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls = append(f.deleteCalls, ref.TenantID+":"+ref.Project+":"+ref.Environment+":"+ref.Name)
	return nil
}

// fakeFailingStore stubs ports.SecretStore with per-key failure injection so
// EC-01 (continue-best-effort) can be exercised: entry 3 of 5 fails, entries
// 1+2+4+5 succeed.
type fakeFailingStore struct {
	mu         sync.Mutex
	setCalls   int
	failOnName map[string]error // secret name → error to return on Set
}

func (f *fakeFailingStore) ListNames(_ context.Context, _, _, _ string) ([]ports.SecretMetadata, error) {
	return nil, nil
}

func (f *fakeFailingStore) Set(_ context.Context, _, _, _, _, name string, _ []byte) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	if err, ok := f.failOnName[name]; ok {
		return false, err
	}
	return true, nil
}

func (f *fakeFailingStore) Delete(_ context.Context, _, _, _, _, _ string) (bool, error) {
	return true, nil
}

// TestSecretBulk_K8sSyncPerEnv — plan T1.1 acceptance criterion: for each
// VALID entry, h.k8sSync.CreateSecret MUST be invoked exactly ONCE per env
// in entry.Envs. Fixture [DATABASE_URL×1env, API_KEY×2envs, INVALID-KEY×1env]
// → audit×2 (per-key, INVALID excluded), k8sSync×3 (DATABASE_URL×1 + API_KEY×2,
// INVALID excluded).
func TestSecretBulk_K8sSyncPerEnv(t *testing.T) {
	store := &fakeSecretStoreT01{}
	audit := &fakeAuditLoggerT12{}
	k8s := &fakeK8sSyncT11{}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetAuditLogger(audit)
	h.SetK8sSync(k8s)

	body, _ := json.Marshal(map[string]any{
		"project": "proj-x",
		"secrets": []map[string]any{
			{"name": "DATABASE_URL", "value": "postgres://x", "envs": []string{"production"}},
			{"name": "API_KEY", "value": "secret123", "envs": []string{"production", "staging"}},
			{"name": "INVALID-KEY", "value": "x", "envs": []string{"production"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Bulk(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got, want := store.setCalls, 3; got != want {
		t.Errorf("store.Set calls: got %d want %d (DATABASE_URL×1 + API_KEY×2)", got, want)
	}
	if got, want := len(audit.calls), 2; got != want {
		t.Errorf("audit calls: got %d want %d (INVALID excluded). calls=%v", got, want, audit.calls)
	}
	if got, want := len(k8s.createCalls), 3; got != want {
		t.Errorf("k8sSync.CreateSecret calls: got %d want %d (DATABASE_URL×1 + API_KEY×2). calls=%v",
			got, want, k8s.createCalls)
	}
	wantCalls := []string{
		"tn-test-123:proj-x:production:DATABASE_URL",
		"tn-test-123:proj-x:production:API_KEY",
		"tn-test-123:proj-x:staging:API_KEY",
	}
	if len(k8s.createCalls) == 3 {
		for i, want := range wantCalls {
			if k8s.createCalls[i] != want {
				t.Errorf("k8sSync call %d: got %q want %q", i, k8s.createCalls[i], want)
			}
		}
	}
}

// TestSecretBulk_K8sSyncSkipsInvalidKey — plan T1.1 acceptance: INVALID-KEY
// (failing secretNameRegex) MUST NOT trigger k8sSync.CreateSecret OR audit.
// Defense against silent invalid-key materialization.
func TestSecretBulk_K8sSyncSkipsInvalidKey(t *testing.T) {
	store := &fakeSecretStoreT01{}
	audit := &fakeAuditLoggerT12{}
	k8s := &fakeK8sSyncT11{}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetAuditLogger(audit)
	h.SetK8sSync(k8s)

	body, _ := json.Marshal(map[string]any{
		"project": "proj-x",
		"secrets": []map[string]any{
			{"name": "INVALID-KEY", "value": "x", "envs": []string{"production"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Bulk(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if store.setCalls != 0 {
		t.Errorf("store.Set should NOT be invoked for INVALID-KEY; got %d", store.setCalls)
	}
	if len(audit.calls) != 0 {
		t.Errorf("audit should NOT be invoked for INVALID-KEY; got %d", len(audit.calls))
	}
	if len(k8s.createCalls) != 0 {
		t.Errorf("k8sSync should NOT be invoked for INVALID-KEY; got %d calls=%v",
			len(k8s.createCalls), k8s.createCalls)
	}
}

// TestSecretBulk_PartialEnvFailureReturnsPerEnvStatus — EC-02 absorption.
// API_KEY × [production, staging] where production k8sSync succeeds but
// staging fails MUST return:
//   - sync_status_per_env = {"production": "synced", "staging": "failed"}
//   - aggregate sync_status = "partial"
//   - PG row stays for both envs (D2 ADR — k8sSync failure does NOT roll
//     back the store.Set). store.Set called twice.
func TestSecretBulk_PartialEnvFailureReturnsPerEnvStatus(t *testing.T) {
	store := &fakeSecretStoreT01{}
	audit := &fakeAuditLoggerT12{}
	k8s := &fakeK8sSyncT11{
		failOnEnv: map[string]error{
			"staging": errFakeK8sSync,
		},
	}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetAuditLogger(audit)
	h.SetK8sSync(k8s)

	body, _ := json.Marshal(map[string]any{
		"project": "proj-x",
		"secrets": []map[string]any{
			{"name": "API_KEY", "value": "secret123", "envs": []string{"production", "staging"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Bulk(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if store.setCalls != 2 {
		t.Errorf("store.Set should be called 2x (both envs persisted to PG); got %d", store.setCalls)
	}
	if len(k8s.createCalls) != 2 {
		t.Errorf("k8sSync.CreateSecret should be called 2x; got %d", len(k8s.createCalls))
	}

	var resp struct {
		Results []struct {
			Name             string            `json:"name"`
			Status           string            `json:"status"`
			SyncStatus       string            `json:"sync_status"`
			SyncStatusPerEnv map[string]string `json:"sync_status_per_env"`
			ErrorCode        string            `json:"error_code,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results length: got %d want 1", len(resp.Results))
	}
	r := resp.Results[0]
	if r.Status != "created" {
		t.Errorf("aggregate Status: got %q want created (store succeeded for both envs)", r.Status)
	}
	if r.SyncStatus != "partial" {
		t.Errorf("aggregate sync_status: got %q want partial", r.SyncStatus)
	}
	if got := r.SyncStatusPerEnv["production"]; got != "synced" {
		t.Errorf("sync_status_per_env[production]: got %q want synced", got)
	}
	if got := r.SyncStatusPerEnv["staging"]; got != "failed" {
		t.Errorf("sync_status_per_env[staging]: got %q want failed", got)
	}
}

// TestSecretBulk_StoreFailureContinuesRemainingKeys — EC-01 absorption.
// 5 entries where entry 3 (BROKEN_KEY) gets store.Set failure → entries
// 1+2+4+5 MUST still succeed with sync_status="pending" or "synced" per
// k8sSync wiring. Entry 3 gets sync_status="failed" + error_code populated.
// No short-circuit: store.Set called for all 5 (we attempt each).
func TestSecretBulk_StoreFailureContinuesRemainingKeys(t *testing.T) {
	store := &fakeFailingStore{
		failOnName: map[string]error{
			"BROKEN_KEY": errFakeStoreSet,
		},
	}
	audit := &fakeAuditLoggerT12{}
	k8s := &fakeK8sSyncT11{}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetAuditLogger(audit)
	h.SetK8sSync(k8s)

	body, _ := json.Marshal(map[string]any{
		"project": "proj-x",
		"secrets": []map[string]any{
			{"name": "KEY1", "value": "v1", "envs": []string{"production"}},
			{"name": "KEY2", "value": "v2", "envs": []string{"production"}},
			{"name": "BROKEN_KEY", "value": "v3", "envs": []string{"production"}},
			{"name": "KEY4", "value": "v4", "envs": []string{"production"}},
			{"name": "KEY5", "value": "v5", "envs": []string{"production"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Bulk(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if store.setCalls != 5 {
		t.Errorf("store.Set called: got %d want 5 (continue-best-effort attempts each)", store.setCalls)
	}
	if got, want := len(k8s.createCalls), 4; got != want {
		t.Errorf("k8sSync calls: got %d want %d (BROKEN_KEY excluded). calls=%v",
			got, want, k8s.createCalls)
	}

	var resp struct {
		Results []struct {
			Name      string `json:"name"`
			Status    string `json:"status"`
			ErrorCode string `json:"error_code,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Results) != 5 {
		t.Fatalf("results length: got %d want 5", len(resp.Results))
	}
	for i, r := range resp.Results {
		if r.Name == "BROKEN_KEY" {
			if r.Status != "failed" {
				t.Errorf("entry %d BROKEN_KEY: status=%q want failed", i, r.Status)
			}
			if r.ErrorCode == "" {
				t.Errorf("entry %d BROKEN_KEY: error_code MUST be populated", i)
			}
		} else {
			if r.Status != "created" {
				t.Errorf("entry %d %s: status=%q want created (EC-01 continue-best-effort)",
					i, r.Name, r.Status)
			}
		}
	}
}

// Sentinel errors used by EC-01 + EC-02 tests above.
var (
	errFakeK8sSync  = newFakeErr("simulated k8sSync failure")
	errFakeStoreSet = newFakeErr("simulated store.Set failure")
)

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }

func newFakeErr(msg string) *fakeErr { return &fakeErr{msg: msg} }

// ============================================================================
// T1.2 of plan 2026-06-10-j22-openbao-transit-eso-sync-plan v1.2 —
// Cross-tenant binding check pre store.Set + k8sSync. EC-03 absorbed: response
// is BYTE-IDENTICAL between (b) cross-tenant attempt and (c) genuinely-
// nonexistent project. Both paths return 404 PROJECT_NOT_FOUND to prevent
// observable response discrepancy (CWE-204 / existence-leak).
// ============================================================================

// fakeProjectCheckerT12 implements SecretProjectChecker. Maps (tenant, name)
// → "exists" for the legit owner; missing entries return nil to simulate both
// cross-tenant attempts (entry exists for OTHER tenant) and nonexistent paths.
type fakeProjectCheckerT12 struct {
	// owned[name] = tenant_id of the actual owner; lookups for any other
	// tenant return nil (mimicking the tenant-scoped GetByName SQL).
	owned map[string]string
}

func (f *fakeProjectCheckerT12) GetByName(_ context.Context, tenantID, name string) (*ports.ProjectResult, error) {
	owner, ok := f.owned[name]
	if !ok || owner != tenantID {
		return nil, nil
	}
	return &ports.ProjectResult{Name: name}, nil
}

// TestSecretBulk_CrossTenantBindingReturns404 — EC-03 absorption. JWT carries
// tenant_id=tn-test-123, request body references project="proj-of-tn-Y"
// (project exists for OTHER tenant). Expected: HTTP 404 PROJECT_NOT_FOUND.
// ZERO store.Set + ZERO k8sSync + ZERO audit calls.
func TestSecretBulk_CrossTenantBindingReturns404(t *testing.T) {
	store := &fakeSecretStoreT01{}
	audit := &fakeAuditLoggerT12{}
	k8s := &fakeK8sSyncT11{}
	checker := &fakeProjectCheckerT12{
		owned: map[string]string{
			"proj-of-tn-Y": "tn-other-tenant", // owned by a DIFFERENT tenant
		},
	}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetAuditLogger(audit)
	h.SetK8sSync(k8s)
	h.SetProjectChecker(checker)

	body, _ := json.Marshal(map[string]any{
		"project": "proj-of-tn-Y",
		"secrets": []map[string]any{
			{"name": "DATABASE_URL", "value": "x", "envs": []string{"production"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Bulk(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (EC-03 existence-leak prevention), got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("PROJECT_NOT_FOUND")) {
		t.Errorf("body should include PROJECT_NOT_FOUND code (EC-03 byte-identical contract): %s", w.Body.String())
	}
	if store.setCalls != 0 {
		t.Errorf("store.Set MUST NOT be invoked on cross-tenant attempt; got %d", store.setCalls)
	}
	// Audit MAY contain a PROJECT_LOOKUP_FAILED row (forensic trail per EC-03);
	// SECRET_CREATED MUST NOT appear.
	for _, c := range audit.calls {
		if bytes.HasPrefix([]byte(c), []byte("SECRET_CREATED:")) {
			t.Errorf("audit MUST NOT receive SECRET_CREATED on cross-tenant attempt; got %q", c)
		}
	}
	if len(k8s.createCalls) != 0 {
		t.Errorf("k8sSync MUST NOT be invoked on cross-tenant attempt; got %d", len(k8s.createCalls))
	}
	// Forensic audit assertion: PROJECT_LOOKUP_FAILED row written so security
	// ops can correlate later via SQL.
	foundForensic := false
	for _, c := range audit.calls {
		if bytes.HasPrefix([]byte(c), []byte("PROJECT_LOOKUP_FAILED:")) {
			foundForensic = true
		}
	}
	if !foundForensic {
		t.Errorf("expected PROJECT_LOOKUP_FAILED audit row for forensics; got calls=%v", audit.calls)
	}
}

// TestSecretBulk_NonexistentProjectReturns404 — EC-03 absorption: a project
// that does NOT exist for ANY tenant returns 404 PROJECT_NOT_FOUND with the
// SAME body shape as the cross-tenant attempt. Defense against observable
// response discrepancy (CWE-204).
func TestSecretBulk_NonexistentProjectReturns404(t *testing.T) {
	store := &fakeSecretStoreT01{}
	audit := &fakeAuditLoggerT12{}
	k8s := &fakeK8sSyncT11{}
	checker := &fakeProjectCheckerT12{
		owned: map[string]string{}, // no projects at all
	}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetAuditLogger(audit)
	h.SetK8sSync(k8s)
	h.SetProjectChecker(checker)

	body, _ := json.Marshal(map[string]any{
		"project": "typo-name-does-not-exist",
		"secrets": []map[string]any{
			{"name": "DATABASE_URL", "value": "x", "envs": []string{"production"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Bulk(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("PROJECT_NOT_FOUND")) {
		t.Errorf("body should include PROJECT_NOT_FOUND code: %s", w.Body.String())
	}
	if store.setCalls != 0 || len(k8s.createCalls) != 0 {
		t.Errorf("no mutation side effects expected; got store=%d k8sSync=%d",
			store.setCalls, len(k8s.createCalls))
	}
	// Audit MAY contain a PROJECT_LOOKUP_FAILED row (forensic trail per EC-03);
	// SECRET_CREATED MUST NOT appear.
	for _, c := range audit.calls {
		if bytes.HasPrefix([]byte(c), []byte("SECRET_CREATED:")) {
			t.Errorf("audit MUST NOT receive SECRET_CREATED on nonexistent project; got %q", c)
		}
	}
}

// ============================================================================
// The store is the single encryption boundary (principle_violations#180): the
// Bulk handler hands it the user's value and never pre-encrypts or sniffs.
// ============================================================================

// recordingStore stubs SecretStore, captures the value bytes Set received and
// fails the names listed in failWith.
type recordingStore struct {
	mu       sync.Mutex
	setCalls int
	values   []string // captured value-bytes per successful Set call
	failWith map[string]error
}

func (r *recordingStore) ListNames(_ context.Context, _, _, _ string) ([]ports.SecretMetadata, error) {
	return nil, nil
}

func (r *recordingStore) Set(_ context.Context, _, _, _, _, name string, value []byte) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setCalls++
	if err, ok := r.failWith[name]; ok {
		return false, err
	}
	r.values = append(r.values, string(value))
	return true, nil
}

func (r *recordingStore) Delete(_ context.Context, _, _, _, _, _ string) (bool, error) {
	return true, nil
}

func postBulk(t *testing.T, h *SecretHandler, secrets []map[string]any) secretBulkResponse {
	t.Helper()
	w := httptest.NewRecorder()
	h.Bulk(w, bulkRequest(t, secrets))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp secretBulkResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode bulk response: %v", err)
	}
	return resp
}

// The handler passes the user's value to the store untouched, including a value
// that happens to look like transit ciphertext: encrypting is the store's job,
// and a value is data, not a format marker.
func TestSecretBulk_HandsTheUserValueToTheStore(t *testing.T) {
	store := &recordingStore{}
	h := NewSecretHandler(store, &lockCountingFake{})

	postBulk(t, h, []map[string]any{
		{"name": "DATABASE_URL", "value": "vault:v1:looks-like-ciphertext", "envs": []string{"production"}},
	})

	if len(store.values) != 1 || store.values[0] != "vault:v1:looks-like-ciphertext" {
		t.Errorf("store must receive the user's value; got %v", store.values)
	}
}

// A transit policy denial raised by the store reads "denied" for that env —
// permanent, needs an operator — while the other entries of the batch are
// still written (EC-01 continue-best-effort).
func TestSecretBulk_StoreTransitDenialReadsAsDenied(t *testing.T) {
	store := &recordingStore{failWith: map[string]error{
		"BAD_SECRET": fmt.Errorf("secret_store: encrypt: %w", loopzerovault.ErrTransitKeyCreateFailed),
	}}
	h := NewSecretHandler(store, &lockCountingFake{})

	resp := postBulk(t, h, []map[string]any{
		{"name": "OK_SECRET", "value": "plain-ok", "envs": []string{"production"}},
		{"name": "BAD_SECRET", "value": "plain-bad", "envs": []string{"production"}},
	})

	statuses := map[string]string{}
	for _, r := range resp.Results {
		statuses[r.Name] = r.SyncStatusPerEnv["production"]
	}
	if statuses["BAD_SECRET"] != "denied" {
		t.Errorf("BAD_SECRET production status = %q, want denied", statuses["BAD_SECRET"])
	}
	if len(store.values) != 1 || store.values[0] != "plain-ok" {
		t.Errorf("OK_SECRET must still be written; got %v", store.values)
	}
}

// fakeFailingStoreT23 is a SecretStore that records Set calls
// but always returns success — used by tests that need to capture
// the byte sequences the Bulk handler passed in.
//
// Note: distinct from fakeSecretStoreT01 (which only counts calls without
// capturing values).

// TestSecretBulk_CrossTenantAndNonexistentResponsesByteIdentical — EC-03 hard
// invariant: the JSON body MUST be byte-identical between cross-tenant and
// nonexistent paths so an attacker cannot probe project name existence via
// response discrepancy (CWE-204).
func TestSecretBulk_CrossTenantAndNonexistentResponsesByteIdentical(t *testing.T) {
	makeReq := func(checker *fakeProjectCheckerT12, projectName string) []byte {
		store := &fakeSecretStoreT01{}
		h := NewSecretHandler(store, &lockCountingFake{})
		h.SetProjectChecker(checker)
		body, _ := json.Marshal(map[string]any{
			"project": projectName,
			"secrets": []map[string]any{
				{"name": "DATABASE_URL", "value": "x", "envs": []string{"production"}},
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = routestest.WithAuth(req, routestest.DefaultUserContext())
		w := httptest.NewRecorder()
		h.Bulk(w, req)
		return w.Body.Bytes()
	}

	crossTenantBody := makeReq(
		&fakeProjectCheckerT12{owned: map[string]string{"shared-name": "tn-other"}},
		"shared-name",
	)
	nonexistentBody := makeReq(
		&fakeProjectCheckerT12{owned: map[string]string{}},
		"shared-name",
	)

	if !bytes.Equal(crossTenantBody, nonexistentBody) {
		t.Errorf("EC-03 BYTE-IDENTICAL invariant violated.\n  cross-tenant: %s\n  nonexistent:  %s",
			crossTenantBody, nonexistentBody)
	}
}

// Red Team loopzero-cloud#92 regression-protector (2026-06-27): the
// /api/v1/secrets?project=<name> List endpoint MUST refuse non-existent
// projects with 404 PROJECT_NOT_FOUND instead of 200 + empty list.
// Without this, the dashboard's Secrets page rendered functional UI
// (with "Add secret" form) on top of a deleted project.
func TestSecretHandler_List_NonExistentProject_Returns404(t *testing.T) {
	store := &fakeSecretStoreT01{}
	checker := &fakeProjectCheckerT12{
		owned: map[string]string{
			// tn-test-123 (= routestest.DefaultUserContext()) owns only "owned-project".
			// "nonexistent-zzz" is NOT in the map → checker returns nil.
			"owned-project": "tn-test-123",
		},
	}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetProjectChecker(checker)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/secrets?env=production&project=nonexistent-zzz", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Red Team #92: want 404 PROJECT_NOT_FOUND for missing project, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PROJECT_NOT_FOUND") {
		t.Errorf("Red Team #92: error body must include PROJECT_NOT_FOUND code, got %s", w.Body.String())
	}
}

func TestSecretHandler_List_ExistingProject_AllowsThrough(t *testing.T) {
	store := &fakeSecretStoreT01{}
	checker := &fakeProjectCheckerT12{
		owned: map[string]string{"owned-project": "tn-test-123"},
	}
	h := NewSecretHandler(store, &lockCountingFake{})
	h.SetProjectChecker(checker)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/secrets?env=production&project=owned-project", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 OK for owned project, got %d body=%s", w.Code, w.Body.String())
	}
}

// B-072: a policy denial and a transient failure are opposite facts for whoever
// is on call, and both reported "failed" until 2026-08-13. The branch that now
// separates them shipped untested in its first version — an independent review
// measured that no test asserted "denied".
func TestTransitFailureStatus_SeparatesDenialFromTransient(t *testing.T) {
	if got := transitFailureStatus(fmt.Errorf("wrapped: %w", loopzerovault.ErrTransitKeyCreateFailed)); got != "denied" {
		t.Errorf("policy denial classified as %q; it is PERMANENT and needs an operator, so it must "+
			"not read like a timeout that clears itself (Rule 8)", got)
	}
	if got := transitFailureStatus(context.DeadlineExceeded); got != "failed" {
		t.Errorf("a transient failure classified as %q; only ErrTransitKeyCreateFailed is a denial", got)
	}
	if got := transitFailureStatus(errors.New("boom")); got != "failed" {
		t.Errorf("an unclassified error became %q; the default must be the recoverable one", got)
	}
}

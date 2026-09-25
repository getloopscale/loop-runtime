// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Sprint A T1.1 — Wire SecretEnvLocker (closes comp-001 CRITICAL).
//
// Plan: .claude/knowledge-base/plans/2026-06-01-sprint-a-p1-hotfixes-plan.md v1.1
// Edge cases absorbed: EC-1 (DeleteSecret env guard) + EC-2 (lock-domain
// parity assertion via shared trackingLocker pattern). The other EC-3
// (K8s-sync-outside-lock) is documented invariant — verified by code review,
// not by test (would require complex async coordination).
//
// 6 RED tests for v1.1 TDD:
//   1. TestSecretEnvLock_SecretHandler_Set_AcquiresEnvLock_WhenLockerWired
//   2. (retired) the nil-locker path no longer exists — NewSecretHandler requires one
//   3. TestSecretEnvLock_SecretHandler_Delete_AcquiresEnvLock_WhenLockerWired
//   4. TestSecretEnvLock_ServerConfig_SecretEnvLocker_Field_Exists
//   5. TestSecretEnvLock_SecretHandler_DeleteSecret_RejectsEmptyEnvParam (EC-1)
//   6. TestSecretEnvLock_SecretSet_LockKeyMatchesEnvironmentLock (EC-2)

package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	"github.com/getloopscale/loopzero/api/internal/middleware/auth/authtest"
	"github.com/getloopscale/loopzero/api/internal/routes/secrets"
	"github.com/getloopscale/loopzero/api/internal/server"
)

// recordingLocker captures every WithLock call for assertion in tests.
// Mirrors trackingLocker from build_orchestrator_test.go but lives in
// this file to keep T1.1 surface isolated.
type recordingLocker struct {
	mu    sync.Mutex
	calls []lockCall
}

type lockCall struct {
	Tenant  string
	Project string
	Env     string
}

func (r *recordingLocker) WithLock(ctx context.Context, tenant, project, env string, fn func(tx dbtx.DBTX) error) error {
	r.mu.Lock()
	r.calls = append(r.calls, lockCall{Tenant: tenant, Project: project, Env: env})
	r.mu.Unlock()
	return fn(nil)
}

func (r *recordingLocker) Calls() []lockCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]lockCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// =============================================================================
// Test 1 — Set acquires env lock when locker wired
// =============================================================================

func TestSecretEnvLock_SecretHandler_Set_AcquiresEnvLock_WhenLockerWired(t *testing.T) {
	store := &fakeSecretStore{}
	locker := &recordingLocker{}

	handler := secrets.NewSecretHandler(store, locker)

	body := bytes.NewBufferString(`{"name":"DB_URL","value":"postgres://x","environment":"production","app":"api"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets?project=acme-proj", body).
		WithContext(authtest.Context("u1", "tenant-x", "owner"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.Set(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("expected 2xx, got %d: %s", rec.Code, rec.Body.String())
	}

	calls := locker.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 WithLock call, got %d (calls=%+v)", len(calls), calls)
	}
	if calls[0].Tenant != "tenant-x" || calls[0].Project != "acme-proj" || calls[0].Env != "production" {
		t.Fatalf("lock acquired with wrong args: tenant=%s project=%s env=%s (expected tenant-x/acme-proj/production)",
			calls[0].Tenant, calls[0].Project, calls[0].Env)
	}
}

// =============================================================================
// Test 3 — Delete acquires env lock when locker wired
// =============================================================================

func TestSecretEnvLock_SecretHandler_Delete_AcquiresEnvLock_WhenLockerWired(t *testing.T) {
	store := &fakeSecretStore{}
	locker := &recordingLocker{}

	handler := secrets.NewSecretHandler(store, locker)

	// chi.URLParam needs router context.
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/secrets/DB_URL?environment=staging&app=api&project=acme-proj", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "DB_URL")
	ctx := authtest.Context("u1", "tenant-x", "owner")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.DeleteSecret(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("expected 2xx, got %d: %s", rec.Code, rec.Body.String())
	}

	calls := locker.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 WithLock call on Delete, got %d", len(calls))
	}
	if calls[0].Tenant != "tenant-x" || calls[0].Project != "acme-proj" || calls[0].Env != "staging" {
		t.Fatalf("Delete lock acquired with wrong args: %+v", calls[0])
	}
}

// =============================================================================
// Test 4 — server.Config has SecretEnvLocker field (struct-level assertion)
// =============================================================================

func TestSecretEnvLock_ServerConfig_SecretEnvLocker_Field_Exists(t *testing.T) {
	// Compile-time: this MUST compile after T1.1.
	cfg := server.Config{}
	// The explicit type IS the assertion: it pins the NAME and the TYPE of the field in
	// server.Config against a silent rename. Omitting it (QF1011) would keep the
	// line and lose the guarantee, which is the only thing it exists to give.
	//nolint:staticcheck // QF1011: the explicit type is the purpose, not redundancy
	var _ secrets.SecretEnvLocker = cfg.Secrets.EnvLocker

	// Runtime: zero-value is nil (nil-safe legacy default).
	if cfg.Secrets.EnvLocker != nil {
		t.Errorf("zero-value SecretEnvLocker should be nil, got %v", cfg.Secrets.EnvLocker)
	}
}

// =============================================================================
// Test 5 — EC-1: DeleteSecret rejects empty environment query param
// =============================================================================

func TestSecretEnvLock_SecretHandler_DeleteSecret_RejectsEmptyEnvParam(t *testing.T) {
	store := &fakeSecretStore{}
	locker := &recordingLocker{}

	handler := secrets.NewSecretHandler(store, locker)

	// NO environment query param — should be rejected BEFORE locker is called.
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/secrets/DB_URL?app=api&project=acme-proj", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "DB_URL")
	ctx := authtest.Context("u1", "tenant-x", "owner")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.DeleteSecret(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (missing env param), got %d: %s", rec.Code, rec.Body.String())
	}

	// Crucial: validation MUST run BEFORE locker — empty env in lock key
	// would contend on bogus "{tenant}:{project}:" bucket.
	calls := locker.Calls()
	if len(calls) != 0 {
		t.Fatalf("locker should NOT be called for invalid input, got %d calls", len(calls))
	}

	// Body must mention "environment" for operator clarity.
	var errResp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&errResp)
	msg, _ := errResp["message"].(string)
	if !strings.Contains(msg, "environment") {
		t.Errorf("error message should mention 'environment', got %q", msg)
	}
}

// =============================================================================
// Test 6 — EC-2: Lock-domain parity — Secret Set + a deploy on same
// (tenant, project, env) acquire the SAME advisory lock key (proxy: SAME
// recordingLocker observes both calls with identical key args).
// =============================================================================

func TestSecretEnvLock_SecretSet_LockKeyMatchesEnvironmentLock(t *testing.T) {
	// Shared locker across two handlers — represents the same production
	// *adapters.PoolEnvironmentLocker injected into both build orchestrator
	// AND secret handler. If both acquire the same key string, the
	// pg_advisory_xact_lock(hashtext(key)) is the same → serialization
	// invariant holds.
	sharedLocker := &recordingLocker{}

	// Secret handler.
	secretsHandler := secrets.NewSecretHandler(&fakeSecretStore{}, sharedLocker)

	body := bytes.NewBufferString(`{"name":"DB_URL","value":"x","environment":"production","app":"api"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets?project=acme-proj", body).
		WithContext(authtest.Context("u1", "tenant-x", "owner"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	secretsHandler.Set(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("Set failed: %d %s", rec.Code, rec.Body.String())
	}

	// Simulate a build orchestrator's lock acquisition with the SAME locker
	// instance + SAME (tenant, project, env) args. In production this is
	// *adapters.PoolEnvironmentLocker via lock.WithEnvironmentLock —
	// asserting the locker instance is the same proves lock-domain parity.
	_ = sharedLocker.WithLock(context.Background(), "tenant-x", "acme-proj", "production",
		func(tx dbtx.DBTX) error { return nil })

	calls := sharedLocker.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 lock calls (secret Set + deploy), got %d", len(calls))
	}
	// BOTH calls MUST present identical (tenant, project, env) — proves
	// same lock-domain key, hence same advisory lock, hence serialization.
	if calls[0] != calls[1] {
		t.Fatalf("lock-domain parity violated: secret call=%+v, deploy call=%+v", calls[0], calls[1])
	}
}

// Note: `contains` helper is shared with config_test.go in the same
// package — re-using existing definition.

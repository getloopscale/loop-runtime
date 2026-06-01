// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T0.1 of plan 2026-06-01-p1-security-hardening (Plano A) — regression tests
// for comp-001 (CRITICAL): SecretHandler.locker MUST be non-nil at runtime
// so secret mutations serialize under pg_advisory_xact_lock with concurrent
// deploys. The wiring was done in Sprint A T1.1 (setup_server.go:82); these
// tests lock the invariant so a future refactor cannot silently re-introduce
// the unlocked path.

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/domain"
)

// --- Fakes ---

// lockCountingFake is a recording SecretEnvLocker that captures every
// WithLock invocation. After a request that should acquire the lock, the
// test asserts calls length == 1 + key matches expected tenant:project:env.
type lockCountingFake struct {
	mu    sync.Mutex
	calls []string
}

func (l *lockCountingFake) WithLock(_ context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error {
	l.mu.Lock()
	l.calls = append(l.calls, tenant+":"+project+":"+env)
	l.mu.Unlock()
	return fn(nil)
}

// fakeSecretStoreT01 stubs domain.SecretStore for routing tests.
type fakeSecretStoreT01 struct {
	setCalls    int
	deleteCalls int
}

func (f *fakeSecretStoreT01) ListNames(_ context.Context, _, _, _ string) ([]domain.SecretMetadata, error) {
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
	h := NewSecretHandler(store)
	h.SetLocker(locker)

	body, _ := json.Marshal(map[string]string{
		"environment": "production",
		"app":         "api",
		"name":        "DB_PASS",
		"value":       "s3cret",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets?project=proj-x", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(req, defaultUserContext())
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

// TestSecretSet_LegacyPathStillWorks_WhenLockerNil proves the nil-locker
// fallback (legacy/test path) does not panic and still invokes the store.
// This guards the NewSecretHandler default (no locker), used by unit tests
// + legacy boot paths.
func TestSecretSet_LegacyPathStillWorks_WhenLockerNil(t *testing.T) {
	store := &fakeSecretStoreT01{}
	h := NewSecretHandler(store)
	// NO SetLocker call — h.locker stays nil.

	body, _ := json.Marshal(map[string]string{
		"environment": "production",
		"app":         "api",
		"name":        "DB_PASS",
		"value":       "s3cret",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets?project=proj-x", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("nil-locker path must NOT panic; got %v", rec)
		}
	}()

	h.Set(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("want 200/201 on legacy path, got %d body=%s", w.Code, w.Body.String())
	}
	if store.setCalls != 1 {
		t.Fatalf("store.Set should fire exactly 1x even without locker; got %d", store.setCalls)
	}
}

// TestSecretDelete_AcquiresEnvLock mirrors the Set test for Delete handler
// — both mutate the secret store and both MUST serialize via the env lock.
func TestSecretDelete_AcquiresEnvLock(t *testing.T) {
	store := &fakeSecretStoreT01{}
	locker := &lockCountingFake{}
	h := NewSecretHandler(store)
	h.SetLocker(locker)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/secrets/DB_PASS?project=proj-x&environment=production&app=api", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "DB_PASS")
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

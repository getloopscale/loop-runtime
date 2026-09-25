// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secretshttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/routestest"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// heldLocker records which (project, env) lock is held while fn runs, so a
// store can assert that every write happened inside the lock for its env.
type heldLocker struct {
	mu      sync.Mutex
	held    map[string]bool
	calls   []string
	failEnv string // WithLock returns ErrLockTimeout for this env without running fn
}

func (l *heldLocker) WithLock(_ context.Context, _, project, env string, fn func(tx dbtx.DBTX) error) error {
	key := project + ":" + env
	l.mu.Lock()
	l.calls = append(l.calls, key)
	if env == l.failEnv {
		l.mu.Unlock()
		return loopzeroerrors.ErrLockTimeout
	}
	if l.held == nil {
		l.held = map[string]bool{}
	}
	l.held[key] = true
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.held[key] = false
		l.mu.Unlock()
	}()
	return fn(nil)
}

func (l *heldLocker) isHeld(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held[key]
}

// lockAssertingStore fails the test when Set runs outside the lock of its env.
type lockAssertingStore struct {
	t        *testing.T
	locker   *heldLocker
	unlocked []string
}

func (s *lockAssertingStore) ListNames(context.Context, string, string, string) ([]ports.SecretMetadata, error) {
	return nil, nil
}

func (s *lockAssertingStore) Set(_ context.Context, _, project, env, _, name string, _ []byte) (bool, error) {
	if !s.locker.isHeld(project + ":" + env) {
		s.unlocked = append(s.unlocked, env+"/"+name)
	}
	return true, nil
}

func (s *lockAssertingStore) Delete(context.Context, string, string, string, string, string) (bool, error) {
	return true, nil
}

func bulkRequest(t *testing.T, secrets []map[string]any) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]any{"project": "proj-x", "secrets": secrets})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return routestest.WithAuth(req, routestest.DefaultUserContext())
}

func TestSecretBulk_WritesEachEnvironmentUnderItsEnvironmentLock(t *testing.T) {
	locker := &heldLocker{}
	store := &lockAssertingStore{t: t, locker: locker}
	h := NewSecretHandler(store, locker)
	w := httptest.NewRecorder()

	h.Bulk(w, bulkRequest(t, []map[string]any{
		{"name": "DATABASE_URL", "value": "postgres://x", "envs": []string{"production"}},
		{"name": "API_KEY", "value": "k", "envs": []string{"production", "staging"}},
	}))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if len(store.unlocked) != 0 {
		t.Fatalf("bulk writes ran outside the environment lock: %v", store.unlocked)
	}
	if got, want := len(locker.calls), 2; got != want {
		t.Fatalf("want one lock per (project, env) group, got %d: %v", got, locker.calls)
	}
}

func TestSecretBulk_ReportsLockTimeoutAsFailureForThatEnvironmentOnly(t *testing.T) {
	locker := &heldLocker{failEnv: "staging"}
	store := &lockAssertingStore{t: t, locker: locker}
	h := NewSecretHandler(store, locker)
	w := httptest.NewRecorder()

	h.Bulk(w, bulkRequest(t, []map[string]any{
		{"name": "API_KEY", "value": "k", "envs": []string{"production", "staging"}},
	}))

	var resp secretBulkResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	got := resp.Results[0]
	if got.SyncStatusPerEnv["staging"] != "failed" || got.ErrorCode != "STORE_SET_PARTIAL" {
		t.Fatalf("staging lock timeout must fail staging only: %+v", got)
	}
}

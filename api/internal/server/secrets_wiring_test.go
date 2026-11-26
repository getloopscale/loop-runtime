// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/server"
	"github.com/getloopscale/loop-runtime/pkg/enforcement"
)

// The secret audit rows and the secrets plan gate were implemented on the
// handler and never wired from the server ("implemented but not wired"),
// so production wrote no SECRET_* audit row and evaluated no plan limit.
// These assertions go through the REAL router, where that omission lives.

type recordingAuditLogger struct {
	mu      sync.Mutex
	actions []string
}

func (r *recordingAuditLogger) LogAudit(_ context.Context, _, action, _, _, _, _ string, _ map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions = append(r.actions, action)
	return nil
}

type fixedLimitsStore struct {
	plan string
	hard int
}

func (f fixedLimitsStore) GetTenantPlan(context.Context, string) (string, error) { return f.plan, nil }
func (f fixedLimitsStore) GetLimits(context.Context, string, string) (int, int, error) {
	return f.hard, 0, nil
}

func secretsRouter(t *testing.T, cfgFn func(*server.Config)) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := t61BaseCfg(logger)
	cfg.Secrets.Store = &fakeSecretStore{}
	cfg.Secrets.EnvLocker = &recordingLocker{}
	cfgFn(&cfg)
	return server.NewRouter(cfg)
}

func postAuthed(h http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRouter_SecretSetWritesADomainAuditRow(t *testing.T) {
	audit := &recordingAuditLogger{}
	h := secretsRouter(t, func(c *server.Config) { c.Secrets.AuditLogger = audit })

	rec := postAuthed(h, "/api/v1/secrets?project=proj-x",
		`{"name":"DB_URL","value":"postgres://x","environment":"production","app":"api"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(audit.actions) != 1 || audit.actions[0] != "SECRET_CREATED" {
		t.Fatalf("want one SECRET_CREATED audit row, got %v", audit.actions)
	}
}

func TestRouter_SecretBulkIsGatedByThePlanLimit(t *testing.T) {
	limiter := enforcement.NewPlanLimiter(fixedLimitsStore{plan: "starter", hard: 1}, slog.Default())
	h := secretsRouter(t, func(c *server.Config) { c.Guards.PlanLimiter = limiter })

	rec := postAuthed(h, "/api/v1/secrets/bulk",
		`{"project":"proj-x","secrets":[{"name":"A","value":"1","envs":["production"]},{"name":"B","value":"2","envs":["production"]}]}`)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("two new secrets in an environment capped at one: want 402, got %d body=%s", rec.Code, rec.Body.String())
	}
}

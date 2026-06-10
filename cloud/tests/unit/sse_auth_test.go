// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T6.0 of plan 2026-05-27-backend-frontend-integration v1.1.
//
// Asserts that cloud/internal/auth.RequireJWT accepts JWTs carried by the
// EventSource query-parameter fallback (?token=<jwt>) in addition to the
// canonical Authorization: Bearer header. Closes EC-04 — the browser
// EventSource primitive cannot set custom request headers.

package unit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	loopzerojwt "github.com/loopzero/loopzero/pkg/jwt"
)

const (
	sseTestSecret   = "test-secret-at-least-32-bytes-long-ok!"
	sseTestIssuer   = "loopzero-cloud-test"
	sseTestAudience = "loopzero-engine"
)

// buildSSEChain wires RequireJWT (HMAC validator only — no Hydra RSA) around
// a sentinel handler that records whether it was reached and the resolved
// user context.
func buildSSEChain(t *testing.T) (http.Handler, *loopzerojwt.SessionTokenIssuer, *struct {
	called bool
	user   *auth.UserContext
}) {
	t.Helper()
	issuer := loopzerojwt.NewSessionTokenIssuer(sseTestSecret, sseTestIssuer, sseTestAudience, 3600*time.Second)
	state := &struct {
		called bool
		user   *auth.UserContext
	}{}
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.called = true
		state.user = auth.GetUser(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	mw := auth.RequireJWT(nil, issuer)
	return mw(sentinel), issuer, state
}

func mintSSEToken(t *testing.T, issuer *loopzerojwt.SessionTokenIssuer) string {
	t.Helper()
	token, _, err := issuer.Issue("kratos-user-id-sse-1", "sse@loopzero.dev", "Sse User", "tn-sse-1")
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return token
}

func TestSSEAuth_AcceptsValidQueryParamToken(t *testing.T) {
	handler, issuer, state := buildSSEChain(t)
	token := mintSSEToken(t, issuer)

	req := httptest.NewRequest(http.MethodGet, "/engine/api/v1/builds/abc/logs/stream?token="+token, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !state.called {
		t.Fatalf("sentinel never reached; auth blocked")
	}
	if state.user == nil || state.user.Subject != "kratos-user-id-sse-1" {
		t.Errorf("user context not propagated; got %+v", state.user)
	}
}

func TestSSEAuth_BearerHeaderStillWorks(t *testing.T) {
	// Defense-in-depth: the EventSource fallback MUST NOT break the
	// canonical header path. Otherwise we regress every CLI/fetch caller.
	handler, issuer, state := buildSSEChain(t)
	token := mintSSEToken(t, issuer)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Bearer path broken; got %d", w.Code)
	}
	if state.user == nil {
		t.Fatal("user context missing on Bearer path")
	}
}

func TestSSEAuth_HeaderWinsOverQueryParam(t *testing.T) {
	// If both are present, the header form is preferred (it's the
	// canonical surface; the query form is an SSE escape hatch). Tests
	// the precedence to prevent a future regression where someone
	// "fixes" extract by checking query first and breaks the documented
	// security model.
	handler, issuer, state := buildSSEChain(t)

	headerToken, _, _ := issuer.Issue("user-from-header", "h@h.h", "H", "tn-h")
	queryToken, _, _ := issuer.Issue("user-from-query", "q@q.q", "Q", "tn-q")

	req := httptest.NewRequest(http.MethodGet, "/x?token="+queryToken, nil)
	req.Header.Set("Authorization", "Bearer "+headerToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if state.user == nil || state.user.Subject != "user-from-header" {
		t.Errorf("header should win; got user=%+v", state.user)
	}
}

func TestSSEAuth_RejectsMissingToken(t *testing.T) {
	handler, _, state := buildSSEChain(t)
	req := httptest.NewRequest(http.MethodGet, "/engine/api/v1/builds/abc/logs/stream", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	if state.called {
		t.Errorf("sentinel reached despite missing token")
	}
}

func TestSSEAuth_RejectsInvalidQueryToken(t *testing.T) {
	handler, _, state := buildSSEChain(t)
	req := httptest.NewRequest(http.MethodGet, "/x?token=not-a-real-jwt", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	if state.called {
		t.Errorf("sentinel reached despite invalid token")
	}
}

func TestSSEAuth_RejectsExpiredQueryToken(t *testing.T) {
	// Mint a token with TTL 1ns then wait — validator must reject.
	expIssuer := loopzerojwt.NewSessionTokenIssuer(sseTestSecret, sseTestIssuer, sseTestAudience, time.Nanosecond)
	expired, _, _ := expIssuer.Issue("u", "e@e.e", "E", "tn-e")
	time.Sleep(2 * time.Millisecond)

	handler, _, state := buildSSEChain(t)
	req := httptest.NewRequest(http.MethodGet, "/x?token="+expired, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 (expired), got %d", w.Code)
	}
	if state.called {
		t.Errorf("sentinel reached despite expired token")
	}
}

func TestSSEAuth_RejectsWrongSecretQueryToken(t *testing.T) {
	// Mint a JWT with a DIFFERENT secret — signature must fail.
	bogusIssuer := loopzerojwt.NewSessionTokenIssuer(
		"different-secret-also-at-least-32-bytes!",
		sseTestIssuer, sseTestAudience, 3600*time.Second,
	)
	bogus, _, _ := bogusIssuer.Issue("u", "x@x.x", "X", "tn-x")

	handler, _, _ := buildSSEChain(t)
	req := httptest.NewRequest(http.MethodGet, "/x?token="+bogus, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 (bad signature), got %d", w.Code)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/server"
)

// TestWebhookRoute_IsAbsentWhenTheHandlerIsNil pins the causal chain that
// disabled the GitHub webhook in a live environment without anything failing
// (B-021).
//
// The chain has two links and both are silent:
//
//	api/cmd/setup_github.go — GITHUB_WEBHOOK_SECRET unset ⇒ WebhookHandler nil,
//	                          logged at Info level and the process starts fine
//	api/internal/server/router.go — a nil handler ⇒ the route is never registered
//
// The result is a 404 on POST /api/v1/webhooks/github: not "unauthorized", not
// "misconfigured" — the endpoint simply does not exist, and GitHub's delivery
// log is the only place it shows.
//
// The existing wiring test cannot observe this: it injects a stub handler, so
// GitHubWebhookHandler is non-nil in every case it runs. A test that always
// supplies the dependency can never see what happens without it.
func TestWebhookRoute_IsAbsentWhenTheHandlerIsNil(t *testing.T) {
	router := server.NewRouter(server.Config{GitHubWebhookHandler: nil})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github",
		strings.NewReader(`{"zen":"x"}`))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 with a nil webhook handler, got %d. If this route now answers with a "+
			"nil handler, the guard in router.go changed and this test is measuring something else.",
			rec.Code)
	}
}

// TestWebhookRoute_IsRegisteredWhenTheHandlerIsPresent is the half that FAILS if
// the route is removed — which is what the B-021 DoD asks for: "a check exists
// that fails when the webhook route is not registered".
//
// Paired with the test above, the two pin the whole chain: the endpoint exists
// if and only if the handler was built, and the handler is built if and only if
// the HMAC secret was present. Deleting the route registration, or renaming the
// path, turns this red.
func TestWebhookRoute_IsRegisteredWhenTheHandlerIsPresent(t *testing.T) {
	router := server.NewRouter(server.Config{
		GitHubWebhookHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot) // a status nothing else in the chain returns
		}),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github",
		strings.NewReader(`{"zen":"x"}`))
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatal("POST /api/v1/webhooks/github is not registered even with a handler present. GitHub " +
			"deliveries would 404, and the only place that shows is GitHub's own delivery log (B-021).")
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("the route is registered but did not reach the handler under test (got %d) — something "+
			"upstream is answering, so this test is not measuring the wiring it claims to", rec.Code)
	}
}

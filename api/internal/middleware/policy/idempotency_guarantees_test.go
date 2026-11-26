// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package policy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func idempotentPost(path, key string, body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", key)
	return req
}

// A body over the hashing cap must be refused, never forwarded cut short: the
// handler would otherwise act on a truncated request.
func TestIdempotency_OversizedBodyIsRefusedNotTruncated(t *testing.T) {
	called := false
	h := Idempotency(newInMemoryIdempotencyStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, idempotentPost("/api/v1/builds", "k-big", bytes.Repeat([]byte("x"), 10<<20+1)))

	if rec.Code != http.StatusRequestEntityTooLarge || called {
		t.Fatalf("want 413 before the handler, got %d (handler called: %v)", rec.Code, called)
	}
}

// A server failure is not the answer to the request: caching it would replay
// the failure to the very retry the client is told to make.
func TestIdempotency_ServerErrorIsNotCachedForReplay(t *testing.T) {
	store := newInMemoryIdempotencyStore()
	attempts := 0
	h := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	h.ServeHTTP(httptest.NewRecorder(), idempotentPost("/api/v1/builds", "k-retry", []byte(`{}`)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, idempotentPost("/api/v1/builds", "k-retry", []byte(`{}`)))

	if rec.Code != http.StatusAccepted || attempts != 2 {
		t.Fatalf("the retry after a 503 must run: got %d after %d attempts", rec.Code, attempts)
	}
}

// A key is bound to the endpoint it was first used on: reusing it on another
// endpoint with the same body must not return the first endpoint's answer.
func TestIdempotency_KeyReusedOnAnotherEndpointIsRefused(t *testing.T) {
	store := newInMemoryIdempotencyStore()
	h := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"from":"` + r.URL.Path + `"}`))
	}))

	h.ServeHTTP(httptest.NewRecorder(), idempotentPost("/api/v1/domains", "k-shared", []byte(`{}`)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, idempotentPost("/api/v1/builds", "k-shared", []byte(`{}`)))

	if rec.Code != http.StatusUnprocessableEntity || strings.Contains(rec.Body.String(), "/api/v1/domains") {
		t.Fatalf("want 422 without the other endpoint's body, got %d %s", rec.Code, rec.Body.String())
	}
}

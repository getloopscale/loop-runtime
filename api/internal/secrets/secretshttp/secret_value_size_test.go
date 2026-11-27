// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secretshttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// storeCallCounter records whether the store was reached.
type storeCallCounter struct {
	backendDownStore
	sets int
}

func (s *storeCallCounter) Set(ctx context.Context, a, b, c, d, e string, v []byte) (bool, error) {
	s.sets++
	return s.backendDownStore.Set(ctx, a, b, c, d, e, v)
}

func postRawBulk(t *testing.T, h *SecretHandler, body string) (int, secretBulkResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk?project=demo", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Bulk(w, req)
	var resp secretBulkResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v, body: %s", err, w.Body.String())
	}
	return w.Code, resp
}

// A value above the maximum is refused by the API itself, per key, before the
// secret store is called. On staging it reached the store and came back as a
// backend outage (#123).
func TestSecretBulk_OversizedValueIsRefusedBeforeTheStore(t *testing.T) {
	store := &storeCallCounter{}
	h := NewSecretHandler(store, &lockCountingFake{})
	big := strings.Repeat("x", secretValueMaxBytes+1)

	_, resp := postRawBulk(t, h, `{"secrets":[{"name":"TOO_BIG","value":"`+big+`","envs":["production"]}]}`)

	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(resp.Results))
	}
	got := resp.Results[0]
	if got.Status != "failed" || got.ErrorCode != "SECRET_VALUE_TOO_LARGE" {
		t.Fatalf("status=%q error_code=%q, want failed / SECRET_VALUE_TOO_LARGE", got.Status, got.ErrorCode)
	}
	if !strings.Contains(got.Error, "64 KiB") {
		t.Errorf("error %q does not name the limit", got.Error)
	}
	if store.sets != 0 {
		t.Errorf("the store was called %d time(s) for a value the API refuses", store.sets)
	}
}

func TestSecretBulk_ValueAtTheMaximumIsAccepted(t *testing.T) {
	store := &storeCallCounter{}
	h := NewSecretHandler(store, &lockCountingFake{})
	atLimit := strings.Repeat("x", secretValueMaxBytes)

	_, resp := postRawBulk(t, h, `{"secrets":[{"name":"AT_LIMIT","value":"`+atLimit+`","envs":["production"]}]}`)

	if resp.Results[0].ErrorCode == "SECRET_VALUE_TOO_LARGE" {
		t.Fatal("a value of exactly the maximum was refused")
	}
	if store.sets != 1 {
		t.Errorf("store calls = %d, want 1", store.sets)
	}
}

// The per-key error a client shows carries the public message only: no package
// prefixes, no path of the secret store, no tenant key name (#123).
func TestSecretBulk_StoreFailureMessageHidesTheInternalChain(t *testing.T) {
	h := NewSecretHandler(&backendDownStore{}, &lockCountingFake{})

	_, resp := postRawBulk(t, h, `{"secrets":[{"name":"SMALL","value":"v","envs":["production"]}]}`)

	got := resp.Results[0].Error
	for _, internal := range []string{"secret_store:", "vault", "transit", "[SECRETS_BACKEND_UNAVAILABLE]"} {
		if strings.Contains(got, internal) {
			t.Errorf("error %q exposes %q", got, internal)
		}
	}
	if !strings.Contains(got, "secrets backend is not available") {
		t.Errorf("error %q lost the public message", got)
	}
}

func TestSecretSet_OversizedValueAnswers413(t *testing.T) {
	store := &storeCallCounter{}
	h := NewSecretHandler(store, &lockCountingFake{})
	big := strings.Repeat("x", secretValueMaxBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets?project=demo",
		strings.NewReader(`{"name":"TOO_BIG","value":"`+big+`","environment":"production","app":"api"}`))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Set(w, req)

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v, body: %s", err, w.Body.String())
	}
	if w.Code != http.StatusRequestEntityTooLarge || resp["code"] != "SECRET_VALUE_TOO_LARGE" {
		t.Fatalf("status=%d code=%q, want 413 / SECRET_VALUE_TOO_LARGE", w.Code, resp["code"])
	}
	if store.sets != 0 {
		t.Errorf("the store was called %d time(s)", store.sets)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// RED test for issue #138: `loopzero secret set` answers
// `{"code":"INTERNAL_ERROR","error":"Something went wrong..."}` and nothing
// persists, while `loopzero secret list` works.
//
// The reported symptom is a 500. The defect this test locks is narrower and
// more durable: the 500 is UNTYPED. `PostgresSecretStore.Set` encrypts before
// writing (`encryptForStore` → `vault transit encrypt`), and when the Transit
// backend is unreachable — sealed, token expired, ESO not provisioned for a new
// namespace — the failure reaches the user as the same generic
// "Something went wrong" that a database outage produces.
//
// That is why the issue author had to GUESS at the cause. Their note reads
// "isso sugere que o backend de secrets (OpenBao Transit / ESO) não está  // english-only: verbatim quote of the issue #138 report
// provisionado" ("this suggests the secrets backend is not provisioned") — a hypothesis, because the API told them nothing. An operator
// should not have to infer the failing subsystem from which project happens to
// work.
//
// Two rival explanations were eliminated by measurement before writing this:
//   - `secrets.id` is `TEXT` (000001_initial_schema.up.sql:191) — no length
//     ceiling, so the long project name is not a PK overflow.
//   - `secrets.project_id` carries no FOREIGN KEY — so the known
//     name-vs-UUID trap (CLAUDE.md § Common Mistakes) cannot fire here.
//
// Whatever makes Transit unavailable in a given environment, the API's job is
// to say WHICH subsystem refused. Rule 8: fail fast, fail clear, typed.
package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// backendDownStore fails Set exactly the way the store layer does when the
// Transit backend rejects the encrypt — same wrapping, same sentinel.
type backendDownStore struct {
	SecretStore
}

func (s *backendDownStore) Set(_ context.Context, _, _, _, _, _ string, _ []byte) (bool, error) {
	return false, fmt.Errorf("secret_store: encrypt: %w",
		fmt.Errorf("secret_store: vault transit encrypt: %w", loopzeroerrors.ErrSecretsBackendUnavailable))
}

func (s *backendDownStore) ListNames(_ context.Context, _, _, _ string) ([]SecretMetadata, error) {
	return []SecretMetadata{}, nil
}

// TestSecretSet_BackendUnavailableIsTyped is the #138 RED.
//
// A secrets backend that cannot encrypt must NOT look like a generic server
// fault. It has a distinct cause, a distinct owner and a distinct remedy.
func TestSecretSet_BackendUnavailableIsTyped(t *testing.T) {
	h := NewSecretHandler(&backendDownStore{}, &lockCountingFake{})

	body := `{"name":"QA_DB_URL","value":"postgres://qa:test@db.example.com/qa","environment":"production","app":"api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets?project=loopzerokit-example",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Set(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v — body: %s", err, w.Body.String())
	}
	code, _ := resp["code"].(string)

	if code == "INTERNAL_ERROR" {
		t.Errorf("secrets-backend failure surfaced as INTERNAL_ERROR (issue #138).\n"+
			"    status=%d body=%s\n\n"+
			"    A Transit encrypt that cannot reach its backend is indistinguishable from a database\n"+
			"    outage or any other 5xx. The issue author had to GUESS the cause — their report says\n"+
			"    \"isso sugere que o backend de secrets ... não está provisionado\", a hypothesis, because\n"+ // english-only: verbatim quote of the issue #138 report
			"    the API said nothing. Emit a typed code naming the subsystem that refused.",
			w.Code, w.Body.String())
	}
	if code != "SECRETS_BACKEND_UNAVAILABLE" {
		t.Errorf("code = %q, want SECRETS_BACKEND_UNAVAILABLE", code)
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 — the request was VALID and the dependency was down;\n"+
			"    500 would blame the caller's input for an infrastructure fault", w.Code)
	}
}

// TestSecretSet_BackendErrorMessageIsActionableAndClean checks the two
// properties that make the message usable: it must name the subsystem in
// product language, and it must NOT leak internal terms (Rule 3) — "OpenBao",
// "Vault", "Transit" and "ESO" are ours, not the user's.
func TestSecretSet_BackendErrorMessageIsActionableAndClean(t *testing.T) {
	h := NewSecretHandler(&backendDownStore{}, &lockCountingFake{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets?project=loopzerokit-example",
		strings.NewReader(`{"name":"K","value":"v","environment":"production","app":"api"}`))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Set(w, req)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	msg, _ := resp["error"].(string)
	if msg == "" {
		msg, _ = resp["message"].(string)
	}

	for _, banned := range []string{"OpenBao", "Vault", "vault", "Transit", "transit", "ESO", "ClusterSecretStore"} {
		if strings.Contains(msg, banned) {
			t.Errorf("message leaks internal term %q (Rule 3): %q", banned, msg)
		}
	}
	if strings.Contains(strings.ToLower(msg), "something went wrong") {
		t.Errorf("message is still the generic placeholder: %q — the whole point of the typed error\n"+
			"    is that the operator learns WHICH subsystem refused", msg)
	}
	if msg == "" {
		t.Errorf("no message at all — a typed code with no explanation is barely better than the 500")
	}
}

// TestSecretSet_UnrelatedErrorsStayInternal proves the mapping is narrow. If
// every store failure became SECRETS_BACKEND_UNAVAILABLE, the new code would be
// as uninformative as the 500 it replaces — just with a more confident name.
func TestSecretSet_UnrelatedErrorsStayInternal(t *testing.T) {
	h := NewSecretHandler(&genericFailStore{}, &lockCountingFake{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets?project=p",
		strings.NewReader(`{"name":"K","value":"v","environment":"production","app":"api"}`))
	req.Header.Set("Content-Type", "application/json")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Set(w, req)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if code, _ := resp["code"].(string); code == "SECRETS_BACKEND_UNAVAILABLE" {
		t.Errorf("a plain database error was reported as SECRETS_BACKEND_UNAVAILABLE — the mapping\n" +
			"    must key on the sentinel, not on \"the store returned an error\"")
	}
}

type genericFailStore struct{ SecretStore }

func (s *genericFailStore) Set(_ context.Context, _, _, _, _, _ string, _ []byte) (bool, error) {
	return false, fmt.Errorf("secret_store: set: connection refused")
}

func (s *genericFailStore) ListNames(_ context.Context, _, _, _ string) ([]SecretMetadata, error) {
	return []SecretMetadata{}, nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// stubOpenBaoKVServer accepts POST /v1/kv/data/* with token authentication
// and records writes. GET /v1/kv/data/loopruntime/dev/api/secrets/github returns
// 200 if the path was written earlier.
type openBaoStubState struct {
	mu     sync.Mutex
	writes map[string]map[string]string
	token  string
}

func newOpenBaoStubServer(t *testing.T, token string) (*httptest.Server, *openBaoStubState) {
	t.Helper()
	state := &openBaoStubState{
		writes: map[string]map[string]string{},
		token:  token,
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != state.token {
			http.Error(w, "unauth", http.StatusForbidden)
			return
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		switch r.Method {
		case http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			var body struct {
				Data map[string]string `json:"data"`
			}
			_ = json.Unmarshal(b, &body)
			state.writes[r.URL.Path] = body.Data
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if _, ok := state.writes[r.URL.Path]; ok {
				w.WriteHeader(http.StatusOK)
			} else {
				http.NotFound(w, r)
			}
		}
	}))
	return server, state
}

func writeSecretsSeedCreds(t *testing.T, unsealDir string) (credsPath, unsealPath string) {
	t.Helper()
	credsPath = filepath.Join(unsealDir, "dev-credentials.json")
	credsContent := `{
		"github":{"app_id":"3319955","app_private_key":"pk","webhook_secret":"whsec_gh"},
		"stripe":{"api_key":"rk_test_abc","webhook_secret":"whsec_stripe"},
		"resend":{"api_key":"re_abc"},
		"openbao":{"vault_role_id":"role-uuid-1234567890abcdef"},
		"cloud":{"service_account_key":"sa-key-x"}
	}`
	if err := os.WriteFile(credsPath, []byte(credsContent), 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	unsealPath = filepath.Join(unsealDir, "unseal.json")
	if err := os.WriteFile(unsealPath, []byte(`{"root_token":"s.stub-root"}`), 0o600); err != nil {
		t.Fatalf("write unseal: %v", err)
	}
	return
}

func TestOpenBaoKVPut_Writes(t *testing.T) {
	server, state := newOpenBaoStubServer(t, "s.tok")
	defer server.Close()
	// FIX-3 (M24): no http.DefaultTransport mutation. openBaoKVPut carries its
	// own #nosec-annotated InsecureSkipVerify transport, so the global mutation
	// was both unnecessary AND leaked insecure TLS into every later test that
	// used the default client. Removed — test isolation restored.

	err := openBaoKVPut(context.Background(), server.URL, "s.tok",
		"secret/data/loopruntime/dev/api/secrets/github",
		map[string]string{"app_id": "3319955"})

	if err != nil {
		t.Fatalf("write: %v", err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	got := state.writes["/v1/secret/data/loopruntime/dev/api/secrets/github"]
	if got["app_id"] != "3319955" {
		t.Errorf("write missing app_id: %v", got)
	}
}

func TestOpenBaoKVPut_ForbiddenIsError(t *testing.T) {
	server, _ := newOpenBaoStubServer(t, "s.correct")
	defer server.Close()

	err := openBaoKVPut(context.Background(), server.URL, "s.wrong",
		"secret/data/x", map[string]string{"k": "v"})

	if err == nil {
		t.Fatal("want error on 403")
	}
}

func TestSecretsSeedDev_WritesAllPopulatedPaths(t *testing.T) {
	var host Host
	server, state := newOpenBaoStubServer(t, "s.stub-root")
	defer server.Close()

	dir := t.TempDir()
	credsPath, unsealPath := writeSecretsSeedCreds(t, dir)

	host.Getenv = func(k string) string {
		if k == vaultUnsealFileEnv {
			return unsealPath
		}
		return ""
	}

	creds, err := LoadCreds(credsPath)
	if err != nil {
		t.Fatalf("LoadCreds: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Host: host,
		Ctx:             context.Background(),
		Kube:            NewKubeClient(c),
		Creds:           creds,
		PortForwardCtor: pfCtorPointingAtTLS(server),
	}

	got := NewSecretsSeedDevSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, path := range []string{
		"/v1/secret/data/loopruntime/dev/api/secrets/github",
		"/v1/secret/data/loopruntime/dev/cloud/secrets/stripe",
		"/v1/secret/data/loopruntime/dev/cloud/secrets/stripe-webhook",
		"/v1/secret/data/loopruntime/dev/cloud/secrets/resend",
		"/v1/secret/data/loopruntime/dev/platform/config/openbao",
		"/v1/secret/data/loopruntime/dev/cloud/secrets/service-account",
	} {
		if _, ok := state.writes[path]; !ok {
			t.Errorf("missing write for %s", path)
		}
	}
}

func TestSecretsSeedDev_SkipWhenCredsAbsent(t *testing.T) {
	deps := Deps{Ctx: context.Background(), Creds: nil}
	if got := NewSecretsSeedDevSeed().Run(deps); got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}

func TestSecretsSeedDev_SkipsMissingFields(t *testing.T) {
	var host Host
	server, state := newOpenBaoStubServer(t, "s.root")
	defer server.Close()
	dir := t.TempDir()
	// Only github populated.
	credsPath := filepath.Join(dir, "dc.json")
	_ = os.WriteFile(credsPath, []byte(`{"github":{"app_id":"x","app_private_key":"y","webhook_secret":"z"}}`), 0o600)
	unsealPath := filepath.Join(dir, "u.json")
	_ = os.WriteFile(unsealPath, []byte(`{"root_token":"s.root"}`), 0o600)

	host.Getenv = func(k string) string {
		if k == vaultUnsealFileEnv {
			return unsealPath
		}
		return ""
	}

	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Host: host,
		Ctx:             context.Background(),
		Kube:            NewKubeClient(c),
		Creds:           creds,
		PortForwardCtor: pfCtorPointingAtTLS(server),
	}
	NewSecretsSeedDevSeed().Run(deps)

	state.mu.Lock()
	defer state.mu.Unlock()
	// Only github written; the other paths should be absent.
	if _, ok := state.writes["/v1/secret/data/loopruntime/dev/api/secrets/github"]; !ok {
		t.Error("github write missing")
	}
	if _, ok := state.writes["/v1/secret/data/loopruntime/dev/cloud/secrets/stripe"]; ok {
		t.Error("stripe write present but api_key empty (should skip)")
	}
}

func TestSecretsSeedDev_Name(t *testing.T) {
	if got := NewSecretsSeedDevSeed().Name(); got != "secrets-seed-dev" {
		t.Errorf("Name = %q", got)
	}
}

// TestSecretsSeedDev_FailsLoudOnPartialWrite (FIX-1, M24): when a subset of the
// canonical writes fails, Run MUST return FAIL with every failed path named in a
// joined error — never PASS (partial-success masking would hide missing security
// secrets). Blueprint ADR-M24-1 + error-handling.md fail-loud.
func TestSecretsSeedDev_FailsLoudOnPartialWrite(t *testing.T) {
	var host Host
	// stub that 500s any path containing "stripe", 200 for the rest.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "s.stub-root" {
			http.Error(w, "unauth", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "stripe") {
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	credsPath, unsealPath := writeSecretsSeedCreds(t, dir)
	host.Getenv = func(k string) string {
		if k == vaultUnsealFileEnv {
			return unsealPath
		}
		return ""
	}
	creds, err := LoadCreds(credsPath)
	if err != nil {
		t.Fatalf("LoadCreds: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Host: host,
		Ctx:             context.Background(),
		Kube:            NewKubeClient(c),
		Creds:           creds,
		PortForwardCtor: pfCtorPointingAtTLS(server),
	}

	got := NewSecretsSeedDevSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL on partial write", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "stripe") {
		t.Errorf("Reason must name the failed path 'stripe', got: %s", got.Reason)
	}
}

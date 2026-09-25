// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// stubVaultRoleIDServer returns a TLS server that hosts the AppRole read
// endpoint. Only requests with the canonical X-Vault-Token header succeed.
func stubVaultRoleIDServer(t *testing.T, roleID, wantToken string) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != wantToken {
			http.Error(w, "unauth", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/v1/auth/approle/role/loopzero-api/role-id" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{"role_id": roleID},
		})
	}))
}

func writeUnsealFile(t *testing.T, dir, rootToken string) string {
	t.Helper()
	path := filepath.Join(dir, "cleanroom-unseal.json")
	content := `{"root_token":"` + rootToken + `"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write unseal: %v", err)
	}
	return path
}

func TestOpenBaoReadRoleID_Success(t *testing.T) {
	server := stubVaultRoleIDServer(t, "canonical-role-id-1234567890abcdef", "s.roottoken")
	defer server.Close()
	// Use InsecureSkipVerify for the test client since httptest.TLS emits self-signed.
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	oldTransport := http.DefaultTransport
	http.DefaultTransport = tr
	defer func() { http.DefaultTransport = oldTransport }()

	got, err := openBaoReadRoleID(context.Background(), server.URL, "s.roottoken")

	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "canonical-role-id-1234567890abcdef" {
		t.Errorf("role_id = %q, want canonical…", got)
	}
}

func TestOpenBaoReadRoleID_WrongTokenFails(t *testing.T) {
	server := stubVaultRoleIDServer(t, "x", "correct-token")
	defer server.Close()

	_, err := openBaoReadRoleID(context.Background(), server.URL, "wrong-token")

	if err == nil {
		t.Fatal("want error on 403, got nil")
	}
}

func TestReadVaultUnsealRootToken_ReadsFromEnvOverridePath(t *testing.T) {
	dir := t.TempDir()
	path := writeUnsealFile(t, dir, "s.envtoken")
	origGetenv := osGetenv
	defer func() { osGetenv = origGetenv }()
	osGetenv = func(k string) string {
		if k == vaultUnsealFileEnv {
			return path
		}
		return ""
	}

	got, err := readVaultUnsealRootToken()

	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "s.envtoken" {
		t.Errorf("root_token = %q, want s.envtoken", got)
	}
}

func TestReadVaultUnsealRootToken_FailWhenFileMissing(t *testing.T) {
	origGetenv := osGetenv
	defer func() { osGetenv = origGetenv }()
	osGetenv = func(k string) string {
		if k == vaultUnsealFileEnv {
			return "/nonexistent/file.json"
		}
		return ""
	}

	_, err := readVaultUnsealRootToken()

	if err == nil {
		t.Fatal("want error on missing file")
	}
}

func TestReadVaultUnsealRootToken_FailWhenRootTokenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unseal.json")
	if err := os.WriteFile(path, []byte(`{"root_token":""}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	origGetenv := osGetenv
	defer func() { osGetenv = origGetenv }()
	osGetenv = func(k string) string {
		if k == vaultUnsealFileEnv {
			return path
		}
		return ""
	}

	_, err := readVaultUnsealRootToken()

	if err == nil {
		t.Fatal("want error on empty root_token")
	}
}

// pfCtorPointingAtTLS returns a Deps.PortForwardCtor that points at an
// httptest.NewTLSServer URL. Callers pass "https:" scheme URLs to the
// caller — this factory ignores the scheme and just returns the port.
func pfCtorPointingAtTLS(server *httptest.Server) func(context.Context, TunnelTarget) (*Tunnel, error) {
	return func(context.Context, TunnelTarget) (*Tunnel, error) {
		u, _ := url.Parse(server.URL)
		port, _ := strconv.Atoi(u.Port())
		return &Tunnel{LocalPort: port, Close: func() {}}, nil
	}
}

func TestVaultRoleIDSync_HappyPath(t *testing.T) {
	server := stubVaultRoleIDServer(t, "canonical-role-id-1234567890abcdef", "s.roottoken")
	defer server.Close()
	dir := t.TempDir()
	unsealPath := writeUnsealFile(t, dir, "s.roottoken")

	origGetenv := osGetenv
	defer func() { osGetenv = origGetenv }()
	osGetenv = func(k string) string {
		if k == vaultUnsealFileEnv {
			return unsealPath
		}
		return ""
	}

	// mgmt cluster has loopzero-api Deployment.
	mgmt := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(makeLoopZeroAPIDeploymentWithEnv(nil)).Build()
	deps := Deps{
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		Kube:                 NewKubeClient(mgmt),
		CtrlClient:           mgmt,
		PortForwardCtor:      pfCtorPointingAtTLS(server),
	}

	got := NewVaultRoleIDSyncSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	value, _ := getDeploymentContainerEnv(context.Background(), mgmt,
		"loopzero-system", "loopzero-api", "OPENBAO_VAULT_ROLE_ID")
	if value != "canonical-role-id-1234567890abcdef" {
		t.Errorf("OPENBAO_VAULT_ROLE_ID = %q", value)
	}
}

func TestVaultRoleIDSync_Name(t *testing.T) {
	if got := NewVaultRoleIDSyncSeed().Name(); got != "vault-role-id-sync" {
		t.Errorf("Name = %q, want vault-role-id-sync", got)
	}
}

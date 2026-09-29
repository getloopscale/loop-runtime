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
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
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
	var host Host
	dir := t.TempDir()
	path := writeUnsealFile(t, dir, "s.envtoken")
	host.Getenv = func(k string) string {
		if k == vaultUnsealFileEnv {
			return path
		}
		return ""
	}

	got, err := readVaultUnsealRootToken(host)

	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "s.envtoken" {
		t.Errorf("root_token = %q, want s.envtoken", got)
	}
}

func TestReadVaultUnsealRootToken_FailWhenFileMissing(t *testing.T) {
	var host Host
	host.Getenv = func(k string) string {
		if k == vaultUnsealFileEnv {
			return "/nonexistent/file.json"
		}
		return ""
	}

	_, err := readVaultUnsealRootToken(host)

	if err == nil {
		t.Fatal("want error on missing file")
	}
}

func TestReadVaultUnsealRootToken_FailWhenRootTokenEmpty(t *testing.T) {
	var host Host
	dir := t.TempDir()
	path := filepath.Join(dir, "unseal.json")
	if err := os.WriteFile(path, []byte(`{"root_token":""}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	host.Getenv = func(k string) string {
		if k == vaultUnsealFileEnv {
			return path
		}
		return ""
	}

	_, err := readVaultUnsealRootToken(host)

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

func TestVaultRoleIDSync_Name(t *testing.T) {
	if got := NewVaultRoleIDSyncSeed().Name(); got != "vault-role-id-sync" {
		t.Errorf("Name = %q, want vault-role-id-sync", got)
	}
}

// roleIDSyncDeps builds the dependencies for a Run against a fake core cluster
// holding the loopzero-api Deployment plus the given objects, with OpenBao served by
// a TLS stub that issues roleID.
func roleIDSyncDeps(t *testing.T, roleID string, objs ...ctrlclient.Object) (Deps, ctrlclient.Client) {
	t.Helper()
	server := stubVaultRoleIDServer(t, roleID, "s.roottoken")
	t.Cleanup(server.Close)
	unsealPath := writeUnsealFile(t, t.TempDir(), "s.roottoken")
	mgmt := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(append([]ctrlclient.Object{makeLoopZeroAPIDeploymentWithEnv(nil)}, objs...)...).Build()
	return Deps{
		Host: Host{Getenv: func(k string) string {
			if k == vaultUnsealFileEnv {
				return unsealPath
			}
			return ""
		}},
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		Kube:                 NewKubeClient(mgmt),
		CtrlClient:           mgmt,
		PortForwardCtor:      pfCtorPointingAtTLS(server),
	}, mgmt
}

func approleSecret(data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: "loopzero-openbao-approle"},
		Data:       data,
	}
}

// The role id is secret material the seed legitimately owns; the Deployment spec is
// not — helmfile renders it, and an env var the seed adds there overrides every
// envFrom source and survives the next sync (principle_violations#67).
func TestVaultRoleIDSync_WritesTheRoleIDIntoTheAppRoleSecret(t *testing.T) {
	const roleID = "fresh-role-id-1234567890abcdef0123"
	deps, mgmt := roleIDSyncDeps(t, roleID,
		approleSecret(map[string][]byte{"OPENBAO_VAULT_SECRET_ID": []byte("secret-id-from-bootstrap")}))

	got := NewVaultRoleIDSyncSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	sec, err := deps.Kube.GetSecret(deps.Ctx, "loopzero-system", "loopzero-openbao-approle")
	if err != nil {
		t.Fatalf("get the AppRole Secret: %v", err)
	}
	if string(sec.Data["OPENBAO_VAULT_ROLE_ID"]) != roleID {
		t.Errorf("Secret OPENBAO_VAULT_ROLE_ID = %q, want %q", sec.Data["OPENBAO_VAULT_ROLE_ID"], roleID)
	}
	if string(sec.Data["OPENBAO_VAULT_SECRET_ID"]) != "secret-id-from-bootstrap" {
		t.Errorf("the secret_id vault-bootstrap-dev.sh wrote was lost: %q", sec.Data["OPENBAO_VAULT_SECRET_ID"])
	}
	env, _ := getDeploymentEnvValue(t, mgmt, "OPENBAO_VAULT_ROLE_ID")
	if env {
		t.Error("the seed set OPENBAO_VAULT_ROLE_ID on the loopzero-api Deployment spec, which helmfile renders")
	}
}

// Without the Secret, vault-bootstrap-dev.sh has not run: there is no secret_id for
// the role id to pair with, and inventing the Secret would hide that.
func TestVaultRoleIDSync_FailsWhenTheAppRoleSecretIsAbsent(t *testing.T) {
	deps, _ := roleIDSyncDeps(t, "fresh-role-id-1234567890abcdef0123")

	got := NewVaultRoleIDSyncSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "vault-bootstrap-dev.sh") {
		t.Errorf("Reason = %q, want it to name vault-bootstrap-dev.sh, the step that creates the Secret", got.Reason)
	}
}

func TestVaultRoleIDSync_VerifyReadsTheAppRoleSecret(t *testing.T) {
	deps, _ := roleIDSyncDeps(t, "unused",
		approleSecret(map[string][]byte{"OPENBAO_VAULT_ROLE_ID": []byte("fresh-role-id-1234567890abcdef0123")}))

	got := NewVaultRoleIDSyncSeed().Verify(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

// getDeploymentEnvValue reports whether the loopzero-api container sets name in env.
func getDeploymentEnvValue(t *testing.T, cli ctrlclient.Client, name string) (bool, string) {
	t.Helper()
	var d appsv1.Deployment
	if err := cli.Get(context.Background(), ctrlclient.ObjectKey{Namespace: "loopzero-system", Name: "loopzero-api"}, &d); err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	for _, e := range d.Spec.Template.Spec.Containers[0].Env {
		if e.Name == name {
			return true, e.Value
		}
	}
	return false, ""
}

// The setup wires OpenBao before the dev loop deploys loopzero-api, and the vault bootstrap
// issues a NEW role id every time it runs — so the sync has to run right after it, when
// no loopzero-api Deployment exists yet. Nothing running means no stale value to replace:
// the first container start reads the Secret the seed just wrote (#405).
func TestVaultRoleIDSync_WritesTheRoleIDWhenLoopZeroAPIIsNotDeployedYet(t *testing.T) {
	const roleID = "fresh-role-id-1234567890abcdef0123"
	deps, _ := roleIDSyncDeps(t, roleID,
		approleSecret(map[string][]byte{"OPENBAO_VAULT_SECRET_ID": []byte("secret-id-from-bootstrap")}))
	if err := deps.CtrlClient.Delete(deps.Ctx, makeLoopZeroAPIDeploymentWithEnv(nil)); err != nil {
		t.Fatalf("remove the loopzero-api Deployment: %v", err)
	}

	got := NewVaultRoleIDSyncSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	sec, err := deps.Kube.GetSecret(deps.Ctx, "loopzero-system", "loopzero-openbao-approle")
	if err != nil {
		t.Fatalf("get the AppRole Secret: %v", err)
	}
	if string(sec.Data["OPENBAO_VAULT_ROLE_ID"]) != roleID {
		t.Errorf("Secret OPENBAO_VAULT_ROLE_ID = %q, want %q", sec.Data["OPENBAO_VAULT_ROLE_ID"], roleID)
	}
}

// On an environment whose loopzero-api Helmfile installs (dev-public), the seed runs while
// the API cannot be Ready yet: its database roles and schema come later in `task
// setup`. The seed's job is that every new pod reads the role id — a pod reads its
// envFrom at start, whether or not it then passes readiness. Waiting for Ready made the
// seed fail setup at `k3d:secrets:wire` on a cold dev-public install (#444).
func TestVaultRoleIDSync_PassesWhenTheRestartedPodsExistButAreNotReadyYet(t *testing.T) {
	deps, mgmt := roleIDSyncDeps(t, "fresh-role-id-1234567890abcdef0123",
		approleSecret(map[string][]byte{"OPENBAO_VAULT_SECRET_ID": []byte("secret-id-from-bootstrap")}))
	var d appsv1.Deployment
	if err := mgmt.Get(deps.Ctx, ctrlclient.ObjectKey{Namespace: "loopzero-system", Name: "loopzero-api"}, &d); err != nil {
		t.Fatalf("get loopzero-api: %v", err)
	}
	d.Status.ReadyReplicas = 0
	d.Status.UpdatedReplicas = 1
	if err := mgmt.Status().Update(deps.Ctx, &d); err != nil {
		t.Fatalf("mark loopzero-api not ready: %v", err)
	}
	var marked appsv1.Deployment
	if err := mgmt.Get(deps.Ctx, ctrlclient.ObjectKey{Namespace: "loopzero-system", Name: "loopzero-api"}, &marked); err != nil || marked.Status.ReadyReplicas != 0 {
		t.Fatalf("the fixture did not take: ready=%d err=%v — the test would measure nothing", marked.Status.ReadyReplicas, err)
	}

	got := NewVaultRoleIDSyncSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS — the restarted pods exist and read the role id at start", got.Verdict, got.Reason)
	}
}

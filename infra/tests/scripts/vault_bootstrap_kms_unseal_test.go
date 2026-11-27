// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

// vault-bootstrap-dev.sh under a cloud KMS auto-unseal (AUTO_UNSEAL_MODE=awskms, the
// staging and production seal): the vault is initialised with recovery keys, the root
// token lands in the init file, and nothing tries a Shamir unseal, which a vault with
// an auto-unseal seal refuses. The script runs for real against a fake kubectl that
// plays OpenBao.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fakeKubectl = `#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
case "$*" in
  *"bao status -format=json"*) echo '{"initialized": false, "sealed": true}' ;;
  *"bao operator init"*) echo '{"root_token": "root-from-init", "recovery_keys_b64": ["k1", "k2", "k3", "k4", "k5"]}' ;;
esac
exit 0
`

func runBootstrapWithFakeVault(t *testing.T, env ...string) (calls string, initFile string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"kubectl":  fakeKubectl,
		"helmfile": "#!/usr/bin/env bash\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(dir, "calls.log")
	initFile = filepath.Join(dir, "init.json")
	cmd := exec.Command("bash", scriptPath(t, "vault-bootstrap-dev.sh"))
	cmd.Dir = filepath.Join("..", "..", "..")
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"FAKE_LOG="+log,
		"UNSEAL_FILE="+initFile,
		// One cluster: the cross-cluster TokenReview step is not under test.
		"OPENBAO_CONTEXT=vault",
		"TARGET_CONTEXT=vault",
	)
	cmd.Env = append(cmd.Env, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vault-bootstrap-dev.sh failed: %v\n%s", err, out)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), initFile
}

func TestAKMSAutoUnsealedVaultIsInitialisedWithRecoveryKeys(t *testing.T) {
	calls, initFile := runBootstrapWithFakeVault(t, "AUTO_UNSEAL_MODE=awskms")

	if !strings.Contains(calls, "bao operator init -recovery-shares=5 -recovery-threshold=3 -format=json") {
		t.Errorf("the vault was not initialised with recovery keys; kubectl calls:\n%s", calls)
	}
	if strings.Contains(calls, "operator unseal") {
		t.Errorf("an auto-unsealed vault was sent a Shamir unseal; kubectl calls:\n%s", calls)
	}
	b, err := os.ReadFile(initFile)
	if err != nil {
		t.Fatalf("no init file: %v", err)
	}
	var init struct {
		RootToken string `json:"root_token"`
	}
	if err := json.Unmarshal(b, &init); err != nil || init.RootToken != "root-from-init" {
		t.Errorf("the init file does not carry the root token (%v): %s", err, b)
	}
	if fi, err := os.Stat(initFile); err == nil && fi.Mode().Perm() != 0o600 {
		t.Errorf("init file mode = %o, want 600: it holds the root token", fi.Mode().Perm())
	}
	if !strings.Contains(calls, "create secret generic openbao-init-token") {
		t.Errorf("the root token never reached openbao-init-token, which the openbao-init job reads; kubectl calls:\n%s", calls)
	}
}

func TestAShamirVaultIsStillInitialisedWithUnsealShares(t *testing.T) {
	calls, _ := runBootstrapWithFakeVault(t)

	if !strings.Contains(calls, "bao operator init -key-shares=5 -key-threshold=3 -format=json") {
		t.Errorf("the default Shamir vault was not initialised with key shares; kubectl calls:\n%s", calls)
	}
}

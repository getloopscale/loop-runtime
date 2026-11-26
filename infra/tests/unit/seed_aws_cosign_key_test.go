// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// infra/helmfile/scripts/seed-aws-cosign-key.sh runs against a fake aws CLI, which keeps the
// one secret it serves in a file, and a fake cosign, which writes a recognisable key pair.

const fakePrivateKey = "-----BEGIN ENCRYPTED SIGSTORE PRIVATE KEY-----\nfake-private\n-----END ENCRYPTED SIGSTORE PRIVATE KEY-----"

type cosignSeed struct {
	dir    string
	store  string // the secret's SecretString; absent file = the secret does not exist
	writes string // one line per aws call that writes
}

func newCosignSeed(t *testing.T) *cosignSeed {
	t.Helper()
	dir := t.TempDir()
	s := &cosignSeed{dir: dir, store: filepath.Join(dir, "secret.json"), writes: filepath.Join(dir, "writes.log")}
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// aws secretsmanager get-secret-value | create-secret | put-secret-value, with
	// --secret-string file://<path> for the writes.
	writeExec(t, filepath.Join(bin, "aws"), `#!/usr/bin/env bash
op="$2"; value=""
while [[ $# -gt 0 ]]; do
  if [[ $1 == --secret-string ]]; then value="${2#file://}"; fi
  shift
done
case "$op" in
  get-secret-value)
    [[ -f `+s.store+` ]] || { echo "An error occurred (ResourceNotFoundException)" >&2; exit 254; }
    cat `+s.store+` ;;
  create-secret|put-secret-value)
    cp "$value" `+s.store+`; echo "$op" >>`+s.writes+` ;;
  *) echo "unexpected aws call: $op" >&2; exit 1 ;;
esac
`)
	// cosign generate-key-pair: cosign.key and cosign.pub in the working directory,
	// encrypted with $COSIGN_PASSWORD.
	writeExec(t, filepath.Join(bin, "cosign"), `#!/usr/bin/env bash
[[ "$1" == generate-key-pair && -n "$COSIGN_PASSWORD" ]] || { echo "bad cosign call: $*" >&2; exit 1; }
printf '%s\n' "`+fakePrivateKey+`" >cosign.key
printf '%s\n' "-----BEGIN PUBLIC KEY-----" "fake-public" "-----END PUBLIC KEY-----" >cosign.pub
`)
	return s
}

func (s *cosignSeed) run(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(repoRoot(), "infra", "helmfile", "scripts", "seed-aws-cosign-key.sh"),
		"loopruntime/staging/cosign-signing-key")
	cmd.Env = append(os.Environ(), "PATH="+filepath.Join(s.dir, "bin")+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seed-aws-cosign-key.sh: %v\n%s", err, out)
	}
	return string(out)
}

func (s *cosignSeed) secret(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(s.store)
	if err != nil {
		t.Fatalf("the secret was not written: %v", err)
	}
	var doc map[string]string
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the secret is not a JSON object: %v\n%s", err, data)
	}
	return doc
}

func (s *cosignSeed) writeCount(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(s.writes)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Fields(string(data)))
}

// The build signs with the key in Secret argo/cosign-signing-key, which External Secrets
// fills from this secret; on AWS nothing created it, so no build could sign (#53).
func TestTheCosignSeedCreatesTheSigningKeyWhenTheSecretIsMissing(t *testing.T) {
	s := newCosignSeed(t)
	out := s.run(t)

	doc := s.secret(t)
	if !strings.Contains(doc["cosign.key"], "BEGIN ENCRYPTED SIGSTORE PRIVATE KEY") {
		t.Errorf("cosign.key = %q, want the generated private key", doc["cosign.key"])
	}
	if !strings.Contains(doc["cosign.pub"], "BEGIN PUBLIC KEY") {
		t.Errorf("cosign.pub = %q, want the generated public key", doc["cosign.pub"])
	}
	if len(doc["cosign.password"]) < 32 {
		t.Errorf("cosign.password has %d characters, want at least 32", len(doc["cosign.password"]))
	}
	if strings.Contains(out, "fake-private") || strings.Contains(out, doc["cosign.password"]) {
		t.Errorf("the output carries the key or its password:\n%s", out)
	}
}

// A key that exists signs every image built so far; replacing it would orphan them.
func TestTheCosignSeedKeepsAnExistingSigningKey(t *testing.T) {
	s := newCosignSeed(t)
	existing := `{"cosign.key":"` + strings.ReplaceAll(fakePrivateKey, "\n", `\n`) + `","cosign.pub":"kept","cosign.password":"kept-password-kept-password-kept"}`
	if err := os.WriteFile(s.store, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	s.run(t)

	if n := s.writeCount(t); n != 0 {
		t.Errorf("%d writes to an existing key, want 0", n)
	}
	if got := s.secret(t)["cosign.pub"]; got != "kept" {
		t.Errorf("cosign.pub = %q, want the existing kept", got)
	}
}

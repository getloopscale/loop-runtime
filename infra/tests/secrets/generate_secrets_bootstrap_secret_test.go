// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secrets

import (
	"os"
	"strings"
	"testing"
)

// secretLiterals runs generate-secrets.sh (only kubectl is faked, see
// generate_secrets_db_roles_test.go) and returns the literals written into one Secret.
func secretLiterals(t *testing.T, secretName string) map[string]string {
	t.Helper()
	record := t.TempDir() + "/secrets.tsv"
	run := runScript(t, "../../helmfile/scripts/generate-secrets.sh", dbRolesProfile,
		[]string{secretRecordEnv + "=" + record}, "standalone", "core")
	if run.code != 0 {
		t.Fatalf("generate-secrets.sh failed:\n%s", run)
	}
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the script created no Secret at all: %v\n%s", err, run)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		name, literal, _ := strings.Cut(line, "\t")
		if name == secretName {
			key, value, _ := strings.Cut(literal, "=")
			got[key] = value
		}
	}
	return got
}

// Without this Secret loop-runtime-api fell back to its built-in development bootstrap secret, so
// /internal/bootstrap was guarded only by the network. The producer contract claimed a
// Secret named loop-runtime-bootstrap-secret that nothing ever created (#453). The value lands in
// loop-runtime-api-internal, the optional envFrom source the chart already reads.
func TestGenerateSecretsGivesTheAPIARandomBootstrapSecret(t *testing.T) {
	got := secretLiterals(t, "loop-runtime-api-internal")
	v := got["LOOP_RUNTIME_BOOTSTRAP_SECRET"]
	if len(v) < 32 {
		t.Fatalf("loop-runtime-api-internal LOOP_RUNTIME_BOOTSTRAP_SECRET is missing or short (len %d)", len(v))
	}
	// The binary's built-in default (api/internal/config: devDefaultBootstrapSecret).
	if v == "loop-runtime-dev-bootstrap-secret" {
		t.Errorf("the bootstrap secret is the development default, not a random value")
	}
}

// Two installs never share a bootstrap secret.
func TestGenerateSecretsBootstrapSecretIsNotFixed(t *testing.T) {
	a := secretLiterals(t, "loop-runtime-api-internal")["LOOP_RUNTIME_BOOTSTRAP_SECRET"]
	b := secretLiterals(t, "loop-runtime-api-internal")["LOOP_RUNTIME_BOOTSTRAP_SECRET"]
	if a == "" || a == b {
		t.Errorf("two cold installs produced the same bootstrap secret")
	}
}

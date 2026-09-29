// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secrets

import (
	"os"
	"strings"
	"testing"
)

// dataPlaneLiterals runs generate-secrets.sh for one environment and returns every
// Secret it wrote, as name → key → value.
func dataPlaneLiterals(t *testing.T, env string, extraEnv ...string) map[string]map[string]string {
	t.Helper()
	record := t.TempDir() + "/secrets.tsv"
	run := runScript(t, "../../helmfile/scripts/generate-secrets.sh", dbRolesProfile,
		append([]string{secretRecordEnv + "=" + record}, extraEnv...), env, "core")
	if run.code != 0 {
		t.Fatalf("generate-secrets.sh failed:\n%s", run)
	}
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the script created no Secret at all: %v\n%s", err, run)
	}
	got := map[string]map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		name, literal, _ := strings.Cut(line, "\t")
		key, value, _ := strings.Cut(literal, "=")
		if got[name] == nil {
			got[name] = map[string]string{}
		}
		got[name][key] = value
	}
	return got
}

var dataPlaneSecrets = []string{"loopzero-memory", "loopzero-rag", "loopzero-lens"}

// dev-public's data plane runs from Helmfile (#446); each service reads its database
// password, its admin bootstrap token and the model provider key from one Secret named
// after its release.
func TestGenerateSecretsGivesEachDataPlaneServiceItsSecret(t *testing.T) {
	got := dataPlaneLiterals(t, "dev-public", "LOOPZERO_OPENAI_API_KEY=sk-test", "LOOPZERO_DATA_PLANE_ADMIN_BOOTSTRAP_TOKEN=dp-boot")
	for _, name := range dataPlaneSecrets {
		s := got[name]
		if len(s["postgres-password"]) < 32 {
			t.Errorf("%s: postgres-password missing or short (len %d)", name, len(s["postgres-password"]))
		}
		if s["openai-api-key"] != "sk-test" {
			t.Errorf("%s: openai-api-key is not the operator's key", name)
		}
		if s["admin-bootstrap-token"] != "dp-boot" {
			t.Errorf("%s: admin-bootstrap-token is not the operator's token", name)
		}
	}
}

// Without the operator's token each service still gets one, random — never empty,
// which would leave the admin surface without a credential to check against.
func TestGenerateSecretsGeneratesADataPlaneBootstrapTokenWhenNoneIsGiven(t *testing.T) {
	got := dataPlaneLiterals(t, "dev-public", "LOOPZERO_OPENAI_API_KEY=sk-test")
	for _, name := range dataPlaneSecrets {
		if v := got[name]["admin-bootstrap-token"]; len(v) < 32 {
			t.Errorf("%s: admin-bootstrap-token missing or short (len %d)", name, len(v))
		}
	}
}

// Only dev-public runs the data plane from this repository; no other environment gets
// its namespace or Secrets.
func TestGenerateSecretsLeavesTheDataPlaneOutOfOtherEnvironments(t *testing.T) {
	got := dataPlaneLiterals(t, "standalone")
	for _, name := range dataPlaneSecrets {
		if _, ok := got[name]; ok {
			t.Errorf("standalone got the %s Secret", name)
		}
	}
}

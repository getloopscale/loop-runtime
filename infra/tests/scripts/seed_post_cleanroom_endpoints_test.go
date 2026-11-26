// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// endpointsHarness runs the script's real ensure_seed_endpoints and secret_value against a
// fake kubectl (Secrets only) and a stub port_forward that records what it would open.
func endpointsHarness(t *testing.T, preset map[string]string) map[string]string {
	t.Helper()
	body := readScript(t, "seed-post-cleanroom-all.sh")
	var fns []string
	for _, name := range []string{"secret_value", "ensure_seed_endpoints"} {
		m := regexp.MustCompile(`(?s)\n` + name + `\(\) \{.*?\n\}`).FindString(body)
		if m == "" {
			t.Fatalf("%s function not found in script", name)
		}
		fns = append(fns, m)
	}
	bin := t.TempDir()
	fake := `#!/usr/bin/env bash
case "$*" in
  *"get secret loopruntime-db-credentials"*"password"*) printf 'ZGItcHc=' ;;          # db-pw
  *"get secret loopruntime-api-internal"*"LOOPRUNTIME_BOOTSTRAP_SECRET"*) printf 'YnMtdmFsdWU=' ;; # bs-value
  *) exit 1 ;;
esac`
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	harness := `set -uo pipefail
CORE_CONTEXT=k3d-loopruntime-core
log() { :; }
FORWARDS=""
port_forward() { FORWARDS="$FORWARDS $2=$3"; return 0; }
` + strings.Join(fns, "\n") + `
ensure_seed_endpoints
printf 'KRATOS_ADMIN_URL=%s\n' "${KRATOS_ADMIN_URL:-}"
printf 'LOOPRUNTIME_ADMIN_HYDRA_PG_URL=%s\n' "${LOOPRUNTIME_ADMIN_HYDRA_PG_URL:-}"
printf 'LOOPRUNTIME_API_URL=%s\n' "${LOOPRUNTIME_API_URL:-}"
printf 'LOOPRUNTIME_BOOTSTRAP_SECRET=%s\n' "${LOOPRUNTIME_BOOTSTRAP_SECRET:-}"
printf 'FORWARDS=%s\n' "$FORWARDS"`
	cmd := exec.Command("bash", "-c", harness)
	env := []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
	for k, v := range preset {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			got[k] = v
		}
	}
	return got
}

// The seeds read their endpoints from the environment, and on the 2026-09-29 cleanroom
// nothing set them: the admin seed skipped and the Hydra client seed could not reach
// Postgres (#453). The chain now opens what its seeds need and exports it.
func TestTheChainOpensTheEndpointsItsSeedsNeed(t *testing.T) {
	got := endpointsHarness(t, nil)
	if got["KRATOS_ADMIN_URL"] == "" || !strings.Contains(got["FORWARDS"], "ory-kratos-admin=") {
		t.Errorf("Kratos admin not forwarded/exported: %v", got)
	}
	if got["LOOPRUNTIME_ADMIN_HYDRA_PG_URL"] != "postgres://loopruntime:db-pw@127.0.0.1:15432/loopruntime?sslmode=disable" ||
		!strings.Contains(got["FORWARDS"], "postgresql=15432:5432") {
		t.Errorf("Postgres not forwarded with the real credentials: %v", got)
	}
	if got["LOOPRUNTIME_API_URL"] == "" {
		t.Error("LOOPRUNTIME_API_URL not exported")
	}
	if got["LOOPRUNTIME_BOOTSTRAP_SECRET"] != "bs-value" {
		t.Errorf("bootstrap secret not read from loopruntime-api-internal: %q", got["LOOPRUNTIME_BOOTSTRAP_SECRET"])
	}
}

// An operator who points the chain somewhere else is obeyed: nothing preset is
// overwritten and no port-forward is opened for it.
func TestTheChainKeepsEndpointsTheOperatorSet(t *testing.T) {
	got := endpointsHarness(t, map[string]string{
		"KRATOS_ADMIN_URL":               "http://kratos.example",
		"LOOPRUNTIME_ADMIN_HYDRA_PG_URL": "postgres://elsewhere",
		"LOOPRUNTIME_API_URL":            "http://api.example",
		"LOOPRUNTIME_BOOTSTRAP_SECRET":   "operator-value",
	})
	if got["KRATOS_ADMIN_URL"] != "http://kratos.example" || got["LOOPRUNTIME_ADMIN_HYDRA_PG_URL"] != "postgres://elsewhere" ||
		got["LOOPRUNTIME_API_URL"] != "http://api.example" || got["LOOPRUNTIME_BOOTSTRAP_SECRET"] != "operator-value" {
		t.Errorf("a preset endpoint was overwritten: %v", got)
	}
	if strings.TrimSpace(got["FORWARDS"]) != "" {
		t.Errorf("port-forwards opened for endpoints the operator set: %q", got["FORWARDS"])
	}
}

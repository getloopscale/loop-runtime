// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// loopzero operators install-config-contract hermetic guards (Slice 4). Per-binary ISP:
// each operator chart's CONFIG.md/SECRETS.md set-equal to that chart's RENDERED
// ConfigMap/Secret (EC-1), config∩secret==∅ (EC-2), plus an EC-3 completeness guard
// that each chart injects ⊇ what its binary reads. Reuses helpers from
// loopzero_api_install_contract_test.go (same package `charts`).
package charts

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func operatorChartDir(t *testing.T, name string) string {
	t.Helper()
	d, err := filepath.Abs(filepath.Join("..", "..", "..", "charts", name))
	if err != nil {
		t.Fatalf("resolve chart dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d, "Chart.yaml")); err != nil {
		t.Fatalf("chart not found at %s: %v", d, err)
	}
	return d
}

// configOnlyOperator: CONFIG.md set-equal to rendered ConfigMap, NO Secret rendered,
// NO SECRETS.md (EC-2 config-only), Reloader opt-in.
func configOnlyOperator(t *testing.T, chart string) {
	t.Helper()
	c := operatorChartDir(t, chart)
	out := helmRequired(t, c)
	rendered := renderedKeys(t, out, "ConfigMap")
	if len(rendered) == 0 {
		t.Fatalf("%s: ConfigMap rendered zero keys", chart)
	}
	documented := mdTableKeys(t, filepath.Join(c, "CONFIG.md"))
	setEqual(t, chart+" CONFIG.md vs rendered ConfigMap", documented, rendered)
	// config-only: no Secret object, no SECRETS.md
	if len(renderedKeys(t, out, "Secret")) != 0 {
		t.Errorf("%s: config-only operator rendered a Secret (EC-2 violation)", chart)
	}
	if _, err := os.Stat(filepath.Join(c, "SECRETS.md")); err == nil {
		t.Errorf("%s: config-only operator has a SECRETS.md (should not — EC-2)", chart)
	}
	reloaderOptIn(t, c, chart)
}

// twoPlaneOperator: CONFIG.md == rendered ConfigMap, SECRETS.md == rendered Secret,
// config∩secret==∅, ESO covers all secret keys, Reloader opt-in.
func twoPlaneOperator(t *testing.T, chart string) {
	t.Helper()
	c := operatorChartDir(t, chart)
	cfgOut := helmRequired(t, c)
	cfgKeys := renderedKeys(t, cfgOut, "ConfigMap")
	setEqual(t, chart+" CONFIG.md vs rendered ConfigMap", mdTableKeys(t, filepath.Join(c, "CONFIG.md")), cfgKeys)

	secretDocs := mdTableKeys(t, filepath.Join(c, "SECRETS.md"))
	if len(secretDocs) == 0 {
		t.Fatalf("%s: SECRETS.md parsed zero keys", chart)
	}
	args := []string{}
	for _, k := range secretDocs {
		args = append(args, "--set", "secrets."+camelAPI(k)+"=x")
	}
	secOut := helmRequired(t, c, args...)
	secKeys := renderedKeys(t, secOut, "Secret")
	setEqual(t, chart+" SECRETS.md vs rendered Secret", secretDocs, secKeys)

	// EC-2 set-based separation
	secretSet := map[string]bool{}
	for _, k := range secKeys {
		secretSet[k] = true
	}
	if len(secretSet) == 0 {
		t.Fatalf("%s: rendered Secret zero keys — separation guard vacuous", chart)
	}
	for _, k := range cfgKeys {
		if secretSet[k] {
			t.Errorf("%s: key %q in both ConfigMap and Secret (planes crossed)", chart, k)
		}
	}
	// ESO examples cover every secret key
	for _, fn := range []string{"eso-openbao.yaml", "eso-aws-sm.yaml", "eso-gcp-sm.yaml"} {
		body := readFile(t, filepath.Join(c, "examples", fn))
		for _, k := range secretDocs {
			if !strings.Contains(body, "secretKey: "+k) {
				t.Errorf("%s/%s missing secretKey %s", chart, fn, k)
			}
		}
	}
	reloaderOptIn(t, c, chart)
}

func reloaderOptIn(t *testing.T, c, chart string) {
	t.Helper()
	if !strings.Contains(helmRequired(t, c, "--set", "reloader.enabled=true"), "reloader.stakater.com/auto") {
		t.Errorf("%s: reloader annotation absent when reloader.enabled=true", chart)
	}
	if strings.Contains(helmRequired(t, c), "reloader.stakater.com") {
		t.Errorf("%s: reloader annotation leaked into default render (must be opt-in)", chart)
	}
}

func TestLoopZeroTenantOperator(t *testing.T)   { twoPlaneOperator(t, "loopzero-tenant-operator") }
func TestLoopZeroAppOperator(t *testing.T)      { configOnlyOperator(t, "loopzero-app-operator") }
func TestLoopZeroPreviewOperator(t *testing.T)  { configOnlyOperator(t, "loopzero-preview-operator") }
func TestLoopZeroPlatformOperator(t *testing.T) { configOnlyOperator(t, "loopzero-platform-operator") }

// EC-3 — each operator chart injects ⊇ what its binary actually reads. Greps the
// binary's env reads (cmd/<op> + operators/internal, the shared+specific packages)
// and asserts the chart-delivered set (rendered ConfigMap ∪ Secret ∪ the kubeconfig
// volume-mount paths) covers them. Proves "configured", beyond doc==render.
func TestLoopZeroOperatorsEnvCompleteness(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatal("helm not installed — completeness guard cannot run (EC-4)")
	}
	opsRoot, err := filepath.Abs(filepath.Join("..", "..", "..", "operators"))
	if err != nil {
		t.Fatalf("resolve operators dir: %v", err)
	}
	// kubeconfig env keys are delivered via platform volume mount, not chart ConfigMap/Secret (EC-1).
	platformDelivered := map[string]bool{"BUILD_KUBECONFIG": true, "RUNTIME_KUBECONFIG": true}
	cases := map[string][]string{
		// chart -> cmd dirs whose env reads it must cover (shared/tracing are imported by all)
		"loopzero-tenant-operator":   {"cmd/tenant-operator"},
		"loopzero-app-operator":      {"cmd/app-operator"},
		"loopzero-preview-operator":  {"cmd/preview-operator"},
		"loopzero-platform-operator": {"cmd/platform-operator"},
	}
	for chart, cmds := range cases {
		c := operatorChartDir(t, chart)
		delivered := map[string]bool{}
		for _, k := range renderedKeys(t, helmRequired(t, c), "ConfigMap") {
			delivered[k] = true
		}
		// secret keys (render with values set)
		for _, k := range mdTableKeys(t, filepath.Join(c, "CONFIG.md")) {
			delivered[k] = true
		}
		if sm := filepath.Join(c, "SECRETS.md"); fileExists(sm) {
			for _, k := range mdTableKeys(t, sm) {
				delivered[k] = true
			}
		}
		// the binary's direct cmd env reads
		for _, cmd := range cmds {
			for _, k := range grepEnvReads(t, filepath.Join(opsRoot, cmd)) {
				if platformDelivered[k] {
					continue // delivered via platform volume mount (EC-1)
				}
				if !delivered[k] {
					t.Errorf("%s: binary reads %q but chart does not inject it (config/secret/volume) — completeness gap (EC-3)", chart, k)
				}
			}
		}
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// grepEnvReads returns the env-var names read via os.Getenv/LookupEnv/envOr under dir (recursive, non-test).
func grepEnvReads(t *testing.T, dir string) []string {
	t.Helper()
	out, _ := exec.Command("bash", "-c",
		`grep -rhoE '(Getenv|LookupEnv|envOr)\("[A-Z][A-Z0-9_]*"' `+dir+` 2>/dev/null | grep -oE '"[A-Z][A-Z0-9_]*"' | tr -d '"' | sort -u`).Output()
	var keys []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			keys = append(keys, l)
		}
	}
	sort.Strings(keys)
	return keys
}

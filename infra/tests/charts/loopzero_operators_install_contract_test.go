// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// loopzero operators install-config-contract hermetic guards (Slice 4). Per-binary ISP:
// each operator chart's CONFIG.md/SECRETS.md set-equal to that chart's RENDERED
// ConfigMap/Secret (EC-1), config∩secret==∅ (EC-2), plus an EC-3 completeness guard
// that each chart injects ⊇ what its binary reads. Reuses helpers from
// loopzero_api_install_contract_test.go (same package `charts`).
package charts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// operatorChartDir resolves a published chart that exists in charts/ only.
func operatorChartDir(t *testing.T, name string) string {
	t.Helper()
	return publishedChart(t, name)
}

// configOnlyOperator: CONFIG.md set-equal to rendered ConfigMap, NO Secret rendered,
// NO SECRETS.md (EC-2 config-only), Reloader opt-in.
func configOnlyOperator(t *testing.T, chart string) {
	t.Helper()
	c := operatorChartDir(t, chart)
	out := renderChart(t, c, defaultRelease, nil)
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
	cfgOut := renderChart(t, c, defaultRelease, nil)
	cfgKeys := renderedKeys(t, cfgOut, "ConfigMap")
	if len(cfgKeys) == 0 {
		t.Fatalf("%s: ConfigMap rendered zero keys — set-equality would pass vacuously", chart)
	}
	setEqual(t, chart+" CONFIG.md vs rendered ConfigMap", mdTableKeys(t, filepath.Join(c, "CONFIG.md")), cfgKeys)

	secretDocs := mdTableKeys(t, filepath.Join(c, "SECRETS.md"))
	if len(secretDocs) == 0 {
		t.Fatalf("%s: SECRETS.md parsed zero keys", chart)
	}
	secOut := renderChart(t, c, defaultRelease, allSecretsSet(secretDocs))
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
	if !strings.Contains(renderChart(t, c, defaultRelease, map[string]string{"reloader.enabled": "true"}), "reloader.stakater.com/auto") {
		t.Errorf("%s: reloader annotation absent when reloader.enabled=true", chart)
	}
	if strings.Contains(renderChart(t, c, defaultRelease, nil), "reloader.stakater.com") {
		t.Errorf("%s: reloader annotation leaked into default render (must be opt-in)", chart)
	}
}

func TestLoopZeroTenantOperator(t *testing.T)   { twoPlaneOperator(t, "loopzero-tenant-operator") }
func TestLoopZeroAppOperator(t *testing.T)      { configOnlyOperator(t, "loopzero-app-operator") }
func TestLoopZeroPreviewOperator(t *testing.T)  { configOnlyOperator(t, "loopzero-preview-operator") }
func TestLoopZeroPlatformOperator(t *testing.T) { configOnlyOperator(t, "loopzero-platform-operator") }

// EC-3 — each operator chart injects EXACTLY what its binary reads (bidirectional ISP):
//
//	(1) completeness: chart-delivered ⊇ binary-reads  (no MISSING key — under-injection)
//	(2) ISP no-bleed:  chart-delivered ⊆ binary-reads  (no EXTRA key — over-injection)
//
// The binary's env reads are the FULL transitive set: cmd/<op> + every operators/internal/*
// package reachable via `go list -deps` (NOT cmd-only — a key read inside internal/tenant,
// internal/httpclient, internal/tracing, etc. is part of the binary's contract). Kubeconfig
// paths are delivered via a platform volume mount, not the chart ConfigMap/Secret (EC-1), so
// they are exempt from both directions. Proves "configured" AND "no dead config", beyond doc==render.
func TestLoopZeroOperatorsEnvCompleteness(t *testing.T) {
	opsRoot := filepath.Join(repoRoot(t), "operators")
	// kubeconfig env keys are delivered via platform volume mount, not chart ConfigMap/Secret (EC-1).
	platformDelivered := map[string]bool{"BUILD_KUBECONFIG": true, "CELL_0_KUBECONFIG": true}
	cmds := map[string]string{
		"loopzero-tenant-operator":   "cmd/tenant-operator",
		"loopzero-app-operator":      "cmd/app-operator",
		"loopzero-preview-operator":  "cmd/preview-operator",
		"loopzero-platform-operator": "cmd/platform-operator",
	}
	for chart, cmdPath := range cmds {
		c := operatorChartDir(t, chart)
		// chart-delivered keys: rendered ConfigMap ∪ CONFIG.md ∪ SECRETS.md (when present).
		delivered := map[string]bool{}
		for _, k := range renderedKeys(t, renderChart(t, c, defaultRelease, nil), "ConfigMap") {
			delivered[k] = true
		}
		for _, k := range mdTableKeys(t, filepath.Join(c, "CONFIG.md")) {
			delivered[k] = true
		}
		if sm := filepath.Join(c, "SECRETS.md"); fileExists(sm) {
			for _, k := range mdTableKeys(t, sm) {
				delivered[k] = true
			}
		}
		// binary's FULL transitive env reads: cmd/<op> + every imported operators/internal/* package.
		reads := map[string]bool{}
		for _, d := range binaryReadDirs(t, opsRoot, cmdPath) {
			for _, k := range grepEnvReads(t, filepath.Join(opsRoot, d)) {
				reads[k] = true
			}
		}
		if len(reads) == 0 {
			t.Fatalf("%s: discovered zero env reads for %s — closure/path error (guard would be vacuous)", chart, cmdPath)
		}
		// (1) completeness — every key the binary reads is delivered (kubeconfig paths exempt).
		for k := range reads {
			if platformDelivered[k] {
				continue
			}
			if !delivered[k] {
				t.Errorf("%s: binary reads %q but chart does not inject it (under-injection — completeness gap, EC-3)", chart, k)
			}
		}
		// (2) ISP no-bleed — every key the chart delivers is actually read by the binary.
		for k := range delivered {
			if platformDelivered[k] {
				continue
			}
			if !reads[k] {
				t.Errorf("%s: chart injects %q but binary never reads it (over-injection — ISP goal #3 violation)", chart, k)
			}
		}
	}
}

// TestLoopZeroOperatorsSchemaFailFast — every operator schema must reject unknown TOP-LEVEL keys
// (root additionalProperties:false), matching the released loopzero-api reference (Slice 3). A typo'd
// top-level key (e.g. replicaCnt) must fail `helm install` validation, not pass silently.
func TestLoopZeroOperatorsSchemaFailFast(t *testing.T) {
	for _, chart := range []string{"loopzero-tenant-operator", "loopzero-app-operator", "loopzero-preview-operator", "loopzero-platform-operator"} {
		c := operatorChartDir(t, chart)
		var schema map[string]interface{}
		if err := json.Unmarshal([]byte(readFile(t, filepath.Join(c, "values.schema.json"))), &schema); err != nil {
			t.Fatalf("%s: values.schema.json parse: %v", chart, err)
		}
		if ap, ok := schema["additionalProperties"]; !ok || ap != false {
			t.Errorf("%s: root values.schema.json must set additionalProperties:false (fail-fast, matches loopzero-api) — got %v", chart, ap)
		}
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// binaryReadDirs returns, for a cmd/<op> entrypoint, the cmd dir plus every
// operators/internal/* package reachable in its import closure (via `go list -deps`),
// so env reads inside internal packages count toward the binary's contract.
func binaryReadDirs(t *testing.T, opsRoot, cmdPath string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "./"+cmdPath)
	cmd.Dir = opsRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps ./%s (in %s): %v", cmdPath, opsRoot, err)
	}
	const prefix = "github.com/getloopscale/loop-runtime/operators/"
	dirs := []string{cmdPath}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if rel := strings.TrimPrefix(line, prefix); rel != line && strings.HasPrefix(rel, "internal/") {
			dirs = append(dirs, rel)
		}
	}
	return dirs
}

// grepEnvReads returns the env-var names read under dir (recursive, production
// .go only — test files excluded so test-only env names don't widen the set).
// The readers are os.Getenv/LookupEnv, the envOr helper, and the tenant
// operator's loadConfig, which reads through an injected getenv and its
// required/orDefault closures so the config can be tested without the process
// environment, and operatorboot.PositiveInt/PositiveDuration, which take the
// getenv and the key and refuse a malformed value. A reader missing here makes every variable it reads look
// over-injected.
func grepEnvReads(t *testing.T, dir string) []string {
	t.Helper()
	out, _ := exec.Command("bash", "-c",
		`grep -rhoE --include='*.go' --exclude='*_test.go' '((Getenv|getenv|LookupEnv|envOr|required|orDefault)\(|Positive(Int|Duration)\([A-Za-z.]+, )"[A-Z][A-Z0-9_]*"' `+dir+` 2>/dev/null | grep -oE '"[A-Z][A-Z0-9_]*"' | tr -d '"' | sort -u`).Output()
	var keys []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			keys = append(keys, l)
		}
	}
	sort.Strings(keys)
	return keys
}

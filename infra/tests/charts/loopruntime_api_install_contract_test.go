// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// loopruntime-api install-config-contract hermetic guards (Slice 3). Homed as Go per
// the un-homed-logic gate (NOT bash). The two-plane invariant is executable:
// CONFIG.md set-equals the rendered ConfigMap keys; SECRETS.md set-equals the
// rendered Secret keys; the two planes never intersect. Set-equality is vs the
// RENDERED chart output (helm template), not a static template parse (EC-1).
package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// apiChartDir resolves the PUBLISHED loopruntime-api chart. The name exists in both
// chart trees, so it is reached by tree, never by bare name.
func apiChartDir(t *testing.T) string {
	t.Helper()
	return publishedChart(t, "loopruntime-api")
}

// allSecretsSet returns a --set map giving every documented secret a value, so
// the conditional secret templates emit each key.
func allSecretsSet(documented []string) map[string]string {
	set := make(map[string]string, len(documented))
	for _, k := range documented {
		set["secrets."+camelAPI(k)] = "x"
	}
	return set
}

// camelAPI mirrors the generator's ENV->camelCase value-name mapping.
func camelAPI(env string) string {
	parts := strings.Split(strings.ToLower(env), "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// renderedKeys decodes helm multi-doc output and returns the data/stringData
// keys of the first document matching kind.
func renderedKeys(t *testing.T, out, kind string) []string {
	t.Helper()
	dec := yaml.NewDecoder(strings.NewReader(out))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			break
		}
		if doc == nil || doc["kind"] != kind {
			continue
		}
		var data map[string]any
		if d, ok := doc["stringData"].(map[string]any); ok {
			data = d
		} else if d, ok := doc["data"].(map[string]any); ok {
			data = d
		}
		var keys []string
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}
	return nil
}

// mdTableKeys extracts first-column `KEY` cells from a markdown table.
func mdTableKeys(t *testing.T, path string) []string {
	t.Helper()
	body := readFile(t, path)
	re := regexp.MustCompile("(?m)^\\| `([A-Z][A-Z0-9_]+)` \\|")
	var keys []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		keys = append(keys, m[1])
	}
	sort.Strings(keys)
	return keys
}

func setEqual(t *testing.T, label string, want, got []string) {
	t.Helper()
	wm := map[string]bool{}
	gm := map[string]bool{}
	for _, k := range want {
		wm[k] = true
	}
	for _, k := range got {
		gm[k] = true
	}
	var missing, extra []string
	for k := range wm {
		if !gm[k] {
			missing = append(missing, k)
		}
	}
	for k := range gm {
		if !wm[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("%s NOT set-equal:\n  documented-but-not-rendered: %v\n  rendered-but-not-documented: %v", label, missing, extra)
	}
}

// T1.1 — CONFIG.md set-equals the rendered ConfigMap keys (EC-1: rendered, not template source).
func TestLoopRuntimeApiConfigManifest(t *testing.T) {
	c := apiChartDir(t)
	out := renderChart(t, c, defaultRelease, nil)
	rendered := renderedKeys(t, out, "ConfigMap")
	documented := mdTableKeys(t, filepath.Join(c, "CONFIG.md"))
	if len(rendered) == 0 {
		t.Fatal("ConfigMap rendered zero keys")
	}
	setEqual(t, "CONFIG.md vs rendered ConfigMap", documented, rendered)
	// EC-2: cross-plane check is SET-based (no config key is also a secret key),
	// NOT a name-pattern denylist — patterns false-positive on legit config like
	// CB_KEY (circuit-breaker key = "argocd", not a credential).
	secretSet := map[string]bool{}
	for _, k := range mdTableKeys(t, filepath.Join(c, "SECRETS.md")) {
		secretSet[k] = true
	}
	for _, k := range documented {
		if secretSet[k] {
			t.Errorf("CONFIG.md documents %q which is also a SECRETS.md key (plane collision)", k)
		}
	}
}

// T2.1 — SECRETS.md set-equals the rendered Secret keys (EC-1 + EC-5 bidirectional).
func TestLoopRuntimeApiSecretManifest(t *testing.T) {
	c := apiChartDir(t)
	documented := mdTableKeys(t, filepath.Join(c, "SECRETS.md"))
	if len(documented) == 0 {
		t.Fatal("SECRETS.md documents zero keys")
	}
	// Render with every documented secret value set so the conditional secret.yaml emits each.
	out := renderChart(t, c, defaultRelease, allSecretsSet(documented))
	rendered := renderedKeys(t, out, "Secret")
	setEqual(t, "SECRETS.md vs rendered Secret", documented, rendered)
	if !strings.Contains(readFile(t, filepath.Join(c, "SECRETS.md")), "openssl rand") {
		t.Error("SECRETS.md missing an `openssl rand` generation hint")
	}
}

// T3.2 — config/secret separation: rendered ConfigMap keys have EMPTY intersection
// with the SECRET key set (EC-2 set-based, not name-pattern).
func TestLoopRuntimeApiConfigSecretSeparation(t *testing.T) {
	c := apiChartDir(t)
	secretSet := map[string]bool{}
	for _, k := range mdTableKeys(t, filepath.Join(c, "SECRETS.md")) {
		secretSet[k] = true
	}
	if len(secretSet) == 0 {
		t.Fatal("SECRETS.md parsed zero keys — separation guard would pass vacuously (review F2)")
	}
	out := renderChart(t, c, defaultRelease, nil)
	for _, k := range renderedKeys(t, out, "ConfigMap") {
		if secretSet[k] {
			t.Errorf("secret key %q rendered into the ConfigMap (two planes crossed)", k)
		}
	}
}

// T3.2 — no committed values*.yaml inlines a secret value (+ scanner self-checks).
func TestLoopRuntimeApiNoInlineSecret(t *testing.T) {
	c := apiChartDir(t)
	files, _ := filepath.Glob(filepath.Join(c, "values*.yaml"))
	if len(files) == 0 {
		t.Fatal("no values*.yaml to scan")
	}
	for _, f := range files {
		if v := scanInlineSecret(readFile(t, f)); len(v) > 0 {
			t.Errorf("inline secret in %s: %v", filepath.Base(f), v)
		}
	}
	if len(scanInlineSecret("config:\n  foo: bar\nSTRIPE_API_KEY: sk_live_planted123\n")) == 0 {
		t.Error("scanner FAILED to flag a planted inline secret")
	}
	if len(scanInlineSecret("existingSecret: \"\"\nimage:\n  tag: \"!!changeme!!\"\n")) != 0 {
		t.Error("scanner false-positive on sentinel/empty")
	}
}

// T2.2 — the 3 ESO examples are valid YAML, cover every secret key, target the chart Secret.
func TestLoopRuntimeApiESOExamples(t *testing.T) {
	c := apiChartDir(t)
	want := mdTableKeys(t, filepath.Join(c, "SECRETS.md"))
	for _, fn := range []string{"eso-openbao.yaml", "eso-aws-sm.yaml", "eso-gcp-sm.yaml"} {
		body := readFile(t, filepath.Join(c, "examples", fn))
		dec := yaml.NewDecoder(strings.NewReader(body))
		for {
			var doc any
			if err := dec.Decode(&doc); err != nil {
				if err.Error() == "EOF" {
					break
				}
				t.Errorf("%s invalid YAML: %v", fn, err)
				break
			}
		}
		for _, tok := range []string{"kind: ClusterSecretStore", "kind: ExternalSecret", "name: loopruntime-api-secrets"} {
			if !strings.Contains(body, tok) {
				t.Errorf("%s missing %q", fn, tok)
			}
		}
		for _, k := range want {
			if !strings.Contains(body, "secretKey: "+k) {
				t.Errorf("%s missing secretKey %s", fn, k)
			}
		}
	}
}

// F3 (review) — fail-closed: secrets.create=false without existingSecret must NOT render
// a dangling secretRef; the template must fail loudly instead of producing an un-startable pod.
func TestLoopRuntimeApiSecretSourceFailClosed(t *testing.T) {
	c := apiChartDir(t)
	// Invalid combo → helm template MUST fail.
	out, err := renderChartE(t, c, defaultRelease, map[string]string{"secrets.create": "false"})
	if err == nil {
		t.Errorf("helm template with secrets.create=false + no existingSecret should FAIL (dangling secretRef), but succeeded:\n%s", out)
	}
	// Valid combo (existingSecret) → must succeed even with create=false.
	if _, err := renderChartE(t, c, defaultRelease, map[string]string{
		"secrets.create": "false", "secrets.existingSecret": "loopruntime-api-secrets",
	}); err != nil {
		t.Errorf("helm template with create=false + existingSecret should succeed: %v", err)
	}
}

// T3.1 — Reloader annotation present iff enabled (opt-in, back-compat).
func TestLoopRuntimeApiReloaderOptIn(t *testing.T) {
	c := apiChartDir(t)
	if !strings.Contains(renderChart(t, c, defaultRelease, map[string]string{"reloader.enabled": "true"}), "reloader.stakater.com/auto") {
		t.Error("reloader annotation absent when reloader.enabled=true")
	}
	if strings.Contains(renderChart(t, c, defaultRelease, nil), "reloader.stakater.com") {
		t.Error("reloader annotation leaked into default render (must be opt-in)")
	}
}

// T1.2 — values.example.yaml is sentinel-only and renders.
func TestLoopRuntimeApiValuesExample(t *testing.T) {
	c := apiChartDir(t)
	ex := readFile(t, filepath.Join(c, "values.example.yaml"))
	if regexp.MustCompile(`(?i)sk_(live|test)_[a-z0-9]|whsec_[a-z0-9]|\b[a-f0-9]{32,}\b`).MatchString(ex) {
		t.Error("values.example.yaml appears to contain a real secret value")
	}
	if !strings.Contains(ex, "existingSecret") {
		t.Error("values.example.yaml missing existingSecret")
	}
	if renderChart(t, c, defaultRelease, nil, filepath.Join(c, "values.example.yaml")) == "" {
		t.Error("values.example.yaml rendered empty")
	}
}

// T3.3 — README documents the two planes + links resolve.
func TestLoopRuntimeApiReadme(t *testing.T) {
	c := apiChartDir(t)
	readme := readFile(t, filepath.Join(c, "README.md"))
	for _, tok := range []string{"existingSecret", "values-", "CONFIG.md", "SECRETS.md", "examples/", "reloader.enabled"} {
		if !strings.Contains(readme, tok) {
			t.Errorf("README.md missing reference: %s", tok)
		}
	}
	for _, f := range []string{"CONFIG.md", "SECRETS.md", "values.example.yaml", "examples/eso-openbao.yaml"} {
		if _, err := os.Stat(filepath.Join(c, f)); err != nil {
			t.Errorf("README links a missing file: %s", f)
		}
	}
}

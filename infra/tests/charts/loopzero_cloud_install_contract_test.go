// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package charts holds hermetic Go contract tests for the published Helm charts.
// These replace the bash hermetic tests of install-config-contract Slice 1 + 2
// (loopzero-cloud-secret-manifest / eso-examples / reloader / no-inline-secret) — homed
// as Go per the operational-logic-capability-architecture (un-homed-logic gate): test
// logic belongs in Go, not new shell. helm-render assertions FAIL when helm is absent (one policy, owned by chartrender).
package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// cloudChartDir resolves the PUBLISHED loopzero-cloud chart. The name exists in both
// chart trees, so it is reached by tree, never by bare name.
func cloudChartDir(t *testing.T) string {
	t.Helper()
	return publishedChart(t, "loopzero-cloud")
}

// secretKeys are the Secret-typed env vars the cloud-API reads (config.go).
var secretKeys = []string{
	"STRIPE_API_KEY", "STRIPE_WEBHOOK_SECRET", "SESSION_TOKEN_SECRET",
	"LOOPZERO_SERVICE_AUTH_SECRET", "THEMORY_API_KEY",
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// Slice 1 T3.1 — SECRETS.md documents every secret key + values.example is sentinel-only + renders.
func TestLoopZeroCloudSecretManifest(t *testing.T) {
	c := cloudChartDir(t)
	secrets := readFile(t, filepath.Join(c, "SECRETS.md"))
	for _, k := range secretKeys {
		if !strings.Contains(secrets, k) {
			t.Errorf("SECRETS.md missing secret key %s", k)
		}
	}
	if !strings.Contains(secrets, "openssl rand -hex 32") {
		t.Error("SECRETS.md missing openssl generation command")
	}
	example := readFile(t, filepath.Join(c, "values.example.yaml"))
	if regexp.MustCompile(`(?i)sk_(live|test)_[a-z0-9]|whsec_[a-z0-9]|\b[a-f0-9]{32,}\b`).MatchString(example) {
		t.Error("values.example.yaml appears to contain a real secret value")
	}
	if !strings.Contains(example, "existingSecret") {
		t.Error("values.example.yaml missing existingSecret")
	}
	render := renderChart(t, c, defaultRelease, nil, filepath.Join(c, "values.example.yaml"))
	if render == "" {
		t.Error("values.example.yaml rendered empty")
	}
}

// Slice 1 T3.2 — the 3 ESO examples are valid YAML, target the existingSecret convention, cover all keys.
func TestLoopZeroCloudESOExamples(t *testing.T) {
	c := cloudChartDir(t)
	cases := map[string]string{
		"eso-openbao.yaml": "vault", "eso-aws-sm.yaml": "aws", "eso-gcp-sm.yaml": "gcpsm",
	}
	for file, provider := range cases {
		p := filepath.Join(c, "examples", file)
		body := readFile(t, p)
		// valid multi-doc YAML
		dec := yaml.NewDecoder(strings.NewReader(body))
		for {
			var doc any
			if err := dec.Decode(&doc); err != nil {
				if err.Error() == "EOF" {
					break
				}
				t.Errorf("%s invalid YAML: %v", file, err)
				break
			}
		}
		for _, want := range []string{"kind: ClusterSecretStore", "kind: ExternalSecret", "name: loopzero-cloud-secrets", provider + ":"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q", file, want)
			}
		}
		for _, k := range secretKeys {
			if !strings.Contains(body, "secretKey: "+k) {
				t.Errorf("%s missing secretKey %s", file, k)
			}
		}
	}
}

// Slice 2 T1.1+T2.1 — Reloader annotation present iff enabled + README documents the install surface.
func TestLoopZeroCloudReloaderOptIn(t *testing.T) {
	c := cloudChartDir(t)
	enabled := renderChart(t, c, defaultRelease, map[string]string{"reloader.enabled": "true"})
	if !strings.Contains(enabled, "reloader.stakater.com/auto") {
		t.Error("reloader annotation absent when reloader.enabled=true")
	}
	def := renderChart(t, c, defaultRelease, nil)
	if strings.Contains(def, "reloader.stakater.com") {
		t.Error("reloader annotation leaked into default render (must be opt-in)")
	}
	readme := readFile(t, filepath.Join(c, "README.md"))
	for _, tok := range []string{"existingSecret", "values.example.yaml", "SECRETS.md", "examples/", "reloader.enabled"} {
		if !strings.Contains(readme, tok) {
			t.Errorf("README.md missing reference: %s", tok)
		}
	}
	for _, f := range []string{"SECRETS.md", "values.example.yaml", "examples/eso-openbao.yaml"} {
		if _, err := os.Stat(filepath.Join(c, f)); err != nil {
			t.Errorf("README links a missing file: %s", f)
		}
	}
}

// scanInlineSecret returns violations: a secret key carrying a non-sentinel value, or a secret-shaped literal.
func scanInlineSecret(body string) []string {
	keyRe := regexp.MustCompile(`(` + strings.Join(secretKeys, "|") + `)\s*:\s*(\S.*)$`)
	litRe := regexp.MustCompile(`(?i)sk_(live|test)_[a-z0-9]|whsec_[a-z0-9]|\b[a-f0-9]{32,}\b`)
	sentinel := regexp.MustCompile(`(?i)!!changeme`)
	var v []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if m := keyRe.FindStringSubmatch(line); m != nil {
			val := strings.Trim(strings.TrimSpace(m[2]), `"'`)
			if val != "" && !sentinel.MatchString(val) {
				v = append(v, "inline "+m[1])
			}
		}
		if litRe.MatchString(line) && !sentinel.MatchString(line) {
			v = append(v, "secret-shaped literal")
		}
	}
	return v
}

// Slice 2 T3.1 — secrets-by-reference guard: no committed values file inlines a secret.
func TestLoopZeroCloudNoInlineSecret(t *testing.T) {
	c := cloudChartDir(t)
	files, _ := filepath.Glob(filepath.Join(c, "values*.yaml"))
	if len(files) == 0 {
		t.Fatal("no values*.yaml found to scan")
	}
	for _, f := range files {
		if v := scanInlineSecret(readFile(t, f)); len(v) > 0 {
			t.Errorf("inline secret in %s: %v", filepath.Base(f), v)
		}
	}
	// self-check: scanner MUST fire on a planted secret
	if len(scanInlineSecret("config:\n  foo: bar\nSTRIPE_API_KEY: sk_live_planted123\n")) == 0 {
		t.Error("scanner FAILED to flag a planted inline secret (guard is broken)")
	}
	// self-check: sentinels/empty MUST NOT trip
	if len(scanInlineSecret("existingSecret: \"\"\nimage:\n  tag: \"!!changeme-image-tag!!\"\n")) != 0 {
		t.Error("scanner false-positives on sentinel/empty")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func helmDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "helm")
}

// helmValues reads a chart's values file, tolerating the two canonical forms
// (`values.yaml` plain and `values.yaml.gotmpl` for helmfile-templated charts —
// ory-kratos/ory-hydra adopted `.gotmpl` for env-var interpolation per the
// Kratos email courier canonicalization work). Returns (bytes, actualPath, err).
// F27 (M23 audit 2026-07-10): unit tests that hard-coded `values.yaml` failed
// after the migration; this helper closes the gap by falling back transparently.
func helmValues(chart string) ([]byte, string, error) {
	dir := helmDir()
	primary := filepath.Join(dir, chart, "values.yaml")
	if data, err := os.ReadFile(primary); err == nil {
		return data, primary, nil
	}
	fallback := filepath.Join(dir, chart, "values.yaml.gotmpl")
	data, err := os.ReadFile(fallback)
	return data, fallback, err
}

var helmCharts = []string{
	"postgresql",
	"gitea",
	"minio",
	"argocd",
	"cert-manager",
	"argo-workflows",
	"traefik",
	"monitoring",
	"otel",
	"ory-kratos",
	"ory-hydra",
}

func TestHelmValuesFilesExist(t *testing.T) {
	dir := helmDir()
	for _, chart := range helmCharts {
		primary := filepath.Join(dir, chart, "values.yaml")
		fallback := filepath.Join(dir, chart, "values.yaml.gotmpl")
		// F27 (M23 audit 2026-07-10): accept both canonical forms — plain
		// values.yaml and helmfile-templated values.yaml.gotmpl (ory-kratos/
		// ory-hydra adopted .gotmpl for env-var interpolation).
		_, err1 := os.Stat(primary)
		_, err2 := os.Stat(fallback)
		if os.IsNotExist(err1) && os.IsNotExist(err2) {
			t.Errorf("values file missing (tried both %s and %s)", primary, fallback)
		}
	}
}

func TestHelmValuesNoLatestImageTag(t *testing.T) {
	for _, chart := range helmCharts {
		data, actualPath, err := helmValues(chart)
		if err != nil {
			t.Errorf("%s: cannot read values file: %v", chart, err)
			continue
		}
		_ = actualPath // path recorded for debugging when tests fail

		// F27 (M23 audit 2026-07-10): .gotmpl files may contain Go-template
		// directives that yaml.Unmarshal cannot parse. Fall back to substring
		// check for those (mechanical parity — same invariant, no yaml round-trip).
		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			if strings.Contains(string(data), "tag: latest") || strings.Contains(string(data), "tag: \"latest\"") {
				t.Errorf("%s: forbidden `latest` tag detected via substring scan (values.yaml.gotmpl)", chart)
			}
			continue
		}
		if containsLatestTag(raw) {
			t.Errorf("%s: values.yaml contains an image tag set to \"latest\"", chart)
		}

		// Second check: scan raw text for common patterns like `tag: latest` or `tag: "latest"`
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "tag:") {
				value := strings.TrimSpace(strings.TrimPrefix(trimmed, "tag:"))
				value = strings.Trim(value, `"'`)
				if value == "latest" {
					t.Errorf("%s: values.yaml line %d has tag set to \"latest\"", chart, i+1)
				}
			}
		}
	}
}

func TestHelmValuesHaveResourceLimits(t *testing.T) {
	// F27 (M23 audit 2026-07-10): use helmValues() to tolerate .gotmpl variant.
	for _, chart := range helmCharts {
		data, _, err := helmValues(chart)
		if err != nil {
			t.Errorf("%s: cannot read values file: %v", chart, err)
			continue
		}

		content := string(data)
		if !strings.Contains(content, "limits:") {
			t.Errorf("%s: values.yaml missing resource \"limits:\"", chart)
		}
		if !strings.Contains(content, "cpu:") {
			t.Errorf("%s: values.yaml missing \"cpu:\" in resource limits", chart)
		}
		if !strings.Contains(content, "memory:") {
			t.Errorf("%s: values.yaml missing \"memory:\" in resource limits", chart)
		}
	}
}

func TestHelmValuesRunAsNonRoot(t *testing.T) {
	// F27 (M23 audit 2026-07-10): use helmValues() to tolerate .gotmpl variant.
	for _, chart := range helmCharts {
		data, _, err := helmValues(chart)
		if err != nil {
			t.Errorf("%s: cannot read values file: %v", chart, err)
			continue
		}

		content := string(data)
		if !strings.Contains(content, "runAsNonRoot: true") {
			t.Errorf("%s: values.yaml missing \"runAsNonRoot: true\"", chart)
		}
	}
}

func TestHelmValuesHaveHealthProbes(t *testing.T) {
	dir := helmDir()
	// Charts that manage probes internally (not configurable via values.yaml)
	skipProbeCheck := map[string]bool{
		"cert-manager": true, // cert-manager v1.20+ manages probes internally
		"ory-kratos":   true, // Ory charts manage probes internally via chart defaults
		"ory-hydra":    true, // Ory charts manage probes internally via chart defaults
	}
	for _, chart := range helmCharts {
		if skipProbeCheck[chart] {
			continue
		}
		path := filepath.Join(dir, chart, "values.yaml")

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: cannot read values file: %v", chart, err)
			continue
		}

		content := string(data)
		hasLiveness := strings.Contains(content, "livenessProbe")
		hasReadiness := strings.Contains(content, "readinessProbe")
		if !hasLiveness {
			t.Errorf("%s: values.yaml missing livenessProbe configuration", chart)
		}
		if !hasReadiness {
			t.Errorf("%s: values.yaml missing readinessProbe configuration", chart)
		}
	}
}

// containsLatestTag recursively checks a parsed YAML tree for any key named
// "tag" with a string value of "latest".
func containsLatestTag(node interface{}) bool {
	switch v := node.(type) {
	case map[string]interface{}:
		for key, val := range v {
			if key == "tag" {
				if s, ok := val.(string); ok && s == "latest" {
					return true
				}
			}
			if containsLatestTag(val) {
				return true
			}
		}
	case []interface{}:
		for _, item := range v {
			if containsLatestTag(item) {
				return true
			}
		}
	}
	return false
}

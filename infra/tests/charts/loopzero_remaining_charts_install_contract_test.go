// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// install-config-contract Slice 5 — universal schema fail-fast invariant + the
// loopzero-build-cwts referenced-secret contract + the loopzero-dashboard runtime-config
// contract. Closes INCLUIR TUDO 7/7. Reuses helpers from the sibling contract
// tests (same package `charts`): operatorChartDir, renderChart, mdTableKeys,
// readFile, camelAPI, setEqual, fileExists.
package charts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// chartsRoot resolves the published-charts dir.
func chartsRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "charts")
}

// chartType returns the Chart.yaml `type:` (default "application" when absent).
func chartType(t *testing.T, chartDir string) string {
	t.Helper()
	body := readFile(t, filepath.Join(chartDir, "Chart.yaml"))
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "type:") {
			return strings.TrimSpace(strings.TrimPrefix(s, "type:"))
		}
	}
	return "application"
}

// TestAllPublishedChartsSchemaFailFast — universal invariant (D1): every non-library
// chart under charts/ MUST ship values.schema.json with root additionalProperties:false,
// so a typo'd top-level Helm value fails `helm install` instead of being silently ignored.
func TestAllPublishedChartsSchemaFailFast(t *testing.T) {
	root := chartsRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read charts dir: %v", err)
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if !fileExists(filepath.Join(dir, "Chart.yaml")) {
			continue
		}
		if chartType(t, dir) == "library" {
			continue // loopzero-common — no installable values surface
		}
		schemaPath := filepath.Join(dir, "values.schema.json")
		if !fileExists(schemaPath) {
			t.Errorf("%s: missing values.schema.json (non-library chart must ship a fail-fast schema)", e.Name())
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal([]byte(readFile(t, schemaPath)), &schema); err != nil {
			t.Errorf("%s: values.schema.json parse: %v", e.Name(), err)
			continue
		}
		checked++
		if ap, ok := schema["additionalProperties"]; !ok || ap != false {
			t.Errorf("%s: root values.schema.json must set additionalProperties:false (fail-fast) — got %v", e.Name(), ap)
		}
	}
	if checked == 0 {
		t.Fatal("no non-library charts checked — glob/path error (guard would be vacuous)")
	}
}

var secretRefNameRe = regexp.MustCompile(`(?m)secretRef:\s*\n\s*name:\s*([A-Za-z0-9][A-Za-z0-9._-]*)`)

// renderedSecretRefNames returns the set of secret NAMES referenced by `secretRef:`
// blocks in rendered helm output — ONLY the name immediately under each secretRef
// (EC-4: container/template `name:` fields must NOT be collected).
func renderedSecretRefNames(out string) []string {
	seen := map[string]bool{}
	for _, m := range secretRefNameRe.FindAllStringSubmatch(out, -1) {
		seen[m[1]] = true
	}
	var names []string
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

var referencedSecretLineRe = regexp.MustCompile("(?m)^\\*\\*Referenced Secret:\\*\\*\\s*`([^`]+)`")

// grepReferencedSecretName parses the `**Referenced Secret:** ` + "`name`" line in a
// SECRETS.md (EC-2: the kebab-case name is NOT a mdTableKeys row).
func grepReferencedSecretName(t *testing.T, path string) string {
	t.Helper()
	m := referencedSecretLineRe.FindStringSubmatch(readFile(t, path))
	if m == nil {
		return ""
	}
	return m[1]
}

// TestBuildCwtsSecretContract — loopzero-build-cwts consumes loopzero-minio-credentials by
// reference. Asserts (EC-2/EC-4): (a) SECRETS.md documents the UPPER_SNAKE secret KEYS;
// (b) the rendered secretRef name == the documented Referenced Secret name; (c) the
// minio.existingSecret seam overrides the rendered name.
func TestBuildCwtsSecretContract(t *testing.T) {
	c := operatorChartDir(t, "loopzero-build-cwts")

	// (a) KEYS — UPPER_SNAKE, mdTableKeys-parseable, non-empty floor.
	keys := mdTableKeys(t, filepath.Join(c, "SECRETS.md"))
	if len(keys) == 0 {
		t.Fatalf("loopzero-build-cwts: SECRETS.md documents zero UPPER_SNAKE secret keys (missing or unparseable)")
	}

	// (b) NAME — dedicated parser (NOT mdTableKeys); set-equal to rendered secretRef name.
	refs := renderedSecretRefNames(renderChart(t, c, defaultRelease, nil))
	if len(refs) == 0 {
		t.Fatalf("loopzero-build-cwts: no secretRef name rendered (guard vacuous)")
	}
	docName := grepReferencedSecretName(t, filepath.Join(c, "SECRETS.md"))
	if docName == "" {
		t.Fatalf("loopzero-build-cwts: SECRETS.md missing `**Referenced Secret:** \"name\"` line")
	}
	setEqual(t, "loopzero-build-cwts referenced secret name", []string{docName}, refs)

	// (c) SEAM — existingSecret override changes the rendered name to exactly the override.
	custom := renderedSecretRefNames(renderChart(t, c, defaultRelease, map[string]string{"minio.existingSecret": "probe-secret"}))
	setEqual(t, "loopzero-build-cwts existingSecret override", []string{"probe-secret"}, custom)
}

var valuesConfigKeyRe = regexp.MustCompile(`\.Values\.config\.([A-Za-z][A-Za-z0-9]*)`)

// grepValuesConfigKeys returns the camelCase keys referenced as .Values.config.<key>
// in a chart template file.
func grepValuesConfigKeys(t *testing.T, path string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, m := range valuesConfigKeyRe.FindAllStringSubmatch(readFile(t, path), -1) {
		seen[m[1]] = true
	}
	var keys []string
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestDashboardConfigContract — loopzero-dashboard renders config.* values into __config.js.
// Asserts (EC-1): camelAPI(mdTableKeys(CONFIG.md)) == .Values.config.* keys referenced
// in configmap.yaml. CONFIG.md uses UPPER_SNAKE (mdTableKeys is UPPER_SNAKE-only); camelAPI
// maps each to the camelCase value name.
func TestDashboardConfigContract(t *testing.T) {
	c := operatorChartDir(t, "loopzero-dashboard")
	docUpper := mdTableKeys(t, filepath.Join(c, "CONFIG.md"))
	if len(docUpper) == 0 {
		t.Fatalf("loopzero-dashboard: CONFIG.md documents zero UPPER_SNAKE keys (missing or unparseable)")
	}
	var documented []string
	for _, k := range docUpper {
		documented = append(documented, camelAPI(k))
	}
	tmplKeys := grepValuesConfigKeys(t, filepath.Join(c, "templates", "configmap.yaml"))
	setEqual(t, "loopzero-dashboard CONFIG.md vs .Values.config.* keys", documented, tmplKeys)
}

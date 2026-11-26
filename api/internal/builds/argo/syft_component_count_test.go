// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// componentCountSnippet returns the lines of the generated SBOM script that set
// COMPONENT_COUNT, with the SBOM path replaced by sbomPath, followed by a line
// that prints the result.
func componentCountSnippet(t *testing.T, sbomPath string) string {
	t.Helper()
	s := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.example.com/org/app:abc", false, "", objectStore{})
	generatedPath := sbomLocalDir + "/sbom-api.cdx.json"

	var snippet []string
	inside := false
	for _, line := range strings.Split(s.Source, "\n") {
		if strings.HasPrefix(line, "COMPONENT_COUNT=") {
			inside = true
		}
		if inside {
			snippet = append(snippet, strings.ReplaceAll(line, generatedPath, sbomPath))
		}
		if inside && strings.Contains(line, "COMPONENT_COUNT=0") {
			break
		}
	}
	if len(snippet) == 0 {
		t.Fatalf("no COMPONENT_COUNT assignment in the generated script:\n%s", s.Source)
	}
	return strings.Join(snippet, "\n") + "\nprintf '%s' \"$COMPONENT_COUNT\"\n"
}

func runComponentCount(t *testing.T, sbomPath string) string {
	t.Helper()
	// The scanner image has python3 (through aws-cli) and no jq.
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed; the scanner image provides it")
	}
	out, err := exec.Command("sh", "-c", componentCountSnippet(t, sbomPath)).Output()
	if err != nil {
		t.Fatalf("component count snippet failed: %v", err)
	}
	return string(out)
}

// The count is the length of the top-level components array. Counting "{"
// characters also counted every nested object (hashes, licenses, properties).
func TestSBOMComponentCountIsTheLengthOfTheComponentsArray(t *testing.T) {
	sbom := `{
  "bomFormat": "CycloneDX",
  "metadata": {"component": {"type": "application", "name": "app"}},
  "components": [
    {"type": "library", "name": "a", "hashes": [{"alg": "SHA-256", "content": "x"}],
     "licenses": [{"license": {"id": "MIT"}}], "properties": [{"name": "p", "value": "v"}]},
    {"type": "library", "name": "b"},
    {"type": "file", "name": "c", "hashes": [{"alg": "SHA-1", "content": "y"}]}
  ],
  "dependencies": [{"ref": "a", "dependsOn": ["b"]}]
}`
	path := filepath.Join(t.TempDir(), "sbom.cdx.json")
	if err := os.WriteFile(path, []byte(sbom), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := runComponentCount(t, path); got != "3" {
		t.Errorf("component count = %q, want 3", got)
	}
}

func TestSBOMComponentCountIsZeroWithoutAComponentsField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sbom.cdx.json")
	if err := os.WriteFile(path, []byte(`{"bomFormat": "CycloneDX"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := runComponentCount(t, path); got != "0" {
		t.Errorf("component count = %q, want 0", got)
	}
}

func TestSBOMComponentCountIsZeroWhenTheFileIsNotJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sbom.cdx.json")
	if err := os.WriteFile(path, []byte(`not json {`), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := runComponentCount(t, path); got != "0" {
		t.Errorf("component count = %q, want 0", got)
	}
}

// The scanner image does not ship jq, so the script must not depend on it.
func TestSBOMComponentCountDoesNotNeedJq(t *testing.T) {
	s := syftScanTemplate("tn-1", "proj-1", "abc1234", "api",
		"registry.example.com/org/app:abc", false, "", objectStore{})
	for _, line := range strings.Split(s.Source, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "jq ") {
			t.Errorf("SBOM script calls jq, which the scanner image does not have: %s", line)
		}
	}
}

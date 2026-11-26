// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package k6loadrigor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodThresholds = "export const base = {\n  dropped_iterations: ['count<1'],\n};\nexport const perLevel = {};\n"

const goodManifest = `apiVersion: k6.io/v1alpha1
kind: TestRun
spec:
  parallelism: 4
  script:
    configMap:
      file: scripts/l5-multicell.js
  runner:
    image: grafana/k6:0.52.0
    env:
      - name: K6_PROMETHEUS_RW_SERVER_URL
        valueFrom:
          secretKeyRef:
            name: prom
            key: url
`

const goodTaskfile = "tasks:\n  k6:distributed:l5:\n    cmds:\n      - kubectl apply -f testrun-l5-distributed.yaml\n"

const goodReadme = "# k6\n\n## Which runner per level\n\nL1-L4 single-pod; L5 distributed.\n"

// tree writes a complete, passing layout, then applies overrides (an empty
// body removes the file).
func tree(t *testing.T, overrides map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		thresholdsPath: goodThresholds,
		l5ManifestPath: goodManifest,
		testfilePath:   goodTaskfile,
		readmePath:     goodReadme,
	}
	for k, v := range overrides {
		files[k] = v
	}
	for rel, body := range files {
		if body == "" {
			continue
		}
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func failuresFor(t *testing.T, overrides map[string]string) string {
	t.Helper()
	out, _ := violationOf(Check(Config{RepoRoot: tree(t, overrides)}))
	return strings.Join(out.Failures, "\n")
}

func TestCheck_CompleteLayoutPasses(t *testing.T) {
	out, err := violationOf(Check(Config{RepoRoot: tree(t, nil)}))
	if err != nil {
		t.Fatalf("want PASS, got %v %v", err, out.Failures)
	}
}

func TestThresholds_DroppedIterationsOutsideBaseFails(t *testing.T) {
	got := failuresFor(t, map[string]string{thresholdsPath: "export const base = {};\nexport const perLevel = {\n  dropped_iterations: ['count<1'],\n};\n"})
	if !strings.Contains(got, "not in `base`") {
		t.Fatalf("want the base-block failure, got %q", got)
	}
}

func TestThresholds_RateInsteadOfCountFails(t *testing.T) {
	got := failuresFor(t, map[string]string{thresholdsPath: "export const base = {\n  dropped_iterations: ['rate<0.01'],\n};\n"})
	if !strings.Contains(got, "count<") {
		t.Fatalf("want the count< failure, got %q", got)
	}
}

func TestManifest_MissingPinnedImageFails(t *testing.T) {
	got := failuresFor(t, map[string]string{l5ManifestPath: strings.Replace(goodManifest, "grafana/k6:0.52.0", "grafana/k6:latest", 1)})
	if !strings.Contains(got, "pinned grafana/k6:0.52.0") {
		t.Fatalf("want the pinned-image failure, got %q", got)
	}
}

func TestManifest_InlineSecretFails(t *testing.T) {
	got := failuresFor(t, map[string]string{l5ManifestPath: goodManifest + "      - name: TOKEN\n        value: \"0123456789abcdef0123456789abcdef\"\n"})
	if !strings.Contains(got, "inline secret") {
		t.Fatalf("want the inline-secret failure, got %q", got)
	}
}

// The YAML check runs in-process: it used to shell out to python3+PyYAML,
// passing when python3 was absent and failing a valid manifest when PyYAML
// was missing.
func TestManifest_InvalidYAMLFailsWithoutPython(t *testing.T) {
	t.Setenv("PATH", "")
	got := failuresFor(t, map[string]string{l5ManifestPath: goodManifest + "spec: [unclosed\n"})
	if !strings.Contains(got, "NOT valid YAML") {
		t.Fatalf("want the invalid-YAML failure without python3 on PATH, got %q", got)
	}
}

func TestManifest_MultiDocumentYAMLIsValid(t *testing.T) {
	t.Setenv("PATH", "")
	got := failuresFor(t, map[string]string{l5ManifestPath: goodManifest + "---\nkind: ConfigMap\n"})
	if strings.Contains(got, "NOT valid YAML") {
		t.Fatalf("a valid multi-document manifest was rejected: %q", got)
	}
}

func TestTaskfile_MissingL5TaskFails(t *testing.T) {
	got := failuresFor(t, map[string]string{testfilePath: "tasks: {}\n"})
	if !strings.Contains(got, "k6:distributed:l5 task not defined") {
		t.Fatalf("want the missing-task failure, got %q", got)
	}
}

func TestReadme_MissingPerLevelSectionFails(t *testing.T) {
	got := failuresFor(t, map[string]string{readmePath: "# k6\n\nsingle-pod and distributed exist.\n"})
	if !strings.Contains(got, "per-level") {
		t.Fatalf("want the per-level failure, got %q", got)
	}
}

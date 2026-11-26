// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package valuesduplicatekeys

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCheckFailsWhenATopLevelKeyIsDeclaredTwice(t *testing.T) {
	// Arrange — the gitea values-ha.yaml shape: Helm keeps only the second block.
	root := t.TempDir()
	writeFile(t, root, "infra/helm/gitea/values-ha.yaml",
		"gitea:\n  config:\n    session: redis\nreplicaCount: 2\ngitea:\n  metrics:\n    enabled: true\n")

	// Act
	out, err := Check(Config{RepoRoot: root})

	// Assert
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("Pass = true, want false for a duplicated top-level key")
	}
	if len(out.Findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(out.Findings), out.Findings)
	}
	f := out.Findings[0]
	if f.File != "infra/helm/gitea/values-ha.yaml" || f.Key != "gitea" || f.Line != 5 || f.FirstLine != 1 {
		t.Errorf("finding = %+v, want gitea at line 5 first declared at line 1", f)
	}
}

func TestCheckFailsWhenANestedKeyIsDeclaredTwice(t *testing.T) {
	// Arrange — duplicates are dropped at every depth, not only the top level.
	root := t.TempDir()
	writeFile(t, root, "charts/loop-runtime-api/values.yaml",
		"image:\n  tag: \"\"\n  tag: latest\n")

	// Act
	out, err := Check(Config{RepoRoot: root})

	// Assert
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass || len(out.Findings) != 1 || out.Findings[0].Path != "image.tag" {
		t.Fatalf("outcome = %+v, want one finding at image.tag", out)
	}
}

func TestCheckPassesWhenEveryMappingKeyIsUnique(t *testing.T) {
	// Arrange — the same key under two different parents is not a duplicate.
	root := t.TempDir()
	writeFile(t, root, "infra/helm/tempo/values.yaml",
		"tempo:\n  securityContext: {}\nsecurityContext: {}\n")
	writeFile(t, root, "charts/loop-console/values-prod.yaml", "replicaCount: 2\n")

	// Act
	out, err := Check(Config{RepoRoot: root})

	// Assert
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass || out.Scanned != 2 {
		t.Fatalf("outcome = %+v, want pass over 2 files", out)
	}
}

func TestCheckIgnoresFilesThatAreNotValuesFiles(t *testing.T) {
	// Arrange — templates are Go templates, not YAML, and are not values files.
	root := t.TempDir()
	writeFile(t, root, "charts/loop-runtime-api/templates/deployment.yaml",
		"a: 1\na: 2\n")
	writeFile(t, root, "charts/loop-runtime-api/values.yaml", "a: 1\n")

	// Act
	out, err := Check(Config{RepoRoot: root})

	// Assert
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass || out.Scanned != 1 {
		t.Fatalf("outcome = %+v, want pass over the single values file", out)
	}
}

func TestCheckFailsLoudlyOnUnparseableValuesFile(t *testing.T) {
	// Arrange — a file Helm cannot parse must not be reported as clean.
	root := t.TempDir()
	writeFile(t, root, "infra/helm/broken/values.yaml", "a: [1, 2\n")

	// Act
	_, err := Check(Config{RepoRoot: root})

	// Assert
	if err == nil {
		t.Fatal("Check error = nil, want a parse error naming the file")
	}
}

func TestCheckSkipsWhenNoValuesFileExists(t *testing.T) {
	// Arrange
	root := t.TempDir()

	// Act
	out, err := Check(Config{RepoRoot: root})

	// Assert
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Skipped || out.Pass {
		t.Fatalf("outcome = %+v, want Skipped (an empty scan is not a pass)", out)
	}
}

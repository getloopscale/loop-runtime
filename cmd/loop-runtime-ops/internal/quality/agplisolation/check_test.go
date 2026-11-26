// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package agplisolation

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeAGPL is a stand-in pattern so this test source never contains the real
// AGPL import literal contiguously. (Test files are excluded from the scan
// anyway, but keeping this clean avoids confusion.)
const fakeAGPL = "evil/agpl-pkg"

func writeGo(t *testing.T, root, rel, importPath string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "package x\n\nimport (\n\t" + `"github.com/` + importPath + "\"\n)\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func cfg(root string) Config {
	return Config{
		Root:      root,
		Patterns:  []string{fakeAGPL},
		Allowlist: []Exception{{Import: fakeAGPL, Path: "operators/internal/x/allowed.go"}},
		ScanDirs:  []string{"api", "cli", "cloud", "pkg", "operators"},
	}
}

func TestCleanRepoPasses(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/handler.go", "stretchr/testify")
	if !Check(cfg(root)).Pass {
		t.Error("non-AGPL imports should pass")
	}
}

func TestAGPLInApiFails(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/handler.go", fakeAGPL)
	o := Check(cfg(root))
	if o.Pass || len(o.Violations) != 1 {
		t.Errorf("expected 1 violation in api/, got %v", o.Violations)
	}
}

func TestAGPLInPkgFails(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "pkg/lib/thing.go", fakeAGPL)
	if Check(cfg(root)).Pass {
		t.Error("AGPL in pkg/ must fail (propagates to all consumers)")
	}
}

func TestAGPLInAllowlistedLocationPasses(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "operators/internal/x/allowed.go", fakeAGPL)
	o := Check(cfg(root))
	if !o.Pass {
		t.Errorf("AGPL in allowlisted location should pass, got %v", o.Violations)
	}
}

func TestAGPLInWrongOperatorFileFails(t *testing.T) {
	root := t.TempDir()
	// Same package, different file than the allowlisted one → violation.
	writeGo(t, root, "operators/internal/x/other.go", fakeAGPL)
	if Check(cfg(root)).Pass {
		t.Error("AGPL outside the exact allowlisted path must fail")
	}
}

func TestTestFilesIgnored(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/handler_test.go", fakeAGPL)
	if !Check(cfg(root)).Pass {
		t.Error("_test.go files should be ignored")
	}
}

func TestMultipleViolationsCounted(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/a.go", fakeAGPL)
	writeGo(t, root, "cli/b.go", fakeAGPL)
	o := Check(cfg(root))
	if o.Pass || len(o.Violations) != 2 {
		t.Errorf("expected 2 violations, got %v", o.Violations)
	}
}

func TestMissingScanDirTolerated(t *testing.T) {
	root := t.TempDir()
	// Only api/ exists; cli/cloud/pkg/operators absent → walk tolerates, passes.
	writeGo(t, root, "api/clean.go", "spf13/cobra")
	if !Check(cfg(root)).Pass {
		t.Error("missing scan dirs should be tolerated")
	}
}

func TestDefaultsApplied(t *testing.T) {
	root := t.TempDir()
	// Default pattern is minio/madmin-go in the allowlisted location → pass.
	writeGo(t, root, "operators/internal/minio/provisioner.go", "minio/madmin-go")
	if !Check(Config{Root: root}).Pass {
		t.Error("default allowlist should permit minio/madmin-go in provisioner.go")
	}
	// Same default pattern elsewhere → violation.
	writeGo(t, root, "pkg/leak.go", "minio/madmin-go")
	if Check(Config{Root: root}).Pass {
		t.Error("default pattern outside allowlist should fail")
	}
}

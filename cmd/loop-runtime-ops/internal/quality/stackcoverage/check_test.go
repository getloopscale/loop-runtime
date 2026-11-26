// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package stackcoverage

import (
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T, langSrc string, fixtures map[string]string) Config {
	t.Helper()
	root := t.TempDir()
	langFile := filepath.Join(root, "languages.go")
	if err := os.WriteFile(langFile, []byte(langSrc), 0o644); err != nil {
		t.Fatalf("write languages.go: %v", err)
	}
	fixDir := filepath.Join(root, "fixtures")
	for name, loopRuntime := range fixtures {
		d := filepath.Join(fixDir, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(d, "loopzero.yaml"), []byte(loopRuntime), 0o644); err != nil {
			t.Fatalf("write loopzero.yaml: %v", err)
		}
	}
	if err := os.MkdirAll(fixDir, 0o755); err != nil {
		t.Fatalf("mkdir fixtures: %v", err)
	}
	return Config{Root: root, LanguagesFile: langFile, FixturesDir: fixDir}
}

// registrySrc is a languages.go stub in the shape the real file has: the
// strategy set is declared by runtimeRegistry, which languages.go itself calls
// "the single source of truth for which runtimes resolve".
const registrySrc = `package argo
type NodeStrategy struct{}
type PythonStrategy struct{}

var runtimeRegistry = map[string]RuntimeDescriptor{
	"node":   {Name: "node", Strategy: &NodeStrategy{}},
	"python": {Name: "python", Strategy: &PythonStrategy{}},
}
`

// The gate must read the strategy set OFF the registry rather than off a list
// kept beside it. A duplicated list rots: when the product narrowed to the
// fixed LoopZeroKit stack (node + python), the gate still asserted 9 languages and
// reported 7 phantom violations against types nobody had deleted by mistake.
func TestStrategiesDerivedFromRuntimeRegistry(t *testing.T) {
	cfg := setup(t, registrySrc, map[string]string{
		"node-app": "framework: node\n",
		"py-app":   "framework: fastapi\n",
	})
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.StrategyCount != 2 {
		t.Errorf("expected the 2 strategies the registry declares, got %d", o.StrategyCount)
	}
	if !o.Pass {
		t.Errorf("expected Pass; typeErrors=%v missing=%v", o.TypeErrors, o.Missing)
	}
}

// Adding a runtime to the registry without a fixture must fail — that is the
// assertion the gate exists for, and deriving the set must not weaken it.
func TestRegistryRuntimeWithoutFixture_Fail(t *testing.T) {
	src := registrySrc + `
type RubyStrategy struct{}
var _ = map[string]RuntimeDescriptor{"ruby": {Name: "ruby", Strategy: &RubyStrategy{}}}
`
	// Same registry block, now with ruby inside it.
	src = replaceOnce(src,
		`	"python": {Name: "python", Strategy: &PythonStrategy{}},`,
		`	"python": {Name: "python", Strategy: &PythonStrategy{}},
	"ruby":   {Name: "ruby", Strategy: &RubyStrategy{}},`)
	cfg := setup(t, src, map[string]string{
		"node-app": "framework: node\n",
		"py-app":   "framework: fastapi\n",
	})
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail — ruby is registered but has no fixture")
	}
	if len(o.Missing) != 1 || o.Missing[0] != "ruby" {
		t.Errorf("expected Missing=[ruby], got %v", o.Missing)
	}
}

// A registry entry pointing at a type nobody declared is the drift the gate
// must still catch after the rewrite.
func TestRegistryTypeMissing_Fail(t *testing.T) {
	src := replaceOnce(registrySrc, "type PythonStrategy struct{}\n", "")
	cfg := setup(t, src, map[string]string{
		"node-app": "framework: node\n",
		"py-app":   "framework: fastapi\n",
	})
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass || len(o.TypeErrors) != 1 {
		t.Errorf("expected 1 typeError, got pass=%v errs=%v", o.Pass, o.TypeErrors)
	}
}

// Negative case: a languages.go with no registry at all is a structural
// failure, not a vacuous pass. A gate that silently asserts nothing is the
// exact defect #255 is about.
func TestNoRegistry_Error(t *testing.T) {
	cfg := setup(t, "package argo\ntype NodeStrategy struct{}\n", map[string]string{
		"node-app": "framework: node\n",
	})
	if _, err := Check(cfg); err == nil {
		t.Error("expected an error when runtimeRegistry is absent, not a vacuous pass")
	}
}

// A fixture whose framework value is quoted still maps to its language.
func TestFrameworkQuotesStripped(t *testing.T) {
	cfg := setup(t, registrySrc, map[string]string{
		"node-app": "framework: node\n",
		"py-app":   "framework: \"django\"\n",
	})
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("quoted framework should still map; missing=%v", o.Missing)
	}
}

func TestMissingLanguagesFile_Error(t *testing.T) {
	cfg := setup(t, registrySrc, map[string]string{"node-app": "framework: node\n"})
	cfg.LanguagesFile = filepath.Join(cfg.Root, "nonexistent.go")
	if _, err := Check(cfg); err == nil {
		t.Error("expected error for missing languages.go")
	}
}

func TestMissingFixturesDir_Error(t *testing.T) {
	cfg := setup(t, registrySrc, map[string]string{"node-app": "framework: node\n"})
	cfg.FixturesDir = filepath.Join(cfg.Root, "nope")
	if _, err := Check(cfg); err == nil {
		t.Error("expected error for missing fixtures dir")
	}
}

func TestFixtureWithoutLoopRuntimeYAMLIgnored(t *testing.T) {
	cfg := setup(t, registrySrc, map[string]string{
		"node-app": "framework: node\n",
		"py-app":   "framework: fastapi\n",
	})
	// A dir with no loopzero.yaml must neither crash nor affect the result.
	if err := os.MkdirAll(filepath.Join(cfg.FixturesDir, "empty"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("empty fixture dir should be ignored; missing=%v", o.Missing)
	}
}

func replaceOnce(s, old, new string) string {
	i := indexOf(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

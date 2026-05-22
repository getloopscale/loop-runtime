// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package stackcoverage

import (
	"os"
	"path/filepath"
	"testing"
)

// allTypes is a languages.go stub declaring every strategy type.
const allTypes = `package argo
type NodeStrategy struct{}
type GoStrategy struct{}
type PythonStrategy struct{}
type RubyStrategy struct{}
type JavaStrategy struct{}
type PHPStrategy struct{}
type RustStrategy struct{}
type CustomStrategy struct{}
type UserDockerfileStrategy struct{}
`

// fullFixtures lists a fixture per strategy.
var fullFixtures = map[string]string{
	"node-app":   "framework: node\n",
	"go-app":     "framework: gin\n",
	"py-app":     "framework: fastapi\n",
	"ruby-app":   "framework: rails\n",
	"java-app":   "framework: spring-boot\n",
	"php-app":    "framework: laravel\n",
	"rust-app":   "framework: axum\n",
	"custom-app": "framework: custom\n",
	"docker-app": "build: dockerfile\n",
}

func setup(t *testing.T, langSrc string, fixtures map[string]string) Config {
	t.Helper()
	root := t.TempDir()
	langFile := filepath.Join(root, "languages.go")
	if err := os.WriteFile(langFile, []byte(langSrc), 0o644); err != nil {
		t.Fatalf("write languages.go: %v", err)
	}
	fixDir := filepath.Join(root, "fixtures")
	for name, loopzero := range fixtures {
		d := filepath.Join(fixDir, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(d, "loopzero.yaml"), []byte(loopzero), 0o644); err != nil {
			t.Fatalf("write loopzero.yaml: %v", err)
		}
	}
	if err := os.MkdirAll(fixDir, 0o755); err != nil {
		t.Fatalf("mkdir fixtures: %v", err)
	}
	return Config{Root: root, LanguagesFile: langFile, FixturesDir: fixDir}
}

func TestFullCoverage_Pass(t *testing.T) {
	cfg := setup(t, allTypes, fullFixtures)
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass; typeErrors=%v missing=%v", o.TypeErrors, o.Missing)
	}
	if o.StrategyCount != 9 {
		t.Errorf("expected 9 strategies, got %d", o.StrategyCount)
	}
}

func TestMissingFixture_Fail(t *testing.T) {
	fx := map[string]string{}
	for k, v := range fullFixtures {
		fx[k] = v
	}
	delete(fx, "rust-app") // drop rust coverage
	cfg := setup(t, allTypes, fx)
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail with rust uncovered")
	}
	if len(o.Missing) != 1 || o.Missing[0] != "rust" {
		t.Errorf("expected Missing=[rust], got %v", o.Missing)
	}
}

func TestMissingType_Fail(t *testing.T) {
	langSrc := allTypes // remove RustStrategy declaration
	langSrc = replaceOnce(langSrc, "type RustStrategy struct{}\n", "")
	cfg := setup(t, langSrc, fullFixtures)
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass || len(o.TypeErrors) != 1 {
		t.Errorf("expected 1 typeError, got pass=%v errs=%v", o.Pass, o.TypeErrors)
	}
}

func TestDockerfileBuildCoversUserDockerfile(t *testing.T) {
	fx := map[string]string{}
	for k, v := range fullFixtures {
		fx[k] = v
	}
	// Replace the build:dockerfile fixture with a framework-only one; then
	// user-dockerfile should be missing.
	fx["docker-app"] = "framework: node\n"
	cfg := setup(t, allTypes, fx)
	o, _ := Check(cfg)
	if o.Pass {
		t.Error("expected Fail — user-dockerfile uncovered without build: dockerfile")
	}
	found := false
	for _, m := range o.Missing {
		if m == "user-dockerfile" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected user-dockerfile in Missing, got %v", o.Missing)
	}
}

func TestFrameworkQuotesStripped(t *testing.T) {
	fx := map[string]string{}
	for k, v := range fullFixtures {
		fx[k] = v
	}
	fx["py-app"] = "framework: \"django\"\n" // quoted value
	cfg := setup(t, allTypes, fx)
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("quoted framework should still map; missing=%v", o.Missing)
	}
}

func TestMissingLanguagesFile_Error(t *testing.T) {
	cfg := setup(t, allTypes, fullFixtures)
	cfg.LanguagesFile = filepath.Join(cfg.Root, "nonexistent.go")
	if _, err := Check(cfg); err == nil {
		t.Error("expected error for missing languages.go")
	}
}

func TestMissingFixturesDir_Error(t *testing.T) {
	cfg := setup(t, allTypes, fullFixtures)
	cfg.FixturesDir = filepath.Join(cfg.Root, "nope")
	if _, err := Check(cfg); err == nil {
		t.Error("expected error for missing fixtures dir")
	}
}

func TestFixtureWithoutLoopZeroYAMLIgnored(t *testing.T) {
	cfg := setup(t, allTypes, fullFixtures)
	// Add an empty dir with no loopzero.yaml — must not crash or affect result.
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

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package stackcoverage implements the T10.1 gate: enforce a 1:1 mapping
// between LanguageStrategy types registered in api/internal/builds/argo/languages.go
// and E2E fixture coverage under infra/tests/e2e/fixtures/.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/check-stack-coverage.sh (which was never wired into
// CI — an "implemented but not wired" gap). Pure-Go (regexp + YAML grep) —
// no external CLI.
package stackcoverage

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// runtimeRegistryRe isolates the `var runtimeRegistry = map[...]{ ... }` block
// in languages.go, and registryEntryRe reads one `"key": {… Strategy: &T{}}`
// entry out of it.
//
// The strategy set is DERIVED from that block rather than duplicated here on
// purpose. languages.go calls runtimeRegistry "the single source of truth for
// which runtimes resolve"; a second list beside it is a copy that rots. It did:
// when the product narrowed to the fixed LoopZeroKit stack (node + python), this
// gate still asserted 9 languages and reported 7 phantom violations, which is
// how it ended up quarantined as a failing gate instead of guarding anything.
var (
	runtimeRegistryRe = regexp.MustCompile(`(?s)var runtimeRegistry\s*=\s*map\[string\]\w+\{(.*?)\n\}`)
	registryEntryRe   = regexp.MustCompile(`"([a-z0-9][a-z0-9-]*)"\s*:\s*\{[^}]*Strategy:\s*&(\w+)\{\}`)
)

// parseRuntimeRegistry returns language key -> strategy type name, read off the
// runtimeRegistry block. An absent or empty block is an error, never an empty
// set: a gate that silently asserts nothing is the defect #255 is about.
func parseRuntimeRegistry(langSrc string) (map[string]string, error) {
	block := runtimeRegistryRe.FindStringSubmatch(langSrc)
	if block == nil {
		return nil, fmt.Errorf("runtimeRegistry not found in languages.go — the gate derives the strategy set from it")
	}
	out := map[string]string{}
	for _, m := range registryEntryRe.FindAllStringSubmatch(block[1], -1) {
		out[m[1]] = m[2]
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("runtimeRegistry declares no runtime — refusing to pass vacuously")
	}
	return out, nil
}

// frameworkToLang maps a loopzero.yaml framework value to its language key.
var frameworkToLang = map[string]string{
	"node": "node", "nextjs": "node", "vite": "node", "remix": "node",
	"nestjs": "node", "express": "node",
	"go": "go", "gin": "go",
	"python": "python", "fastapi": "python", "django": "python",
	"ruby": "ruby", "sinatra": "ruby", "rails": "ruby",
	"java": "java", "spring": "java", "spring-boot": "java",
	"php": "php", "laravel": "php", "slim": "php",
	"rust": "rust", "axum": "rust", "actix": "rust", "rocket": "rust",
	"custom":          "custom",
	"user-dockerfile": "user-dockerfile",
}

var frameworkLineRe = regexp.MustCompile(`(?m)^\s*framework:\s*(.+)$`)

// Config drives the gate.
type Config struct {
	Root          string // repo root
	LanguagesFile string // default: api/internal/builds/argo/languages.go
	FixturesDir   string // default: infra/tests/e2e/fixtures
}

// Outcome captures the result.
type Outcome struct {
	Pass          bool
	StrategyCount int
	TypeErrors    []string // strategy types missing from languages.go
	Missing       []string // language keys with no fixture coverage
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Check runs the gate.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	langFile := cfg.LanguagesFile
	if langFile == "" {
		langFile = filepath.Join(root, "api", "internal", "builds", "argo", "languages.go")
	}
	fixturesDir := cfg.FixturesDir
	if fixturesDir == "" {
		fixturesDir = filepath.Join(root, "infra", "tests", "e2e", "fixtures")
	}

	langData, err := os.ReadFile(langFile)
	if err != nil {
		return nil, fmt.Errorf("languages.go not found at %s: %w", langFile, err)
	}
	if info, err := os.Stat(fixturesDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("fixtures dir not found at %s", fixturesDir)
	}
	langSrc := string(langData)

	strategyTypes, err := parseRuntimeRegistry(langSrc)
	if err != nil {
		return nil, err
	}

	out := &Outcome{Pass: true, StrategyCount: len(strategyTypes)}

	// 1. Each strategy type must be declared in languages.go.
	for lang, typ := range strategyTypes {
		re := regexp.MustCompile(`type ` + regexp.QuoteMeta(typ) + `\b`)
		if !re.MatchString(langSrc) {
			out.TypeErrors = append(out.TypeErrors,
				fmt.Sprintf("strategy %q missing type %q in languages.go", lang, typ))
		}
	}

	// 2. Each strategy must have ≥1 fixture covering it.
	covered, err := scanFixtures(fixturesDir)
	if err != nil {
		return nil, err
	}
	for lang := range strategyTypes {
		if !covered[lang] {
			out.Missing = append(out.Missing, lang)
		}
	}

	sort.Strings(out.TypeErrors)
	sort.Strings(out.Missing)
	if len(out.TypeErrors) > 0 || len(out.Missing) > 0 {
		out.Pass = false
	}
	return out, nil
}

// scanFixtures returns the set of language keys covered by fixtures.
func scanFixtures(dir string) (map[string]bool, error) {
	covered := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read fixtures dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		loopzeroYAML := filepath.Join(dir, e.Name(), "loopzero.yaml")
		data, err := os.ReadFile(loopzeroYAML)
		if err != nil {
			continue // no loopzero.yaml in this fixture dir
		}
		content := string(data)
		if fw := extractFramework(content); fw != "" {
			if lang, ok := frameworkToLang[fw]; ok {
				covered[lang] = true
			}
		}
		// user-dockerfile is dispatched by `build: dockerfile`, not framework.
		if hasDockerfileBuild(content) {
			covered["user-dockerfile"] = true
		}
	}
	return covered, nil
}

// extractFramework returns the first framework: value, quote-stripped.
func extractFramework(content string) string {
	m := frameworkLineRe.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	v := strings.TrimSpace(m[1])
	v = strings.Trim(v, `"'`)
	return strings.TrimSpace(v)
}

// hasDockerfileBuild reports whether any line is `build: dockerfile`.
func hasDockerfileBuild(content string) bool {
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "build:") &&
			strings.Contains(line, "dockerfile") {
			return true
		}
	}
	return false
}

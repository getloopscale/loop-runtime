// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package phase2gate

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	taskfilePath = filepath.Join(repoRoot, "taskfiles/infra.yml")
	kratosValues = filepath.Join(repoRoot, "infra/helm/ory-kratos/values.yaml.gotmpl")
	hydraValues  = filepath.Join(repoRoot, "infra/helm/ory-hydra/values.yaml.gotmpl")
	mgmtHelmfile = filepath.Join(repoRoot, "infra/helmfile/mgmt.helmfile.yaml.gotmpl")
	mappingDoc   = filepath.Join(repoRoot, "docs/scripts-audit/2026-05-22-helmfile-mapping.md")
)

// readNonCommentLines returns the lines of a file with comment-only and
// in-line-comment-suffixed portions stripped — used for code-vs-comment
// distinction in YAML/Taskfile audits.
func readNonCommentLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// Skip pure-comment lines (leading whitespace + '#').
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Strip trailing inline comment (best-effort; quoted '#' allowed).
		if idx := strings.Index(line, " #"); idx > 0 {
			line = line[:idx]
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	return lines
}

// TestT2_3a_ZeroBareHelmInTaskfile — T2.3.a EC-01: `helm upgrade --install`
// must be REPLACED by `helmfile sync`. Any survivor outside comments is a
// regression.
func TestT2_3a_ZeroBareHelmInTaskfile(t *testing.T) {
	lines := readNonCommentLines(t, taskfilePath)
	bareHelmRe := regexp.MustCompile(`\bhelm[[:space:]]+upgrade[[:space:]]+--install\b`)
	bareHelm2Re := regexp.MustCompile(`\bhelm[[:space:]]+install[[:space:]]`)
	for i, line := range lines {
		if bareHelmRe.MatchString(line) || bareHelm2Re.MatchString(line) {
			t.Errorf("line %d: bare `helm upgrade --install` survived T2.3 rewrite: %s", i+1, line)
		}
	}
}

// TestT2_3b_ZeroBareBase64DInTaskfile — T2.3.b EC-02: `base64 -d` quebra em
// macOS BSD. Must be replaced by `kubectl -o go-template` OR `openssl base64 -d -A`.
// Go RE2 has no lookahead; we filter manually by inspecting context.
func TestT2_3b_ZeroBareBase64DInTaskfile(t *testing.T) {
	lines := readNonCommentLines(t, taskfilePath)
	// Match `| base64 -d` (pipe + base64 -d) which is the canonical non-portable form.
	bareBase64Re := regexp.MustCompile(`\|[[:space:]]*base64[[:space:]]+-d\b`)
	for i, line := range lines {
		if !bareBase64Re.MatchString(line) {
			continue
		}
		// Exclude openssl base64 -d -A (portable BSD+GNU)
		if strings.Contains(line, "openssl base64 -d") {
			continue
		}
		// Exclude `base64 -d -A` even without openssl prefix (still portable)
		if strings.Contains(line, "base64 -d -A") {
			continue
		}
		t.Errorf("line %d: bare `| base64 -d` survived T2.3.b rewrite (macOS BSD incompat): %s", i+1, line)
	}
}

// TestT2_3a_HelmfileSyncPresent — Taskfile install:* tasks must invoke helmfile sync.
func TestT2_3a_HelmfileSyncPresent(t *testing.T) {
	data, err := os.ReadFile(taskfilePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl") {
		t.Errorf("Taskfile should invoke helmfile sync for mgmt cluster")
	}
	if !strings.Contains(content, "helmfile -f infra/helmfile/build.helmfile.yaml.gotmpl") {
		t.Errorf("Taskfile should invoke helmfile sync for build cluster")
	}
	if !strings.Contains(content, "helmfile -f infra/helmfile/runtime.helmfile.yaml.gotmpl") {
		t.Errorf("Taskfile should invoke helmfile sync for runtime cluster")
	}
}

// TestT2_3b_PortableBase64Present — Taskfile must use kubectl go-template OR
// openssl for base64 decode.
func TestT2_3b_PortableBase64Present(t *testing.T) {
	data, err := os.ReadFile(taskfilePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	hasGoTemplate := strings.Contains(content, "go-template='{{`{{.data.")
	hasOpenssl := strings.Contains(content, "openssl base64 -d -A")
	if !hasGoTemplate && !hasOpenssl {
		t.Errorf("Taskfile should use kubectl go-template OR openssl for base64 decode (T2.3.b EC-02 portability)")
	}
}

// TestT2_2_KratosYAMLEscapeFixed — kratos values.yaml.gotmpl must use single-quoted
// YAML for the DSN line (allows unescaped " inside gotmpl literal).
func TestT2_2_KratosYAMLEscapeFixed(t *testing.T) {
	data, err := os.ReadFile(kratosValues)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	// The new (fixed) form has single-quoted YAML
	if !strings.Contains(content, "dsn: 'postgres://loopzero:") {
		t.Errorf("kratos values.yaml.gotmpl should use single-quoted YAML for DSN (EC-02)")
	}
	// The broken form has `\"` escape inside double-quoted YAML — must not exist
	if strings.Contains(content, `requiredEnv \"LOOPZERO_DB_PASSWORD\"`) {
		t.Errorf("kratos values.yaml.gotmpl still has broken `\\\"` escape (EC-02 regression)")
	}
}

// TestT2_2_HydraYAMLEscapeFixed — same for hydra.
func TestT2_2_HydraYAMLEscapeFixed(t *testing.T) {
	data, err := os.ReadFile(hydraValues)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "dsn: 'postgres://loopzero:") {
		t.Errorf("hydra values.yaml.gotmpl should use single-quoted YAML for DSN (EC-02)")
	}
	if strings.Contains(content, `requiredEnv \"LOOPZERO_DB_PASSWORD\"`) {
		t.Errorf("hydra values.yaml.gotmpl still has broken `\\\"` escape (EC-02 regression)")
	}
}

// TestT2_1_MappingDocExists — the audit doc produced by T2.1 must be committed.
func TestT2_1_MappingDocExists(t *testing.T) {
	data, err := os.ReadFile(mappingDoc)
	if err != nil {
		t.Fatalf("mapping doc missing: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Helmfile Mapping") {
		t.Errorf("mapping doc should have 'Helmfile Mapping' header")
	}
	if !strings.Contains(content, "EC-01") {
		t.Errorf("mapping doc should reference EC-01 compliance check")
	}
	if !strings.Contains(content, "EC-02") {
		t.Errorf("mapping doc should reference EC-02 compliance check")
	}
}

// TestT2_3a_NoBareSelector_InInstallMgmt — install:mgmt should use master sync
// (single `helmfile sync` without `--selector`). Per-release `--selector`
// breaks `needs:` ordering — EC-01 mitigation requires master pattern.
//
// T3.5 of plan 2026-06-11-code-review-remediation v1.2: the original
// end-marker `infra:install:build:` was too broad — a NEW sibling task
// `infra:install:mgmt-dashboard-only:` (legitimately using --selector
// profile=dashboard-only by design — that's its entire purpose) landed
// in between, causing the gate to false-positive. Tightened endMarker
// to the immediate next task header so we scan ONLY install:mgmt's
// own block. Filters comments to avoid false positives when
// documentation mentions --selector.
func TestT2_3a_NoBareSelector_InInstallMgmt(t *testing.T) {
	data, err := os.ReadFile(taskfilePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	// Find install:mgmt block boundaries
	startMarker := "infra:install:mgmt:"
	endMarker := "infra:install:mgmt-dashboard-only:"
	startIdx := strings.Index(content, startMarker)
	endIdx := strings.Index(content, endMarker)
	if startIdx == -1 || endIdx == -1 {
		t.Fatal("install:mgmt block markers not found")
	}
	block := content[startIdx:endIdx]
	// Strip comment lines (leading whitespace + '#') from block
	var codeLines []string
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		codeLines = append(codeLines, line)
	}
	codeOnly := strings.Join(codeLines, "\n")
	// install:mgmt code must NOT use --selector with helmfile sync
	if strings.Contains(codeOnly, "helmfile") && strings.Contains(codeOnly, "--selector") {
		t.Errorf("install:mgmt uses --selector with helmfile — must use master sync per EC-01")
	}
}

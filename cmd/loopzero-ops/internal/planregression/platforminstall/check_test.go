// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platforminstall

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
)

// codeLine is one non-comment line of a file, with its 1-based line number in
// that file so a failure points at the line an editor shows.
type codeLine struct {
	number int
	text   string
}

// readNonCommentLines returns the lines of a file with comment-only lines
// dropped and trailing inline comments stripped — the code, not the prose
// explaining it.
func readNonCommentLines(t *testing.T, path string) []codeLine {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var lines []codeLine
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	number := 0
	for scanner.Scan() {
		number++
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
		lines = append(lines, codeLine{number: number, text: line})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	return lines
}

// The platform is installed by `helmfile sync`, which is what orders releases
// through `needs:`. A bare `helm upgrade --install` in the taskfile bypasses
// that ordering and the Helmfile source of truth.
func TestInfraTaskfileHasNoBareHelmInstall(t *testing.T) {
	lines := readNonCommentLines(t, taskfilePath)
	bareHelmRe := regexp.MustCompile(`\bhelm[[:space:]]+upgrade[[:space:]]+--install\b`)
	bareHelm2Re := regexp.MustCompile(`\bhelm[[:space:]]+install[[:space:]]`)
	for _, line := range lines {
		if bareHelmRe.MatchString(line.text) || bareHelm2Re.MatchString(line.text) {
			t.Errorf("infra.yml:%d: bare helm install bypasses helmfile sync: %s", line.number, line.text)
		}
	}
}

// `base64 -d` is rejected by BSD base64 (macOS); secret reads must decode with
// `kubectl -o go-template` or `openssl base64 -d -A`. Go RE2 has no lookahead,
// so the portable forms are excluded by inspecting the line.
func TestInfraTaskfileDecodesBase64Portably(t *testing.T) {
	lines := readNonCommentLines(t, taskfilePath)
	// Match `| base64 -d` (pipe + base64 -d) which is the canonical non-portable form.
	bareBase64Re := regexp.MustCompile(`\|[[:space:]]*base64[[:space:]]+-d\b`)
	for _, line := range lines {
		if !bareBase64Re.MatchString(line.text) {
			continue
		}
		// `openssl base64 -d` and `base64 -d -A` are portable across BSD and GNU.
		if strings.Contains(line.text, "openssl base64 -d") || strings.Contains(line.text, "base64 -d -A") {
			continue
		}
		t.Errorf("infra.yml:%d: bare `| base64 -d` fails on BSD base64 (macOS): %s", line.number, line.text)
	}
}

// Every cluster's platform is installed from its helmfile.
func TestInfraTaskfileInstallsEveryClusterThroughHelmfile(t *testing.T) {
	data, err := os.ReadFile(taskfilePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "helmfile -f infra/helmfile/core.helmfile.yaml.gotmpl") {
		t.Errorf("Taskfile should invoke helmfile sync for mgmt cluster")
	}
	if !strings.Contains(content, "helmfile -f infra/helmfile/build.helmfile.yaml.gotmpl") {
		t.Errorf("Taskfile should invoke helmfile sync for build cluster")
	}
	if !strings.Contains(content, "helmfile -f infra/helmfile/cell.helmfile.yaml.gotmpl") {
		t.Errorf("Taskfile should invoke helmfile sync for runtime cluster")
	}
}

// Secret reads decode through a portable decoder (kubectl go-template or
// openssl), never the platform-specific base64 binary.
func TestInfraTaskfileUsesAPortableSecretDecoder(t *testing.T) {
	data, err := os.ReadFile(taskfilePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	hasGoTemplate := strings.Contains(content, "go-template='{{`{{.data.")
	hasOpenssl := strings.Contains(content, "openssl base64 -d -A")
	if !hasGoTemplate && !hasOpenssl {
		t.Errorf("Taskfile should use kubectl go-template OR openssl for base64 decode (BSD/GNU portability)")
	}
}

// The Kratos DSN embeds a gotmpl `requiredEnv "..."` call; only single-quoted
// YAML lets that double quote through unescaped.
func TestKratosValuesSingleQuoteTheDSN(t *testing.T) {
	data, err := os.ReadFile(kratosValues)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	// The new (fixed) form has single-quoted YAML
	if !strings.Contains(content, "dsn: 'postgres://loopzero:") {
		t.Errorf("kratos values.yaml.gotmpl should use single-quoted YAML for DSN ")
	}
	// The broken form has `\"` escape inside double-quoted YAML — must not exist
	if strings.Contains(content, `requiredEnv \"LOOPZERO_DB_PASSWORD\"`) {
		t.Errorf("kratos values.yaml.gotmpl still has broken `\\\"` escape ")
	}
}

// Same constraint as the Kratos DSN.
func TestHydraValuesSingleQuoteTheDSN(t *testing.T) {
	data, err := os.ReadFile(hydraValues)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "dsn: 'postgres://loopzero:") {
		t.Errorf("hydra values.yaml.gotmpl should use single-quoted YAML for DSN ")
	}
	if strings.Contains(content, `requiredEnv \"LOOPZERO_DB_PASSWORD\"`) {
		t.Errorf("hydra values.yaml.gotmpl still has broken `\\\"` escape ")
	}
}

// infra:install:core syncs the whole core helmfile at once: a per-release
// `--selector` breaks `needs:` ordering. The scan stops at the next task,
// infra:install:core-dashboard-only, which uses a selector on purpose.
// Comment lines are ignored so documentation may mention --selector.
func TestInstallCoreSyncsTheWholeHelmfile(t *testing.T) {
	data, err := os.ReadFile(taskfilePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	startMarker := "infra:install:core:"
	endMarker := "infra:install:core-dashboard-only:"
	startIdx := strings.Index(content, startMarker)
	endIdx := strings.Index(content, endMarker)
	if startIdx == -1 || endIdx == -1 {
		t.Fatal("infra:install:core block markers not found")
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
	if strings.Contains(codeOnly, "helmfile") && strings.Contains(codeOnly, "--selector") {
		t.Errorf("infra:install:core uses --selector with helmfile — it must sync the whole helmfile so needs: ordering holds")
	}
}

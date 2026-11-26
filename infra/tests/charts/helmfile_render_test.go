// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
)

// helmfileRun is the one place in this package that executes helmfile without a
// network. Every invocation carries `--skip-deps` (B-156): without it `helmfile`
// runs `helm repo add/update` against every remote repository the file declares,
// even with a selector pointing at one release.
//
// Two commands are network-free with the flag, and callers pick by what they need
// to prove:
//
//   - `template` for a LOCAL chart (charts/ or infra/helm/): the manifests helm
//     renders, byte for byte.
//   - `write-values` for a REMOTE chart (ory/kratos, grafana/loki, ...): the merged
//     values Helmfile hands to the chart. Rendering a remote chart downloads it, and
//     a test that needs a registry to answer is a test that blocks a delivery on the
//     network rather than on the content.
//
// extraEnv adds (never replaces) process environment: the helmfiles read
// `requiredEnv` for LOOP_RUNTIME_DB_PASSWORD and LOOP_RUNTIME_CELL_CONTEXT, and a test must say
// which it needed instead of inheriting whatever the shell exported.
func helmfileRun(t *testing.T, extraEnv []string, args ...string) string {
	t.Helper()
	out, stderr, err := helmfileRunResult(t, extraEnv, args...)
	if err != nil {
		_, howToRead := ClassifyRenderFailure(stderr)
		t.Fatalf("helmfile %s failed: %v\n%s\n\n%s", strings.Join(args, " "), err, truncate(stderr, 1500), howToRead)
	}
	return out
}

// helmfileRunResult runs helmfile like helmfileRun and hands back the failure instead
// of failing the test, for a test whose point is that the render refuses its input.
func helmfileRunResult(t *testing.T, extraEnv []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	full := append([]string{}, args[:len(args)-1]...)
	full = append(full, "--skip-deps", args[len(args)-1])
	cmd := exec.Command("helmfile", full...)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), extraEnv...)
	// Manifests only: helmfile narrates ("Templating release=...") on stderr, and
	// that narration is not a YAML document.
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	return string(out), errBuf.String(), err
}

// helmfileTemplate renders one release of a LOCAL chart. localChart is the
// chart's directory relative to the repository root: its dependencies (the
// file:// loop-runtime-common library, for the published charts) are resolved first
// through chartrender, because `--skip-deps` also skips `helm dependency build`
// and a fresh clone has no charts/ directory to render from.
func helmfileTemplate(t *testing.T, helmfile, env, release, localChart string, extraEnv ...string) string {
	t.Helper()
	if _, err := chartrender.NewOptions(filepath.Join(repoRoot(t), localChart), ""); err != nil {
		t.Fatalf("resolve the dependencies of %s: %v", localChart, err)
	}
	return helmfileRun(t, extraEnv,
		"-f", "infra/helmfile/"+helmfile, "-e", env, "--selector", "name="+release, "template")
}

// helmfileValues returns the merged values Helmfile passes to one release — the
// network-free proof for a release whose chart lives in a remote repository.
func helmfileValues(t *testing.T, helmfile, env, release string, extraEnv ...string) map[string]any {
	t.Helper()
	dir := t.TempDir()
	helmfileRun(t, extraEnv,
		"-f", "infra/helmfile/"+helmfile, "-e", env, "--selector", "name="+release,
		"--output-file-template", filepath.Join(dir, "{{ .Release.Name }}.yaml"), "write-values")
	b, err := os.ReadFile(filepath.Join(dir, release+".yaml"))
	if err != nil {
		t.Fatalf("read the values helmfile wrote for %s: %v", release, err)
	}
	var values map[string]any
	if err := yaml.Unmarshal(b, &values); err != nil {
		t.Fatalf("decode the values helmfile wrote for %s: %v", release, err)
	}
	return values
}

// renderedObject returns the first rendered document of the given kind and name,
// or nil. It decodes the documents instead of matching text, so a key in a comment
// or in a neighbouring object cannot satisfy an assertion.
func renderedObject(t *testing.T, rendered, kind, name string) map[string]any {
	t.Helper()
	for _, doc := range renderedObjectsOfKind(t, rendered, kind) {
		if dig(doc, "metadata", "name") == name {
			return doc
		}
	}
	return nil
}

// renderedObjectsOfKind returns every rendered document of the given kind.
func renderedObjectsOfKind(t *testing.T, rendered, kind string) []map[string]any {
	t.Helper()
	var found []map[string]any
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	for {
		var doc any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return found
		}
		if err != nil {
			t.Fatalf("decode rendered manifests: %v", err)
		}
		if m, ok := doc.(map[string]any); ok && m["kind"] == kind {
			found = append(found, m)
		}
	}
}

// dig walks a decoded YAML tree by map keys and list indexes written as ints.
// It returns nil for any path that does not exist.
func dig(node any, path ...any) any {
	for _, step := range path {
		switch key := step.(type) {
		case string:
			m, ok := node.(map[string]any)
			if !ok {
				return nil
			}
			node = m[key]
		case int:
			l, ok := node.([]any)
			if !ok || key >= len(l) {
				return nil
			}
			node = l[key]
		default:
			return nil
		}
	}
	return node
}

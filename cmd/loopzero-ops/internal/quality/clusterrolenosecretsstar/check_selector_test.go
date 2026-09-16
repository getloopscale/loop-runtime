// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// RED tests for B-048: the gate selects files by BASENAME
// (check.go:87 `filepath.Base(path) != "clusterrole.yaml"`), not by the
// `kind:` of the declarations inside them. Nine ClusterRole declarations
// exist under charts/ and infra/helm/; the gate opens the five that happen
// to live in a file called clusterrole.yaml.
//
// These tests FAIL on the current state. They are the measurement, not the
// fix — nothing here widens the selector.
package clusterrolenosecretsstar

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// clusterRoleDecl matches a ClusterRole DECLARATION: `kind: ClusterRole` at
// column 0. An indented `kind: ClusterRole` is a roleRef inside a
// ClusterRoleBinding and declares nothing.
var clusterRoleDecl = regexp.MustCompile(`(?m)^kind:[ \t]*['"]?ClusterRole['"]?[ \t]*(#.*)?$`)

// repoRoot walks up to the directory holding CLAUDE.md.
func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for cwd != "/" {
		if _, err := os.Stat(filepath.Join(cwd, "CLAUDE.md")); err == nil {
			return cwd
		}
		cwd = filepath.Dir(cwd)
	}
	t.Skip("CLAUDE.md not found — cannot resolve repo root")
	return ""
}

// liveClusterRoleFiles returns every file under the gate's own scanDirs that
// declares at least one ClusterRole, derived by `kind:` rather than by name.
func liveClusterRoleFiles(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	for _, dir := range scanDirs {
		abs := filepath.Join(root, dir)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			continue
		}
		err := filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			b, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			if clusterRoleDecl.Match(b) {
				rel, _ := filepath.Rel(root, path)
				found = append(found, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	return found
}

// RED 1 — a ClusterRole carrying an unguarded cluster-wide secrets grant is
// invisible to the gate purely because its file is not called
// clusterrole.yaml. Hermetic: proves the selector defect in isolation.
// ESTES TESTES SAO RED POR DESENHO ate' B-048 ser implementado.
//
// Medem o defeito, nao o conserto. Ficam SKIP pelo mesmo argumento que os de B-001
// em `internal/quality/chartpairs`: um teste permanentemente vermelho e' ignorado
// dentro de uma semana e leva a suite junto (`rules/testing.md`). Um skip que NOMEIA
// o item e a medicao e' o oposto do skip invisivel que a mesma regra proibe.
//
// `LOOPZERO_RUN_B048_RED=1 go test ./internal/quality/clusterrolenosecretsstar/` roda-os.
// Quando B-048 ficar verde, apague este bloco — se passarem sem a variavel, chegou.
func skipUnlessRedB048(t *testing.T) {
	t.Helper()
	if os.Getenv("LOOPZERO_RUN_B048_RED") == "" {
		t.Skip("DEBITO CONHECIDO B-048: o gate seleciona por BASENAME do arquivo e nao " +
			"pelo `kind:` das declaracoes — nove ClusterRole existem sob charts/ e " +
			"infra/helm/, e o gate abre as cinco que calham de morar num clusterrole.yaml. " +
			"Rode com LOOPZERO_RUN_B048_RED=1 para ver a medicao.")
	}
}

func TestCheck_ClusterRoleInNonCanonicalFilename_FAIL(t *testing.T) {
	skipUnlessRedB048(t)
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "infra", "helm", "some-chart", "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Byte-for-byte the shape of infra/helm/loopzero-api/templates/serviceaccount.yaml:
	// a ServiceAccount, then a ClusterRole with an unconditional core-group
	// secrets+configmaps grant including delete. No helm guard anywhere.
	content := `apiVersion: v1
kind: ServiceAccount
metadata:
  name: loopzero-api
  namespace: loopzero-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: loopzero-api
rules:
  - apiGroups: [""]
    resources: [namespaces, configmaps, secrets, services, pods, events]
    verbs: [get, list, watch, create, update, patch, delete]
`
	if err := os.WriteFile(filepath.Join(dir, "serviceaccount.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatalf("B-048: gate PASSed a ClusterRole with an unconditional cluster-wide " +
			"secrets+configmaps grant, because the declaration lives in " +
			"serviceaccount.yaml rather than clusterrole.yaml. The rule is about the " +
			"kind, not the filename.")
	}
}

// RED 2 — selection must be rename-invariant. For every file in the live tree
// that declares a ClusterRole, the verdict must not depend on what the file is
// called. Any divergence is coverage the gate silently does not have.
func TestCheck_LIVE_SelectionIsRenameInvariant(t *testing.T) {
	skipUnlessRedB048(t)
	root := repoRoot(t)
	files := liveClusterRoleFiles(t, root)
	if len(files) == 0 {
		t.Skip("no ClusterRole declarations found — nothing to compare")
	}
	t.Logf("B-048: %d files under %v declare a ClusterRole", len(files), scanDirs)

	var divergent []string
	for _, rel := range files {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		// Same bytes, same directory depth, two names.
		underOwnName := runOne(t, filepath.Base(rel), body)
		underCanonical := runOne(t, "clusterrole.yaml", body)
		if underOwnName != underCanonical {
			divergent = append(divergent, rel)
			t.Errorf("B-048: %s — verdict depends on the FILENAME: "+
				"pass=%v as %q, pass=%v as clusterrole.yaml",
				rel, underOwnName, filepath.Base(rel), underCanonical)
		}
	}
	if len(divergent) > 0 {
		t.Errorf("B-048: %d of %d ClusterRole-declaring files are judged by their name "+
			"rather than their content: %s", len(divergent), len(files),
			strings.Join(divergent, ", "))
	}
}

// runOne writes body under the given basename into a throwaway chart and
// returns the gate's Pass verdict.
func runOne(t *testing.T, basename string, body []byte) bool {
	t.Helper()
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "charts", "probe", "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, basename), body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return out.Pass
}

// RED 3 — the specific live exposure. The bootstrap chart's ClusterRole grants
// cluster-wide secrets+configmaps with delete, unconditionally, and the gate
// reports PASS over the whole repository.
func TestCheck_LIVE_FlagsLoopZeroApiBootstrapSecretsGrant(t *testing.T) {
	skipUnlessRedB048(t)
	root := repoRoot(t)
	const target = "infra/helm/loopzero-api/templates/serviceaccount.yaml"
	if _, err := os.Stat(filepath.Join(root, target)); err != nil {
		t.Skipf("%s absent: %v", target, err)
	}

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, h := range out.Hits {
		if filepath.ToSlash(h.File) == target {
			return // the gate sees it — this test would then be GREEN
		}
	}
	t.Fatalf("B-048: %s declares a ClusterRole granting apiGroups [\"\"] "+
		"resources [namespaces configmaps secrets services pods events] with delete, "+
		"under no helm guard — and the gate does not flag it. Gate verdict: pass=%v, "+
		"%d hit(s) across the repository.", target, out.Pass, len(out.Hits))
}

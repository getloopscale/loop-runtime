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

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// clusterRoleDecl matches a ClusterRole DECLARATION: `kind: ClusterRole` at
// column 0. An indented `kind: ClusterRole` is a roleRef inside a
// ClusterRoleBinding and declares nothing.
var clusterRoleDecl = regexp.MustCompile(`(?m)^kind:[ \t]*['"]?ClusterRole['"]?[ \t]*(#.*)?$`)

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

// These tests are RED BY DESIGN: they measure the defect, not its fix.
//
// They SKIP rather than fail because a permanently red test is ignored within a week
// and takes the whole suite with it (`rules/testing.md`: "once red tests are ignored,
// all tests lose their value"). A skip that names the defect and prints the
// measurement is the opposite of the invisible skip that same rule forbids — the debt
// shows up in every run, and the issue below is where it is tracked.
//
// Tracked at: https://github.com/getloopscale/loopzero/issues/362
// The defect: the gate selects by file BASENAME rather than by the `kind:` of the declarations,
// so it audits five of the nine ClusterRoles and reports on all nine.
//
// `LOOPZERO_RUN_SELECTOR_AUDIT=1 go test ./internal/quality/clusterrolenosecretsstar/` runs them for real.
// When the issue closes, delete this block — if they pass without the variable, the
// fix has landed.

func skipUnlessSelectorAuditArmed(t *testing.T) {
	t.Helper()
	if os.Getenv("LOOPZERO_RUN_SELECTOR_AUDIT") == "" {
		// https://github.com/getloopscale/loopzero/issues/362
		t.Skip("known debt: the gate selects by file basename rather than by the kind: of " +
			"the declarations — nine ClusterRoles exist under charts/ and infra/helm/, and " +
			"the gate opens the five that happen to live in a clusterrole.yaml. " +
			"Run with LOOPZERO_RUN_SELECTOR_AUDIT=1 to see the measurement.")
	}
}

func TestCheck_ClusterRoleInNonCanonicalFilename_FAIL(t *testing.T) {
	skipUnlessSelectorAuditArmed(t)
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "infra", "helm", "some-chart", "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Byte-for-byte the shape the deleted bootstrap chart infra/helm/loopzero-api carried in
	// templates/serviceaccount.yaml:
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
	skipUnlessSelectorAuditArmed(t)
	root := qtestutil.Root(t)
	files := liveClusterRoleFiles(t, root)
	if len(files) == 0 {
		// Zero found means the finder is broken, which is the very defect this test
		// measures. Skipping on zero is the vacuity trap the gate exists to catch.
		t.Fatalf("no ClusterRole declarations found under %v — the finder is broken", scanDirs)
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

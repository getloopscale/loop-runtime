// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package servicesacyclic

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/subpackagefence"
)

func writeGo(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func importing(pkg string, contexts ...string) string {
	body := "package " + pkg + "\n\n"
	for _, c := range contexts {
		body += "import _ \"" + servicesPkgPath + "/" + c + "\"\n"
	}
	return body
}

// legalTree is the frozen DAG in miniature: build reaches release,
// everything else is self-contained.
func legalTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/build/b.go", importing("build", "release"))
	writeGo(t, root, "api/internal/services/release/r.go", importing("release"))
	writeGo(t, root, "api/internal/services/placement/p.go", importing("placement"))
	return root
}

func TestCheck_LegalGraphPasses(t *testing.T) {
	out, err := Check(Config{RepoRoot: legalTree(t)})

	if err != nil {
		t.Fatalf("the frozen DAG must pass: %v edges=%+v", err, out.Edges)
	}
}

func TestCheck_ReverseEdgeFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "api/internal/services/release/back.go", importing("release", "build"))

	out, err := Check(Config{RepoRoot: root})

	if !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Fatalf("release → build is the reverse edge the split removed; err = %v", err)
	}
	if out.Edges[0].From != "release" || out.Edges[0].To != "build" {
		t.Errorf("want release → build, got %+v", out.Edges[0])
	}
}

func TestCheck_CrossEdgeBetweenSelfContainedContextsFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "api/internal/services/placement/x.go", importing("placement", "release"))

	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Fatalf("placement → release is not in the allow-list; err = %v", err)
	}
}

func TestCheck_UnregisteredContextFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "api/internal/services/newthing/n.go", importing("newthing"))

	out, err := Check(Config{RepoRoot: root})

	if !errors.Is(err, subpackagefence.ErrUnregisteredSubpackage) {
		t.Fatalf("an unregistered context must fail; err = %v", err)
	}
	if len(out.Unregistered) != 1 || out.Unregistered[0] != "newthing" {
		t.Errorf("Unregistered = %v, want [newthing]", out.Unregistered)
	}
}

func TestCheck_ForbiddenEdgeInATestFileFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "api/internal/services/release/r_test.go", importing("release", "build"))

	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Fatalf("a test import is still a build edge; err = %v", err)
	}
}

func TestCheck_NestedPackageDoesNotEscapeItsContext(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "api/internal/services/release/internal/helper/h.go", importing("helper", "build"))

	out, err := Check(Config{RepoRoot: root})

	if !errors.Is(err, subpackagefence.ErrForbiddenEdge) || out.Edges[0].From != "release" {
		t.Fatalf("nesting must not bypass the fence; err = %v edges = %+v", err, out.Edges)
	}
}

// An empty directory is not a context: it survives on the disk of whoever built
// before and does not exist in a fresh clone, so counting it would give one commit
// two verdicts. Measured when billing's code left for loopzero-cloud.
func TestCheck_EmptyDirectoryIsNotAnUnknownContext(t *testing.T) {
	root := legalTree(t)
	if err := os.MkdirAll(filepath.Join(root, "api/internal/services/billing"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Check(Config{RepoRoot: root}); err != nil {
		t.Fatalf("an empty directory must not fail the gate: %v", err)
	}
}

func TestCheck_MissingServicesRootFails(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/other/x.go", "package other\n")

	if _, err := Check(Config{RepoRoot: root}); err == nil {
		t.Fatal("a services root renamed away must fail, not pass over nothing")
	}
}

func TestCheck_EmptyServicesRootFails(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, servicesRoot), 0o750); err != nil {
		t.Fatal(err)
	}

	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, subpackagefence.ErrNothingParsed) {
		t.Fatalf("an empty services root means the decomposition vanished; err = %v", err)
	}
}

func TestRegistryIsAcyclic(t *testing.T) {
	if err := subpackagefence.ValidateRegistry(fence()); err != nil {
		t.Fatalf("the committed registry must validate: %v", err)
	}
}

func TestCheck_RealTreePasses(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.Root(t)})

	if err != nil {
		t.Fatalf("the real tree must pass: %v unregistered=%v edges=%+v", err, out.Unregistered, out.Edges)
	}
}

// A registered context with no tracked Go code pre-approves the decision this gate
// exists to force: whoever creates the directory later starts importing without
// being asked. The check uses `git ls-files`, not the disk, because an empty
// directory exists on one machine and not in a fresh clone.
func TestEveryRegisteredContextHasTrackedCode(t *testing.T) {
	root := qtestutil.Root(t)
	for context := range allowed {
		dir := filepath.Join(servicesRoot, context)
		out, err := exec.Command("git", "-C", root, "ls-files", "--", dir).Output()
		if err != nil {
			t.Fatalf("git ls-files %s: %v", dir, err)
		}
		tracked := 0
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.HasSuffix(line, ".go") {
				tracked++
			}
		}
		if tracked == 0 {
			t.Errorf("the registry names context %q and %s tracks no .go file — a phantom context "+
				"pre-approves the decision this gate exists to force", context, dir)
		}
	}
}

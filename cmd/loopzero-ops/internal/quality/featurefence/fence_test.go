// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package featurefence

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/subpackagefence"
)

func writeGo(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, internalDir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func importing(pkg string, targets ...string) string {
	body := "package " + pkg + "\n\n"
	for _, t := range targets {
		body += "import _ \"" + internalPkgPath + "/" + t + "\"\n"
	}
	return body
}

// miniRegistry is the layout in miniature: two features where one depends on
// the other, a kernel pair, a composition root and a test-only kernel leaf.
func miniRegistry() map[string]Entry {
	return map[string]Entry{
		"build":      {Kind: Feature, Deps: []string{"release"}},
		"release":    {Kind: Feature},
		"db":         {Kind: Kernel, Deps: []string{"metrics"}},
		"metrics":    {Kind: Kernel},
		"routestest": {Kind: Kernel, TestOnlyLeaf: true},
		"server":     {Kind: Composition},
		"archtest":   {Kind: TestOnly},
	}
}

func legalTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeGo(t, root, "build/b.go", importing("build", "release", "db"))
	writeGo(t, root, "build/buildhttp/h.go", importing("buildhttp", "build", "metrics"))
	writeGo(t, root, "build/buildhttp/h_test.go", importing("buildhttp", "routestest"))
	writeGo(t, root, "build/buildadapter/a.go", importing("buildadapter", "build", "build/buildhttp", "db", "release"))
	writeGo(t, root, "release/r.go", importing("release", "db"))
	writeGo(t, root, "release/promotehttp/p.go", importing("promotehttp", "release"))
	writeGo(t, root, "db/d.go", importing("db", "metrics"))
	writeGo(t, root, "metrics/m.go", importing("metrics"))
	writeGo(t, root, "routestest/r.go", importing("routestest"))
	writeGo(t, root, "server/s.go", importing("server", "build/buildhttp", "build/buildadapter", "release/promotehttp"))
	writeGo(t, root, "archtest/a_test.go", importing("archtest"))
	return root
}

// check runs the fence over root and returns what it refused. A layout that
// breaks a rule is a refusal on the outcome, never an error: an error fails the
// test here, so no case below can pass on a scan that did not complete.
func check(t *testing.T, root string) (subpackagefence.Outcome, error) {
	t.Helper()
	out, err := Check(Config{RepoRoot: root, Registry: miniRegistry()})
	if err != nil {
		t.Fatalf("the check could not complete: %v", err)
	}
	return out, out.Violation
}

func TestCheck_LegalLayoutPasses(t *testing.T) {
	out, err := check(t, legalTree(t))
	if err != nil {
		t.Fatalf("the legal layout must pass: %v edges=%+v", err, out.Edges)
	}
}

func TestCheck_KernelImportingAFeatureFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "db/x.go", importing("db", "release"))

	if _, err := check(t, root); !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Fatalf("a kernel package reaching a feature must fail; err = %v", err)
	}
}

func TestCheck_FeatureImportingAnUndeclaredFeatureFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "release/x.go", importing("release", "build"))

	if _, err := check(t, root); !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Fatalf("release → build is not declared; err = %v", err)
	}
}

func TestCheck_UnregisteredTopLevelDirectoryFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "stray/s.go", importing("stray"))

	if _, err := check(t, root); !errors.Is(err, subpackagefence.ErrUnregisteredSubpackage) {
		t.Fatalf("an unregistered directory must fail; err = %v", err)
	}
}

func TestCheck_FeatureRootImportingItsOwnHTTPFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "build/x.go", importing("build", "build/buildhttp"))

	_, err := check(t, root)
	if !errors.Is(err, subpackagefence.ErrForbiddenEdge) || !errors.Is(err, ErrHTTPReached) {
		t.Fatalf("a feature root depending on its transport must fail with ErrHTTPReached; err = %v", err)
	}
}

func TestCheck_AnotherFeatureImportingAnHTTPPackageFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "build/y.go", importing("build", "release/promotehttp"))

	if _, err := check(t, root); !errors.Is(err, ErrHTTPReached) {
		t.Fatalf("a feature reaching another feature's handlers must fail; err = %v", err)
	}
}

func TestCheck_HTTPPackageImportingAnotherFeatureFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "build/buildhttp/x.go", importing("buildhttp", "release"))

	if _, err := check(t, root); !errors.Is(err, ErrHTTPReachesOut) {
		t.Fatalf("a handler reaching around its feature must fail; err = %v", err)
	}
}

func TestCheck_TwoHTTPPackagesOfOneFeatureStayApart(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "release/rollbackhttp/r.go", importing("rollbackhttp", "release/promotehttp"))

	if _, err := check(t, root); !errors.Is(err, ErrHTTPReached) {
		t.Fatalf("one handler family importing another must fail; err = %v", err)
	}
}

func TestCheck_AdapterImportedByFeatureCodeFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "build/buildhttp/z.go", importing("buildhttp", "build/buildadapter"))

	if _, err := check(t, root); !errors.Is(err, ErrAdapterReached) {
		t.Fatalf("only a composition root may import an adapter package; err = %v", err)
	}
}

func TestCheck_AdapterNotNamedAfterItsFeatureFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "build/storeadapter/s.go", importing("storeadapter", "build"))

	if _, err := check(t, root); !errors.Is(err, ErrMisplacedRole) {
		t.Fatalf("an adapter package must be named <feature>adapter; err = %v", err)
	}
}

func TestCheck_NestedHTTPPackageFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "build/argo/argohttp/a.go", importing("argohttp", "build"))

	if _, err := check(t, root); !errors.Is(err, ErrMisplacedRole) {
		t.Fatalf("an HTTP package must sit directly under its feature; err = %v", err)
	}
}

func TestCheck_TestOnlyLeafFromProductionFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "build/buildhttp/w.go", importing("buildhttp", "routestest"))

	if _, err := check(t, root); !errors.Is(err, subpackagefence.ErrTestOnlyLeafFromProduction) {
		t.Fatalf("production code reaching a test-only leaf must fail; err = %v", err)
	}
}

func TestCheck_TestHarnessImportedFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "server/t.go", importing("server", "archtest"))

	if _, err := check(t, root); !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Fatalf("nothing may import a test harness; err = %v", err)
	}
}

func TestCheck_RegistryPermittingACycleIsRefused(t *testing.T) {
	reg := miniRegistry()
	reg["release"] = Entry{Kind: Feature, Deps: []string{"build"}}

	out, err := Check(Config{RepoRoot: legalTree(t), Registry: reg})
	if err != nil || !errors.Is(out.Violation, subpackagefence.ErrRegistryCycle) {
		t.Fatalf("a registry with build ⇄ release must be refused; err = %v", err)
	}
}

func TestCheck_EmptyTreeExaminedNothing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, internalDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := check(t, root); !errors.Is(err, subpackagefence.ErrNothingParsed) {
		t.Fatalf("a tree with no Go file must not pass; err = %v", err)
	}
}

func TestRegistry_IsCoherentAndAcyclic(t *testing.T) {
	f, err := Config{}.Fence()
	if err != nil {
		t.Fatalf("building the fence: %v", err)
	}
	if err := subpackagefence.ValidateRegistry(f); err != nil {
		t.Fatalf("the shipped registry must be coherent and acyclic: %v", err)
	}
}

func TestRegistry_KernelNamesNoFeature(t *testing.T) {
	for name, e := range registry {
		if e.Kind != Kernel {
			continue
		}
		for _, d := range e.Deps {
			if registry[d].Kind != Kernel {
				t.Errorf("kernel package %s names %s, which is not kernel", name, d)
			}
		}
	}
}

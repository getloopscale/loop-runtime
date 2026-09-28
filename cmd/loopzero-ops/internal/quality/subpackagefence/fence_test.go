// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package subpackagefence

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

const pkg = "example.com/app/internal/svc"

func fence() Fence {
	return Fence{
		Dir:     "internal/svc",
		PkgPath: pkg,
		Allowed: map[string]map[string]bool{
			"build":   {"release": true, "httpio": true, "testkit": true},
			"release": {"httpio": true, "testkit": true},
			"httpio":  {},
			"testkit": {},
		},
		Leaves:         map[string]bool{"httpio": true, "testkit": true},
		TestOnlyLeaves: map[string]bool{"testkit": true},
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func importing(pkgName string, paths ...string) string {
	var b strings.Builder
	b.WriteString("package " + pkgName + "\n\n")
	for _, p := range paths {
		b.WriteString("import _ \"" + p + "\"\n")
	}
	return b.String()
}

func legalTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "internal/svc/build/b.go", importing("build", pkg+"/release", pkg+"/httpio"))
	write(t, root, "internal/svc/release/r.go", importing("release", pkg+"/httpio"))
	write(t, root, "internal/svc/release/r_test.go", importing("release", pkg+"/testkit"))
	write(t, root, "internal/svc/httpio/h.go", importing("httpio"))
	write(t, root, "internal/svc/testkit/k.go", importing("testkit"))
	write(t, root, "internal/svc/svc.go", importing("svc", pkg+"/httpio"))
	return root
}

func TestCheck_LegalTreePasses(t *testing.T) {
	out, err := Check(legalTree(t), fence())

	if err != nil || out.Verdict() != verdict.Pass {
		t.Fatalf("legal tree must pass: %v %v edges=%+v", err, out.Violation, out.Edges)
	}
	if out.FilesParsed != 6 {
		t.Errorf("FilesParsed = %d, want 6", out.FilesParsed)
	}
}

func TestCheck_ForbiddenSiblingEdgeFails(t *testing.T) {
	root := legalTree(t)
	write(t, root, "internal/svc/release/back.go", importing("release", pkg+"/build"))

	out, err := Check(root, fence())

	if err != nil || !errors.Is(out.Violation, ErrForbiddenEdge) {
		t.Fatalf("err = %v, want ErrForbiddenEdge", err)
	}
	if len(out.Edges) != 1 || out.Edges[0].From != "release" || out.Edges[0].To != "build" {
		t.Errorf("edges = %+v, want release → build", out.Edges)
	}
}

func TestCheck_NestedDirectoryDoesNotEscapeTheAllowList(t *testing.T) {
	root := legalTree(t)
	write(t, root, "internal/svc/release/internal/deep/d.go", importing("deep", pkg+"/build"))

	out, err := Check(root, fence())

	if err != nil || !errors.Is(out.Violation, ErrForbiddenEdge) || out.Edges[0].From != "release" {
		t.Fatalf("err = %v edges = %+v, want release → build from the nested package", err, out.Edges)
	}
}

func TestCheck_SubpackageImportingTheParentFails(t *testing.T) {
	root := legalTree(t)
	write(t, root, "internal/svc/release/up.go", importing("release", pkg))

	out, err := Check(root, fence())

	if err != nil || !errors.Is(out.Violation, ErrForbiddenEdge) || out.Edges[0].To != ParentNode {
		t.Fatalf("err = %v edges = %+v, want release → parent", err, out.Edges)
	}
}

func TestCheck_ParentImportingANonLeafFails(t *testing.T) {
	root := legalTree(t)
	write(t, root, "internal/svc/wire.go", importing("svc", pkg+"/build"))

	out, err := Check(root, fence())

	if err != nil || !errors.Is(out.Violation, ErrForbiddenEdge) || out.Edges[0].From != ParentNode {
		t.Fatalf("err = %v edges = %+v, want parent → build", err, out.Edges)
	}
}

func TestCheck_ProductionImportOfTestOnlyLeafIsItsOwnFailure(t *testing.T) {
	root := legalTree(t)
	write(t, root, "internal/svc/release/prod.go", importing("release", pkg+"/testkit"))

	out, err := Check(root, fence())

	if err != nil || !errors.Is(out.Violation, ErrTestOnlyLeafFromProduction) {
		t.Fatalf("err = %v, want ErrTestOnlyLeafFromProduction", err)
	}
	if !out.Edges[0].TestOnly {
		t.Errorf("edge = %+v, want TestOnly", out.Edges[0])
	}
}

func TestCheck_UnregisteredSubpackageFails(t *testing.T) {
	root := legalTree(t)
	write(t, root, "internal/svc/newthing/n.go", importing("newthing"))

	out, err := Check(root, fence())

	if err != nil || !errors.Is(out.Violation, ErrUnregisteredSubpackage) {
		t.Fatalf("err = %v, want ErrUnregisteredSubpackage", err)
	}
	if len(out.Unregistered) != 1 || out.Unregistered[0] != "newthing" {
		t.Errorf("Unregistered = %v, want [newthing]", out.Unregistered)
	}
}

func TestCheck_DirectoryWithoutGoIsNotASubpackage(t *testing.T) {
	root := legalTree(t)
	if err := os.MkdirAll(filepath.Join(root, "internal/svc/leftover"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := Check(root, fence())

	if err != nil || out.Violation != nil {
		t.Fatalf("an empty directory must not fail the fence: %v %v unregistered=%v", err, out.Violation, out.Unregistered)
	}
}

func TestCheck_EmptyRootIsNothingParsed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal/svc"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := Check(root, fence())

	if err != nil || !errors.Is(out.Violation, ErrNothingParsed) {
		t.Fatalf("err = %v, want ErrNothingParsed", err)
	}
}

func TestCheck_MissingRootIsAnError(t *testing.T) {
	_, err := Check(t.TempDir(), fence())

	if err == nil || !strings.Contains(err.Error(), "internal/svc") {
		t.Fatalf("err = %v, want an error naming the missing directory", err)
	}
}

func TestValidateRegistry_RefusesAnEmptyAllowList(t *testing.T) {
	err := ValidateRegistry(Fence{})

	if !errors.Is(err, ErrEmptyAllowList) {
		t.Fatalf("err = %v, want ErrEmptyAllowList", err)
	}
}

func TestValidateRegistry_RefusesACycle(t *testing.T) {
	f := fence()
	f.Allowed["release"]["build"] = true

	err := ValidateRegistry(f)

	if !errors.Is(err, ErrRegistryCycle) {
		t.Fatalf("err = %v, want ErrRegistryCycle", err)
	}
	if !strings.Contains(err.Error(), "build → release → build") {
		t.Errorf("err = %q, want the cycle spelled out", err)
	}
}

func TestValidateRegistry_RefusesASelfLoop(t *testing.T) {
	f := fence()
	f.Allowed["build"]["build"] = true

	if err := ValidateRegistry(f); !errors.Is(err, ErrRegistryCycle) {
		t.Fatalf("err = %v, want ErrRegistryCycle", err)
	}
}

func TestValidateRegistry_RefusesATargetNobodyRegistered(t *testing.T) {
	f := fence()
	f.Allowed["release"]["ghost"] = true

	err := ValidateRegistry(f)

	if !errors.Is(err, ErrIncoherentRegistry) || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v, want ErrIncoherentRegistry naming ghost", err)
	}
}

func TestValidateRegistry_RefusesALeafThatPermitsSiblings(t *testing.T) {
	f := fence()
	f.Allowed["httpio"] = map[string]bool{"testkit": true}

	if err := ValidateRegistry(f); !errors.Is(err, ErrIncoherentRegistry) {
		t.Fatalf("err = %v, want ErrIncoherentRegistry", err)
	}
}

func TestCheck_ACyclicRegistryFailsBeforeAnyFileIsRead(t *testing.T) {
	f := fence()
	f.Allowed["release"]["build"] = true

	out, err := Check(legalTree(t), f)

	if err != nil || !errors.Is(out.Violation, ErrRegistryCycle) {
		t.Fatalf("err = %v, want ErrRegistryCycle", err)
	}
}

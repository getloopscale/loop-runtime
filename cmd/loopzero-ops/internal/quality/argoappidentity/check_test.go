// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argoappidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDetectsAdHocConstruction uses the real shapes that existed before the fix,
// including the two inside the prune — which are the dangerous ones, because they decide
// what to delete with the cascade finalizer active.
func TestDetectsAdHocConstruction(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/tenant/tenant_gitops.go", `package tenant
func a() {
	appName := fmt.Sprintf("%s-%s", tenant.Name, env.Name)
	_ = appName
}
`)
	write(t, root, "api/internal/release/container_delivery.go", `package release
func b() {
	argoAppName := fmt.Sprintf("%s-%s", build.TenantID, build.Environment)
	_ = argoAppName
}
`)

	rep, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("both ad-hoc constructions must be found; got %d: %+v",
			len(rep.Findings), rep.Findings)
	}
}

// TestDetectsEnvironmentCRIdentity — #273: the Environment CR had the same shape and a
// worse consequence (the losing project ended up with no namespace at all). The gate covers both.
func TestDetectsEnvironmentCRIdentity(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/tenant/tenant_controller.go", `package tenant
func c() {
	envCRName := fmt.Sprintf("%s-%s", tenant.Name, env.Name)
	_ = envCRName
}
`)
	rep, _ := Check(root)
	if len(rep.Findings) != 1 {
		t.Errorf("the Environment CR identity is also in the gate's scope (#273); findings=%+v",
			rep.Findings)
	}
}

// TestDetectsGCDesiredSet — the `desired` set of the Environment CR GC decides what to
// DELETE, and deleting an Environment CR erases the tenant's namespace in the runtime cluster.
func TestDetectsGCDesiredSet(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/tenant/tenant_controller.go", `package tenant
func d() {
	desired[fmt.Sprintf("%s-%s", tenant.Name, env.Name)] = true
}
`)
	rep, _ := Check(root)
	if rep.OK() {
		t.Error("the set that protects from deletion must come from the canonical source (#273)")
	}
}

// TestDoesNotFlagReleaseAppName — release_convergence_loop.go builds "{project}-{app}"
// for the apps INSIDE a release. It has no tenant term, so it does not match.
func TestDoesNotFlagReleaseAppName(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/internal/release/release_convergence_loop.go", `package release
func d() {
	appRef := fmt.Sprintf("%s-%s", release.ProjectID, app.Name)
	_ = appRef
}
`)
	rep, _ := Check(root)
	if !rep.OK() {
		t.Errorf("an app name inside a release is not an Application name; findings=%+v", rep.Findings)
	}
}

// TestCommentIsNotAFinding — a comment citing the old shape is history.
func TestCommentIsNotAFinding(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/tenant/tenant_gitops.go", `package tenant
// Before #271 this was: appName := fmt.Sprintf("%s-%s", tenant.Name, env.Name)
func e() {}
`)
	rep, _ := Check(root)
	if !rep.OK() {
		t.Errorf("a historical comment is not a finding; findings=%+v", rep.Findings)
	}
}

// TestRealRepositoryPasses is the assertion that the fix actually landed.
func TestRealRepositoryPasses(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	rep, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Findings {
		t.Errorf("%s", f.Message())
	}
	if rep.Scanned == 0 {
		t.Error("no file scanned — the gate would pass vacuously")
	}
}

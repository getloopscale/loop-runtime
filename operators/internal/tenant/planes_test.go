// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The plane seam of this package.
//
// `operators/internal/tenant` is where more than one platform plane meets,
// and the grouping is defensible: the Tenant aggregate IS the right unit —
// a tenant really does need a repository, an image project and a bucket.
// What was missing was a seam saying WHICH plane each file serves (B-060).
//
// Without it the information exists only as tribal knowledge, and tribal
// knowledge ages without warning: the evidence that filed B-060 on
// 2026-08-11 claimed four planes, and five days later Transit provisioning
// left the operator (`e19e6dea5`, provisioning is api-side by ADR) with
// nothing in the package recording the change. A declaration checked
// against the code cannot age in silence — the tests below fail when it
// diverges.
//
// The declaration is NOT an interface per plane. The shape B-060's DoD
// proposed copying — the `gitea`/`argocd`/`minio`/`harbor` provisioners —
// is a narrow facade over an EXTERNAL service, and each one already exists.
// Repeating it here would produce single-implementer interfaces over
// internal code, the anti-pattern `rules/architecture.md` and Rule 11 name.
// The measured damage was not a missing abstraction; it was a missing seam.
//
// The declaration lives in this test file because nothing but these tests
// reads it; production code carries no data it never uses.
type plane string

const (
	// BUILD — what builds and stores artefacts: the tenant's bundle bucket
	// and image project.
	planeBuild plane = "BUILD"
	// CONTROL — what declares the desired state: the git repository and the
	// ArgoCD project that syncs it.
	planeControl plane = "CONTROL"
	// SECRETS — today only the half that stayed: `VaultClient` carries
	// `DeleteTenantKey` (crypto-shredding, B-075). Provisioning left on
	// 2026-08-16 and must not come back as tidying — see
	// `tenant_transit_ownership_test.go`, which locks the decision both ways.
	planeSecrets plane = "SECRETS"
)

// planeOfClient says which plane each `TenantReconciler` client belongs to.
// Keys are struct field names; a test checks that every client field of the
// struct appears here, so a new client cannot arrive without a plane.
var planeOfClient = map[string]plane{
	"MinIOProvisioner":  planeBuild,
	"HarborClient":      planeBuild,
	"GitProvisioner":    planeControl,
	"ArgoCDProvisioner": planeControl,
	"VaultClient":       planeSecrets,
}

// planesOfFile declares the planes each file of the package may touch,
// measured on 2026-08-18 from the real `r.<Client>` uses.
//
// Two files cross planes, on purpose — the list says why, because "crosses
// everything" with no written reason is the same as not declaring:
//
//   - `tenant_controller.go` is the orchestrator: it holds the clients and
//     calls the steps. Crossing planes is its job.
//   - `tenant_deletion.go` is the teardown: deleting a tenant undoes what each
//     plane created, so it crosses by symmetry with creation.
//
// A file with no client is pure logic and declares an empty list — also a
// checked claim: if it starts calling a client, the test fails until someone
// decides which plane that belongs to.
var planesOfFile = map[string][]plane{
	"registry_auth_merge.go":   {},
	"tenant_build.go":          {planeBuild},
	"tenant_controller.go":     {planeBuild, planeControl, planeSecrets},
	"tenant_credentials.go":    {},
	"tenant_deletion.go":       {planeBuild, planeControl},
	"tenant_gitops.go":         {planeControl},
	"tenant_harbor.go":         {planeBuild},
	"tenant_priority.go":       {},
	"misplaced_tenant_scan.go": {},
}

// declares reports whether a file declared the given plane.
func declares(declared []plane, p plane) bool {
	for _, d := range declared {
		if d == p {
			return true
		}
	}
	return false
}

// servicePackages are the operator's external-client packages. A
// `TenantReconciler` field whose type comes from one of them IS a plane client.
var servicePackages = map[string]bool{
	"argocd": true, "gitea": true, "minio": true, "miniop": true,
	"harbor": true, "vault": true,
}

// packageFiles lists the package's production .go files.
func packageFiles(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// A new file entering the package without a declared plane makes the seam
// cover less than it appears to. Declaring is mandatory; declaring an empty
// list (pure logic) is a valid, checked answer.
func TestPlaneSeam_EveryFileDeclaresItsPlanes(t *testing.T) {
	files := packageFiles(t)
	if len(files) == 0 {
		t.Fatal("zero files scanned: a seam over nothing passes vacuously")
	}
	for _, f := range files {
		if _, ok := planesOfFile[f]; !ok {
			t.Errorf("%s declares no plane in planesOfFile (planes_test.go)", f)
		}
	}
	for f := range planesOfFile {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("planesOfFile declares %s, which does not exist — a declaration that outlived its file", f)
		}
	}
}

// A new client in the struct with no entry in `planeOfClient` would be
// invisible to the next check: its uses would belong to no plane and pass by
// omission.
func TestPlaneSeam_EveryStructClientHasAPlane(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "tenant_controller.go", nil, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var fields []string
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "TenantReconciler" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, field := range st.Fields.List {
			sel, ok := field.Type.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || !servicePackages[pkg.Name] {
				continue
			}
			for _, name := range field.Names {
				fields = append(fields, name.Name)
			}
		}
		return false
	})
	if len(fields) == 0 {
		t.Fatal("no client found in the struct: the scan reached nothing")
	}
	for _, c := range fields {
		if _, ok := planeOfClient[c]; !ok {
			t.Errorf("field %s is an external service client with no plane in planeOfClient (planes_test.go)", c)
		}
	}
	for c := range planeOfClient {
		found := false
		for _, v := range fields {
			if v == c {
				found = true
			}
		}
		if !found {
			t.Errorf("planeOfClient declares %s, which is no longer a struct field", c)
		}
	}
}

var clientUse = regexp.MustCompile(`\br\.([A-Z][A-Za-z]*)\b`)

// The check that gives the two above their meaning: the declared plane must
// cover the clients the file actually calls. This is what stops the
// declaration from aging — moving a call to another plane without moving
// the declaration fails here.
func TestPlaneSeam_UsesStayWithinTheDeclaredPlanes(t *testing.T) {
	var uses int
	for _, f := range packageFiles(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		declared := planesOfFile[f]
		for _, m := range clientUse.FindAllStringSubmatch(string(src), -1) {
			p, isClient := planeOfClient[m[1]]
			if !isClient {
				continue
			}
			uses++
			if !declares(declared, p) {
				t.Errorf("%s uses r.%s (plane %s) but declares %v", f, m[1], p, declared)
			}
		}
	}
	// The floor: a regex that stopped matching would turn this test green
	// over nothing. Measured on 2026-08-18: 8 client uses in the real tree.
	if uses < 8 {
		t.Fatalf("only %d client uses scanned; the real tree had 8 on 2026-08-18 — "+
			"the scan shrank and green stopped meaning anything", uses)
	}
}

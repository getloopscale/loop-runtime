// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package packageadmission is the fitness function for the rule B-058 asks for:
// a package's admission criterion must be able to REFUSE.
//
// # The defect, measured
//
// B-058, 2026-08-11 and re-measured 2026-09-04. Three packages admitted members by
// something that is not a responsibility:
//
//   - api/internal/adapters admitted by PATTERN — "it is an adapter". Closed by the
//     sibling gate adapter-ports: every adapter now names, at compile time, the port
//     it satisfies.
//   - api/internal/k8s admitted by TECHNOLOGY — "it uses client-go". Measured on
//     2026-09-04: 7 non-test files, 1520 non-test lines, three responsibilities
//     sharing one directory because they share a client library. This gate closes it.
//   - api/internal/services/platform admitted by nothing this gate could express.
//     It was dissolved when api/internal was organized by feature (2026-09-25):
//     each of its concerns moved to the feature it serves.
//
// # The criterion this gate enforces
//
// api/internal/k8s is Kubernetes client PLUMBING: scheme registration, rest.Config
// resolution, typed client construction. Plumbing has no domain vocabulary. So a
// top-level file of that package may not name a responsibility — not in its file
// name, and not in any identifier it exports.
//
// That is the sentence the old criterion could not say. "It uses client-go" admits
// tenant_cr_manager.go, application_cr_manager.go and secret_k8s_manager.go alike;
// "plumbing names no domain" refuses all three, and refuses the next one too.
//
// # The floor
//
// Refusing is only half. If the three responsibilities were refused into nowhere the
// gate would go green over a dissolved package, which is how a guard outlives its own
// object (the lesson adapter-ports records). So the homes are named here and each one
// must exist and hold at least one exported type. A home that empties out FAILS.
//
// # The limit
//
// It checks that plumbing carries no domain vocabulary, not that each home holds the
// right domain. A file moved from tenancy to workload compiles and passes here. The
// import graph and the reviewer cover that half; this gate covers the half that has
// no other reader.
package packageadmission

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// ErrForeignDomain means a top-level file of the plumbing package names a responsibility.
var ErrForeignDomain = errors.New("plumbing package names a responsibility it should not admit")

// ErrHomeMissing means a declared home is absent or holds no exported type.
//
// Refusing a responsibility out of the plumbing package is only useful if it landed
// somewhere. A home that vanished would leave this gate green over the vacuum.
var ErrHomeMissing = errors.New("declared home is missing or holds no exported type")

// ErrNothingScanned means the governed package is absent, or held no file to read.
//
// Finding nothing is not the same as finding nothing wrong.
var ErrNothingScanned = errors.New("nothing scanned in the governed package")

// governed is the package held to the plumbing charter.
const governed = "api/internal/k8s"

// homes are the responsibility packages the B-058 split produced. Each must exist.
var homes = []string{"tenancy", "workload", "secretsync"}

// domainWords is the vocabulary that names a responsibility rather than plumbing.
//
// Measured from the population the audit found in this very package on 2026-08-11:
// tenancy (tenant_cr_manager.go, tenant_evictor.go), workload (application_cr_manager.go,
// preview_cr_manager.go) and secrets (secret_k8s_manager.go).
var domainWords = []string{"tenant", "application", "preview", "secret"}

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Violation is one piece of domain vocabulary found inside the plumbing package.
type Violation struct {
	File  string // file name, relative to the governed package
	Where string // "file name" or the exported identifier that carries the word
	Word  string // the domain word found
}

// Outcome reports what was established.
type Outcome struct {
	Scanned    int // top-level non-test files read
	Homes      map[string]int
	Violations []Violation
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

// domainWordIn returns the first domain word carried by s, or "".
func domainWordIn(s string) string {
	low := strings.ToLower(s)
	for _, w := range domainWords {
		if strings.Contains(low, w) {
			return w
		}
	}
	return ""
}

// exportedNames returns every exported top-level identifier a file declares:
// types, funcs, consts and vars. All four can carry a domain into a package.
func exportedNames(f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Recv != nil {
				continue // a method travels with its receiver type, already counted
			}
			if decl.Name.IsExported() {
				out = append(out, decl.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						out = append(out, s.Name.Name)
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.IsExported() {
							out = append(out, n.Name)
						}
					}
				}
			}
		}
	}
	return out
}

// goFiles lists the non-test .go files directly inside dir.
func goFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// countExportedTypes reports how many exported types a package directory declares.
func countExportedTypes(dir string) (int, error) {
	files, err := goFiles(dir)
	if err != nil {
		return 0, err
	}
	fset := token.NewFileSet()
	n := 0
	for _, name := range files {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return 0, fmt.Errorf("parse %s: %w", name, err)
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.TYPE {
				continue
			}
			for _, spec := range g.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.IsExported() {
					n++
				}
			}
		}
	}
	return n, nil
}

// Check verifies the plumbing package admits no responsibility, and that every
// declared home still holds one.
func Check(cfg Config) (Outcome, error) {
	out := Outcome{Homes: map[string]int{}}
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	dir := filepath.Join(root, filepath.FromSlash(governed))

	files, err := goFiles(dir)
	if err != nil {
		// Both errors are wrapped: the sentinel so callers can errors.Is on it, and
		// the cause so the reader learns WHY nothing was scanned (absent directory
		// versus unreadable one) without the message having to guess.
		return out, fmt.Errorf("%w: %s: %w", ErrNothingScanned, governed, err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		out.Scanned++
		if w := domainWordIn(name); w != "" {
			out.Violations = append(out.Violations, Violation{File: name, Where: "file name", Word: w})
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if perr != nil {
			return out, fmt.Errorf("parse %s: %w", name, perr)
		}
		for _, id := range exportedNames(f) {
			if w := domainWordIn(id); w != "" {
				out.Violations = append(out.Violations, Violation{File: name, Where: id, Word: w})
			}
		}
	}
	if out.Scanned == 0 {
		return out, fmt.Errorf("%w: %s holds no non-test file", ErrNothingScanned, governed)
	}

	var homeless []string
	for _, h := range homes {
		n, herr := countExportedTypes(filepath.Join(dir, h))
		if herr != nil || n == 0 {
			homeless = append(homeless, h)
			continue
		}
		out.Homes[h] = n
	}

	// Violations first: when both halves fail, the reader needs to see WHICH names do
	// not belong before being told where they should have gone.
	if len(out.Violations) > 0 {
		out.Violation = fmt.Errorf("%w: %s admits %d name(s) that belong to a responsibility",
			ErrForeignDomain, governed, len(out.Violations))
		return out, nil
	}
	if len(homeless) > 0 {
		out.Violation = fmt.Errorf("%w: %s under %s", ErrHomeMissing, strings.Join(homeless, ", "), governed)
		return out, nil
	}
	return out, nil
}

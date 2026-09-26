// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package ldflagsymbols refuses a `-X` injection that points at a symbol which
// does not exist.
//
// # Why it exists
//
// `go build -ldflags "-X path/to/pkg.Symbol=value"` over a path or symbol that
// does not exist is a **no-op that exits 0**. The linker does not warn — that is
// its documented behaviour — so the build succeeds, the binary keeps its default,
// and nothing anywhere says the injection did nothing.
//
// Measured (B-053): `api/Dockerfile:82-84` injected into
// `github.com/getloopscale/loopzero/api/internal/routes.versionTag`, with TWO compounded
// errors — the org is `getloopscale`, and the package moved to
// `internal/routes/version` during the B-041/B-080 decomposition. Live on
// 2026-08-16 the engine answered `{"version":"dev","built_at":"unknown"}`, and the
// fleet-status contract in loopzero-cloud had been reporting it as "no provenance"
// for an indeterminate time.
//
// # Why it resolves instead of matching
//
// A gate that grepped for the wrong org would have caught that one typo and
// nothing else. The failure that costs more is a package MOVE: a legitimate
// refactor relocates the symbol, the Dockerfile keeps the old path, and the
// injection silently stops working — which is exactly what the decomposition did.
//
// So this resolves: the import path against the `go.mod` files on disk, and the
// symbol against the package's AST. The assertion depends on what EXISTS, not on
// how somebody spelled it. That inversion is the correction three other gates in
// this repository still need (B-110 among them).
package ldflagsymbols

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// Config parameterises one run.
type Config struct {
	// Root is the repository root.
	Root string
}

// Violation is one `-X` injection that cannot land.
type Violation struct {
	// File is the Dockerfile, relative to Root.
	File string
	// Line is where the flag appears.
	Line int
	// ImportPath is the package the flag names.
	ImportPath string
	// Symbol is the variable the flag names.
	Symbol string
	// Reason says which resolution step failed, in the reader's language.
	Reason string
}

// Outcome is the result of one run.
type Outcome struct {
	Pass            bool
	DockerfilesRead int
	Injections      int
	Violations      []Violation
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Summary renders the one-line result the gate runner prints.
func (o Outcome) Summary() string {
	if o.Pass {
		return fmt.Sprintf("PASS — %d `-X` injection(s) resolve to a real symbol "+
			"(%d Dockerfile(s) read)", o.Injections, o.DockerfilesRead)
	}
	return fmt.Sprintf("FAIL — %d of %d `-X` injection(s) point at something that does not exist. "+
		"The linker accepts that silently and the binary keeps its default value",
		len(o.Violations), o.Injections)
}

// ActionableFix is printed under a failure.
const ActionableFix = `A ` + "`-X`" + ` injection only works when all three of these exist:

  1. the import path belongs to a module declared in a go.mod of this repository
  2. the corresponding directory exists
  3. the symbol is a top-level VARIABLE in that package (const and func do not take -X)

Copy the path from the package's own doc comment when it has one — that is the
reference that moves together with the code. Contract: B-053.`

// ldflagXRe captures `-X <importpath>.<symbol>=`. The path may carry dots (the
// domain does), so the split between path and symbol is taken at the LAST dot
// before the `=`, which is what the linker itself does.
var ldflagXRe = regexp.MustCompile(`-X\s+["']?([^\s"'=]+)=`)

// module is one go.mod found on disk.
type module struct {
	path string // module path declared in go.mod
	dir  string // directory containing go.mod, relative to Root
}

// discoverModules reads every go.mod under Root.
func discoverModules(root string) ([]module, error) {
	var mods []module
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "vendor" || n == "node_modules" || n == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		raw, rerr := os.ReadFile(filepath.Clean(path))
		if rerr != nil {
			return rerr
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if after, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
				rel, _ := filepath.Rel(root, filepath.Dir(path))
				mods = append(mods, module{path: strings.TrimSpace(after), dir: rel})
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Longest module path first: with nested modules (`api` and `api/internal/x`)
	// the more specific one owns the import, and matching the shorter first would
	// resolve into the wrong tree.
	sort.Slice(mods, func(i, j int) bool { return len(mods[i].path) > len(mods[j].path) })
	return mods, nil
}

// resolve maps an import path to a directory on disk, or reports why it cannot.
func resolve(mods []module, root, importPath string) (string, string) {
	for _, m := range mods {
		if importPath == m.path {
			return filepath.Join(root, m.dir), ""
		}
		if strings.HasPrefix(importPath, m.path+"/") {
			sub := strings.TrimPrefix(importPath, m.path+"/")
			return filepath.Join(root, m.dir, filepath.FromSlash(sub)), ""
		}
	}
	return "", fmt.Sprintf("no module in this repository declares that path — " +
		"check the org and the module name in go.mod")
}

// isStringSpec reports whether the i-th name of vs is a string variable:
// declared `string`, or untyped and initialized with a string literal.
func isStringSpec(vs *ast.ValueSpec, i int) bool {
	if vs.Type != nil {
		id, ok := vs.Type.(*ast.Ident)
		return ok && id.Name == "string"
	}
	if i >= len(vs.Values) {
		return false
	}
	lit, ok := vs.Values[i].(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

// packageHasStringVar reports whether dir declares symbol as a top-level var,
// and separately whether dir holds a Go package at all.
//
// The two are different facts and the distinction is load-bearing: a directory
// can exist as the PARENT of the package that moved — `internal/routes` survives
// as a parent of `internal/routes/version` — while holding no Go file. Reporting
// "the package exists but lacks the symbol" there would send the reader looking
// for a missing variable in a package that is not there.
//
// `-X` writes only into a package-level STRING variable. A const or a func with
// the same name would leave the gate green over an injection that stays a no-op,
// so the kind is checked, not just the name.
func packageHasStringVar(dir, symbol string) (hasSymbol, isPackage bool, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, false, err
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		isPackage = true
		parsed, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if perr != nil {
			return false, isPackage, fmt.Errorf("parsing %s: %w", e.Name(), perr)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if name.Name == symbol && isStringSpec(vs, i) {
						return true, true, nil
					}
				}
			}
		}
	}
	return false, isPackage, nil
}

// Check walks Root for Dockerfiles and resolves every `-X` injection they carry.
func Check(cfg Config) (Outcome, error) {
	mods, err := discoverModules(cfg.Root)
	if err != nil {
		return Outcome{}, fmt.Errorf("discovering modules: %w", err)
	}

	var out Outcome
	err = filepath.WalkDir(cfg.Root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if n := d.Name(); n == "vendor" || n == "node_modules" || n == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasPrefix(d.Name(), "Dockerfile") {
			return nil
		}
		raw, rerr := os.ReadFile(filepath.Clean(path))
		if rerr != nil {
			return rerr
		}
		out.DockerfilesRead++
		rel, _ := filepath.Rel(cfg.Root, path)

		for i, line := range strings.Split(string(raw), "\n") {
			for _, m := range ldflagXRe.FindAllStringSubmatch(line, -1) {
				target := m[1]
				dot := strings.LastIndex(target, ".")
				if dot < 0 {
					continue // not a `path.Symbol` form; nothing to resolve
				}
				importPath, symbol := target[:dot], target[dot+1:]
				out.Injections++

				v := Violation{File: rel, Line: i + 1, ImportPath: importPath, Symbol: symbol}
				dir, why := resolve(mods, cfg.Root, importPath)
				if why != "" {
					v.Reason = why
					out.Violations = append(out.Violations, v)
					continue
				}
				ok, isPkg, herr := packageHasStringVar(dir, symbol)
				if herr != nil {
					if os.IsNotExist(herr) {
						v.Reason = "the directory does not exist — the package may have been moved by a " +
							"refactor, and the Dockerfile was left pointing at the old location"
						out.Violations = append(out.Violations, v)
						continue
					}
					return herr
				}
				if !isPkg {
					// The directory exists and holds no Go package at all: that is what is
					// left when the package MOVED and the old path survived only as the
					// parent of the new one. Saying "the package exists, the symbol is
					// missing" would send the reader looking for a variable in a package
					// that is not there.
					v.Reason = "the directory exists but contains no Go package — the package probably " +
						"MOVED (the old path survives as the parent of the new one) and the Dockerfile did not follow"
					out.Violations = append(out.Violations, v)
					continue
				}
				if !ok {
					v.Reason = fmt.Sprintf("the package exists, but does not declare `%s` as a top-level variable "+
						"(`-X` does not write into a const or a func)", symbol)
					out.Violations = append(out.Violations, v)
				}
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("walking %s: %w", cfg.Root, err)
	}

	// Reading no Dockerfile at all means the walk ran against something that is
	// not this repository. "Nothing to check" and "everything is clean" are
	// different facts, and reporting the second for the first is how a gate goes
	// quiet without anyone noticing.
	if out.DockerfilesRead == 0 {
		return out, fmt.Errorf("no Dockerfile found under %s — a gate that reads nothing "+
			"verifies nothing", cfg.Root)
	}

	out.Pass = len(out.Violations) == 0
	return out, nil
}

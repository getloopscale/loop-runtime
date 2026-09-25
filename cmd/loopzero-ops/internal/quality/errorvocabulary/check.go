// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package errorvocabulary ensures that the error messages AUTHORED in the
// handlers do not carry the internal terms that Unbreakable Rule 3 forbids in
// user-facing output.
//
// Why the writer does not solve this: `httpio.WriteLoopZeroError` is pure
// pass-through — it neither invents vocabulary nor filters it, and that is
// correct. Fed a dirty message, it delivers the terms verbatim in the body. The
// risk surface is the ~277 messages written at the call sites (B-081), and among
// the gates there was none about their vocabulary.
//
// The gate IS BORN GREEN: measured 2026-08-19, no message in `routes/` or
// `services/` carries a banned term. It fixes nothing today — it prevents the
// next message from breaking the rule without anyone noticing.
package errorvocabulary

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// errorWriters are the functions whose argument reaches the response BODY.
// A literal inside the argument expression of one of them is text the user
// reads. Outside them, a literal containing "ArgoCD" is a log or an internal
// field name, and does not violate Rule 3.
var errorWriters = map[string]bool{
	"WriteLoopZeroError":                true,
	"WriteLoopZeroErrorWithFieldErrors": true,
	"WriteCodeErrorJSON":            true,
	"WriteErrorJSON":                true,
	"WriteJSONWithErrors":           true,
	"FieldError":                    true,
}

// bannedTerms is the list from Unbreakable Rule 3 (CLAUDE.md § The Rules).
// The comparison is case-insensitive: the rule lists `ArgoCD` and `argocd`
// separately, and what it protects is the user not reading the tool's name —
// letter case does not change that.
var bannedTerms = []string{
	"argocd",
	"gitops",
	"crd",
	"tenant_id",
	"advisory lock",
	"pg_advisory",
	"gitea",
	"minio",
	"kubebuilder",
	"envtest",
	"kaniko",
}

// Config drives the gate.
type Config struct {
	RepoRoot string
	// Roots are the scanned directories. Empty uses the default measured in B-081.
	Roots []string
}

// Violation is a dirty message, with the term that makes it dirty.
type Violation struct {
	File    string
	Line    int
	Term    string
	Message string
}

// Outcome carries the gate result.
type Outcome struct {
	Pass       bool
	Files      int
	Messages   int
	Violations []Violation
}

// Check runs the gate.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	roots := cfg.Roots
	if len(roots) == 0 {
		roots = []string{
			filepath.Join("api", "internal", "routes"),
			filepath.Join("api", "internal", "services"),
		}
	}

	for _, r := range roots {
		dir := filepath.Join(cfg.RepoRoot, r)
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				// A missing root is a FAILURE, never a skip: a scan that reaches
				// nothing reports green about nothing.
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			out.Files++
			return scanFile(cfg.RepoRoot, p, &out)
		})
		if err != nil {
			return out, fmt.Errorf("scanning %s: %w", r, err)
		}
	}

	sort.Slice(out.Violations, func(i, j int) bool {
		if out.Violations[i].File != out.Violations[j].File {
			return out.Violations[i].File < out.Violations[j].File
		}
		return out.Violations[i].Line < out.Violations[j].Line
	})
	out.Pass = len(out.Violations) == 0
	return out, nil
}

func scanFile(root, path string, out *Outcome) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	rel, _ := filepath.Rel(root, path)

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !errorWriters[sel.Sel.Name] {
			return true
		}
		// Descend into the WHOLE argument expression: the message usually comes from
		// a `.Wrap("...")` chained inside the call, not as a direct literal.
		for _, arg := range call.Args {
			ast.Inspect(arg, func(m ast.Node) bool {
				lit, ok := m.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				out.Messages++
				lower := strings.ToLower(text)
				for _, t := range bannedTerms {
					if strings.Contains(lower, t) {
						out.Violations = append(out.Violations, Violation{
							File:    rel,
							Line:    fset.Position(lit.Pos()).Line,
							Term:    t,
							Message: text,
						})
						break
					}
				}
				return true
			})
		}
		return true
	})
	return nil
}

// Summary describes the outcome.
func (o Outcome) Summary() string {
	return fmt.Sprintf("%d file(s) scanned, %d message(s) authored in an error writer, %d violation(s)",
		o.Files, o.Messages, len(o.Violations))
}

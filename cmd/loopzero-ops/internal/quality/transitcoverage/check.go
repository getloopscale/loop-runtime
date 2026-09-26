// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package transitcoverage is the Go home for the vault-prefix-format gate
// (T3.7 of plan 2026-06-02-openbao-adoption v1.3). Enforces the FAANG
// dual-path invariant that ALL ciphertext writes go through the
// encryptForStore dispatcher (which routes to Vault Transit when configured,
// AES legacy otherwise) — NEVER directly via s.encrypt() bypassing the dispatcher.
//
// Three invariants asserted statically, by parsing every non-test file of
// api/internal/db/store with go/parser:
//
//  1. vaultPrefix in secret_wrapper.go is declared as []byte("vault:v1:")
//     (canonical OpenBao Transit ciphertext prefix).
//
//  2. No PostgresSecretStore method outside the dispatcher calls the raw
//     s.encrypt primitive, and at least one calls s.encryptForStore.
//
//  3. No PostgresSecretStore method outside the dispatcher calls the raw
//     s.decrypt primitive, and at least one calls s.decryptFromStore.
//
// Rationale: post T3.5, the dispatcher chooses Vault Transit or AES based on
// runtime config. Bypassing the dispatcher would silently fall back to AES
// even when Vault was configured — exactly the regression vector we built
// the dual-path to prevent.
package transitcoverage

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

type Config struct {
	RepoRoot string
}

type Outcome struct {
	Pass       bool
	Violations []string
	Findings   map[string]string // metric name → value
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

const (
	// storeDir holds every file that may write or read secrets.encrypted_value.
	// The whole package is read, not one file: moving a bypass one file
	// sideways must not take it out of the gate's sight.
	storeDir          = "api/internal/db/store"
	secretWrapperPath = storeDir + "/secret_wrapper.go"
	storeType         = "PostgresSecretStore"
	canonicalPrefix   = "vault:v1:"
)

// ErrUnparseable means a store file is not valid Go: the gate cannot say what
// it calls, so it refuses to pass.
var ErrUnparseable = errors.New("a store file could not be parsed")

// Methods that are EXPECTED to call s.encrypt / s.decrypt directly because
// they ARE the dispatcher's underlying primitive. Calls OUTSIDE these methods
// (e.g., directly from Set() or ListForResync()) are violations.
var dispatcherInternalMethods = map[string]bool{
	"encryptForStore":  true,
	"decryptFromStore": true,
	"encrypt":          true, // the AES primitive itself
	"decrypt":          true,
}

// counts accumulates the calls made by PostgresSecretStore methods outside the
// dispatcher.
type counts struct {
	rawEnc, rawDec, dispEnc, dispDec int
}

// Check runs all three invariants and returns Outcome.
func Check(cfg Config) (*Outcome, error) {
	out := &Outcome{Findings: make(map[string]string)}
	dir := filepath.Join(cfg.RepoRoot, storeDir)
	if _, err := os.Stat(filepath.Join(cfg.RepoRoot, secretWrapperPath)); err != nil {
		out.Violations = append(out.Violations, fmt.Sprintf("cannot read %s: %v", secretWrapperPath, err))
		return out, nil
	}
	files, err := parseStore(dir)
	if err != nil {
		return nil, err
	}

	prefixFound := false
	var c counts
	for _, f := range files {
		if f.rel == secretWrapperPath && declaresVaultPrefix(f.file) {
			prefixFound = true
		}
		out.Violations = append(out.Violations, scanMethods(f, &c)...)
	}

	if prefixFound {
		out.Findings["vault_prefix"] = "found"
	} else {
		out.Violations = append(out.Violations,
			fmt.Sprintf("%s: vaultPrefix constant missing or does not match `[]byte(\"%s\")`",
				secretWrapperPath, canonicalPrefix))
	}
	out.Findings["raw_encrypt_outside_dispatcher"] = strconv.Itoa(c.rawEnc)
	out.Findings["raw_decrypt_outside_dispatcher"] = strconv.Itoa(c.rawDec)
	out.Findings["dispatcher_encrypt_uses"] = strconv.Itoa(c.dispEnc)
	out.Findings["dispatcher_decrypt_uses"] = strconv.Itoa(c.dispDec)

	// At least 1 dispatcher use each is expected (Set must call encryptForStore;
	// ListForResync must call decryptFromStore).
	if c.dispEnc < 1 {
		out.Violations = append(out.Violations,
			fmt.Sprintf("%s: no s.encryptForStore() call found outside dispatcher — Set() likely bypasses dual-path (T3.5)",
				storeDir))
	}
	if c.dispDec < 1 {
		out.Violations = append(out.Violations,
			fmt.Sprintf("%s: no s.decryptFromStore() call found outside dispatcher — read path likely bypasses dual-path (T3.4)",
				storeDir))
	}
	out.Pass = len(out.Violations) == 0
	return out, nil
}

type parsedFile struct {
	rel  string
	fset *token.FileSet
	file *ast.File
}

// parseStore parses every non-test Go file of the store package, in name order.
func parseStore(dir string) ([]parsedFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", storeDir, err)
	}
	var out []parsedFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrUnparseable, name, err)
		}
		out = append(out, parsedFile{rel: storeDir + "/" + name, fset: fset, file: f})
	}
	return out, nil
}

// declaresVaultPrefix reports whether f declares `vaultPrefix = []byte("vault:v1:")`.
func declaresVaultPrefix(f *ast.File) bool {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if n.Name == "vaultPrefix" && i < len(vs.Values) && isBytesLiteral(vs.Values[i], canonicalPrefix) {
					return true
				}
			}
		}
	}
	return false
}

func isBytesLiteral(e ast.Expr, want string) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	at, ok := call.Fun.(*ast.ArrayType)
	if !ok || at.Len != nil {
		return false
	}
	if id, ok := at.Elt.(*ast.Ident); !ok || id.Name != "byte" {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	v, err := strconv.Unquote(lit.Value)
	return err == nil && v == want
}

// scanMethods inspects every PostgresSecretStore method of f outside the
// dispatcher and reports each raw s.encrypt / s.decrypt call it makes.
func scanMethods(f parsedFile, c *counts) []string {
	var violations []string
	for _, decl := range f.file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil || !isStoreMethod(fd) || dispatcherInternalMethods[fd.Name.Name] {
			continue
		}
		recv := receiverName(fd)
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); !ok || id.Name != recv {
				return true
			}
			line := f.fset.Position(call.Pos()).Line
			switch sel.Sel.Name {
			case "encrypt":
				c.rawEnc++
				violations = append(violations, fmt.Sprintf(
					"%s:%d: %s() calls s.encrypt() directly — must use s.encryptForStore() dispatcher (T3.5)",
					f.rel, line, fd.Name.Name))
			case "decrypt":
				c.rawDec++
				violations = append(violations, fmt.Sprintf(
					"%s:%d: %s() calls s.decrypt() directly — must use s.decryptFromStore() dispatcher (T3.4)",
					f.rel, line, fd.Name.Name))
			case "encryptForStore":
				c.dispEnc++
			case "decryptFromStore":
				c.dispDec++
			}
			return true
		})
	}
	return violations
}

func isStoreMethod(fd *ast.FuncDecl) bool {
	if fd.Recv == nil || len(fd.Recv.List) != 1 {
		return false
	}
	t := fd.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	id, ok := t.(*ast.Ident)
	return ok && id.Name == storeType
}

func receiverName(fd *ast.FuncDecl) string {
	if names := fd.Recv.List[0].Names; len(names) == 1 {
		return names[0].Name
	}
	return ""
}

// FormatReport returns a human-readable summary suitable for CLI output. The
// findings are printed in key order so two runs over one tree print the same
// report.
func (o *Outcome) FormatReport() string {
	var b strings.Builder
	if o.Pass {
		fmt.Fprintf(&b, "transit-coverage: PASS\n")
	} else {
		fmt.Fprintf(&b, "transit-coverage: FAIL\n")
		for _, v := range o.Violations {
			fmt.Fprintf(&b, "  %s\n", v)
		}
	}
	keys := make([]string, 0, len(o.Findings))
	for k := range o.Findings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s: %s\n", k, o.Findings[k])
	}
	return b.String()
}

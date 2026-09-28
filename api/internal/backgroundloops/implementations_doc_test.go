// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package backgroundloops

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests make the "Real implementations are required" block of the package
// doc in clients.go a checked contract rather than a convention. The block lived
// in api/internal/adapters until api/internal was organized by feature
// (2026-09-25) and moved to the composition root that wires those ports. It drifted for six months after commit
// ad6de675f deleted one of the ports it listed, together with that port's caller and
// its wiring. Rationale: .squad/wiki/decisions/B-022-health-prober-disposition.md

const (
	moduleRoot = "../.."                              // the api module root, from this package dir
	markerLine = "Real implementations are required:" // opens the block inside clients.go
)

type docRow struct{ Port, Path string }

var docRowRE = regexp.MustCompile(`^//\s*-\s*(\S+)\s*(?:\x{2192}|->)\s*(\S+)\s*$`)

func docContractRows(t *testing.T) []docRow {
	t.Helper()
	src, err := os.ReadFile("clients.go")
	if err != nil {
		t.Fatalf("read clients.go: %v", err)
	}
	var rows []docRow
	started := false
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if !started {
			started = strings.Contains(line, markerLine)
			continue
		}
		if !strings.HasPrefix(line, "//") {
			break // reached `package backgroundloops`
		}
		if m := docRowRE.FindStringSubmatch(line); m != nil {
			rows = append(rows, docRow{Port: m[1], Path: m[2]})
		}
	}
	// Emptying the block is the one way a guard over a list silently stops guarding.
	if len(rows) == 0 {
		t.Fatal("parsed 0 rows from the required-implementations block - it would pass vacuously")
	}
	return rows
}

func TestDocCommentPathsResolveOnDisk(t *testing.T) {
	for _, row := range docContractRows(t) {
		rel := strings.TrimPrefix(row.Path, "api/")
		if _, err := os.Stat(filepath.Join(moduleRoot, rel)); err != nil {
			t.Errorf("doc comment cites %s for port %s, which is not on disk; "+
				"repoint the row at the real file or delete the row", row.Path, row.Port)
		}
	}
}

func declaredTypes(t *testing.T) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "vendor" || n == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		// Non-test files only: the block requires *real* implementations, not fakes.
		if n := d.Name(); !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil // `go build` is the right messenger for a module that does not parse
		}
		for _, decl := range f.Decls {
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						names[ts.Name.Name] = true
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", moduleRoot, err)
	}
	if len(names) == 0 {
		t.Fatalf("walked %s and found no type declarations - it would pass vacuously", moduleRoot)
	}
	return names
}

func TestDocCommentNamesOnlyDeclaredPorts(t *testing.T) {
	declared := declaredTypes(t)
	for _, row := range docContractRows(t) {
		if !declared[row.Port] {
			t.Errorf("doc comment names port %s, which is declared nowhere in the module; "+
				"declare the port or delete the row", row.Port)
		}
	}
}

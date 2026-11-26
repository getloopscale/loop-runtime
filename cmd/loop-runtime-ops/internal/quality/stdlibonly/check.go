// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package stdlibonly refuses a dependency-free package that stopped being dependency-free.
//
// # The defect it prevents, measured
//
// B-057: the shared vocabulary package, api/internal/ports (renamed from
// api/internal/domain in c571c7180), is imported by 90 files of the api module, 49 of
// them production code (measured 2026-09-25 with `git grep`; the number moves with the
// tree). It can be imported that widely precisely because it drags no dependency along:
// `go list` reports 9 imports, all from the standard library.
//
// A single non-standard import there propagates to every importer, and nothing
// prevented it. The property held by discipline, not by contract.
//
// # Why an absent package fails
//
// A rename that moves the directory would leave the guard green forever over nothing —
// the `PASS — 0` shape this repository has already paid to learn. Absence fails, and
// whoever renames the package updates the path deliberately.
//
// # The limit
//
// It reads the declared imports of the named package, not the transitive tree. A
// standard-library import that starts doing network I/O is still standard library and
// passes here.
package stdlibonly

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// ErrExternalImport means the package imports something outside the standard library.
var ErrExternalImport = errors.New("the dependency-free package imported something outside the standard library")

// ErrNothingScanned means the guarded package is absent — silence, not cleanliness.
var ErrNothingScanned = errors.New("the guarded package is absent — finding nothing is not the same as finding nothing wrong")

// guardedPackage is the package whose property this guard protects.
//
// It is one path on purpose: the property was MEASURED here, and extending the guard to
// packages whose property nobody measured would assert what is not known.
const guardedPackage = "api/internal/ports"

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Outcome reports what was established.
type Outcome struct {
	Scanned  int
	External []string
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

// Check verifies that the guarded package imports only the standard library.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	dir := filepath.Join(root, filepath.FromSlash(guardedPackage))

	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return out, fmt.Errorf("%w: %s", ErrNothingScanned, guardedPackage)
	}
	if err != nil {
		return out, fmt.Errorf("reading %s: %w", guardedPackage, err)
	}

	fset := token.NewFileSet()
	seen := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			return out, fmt.Errorf("parsing %s: %w", name, err)
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return out, fmt.Errorf("reading an import path in %s: %w", name, err)
			}
			if seen[path] {
				continue
			}
			seen[path] = true
			out.Scanned++
			// A standard-library import never has a dot in its first segment: the dot is
			// what marks a host (`github.com/...`, `golang.org/x/...`).
			if strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
				out.External = append(out.External, path)
			}
		}
	}

	if out.Scanned == 0 {
		return out, fmt.Errorf("%w: %s declares no import at all", ErrNothingScanned, guardedPackage)
	}
	if len(out.External) > 0 {
		out.Violation = fmt.Errorf("%w: %s imports %s — and that reaches every one of its importers",
			ErrExternalImport, guardedPackage, strings.Join(out.External, ", "))
		return out, nil
	}
	return out, nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// loopruntime-cloud is the only component that talks to Stripe. Who issues the
// portal session is an OPEN entry in docs/decisions.md (2026-08-11, not yet
// ratified); until it is decided otherwise, the engine must not regain the
// capability by default. While the Stripe library is imported here, seeding a
// Secret is enough to bring back a second issuer, with no new code and no
// review. The guard targets the CAPABILITY — the import — not configuration,
// which is environment state.
func TestEngineDoesNotImportTheStripeLibrary(t *testing.T) {
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(moduleRoot, "go.mod")); err != nil {
		t.Fatalf("api module root not found at %s: %v", moduleRoot, err)
	}
	fset := token.NewFileSet()
	var offenders []string
	scanned := 0
	err = filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		scanned++
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(p, "github.com/stripe/stripe-go") {
				rel, _ := filepath.Rel(moduleRoot, path)
				offenders = append(offenders, rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk api module: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned no Go file — the guard measured nothing")
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("the engine imports the Stripe library in %d file(s): %s\n"+
			"loopruntime-cloud is the only portal-session issuer; keeping the library here keeps the "+
			"capability one Secret away from coming back.", len(offenders), strings.Join(offenders, ", "))
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package stdlibonly_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/stdlibonly"
)

func portsPackage(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "api", "internal", "ports", "types.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// The shared vocabulary package imports ONLY the standard library, and that must be
// verified rather than assumed: one non-standard import there reaches every importer.
func TestStdlibOnly_CleanPackagePasses(t *testing.T) {
	root := portsPackage(t, `package domain

import (
	"context"
	"errors"
	"time"
)

var _ = context.Background
var _ = errors.New
var _ = time.Now
`)
	out, err := violationOf(stdlibonly.Check(stdlibonly.Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("a stdlib-only package should pass; got %v", err)
	}
	if out.Scanned == 0 {
		t.Fatal("zero imports scanned — green over nothing")
	}
}

// A non-standard import is the defect: it reaches every importer.
func TestStdlibOnly_ExternalImportIsRefused(t *testing.T) {
	root := portsPackage(t, `package domain

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

var _ = context.Background
var _ = prometheus.NewCounter
`)
	out, err := violationOf(stdlibonly.Check(stdlibonly.Config{RepoRoot: root}))
	if !errors.Is(err, stdlibonly.ErrExternalImport) {
		t.Fatalf("want ErrExternalImport, got %v", err)
	}
	if len(out.External) != 1 || out.External[0] != "github.com/prometheus/client_golang/prometheus" {
		t.Errorf("the finding must name the import: %v", out.External)
	}
}

// An import of another package of THIS repository breaks the property too — it is not
// third-party, but it drags the same coupling into every importer.
func TestStdlibOnly_InternalImportIsAlsoRefused(t *testing.T) {
	root := portsPackage(t, `package domain

import "github.com/getloopscale/loop-runtime/pkg/errors"

var _ = errors.New
`)
	if _, err := violationOf(stdlibonly.Check(stdlibonly.Config{RepoRoot: root})); !errors.Is(err, stdlibonly.ErrExternalImport) {
		t.Fatalf("an import from this repository breaks the property too; got %v", err)
	}
}

// An absent package is an error, not silence: a rename that moves the directory would leave
// the guard green forever over nothing.
func TestStdlibOnly_MissingPackageIsRefused(t *testing.T) {
	if _, err := violationOf(stdlibonly.Check(stdlibonly.Config{RepoRoot: t.TempDir()})); !errors.Is(err, stdlibonly.ErrNothingScanned) {
		t.Fatal("an absent package should fail, not pass silently")
	}
}

// The real population: 9 imports today, all standard library.
func TestStdlibOnly_RealTree(t *testing.T) {
	out, err := violationOf(stdlibonly.Check(stdlibonly.Config{RepoRoot: qtestutil.Root(t)}))
	if err != nil {
		t.Fatalf("the real tree broke the property: %v (external=%v)", err, out.External)
	}
	if out.Scanned == 0 {
		t.Fatal("zero imports scanned in the real tree")
	}
	t.Logf("real population: %d import(s) in api/internal/ports, all standard library", out.Scanned)
}

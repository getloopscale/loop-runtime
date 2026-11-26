// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package sqlctxprop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
)

func store(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, defaultStoreDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const header = `package store

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)
`

func TestTxThreadedThroughEveryCallPasses(t *testing.T) {
	root := store(t, map[string]string{"s.go": header + `
func (s *S) Insert(ctx context.Context, tx db.DBTX) error {
	_, err := s.q.InsertBuild(ctx, s.gen(tx), generated.InsertBuildParams{})
	return err
}

func (s *S) Get(ctx context.Context, id string) error {
	_, err := s.q.GetBuild(ctx, s.gen(nil), id)
	return err
}
`})

	out, err := Check(Config{RepoRoot: root})

	if err != nil || !out.Pass {
		t.Fatalf("threading tx, and using the pool outside a transaction, both pass; out=%+v err=%v", out, err)
	}
	if out.TxFunctions != 1 {
		t.Errorf("TxFunctions = %d, want 1", out.TxFunctions)
	}
}

// The R-DS-1 defect: a function that received the caller's transaction runs one
// query on the pool instead — a different connection from the lock holder.
func TestPoolCallInsideATransactionalFunctionFails(t *testing.T) {
	root := store(t, map[string]string{"s.go": header + `
func (s *S) Activate(ctx context.Context, tx db.DBTX, id string) error {
	if _, err := s.q.GetBuild(ctx, s.gen(nil), id); err != nil {
		return err
	}
	_, err := s.q.InsertBuild(ctx, s.gen(tx), generated.InsertBuildParams{})
	return err
}
`})

	out, err := Check(Config{RepoRoot: root})

	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass || len(out.Violations) != 1 || out.Violations[0].Function != "Activate" {
		t.Fatalf("gen(nil) inside a tx function must be one violation in Activate; got %+v", out.Violations)
	}
}

func TestPoolFieldPassedInsideATransactionalFunctionFails(t *testing.T) {
	root := store(t, map[string]string{"s.go": header + `
func (s *S) Activate(ctx context.Context, tx db.DBTX, id string) error {
	_, err := s.q.GetBuild(ctx, s.pool, id)
	return err
}
`})

	out, err := Check(Config{RepoRoot: root})

	if err != nil || out.Pass {
		t.Fatalf("passing the pool inside a tx function must fail; out=%+v err=%v", out, err)
	}
	if !strings.Contains(out.Violations[0].Detail, "pool") {
		t.Errorf("the violation must say the pool was used: %+v", out.Violations[0])
	}
}

func TestFileNotImportingGeneratedIsNotScanned(t *testing.T) {
	root := store(t, map[string]string{
		"s.go":     header + "\nfunc (s *S) X(ctx context.Context, tx db.DBTX) error { _, err := s.q.Y(ctx, s.gen(tx)); return err }\n",
		"plain.go": "package store\n\nimport \"github.com/getloopscale/loop-runtime/api/internal/db\"\n\nfunc (s *S) Z(tx db.DBTX) { s.gen(nil) }\n",
	})

	out, err := Check(Config{RepoRoot: root})

	if err != nil || !out.Pass || out.Scanned != 1 {
		t.Fatalf("only files importing generated are in scope; out=%+v err=%v", out, err)
	}
}

func TestNoTransactionalFunctionIsNothingScanned(t *testing.T) {
	root := store(t, map[string]string{"s.go": header + "\nfunc (s *S) Get(ctx context.Context) { _, _ = s.q.X(ctx, s.gen(nil)) }\n"})

	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("a scan that met no transactional function checked nothing; err=%v", err)
	}
}

func TestMissingStoreDirIsAnError(t *testing.T) {
	if _, err := Check(Config{RepoRoot: t.TempDir()}); err == nil {
		t.Fatal("a missing store dir is an error, not a pass")
	}
}

func TestUnparseableFileIsAnError(t *testing.T) {
	root := store(t, map[string]string{"s.go": "package store\nfunc {"})

	if _, err := Check(Config{RepoRoot: root}); err == nil {
		t.Fatal("an unparseable file must be an error")
	}
}

func TestRealTreePasses(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.Root(t)})
	if err != nil {
		t.Fatalf("real tree: %v", err)
	}
	if !out.Pass {
		t.Fatalf("real tree violations: %+v", out.Violations)
	}
}

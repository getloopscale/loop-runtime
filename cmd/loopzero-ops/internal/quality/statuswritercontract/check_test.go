// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statuswritercontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write lays a Go file into a fake operators tree.
func write(t *testing.T, root, rel, src string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

const preamble = "package x\n\nimport (\n\t\"context\"\n)\n\nfunc f(ctx context.Context) error {\n"

func TestCleanTreePasses(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/app/a.go",
		preamble+"\treturn r.Status().Patch(ctx, o, client.MergeFrom(base))\n}\n")

	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Pass {
		t.Fatalf("a merge patch is the prescribed shape and must pass: %v", out.Violations)
	}
	if out.FilesScanned == 0 {
		t.Fatal("scanned nothing; a gate that reads no file verifies no file")
	}
}

func TestBareStatusUpdateIsRefused(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/environment/env.go",
		preamble+"\treturn r.Status().Update(ctx, o)\n}\n")

	out, _ := Check(Config{Root: root})
	if out.Pass {
		t.Fatal("a bare Status().Update passed. This is the write the contract forbids: it replays " +
			"the whole object and silently loses any concurrent spec or metadata write (B-085).")
	}
	if len(out.Violations) != 1 || !strings.Contains(out.Violations[0].File, "env.go") {
		t.Fatalf("the violation does not name the offending file: %+v", out.Violations)
	}
}

// The three shapes the review of slice-27 proved evade a naive match. Each one
// compiles and persists status; a gate that misses them reports clean while the
// defect is live.
func TestEvasionsAreRefused(t *testing.T) {
	cases := map[string]string{
		"local alias":  "\tsw := r.Status()\n\treturn sw.Update(ctx, o)\n}\n",
		"sub resource": "\treturn r.SubResource(\"status\").Update(ctx, o)\n}\n",
		"alias of sub": "\tsw := r.SubResource(\"status\")\n\treturn sw.Update(ctx, o)\n}\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "operators/internal/tenant/t.go", preamble+body)

			out, _ := Check(Config{Root: root})
			if out.Pass {
				t.Fatalf("the %q shape passed. It reaches the same status subresource by a spelling "+
					"the gate does not recognise — which is how a fence stops being one.", name)
			}
		})
	}
}

func TestTestFilesAreNotJudged(t *testing.T) {
	root := t.TempDir()
	// A production file alongside it, because a tree with ONLY a test file is
	// empty as far as this gate is concerned — and that case is asserted
	// separately, by TestEmptyTreeIsAFailureNotAPass.
	write(t, root, "operators/internal/app/a.go",
		preamble+"\treturn r.Status().Patch(ctx, o, client.MergeFrom(base))\n}\n")
	write(t, root, "operators/internal/app/a_test.go",
		preamble+"\treturn r.Status().Update(ctx, o)\n}\n")

	out, _ := Check(Config{Root: root})
	if !out.Pass {
		t.Fatalf("a _test.go was judged. Tests legitimately seed state with Update, and judging them "+
			"would push authors to weaken the gate rather than the test: %v", out.Violations)
	}
}

func TestCommentsAreNotCode(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/app/a.go",
		"package x\n\n// The old shape was r.Status().Update(ctx, o) and it is forbidden.\n/* also r.Status().Update(x) */\nfunc f() {}\n")

	out, _ := Check(Config{Root: root})
	if !out.Pass {
		t.Fatalf("a comment quoting the forbidden pattern was counted as code. Measured once already "+
			"in slice-21, where it produced a false FAIL: %v", out.Violations)
	}
}

func TestEmptyTreeIsAFailureNotAPass(t *testing.T) {
	root := t.TempDir()

	out, err := Check(Config{Root: root})
	if err == nil && out.Pass {
		t.Fatal("an operators tree with no file scanned reported PASS. A gate that finds nothing must " +
			"say so — 'nothing to check' and 'everything is clean' are different facts, and reporting " +
			"the second for the first is how a gate goes quiet without anyone noticing.")
	}
}

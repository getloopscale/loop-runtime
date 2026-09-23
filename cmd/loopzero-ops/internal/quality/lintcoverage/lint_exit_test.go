// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package lintcoverage

import "testing"

func TestLintFindings_CleanRunIsZero(t *testing.T) {
	n, err := lintFindings(0, []byte("0 issues.\n"))
	if err != nil || n != 0 {
		t.Fatalf("want 0 findings and no error, got %d %v", n, err)
	}
}

func TestLintFindings_IssuesExitCountsTheLines(t *testing.T) {
	out := []byte("a/b.go:1:2: unused\nc/d.go:3:4: errcheck\n")
	n, err := lintFindings(1, out)
	if err != nil || n != 2 {
		t.Fatalf("want 2 findings, got %d %v", n, err)
	}
}

// A crash (config error, type-check failure) exits non-zero and prints no
// finding lines. Counting that as zero findings would record a free
// "improvement" for a module nobody measured.
func TestLintFindings_CrashIsAMeasurementFailure(t *testing.T) {
	if _, err := lintFindings(3, []byte("level=error msg=\"can't load config\"\n")); err == nil {
		t.Fatal("a non-issues exit code must be a measurement failure")
	}
}

func TestLintFindings_IssuesExitWithoutFindingLinesIsAMeasurementFailure(t *testing.T) {
	if _, err := lintFindings(1, []byte("typecheck: something went wrong\n")); err == nil {
		t.Fatal("exit 1 with no parseable finding is not zero findings")
	}
}

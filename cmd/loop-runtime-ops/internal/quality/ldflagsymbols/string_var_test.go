// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package ldflagsymbols

import (
	"os"
	"path/filepath"
	"testing"
)

func pkgWith(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "v.go"), []byte("package v\n\n"+src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// `-X` writes only into a package-level string variable; a var of another
// kind with the right name leaves the injection a silent no-op.
func TestPackageHasStringVar_AcceptsOnlyStringVars(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"var Version string\n", true},
		{"var Version = \"dev\"\n", true},
		{"var (\n\tA, Version string\n)\n", true},
		{"var Version int\n", false},
		{"var Version = 3\n", false},
		{"var Version []byte\n", false},
		{"const Version = \"dev\"\n", false},
		{"func Version() string { return \"\" }\n", false},
	}
	for _, c := range cases {
		got, isPkg, err := packageHasStringVar(pkgWith(t, c.src), "Version")
		if err != nil || !isPkg {
			t.Fatalf("%q: err=%v isPkg=%v", c.src, err, isPkg)
		}
		if got != c.want {
			t.Errorf("%q: got %v, want %v", c.src, got, c.want)
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package ldflagsymbols

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a throwaway repo: one module, one package, one Dockerfile.
func fixture(t *testing.T, module, pkgDir, pkgSrc, dockerfile string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module "+module+"\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	if pkgDir != "" {
		full := filepath.Join(root, pkgDir)
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(full, "x.go"), []byte(pkgSrc), 0o600); err != nil {
			t.Fatalf("pkg src: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte(dockerfile), 0o600); err != nil {
		t.Fatalf("dockerfile: %v", err)
	}
	return root
}

const pkgWithSymbols = `package version

var (
	versionTag    = "dev"
	versionCommit = "unknown"
)
`

// TestCheck_ResolvesThePathAgainstReality is the whole point of this gate.
//
// `-X <importpath>.<symbol>` over a path that does not exist is a NO-OP that
// exits 0. The Go linker does not warn, by design, so the build succeeds and the
// binary keeps its default — measured live on 2026-08-16 as
// `{"version":"dev","built_at":"unknown"}` (B-053).
//
// A gate that matched the spelling of the wrong org would catch that one typo and
// nothing else. The failure that actually costs more is a package MOVE: a
// refactor relocates the symbol, the Dockerfile keeps the old path, and the
// injection silently stops working. Resolving the path against the go.mod and the
// symbol against the AST is what makes the assertion depend on what EXISTS.
func TestCheck_ResolvesThePathAgainstReality(t *testing.T) {
	const mod = "github.com/getloopscale/loop-runtime/api"

	cases := []struct {
		name       string
		dockerfile string
		pkgDir     string
		pkgSrc     string
		wantPass   bool
		wantIn     string
	}{
		{
			name:       "path and symbol exist",
			dockerfile: "RUN go build -ldflags \"-X " + mod + "/internal/routes/version.versionTag=v1\"\n",
			pkgDir:     "internal/routes/version", pkgSrc: pkgWithSymbols,
			wantPass: true,
		},
		{
			name: "wrong org — the defect measured in B-053",
			// One word apart: getloopscale versus getloopscaledev. The linker accepts it and injects nothing.
			dockerfile: "RUN go build -ldflags \"-X github.com/getloopscaledev/loopruntime/api/internal/routes/version.versionTag=v1\"\n",
			pkgDir:     "internal/routes/version", pkgSrc: pkgWithSymbols,
			wantPass: false, wantIn: "no module",
		},
		{
			name: "package moved — the failure a legitimate refactor introduces",
			// The symbol lives in internal/routes/version; the Dockerfile points at the
			// flat package that the decomposition emptied.
			dockerfile: "RUN go build -ldflags \"-X " + mod + "/internal/routes.versionTag=v1\"\n",
			pkgDir:     "internal/routes/version", pkgSrc: pkgWithSymbols,
			// The reason has to say MOVED, not "the symbol is missing". The directory
			// `internal/routes` EXISTS — it survives as the parent of `version` — and
			// holds no Go package at all. Saying "the package exists, the variable is
			// missing" would send the reader looking in a package that is not there,
			// which was the first message I wrote and the one this case failed.
			wantPass: false, wantIn: "MOVED",
		},
		{
			name:       "package exists but the symbol does not",
			dockerfile: "RUN go build -ldflags \"-X " + mod + "/internal/routes/version.versionSHA=abc\"\n",
			pkgDir:     "internal/routes/version", pkgSrc: pkgWithSymbols,
			wantPass: false, wantIn: "versionSHA",
		},
		{
			name:       "Dockerfile with no -X at all is not a violation",
			dockerfile: "FROM alpine\nRUN echo hi\n",
			pkgDir:     "internal/routes/version", pkgSrc: pkgWithSymbols,
			wantPass: true,
		},
		{
			name: "symbol that is not a top-level variable does not count as existing",
			// `-X` only writes into a package-level STRING variable. A constant or a
			// function with the same name would leave the gate green over an injection
			// that is still a no-op.
			dockerfile: "RUN go build -ldflags \"-X " + mod + "/internal/routes/version.versionTag=v1\"\n",
			pkgDir:     "internal/routes/version",
			pkgSrc:     "package version\n\nconst versionTag = \"dev\"\n",
			wantPass:   false, wantIn: "versionTag",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t, mod, tc.pkgDir, tc.pkgSrc, tc.dockerfile)
			out, err := Check(Config{Root: root})
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if out.Pass != tc.wantPass {
				t.Fatalf("Pass = %v, want %v (violations: %+v)", out.Pass, tc.wantPass, out.Violations)
			}
			if tc.wantIn != "" {
				joined := ""
				for _, v := range out.Violations {
					joined += v.Reason + " " + v.Symbol + " "
				}
				if !strings.Contains(joined, tc.wantIn) {
					t.Errorf("the violation does not name the cause (%q): %+v", tc.wantIn, out.Violations)
				}
			}
		})
	}
}

// TestCheck_TreeWithoutDockerfileIsFailure keeps the invariant every gate in this tree
// shares: "nothing to check" and "everything is clean" are different facts, and
// reporting the second for the first is how a gate goes quiet.
func TestCheck_TreeWithoutDockerfileIsFailure(t *testing.T) {
	if _, err := Check(Config{Root: t.TempDir()}); err == nil {
		t.Fatal("a tree with no Dockerfile at all was accepted as clean")
	}
}

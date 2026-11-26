// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Regression-lock: no source under cmd/loopruntime-ops may cite the `pkg/quality/`
// path. That package was relocated to `cmd/loopruntime-ops/internal/quality/` at M9
// (ROADMAP steps 3+4, "pkg/quality boundary misfit") and `pkg/quality/` no
// longer exists on disk.
//
// This is not cosmetic. Most of the surviving references sit inside
// `fmt.Fprintln(os.Stderr, ...)` remediation hints — the text a gate prints
// to an operator at the moment it fails. Telling someone to "add the alert
// name to ExceptionList (pkg/quality/alertrunbooks)" sends them to a
// directory that is not there; the gate reads as broken and the actual
// exception never gets added. A wrong instruction at the failure boundary is
// worse than no instruction, because it is followed.
//
// Found while auditing issue #255 (gates registered but never run): the
// gates' own output still described the pre-M9 tree.
//
// SCOPE: asserts the literal path token only. It deliberately does not
// validate that the *target* package exists — `check_stale_paths` style
// resolution belongs in a separate guard if it is ever wanted.
package repoinvariants

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

// stalePkgQualityToken is the pre-M9 path. Kept as a single const so the
// negative control and the guard cannot drift apart.
const stalePkgQualityToken = "pkg/quality/"

// canonicalQualityPath is where the packages actually live post-M9.
const canonicalQualityPath = "cmd/loopruntime-ops/internal/quality/"

// collectGoSources walks cmd/loopruntime-ops returning path -> body for every .go
// file, including tests: a stale path in a test fixture is equally misleading
// to whoever reads it next.
func collectGoSources(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join(qtestutil.MustRepoRoot(), "cmd", "loopruntime-ops")

	out := map[string]string{}
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip the guards themselves — each must name the stale token to detect
		// it. This named ONE file until 2026-09-16, and a second guard written
		// later was not covered: it reported itself from the day it landed.
		if isStalePkgQualityGuard(path) {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk cmd/loopruntime-ops: %v", walkErr)
	}
	if len(out) == 0 {
		t.Fatalf("no .go sources found under cmd/loopruntime-ops — guard would pass vacuously")
	}
	return out
}

// findStalePkgQuality returns "relpath:lineno" for every line citing the
// pre-M9 path.
func findStalePkgQuality(sources map[string]string) []string {
	var hits []string
	for rel, body := range sources {
		for i, line := range strings.Split(body, "\n") {
			if strings.Contains(line, stalePkgQualityToken) {
				hits = append(hits, rel+":"+itoa(i+1))
			}
		}
	}
	return hits
}

// itoa avoids pulling strconv for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestNoStalePkgQualityPath is the guard.
func TestNoStalePkgQualityPath(t *testing.T) {
	hits := findStalePkgQuality(collectGoSources(t))
	for _, h := range hits {
		t.Errorf("%s cites %q, which was relocated to %q at M9 and no longer exists.\n"+
			"    Most of these live in operator-facing remediation hints — the gate tells the\n"+
			"    operator to edit a directory that is not there, so the fix never lands.",
			h, stalePkgQualityToken, canonicalQualityPath)
	}
}

// TestNoStalePkgQualityPath_NegativeControl proves the guard detects the
// pattern it exists to catch, rather than passing because the matcher is
// broken.
func TestNoStalePkgQualityPath_NegativeControl(t *testing.T) {
	synthetic := map[string]string{
		"synthetic_gate.go": "package main\n// hint: bump the baseline in pkg/quality/baseimagedigest\n",
	}
	hits := findStalePkgQuality(synthetic)
	if len(hits) == 0 {
		t.Fatalf("negative control did NOT fire: the matcher cannot see %q even when present", stalePkgQualityToken)
	}
	if hits[0] != "synthetic_gate.go:2" {
		t.Errorf("negative control reported %q, want synthetic_gate.go:2 — line attribution is off, so real failures would point at the wrong line", hits[0])
	}
}
